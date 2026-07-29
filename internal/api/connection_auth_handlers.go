package api

import (
	"context"
	"errors"
	"fmt"
	"html"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"tokhub/internal/auth"
	"tokhub/internal/connections"
	secretcrypto "tokhub/internal/crypto"
	gatewaycache "tokhub/internal/gateway"
	"tokhub/internal/store"
)

type aiAuthStepUpRequest struct {
	Password string `json:"password"`
}

type startAIAuthorizationRequest struct {
	Provider             string   `json:"provider"`
	Method               string   `json:"method"`
	StepUpGrant          string   `json:"stepUpGrant"`
	DisplayName          string   `json:"displayName"`
	ProjectID            string   `json:"projectId"`
	Models               []string `json:"models"`
	TermsAckVersion      string   `json:"termsAckVersion"`
	ExistingConnectionID string   `json:"existingConnectionId"`
}

type completeAIAuthorizationRequest struct {
	CallbackURL     string `json:"callbackUrl"`
	DeepSeekToken   string `json:"deepSeekToken"`
	TermsAckVersion string `json:"termsAckVersion"`
}

type disconnectAIConnectionRequest struct {
	Password string `json:"password"`
}

func (s *Server) stepUpAIConnectionAuth(w http.ResponseWriter, r *http.Request) {
	if !s.aiAuthorizationAvailable(w, r) {
		return
	}
	user, _ := s.userFromRequest(r)
	if !s.allowRate(s.authLimiter, "ai-auth-step-up:"+user.ID+":"+clientIP(r), 5, 30*time.Minute) {
		writeError(w, r, http.StatusTooManyRequests, "step_up_rate_limited", "二次验证失败次数过多，请稍后再试")
		return
	}
	var request aiAuthStepUpRequest
	if err := decodeAIConnectionJSON(w, r, &request); err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid_json", "Invalid JSON body")
		return
	}
	if err := s.auth.VerifyPassword(r.Context(), user.ID, request.Password); err != nil {
		status := http.StatusUnauthorized
		if !errors.Is(err, auth.ErrInvalidPassword) {
			status = http.StatusServiceUnavailable
		}
		writeError(w, r, status, "step_up_failed", "当前账号密码验证失败")
		return
	}
	sessionHash, ok := s.authorizationSessionHash(r)
	if !ok {
		writeError(w, r, http.StatusUnauthorized, "unauthorized", "Login required")
		return
	}
	token, err := connections.GenerateOpaqueToken("step_")
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "step_up_failed", "Could not create a verification grant")
		return
	}
	expiresAt := time.Now().Add(10 * time.Minute)
	if err := s.authzStore.PutStepUp(r.Context(), connections.StepUpGrant{
		Token: token, UserID: user.ID, SessionHash: sessionHash, ExpiresAt: expiresAt,
	}); err != nil {
		writeError(w, r, http.StatusServiceUnavailable, "authorization_store_unavailable", "Authorization service is temporarily unavailable")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"grant": token, "expiresAt": expiresAt})
}

func (s *Server) startAIConnectionAuthorization(w http.ResponseWriter, r *http.Request) {
	if !s.aiAuthorizationAvailable(w, r) {
		return
	}
	user, _ := s.userFromRequest(r)
	orgID, ok := s.personalAIConnectionWorkspace(w, r, user)
	if !ok {
		return
	}
	if !s.allowRate(s.authLimiter, "ai-auth-start:"+user.ID+":"+clientIP(r), 8, time.Hour) {
		writeError(w, r, http.StatusTooManyRequests, "authorization_rate_limited", "授权尝试过于频繁，请稍后再试")
		return
	}
	var request startAIAuthorizationRequest
	if err := decodeAIConnectionJSON(w, r, &request); err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid_json", "Invalid JSON body")
		return
	}
	if request.ExistingConnectionID == "" {
		request.ExistingConnectionID = chi.URLParam(r, "connectionID")
	}
	provider := strings.ToLower(strings.TrimSpace(request.Provider))
	method := strings.ToLower(strings.TrimSpace(request.Method))
	adapter, ok := s.authRegistry.Adapter(provider, method)
	if !ok {
		writeError(w, r, http.StatusNotFound, "authorization_method_unavailable", "This authorization method is disabled")
		return
	}
	sessionHash, ok := s.authorizationSessionHash(r)
	if !ok {
		writeError(w, r, http.StatusUnauthorized, "unauthorized", "Login required")
		return
	}
	resolved, err := connections.ResolveProvider(connections.ResolveProviderInput{Code: provider})
	if err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid_provider_profile", "Provider profile is invalid")
		return
	}
	models := request.Models
	if len(models) == 0 {
		models = resolved.Manifest.RecommendedModels
	}
	models, err = normalizeConnectionModels(models)
	if err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid_models", err.Error())
		return
	}
	if method == "codex_oauth" && strings.TrimSpace(request.TermsAckVersion) != "chatgpt-codex-experimental-v1" {
		writeError(w, r, http.StatusUnprocessableEntity, "experimental_terms_required", "请确认 ChatGPT Codex 实验功能风险说明")
		return
	}
	if method == "deepseek_web_token" && strings.TrimSpace(request.TermsAckVersion) != connections.DeepSeekWebTermsVersion {
		writeError(w, r, http.StatusUnprocessableEntity, "experimental_terms_required", "请确认 DeepSeek 网页账号实验功能风险说明")
		return
	}
	if request.ExistingConnectionID != "" {
		existing, existingErr := s.repo.AIConnectionForOwnerOrg(r.Context(), user.ID, orgID, request.ExistingConnectionID)
		if existingErr != nil || existing.Provider != provider || existing.AuthMethod != method {
			writeError(w, r, http.StatusBadRequest, "reauthorization_target_invalid", "The connection cannot be reauthorized with this method")
			return
		}
		models = connectionModelIDs(existing.Models)
		request.DisplayName = existing.DisplayName
		if provider == "gemini" && method == "oauth" && strings.TrimSpace(request.ProjectID) == "" {
			request.ProjectID, _ = existing.ProviderConfig["projectId"].(string)
		}
	}
	if provider == "gemini" && method == "oauth" {
		projectID := strings.TrimSpace(request.ProjectID)
		if projectID == "" {
			projectID = strings.TrimSpace(s.cfg.GoogleOAuthProjectID)
		}
		projectID, err = connections.NormalizeGoogleCloudProjectID(projectID)
		if err != nil {
			writeError(w, r, http.StatusBadRequest, "invalid_google_project_id", "请输入有效的 Google Cloud Project ID（6–30 位小写字母、数字或连字符）")
			return
		}
		request.ProjectID = projectID
	}
	if requiresAIConnectionAuthorizationStartStepUp(method) {
		if err := s.authzStore.ConsumeStepUp(r.Context(), request.StepUpGrant, user.ID, sessionHash); err != nil {
			writeError(w, r, http.StatusUnauthorized, "step_up_required", "请重新输入 TokHub 密码完成二次验证")
			return
		}
	}
	transactionID := "authz_" + uuid.NewString()
	proof, err := connections.GenerateOAuthProof()
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "authorization_start_failed", "Could not start authorization")
		return
	}
	expiresAt := time.Now().Add(s.cfg.AIOAuthTTL)
	redirectURI := ""
	switch method {
	case "oauth":
		redirectURI = strings.TrimRight(s.cfg.PublicURL, "/") + "/api/me/ai-authorizations/google/callback"
	case "codex_oauth":
		redirectURI = connections.CodexOAuthRedirectURI
	}
	transaction := connections.AuthorizationTransaction{
		ID: transactionID, UserID: user.ID, OrgID: orgID, SessionHash: sessionHash,
		Provider: provider, Method: method, State: transactionID + "." + proof.State,
		CodeVerifier: proof.CodeVerifier, Nonce: proof.Nonce, RedirectURI: redirectURI,
		DisplayName: cleanConnectionDisplayName(request.DisplayName, resolved.Manifest.Name),
		ProjectID:   strings.TrimSpace(request.ProjectID), Models: models,
		TermsVersion: strings.TrimSpace(request.TermsAckVersion),
		ExistingID:   request.ExistingConnectionID, CreatedAt: time.Now(), ExpiresAt: expiresAt,
	}
	start, err := adapter.Start(r.Context(), transaction, proof.CodeChallenge)
	if err != nil {
		writeError(w, r, http.StatusServiceUnavailable, "authorization_start_failed", "Provider authorization is unavailable")
		return
	}
	if _, err := s.repo.CreateAIAuthorizationAttempt(r.Context(), store.AIAuthorizationAttemptInput{
		ID: transaction.ID, OwnerUserID: user.ID, OrgID: orgID, Provider: provider,
		AuthMethod: method, CompletionMode: start.CompletionMode, ExpiresAt: expiresAt,
	}); err != nil {
		writeError(w, r, http.StatusInternalServerError, "authorization_start_failed", "Could not record authorization attempt")
		return
	}
	if err := s.authzStore.Put(r.Context(), transaction); err != nil {
		_ = s.repo.FailAIAuthorizationAttempt(r.Context(), user.ID, transaction.ID, "authorization_store_unavailable", "Authorization transaction store unavailable")
		writeError(w, r, http.StatusServiceUnavailable, "authorization_store_unavailable", "Authorization service is temporarily unavailable")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"id": transaction.ID, "authorizationUrl": start.AuthorizationURL,
		"completionMode": start.CompletionMode, "expiresAt": expiresAt, "pollIntervalMs": 1500,
	})
}

func (s *Server) aiConnectionAuthorizationStatus(w http.ResponseWriter, r *http.Request) {
	user, _ := s.userFromRequest(r)
	attempt, err := s.repo.AIAuthorizationAttemptForOwner(r.Context(), user.ID, chi.URLParam(r, "authorizationID"))
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, r, http.StatusNotFound, "authorization_not_found", "Authorization attempt was not found")
		return
	}
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "authorization_unavailable", "Could not load authorization status")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"authorization": attempt})
}

func (s *Server) completeAIConnectionAuthorization(w http.ResponseWriter, r *http.Request) {
	if !s.aiAuthorizationAvailable(w, r) {
		return
	}
	var request completeAIAuthorizationRequest
	if err := decodeAIConnectionJSON(w, r, &request); err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid_json", "Invalid JSON body")
		return
	}
	authorizationID := chi.URLParam(r, "authorizationID")
	user, _ := s.userFromRequest(r)
	if !s.allowRate(s.authLimiter, "ai-auth-complete:"+user.ID+":"+clientIP(r), 8, time.Hour) {
		writeError(w, r, http.StatusTooManyRequests, "authorization_rate_limited", "授权提交过于频繁，请稍后再试")
		return
	}
	sessionHash, ok := s.authorizationSessionHash(r)
	if !ok {
		writeError(w, r, http.StatusUnauthorized, "unauthorized", "Login required")
		return
	}
	pending, err := s.authzStore.Get(r.Context(), authorizationID)
	if err != nil ||
		!connections.SecureStateEqual(pending.UserID, user.ID) ||
		!connections.SecureStateEqual(pending.SessionHash, sessionHash) {
		writeError(w, r, http.StatusConflict, "authorization_expired", "Authorization expired or was already used")
		return
	}
	code := ""
	switch {
	case pending.Provider == "openai" && pending.Method == "codex_oauth":
		var state string
		code, state, err = connections.ParseCodexCallback(strings.TrimSpace(request.CallbackURL))
		if err != nil {
			writeError(w, r, http.StatusBadRequest, "invalid_callback_url", "请粘贴完整的 localhost Codex 回调地址")
			return
		}
		stateID, stateErr := connections.AuthorizationIDFromState(state)
		if stateErr != nil || stateID != authorizationID || !connections.SecureStateEqual(pending.State, state) {
			writeError(w, r, http.StatusConflict, "authorization_state_mismatch", "Authorization state does not match")
			return
		}
	case pending.Provider == "deepseek" && pending.Method == "deepseek_web_token":
		if pending.TermsVersion != connections.DeepSeekWebTermsVersion ||
			strings.TrimSpace(request.TermsAckVersion) != connections.DeepSeekWebTermsVersion {
			writeError(w, r, http.StatusUnprocessableEntity, "experimental_terms_required", "请确认 DeepSeek 网页账号实验功能风险说明")
			return
		}
		code, err = connections.NormalizeDeepSeekWebToken(request.DeepSeekToken)
		if err != nil {
			writeError(w, r, http.StatusBadRequest, "invalid_deepseek_token", "请粘贴 DeepSeek userToken 的 value，不要粘贴 Cookie、密码或完整存储对象")
			return
		}
	default:
		writeError(w, r, http.StatusBadRequest, "authorization_completion_invalid", "This authorization method cannot be completed here")
		return
	}
	transaction, err := s.authzStore.Consume(r.Context(), authorizationID, user.ID, sessionHash)
	if err != nil || transaction.Provider != pending.Provider || transaction.Method != pending.Method {
		writeError(w, r, http.StatusConflict, "authorization_expired", "Authorization expired or was already used")
		return
	}
	connection, err := s.finishAIConnectionAuthorization(r.Context(), transaction, code)
	if err != nil {
		if transaction.Method == "deepseek_web_token" {
			status := http.StatusBadGateway
			message := "DeepSeek 登录态验证失败，请重新登录 DeepSeek 后复制新的 userToken"
			if errors.Is(err, connections.ErrCredentialTemporary) {
				status = http.StatusServiceUnavailable
				message = "DeepSeek 网页协议桥暂时不可用，请稍后重试"
			}
			if errors.Is(err, connections.ErrCredentialRejected) {
				message = "DeepSeek 网页接口拒绝了验证请求，请检查模型 ID，或稍后重试"
			}
			writeError(w, r, status, "deepseek_web_authorization_failed", message)
			return
		}
		status, code, message := managedAuthorizationFailureResponse("ChatGPT", err)
		writeError(w, r, status, code, message)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"connection": connection, "authorizationId": authorizationID})
}

func requiresAIConnectionAuthorizationStartStepUp(method string) bool {
	return method != connections.AuthModeDeepSeekWeb
}

func (s *Server) googleAIAuthorizationCallback(w http.ResponseWriter, r *http.Request) {
	if !s.aiAuthorizationAvailable(w, r) {
		return
	}
	state := strings.TrimSpace(r.URL.Query().Get("state"))
	authorizationID, err := connections.AuthorizationIDFromState(state)
	if err != nil {
		s.writeAuthorizationCallbackPage(w, "failed", "", "授权状态无效")
		return
	}
	user, userErr := s.userFromRequest(r)
	if userErr != nil {
		s.writeAuthorizationCallbackPage(w, "failed", authorizationID, "TokHub 登录已失效")
		return
	}
	sessionHash, sessionOK := s.authorizationSessionHash(r)
	if !sessionOK {
		s.writeAuthorizationCallbackPage(w, "failed", authorizationID, "TokHub 登录已失效")
		return
	}
	transaction, err := s.authzStore.Consume(r.Context(), authorizationID, user.ID, sessionHash)
	if err != nil || transaction.Provider != "gemini" || transaction.Method != "oauth" ||
		!connections.SecureStateEqual(transaction.State, state) {
		s.writeAuthorizationCallbackPage(w, "failed", authorizationID, "授权已过期或已使用")
		return
	}
	if providerError := strings.TrimSpace(r.URL.Query().Get("error")); providerError != "" {
		_ = s.repo.FailAIAuthorizationAttempt(r.Context(), user.ID, authorizationID, "provider_denied", "Google authorization was not granted")
		s.writeAuthorizationCallbackPage(w, "failed", authorizationID, "Google 授权未完成")
		return
	}
	code := strings.TrimSpace(r.URL.Query().Get("code"))
	if code == "" {
		_ = s.repo.FailAIAuthorizationAttempt(r.Context(), user.ID, authorizationID, "code_missing", "Authorization code was missing")
		s.writeAuthorizationCallbackPage(w, "failed", authorizationID, "授权结果缺少必要信息")
		return
	}
	connection, err := s.finishAIConnectionAuthorization(r.Context(), transaction, code)
	if err != nil {
		_, _, message := managedAuthorizationFailureResponse("Gemini", err)
		s.writeAuthorizationCallbackPage(w, "failed", authorizationID, message)
		return
	}
	s.writeAuthorizationCallbackPage(w, "completed", authorizationID, connection.DisplayName+" 已连接")
}

func (s *Server) cancelAIConnectionAuthorization(w http.ResponseWriter, r *http.Request) {
	if !s.aiAuthorizationAvailable(w, r) {
		return
	}
	user, _ := s.userFromRequest(r)
	sessionHash, ok := s.authorizationSessionHash(r)
	if !ok {
		writeError(w, r, http.StatusUnauthorized, "unauthorized", "Login required")
		return
	}
	id := chi.URLParam(r, "authorizationID")
	if err := s.authzStore.Delete(r.Context(), id, user.ID, sessionHash); err != nil {
		writeError(w, r, http.StatusConflict, "authorization_not_pending", "Authorization is no longer pending")
		return
	}
	if err := s.repo.CancelAIAuthorizationAttempt(r.Context(), user.ID, id); err != nil {
		writeError(w, r, http.StatusConflict, "authorization_not_pending", "Authorization is no longer pending")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) disconnectAIConnection(w http.ResponseWriter, r *http.Request) {
	var request disconnectAIConnectionRequest
	if err := decodeAIConnectionJSON(w, r, &request); err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid_json", "Invalid JSON body")
		return
	}
	user, _ := s.userFromRequest(r)
	if !s.verifyInteractiveAdminPassword(w, r, user, request.Password, "ai-connection-disconnect") {
		return
	}
	s.deleteAIConnectionRecord(w, r, true)
}

func (s *Server) finishAIConnectionAuthorization(ctx context.Context, transaction connections.AuthorizationTransaction, code string) (store.AIConnection, error) {
	if err := s.repo.SetAIAuthorizationValidating(ctx, transaction.UserID, transaction.ID); err != nil {
		return store.AIConnection{}, err
	}
	adapter, ok := s.authRegistry.Adapter(transaction.Provider, transaction.Method)
	if !ok {
		_ = s.repo.FailAIAuthorizationAttempt(ctx, transaction.UserID, transaction.ID, "adapter_disabled", "Authorization adapter is disabled")
		return store.AIConnection{}, connections.ErrAdapterDisabled
	}
	bundle, profile, err := adapter.Exchange(ctx, transaction, code)
	if err != nil {
		_ = s.repo.FailAIAuthorizationAttempt(ctx, transaction.UserID, transaction.ID, authorizationErrorCode(err), "Provider token exchange failed")
		return store.AIConnection{}, err
	}
	if err := s.verifyReauthorizationIdentity(ctx, transaction, &bundle); err != nil {
		message := "Stored connection identity could not be verified"
		if errors.Is(err, connections.ErrCredentialIdentityMismatch) {
			message = "Reauthorization identity did not match the existing connection"
		}
		_ = s.repo.FailAIAuthorizationAttempt(ctx, transaction.UserID, transaction.ID, authorizationErrorCode(err), message)
		return store.AIConnection{}, err
	}
	material, err := adapter.ResolveAuthMaterial(ctx, bundle)
	if err != nil {
		_ = s.repo.FailAIAuthorizationAttempt(ctx, transaction.UserID, transaction.ID, authorizationErrorCode(err), "Provider credential could not be resolved")
		return store.AIConnection{}, err
	}
	resolved, err := connections.ResolveProvider(connections.ResolveProviderInput{Code: transaction.Provider})
	if err != nil {
		return store.AIConnection{}, err
	}
	resolved.Endpoint = material.Endpoint
	if transaction.Method == "codex_oauth" {
		resolved.Manifest.ValidationMode = "generation"
		resolved.Manifest.GenerationKind = "responses"
		resolved.Manifest.ProductLine = "ChatGPT Codex"
		resolved.ProviderConfig["pathMode"] = "direct"
		resolved.ProviderConfig["experimental"] = true
	}
	if transaction.Method == "deepseek_web_token" {
		resolved.Manifest.ValidationMode = "generation"
		resolved.Manifest.GenerationKind = "chat"
		resolved.Manifest.ProductLine = "DeepSeek Web"
		delete(resolved.ProviderConfig, "pathMode")
		resolved.ProviderConfig["experimental"] = true
		resolved.ProviderConfig["bridge"] = "ds2api"
		resolved.ProviderConfig["bridgeVersion"] = connections.DeepSeekWebAdapterVersion()
	}
	resolved.ProviderConfig["authMethod"] = transaction.Method
	resolved.ProviderConfig["sharingScope"] = "personal"
	if strings.TrimSpace(bundle.ProjectID) != "" {
		resolved.ProviderConfig["projectId"] = strings.TrimSpace(bundle.ProjectID)
	}
	validation := s.validateAuthorizedCredentialSet(ctx, resolved, transaction.Models, material)
	if !validation.OK {
		validationErr := authorizedValidationCredentialError(validation.ErrorType)
		_ = s.repo.FailAIAuthorizationAttempt(
			ctx,
			transaction.UserID,
			transaction.ID,
			authorizationErrorCode(validationErr),
			"Authorized provider credential validation failed",
		)
		return store.AIConnection{}, validationErr
	}
	rawBundle, err := bundle.Marshal()
	if err != nil {
		return store.AIConnection{}, err
	}
	fingerprintSource := strings.Join([]string{
		transaction.Method, bundle.ProviderSubject, bundle.AccountID,
	}, "\x00")
	encrypted, err := s.credentialKeys.EncryptWithFingerprint(transaction.UserID, transaction.Provider, rawBundle, fingerprintSource)
	if err != nil {
		return store.AIConnection{}, err
	}
	accountMask := strings.TrimSpace(profile.EmailMask)
	if accountMask == "" {
		accountMask = "已授权账号"
	}
	credentialMaskPrefix := "OAuth · "
	if transaction.Method == "deepseek_web_token" {
		credentialMaskPrefix = "Web Session · "
	}
	encrypted.Mask = credentialMaskPrefix + accountMask
	var expiresAt *time.Time
	var nextRefresh *time.Time
	if !bundle.ExpiresAt.IsZero() {
		expiry := bundle.ExpiresAt
		expiresAt = &expiry
		if transaction.Method != "deepseek_web_token" {
			refreshAt := bundle.ExpiresAt.Add(-s.cfg.AIOAuthRefreshSkew)
			if refreshAt.Before(time.Now().Add(time.Minute)) {
				refreshAt = time.Now().Add(time.Minute)
			}
			nextRefresh = &refreshAt
		}
	}
	riskLevel := "standard"
	adapterVersion := "gemini-oauth-v1"
	if transaction.Method == "codex_oauth" {
		riskLevel = "experimental"
		adapterVersion = "chatgpt-codex-v1"
	}
	if transaction.Method == "deepseek_web_token" {
		riskLevel = "experimental"
		adapterVersion = connections.DeepSeekWebAdapterVersion()
	}
	connectionInput := store.AIConnectionCreateInput{
		OwnerUserID: transaction.UserID, OrgID: transaction.OrgID,
		Provider: transaction.Provider, ProductLine: resolved.Manifest.ProductLine,
		Region: resolved.Region, Protocol: resolved.Manifest.Protocol,
		AdapterType: resolved.Manifest.Type, Endpoint: resolved.Endpoint,
		ProviderConfig: resolved.ProviderConfig, DisplayName: transaction.DisplayName,
		Models: transaction.Models,
		Credential: store.AIConnectionSecret{
			Ciphertext: encrypted.Ciphertext, Nonce: encrypted.Nonce, Mask: encrypted.Mask,
			Fingerprint: encrypted.Fingerprint, EncryptionKeyID: encrypted.EncryptionKeyID,
			FingerprintKeyID: encrypted.FingerprintKeyID, Algorithm: encrypted.Algorithm,
			SecretType: "oauth_bundle", PayloadFormat: connections.CredentialBundleSchemaV1,
			SubjectFingerprint: encrypted.Fingerprint, ExpiresAt: expiresAt,
			NextRefreshAt: nextRefresh,
		},
		Validation: validationStoreValue(validation), AuthMethod: transaction.Method,
		AuthStatus:   "active",
		SharingScope: "personal", RiskLevel: riskLevel,
		ProviderAdapterVersion: adapterVersion, TermsAckVersion: transaction.TermsVersion,
		AccountMask: accountMask, AuthorizationID: transaction.ID,
	}
	var connection store.AIConnection
	if transaction.ExistingID != "" {
		connection, err = s.repo.ReplaceOAuthAIConnectionAuthorization(ctx, transaction.ExistingID, connectionInput)
	} else {
		connection, err = s.repo.CreateAIConnection(ctx, connectionInput)
	}
	if err != nil {
		_ = s.repo.FailAIAuthorizationAttempt(ctx, transaction.UserID, transaction.ID, "connection_create_failed", "Authorized connection could not be saved")
		return store.AIConnection{}, err
	}
	return connection, nil
}

func (s *Server) verifyReauthorizationIdentity(
	ctx context.Context,
	transaction connections.AuthorizationTransaction,
	replacement *connections.CredentialBundle,
) error {
	if strings.TrimSpace(transaction.ExistingID) == "" {
		return nil
	}
	if s.credentialKeys == nil {
		return fmt.Errorf("credential keyring is unavailable")
	}
	secret, err := s.repo.AIConnectionSecretForOwnerOrg(
		ctx,
		transaction.UserID,
		transaction.OrgID,
		transaction.ExistingID,
	)
	if err != nil {
		return fmt.Errorf("load existing credential identity: %w", err)
	}
	if !strings.EqualFold(strings.TrimSpace(secret.Provider), strings.TrimSpace(transaction.Provider)) ||
		secret.SecretType != "oauth_bundle" ||
		secret.PayloadFormat != connections.CredentialBundleSchemaV1 {
		return fmt.Errorf("existing credential identity format is invalid")
	}
	raw, err := s.credentialKeys.Decrypt(transaction.UserID, secret.Provider, secretcrypto.CredentialEnvelope{
		Ciphertext: secret.Ciphertext, Nonce: secret.Nonce,
		EncryptionKeyID: secret.EncryptionKeyID, Fingerprint: secret.Fingerprint,
		FingerprintKeyID: secret.FingerprintKeyID, Mask: secret.Mask, Algorithm: secret.Algorithm,
	})
	if err != nil {
		return fmt.Errorf("decrypt existing credential identity: %w", err)
	}
	current, err := connections.ParseCredentialBundle(raw)
	if err != nil {
		return fmt.Errorf("parse existing credential identity: %w", err)
	}
	if replacement == nil || !connections.SameCredentialIdentity(current, *replacement) {
		return connections.ErrCredentialIdentityMismatch
	}
	if strings.TrimSpace(transaction.ProjectID) == "" && strings.TrimSpace(current.ProjectID) != "" {
		replacement.ProjectID = current.ProjectID
	}
	return nil
}

func (s *Server) validateAuthorizedCredentialSet(ctx context.Context, resolved connections.ResolvedProvider, models []string, material connections.AuthMaterial) connectionValidationResult {
	if len(models) == 0 {
		return connectionValidationResult{
			Provider: resolved.Manifest.Name, Type: resolved.Manifest.Type, Endpoint: resolved.Endpoint,
			Stage: "models", ErrorType: "models_missing", Message: "连接没有可验证的模型",
		}
	}
	if s.upstreamClient == nil {
		return connectionValidationResult{
			Provider: resolved.Manifest.Name, Type: resolved.Manifest.Type, Endpoint: resolved.Endpoint,
			Model: models[0], Stage: "client", ErrorType: "validation_client_unavailable",
			Message: "连接验证服务暂时不可用，请稍后重试",
			Models: failedModelValidationResults(models, connectionValidationResult{
				ErrorType: "validation_client_unavailable", Message: "连接验证服务暂时不可用，请稍后重试",
			}),
		}
	}
	ctx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	start := time.Now()
	upstream := gatewaycache.Upstream{
		Name: resolved.Manifest.Name, Provider: resolved.Manifest.Code, Type: resolved.Manifest.Type,
		Endpoint: resolved.Endpoint, Model: models[0], ProviderConfig: resolved.ProviderConfig,
	}
	if resolved.Manifest.ValidationMode == "models_then_generation" {
		discovery, err := s.upstreamClient.ModelsWithAuth(ctx, upstream, material, false)
		if err != nil {
			return connectionValidationResult{
				OK: false, Stage: "models", LatencyMs: int(time.Since(start).Milliseconds()),
				ErrorType: nonEmpty(discovery.ErrorType, "upstream_validation_failed"),
				Message:   "Provider model discovery failed",
				Models:    failedModelValidationResults(models, connectionValidationResult{ErrorType: nonEmpty(discovery.ErrorType, "upstream_validation_failed"), Message: "Provider model discovery failed"}),
			}
		}
	}
	results := make([]connectionModelValidationResult, 0, len(models))
	allOK := true
	firstFailure := connectionValidationResult{}
	for _, model := range models {
		modelStart := time.Now()
		upstream.Model = model
		raw := officialValidationPayload(resolved.Manifest.GenerationKind, model)
		result, err := s.upstreamClient.JSONWithAuth(ctx, upstream, material, resolved.Manifest.GenerationKind, raw, gatewaycache.UpstreamUsage{
			PromptTokens: 4, CompletionTokens: 2, TotalTokens: 6, Estimated: true,
		})
		modelResult := connectionModelValidationResult{
			Model: model, OK: err == nil, LatencyMs: int(time.Since(modelStart).Milliseconds()),
		}
		if err != nil {
			allOK = false
			modelResult.ErrorType = nonEmpty(result.ErrorType, "upstream_validation_failed")
			modelResult.Message = "Provider generation validation failed"
			if firstFailure.ErrorType == "" {
				firstFailure = connectionValidationResult{
					Model: model, Stage: "generation", ErrorType: modelResult.ErrorType, Message: modelResult.Message,
				}
			}
		}
		results = append(results, modelResult)
	}
	return connectionValidationResult{
		OK: allOK, Model: firstFailure.Model, Stage: "generation",
		LatencyMs: int(time.Since(start).Milliseconds()), ErrorType: firstFailure.ErrorType,
		Message: firstFailure.Message, ModelCount: len(models), Models: results,
	}
}

func (s *Server) authorizationSessionHash(r *http.Request) (string, bool) {
	cookie, err := r.Cookie(auth.CookieName)
	if err != nil || strings.TrimSpace(cookie.Value) == "" {
		return "", false
	}
	return store.HashOpaqueToken(cookie.Value), true
}

func (s *Server) aiAuthorizationAvailable(w http.ResponseWriter, r *http.Request) bool {
	if !s.cfg.AIWebAuthEnabled || s.authRegistry == nil {
		writeError(w, r, http.StatusNotFound, "ai_web_auth_disabled", "AI web authorization is disabled")
		return false
	}
	if s.authzStore == nil || s.credentialKeys == nil {
		writeError(w, r, http.StatusServiceUnavailable, "authorization_store_unavailable", "Authorization service is temporarily unavailable")
		return false
	}
	return true
}

func authorizationErrorCode(err error) string {
	switch {
	case errors.Is(err, connections.ErrCredentialIdentityMismatch):
		return "identity_mismatch"
	case errors.Is(err, connections.ErrCredentialReauth):
		return "reauth_required"
	case errors.Is(err, connections.ErrCredentialTemporary):
		return "provider_temporary"
	case errors.Is(err, connections.ErrCredentialRejected):
		return "provider_rejected"
	case errors.Is(err, connections.ErrAdapterDisabled):
		return "adapter_disabled"
	default:
		return "authorization_failed"
	}
}

func authorizedValidationCredentialError(errorType string) error {
	switch strings.TrimSpace(errorType) {
	case "upstream_auth_error":
		return connections.ErrCredentialReauth
	case "upstream_rejected":
		return connections.ErrCredentialRejected
	default:
		return connections.ErrCredentialTemporary
	}
}

func managedAuthorizationFailureResponse(provider string, err error) (int, string, string) {
	switch {
	case errors.Is(err, connections.ErrCredentialReauth):
		return http.StatusUnauthorized, "provider_reauthorization_required", provider + " 授权已失效，请重新登录后再试"
	case errors.Is(err, connections.ErrCredentialRejected):
		return http.StatusUnprocessableEntity, "provider_validation_rejected", provider + " 已拒绝模型验证，请检查账号权限和模型 ID"
	case errors.Is(err, connections.ErrCredentialTemporary):
		return http.StatusServiceUnavailable, "provider_temporarily_unavailable", provider + " 授权或模型验证暂时不可用，请稍后重试"
	default:
		return http.StatusBadGateway, "authorization_exchange_failed", provider + " 授权验证未完成，请重新发起"
	}
}

func (s *Server) writeAuthorizationCallbackPage(w http.ResponseWriter, status string, authorizationID string, message string) {
	redirect := "/console/connections"
	if authorizationID != "" {
		redirect += "?authorization=" + url.QueryEscape(status) + "&id=" + url.QueryEscape(authorizationID)
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; script-src 'unsafe-inline'; base-uri 'none'; frame-ancestors 'none'")
	_, _ = fmt.Fprintf(w, `<!doctype html><html lang="zh-CN"><meta charset="utf-8"><title>TokHub 授权结果</title><style>body{font-family:system-ui,sans-serif;background:#f6f7fb;color:#172033;display:grid;place-items:center;min-height:100vh;margin:0}.card{max-width:420px;background:#fff;border:1px solid #dfe3eb;border-radius:16px;padding:32px;box-shadow:0 16px 50px rgba(28,43,76,.1)}a{color:#2959c8}</style><body><main class="card"><h1>%s</h1><p>%s</p><p><a href="%s">返回 TokHub</a></p></main><script>if(window.opener){window.opener.postMessage({type:"tokhub:ai-authorization",status:%q,id:%q},window.location.origin);window.close()}</script></body></html>`,
		map[bool]string{true: "授权完成", false: "授权未完成"}[status == "completed"],
		html.EscapeString(message), html.EscapeString(redirect), status, authorizationID)
}
