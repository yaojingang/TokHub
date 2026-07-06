package api

import (
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"tokhub/internal/auth"
	"tokhub/internal/store"
)

type statusPatchRequest struct {
	Status string `json:"status"`
}

type adminUserRequest struct {
	Email         string `json:"email"`
	Username      string `json:"username"`
	Password      string `json:"password"`
	Name          string `json:"name"`
	Role          string `json:"role"`
	Plan          string `json:"plan"`
	Status        string `json:"status"`
	EmailVerified *bool  `json:"emailVerified"`
	DataOrigin    string `json:"dataOrigin"`
}

type adminOrgRequest struct {
	Name       string `json:"name"`
	Slug       string `json:"slug"`
	Plan       string `json:"plan"`
	Status     string `json:"status"`
	Timezone   string `json:"timezone"`
	DataOrigin string `json:"dataOrigin"`
}

type adminBulkRequest struct {
	Action  string   `json:"action"`
	IDs     []string `json:"ids"`
	Status  string   `json:"status"`
	Role    string   `json:"role"`
	Plan    string   `json:"plan"`
	Enabled *bool    `json:"enabled"`
	Message string   `json:"message"`
}

type openAPISitePatchRequest struct {
	Name     string   `json:"name"`
	Scopes   []string `json:"scopes"`
	QPSLimit int      `json:"qpsLimit"`
	Status   string   `json:"status"`
}

func (s *Server) adminUsers(w http.ResponseWriter, r *http.Request) {
	result, err := s.repo.AdminUsers(r.Context(), store.AdminUserFilter{
		Query:      r.URL.Query().Get("q"),
		Status:     r.URL.Query().Get("status"),
		Role:       r.URL.Query().Get("role"),
		Plan:       r.URL.Query().Get("plan"),
		DataOrigin: r.URL.Query().Get("origin"),
	})
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "admin_users_unavailable", "Could not load users")
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) createAdminUser(w http.ResponseWriter, r *http.Request) {
	actor, _ := s.userFromRequest(r)
	var req adminUserRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid_json", "Invalid JSON body")
		return
	}
	if len(req.Password) < 8 {
		writeError(w, r, http.StatusBadRequest, "weak_password", "Password must be at least 8 characters")
		return
	}
	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "password_hash_failed", "Could not prepare password")
		return
	}
	item, err := s.repo.CreateAdminUser(r.Context(), store.AdminUserInput{
		Email: req.Email, Username: req.Username, PasswordHash: hash, Name: req.Name, Role: req.Role, Plan: req.Plan, Status: req.Status, EmailVerified: boolValue(req.EmailVerified, true), EmailVerifiedSet: true, DataOrigin: req.DataOrigin,
	}, actor.ID)
	if errors.Is(err, store.ErrInvalidAdminRole) || errors.Is(err, store.ErrInvalidAdminStatus) || errors.Is(err, store.ErrInvalidUserPlan) {
		writeError(w, r, http.StatusBadRequest, "invalid_admin_user", err.Error())
		return
	}
	if errors.Is(err, store.ErrAdminUserIdentifierExists) {
		writeError(w, r, http.StatusConflict, "admin_user_identifier_exists", "Email or username already exists")
		return
	}
	if err != nil {
		writeError(w, r, http.StatusBadRequest, "admin_user_create_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"user": item})
}

func (s *Server) patchAdminUser(w http.ResponseWriter, r *http.Request) {
	actor, _ := s.userFromRequest(r)
	var req adminUserRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid_json", "Invalid JSON body")
		return
	}
	hash := ""
	if req.Password != "" {
		if len(req.Password) < 8 {
			writeError(w, r, http.StatusBadRequest, "weak_password", "Password must be at least 8 characters")
			return
		}
		var err error
		hash, err = auth.HashPassword(req.Password)
		if err != nil {
			writeError(w, r, http.StatusInternalServerError, "password_hash_failed", "Could not prepare password")
			return
		}
	}
	item, err := s.repo.UpdateAdminUser(r.Context(), chi.URLParam(r, "userID"), actor.ID, store.AdminUserInput{
		Email: req.Email, Username: req.Username, PasswordHash: hash, Name: req.Name, Role: req.Role, Plan: req.Plan, Status: req.Status, EmailVerified: boolValue(req.EmailVerified, false), EmailVerifiedSet: req.EmailVerified != nil, DataOrigin: req.DataOrigin,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, r, http.StatusNotFound, "admin_user_not_found", "User was not found")
		return
	}
	if errors.Is(err, store.ErrInvalidAdminRole) || errors.Is(err, store.ErrInvalidAdminStatus) || errors.Is(err, store.ErrInvalidUserPlan) {
		writeError(w, r, http.StatusBadRequest, "invalid_admin_user", err.Error())
		return
	}
	if errors.Is(err, store.ErrAdminUserIdentifierExists) {
		writeError(w, r, http.StatusConflict, "admin_user_identifier_exists", "Email or username already exists")
		return
	}
	if errors.Is(err, store.ErrProtectedOwner) || errors.Is(err, store.ErrWorkspaceSelfRoleChange) {
		writeError(w, r, http.StatusBadRequest, "admin_user_protected", err.Error())
		return
	}
	if err != nil {
		writeError(w, r, http.StatusBadRequest, "admin_user_update_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"user": item})
}

func (s *Server) patchAdminUserStatus(w http.ResponseWriter, r *http.Request) {
	user, _ := s.userFromRequest(r)
	var req statusPatchRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid_json", "Invalid JSON body")
		return
	}
	item, err := s.repo.UpdateAdminUserStatus(r.Context(), chi.URLParam(r, "userID"), user.ID, req.Status)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, r, http.StatusNotFound, "admin_user_not_found", "User was not found")
		return
	}
	if errors.Is(err, store.ErrInvalidAdminStatus) {
		writeError(w, r, http.StatusBadRequest, "invalid_admin_status", err.Error())
		return
	}
	if errors.Is(err, store.ErrProtectedOwner) || errors.Is(err, store.ErrWorkspaceSelfRoleChange) {
		writeError(w, r, http.StatusBadRequest, "admin_user_protected", err.Error())
		return
	}
	if err != nil {
		writeError(w, r, http.StatusBadRequest, "admin_user_update_failed", "Could not update user status")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"user": item})
}

func (s *Server) deleteAdminUser(w http.ResponseWriter, r *http.Request) {
	actor, _ := s.userFromRequest(r)
	err := s.repo.DeleteAdminUser(r.Context(), chi.URLParam(r, "userID"), actor.ID)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, r, http.StatusNotFound, "admin_user_not_found", "User was not found")
		return
	}
	if errors.Is(err, store.ErrProtectedOwner) || errors.Is(err, store.ErrWorkspaceSelfRoleChange) {
		writeError(w, r, http.StatusBadRequest, "admin_user_protected", err.Error())
		return
	}
	if err != nil {
		writeError(w, r, http.StatusBadRequest, "admin_user_delete_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) bulkAdminUsers(w http.ResponseWriter, r *http.Request) {
	actor, _ := s.userFromRequest(r)
	var req adminBulkRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid_json", "Invalid JSON body")
		return
	}
	if !hasBulkSelection(req.IDs) {
		writeError(w, r, http.StatusBadRequest, "empty_bulk_selection", "Select at least one user")
		return
	}
	var result store.AdminUsersResult
	var err error
	switch req.Action {
	case "status":
		result, err = s.repo.BulkUpdateAdminUsers(r.Context(), req.IDs, actor.ID, req.Status)
	case "role":
		result, err = s.repo.BulkUpdateAdminUserRoles(r.Context(), req.IDs, actor.ID, req.Role)
	case "delete":
		result, err = s.repo.BulkDeleteAdminUsers(r.Context(), req.IDs, actor.ID)
	default:
		writeError(w, r, http.StatusBadRequest, "invalid_bulk_action", "Unsupported bulk action")
		return
	}
	if errors.Is(err, store.ErrInvalidAdminStatus) || errors.Is(err, store.ErrInvalidAdminRole) || errors.Is(err, store.ErrProtectedOwner) || errors.Is(err, store.ErrWorkspaceSelfRoleChange) {
		writeError(w, r, http.StatusBadRequest, "admin_user_bulk_failed", err.Error())
		return
	}
	if err != nil {
		writeError(w, r, http.StatusBadRequest, "admin_user_bulk_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) adminOrgs(w http.ResponseWriter, r *http.Request) {
	result, err := s.repo.AdminOrgs(r.Context(), store.AdminOrgFilter{
		Query:      r.URL.Query().Get("q"),
		Status:     r.URL.Query().Get("status"),
		Plan:       r.URL.Query().Get("plan"),
		DataOrigin: r.URL.Query().Get("origin"),
	})
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "admin_orgs_unavailable", "Could not load organizations")
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) createAdminOrg(w http.ResponseWriter, r *http.Request) {
	actor, _ := s.userFromRequest(r)
	var req adminOrgRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid_json", "Invalid JSON body")
		return
	}
	item, err := s.repo.CreateAdminOrg(r.Context(), store.AdminOrgInput{
		Name: req.Name, Slug: req.Slug, Plan: req.Plan, Status: req.Status, Timezone: req.Timezone, DataOrigin: req.DataOrigin,
	}, actor.ID)
	if errors.Is(err, store.ErrInvalidAdminStatus) || errors.Is(err, store.ErrInvalidAdminPlan) {
		writeError(w, r, http.StatusBadRequest, "invalid_admin_org", err.Error())
		return
	}
	if err != nil {
		writeError(w, r, http.StatusBadRequest, "admin_org_create_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"org": item})
}

func (s *Server) patchAdminOrg(w http.ResponseWriter, r *http.Request) {
	actor, _ := s.userFromRequest(r)
	var req adminOrgRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid_json", "Invalid JSON body")
		return
	}
	item, err := s.repo.UpdateAdminOrg(r.Context(), chi.URLParam(r, "orgID"), actor.ID, store.AdminOrgInput{
		Name: req.Name, Slug: req.Slug, Plan: req.Plan, Status: req.Status, Timezone: req.Timezone, DataOrigin: req.DataOrigin,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, r, http.StatusNotFound, "admin_org_not_found", "Organization was not found")
		return
	}
	if errors.Is(err, store.ErrInvalidAdminStatus) || errors.Is(err, store.ErrInvalidAdminPlan) {
		writeError(w, r, http.StatusBadRequest, "invalid_admin_org", err.Error())
		return
	}
	if errors.Is(err, store.ErrProtectedOrg) {
		writeError(w, r, http.StatusBadRequest, "admin_org_protected", err.Error())
		return
	}
	if err != nil {
		writeError(w, r, http.StatusBadRequest, "admin_org_update_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"org": item})
}

func (s *Server) patchAdminOrgStatus(w http.ResponseWriter, r *http.Request) {
	user, _ := s.userFromRequest(r)
	var req statusPatchRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid_json", "Invalid JSON body")
		return
	}
	item, err := s.repo.UpdateAdminOrgStatus(r.Context(), chi.URLParam(r, "orgID"), user.ID, req.Status)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, r, http.StatusNotFound, "admin_org_not_found", "Organization was not found")
		return
	}
	if errors.Is(err, store.ErrInvalidAdminStatus) {
		writeError(w, r, http.StatusBadRequest, "invalid_admin_status", err.Error())
		return
	}
	if errors.Is(err, store.ErrProtectedOrg) {
		writeError(w, r, http.StatusBadRequest, "admin_org_protected", err.Error())
		return
	}
	if err != nil {
		writeError(w, r, http.StatusBadRequest, "admin_org_update_failed", "Could not update organization status")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"org": item})
}

func (s *Server) deleteAdminOrg(w http.ResponseWriter, r *http.Request) {
	actor, _ := s.userFromRequest(r)
	err := s.repo.DeleteAdminOrg(r.Context(), chi.URLParam(r, "orgID"), actor.ID)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, r, http.StatusNotFound, "admin_org_not_found", "Organization was not found")
		return
	}
	if errors.Is(err, store.ErrProtectedOrg) {
		writeError(w, r, http.StatusBadRequest, "admin_org_protected", err.Error())
		return
	}
	if err != nil {
		writeError(w, r, http.StatusBadRequest, "admin_org_delete_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) bulkAdminOrgs(w http.ResponseWriter, r *http.Request) {
	actor, _ := s.userFromRequest(r)
	var req adminBulkRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid_json", "Invalid JSON body")
		return
	}
	if !hasBulkSelection(req.IDs) {
		writeError(w, r, http.StatusBadRequest, "empty_bulk_selection", "Select at least one organization")
		return
	}
	var result store.AdminOrgsResult
	var err error
	switch req.Action {
	case "status":
		result, err = s.repo.BulkUpdateAdminOrgs(r.Context(), req.IDs, actor.ID, req.Status)
	case "plan":
		result, err = s.repo.BulkUpdateAdminOrgPlans(r.Context(), req.IDs, actor.ID, req.Plan)
	case "delete":
		result, err = s.repo.BulkDeleteAdminOrgs(r.Context(), req.IDs, actor.ID)
	default:
		writeError(w, r, http.StatusBadRequest, "invalid_bulk_action", "Unsupported bulk action")
		return
	}
	if errors.Is(err, store.ErrInvalidAdminStatus) || errors.Is(err, store.ErrInvalidAdminPlan) || errors.Is(err, store.ErrProtectedOrg) {
		writeError(w, r, http.StatusBadRequest, "admin_org_bulk_failed", err.Error())
		return
	}
	if err != nil {
		writeError(w, r, http.StatusBadRequest, "admin_org_bulk_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) patchOpenAPISite(w http.ResponseWriter, r *http.Request) {
	user, _ := s.userFromRequest(r)
	var req openAPISitePatchRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid_json", "Invalid JSON body")
		return
	}
	site, err := s.repo.UpdateOpenAPISite(r.Context(), chi.URLParam(r, "siteID"), store.OpenAPISiteUpdateInput{
		Name: req.Name, Scopes: req.Scopes, QPSLimit: req.QPSLimit, Status: req.Status, ActorID: user.ID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, r, http.StatusNotFound, "open_api_site_not_found", "Open API site was not found")
		return
	}
	if errors.Is(err, store.ErrInvalidOpenAPIStatus) || errors.Is(err, store.ErrInvalidOpenAPIScope) {
		writeError(w, r, http.StatusBadRequest, "invalid_open_api_site", err.Error())
		return
	}
	if err != nil {
		writeError(w, r, http.StatusBadRequest, "open_api_site_update_failed", "Could not update Open API site")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"site": site})
}

func (s *Server) revokeOpenAPISite(w http.ResponseWriter, r *http.Request) {
	user, _ := s.userFromRequest(r)
	site, err := s.repo.RevokeOpenAPISite(r.Context(), chi.URLParam(r, "siteID"), user.ID)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, r, http.StatusNotFound, "open_api_site_not_found", "Open API site was not found")
		return
	}
	if err != nil {
		writeError(w, r, http.StatusBadRequest, "open_api_site_revoke_failed", "Could not revoke Open API site")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"site": site})
}

func (s *Server) deleteOpenAPISite(w http.ResponseWriter, r *http.Request) {
	user, _ := s.userFromRequest(r)
	site, err := s.repo.DeleteOpenAPISite(r.Context(), chi.URLParam(r, "siteID"), user.ID)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, r, http.StatusNotFound, "open_api_site_not_found", "Open API site was not found")
		return
	}
	if err != nil {
		writeError(w, r, http.StatusBadRequest, "open_api_site_delete_failed", "Could not delete Open API site")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"site": site})
}

func (s *Server) bulkOpenAPISites(w http.ResponseWriter, r *http.Request) {
	user, _ := s.userFromRequest(r)
	var req adminBulkRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid_json", "Invalid JSON body")
		return
	}
	if !hasBulkSelection(req.IDs) {
		writeError(w, r, http.StatusBadRequest, "empty_bulk_selection", "Select at least one Open API site")
		return
	}
	var sites []store.OpenAPISite
	var err error
	switch req.Action {
	case "status":
		sites, err = s.repo.BulkUpdateOpenAPISites(r.Context(), req.IDs, user.ID, req.Status)
	case "revoke":
		sites, err = s.repo.BulkRevokeOpenAPISites(r.Context(), req.IDs, user.ID)
	case "delete":
		sites, err = s.repo.BulkDeleteOpenAPISites(r.Context(), req.IDs, user.ID)
	default:
		writeError(w, r, http.StatusBadRequest, "invalid_bulk_action", "Unsupported bulk action")
		return
	}
	if errors.Is(err, store.ErrInvalidOpenAPIStatus) || errors.Is(err, store.ErrInvalidOpenAPIScope) {
		writeError(w, r, http.StatusBadRequest, "open_api_bulk_failed", err.Error())
		return
	}
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, r, http.StatusNotFound, "open_api_site_not_found", "Open API site was not found")
		return
	}
	if err != nil {
		writeError(w, r, http.StatusBadRequest, "open_api_bulk_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"sites": sites})
}

func (s *Server) adminProductionHealth(w http.ResponseWriter, r *http.Request) {
	health, err := s.repo.ProductionHealth(r.Context())
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "production_health_unavailable", "Could not load production health")
		return
	}
	checks := health.Checks
	add := func(id string, label string, ok bool, severity string, value string, message string, action string) {
		status := "pass"
		if !ok {
			status = "fail"
			if severity == "warning" {
				status = "warn"
			}
		}
		checks = append(checks, store.ProductionHealthCheck{ID: id, Label: label, Status: status, Severity: severity, Value: value, Message: message, Action: action})
	}
	add("env", "运行环境", s.cfg.Env == "production", "warning", s.cfg.Env, "生产发布建议 TOKHUB_ENV=production", "上线前运行 deploy/scripts/preflight.sh")
	add("seed_mode", "Seed 模式", s.cfg.SeedMode == "prod", "critical", s.cfg.SeedMode, "生产环境必须使用 prod seed", "设置 TOKHUB_SEED_MODE=prod")
	add("upstream_mode", "上游模式", s.cfg.UpstreamMode == "real", "critical", s.cfg.UpstreamMode, "生产网关必须真实透传上游", "设置 TOKHUB_UPSTREAM_MODE=real")
	add("session_secure", "安全 Cookie", s.cfg.SessionSecure, "warning", mapBool(s.cfg.SessionSecure, "true", "false"), "生产环境 Cookie 应启用 secure", "设置 TOKHUB_SESSION_SECURE=true")
	add("dev_tokens", "开发 Token 暴露", !s.cfg.ExposeDevTokens, "critical", mapBool(s.cfg.ExposeDevTokens, "enabled", "disabled"), "生产环境不能暴露 dev token", "设置 TOKHUB_EXPOSE_DEV_TOKENS=false")
	summary := map[string]int{"pass": 0, "warn": 0, "fail": 0}
	for _, check := range checks {
		summary[check.Status]++
	}
	health.Checks = checks
	health.Summary = summary
	writeJSON(w, http.StatusOK, health)
}
