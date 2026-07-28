package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"tokhub/internal/connections"
	secretcrypto "tokhub/internal/crypto"
	gatewaycache "tokhub/internal/gateway"
	"tokhub/internal/store"
)

type createAIConnectionRequest struct {
	Provider        string   `json:"provider"`
	Region          string   `json:"region"`
	WorkspaceID     string   `json:"workspaceId"`
	DisplayName     string   `json:"displayName"`
	APIKey          string   `json:"apiKey"`
	Models          []string `json:"models"`
	ConfirmBillable bool     `json:"confirmBillable"`
}

type rotateAIConnectionRequest struct {
	APIKey          string `json:"apiKey"`
	ConfirmBillable bool   `json:"confirmBillable"`
}

type validateAIConnectionRequest struct {
	ConfirmBillable bool `json:"confirmBillable"`
}

type quickRelayRequest struct {
	ModelIDs   []string `json:"modelIds"`
	Name       string   `json:"name"`
	Policy     string   `json:"policy"`
	QPSLimit   int      `json:"qpsLimit"`
	QuotaMonth int      `json:"quotaMonth"`
}

var (
	connectionModelPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/-]{0,159}$`)
	idempotencyKeyPattern  = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{7,127}$`)
)

const aiConnectionRequestBodyLimit = 64 << 10

func (s *Server) meAIConnectionProviders(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"items":         connections.ProviderRegistry(),
		"policyVersion": "official-developer-credentials-v1",
		"credentialPolicy": map[string]any{
			"accepted": []string{"official developer API keys"},
			"rejected": []string{"passwords", "one-time codes", "browser cookies", "consumer session tokens", "CLI OAuth sessions"},
		},
	})
}

func (s *Server) meAIConnections(w http.ResponseWriter, r *http.Request) {
	user, _ := s.userFromRequest(r)
	orgID, ok := s.personalAIConnectionWorkspace(w, r, user)
	if !ok {
		return
	}
	items, err := s.repo.AIConnectionsForOwnerOrg(r.Context(), user.ID, orgID)
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "ai_connections_unavailable", "Could not load AI service connections")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) meAIConnection(w http.ResponseWriter, r *http.Request) {
	user, _ := s.userFromRequest(r)
	orgID, ok := s.personalAIConnectionWorkspace(w, r, user)
	if !ok {
		return
	}
	item, err := s.repo.AIConnectionForOwnerOrg(r.Context(), user.ID, orgID, chi.URLParam(r, "connectionID"))
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, r, http.StatusNotFound, "ai_connection_not_found", "AI service connection was not found")
		return
	}
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "ai_connection_unavailable", "Could not load AI service connection")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"connection": item})
}

func (s *Server) createAIConnection(w http.ResponseWriter, r *http.Request) {
	user, _ := s.userFromRequest(r)
	orgID, ok := s.personalAIConnectionWorkspace(w, r, user)
	if !ok {
		return
	}
	if !s.allowRate(s.authLimiter, "ai-connection-create:"+user.ID+":"+clientIP(r), 6, time.Minute) {
		writeError(w, r, http.StatusTooManyRequests, "rate_limited", "AI service connection attempts are too frequent")
		return
	}
	var req createAIConnectionRequest
	if err := decodeAIConnectionJSON(w, r, &req); err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid_json", "Invalid JSON body")
		return
	}
	if !req.ConfirmBillable {
		writeError(w, r, http.StatusUnprocessableEntity, "billable_validation_confirmation_required", "Confirm the provider may charge for minimal model validation")
		return
	}
	resolved, err := connections.ResolveProvider(connections.ResolveProviderInput{
		Code: req.Provider, Region: req.Region, WorkspaceID: req.WorkspaceID,
	})
	if err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid_provider_profile", err.Error())
		return
	}
	if strings.TrimSpace(req.APIKey) == "" || len(req.APIKey) > 8192 {
		writeError(w, r, http.StatusBadRequest, "credential_required", "Official developer API key is required")
		return
	}
	if len(req.Models) == 0 {
		if len(resolved.Manifest.RecommendedModels) == 0 {
			writeError(w, r, http.StatusBadRequest, "connection_models_missing", "Select at least one provider model")
			return
		}
		req.Models = append([]string(nil), resolved.Manifest.RecommendedModels[0])
	}
	models, err := normalizeConnectionModels(req.Models)
	if err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid_models", err.Error())
		return
	}
	if s.credentialKeys == nil {
		writeError(w, r, http.StatusServiceUnavailable, "credential_vault_unavailable", "Credential vault is unavailable")
		return
	}
	encrypted, err := s.credentialKeys.Encrypt(user.ID, resolved.Manifest.Code, req.APIKey)
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "credential_encrypt_failed", "Could not protect the API key")
		return
	}
	credential := store.AIConnectionSecret{
		Ciphertext: encrypted.Ciphertext, Nonce: encrypted.Nonce, Mask: encrypted.Mask,
		Fingerprint: encrypted.Fingerprint, EncryptionKeyID: encrypted.EncryptionKeyID,
		FingerprintKeyID: encrypted.FingerprintKeyID, Algorithm: encrypted.Algorithm,
	}
	duplicate, err := s.repo.AIConnectionCredentialExists(r.Context(), user.ID, orgID, resolved.Manifest.Code, resolved.Endpoint, credential, "")
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "ai_connection_duplicate_check_failed", "Could not check existing AI service connections")
		return
	}
	if duplicate {
		writeError(w, r, http.StatusConflict, "ai_connection_duplicate", "This official credential is already connected to the same provider endpoint")
		return
	}
	validation := s.validateOfficialCredentialSet(r.Context(), resolved, models, req.APIKey)
	item, err := s.repo.CreateAIConnection(r.Context(), store.AIConnectionCreateInput{
		OwnerUserID: user.ID, OrgID: orgID, Provider: resolved.Manifest.Code,
		ProductLine: resolved.Manifest.ProductLine, Region: resolved.Region,
		WorkspaceID: resolved.WorkspaceID, Protocol: resolved.Manifest.Protocol,
		AdapterType: resolved.Manifest.Type, Endpoint: resolved.Endpoint,
		ProviderConfig: resolved.ProviderConfig, DisplayName: cleanConnectionDisplayName(req.DisplayName, resolved.Manifest.Name),
		Models:     models,
		Credential: credential,
		Validation: validationStoreValue(validation),
	})
	if errors.Is(err, store.ErrAIConnectionLimit) {
		writeError(w, r, http.StatusConflict, "ai_connection_limit_reached", "This workspace has reached the limit of 32 AI service connections")
		return
	}
	if errors.Is(err, store.ErrAIConnectionDuplicate) {
		writeError(w, r, http.StatusConflict, "ai_connection_duplicate", "This official credential is already connected to the same provider endpoint")
		return
	}
	if err != nil {
		writeError(w, r, http.StatusBadRequest, "ai_connection_create_failed", "Could not save AI service connection")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"connection": item, "validation": validation})
}

func (s *Server) validateAIConnection(w http.ResponseWriter, r *http.Request) {
	user, _ := s.userFromRequest(r)
	orgID, ok := s.personalAIConnectionWorkspace(w, r, user)
	if !ok {
		return
	}
	if !s.allowRate(s.authLimiter, "ai-connection-validate:"+user.ID+":"+clientIP(r), 12, time.Minute) {
		writeError(w, r, http.StatusTooManyRequests, "rate_limited", "Connection validation attempts are too frequent")
		return
	}
	var req validateAIConnectionRequest
	if err := decodeAIConnectionJSON(w, r, &req); err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid_json", "Invalid JSON body")
		return
	}
	if !req.ConfirmBillable {
		writeError(w, r, http.StatusUnprocessableEntity, "billable_validation_confirmation_required", "Confirm the provider may charge for minimal model validation")
		return
	}
	connectionID := chi.URLParam(r, "connectionID")
	item, secret, resolved, apiKey, ok := s.loadAIConnectionCredential(w, r, user.ID, orgID, connectionID)
	if !ok {
		return
	}
	if len(item.Models) == 0 {
		writeError(w, r, http.StatusBadRequest, "connection_models_missing", "Connection has no configured models")
		return
	}
	validation := s.validateOfficialCredentialSet(r.Context(), resolved, connectionModelIDs(item.Models), apiKey)
	updated, err := s.repo.UpdateAIConnectionValidation(r.Context(), user.ID, orgID, connectionID, secret.Version, validationStoreValue(validation))
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, r, http.StatusConflict, "credential_changed", "The credential changed during validation; retry with the current connection")
		return
	}
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "ai_connection_validate_failed", "Could not update connection validation")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"connection": updated, "validation": validation})
}

func (s *Server) rotateAIConnectionCredential(w http.ResponseWriter, r *http.Request) {
	user, _ := s.userFromRequest(r)
	orgID, ok := s.personalAIConnectionWorkspace(w, r, user)
	if !ok {
		return
	}
	if !s.allowRate(s.authLimiter, "ai-connection-rotate:"+user.ID+":"+clientIP(r), 6, time.Minute) {
		writeError(w, r, http.StatusTooManyRequests, "rate_limited", "Credential rotation attempts are too frequent")
		return
	}
	var req rotateAIConnectionRequest
	if err := decodeAIConnectionJSON(w, r, &req); err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid_json", "Invalid JSON body")
		return
	}
	if !req.ConfirmBillable {
		writeError(w, r, http.StatusUnprocessableEntity, "billable_validation_confirmation_required", "Confirm the provider may charge for minimal model validation")
		return
	}
	if strings.TrimSpace(req.APIKey) == "" || len(req.APIKey) > 8192 {
		writeError(w, r, http.StatusBadRequest, "credential_required", "New official developer API key is required")
		return
	}
	connectionID := chi.URLParam(r, "connectionID")
	item, err := s.repo.AIConnectionForOwnerOrg(r.Context(), user.ID, orgID, connectionID)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, r, http.StatusNotFound, "ai_connection_not_found", "AI service connection was not found")
		return
	}
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "ai_connection_unavailable", "Could not load AI service connection")
		return
	}
	resolved, err := connections.ResolveProvider(connections.ResolveProviderInput{
		Code: item.Provider, Region: item.Region, WorkspaceID: item.WorkspaceID,
	})
	if err != nil || len(item.Models) == 0 {
		writeError(w, r, http.StatusBadRequest, "invalid_provider_profile", "Stored provider profile is invalid")
		return
	}
	if s.credentialKeys == nil {
		writeError(w, r, http.StatusServiceUnavailable, "credential_vault_unavailable", "Credential vault is unavailable")
		return
	}
	encrypted, err := s.credentialKeys.Encrypt(user.ID, item.Provider, req.APIKey)
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "credential_encrypt_failed", "Could not protect the API key")
		return
	}
	credential := store.AIConnectionSecret{
		Ciphertext: encrypted.Ciphertext, Nonce: encrypted.Nonce, Mask: encrypted.Mask,
		Fingerprint: encrypted.Fingerprint, EncryptionKeyID: encrypted.EncryptionKeyID,
		FingerprintKeyID: encrypted.FingerprintKeyID, Algorithm: encrypted.Algorithm,
	}
	duplicate, err := s.repo.AIConnectionCredentialExists(r.Context(), user.ID, orgID, item.Provider, item.Endpoint, credential, connectionID)
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "ai_connection_duplicate_check_failed", "Could not check existing AI service connections")
		return
	}
	if duplicate {
		writeError(w, r, http.StatusConflict, "ai_connection_duplicate", "This official credential is already connected to the same provider endpoint")
		return
	}
	validation := s.validateOfficialCredentialSet(r.Context(), resolved, connectionModelIDs(item.Models), req.APIKey)
	if !validation.OK {
		_ = s.repo.WriteAudit(r.Context(), store.AuditEvent{
			ActorType: "user", ActorID: user.ID, Action: "ai_connection.credential_rotation_rejected",
			ObjectType: "ai_connection", ObjectID: connectionID, Result: "failed",
			Metadata: map[string]any{
				"provider":                      item.Provider,
				"stage":                         validation.Stage,
				"error_code":                    validation.ErrorType,
				"billable_validation_confirmed": true,
			},
		})
		writeError(w, r, http.StatusUnprocessableEntity, "credential_validation_failed", validation.Message)
		return
	}
	updated, err := s.repo.RotateAIConnectionSecret(r.Context(), user.ID, orgID, connectionID, credential, validationStoreValue(validation))
	if errors.Is(err, store.ErrAIConnectionDuplicate) {
		writeError(w, r, http.StatusConflict, "ai_connection_duplicate", "This official credential is already connected to the same provider endpoint")
		return
	}
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "credential_rotate_failed", "Could not rotate connection credential")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"connection": updated, "validation": validation})
}

func (s *Server) deleteAIConnection(w http.ResponseWriter, r *http.Request) {
	user, _ := s.userFromRequest(r)
	orgID, ok := s.personalAIConnectionWorkspace(w, r, user)
	if !ok {
		return
	}
	err := s.repo.DeleteAIConnection(r.Context(), user.ID, orgID, chi.URLParam(r, "connectionID"))
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, r, http.StatusNotFound, "ai_connection_not_found", "AI service connection was not found")
		return
	}
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "ai_connection_delete_failed", "Could not delete AI service connection")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) quickCreateAIConnectionRelay(w http.ResponseWriter, r *http.Request) {
	user, _ := s.userFromRequest(r)
	orgID, ok := s.personalAIConnectionWorkspace(w, r, user)
	if !ok {
		return
	}
	if !s.allowRate(s.authLimiter, "ai-connection-relay:"+user.ID+":"+clientIP(r), 6, time.Minute) {
		writeError(w, r, http.StatusTooManyRequests, "rate_limited", "Personal relay creation attempts are too frequent")
		return
	}
	var req quickRelayRequest
	if err := decodeAIConnectionJSON(w, r, &req); err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid_json", "Invalid JSON body")
		return
	}
	if err := normalizeQuickRelayRequest(&req); err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid_relay_settings", err.Error())
		return
	}
	idempotencyKey := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if !idempotencyKeyPattern.MatchString(idempotencyKey) {
		writeError(w, r, http.StatusBadRequest, "idempotency_key_required", "A valid Idempotency-Key header is required")
		return
	}
	connectionID := chi.URLParam(r, "connectionID")
	item, err := s.repo.AIConnectionForOwnerOrg(r.Context(), user.ID, orgID, connectionID)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, r, http.StatusNotFound, "ai_connection_not_found", "AI service connection was not found")
		return
	}
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "ai_connection_unavailable", "Could not load AI service connection")
		return
	}
	if item.Status != "active" && item.Status != "attention" {
		writeError(w, r, http.StatusConflict, "ai_connection_not_active", "Validate the AI service connection before creating a relay")
		return
	}
	if len(req.ModelIDs) == 0 {
		for _, model := range item.Models {
			if model.Enabled && model.VerificationStatus == "verified" {
				req.ModelIDs = append(req.ModelIDs, model.ID)
			}
		}
	}
	req.ModelIDs = uniqueTrimmed(req.ModelIDs)
	if len(req.ModelIDs) == 0 || len(req.ModelIDs) > 16 {
		writeError(w, r, http.StatusBadRequest, "invalid_models", "Select between 1 and 16 verified models")
		return
	}
	for _, modelID := range req.ModelIDs {
		if len(modelID) > 128 || !strings.HasPrefix(modelID, "aicm_") {
			writeError(w, r, http.StatusBadRequest, "invalid_models", "One or more selected connection models are invalid")
			return
		}
	}
	plainKey, err := store.NewGatewayPlainKey()
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "gateway_key_create_failed", "Could not create gateway key")
		return
	}
	if s.credentialKeys == nil {
		writeError(w, r, http.StatusServiceUnavailable, "credential_vault_unavailable", "Credential vault is unavailable")
		return
	}
	reveal, err := s.credentialKeys.Encrypt(user.ID, "quick-relay", plainKey)
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "gateway_key_create_failed", "Could not protect the one-time gateway key")
		return
	}
	req.Name = cleanConnectionDisplayName(req.Name, item.DisplayName+" 个人中转")
	requestHash := quickRelayRequestHash(connectionID, req)
	result, err := s.repo.CreateQuickRelay(r.Context(), store.QuickRelayInput{
		OwnerUserID: user.ID, OrgID: orgID, ConnectionID: connectionID, ModelIDs: req.ModelIDs,
		Name: req.Name, Policy: req.Policy,
		QPSLimit: req.QPSLimit, QuotaMonth: req.QuotaMonth,
		BaseURL:        strings.TrimRight(s.cfg.PublicURL, "/") + "/gateway/v1",
		IdempotencyKey: idempotencyKey, RequestHash: requestHash, PlainKey: plainKey,
		Reveal: store.QuickRelayReveal{
			Ciphertext: reveal.Ciphertext, Nonce: reveal.Nonce, EncryptionKeyID: reveal.EncryptionKeyID,
			Fingerprint: reveal.Fingerprint, FingerprintKeyID: reveal.FingerprintKeyID, Mask: reveal.Mask,
		},
	})
	if errors.Is(err, store.ErrIdempotencyConflict) {
		writeError(w, r, http.StatusConflict, "idempotency_conflict", "This retry key was already used for a different relay request")
		return
	}
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, r, http.StatusConflict, "ai_connection_not_active", "Validate the AI service connection and selected models before creating a relay")
		return
	}
	if err != nil {
		writeError(w, r, http.StatusBadRequest, "quick_relay_create_failed", "Could not create the personal relay from this connection")
		return
	}
	if result.Replay {
		plainKey, err = s.credentialKeys.Decrypt(user.ID, "quick-relay", secretcrypto.CredentialEnvelope{
			Ciphertext: result.Reveal.Ciphertext, Nonce: result.Reveal.Nonce,
			EncryptionKeyID: result.Reveal.EncryptionKeyID, Fingerprint: result.Reveal.Fingerprint,
			FingerprintKeyID: result.Reveal.FingerprintKeyID, Mask: result.Reveal.Mask,
			Algorithm: "aes-256-gcm",
		})
		if err != nil {
			writeError(w, r, http.StatusInternalServerError, "gateway_key_reveal_failed", "Could not reveal the idempotent gateway key")
			return
		}
		result.Key.PlainKey = plainKey
	}
	status := http.StatusCreated
	if result.Replay {
		status = http.StatusOK
	}
	writeJSON(w, status, map[string]any{"gateway": result.Gateway, "key": result.Key, "replay": result.Replay})
}

func (s *Server) loadAIConnectionCredential(w http.ResponseWriter, r *http.Request, ownerUserID string, orgID string, connectionID string) (store.AIConnection, store.AIConnectionSecret, connections.ResolvedProvider, string, bool) {
	item, err := s.repo.AIConnectionForOwnerOrg(r.Context(), ownerUserID, orgID, connectionID)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, r, http.StatusNotFound, "ai_connection_not_found", "AI service connection was not found")
		return store.AIConnection{}, store.AIConnectionSecret{}, connections.ResolvedProvider{}, "", false
	}
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "ai_connection_unavailable", "Could not load AI service connection")
		return store.AIConnection{}, store.AIConnectionSecret{}, connections.ResolvedProvider{}, "", false
	}
	secret, err := s.repo.AIConnectionSecretForOwnerOrg(r.Context(), ownerUserID, orgID, connectionID)
	if err != nil || s.credentialKeys == nil {
		writeError(w, r, http.StatusInternalServerError, "credential_unavailable", "Could not load connection credential")
		return store.AIConnection{}, store.AIConnectionSecret{}, connections.ResolvedProvider{}, "", false
	}
	resolved, err := connections.ResolveProvider(connections.ResolveProviderInput{
		Code: item.Provider, Region: item.Region, WorkspaceID: item.WorkspaceID,
	})
	if err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid_provider_profile", "Stored provider profile is invalid")
		return store.AIConnection{}, store.AIConnectionSecret{}, connections.ResolvedProvider{}, "", false
	}
	apiKey, err := s.credentialKeys.Decrypt(ownerUserID, item.Provider, secretcrypto.CredentialEnvelope{
		Ciphertext: secret.Ciphertext, Nonce: secret.Nonce, EncryptionKeyID: secret.EncryptionKeyID,
		Fingerprint: secret.Fingerprint, FingerprintKeyID: secret.FingerprintKeyID,
		Mask: secret.Mask, Algorithm: secret.Algorithm,
	})
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "credential_unavailable", "Could not decrypt connection credential")
		return store.AIConnection{}, store.AIConnectionSecret{}, connections.ResolvedProvider{}, "", false
	}
	return item, secret, resolved, apiKey, true
}

func (s *Server) validateOfficialCredential(ctx context.Context, resolved connections.ResolvedProvider, model string, apiKey string) connectionValidationResult {
	return s.validateOfficialCredentialSet(ctx, resolved, []string{model}, apiKey)
}

func (s *Server) validateOfficialCredentialSet(ctx context.Context, resolved connections.ResolvedProvider, models []string, apiKey string) connectionValidationResult {
	if len(models) == 0 {
		return connectionValidationResult{
			Provider: resolved.Manifest.Name, Type: resolved.Manifest.Type, Endpoint: resolved.Endpoint,
			Stage: "models", ErrorType: "models_missing", Message: "连接没有可验证的模型",
		}
	}
	if s.upstreamClient == nil {
		result := connectionValidationResult{
			Provider: resolved.Manifest.Name, Type: resolved.Manifest.Type, Endpoint: resolved.Endpoint,
			Model: models[0], Stage: "client", ErrorType: "validation_client_unavailable",
			Message: "连接验证服务暂时不可用，请稍后重试",
		}
		result.Models = failedModelValidationResults(models, result)
		return result
	}
	ctx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	upstream := gatewaycache.Upstream{
		Provider: resolved.Manifest.Name, Type: resolved.Manifest.Type, Endpoint: resolved.Endpoint,
		Model: models[0], ProviderConfig: resolved.ProviderConfig,
	}
	started := time.Now()
	modelCount := 0
	if resolved.Manifest.ValidationMode == "models_then_generation" {
		discovery, err := s.upstreamClient.ModelsStrict(ctx, upstream, strings.TrimSpace(apiKey))
		modelCount = countModelList(discovery.Body)
		if err != nil {
			result := connectionValidationResult{
				OK: false, Provider: resolved.Manifest.Name, Type: resolved.Manifest.Type,
				Endpoint: resolved.Endpoint, Model: models[0], Stage: "models", StatusCode: discovery.StatusCode,
				LatencyMs: int(time.Since(started).Milliseconds()), ModelCount: modelCount,
				ErrorType: nonEmpty(discovery.ErrorType, "models_unavailable"), Message: "模型列表验证失败，请检查 API Key、地域和产品线",
			}
			result.Models = failedModelValidationResults(models, result)
			return result
		}
	}

	results := make([]connectionValidationResult, len(models))
	limit := make(chan struct{}, 4)
	var group sync.WaitGroup
	for idx, model := range models {
		group.Add(1)
		go func(index int, providerModel string) {
			defer group.Done()
			limit <- struct{}{}
			defer func() { <-limit }()
			results[index] = s.validateOfficialGeneration(ctx, resolved, providerModel, apiKey)
		}(idx, model)
	}
	group.Wait()

	totalTokens := 0
	estimated := false
	failed := 0
	firstFailure := connectionValidationResult{}
	modelResults := make([]connectionModelValidationResult, 0, len(results))
	for _, result := range results {
		totalTokens += result.Tokens
		estimated = estimated || result.UsageEstimated
		if !result.OK {
			failed++
			if firstFailure.ErrorType == "" {
				firstFailure = result
			}
		}
		modelResults = append(modelResults, connectionModelValidationResult{
			OK: result.OK, Model: result.Model, StatusCode: result.StatusCode,
			LatencyMs: result.LatencyMs, Tokens: result.Tokens, UsageEstimated: result.UsageEstimated,
			ErrorType: result.ErrorType, Message: result.Message,
		})
	}
	if failed > 0 {
		firstFailure.LatencyMs = int(time.Since(started).Milliseconds())
		firstFailure.ModelCount = max(modelCount, len(models))
		firstFailure.Tokens = totalTokens
		firstFailure.UsageEstimated = estimated
		firstFailure.Models = modelResults
		firstFailure.Message = fmt.Sprintf("%d/%d 个模型验证通过；请处理失败模型后重新验证", len(models)-failed, len(models))
		return firstFailure
	}
	return connectionValidationResult{
		OK: true, Provider: resolved.Manifest.Name, Type: resolved.Manifest.Type,
		Endpoint: resolved.Endpoint, Model: models[0], Stage: "generation",
		LatencyMs: int(time.Since(started).Milliseconds()), ModelCount: max(modelCount, len(models)),
		Tokens: totalTokens, UsageEstimated: estimated,
		Message: "官方开发者凭证和全部已配置模型均通过最小生成验证",
		Models:  modelResults,
	}
}

func (s *Server) validateOfficialGeneration(ctx context.Context, resolved connections.ResolvedProvider, model string, apiKey string) connectionValidationResult {
	upstream := gatewaycache.Upstream{
		Provider: resolved.Manifest.Name, Type: resolved.Manifest.Type, Endpoint: resolved.Endpoint,
		Model: model, ProviderConfig: resolved.ProviderConfig,
	}
	started := time.Now()
	raw := officialValidationPayload(resolved.Manifest.GenerationKind, model)
	result, err := s.upstreamClient.JSON(ctx, upstream, strings.TrimSpace(apiKey), resolved.Manifest.GenerationKind, raw, gatewaycache.UpstreamUsage{PromptTokens: 4, CompletionTokens: 2, TotalTokens: 6, Estimated: true})
	validation := connectionValidationResult{
		Provider: resolved.Manifest.Name, Type: resolved.Manifest.Type, Endpoint: resolved.Endpoint,
		Model: model, Stage: "generation", StatusCode: result.StatusCode,
		LatencyMs: int(time.Since(started).Milliseconds()), ModelCount: 1,
		Tokens: result.Usage.TotalTokens, UsageEstimated: result.Usage.Estimated,
	}
	if err != nil {
		validation.ErrorType = nonEmpty(result.ErrorType, "generation_failed")
		validation.Message = "模型 " + model + " 的最小生成验证失败，请检查模型权限、余额和地域"
		return validation
	}
	validation.OK = true
	return validation
}

func officialValidationPayload(kind string, model string) []byte {
	var payload map[string]any
	if kind == "responses" {
		payload = map[string]any{
			"model": model, "input": "Reply exactly: OK", "max_output_tokens": 8,
		}
	} else {
		payload = map[string]any{
			"model":      model,
			"messages":   []map[string]any{{"role": "user", "content": "Reply exactly: OK"}},
			"max_tokens": 8,
		}
	}
	raw, _ := json.Marshal(payload)
	return raw
}

func normalizeConnectionModels(values []string) ([]string, error) {
	values = uniqueTrimmed(values)
	if len(values) == 0 || len(values) > 16 {
		return nil, fmt.Errorf("select between 1 and 16 models")
	}
	for _, value := range values {
		if strings.Contains(value, "://") || strings.HasPrefix(value, "/") || !connectionModelPattern.MatchString(value) {
			return nil, fmt.Errorf("model id %q has an invalid format", value)
		}
	}
	return values, nil
}

func uniqueTrimmed(values []string) []string {
	seen := map[string]bool{}
	items := []string{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		items = append(items, value)
	}
	return items
}

func cleanConnectionDisplayName(value string, fallback string) string {
	value = strings.Map(func(r rune) rune {
		if r < 32 || r == 127 {
			return -1
		}
		return r
	}, strings.TrimSpace(value))
	if value == "" {
		value = strings.TrimSpace(fallback)
	}
	runes := []rune(value)
	if len(runes) > 80 {
		value = string(runes[:80])
	}
	return value
}

func validationStoreValue(value connectionValidationResult) store.AIConnectionValidation {
	stored := store.AIConnectionValidation{
		OK: value.OK, Stage: value.Stage, LatencyMs: value.LatencyMs, ModelCount: value.ModelCount,
		ErrorCode: value.ErrorType, ErrorMessage: value.Message, BillableConfirmed: true,
	}
	for _, model := range value.Models {
		stored.Models = append(stored.Models, store.AIConnectionModelValidation{
			ProviderModelID: model.Model, OK: model.OK, LatencyMs: model.LatencyMs,
			ErrorCode: model.ErrorType, ErrorMessage: model.Message,
		})
	}
	return stored
}

func failedModelValidationResults(models []string, failure connectionValidationResult) []connectionModelValidationResult {
	results := make([]connectionModelValidationResult, 0, len(models))
	for _, model := range models {
		results = append(results, connectionModelValidationResult{
			Model: model, StatusCode: failure.StatusCode, LatencyMs: failure.LatencyMs,
			ErrorType: failure.ErrorType, Message: failure.Message,
		})
	}
	return results
}

func decodeAIConnectionJSON(w http.ResponseWriter, r *http.Request, target any) error {
	r.Body = http.MaxBytesReader(w, r.Body, aiConnectionRequestBodyLimit)
	return decodeJSON(r, target)
}

func (s *Server) personalAIConnectionWorkspace(w http.ResponseWriter, r *http.Request, user store.PublicUser) (string, bool) {
	orgID, err := s.repo.EnsurePersonalWorkspace(r.Context(), user)
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "workspace_unavailable", "Could not prepare personal AI service connections")
		return "", false
	}
	return orgID, true
}

func connectionModelIDs(models []store.AIConnectionModel) []string {
	items := make([]string, 0, len(models))
	for _, model := range models {
		if model.Enabled {
			items = append(items, model.ProviderModelID)
		}
	}
	return items
}

func quickRelayRequestHash(connectionID string, req quickRelayRequest) string {
	modelIDs := append([]string(nil), req.ModelIDs...)
	sort.Strings(modelIDs)
	payload, _ := json.Marshal(map[string]any{
		"connectionId": connectionID, "modelIds": modelIDs, "name": strings.TrimSpace(req.Name),
		"policy": strings.TrimSpace(req.Policy), "qpsLimit": req.QPSLimit, "quotaMonth": req.QuotaMonth,
	})
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}

func normalizeQuickRelayRequest(req *quickRelayRequest) error {
	req.Policy = strings.ToLower(strings.TrimSpace(req.Policy))
	if req.Policy == "" {
		req.Policy = "latency"
	}
	switch req.Policy {
	case "latency", "success", "cost":
	default:
		return fmt.Errorf("relay policy must be latency, success, or cost")
	}
	if req.QPSLimit == 0 {
		req.QPSLimit = 20
	}
	if req.QPSLimit < 1 || req.QPSLimit > 1000 {
		return fmt.Errorf("qpsLimit must be between 1 and 1000")
	}
	if req.QuotaMonth == 0 {
		req.QuotaMonth = 100000
	}
	if req.QuotaMonth < 1 || req.QuotaMonth > 1_000_000_000 {
		return fmt.Errorf("quotaMonth must be between 1 and 1000000000")
	}
	return nil
}
