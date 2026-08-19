package api

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"tokhub/internal/clientconnector"
	"tokhub/internal/connections"
	"tokhub/internal/store"
)

const (
	clientConnectorRequestBodyLimit = 2 << 20
	officialClientTermsVersion      = "official-client-container-v1"
)

type originalPeerAddrContextKey struct{}

type createClientConnectorRequest struct {
	DisplayName string `json:"displayName"`
}

type pairClientConnectorRequest struct {
	PairingCode string `json:"pairingCode"`
	PublicKey   string `json:"publicKey"`
}

type createClientConnectionRequest struct {
	ConnectorID string `json:"connectorId"`
	Provider    string `json:"provider"`
	DisplayName string `json:"displayName"`
}

type encryptedClientPayload struct {
	Ciphertext string `json:"ciphertext"`
	Nonce      string `json:"nonce"`
}

type clientTaskLeaseRequest struct {
	LeaseToken string `json:"leaseToken"`
}

func (s *Server) meAIClientConnectors(w http.ResponseWriter, r *http.Request) {
	if !s.requireOfficialClientFeature(w, r) {
		return
	}
	user, _ := s.userFromRequest(r)
	orgID, ok := s.personalAIConnectionWorkspace(w, r, user)
	if !ok {
		return
	}
	items, err := s.repo.AIClientConnectorsForOwner(r.Context(), user.ID, orgID)
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "client_connectors_unavailable", "Could not load official client connectors")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) createAIClientConnector(w http.ResponseWriter, r *http.Request) {
	if !s.requireOfficialClientFeature(w, r) {
		return
	}
	user, _ := s.userFromRequest(r)
	orgID, ok := s.personalAIConnectionWorkspace(w, r, user)
	if !ok {
		return
	}
	if !s.allowRate(s.authLimiter, "official-client-create:"+user.ID+":"+clientIP(r), 6, time.Hour) {
		writeError(w, r, http.StatusTooManyRequests, "rate_limited", "Official client connector creation is temporarily limited")
		return
	}
	var request createClientConnectorRequest
	if !decodeClientConnectorJSON(w, r, &request) {
		return
	}
	result, err := s.repo.CreateAIClientConnector(r.Context(), user.ID, orgID, request.DisplayName)
	if err != nil {
		writeError(w, r, http.StatusBadRequest, "client_connector_create_failed", "Could not create official client connector")
		return
	}
	_ = s.repo.WriteAudit(r.Context(), store.AuditEvent{
		ActorType: "user", ActorID: user.ID, Action: "ai_client_connector.created",
		ObjectType: "ai_client_connector", ObjectID: result.Connector.ID, IP: clientIP(r), Result: "success",
		Metadata: map[string]any{"org_id": orgID, "runtime": "container"},
	})
	writeJSON(w, http.StatusCreated, map[string]any{
		"connector":   result.Connector,
		"pairingCode": result.PairingCode,
		"pairCommand": fmt.Sprintf("tokhub-client-connector pair --server %s --code %s",
			shellQuoteBrowserConnectorArgument(strings.TrimRight(s.cfg.PublicURL, "/")),
			shellQuoteBrowserConnectorArgument(result.PairingCode)),
		"expiresInSeconds": 600,
	})
}

func (s *Server) revokeAIClientConnector(w http.ResponseWriter, r *http.Request) {
	if !s.requireOfficialClientFeature(w, r) {
		return
	}
	user, _ := s.userFromRequest(r)
	orgID, ok := s.personalAIConnectionWorkspace(w, r, user)
	if !ok {
		return
	}
	connectorID := chi.URLParam(r, "connectorID")
	err := s.repo.RevokeAIClientConnector(r.Context(), user.ID, orgID, connectorID)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, r, http.StatusNotFound, "client_connector_not_found", "Official client connector was not found")
		return
	}
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "client_connector_revoke_failed", "Could not revoke official client connector")
		return
	}
	_ = s.repo.WriteAudit(r.Context(), store.AuditEvent{
		ActorType: "user", ActorID: user.ID, Action: "ai_client_connector.revoked",
		ObjectType: "ai_client_connector", ObjectID: connectorID, IP: clientIP(r), Result: "success",
		Metadata: map[string]any{"org_id": orgID},
	})
	writeJSON(w, http.StatusOK, map[string]string{"status": "revoked"})
}

func (s *Server) pairAIClientConnector(w http.ResponseWriter, r *http.Request) {
	if !s.requireOfficialClientFeature(w, r) || !s.requireSecureClientTransport(w, r) {
		return
	}
	if !s.allowRate(s.authLimiter, "official-client-pair:"+clientIP(r), 20, time.Minute) {
		writeError(w, r, http.StatusTooManyRequests, "rate_limited", "Official client pairing is temporarily limited")
		return
	}
	var request pairClientConnectorRequest
	if !decodeClientConnectorJSON(w, r, &request) {
		return
	}
	result, err := s.repo.PairAIClientConnector(r.Context(), request.PairingCode, request.PublicKey)
	if errors.Is(err, store.ErrAIClientConnectorPairingInvalid) {
		writeError(w, r, http.StatusUnauthorized, "pairing_code_invalid", "Pairing code or device public key is invalid")
		return
	}
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "client_connector_pair_failed", "Could not pair official client connector")
		return
	}
	_ = s.repo.WriteAudit(r.Context(), store.AuditEvent{
		ActorType: "connector", ActorID: result.Connector.ID, Action: "ai_client_connector.paired",
		ObjectType: "ai_client_connector", ObjectID: result.Connector.ID, IP: clientIP(r), Result: "success",
		Metadata: map[string]any{"org_id": result.Connector.OrgID, "signature": "ed25519"},
	})
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) heartbeatAIClientConnector(w http.ResponseWriter, r *http.Request) {
	connector, body, ok := s.signedAIClientConnector(w, r)
	if !ok {
		return
	}
	var heartbeat clientconnector.Heartbeat
	if !decodeClientConnectorBytes(w, r, body, &heartbeat) {
		return
	}
	if err := sanitizeAIClientHeartbeat(&heartbeat); err != nil {
		writeError(w, r, http.StatusBadRequest, "client_connector_heartbeat_invalid", "Official client heartbeat is invalid")
		return
	}
	identity := make(map[string]any, len(heartbeat.Identity))
	for provider, status := range heartbeat.Identity {
		identity[provider] = status
	}
	item, err := s.repo.HeartbeatAIClientConnector(r.Context(), connector.ID,
		heartbeat.ConnectorVersion, heartbeat.CodexVersion, heartbeat.GrokVersion,
		heartbeat.Capabilities, identity)
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "client_connector_heartbeat_failed", "Could not update official client connector")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"connector": item})
}

func (s *Server) claimAIClientConnectorTask(w http.ResponseWriter, r *http.Request) {
	connector, _, ok := s.signedAIClientConnector(w, r)
	if !ok {
		return
	}
	task, err := s.repo.ClaimAIClientTask(r.Context(), connector.ID, s.cfg.AIOfficialClientTaskTimeout)
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "client_task_claim_failed", "Could not claim official client task")
		return
	}
	if task == nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	var payload clientconnector.TaskPayload
	if err := s.getEncryptedAIClientPayload(r.Context(), task.PayloadKey, &payload); err != nil {
		_ = s.repo.CompleteAIClientTask(r.Context(), connector.ID, task.ID, task.LeaseToken, false, "", "payload_unavailable", "Encrypted task payload is unavailable")
		_ = s.gatewayCache.DeleteOfficialClientPayload(r.Context(), task.PayloadKey)
		writeError(w, r, http.StatusServiceUnavailable, "client_task_payload_unavailable", "Official client task payload is unavailable")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"task": clientconnector.ClaimedTask{
		ID: task.ID, Provider: task.Provider, Action: task.Action, LeaseToken: task.LeaseToken,
		ExpiresAt: task.ExpiresAt, Payload: payload,
	}})
	// The prompt leaves Redis after delivery. A lost delivery fails closed and is never retried.
	_ = s.gatewayCache.DeleteOfficialClientPayload(context.Background(), task.PayloadKey)
}

func (s *Server) completeAIClientConnectorTask(w http.ResponseWriter, r *http.Request) {
	connector, body, ok := s.signedAIClientConnector(w, r)
	if !ok {
		return
	}
	var request clientconnector.CompleteTaskRequest
	if !decodeClientConnectorBytes(w, r, body, &request) {
		return
	}
	result, err := sanitizeAIClientResult(request.Result)
	if err != nil {
		writeError(w, r, http.StatusBadRequest, "client_task_result_invalid", "Official client task result is invalid")
		return
	}
	if result.ToolEvent {
		result.OK, result.ErrorCode, result.ErrorMessage = false, "tool_event", "Official client attempted a blocked capability"
	}
	result.AccountMask = maskAIClientAccount(result.AccountID)
	if !result.OK {
		result.ErrorMessage = safeAIClientErrorMessage(result.ErrorCode)
	}
	resultKey := "result:" + chi.URLParam(r, "taskID") + ":" + uuid.NewString()
	if result.OK {
		if err := s.putEncryptedAIClientPayload(r.Context(), resultKey, result, 3*time.Minute); err != nil {
			writeError(w, r, http.StatusServiceUnavailable, "client_result_store_unavailable", "Could not store encrypted official client result")
			return
		}
	} else {
		resultKey = ""
	}
	err = s.repo.CompleteAIClientTask(r.Context(), connector.ID, chi.URLParam(r, "taskID"), request.LeaseToken,
		result.OK, resultKey, result.ErrorCode, result.ErrorMessage)
	if errors.Is(err, store.ErrAIClientTaskLeaseInvalid) {
		_ = s.gatewayCache.DeleteOfficialClientPayload(r.Context(), resultKey)
		writeError(w, r, http.StatusConflict, "client_task_lease_invalid", "Official client task lease is invalid or expired")
		return
	}
	if err != nil {
		_ = s.gatewayCache.DeleteOfficialClientPayload(r.Context(), resultKey)
		writeError(w, r, http.StatusInternalServerError, "client_task_complete_failed", "Could not complete official client task")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "accepted"})
}

func (s *Server) statusAIClientConnectorTask(w http.ResponseWriter, r *http.Request) {
	connector, body, ok := s.signedAIClientConnector(w, r)
	if !ok {
		return
	}
	var request clientTaskLeaseRequest
	if !decodeClientConnectorBytes(w, r, body, &request) {
		return
	}
	active, err := s.repo.AIClientTaskLeaseActive(r.Context(), connector.ID, chi.URLParam(r, "taskID"), request.LeaseToken)
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "client_task_status_failed", "Could not check official client task status")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"active": active})
}

func (s *Server) createAIClientConnection(w http.ResponseWriter, r *http.Request) {
	if !s.requireOfficialClientFeature(w, r) {
		return
	}
	user, _ := s.userFromRequest(r)
	orgID, ok := s.personalAIConnectionWorkspace(w, r, user)
	if !ok {
		return
	}
	var request createClientConnectionRequest
	if !decodeClientConnectorJSON(w, r, &request) {
		return
	}
	provider := strings.ToLower(strings.TrimSpace(request.Provider))
	if provider != "openai" && provider != "grok" || s.providerPolicyAllowsWithLab(provider, "official_client", false) != nil {
		writeError(w, r, http.StatusForbidden, "provider_policy_denied", "This provider does not allow an official client connection")
		return
	}
	connector, err := s.repo.AIClientConnectorForOwner(r.Context(), user.ID, orgID, request.ConnectorID)
	capability := map[string]string{"openai": "chatgpt", "grok": "grok"}[provider]
	if err != nil || !connector.Online || !containsClientCapability(connector.Capabilities, capability) {
		writeError(w, r, http.StatusConflict, "client_connector_offline", "Start the container connector and log in to the selected official client")
		return
	}
	payloadKey, task, err := s.createEncryptedAIClientTask(r.Context(), store.AIClientTaskInput{
		ConnectorID: connector.ID, OwnerUserID: user.ID, OrgID: orgID, Provider: provider,
		Action: clientconnector.ActionIdentify, ExpiresAt: time.Now().Add(60 * time.Second),
	}, clientconnector.TaskPayload{Provider: provider, Action: clientconnector.ActionIdentify})
	if errors.Is(err, store.ErrAIClientConnectorBusy) {
		writeError(w, r, http.StatusConflict, "client_connector_busy", "Official client connector is handling another task")
		return
	}
	if err != nil {
		writeError(w, r, http.StatusServiceUnavailable, "client_identity_check_failed", "Could not start official client identity check")
		return
	}
	result, err := s.waitForAIClientTask(r.Context(), user.ID, orgID, task.ID, 60*time.Second)
	_ = s.gatewayCache.DeleteOfficialClientPayload(context.Background(), payloadKey)
	if err != nil || !result.OK {
		message := result.ErrorMessage
		if message == "" {
			message = "Official client did not confirm an authenticated account"
		}
		writeError(w, r, http.StatusConflict, "client_login_required", message)
		return
	}
	if s.credentialKeys == nil {
		writeError(w, r, http.StatusServiceUnavailable, "credential_vault_unavailable", "Credential vault is unavailable")
		return
	}
	referenceRaw, _ := json.Marshal(map[string]string{"connectorId": connector.ID, "provider": provider})
	fingerprintSource := strings.TrimSpace(result.AccountID)
	if fingerprintSource == "" {
		fingerprintSource = "device:" + connector.ID
	}
	encrypted, err := s.credentialKeys.EncryptWithFingerprint(user.ID, provider, string(referenceRaw), "official-client\x00"+fingerprintSource)
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "client_reference_encrypt_failed", "Could not protect official client reference")
		return
	}
	resolved, err := connections.ResolveProvider(connections.ResolveProviderInput{Code: provider})
	if err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid_provider_profile", err.Error())
		return
	}
	model := map[string]string{"openai": "chatgpt-personal", "grok": "grok-personal"}[provider]
	productLine := map[string]string{"openai": "ChatGPT · Codex app-server", "grok": "Grok · Build ACP"}[provider]
	resolved.ProviderConfig["authMethod"] = "official_client"
	resolved.ProviderConfig["connectorId"] = connector.ID
	resolved.ProviderConfig["identityAssurance"] = nonEmpty(result.IdentityAssurance, "device")
	resolved.ProviderConfig["officialClient"] = true
	resolved.ProviderConfig["sharingScope"] = "personal"
	resolved.ProviderConfig["actualModels"] = result.Models
	if provider == "openai" {
		resolved.ProviderConfig["clientVersion"] = connector.CodexVersion
	} else {
		resolved.ProviderConfig["clientVersion"] = connector.GrokVersion
	}
	resolved.Endpoint = "client+official://" + connector.ID + "/" + provider
	validation := store.AIConnectionValidation{
		OK: true, Stage: "official_client_identity", ModelCount: 1,
		Models: []store.AIConnectionModelValidation{{ProviderModelID: model, OK: true}},
	}
	item, err := s.repo.CreateAIConnection(r.Context(), store.AIConnectionCreateInput{
		OwnerUserID: user.ID, OrgID: orgID, Provider: provider, ProductLine: productLine,
		Region: resolved.Region, Protocol: resolved.Manifest.Protocol, AdapterType: resolved.Manifest.Type,
		Endpoint: resolved.Endpoint, ProviderConfig: resolved.ProviderConfig,
		DisplayName: cleanConnectionDisplayName(request.DisplayName, productLine), Models: []string{model},
		Credential: store.AIConnectionSecret{
			Ciphertext: encrypted.Ciphertext, Nonce: encrypted.Nonce, Mask: "Official Client · " + nonEmpty(result.AccountMask, "device identity"),
			Fingerprint: encrypted.Fingerprint, EncryptionKeyID: encrypted.EncryptionKeyID,
			FingerprintKeyID: encrypted.FingerprintKeyID, Algorithm: encrypted.Algorithm,
			SecretType: "client_connector", PayloadFormat: "client_connector_v1", SubjectFingerprint: encrypted.Fingerprint,
		},
		Validation: validation, AuthMethod: "official_client", AuthStatus: "active", SharingScope: "personal",
		RiskLevel: "elevated", ProviderAdapterVersion: "official-client-v1", TermsAckVersion: officialClientTermsVersion,
		AccountMask: nonEmpty(result.AccountMask, "device identity"),
	})
	if errors.Is(err, store.ErrAIConnectionDuplicate) {
		writeError(w, r, http.StatusConflict, "ai_connection_duplicate", "An official client connection for this provider already exists")
		return
	}
	if err != nil {
		writeError(w, r, http.StatusBadRequest, "client_connection_create_failed", "Could not save official client connection")
		return
	}
	if err := s.repo.InitializeAIClientRisk(r.Context(), item.ID, user.ID, orgID, connector.ID, provider,
		encrypted.Fingerprint, nonEmpty(result.IdentityAssurance, "device")); err != nil {
		_ = s.repo.DeleteAIConnection(r.Context(), user.ID, orgID, item.ID)
		writeError(w, r, http.StatusInternalServerError, "client_risk_initialize_failed", "Could not initialize official client safety controls")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"connection": item, "actualModels": result.Models})
}

func (s *Server) validateAIClientConnection(w http.ResponseWriter, r *http.Request, ownerUserID, orgID string, item store.AIConnection) {
	if !s.requireOfficialClientFeature(w, r) {
		return
	}
	if err := s.providerPolicyAllowsWithLab(item.Provider, "official_client", false); err != nil {
		writeError(w, r, http.StatusForbidden, "provider_policy_denied", "Official client validation is disabled by provider policy")
		return
	}
	secret, err := s.repo.AIConnectionSecretForOwnerOrg(r.Context(), ownerUserID, orgID, item.ID)
	if err != nil || secret.SecretType != "client_connector" || secret.PayloadFormat != "client_connector_v1" {
		writeError(w, r, http.StatusConflict, "client_reference_unavailable", "Official client reference is unavailable")
		return
	}
	connectorID := strings.TrimSpace(stringFromAny(item.ProviderConfig["connectorId"]))
	connector, err := s.repo.AIClientConnectorForOwner(r.Context(), ownerUserID, orgID, connectorID)
	capability := map[string]string{"openai": "chatgpt", "grok": "grok"}[item.Provider]
	if err != nil || !connector.Online || !containsClientCapability(connector.Capabilities, capability) {
		writeError(w, r, http.StatusConflict, "client_connector_offline", "Start the dedicated connector container and log in to the official client")
		return
	}
	started := time.Now()
	payloadKey, task, err := s.createEncryptedAIClientTask(r.Context(), store.AIClientTaskInput{
		ConnectorID: connector.ID, OwnerUserID: ownerUserID, OrgID: orgID, ConnectionID: item.ID,
		Provider: item.Provider, Action: clientconnector.ActionIdentify, ExpiresAt: time.Now().Add(time.Minute),
	}, clientconnector.TaskPayload{Provider: item.Provider, Action: clientconnector.ActionIdentify})
	if errors.Is(err, store.ErrAIClientConnectorBusy) {
		writeError(w, r, http.StatusConflict, "client_connector_busy", "Official client connector is handling another task")
		return
	}
	if err != nil {
		writeError(w, r, http.StatusServiceUnavailable, "client_identity_check_failed", "Could not start official client identity check")
		return
	}
	result, waitErr := s.waitForAIClientTask(r.Context(), ownerUserID, orgID, task.ID, time.Minute)
	_ = s.gatewayCache.DeleteOfficialClientPayload(context.Background(), payloadKey)
	model := map[string]string{"openai": "chatgpt-personal", "grok": "grok-personal"}[item.Provider]
	if waitErr != nil || !result.OK {
		code := nonEmpty(result.ErrorCode, "client_unavailable")
		message := strings.TrimSpace(result.ErrorMessage)
		if message == "" {
			message = "Official client did not confirm an authenticated account"
		}
		validation := connectionValidationResult{
			Provider: item.Provider, Type: item.AdapterType, Endpoint: item.Endpoint, Model: model,
			Stage: "official_client_identity", LatencyMs: int(time.Since(started).Milliseconds()),
			ModelCount: 1, ErrorType: code, Message: message,
			Models: []connectionModelValidationResult{{Model: model, ErrorType: code, Message: message}},
		}
		updated, updateErr := s.repo.UpdateAIConnectionValidation(r.Context(), ownerUserID, orgID, item.ID, secret.Version, validationStoreValue(validation))
		if updateErr != nil {
			writeError(w, r, http.StatusInternalServerError, "ai_connection_validate_failed", "Could not update official client validation")
			return
		}
		if risk, riskErr := s.repo.RecordAIClientResult(r.Context(), ownerUserID, orgID, item.ID, false, code, 0, time.Now()); riskErr == nil {
			_ = s.repo.SetAIClientConnectionRiskState(r.Context(), ownerUserID, orgID, item.ID, risk.State, code, message)
			if refreshed, readErr := s.repo.AIConnectionForOwnerOrg(r.Context(), ownerUserID, orgID, item.ID); readErr == nil {
				updated = refreshed
			}
		}
		writeJSON(w, http.StatusOK, map[string]any{"connection": updated, "validation": validation})
		return
	}
	if s.credentialKeys == nil {
		writeError(w, r, http.StatusServiceUnavailable, "credential_vault_unavailable", "Credential vault is unavailable")
		return
	}
	fingerprintSource := strings.TrimSpace(result.AccountID)
	if fingerprintSource == "" {
		fingerprintSource = "device:" + connector.ID
	}
	comparison, err := s.credentialKeys.EncryptWithFingerprint(ownerUserID, item.Provider,
		"official-client-identity-check", "official-client\x00"+fingerprintSource)
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "client_identity_check_failed", "Could not verify official client identity")
		return
	}
	risk, matched, err := s.repo.ConfirmAIClientIdentity(r.Context(), ownerUserID, orgID, item.ID,
		comparison.Fingerprint, nonEmpty(result.IdentityAssurance, "device"), time.Now())
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "client_identity_check_failed", "Could not persist official client identity check")
		return
	}
	validation := connectionValidationResult{
		OK: matched, Provider: item.Provider, Type: item.AdapterType, Endpoint: item.Endpoint, Model: model,
		Stage: "official_client_identity", LatencyMs: int(time.Since(started).Milliseconds()), ModelCount: 1,
		Models: []connectionModelValidationResult{{OK: matched, Model: model, LatencyMs: int(time.Since(started).Milliseconds())}},
	}
	if matched {
		validation.Message = "Official client login and account identity are valid"
		clientVersion := connector.GrokVersion
		if item.Provider == "openai" {
			clientVersion = connector.CodexVersion
		}
		if err := s.repo.UpdateAIClientConnectionRuntimeDetails(r.Context(), ownerUserID, orgID, item.ID, result.Models, clientVersion); err != nil {
			writeError(w, r, http.StatusInternalServerError, "client_runtime_details_update_failed", "Could not update official client runtime details")
			return
		}
	} else {
		validation.ErrorType = "identity_changed"
		validation.Message = "Official client account identity changed; the connection is locked for review"
		validation.Models[0].ErrorType = validation.ErrorType
		validation.Models[0].Message = validation.Message
	}
	updated, err := s.repo.UpdateAIConnectionValidation(r.Context(), ownerUserID, orgID, item.ID, secret.Version, validationStoreValue(validation))
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "ai_connection_validate_failed", "Could not update official client validation")
		return
	}
	_ = s.repo.SetAIClientConnectionRiskState(r.Context(), ownerUserID, orgID, item.ID, risk.State, risk.LastErrorCode, validation.Message)
	if refreshed, readErr := s.repo.AIConnectionForOwnerOrg(r.Context(), ownerUserID, orgID, item.ID); readErr == nil {
		updated = refreshed
	}
	writeJSON(w, http.StatusOK, map[string]any{"connection": updated, "validation": validation, "risk": risk})
}

func (s *Server) meAIClientConnectionRisk(w http.ResponseWriter, r *http.Request) {
	user, _ := s.userFromRequest(r)
	orgID, ok := s.personalAIConnectionWorkspace(w, r, user)
	if !ok {
		return
	}
	risk, err := s.repo.AIClientRiskForConnection(r.Context(), user.ID, orgID, chi.URLParam(r, "connectionID"))
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, r, http.StatusNotFound, "client_connection_not_found", "Official client connection was not found")
		return
	}
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "client_risk_unavailable", "Could not load official client risk state")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"risk": risk})
}

func (s *Server) pauseAIClientConnection(w http.ResponseWriter, r *http.Request) {
	s.setAIClientConnectionPause(w, r, true)
}

func (s *Server) resumeAIClientConnection(w http.ResponseWriter, r *http.Request) {
	s.setAIClientConnectionPause(w, r, false)
}

func (s *Server) setAIClientConnectionPause(w http.ResponseWriter, r *http.Request, paused bool) {
	user, _ := s.userFromRequest(r)
	orgID, ok := s.personalAIConnectionWorkspace(w, r, user)
	if !ok {
		return
	}
	risk, err := s.repo.SetAIClientConnectionPaused(r.Context(), user.ID, orgID, chi.URLParam(r, "connectionID"), paused)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, r, http.StatusNotFound, "client_connection_not_found", "Official client connection was not found")
		return
	}
	if errors.Is(err, store.ErrAIClientRiskTransitionDenied) {
		writeError(w, r, http.StatusConflict, "client_risk_transition_denied", "Complete the required login or security review before changing this connection state")
		return
	}
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "client_risk_update_failed", "Could not update official client risk state")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"risk": risk})
}

func (s *Server) deleteAIClientSession(w http.ResponseWriter, r *http.Request) {
	user, _ := s.userFromRequest(r)
	orgID, ok := s.personalAIConnectionWorkspace(w, r, user)
	if !ok {
		return
	}
	response, err := s.repo.DeleteAIClientResponseForOwner(r.Context(), user.ID, orgID, chi.URLParam(r, "responseID"))
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, r, http.StatusNotFound, "client_session_not_found", "Official client session was not found")
		return
	}
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "client_session_delete_failed", "Could not delete official client session")
		return
	}
	if response.SessionCiphertext != "" && response.SessionNonce != "" && s.secretBox != nil {
		if sessionRef, decryptErr := s.secretBox.Decrypt(response.SessionCiphertext, response.SessionNonce); decryptErr == nil {
			if connector, connectorErr := s.repo.AIClientConnectorForOwner(r.Context(), user.ID, orgID, response.ConnectorID); connectorErr == nil && connector.Online {
				_, _, _ = s.createEncryptedAIClientTask(context.Background(), store.AIClientTaskInput{
					ConnectorID: response.ConnectorID, OwnerUserID: user.ID, OrgID: orgID, ConnectionID: response.ConnectionID,
					Provider: response.Provider, Action: clientconnector.ActionDeleteSession, ExpiresAt: time.Now().Add(time.Minute),
				}, clientconnector.TaskPayload{Provider: response.Provider, Action: clientconnector.ActionDeleteSession, PreviousSessionRef: sessionRef})
			}
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) signedAIClientConnector(w http.ResponseWriter, r *http.Request) (store.AIClientConnector, []byte, bool) {
	if !s.requireOfficialClientFeature(w, r) || !s.requireSecureClientTransport(w, r) {
		return store.AIClientConnector{}, nil, false
	}
	r.Body = http.MaxBytesReader(w, r.Body, clientConnectorRequestBodyLimit)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid_body", "Official client request body is invalid")
		return store.AIClientConnector{}, nil, false
	}
	scheme, token, ok := strings.Cut(strings.TrimSpace(r.Header.Get("Authorization")), " ")
	if !ok || !strings.EqualFold(scheme, "Bearer") {
		writeError(w, r, http.StatusUnauthorized, "client_connector_unauthorized", "Official client connector token is required")
		return store.AIClientConnector{}, nil, false
	}
	connector, err := s.repo.AuthenticateAIClientConnector(r.Context(), token)
	if errors.Is(err, store.ErrAIClientConnectorUnauthorized) {
		writeError(w, r, http.StatusUnauthorized, "client_connector_unauthorized", "Official client connector token is invalid")
		return store.AIClientConnector{}, nil, false
	}
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "client_connector_auth_failed", "Could not authenticate official client connector")
		return store.AIClientConnector{}, nil, false
	}
	timestamp, err := strconv.ParseInt(strings.TrimSpace(r.Header.Get("X-TokHub-Timestamp")), 10, 64)
	if err != nil {
		writeError(w, r, http.StatusUnauthorized, "client_signature_invalid", "Official client request timestamp is invalid")
		return store.AIClientConnector{}, nil, false
	}
	nonce := strings.TrimSpace(r.Header.Get("X-TokHub-Nonce"))
	path := r.URL.EscapedPath()
	if path == "" {
		path = r.URL.Path
	}
	if err := clientconnector.VerifyRequest(connector.PublicKey, r.Header.Get("X-TokHub-Signature"), r.Method, path, body, timestamp, nonce, time.Now()); err != nil {
		writeError(w, r, http.StatusUnauthorized, "client_signature_invalid", "Official client request signature is invalid or expired")
		return store.AIClientConnector{}, nil, false
	}
	if s.gatewayCache == nil {
		writeError(w, r, http.StatusServiceUnavailable, "client_nonce_store_unavailable", "Official client replay protection is unavailable")
		return store.AIClientConnector{}, nil, false
	}
	used, err := s.gatewayCache.UseOfficialClientNonce(r.Context(), connector.ID, nonce, 2*time.Minute)
	if err != nil {
		writeError(w, r, http.StatusServiceUnavailable, "client_nonce_store_unavailable", "Official client replay protection is unavailable")
		return store.AIClientConnector{}, nil, false
	}
	if !used {
		writeError(w, r, http.StatusConflict, "client_nonce_replayed", "Official client request nonce was already used")
		return store.AIClientConnector{}, nil, false
	}
	return connector, body, true
}

func (s *Server) createEncryptedAIClientTask(ctx context.Context, input store.AIClientTaskInput, payload clientconnector.TaskPayload) (string, store.AIClientTask, error) {
	key := "task:" + uuid.NewString()
	ttl := time.Until(input.ExpiresAt)
	if ttl <= 0 {
		ttl = s.cfg.AIOfficialClientTaskTimeout
	}
	if err := s.putEncryptedAIClientPayload(ctx, key, payload, ttl); err != nil {
		return "", store.AIClientTask{}, err
	}
	input.PayloadKey = key
	task, err := s.repo.CreateAIClientTask(ctx, input)
	if err != nil {
		_ = s.gatewayCache.DeleteOfficialClientPayload(context.Background(), key)
		return "", store.AIClientTask{}, err
	}
	return key, task, nil
}

func (s *Server) waitForAIClientTask(ctx context.Context, ownerUserID, orgID, taskID string, timeout time.Duration) (clientconnector.TaskResult, error) {
	waitCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	ticker := time.NewTicker(150 * time.Millisecond)
	defer ticker.Stop()
	for {
		task, err := s.repo.AIClientTaskForOwner(waitCtx, ownerUserID, orgID, taskID)
		if err != nil {
			return clientconnector.TaskResult{}, err
		}
		switch task.Status {
		case "completed":
			var result clientconnector.TaskResult
			if task.ResultKey == "" {
				return result, errors.New("official client result is missing")
			}
			if err := s.getEncryptedAIClientPayload(waitCtx, task.ResultKey, &result); err != nil {
				return result, err
			}
			_ = s.gatewayCache.DeleteOfficialClientPayload(context.Background(), task.ResultKey)
			return result, nil
		case "failed", "expired", "cancelled":
			return clientconnector.TaskResult{ErrorCode: task.ErrorCode, ErrorMessage: task.ErrorMessage}, nil
		}
		select {
		case <-waitCtx.Done():
			_ = s.repo.CancelAIClientTask(context.Background(), ownerUserID, orgID, taskID, "Official client request cancelled")
			return clientconnector.TaskResult{}, waitCtx.Err()
		case <-ticker.C:
		}
	}
}

func (s *Server) putEncryptedAIClientPayload(ctx context.Context, key string, value any, ttl time.Duration) error {
	if s.secretBox == nil || s.gatewayCache == nil {
		return errors.New("official client encrypted payload store is unavailable")
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	encrypted, err := s.secretBox.Encrypt(string(raw))
	if err != nil {
		return err
	}
	envelope, err := json.Marshal(encryptedClientPayload{Ciphertext: encrypted.Ciphertext, Nonce: encrypted.Nonce})
	if err != nil {
		return err
	}
	return s.gatewayCache.PutOfficialClientPayload(ctx, key, envelope, ttl)
}

func (s *Server) getEncryptedAIClientPayload(ctx context.Context, key string, target any) error {
	if s.secretBox == nil || s.gatewayCache == nil {
		return errors.New("official client encrypted payload store is unavailable")
	}
	raw, err := s.gatewayCache.OfficialClientPayload(ctx, key)
	if err != nil {
		return err
	}
	var envelope encryptedClientPayload
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return err
	}
	plain, err := s.secretBox.Decrypt(envelope.Ciphertext, envelope.Nonce)
	if err != nil {
		return err
	}
	return json.Unmarshal([]byte(plain), target)
}

func (s *Server) requireOfficialClientFeature(w http.ResponseWriter, r *http.Request) bool {
	if s.cfg.AIOfficialClientEnabled {
		return true
	}
	writeError(w, r, http.StatusNotFound, "official_client_disabled", "Official client connector is disabled")
	return false
}

func (s *Server) requireSecureClientTransport(w http.ResponseWriter, r *http.Request) bool {
	peerAddr := originalPeerAddr(r)
	secure := r.TLS != nil
	if !secure && strings.EqualFold(strings.TrimSpace(r.Header.Get("X-Forwarded-Proto")), "https") {
		secure = trustedProxyRemote(peerAddr, s.cfg.AIOfficialClientTrustedProxyCIDRs)
	}
	if secure || (s.cfg.Env != "production" && loopbackRequestHost(r.Host) && loopbackRemote(peerAddr)) {
		return true
	}
	writeError(w, r, http.StatusUpgradeRequired, "https_required", "Official client connectors require HTTPS; development HTTP is limited to loopback")
	return false
}

func captureOriginalPeerAddr(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := context.WithValue(r.Context(), originalPeerAddrContextKey{}, r.RemoteAddr)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func originalPeerAddr(r *http.Request) string {
	if r != nil {
		if value, ok := r.Context().Value(originalPeerAddrContextKey{}).(string); ok && strings.TrimSpace(value) != "" {
			return value
		}
		return r.RemoteAddr
	}
	return ""
}

func trustedProxyRemote(remoteAddr string, trustedCIDRs []string) bool {
	ip := remoteIP(remoteAddr)
	if ip == nil {
		return false
	}
	for _, raw := range trustedCIDRs {
		_, network, err := net.ParseCIDR(strings.TrimSpace(raw))
		if err == nil && network.Contains(ip) {
			return true
		}
	}
	return false
}

func loopbackRemote(remoteAddr string) bool {
	ip := remoteIP(remoteAddr)
	return ip != nil && ip.IsLoopback()
}

func remoteIP(remoteAddr string) net.IP {
	host := strings.TrimSpace(remoteAddr)
	if parsed, _, err := net.SplitHostPort(host); err == nil {
		host = parsed
	}
	return net.ParseIP(strings.Trim(host, "[]"))
}

func loopbackRequestHost(hostPort string) bool {
	host := strings.TrimSpace(hostPort)
	if parsed, _, err := net.SplitHostPort(host); err == nil {
		host = parsed
	}
	host = strings.Trim(host, "[]")
	return strings.EqualFold(host, "localhost") || net.ParseIP(host) != nil && net.ParseIP(host).IsLoopback()
}

func decodeClientConnectorJSON(w http.ResponseWriter, r *http.Request, target any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, clientConnectorRequestBodyLimit)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid_json", "Invalid JSON body")
		return false
	}
	return decodeClientConnectorBytes(w, r, body, target)
}

func decodeClientConnectorBytes(w http.ResponseWriter, r *http.Request, body []byte, target any) bool {
	decoder := json.NewDecoder(strings.NewReader(string(body)))
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

func containsClientCapability(capabilities []string, expected string) bool {
	for _, capability := range capabilities {
		if strings.EqualFold(strings.TrimSpace(capability), expected) {
			return true
		}
	}
	return false
}

func sanitizeAIClientResult(result clientconnector.TaskResult) (clientconnector.TaskResult, error) {
	result.Content = strings.TrimSpace(result.Content)
	result.SessionRef = strings.TrimSpace(result.SessionRef)
	result.AccountID = strings.TrimSpace(result.AccountID)
	result.AccountMask = strings.TrimSpace(result.AccountMask)
	result.ErrorCode = strings.ToLower(strings.TrimSpace(result.ErrorCode))
	result.ErrorMessage = strings.TrimSpace(result.ErrorMessage)
	result.IdentityAssurance = strings.ToLower(strings.TrimSpace(result.IdentityAssurance))
	result.ActualModel = strings.TrimSpace(result.ActualModel)
	result.ClientVersion = strings.TrimSpace(result.ClientVersion)
	if len(result.Content) > 2<<20 || len(result.SessionRef) > 4096 || len(result.AccountID) > 512 ||
		len(result.AccountMask) > 512 || len(result.ErrorMessage) > 512 || len(result.Models) > 64 ||
		len(result.ErrorCode) > 64 || len(result.ActualModel) > 160 || len(result.ClientVersion) > 64 || result.RetryAfterSeconds < 0 ||
		result.RetryAfterSeconds > 7*24*60*60 {
		return clientconnector.TaskResult{}, errors.New("official client result exceeds limits")
	}
	if strings.ContainsAny(result.SessionRef+result.AccountID+result.ActualModel+result.ClientVersion, "\r\n\x00") ||
		!validAIClientErrorCode(result.ErrorCode) {
		return clientconnector.TaskResult{}, errors.New("official client result labels are invalid")
	}
	if result.IdentityAssurance != "" && result.IdentityAssurance != "account" && result.IdentityAssurance != "device" {
		return clientconnector.TaskResult{}, errors.New("official client identity assurance is invalid")
	}
	seenModels := make(map[string]struct{}, len(result.Models))
	models := make([]string, 0, len(result.Models))
	for _, model := range result.Models {
		model = strings.TrimSpace(model)
		if model == "" || len(model) > 160 || strings.ContainsAny(model, "\r\n\x00") {
			return clientconnector.TaskResult{}, errors.New("official client model is invalid")
		}
		if _, exists := seenModels[model]; exists {
			continue
		}
		seenModels[model] = struct{}{}
		models = append(models, model)
	}
	result.Models = models
	if result.OK && result.ErrorCode != "" {
		return clientconnector.TaskResult{}, errors.New("successful official client result contains an error")
	}
	if result.SessionRef != "" && !strings.HasPrefix(result.SessionRef, clientconnector.SealedSessionReferencePrefix) {
		return clientconnector.TaskResult{}, errors.New("official client session reference is not device sealed")
	}
	if !result.OK && result.ErrorCode == "" {
		result.ErrorCode = "client_error"
	}
	return result, nil
}

func sanitizeAIClientHeartbeat(heartbeat *clientconnector.Heartbeat) error {
	if heartbeat == nil {
		return errors.New("official client heartbeat is empty")
	}
	versions := []*string{&heartbeat.ConnectorVersion, &heartbeat.CodexVersion, &heartbeat.GrokVersion}
	for _, version := range versions {
		*version = strings.TrimSpace(*version)
		if len(*version) > 64 || strings.ContainsAny(*version, "\r\n\x00") {
			return errors.New("official client version is invalid")
		}
	}
	capabilities := make([]string, 0, 2)
	seen := map[string]struct{}{}
	for _, capability := range heartbeat.Capabilities {
		capability = strings.ToLower(strings.TrimSpace(capability))
		if capability != "chatgpt" && capability != "grok" {
			return errors.New("official client capability is invalid")
		}
		if _, exists := seen[capability]; exists {
			continue
		}
		seen[capability] = struct{}{}
		capabilities = append(capabilities, capability)
	}
	heartbeat.Capabilities = capabilities
	if len(heartbeat.Identity) > 2 {
		return errors.New("official client identity map is too large")
	}
	identities := make(map[string]clientconnector.IdentityStatus, len(heartbeat.Identity))
	for provider, identity := range heartbeat.Identity {
		provider = strings.ToLower(strings.TrimSpace(provider))
		if provider != "openai" && provider != "grok" {
			return errors.New("official client identity provider is invalid")
		}
		if _, exists := identities[provider]; exists {
			return errors.New("official client identity provider is duplicated")
		}
		identity.AccountMask = strings.TrimSpace(identity.AccountMask)
		identity.IdentityAssurance = strings.ToLower(strings.TrimSpace(identity.IdentityAssurance))
		if len(identity.AccountMask) > 128 || strings.ContainsAny(identity.AccountMask, "\r\n\x00") {
			return errors.New("official client account mask is invalid")
		}
		if identity.IdentityAssurance != "" && identity.IdentityAssurance != "account" && identity.IdentityAssurance != "device" {
			return errors.New("official client identity assurance is invalid")
		}
		if identity.CheckedAt.IsZero() || identity.CheckedAt.After(time.Now().Add(5*time.Minute)) {
			identity.CheckedAt = time.Now().UTC()
		}
		identities[provider] = identity
	}
	heartbeat.Identity = identities
	return nil
}

func maskAIClientAccount(value string) string {
	value = strings.TrimSpace(value)
	if value == "" || strings.HasPrefix(strings.ToLower(value), "device:") {
		return "device identity"
	}
	if at := strings.LastIndex(value, "@"); at > 0 && at < len(value)-1 {
		local, domain := value[:at], value[at+1:]
		visible := local[:1]
		if len(local) > 1 {
			visible += "***"
		}
		return visible + "@" + domain
	}
	sum := sha256.Sum256([]byte(value))
	return fmt.Sprintf("account · %x", sum[:4])
}

func safeAIClientErrorMessage(code string) string {
	switch strings.ToLower(strings.TrimSpace(code)) {
	case "unauthorized", "login_required", "401":
		return "Official client login is unavailable or expired"
	case "forbidden", "security_challenge", "403":
		return "Official client reported a security restriction"
	case "rate_limited", "429":
		return "Official client reported a rate limit"
	case "tool_event", "tool_call", "policy_violation":
		return "Official client attempted a blocked capability"
	case "timeout", "cancelled":
		return "Official client task timed out or was interrupted"
	case "client_unavailable", "adapter_incompatible":
		return "Official client is unavailable or incompatible"
	default:
		return "Official client task failed"
	}
}

func validAIClientErrorCode(code string) bool {
	for _, character := range code {
		if character >= 'a' && character <= 'z' || character >= '0' && character <= '9' || character == '_' || character == '-' {
			continue
		}
		return false
	}
	return true
}
