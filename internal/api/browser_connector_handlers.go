package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"tokhub/internal/browserconnector"
	"tokhub/internal/connections"
	"tokhub/internal/store"
)

const openCLIBrowserTermsVersion = "opencli-personal-browser-experimental-v1"
const openCLIBrowserIdentityBindingVersion = "opencli-account-fingerprint-v1"
const browserConnectorRequestBodyLimit = 128 << 10

type createBrowserConnectorRequest struct {
	DisplayName string `json:"displayName"`
}

type pairBrowserConnectorRequest struct {
	PairingCode string `json:"pairingCode"`
}

type heartbeatBrowserConnectorRequest struct {
	OpenCLIVersion   string   `json:"opencliVersion"`
	ExtensionVersion string   `json:"extensionVersion"`
	Capabilities     []string `json:"capabilities"`
}

type completeBrowserTaskRequest struct {
	LeaseToken string                  `json:"leaseToken"`
	Result     browserconnector.Result `json:"result"`
}

type createBrowserAIConnectionRequest struct {
	ConnectorID     string   `json:"connectorId"`
	Provider        string   `json:"provider"`
	DisplayName     string   `json:"displayName"`
	Models          []string `json:"models"`
	TermsAckVersion string   `json:"termsAckVersion"`
}

func (s *Server) meAIBrowserConnectors(w http.ResponseWriter, r *http.Request) {
	if !s.requireOpenCLIBrowserFeature(w, r) {
		return
	}
	user, _ := s.userFromRequest(r)
	orgID, ok := s.personalAIConnectionWorkspace(w, r, user)
	if !ok {
		return
	}
	items, err := s.repo.AIBrowserConnectorsForOwner(r.Context(), user.ID, orgID)
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "browser_connectors_unavailable", "Could not load local browser connectors")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) createAIBrowserConnector(w http.ResponseWriter, r *http.Request) {
	if !s.requireOpenCLIBrowserFeature(w, r) {
		return
	}
	user, _ := s.userFromRequest(r)
	orgID, ok := s.personalAIConnectionWorkspace(w, r, user)
	if !ok {
		return
	}
	if !s.allowRate(s.authLimiter, "browser-connector-create:"+user.ID+":"+clientIP(r), 6, time.Hour) {
		writeError(w, r, http.StatusTooManyRequests, "rate_limited", "Local browser connector creation is temporarily limited")
		return
	}
	var req createBrowserConnectorRequest
	if !decodeBrowserConnectorJSON(w, r, &req) {
		return
	}
	result, err := s.repo.CreateAIBrowserConnector(r.Context(), user.ID, orgID, req.DisplayName)
	if err != nil {
		writeError(w, r, http.StatusBadRequest, "browser_connector_create_failed", "Could not create local browser connector")
		return
	}
	_ = s.repo.WriteAudit(r.Context(), store.AuditEvent{
		ActorType: "user", ActorID: user.ID, Action: "ai_browser_connector.created",
		ObjectType: "ai_browser_connector", ObjectID: result.Connector.ID,
		IP: clientIP(r), Result: "success",
		Metadata: map[string]any{"org_id": orgID},
	})
	writeJSON(w, http.StatusCreated, map[string]any{
		"connector":   result.Connector,
		"pairingCode": result.PairingCode,
		"pairCommand": fmt.Sprintf(
			"tokhub-opencli-connector pair --server %s --code %s",
			shellQuoteBrowserConnectorArgument(strings.TrimRight(s.cfg.PublicURL, "/")),
			shellQuoteBrowserConnectorArgument(result.PairingCode),
		),
	})
}

func (s *Server) revokeAIBrowserConnector(w http.ResponseWriter, r *http.Request) {
	if !s.requireOpenCLIBrowserFeature(w, r) {
		return
	}
	user, _ := s.userFromRequest(r)
	orgID, ok := s.personalAIConnectionWorkspace(w, r, user)
	if !ok {
		return
	}
	err := s.repo.RevokeAIBrowserConnector(r.Context(), user.ID, orgID, chi.URLParam(r, "connectorID"))
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, r, http.StatusNotFound, "browser_connector_not_found", "Local browser connector was not found")
		return
	}
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "browser_connector_revoke_failed", "Could not revoke local browser connector")
		return
	}
	connectorID := chi.URLParam(r, "connectorID")
	_ = s.repo.WriteAudit(r.Context(), store.AuditEvent{
		ActorType: "user", ActorID: user.ID, Action: "ai_browser_connector.revoked",
		ObjectType: "ai_browser_connector", ObjectID: connectorID,
		IP: clientIP(r), Result: "success",
		Metadata: map[string]any{"org_id": orgID},
	})
	writeJSON(w, http.StatusOK, map[string]string{"status": "revoked"})
}

func (s *Server) pairAIBrowserConnector(w http.ResponseWriter, r *http.Request) {
	if !s.requireOpenCLIBrowserFeature(w, r) {
		return
	}
	if !s.allowRate(s.authLimiter, "browser-connector-pair:"+clientIP(r), 20, time.Minute) {
		writeError(w, r, http.StatusTooManyRequests, "rate_limited", "Local browser connector pairing is temporarily limited")
		return
	}
	var req pairBrowserConnectorRequest
	if !decodeBrowserConnectorJSON(w, r, &req) {
		return
	}
	result, err := s.repo.PairAIBrowserConnector(r.Context(), req.PairingCode)
	if errors.Is(err, store.ErrAIBrowserConnectorPairingInvalid) {
		writeError(w, r, http.StatusUnauthorized, "pairing_code_invalid", "Pairing code is invalid or expired")
		return
	}
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "browser_connector_pair_failed", "Could not pair local browser connector")
		return
	}
	_ = s.repo.WriteAudit(r.Context(), store.AuditEvent{
		ActorType: "connector", ActorID: result.Connector.ID, Action: "ai_browser_connector.paired",
		ObjectType: "ai_browser_connector", ObjectID: result.Connector.ID,
		IP: clientIP(r), Result: "success",
		Metadata: map[string]any{"org_id": result.Connector.OrgID},
	})
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) heartbeatAIBrowserConnector(w http.ResponseWriter, r *http.Request) {
	connector, ok := s.deviceAIBrowserConnector(w, r)
	if !ok {
		return
	}
	var req heartbeatBrowserConnectorRequest
	if !decodeBrowserConnectorJSON(w, r, &req) {
		return
	}
	item, err := s.repo.HeartbeatAIBrowserConnector(
		r.Context(), connector.ID, req.OpenCLIVersion, req.ExtensionVersion, req.Capabilities,
	)
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "browser_connector_heartbeat_failed", "Could not update local browser connector")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"connector": item})
}

func (s *Server) claimAIBrowserConnectorTask(w http.ResponseWriter, r *http.Request) {
	connector, ok := s.deviceAIBrowserConnector(w, r)
	if !ok {
		return
	}
	task, err := s.repo.ClaimAIBrowserTask(r.Context(), connector.ID, 115*time.Second)
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "browser_task_claim_failed", "Could not claim local browser task")
		return
	}
	if task == nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"task": task})
}

func (s *Server) completeAIBrowserConnectorTask(w http.ResponseWriter, r *http.Request) {
	connector, ok := s.deviceAIBrowserConnector(w, r)
	if !ok {
		return
	}
	var req completeBrowserTaskRequest
	if !decodeBrowserConnectorJSON(w, r, &req) {
		return
	}
	result, err := browserconnector.SanitizeResult(req.Result)
	if err != nil {
		writeError(w, r, http.StatusBadRequest, "browser_task_result_invalid", "Browser task result is invalid or too large")
		return
	}
	response := map[string]any{
		"content":            strings.TrimSpace(result.Content),
		"accountMask":        strings.TrimSpace(result.AccountMask),
		"accountFingerprint": strings.TrimSpace(result.AccountFingerprint),
	}
	err = s.repo.CompleteAIBrowserTask(
		r.Context(), connector.ID, chi.URLParam(r, "taskID"), req.LeaseToken,
		result.OK, response, result.ErrorCode, result.ErrorMessage,
	)
	if errors.Is(err, store.ErrAIBrowserTaskLeaseInvalid) {
		writeError(w, r, http.StatusConflict, "browser_task_lease_invalid", "Browser task lease is invalid or expired")
		return
	}
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "browser_task_complete_failed", "Could not complete local browser task")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "accepted"})
}

func (s *Server) createAIBrowserConnection(w http.ResponseWriter, r *http.Request) {
	if !s.requireOpenCLIBrowserFeature(w, r) {
		return
	}
	user, _ := s.userFromRequest(r)
	orgID, ok := s.personalAIConnectionWorkspace(w, r, user)
	if !ok {
		return
	}
	if !s.allowRate(s.authLimiter, "browser-connection-create:"+user.ID+":"+clientIP(r), 6, time.Minute) {
		writeError(w, r, http.StatusTooManyRequests, "rate_limited", "Local browser connection attempts are too frequent")
		return
	}
	var req createBrowserAIConnectionRequest
	if !decodeBrowserConnectorJSON(w, r, &req) {
		return
	}
	if strings.TrimSpace(req.TermsAckVersion) != openCLIBrowserTermsVersion {
		writeError(w, r, http.StatusUnprocessableEntity, "browser_terms_confirmation_required", "Confirm the personal browser connection risk notice")
		return
	}
	provider := strings.ToLower(strings.TrimSpace(req.Provider))
	if provider != "openai" && provider != "gemini" && provider != "deepseek" {
		writeError(w, r, http.StatusBadRequest, "browser_provider_unsupported", "This provider does not support local browser connection")
		return
	}
	if !s.openCLIBrowserProviderEnabled(provider) {
		writeError(w, r, http.StatusServiceUnavailable, "browser_provider_disabled", "This local browser provider is currently disabled")
		return
	}
	connector, err := s.repo.AIBrowserConnectorForOwner(r.Context(), user.ID, orgID, req.ConnectorID)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, r, http.StatusNotFound, "browser_connector_not_found", "Local browser connector was not found")
		return
	}
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "browser_connector_unavailable", "Could not load local browser connector")
		return
	}
	if !connector.Online || !containsBrowserCapability(connector.Capabilities, provider) {
		writeError(w, r, http.StatusConflict, "browser_connector_offline", "Start the local connector and confirm the provider tab is available")
		return
	}
	resolved, err := connections.ResolveProvider(connections.ResolveProviderInput{Code: provider})
	if err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid_provider_profile", err.Error())
		return
	}
	models := req.Models
	if len(models) == 0 {
		models = []string{map[string]string{
			"openai": "chatgpt-web", "gemini": "gemini-web", "deepseek": "deepseek-web",
		}[provider]}
	}
	models, err = normalizeConnectionModels(models)
	if err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid_models", err.Error())
		return
	}
	task, err := s.repo.CreateAIBrowserTask(r.Context(), store.AIBrowserTaskInput{
		ConnectorID: connector.ID, OwnerUserID: user.ID, OrgID: orgID,
		Provider: provider, Action: browserconnector.ActionStatus,
		Request: map[string]any{}, ExpiresAt: time.Now().Add(45 * time.Second),
	})
	if errors.Is(err, store.ErrAIBrowserConnectorBusy) {
		writeError(w, r, http.StatusConflict, "browser_connector_busy", "Local browser connector is handling another request")
		return
	}
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "browser_login_check_failed", "Could not start browser login check")
		return
	}
	checked, err := s.waitForAIBrowserTask(r.Context(), user.ID, orgID, task.ID, 45*time.Second)
	if err != nil {
		writeError(w, r, http.StatusGatewayTimeout, "browser_login_check_timeout", "Local connector did not answer; keep it running and try again")
		return
	}
	if checked.Status != "completed" {
		message := checked.ErrorMessage
		if message == "" {
			message = "请先在 Chrome 中登录对应账号，再重新识别"
		}
		writeError(w, r, http.StatusConflict, "browser_login_required", message)
		return
	}
	accountMask := strings.TrimSpace(stringFromAny(checked.Response["accountMask"]))
	if accountMask == "" {
		accountMask = "已识别浏览器账号"
	}
	accountFingerprint := strings.TrimSpace(stringFromAny(checked.Response["accountFingerprint"]))
	if !browserconnector.IsValidAccountFingerprint(accountFingerprint) {
		writeError(w, r, http.StatusBadGateway, "browser_identity_unavailable", "Local connector did not return a verifiable account identity")
		return
	}
	if s.credentialKeys == nil {
		writeError(w, r, http.StatusServiceUnavailable, "credential_vault_unavailable", "Credential vault is unavailable")
		return
	}
	referenceRaw, _ := json.Marshal(map[string]string{"connectorId": connector.ID, "provider": provider})
	encrypted, err := s.credentialKeys.EncryptWithFingerprint(
		user.ID, provider, string(referenceRaw), "opencli-browser\x00"+connector.ID+"\x00"+provider,
	)
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "browser_reference_encrypt_failed", "Could not protect local connector reference")
		return
	}
	resolved.Manifest.ProductLine = map[string]string{
		"openai": "ChatGPT Web", "gemini": "Gemini Web", "deepseek": "DeepSeek Web",
	}[provider]
	resolved.ProviderConfig["authMethod"] = "opencli_browser"
	resolved.ProviderConfig["connectorId"] = connector.ID
	resolved.ProviderConfig["browserTask"] = true
	resolved.ProviderConfig["experimental"] = true
	resolved.ProviderConfig["sharingScope"] = "personal"
	resolved.ProviderConfig["identityBindingVersion"] = openCLIBrowserIdentityBindingVersion
	resolved.Endpoint = "browser+opencli://" + connector.ID + "/" + provider
	validation := store.AIConnectionValidation{
		OK: true, Stage: "browser_login", ModelCount: len(models),
		Models: make([]store.AIConnectionModelValidation, 0, len(models)),
	}
	for _, model := range models {
		validation.Models = append(validation.Models, store.AIConnectionModelValidation{ProviderModelID: model, OK: true})
	}
	item, err := s.repo.CreateAIConnection(r.Context(), store.AIConnectionCreateInput{
		OwnerUserID: user.ID, OrgID: orgID, Provider: provider,
		ProductLine: resolved.Manifest.ProductLine, Region: resolved.Region,
		Protocol: resolved.Manifest.Protocol, AdapterType: resolved.Manifest.Type,
		Endpoint: resolved.Endpoint, ProviderConfig: resolved.ProviderConfig,
		DisplayName: cleanConnectionDisplayName(req.DisplayName, resolved.Manifest.ProductLine),
		Models:      models,
		Credential: store.AIConnectionSecret{
			Ciphertext: encrypted.Ciphertext, Nonce: encrypted.Nonce, Mask: "Local Browser · " + accountMask,
			Fingerprint: encrypted.Fingerprint, EncryptionKeyID: encrypted.EncryptionKeyID,
			FingerprintKeyID: encrypted.FingerprintKeyID, Algorithm: encrypted.Algorithm,
			SecretType: "browser_connector", PayloadFormat: "browser_connector_v1",
			SubjectFingerprint: accountFingerprint,
		},
		Validation: validation, AuthMethod: "opencli_browser", AuthStatus: "active",
		SharingScope: "personal", RiskLevel: "experimental",
		ProviderAdapterVersion: "opencli-browser-v1", TermsAckVersion: openCLIBrowserTermsVersion,
		AccountMask: accountMask,
	})
	if errors.Is(err, store.ErrAIConnectionDuplicate) {
		writeError(w, r, http.StatusConflict, "ai_connection_duplicate", "This browser account connection already exists")
		return
	}
	if err != nil {
		writeError(w, r, http.StatusBadRequest, "browser_connection_create_failed", "Could not save browser account connection")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"connection": item})
}

func (s *Server) validateAIBrowserConnection(w http.ResponseWriter, r *http.Request, ownerUserID string, orgID string, item store.AIConnection) {
	if !s.openCLIBrowserProviderEnabled(item.Provider) {
		writeError(w, r, http.StatusServiceUnavailable, "browser_provider_disabled", "This local browser provider is currently disabled")
		return
	}
	connectorID := strings.TrimSpace(stringFromAny(item.ProviderConfig["connectorId"]))
	connector, err := s.repo.AIBrowserConnectorForOwner(r.Context(), ownerUserID, orgID, connectorID)
	if err != nil || !connector.Online || !containsBrowserCapability(connector.Capabilities, item.Provider) {
		writeError(w, r, http.StatusConflict, "browser_connector_offline", "Start the local connector and confirm the provider tab is available")
		return
	}
	secret, err := s.repo.AIConnectionSecretForOwnerOrg(r.Context(), ownerUserID, orgID, item.ID)
	if err != nil || secret.SecretType != "browser_connector" {
		writeError(w, r, http.StatusConflict, "browser_connection_unavailable", "Local browser connection reference is unavailable")
		return
	}
	started := time.Now()
	task, err := s.repo.CreateAIBrowserTask(r.Context(), store.AIBrowserTaskInput{
		ConnectorID: connector.ID, OwnerUserID: ownerUserID, OrgID: orgID,
		ConnectionID: item.ID, Provider: item.Provider, Action: browserconnector.ActionStatus,
		Request: map[string]any{}, ExpiresAt: time.Now().Add(45 * time.Second),
	})
	if errors.Is(err, store.ErrAIBrowserConnectorBusy) {
		writeError(w, r, http.StatusConflict, "browser_connector_busy", "Local browser connector is handling another request")
		return
	}
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "browser_login_check_failed", "Could not start browser login check")
		return
	}
	completed, waitErr := s.waitForAIBrowserTask(r.Context(), ownerUserID, orgID, task.ID, 45*time.Second)
	validation := connectionValidationResult{
		Provider: item.ProductLine, Type: item.AdapterType, Endpoint: "本机浏览器",
		ModelCount: len(item.Models), Stage: "browser_login",
		LatencyMs: int(time.Since(started).Milliseconds()), UsageEstimated: true,
	}
	if waitErr != nil {
		validation.ErrorType = "browser_login_check_timeout"
		validation.Message = "本地连接器未及时响应，请保持连接器运行后重试"
	} else if completed.Status != "completed" {
		validation.ErrorType = nonEmpty(completed.ErrorCode, "browser_login_required")
		validation.Message = nonEmpty(completed.ErrorMessage, "请先在 Chrome 中登录对应账号")
	} else if !browserconnector.AccountFingerprintMatches(
		secret.SubjectFingerprint,
		strings.TrimSpace(stringFromAny(completed.Response["accountFingerprint"])),
	) {
		validation.ErrorType = "identity_mismatch"
		validation.Message = "当前网页登录账号与连接创建时不一致，请切换回原账号或重新建立连接"
	} else {
		validation.OK = true
		validation.Message = "已识别本机浏览器登录账号"
	}
	for _, model := range item.Models {
		validation.Models = append(validation.Models, connectionModelValidationResult{
			OK: validation.OK, Model: model.ProviderModelID, LatencyMs: validation.LatencyMs,
			UsageEstimated: true, ErrorType: validation.ErrorType, Message: validation.Message,
		})
	}
	updated, err := s.repo.UpdateAIConnectionValidation(
		r.Context(), ownerUserID, orgID, item.ID, secret.Version, validationStoreValue(validation),
	)
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "ai_connection_validate_failed", "Could not update connection validation")
		return
	}
	if err := s.repo.SetAIBrowserConnectionAuthStatus(
		r.Context(), ownerUserID, orgID, item.ID, validation.OK, validation.ErrorType, validation.Message,
	); err == nil {
		if refreshed, readErr := s.repo.AIConnectionForOwnerOrg(r.Context(), ownerUserID, orgID, item.ID); readErr == nil {
			updated = refreshed
		}
	}
	if validation.OK {
		if _, riskErr := s.repo.RecordAIBrowserConnectionResult(
			r.Context(), ownerUserID, orgID, item.ID, true, "", time.Now(),
		); riskErr != nil {
			s.logger.Warn("reset local browser risk after validation failed", "connection_id", item.ID, "error", riskErr)
		}
	} else if validation.ErrorType != "" {
		if _, riskErr := s.repo.RecordAIBrowserConnectionResult(
			r.Context(), ownerUserID, orgID, item.ID, false, validation.ErrorType, time.Now(),
		); riskErr != nil {
			s.logger.Warn("record local browser validation risk failed", "connection_id", item.ID, "error", riskErr)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"connection": updated, "validation": validation})
}

func (s *Server) meAIBrowserConnectionRisk(w http.ResponseWriter, r *http.Request) {
	if !s.requireOpenCLIBrowserFeature(w, r) {
		return
	}
	user, _ := s.userFromRequest(r)
	orgID, ok := s.personalAIConnectionWorkspace(w, r, user)
	if !ok {
		return
	}
	connectionID := chi.URLParam(r, "connectionID")
	item, err := s.repo.AIConnectionForOwnerOrg(r.Context(), user.ID, orgID, connectionID)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, r, http.StatusNotFound, "browser_connection_not_found", "Local browser connection was not found")
		return
	}
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "browser_risk_unavailable", "Could not load local browser risk state")
		return
	}
	if item.AuthMethod != "opencli_browser" {
		writeError(w, r, http.StatusNotFound, "browser_connection_not_found", "Local browser connection was not found")
		return
	}
	risk, err := s.repo.AIBrowserRiskForConnection(
		r.Context(), user.ID, orgID, connectionID, s.openCLIBrowserRiskPolicy(item.Provider),
	)
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "browser_risk_unavailable", "Could not load local browser risk state")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"risk": risk})
}

func (s *Server) pauseAIBrowserConnection(w http.ResponseWriter, r *http.Request) {
	s.setAIBrowserConnectionPaused(w, r, true)
}

func (s *Server) resumeAIBrowserConnection(w http.ResponseWriter, r *http.Request) {
	s.setAIBrowserConnectionPaused(w, r, false)
}

func (s *Server) setAIBrowserConnectionPaused(w http.ResponseWriter, r *http.Request, paused bool) {
	if !s.requireOpenCLIBrowserFeature(w, r) {
		return
	}
	user, _ := s.userFromRequest(r)
	orgID, ok := s.personalAIConnectionWorkspace(w, r, user)
	if !ok {
		return
	}
	connectionID := chi.URLParam(r, "connectionID")
	risk, err := s.repo.SetAIBrowserConnectionPaused(r.Context(), user.ID, orgID, connectionID, paused)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, r, http.StatusNotFound, "browser_connection_not_found", "Local browser connection was not found")
		return
	}
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "browser_risk_update_failed", "Could not update local browser connection")
		return
	}
	if refreshed, riskErr := s.repo.AIBrowserRiskForConnection(
		r.Context(), user.ID, orgID, connectionID, s.openCLIBrowserRiskPolicy(risk.Provider),
	); riskErr == nil {
		risk = refreshed
	}
	if paused && risk.State != "paused" {
		writeError(w, r, http.StatusConflict, "browser_risk_revalidation_required", "Resolve the current browser account protection state before pausing")
		return
	}
	if !paused && risk.State != "normal" {
		writeError(w, r, http.StatusConflict, "browser_risk_revalidation_required", "Revalidate the browser account before resuming")
		return
	}
	errorCode, message := "", ""
	if paused {
		errorCode = "browser_paused"
		message = "个人浏览器中转已由账号所有者暂停"
	}
	_ = s.repo.SetAIBrowserConnectionAuthStatus(
		r.Context(), user.ID, orgID, connectionID, !paused, errorCode, message,
	)
	action := "ai_browser_connection.resumed"
	if paused {
		action = "ai_browser_connection.paused"
	}
	_ = s.repo.WriteAudit(r.Context(), store.AuditEvent{
		ActorType: "user", ActorID: user.ID, Action: action,
		ObjectType: "ai_connection", ObjectID: connectionID, IP: clientIP(r), Result: "success",
		Metadata: map[string]any{"provider": risk.Provider},
	})
	writeJSON(w, http.StatusOK, map[string]any{"risk": risk})
}

func (s *Server) openCLIBrowserProviderEnabled(provider string) bool {
	if !s.cfg.AIOpenCLIBrowserEnabled {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "openai":
		return s.cfg.AIOpenCLIChatGPTEnabled
	case "gemini":
		return s.cfg.AIOpenCLIGeminiEnabled
	case "deepseek":
		return s.cfg.AIOpenCLIDeepSeekEnabled
	default:
		return false
	}
}

func (s *Server) openCLIBrowserRiskPolicy(provider string) store.AIBrowserRiskPolicy {
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "openai":
		return store.AIBrowserRiskPolicy{
			MinimumInterval: s.cfg.AIOpenCLIChatGPTMinInterval,
			HourlyLimit:     s.cfg.AIOpenCLIChatGPTHourlyLimit,
			DailyLimit:      s.cfg.AIOpenCLIChatGPTDailyLimit,
		}
	case "gemini":
		return store.AIBrowserRiskPolicy{
			MinimumInterval: s.cfg.AIOpenCLIGeminiMinInterval,
			HourlyLimit:     s.cfg.AIOpenCLIGeminiHourlyLimit,
			DailyLimit:      s.cfg.AIOpenCLIGeminiDailyLimit,
		}
	default:
		return store.AIBrowserRiskPolicy{
			MinimumInterval: s.cfg.AIOpenCLIDeepSeekMinInterval,
			HourlyLimit:     s.cfg.AIOpenCLIDeepSeekHourlyLimit,
			DailyLimit:      s.cfg.AIOpenCLIDeepSeekDailyLimit,
		}
	}
}

func (s *Server) waitForAIBrowserTask(ctx context.Context, ownerUserID string, orgID string, taskID string, timeout time.Duration) (store.AIBrowserTask, error) {
	waitCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		task, err := s.repo.AIBrowserTaskForOwner(waitCtx, ownerUserID, orgID, taskID)
		if err != nil {
			return store.AIBrowserTask{}, err
		}
		switch task.Status {
		case "completed", "failed", "expired", "cancelled":
			scrubCtx, scrubCancel := context.WithTimeout(context.Background(), 2*time.Second)
			if scrubErr := s.repo.ScrubAIBrowserTaskPayloadForOwner(scrubCtx, ownerUserID, orgID, taskID); scrubErr != nil {
				s.logger.Warn("scrub local browser task payload failed", "task_id", taskID, "error", scrubErr)
			}
			scrubCancel()
			return task, nil
		}
		select {
		case <-waitCtx.Done():
			cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 2*time.Second)
			_ = s.repo.ExpireAIBrowserTaskForOwner(cleanupCtx, ownerUserID, orgID, taskID)
			cleanupCancel()
			return store.AIBrowserTask{}, waitCtx.Err()
		case <-ticker.C:
		}
	}
}

func (s *Server) deviceAIBrowserConnector(w http.ResponseWriter, r *http.Request) (store.AIBrowserConnector, bool) {
	if !s.requireOpenCLIBrowserFeature(w, r) {
		return store.AIBrowserConnector{}, false
	}
	scheme, token, ok := strings.Cut(strings.TrimSpace(r.Header.Get("Authorization")), " ")
	if !ok || !strings.EqualFold(scheme, "Bearer") {
		writeError(w, r, http.StatusUnauthorized, "browser_connector_unauthorized", "Local browser connector token is required")
		return store.AIBrowserConnector{}, false
	}
	connector, err := s.repo.AuthenticateAIBrowserConnector(r.Context(), token)
	if errors.Is(err, store.ErrAIBrowserConnectorUnauthorized) {
		writeError(w, r, http.StatusUnauthorized, "browser_connector_unauthorized", "Local browser connector token is invalid")
		return store.AIBrowserConnector{}, false
	}
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "browser_connector_auth_failed", "Could not authenticate local browser connector")
		return store.AIBrowserConnector{}, false
	}
	return connector, true
}

func (s *Server) requireOpenCLIBrowserFeature(w http.ResponseWriter, r *http.Request) bool {
	if s.cfg.AIOpenCLIBrowserEnabled {
		return true
	}
	writeError(w, r, http.StatusNotFound, "browser_connector_disabled", "Local browser connector is disabled")
	return false
}

func decodeBrowserConnectorJSON(w http.ResponseWriter, r *http.Request, target any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, browserConnectorRequestBodyLimit)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid_json", "Invalid JSON body")
		return false
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		writeError(w, r, http.StatusBadRequest, "invalid_json", "Invalid JSON body")
		return false
	}
	return true
}

func containsBrowserCapability(capabilities []string, provider string) bool {
	for _, capability := range capabilities {
		if strings.EqualFold(strings.TrimSpace(capability), strings.TrimSpace(provider)) {
			return true
		}
	}
	return false
}

func shellQuoteBrowserConnectorArgument(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}
