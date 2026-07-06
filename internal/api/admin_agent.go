package api

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"tokhub/internal/store"
)

type adminAgentContextKey struct{}

type createAdminAgentTokenRequest struct {
	Name      string   `json:"name"`
	Scopes    []string `json:"scopes"`
	ExpiresAt string   `json:"expiresAt"`
	TTLHours  int      `json:"ttlHours"`
}

func isAdminAgentAuthorization(r *http.Request) bool {
	return bearerTokenFromRequest(r) != ""
}

func bearerTokenFromRequest(r *http.Request) string {
	header := strings.TrimSpace(r.Header.Get("Authorization"))
	if len(header) < len("Bearer ") || !strings.EqualFold(header[:len("Bearer ")], "Bearer ") {
		return ""
	}
	return strings.TrimSpace(header[len("Bearer "):])
}

func adminAgentFromContext(ctx context.Context) (store.AuthenticatedAdminAgent, bool) {
	authn, ok := ctx.Value(adminAgentContextKey{}).(store.AuthenticatedAdminAgent)
	return authn, ok && authn.Token.ID != ""
}

func withAdminAgent(r *http.Request, authn store.AuthenticatedAdminAgent, reason string, idempotencyKey string) *http.Request {
	ctx := context.WithValue(r.Context(), adminAgentContextKey{}, authn)
	ctx = store.WithAgentAudit(ctx, store.AgentAuditContext{
		TokenID:            authn.Token.ID,
		TokenName:          authn.Token.Name,
		DelegatedUserID:    authn.User.ID,
		DelegatedUserEmail: authn.User.Email,
		Reason:             reason,
		IdempotencyKey:     idempotencyKey,
	})
	return r.WithContext(ctx)
}

func (s *Server) authenticateAdminAgent(w http.ResponseWriter, r *http.Request, next http.Handler) {
	if !s.cfg.AdminAgentEnabled {
		writeError(w, r, http.StatusForbidden, "admin_agent_disabled", "Admin agent access is disabled")
		return
	}
	if strings.HasPrefix(r.URL.Path, "/api/admin/agent-tokens") {
		writeError(w, r, http.StatusForbidden, "admin_agent_chaining_forbidden", "Admin agent tokens cannot manage admin agent tokens")
		return
	}
	if s.repo == nil {
		writeError(w, r, http.StatusServiceUnavailable, "admin_agent_unavailable", "Admin agent authentication is unavailable")
		return
	}
	authn, err := s.repo.AuthenticateAdminAgentToken(r.Context(), bearerTokenFromRequest(r))
	if err != nil {
		writeError(w, r, http.StatusUnauthorized, "admin_agent_unauthorized", "Admin agent token is invalid or expired")
		return
	}
	requiredScopes := adminAgentRequiredScopes(r.Method, r.URL.Path)
	if missing := missingAdminAgentScopes(authn.Token.Scopes, requiredScopes); len(missing) > 0 {
		writeError(w, r, http.StatusForbidden, "admin_agent_scope_forbidden", "Admin agent token does not allow this operation")
		return
	}
	reason := strings.TrimSpace(r.Header.Get("X-TokHub-Agent-Reason"))
	idempotencyKey := strings.TrimSpace(r.Header.Get("X-Idempotency-Key"))
	if adminAgentNeedsWriteGuard(r.Method, r.URL.Path) {
		if len([]rune(reason)) < 3 || len([]rune(reason)) > 240 {
			writeError(w, r, http.StatusBadRequest, "admin_agent_reason_required", "X-TokHub-Agent-Reason must be 3-240 characters")
			return
		}
		if len(idempotencyKey) < 8 || len(idempotencyKey) > 120 || strings.ContainsAny(idempotencyKey, " \t\r\n") {
			writeError(w, r, http.StatusBadRequest, "admin_agent_idempotency_required", "X-Idempotency-Key must be 8-120 non-space characters")
			return
		}
		if err := s.repo.RecordAdminAgentIdempotencyKey(r.Context(), authn.Token.ID, idempotencyKey, r.Method, r.URL.Path); errors.Is(err, store.ErrAdminAgentIdempotencyConflict) {
			writeError(w, r, http.StatusConflict, "admin_agent_idempotency_conflict", "X-Idempotency-Key was already used")
			return
		} else if err != nil {
			writeError(w, r, http.StatusInternalServerError, "admin_agent_idempotency_failed", "Could not record idempotency key")
			return
		}
	}
	next.ServeHTTP(w, withAdminAgent(r, authn, reason, idempotencyKey))
}

func adminAgentRequiredScopes(method string, path string) []string {
	required := map[string]bool{}
	if isReadMethod(method) {
		required["admin:read"] = true
	} else {
		required["admin:write"] = true
	}
	if adminAgentExportPath(path) {
		required["admin:read"] = true
		required["admin:export"] = true
	}
	if adminAgentDangerousPath(method, path) {
		required["admin:dangerous"] = true
	}
	if adminAgentSecretsPath(method, path) {
		required["admin:secrets"] = true
	}
	ordered := []string{"admin:read", "admin:write", "admin:dangerous", "admin:secrets", "admin:export"}
	out := make([]string, 0, len(required))
	for _, scope := range ordered {
		if required[scope] {
			out = append(out, scope)
		}
	}
	return out
}

func isReadMethod(method string) bool {
	switch strings.ToUpper(strings.TrimSpace(method)) {
	case http.MethodGet, http.MethodHead:
		return true
	default:
		return false
	}
}

func adminAgentNeedsWriteGuard(method string, path string) bool {
	return !isReadMethod(method) || strings.Contains(path, "/channel-sites/") && strings.HasSuffix(path, "/download")
}

func adminAgentExportPath(path string) bool {
	return strings.HasSuffix(path, "/audit/export") ||
		strings.HasSuffix(path, "/usage/export") ||
		strings.HasSuffix(path, "/channels/export") ||
		strings.Contains(path, "/channel-sites/") && strings.HasSuffix(path, "/download")
}

func adminAgentDangerousPath(method string, path string) bool {
	if strings.EqualFold(method, http.MethodDelete) {
		return true
	}
	return strings.Contains(path, "/bulk") ||
		strings.HasSuffix(path, "/revoke") ||
		strings.HasSuffix(path, "/disable") ||
		strings.HasSuffix(path, "/reset") ||
		strings.HasSuffix(path, "/channels/import") ||
		strings.HasSuffix(path, "/channels/sync") ||
		strings.HasSuffix(path, "/credentials") ||
		strings.HasSuffix(path, "/rotate-key") ||
		strings.HasSuffix(path, "/build") ||
		strings.Contains(path, "/channel-sites/") && strings.HasSuffix(path, "/download")
}

func adminAgentSecretsPath(method string, path string) bool {
	if strings.EqualFold(method, http.MethodPost) && (path == "/api/admin/gateway-keys" || path == "/api/admin/open-api/sites" || path == "/api/admin/channel-sites" || path == "/api/admin/channels" || path == "/api/admin/users") {
		return true
	}
	if strings.EqualFold(method, http.MethodPatch) && strings.HasPrefix(path, "/api/admin/users/") && !strings.HasSuffix(path, "/status") {
		return true
	}
	return strings.HasSuffix(path, "/channels/export") ||
		strings.HasSuffix(path, "/channels/import") ||
		strings.HasSuffix(path, "/channels/sync") ||
		strings.HasSuffix(path, "/credentials") ||
		strings.HasSuffix(path, "/rotate-key") ||
		strings.HasSuffix(path, "/build") ||
		strings.Contains(path, "/channel-sites/") && strings.HasSuffix(path, "/download")
}

func missingAdminAgentScopes(actual []string, required []string) []string {
	out := []string{}
	for _, scope := range required {
		if !store.AdminAgentScopesContain(actual, scope) {
			out = append(out, scope)
		}
	}
	return out
}

func (s *Server) adminAgentTokens(w http.ResponseWriter, r *http.Request) {
	user, ok := s.requireOwnerForAdminAgentToken(w, r)
	if !ok {
		return
	}
	_ = user
	items, err := s.repo.ListAdminAgentTokens(r.Context())
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "admin_agent_tokens_unavailable", "Could not load admin agent tokens")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) createAdminAgentToken(w http.ResponseWriter, r *http.Request) {
	user, ok := s.requireOwnerForAdminAgentToken(w, r)
	if !ok {
		return
	}
	var req createAdminAgentTokenRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid_json", "Invalid JSON body")
		return
	}
	expiresAt, err := adminAgentTokenExpiry(req)
	if err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid_admin_agent_expiry", err.Error())
		return
	}
	item, err := s.repo.CreateAdminAgentToken(r.Context(), store.AdminAgentTokenCreateInput{
		Name:      req.Name,
		Scopes:    req.Scopes,
		ExpiresAt: expiresAt,
		ActorID:   user.ID,
	})
	if errors.Is(err, store.ErrInvalidAdminAgentTokenName) || errors.Is(err, store.ErrInvalidAdminAgentTokenScope) {
		writeError(w, r, http.StatusBadRequest, "invalid_admin_agent_token", err.Error())
		return
	}
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "admin_agent_token_create_failed", "Could not create admin agent token")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"token": item})
}

func (s *Server) revokeAdminAgentToken(w http.ResponseWriter, r *http.Request) {
	user, ok := s.requireOwnerForAdminAgentToken(w, r)
	if !ok {
		return
	}
	item, err := s.repo.RevokeAdminAgentToken(r.Context(), chi.URLParam(r, "tokenID"), user.ID)
	if err != nil {
		writeError(w, r, http.StatusNotFound, "admin_agent_token_not_found", "Admin agent token was not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"token": item})
}

func (s *Server) requireOwnerForAdminAgentToken(w http.ResponseWriter, r *http.Request) (store.PublicUser, bool) {
	if !s.cfg.AdminAgentEnabled {
		writeError(w, r, http.StatusForbidden, "admin_agent_disabled", "Admin agent access is disabled")
		return store.PublicUser{}, false
	}
	user, err := s.userFromRequest(r)
	if err != nil {
		writeError(w, r, http.StatusUnauthorized, "unauthorized", "Login required")
		return store.PublicUser{}, false
	}
	if user.Role != "owner" {
		writeError(w, r, http.StatusForbidden, "owner_required", "Owner role required")
		return store.PublicUser{}, false
	}
	return user, true
}

func adminAgentTokenExpiry(req createAdminAgentTokenRequest) (*time.Time, error) {
	if strings.TrimSpace(req.ExpiresAt) != "" {
		parsed, err := time.Parse(time.RFC3339, strings.TrimSpace(req.ExpiresAt))
		if err != nil {
			return nil, err
		}
		return &parsed, nil
	}
	if req.TTLHours > 0 {
		expiresAt := time.Now().Add(time.Duration(req.TTLHours) * time.Hour)
		return &expiresAt, nil
	}
	return nil, nil
}
