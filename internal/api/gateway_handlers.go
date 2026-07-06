package api

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	gatewaycache "tokhub/internal/gateway"
	"tokhub/internal/store"
)

type createGatewayRequest struct {
	Name        string   `json:"name"`
	Policy      string   `json:"policy"`
	UpstreamIDs []string `json:"upstreamIds"`
	QPSLimit    int      `json:"qpsLimit"`
	QuotaMonth  int      `json:"quotaMonth"`
}

type patchGatewayRequest struct {
	Name        string   `json:"name"`
	Policy      string   `json:"policy"`
	Status      string   `json:"status"`
	UpstreamIDs []string `json:"upstreamIds"`
	QPSLimit    int      `json:"qpsLimit"`
	QuotaMonth  int      `json:"quotaMonth"`
}

type createGatewayKeyRequest struct {
	GatewayID  string `json:"gatewayId"`
	Name       string `json:"name"`
	QuotaMonth int    `json:"quotaMonth"`
	QPSLimit   int    `json:"qpsLimit"`
}

type patchGatewayKeyRequest struct {
	Name       string `json:"name"`
	QuotaMonth int    `json:"quotaMonth"`
	QPSLimit   int    `json:"qpsLimit"`
	Status     string `json:"status"`
}

type gatewayPayload struct {
	Model    string           `json:"model"`
	Messages []gatewayMessage `json:"messages"`
	Input    any              `json:"input"`
	Stream   bool             `json:"stream"`
}

type gatewayMessage struct {
	Role    string `json:"role"`
	Content any    `json:"content"`
}

type gatewayUsage struct {
	PromptTokens     int  `json:"prompt_tokens"`
	CompletionTokens int  `json:"completion_tokens"`
	TotalTokens      int  `json:"total_tokens"`
	Estimated        bool `json:"estimated,omitempty"`
}

func (s *Server) adminGateways(w http.ResponseWriter, r *http.Request) {
	gateways, upstreams, err := s.repo.GatewayAdminData(r.Context())
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "gateways_unavailable", "Could not load gateways")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": gateways, "upstreams": upstreams})
}

func (s *Server) consoleGateways(w http.ResponseWriter, r *http.Request) {
	user, _ := s.userFromRequest(r)
	orgID, _, ok := s.consoleWorkspaceForRead(w, r, user)
	if !ok {
		return
	}
	gateways, upstreams, err := s.repo.GatewayConsoleDataForOrg(r.Context(), user, orgID)
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "gateways_unavailable", "Could not load workspace gateways")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": gateways, "upstreams": upstreams, "allowPlatformUpstreams": user.CanUsePlatformGatewayUpstreams()})
}

func (s *Server) createGateway(w http.ResponseWriter, r *http.Request) {
	var req createGatewayRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid_json", "Invalid JSON body")
		return
	}
	user, _ := s.userFromRequest(r)
	gateway, err := s.repo.CreateGateway(r.Context(), store.GatewayCreateInput{
		Name:        req.Name,
		Policy:      req.Policy,
		UpstreamIDs: req.UpstreamIDs,
		QPSLimit:    req.QPSLimit,
		QuotaMonth:  req.QuotaMonth,
		BaseURL:     strings.TrimRight(s.cfg.PublicURL, "/") + "/gateway/v1",
		ActorID:     user.ID,
	})
	if err != nil {
		writeError(w, r, http.StatusBadRequest, "gateway_create_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"gateway": gateway})
}

func (s *Server) patchGateway(w http.ResponseWriter, r *http.Request) {
	var req patchGatewayRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid_json", "Invalid JSON body")
		return
	}
	if strings.EqualFold(strings.TrimSpace(req.Status), "deleted") {
		writeError(w, r, http.StatusBadRequest, "invalid_gateway_status", "Use DELETE /api/admin/gateways/{gatewayID} to delete a gateway")
		return
	}
	user, _ := s.userFromRequest(r)
	gateway, err := s.repo.UpdateGateway(r.Context(), chi.URLParam(r, "gatewayID"), store.GatewayUpdateInput{
		Name:         req.Name,
		Policy:       req.Policy,
		Status:       req.Status,
		UpstreamIDs:  req.UpstreamIDs,
		UpstreamsSet: req.UpstreamIDs != nil,
		QPSLimit:     req.QPSLimit,
		QuotaMonth:   req.QuotaMonth,
		ActorID:      user.ID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, r, http.StatusNotFound, "gateway_not_found", "Gateway was not found")
		return
	}
	if err != nil {
		writeError(w, r, http.StatusBadRequest, "gateway_update_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"gateway": gateway})
}

func (s *Server) deleteGateway(w http.ResponseWriter, r *http.Request) {
	user, _ := s.userFromRequest(r)
	err := s.repo.DeleteGateway(r.Context(), chi.URLParam(r, "gatewayID"), user.ID)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, r, http.StatusNotFound, "gateway_not_found", "Gateway was not found")
		return
	}
	if err != nil {
		writeError(w, r, http.StatusBadRequest, "gateway_delete_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) bulkAdminGateways(w http.ResponseWriter, r *http.Request) {
	user, _ := s.userFromRequest(r)
	var req adminBulkRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid_json", "Invalid JSON body")
		return
	}
	if !hasBulkSelection(req.IDs) {
		writeError(w, r, http.StatusBadRequest, "empty_bulk_selection", "Select at least one gateway")
		return
	}
	var gateways []store.Gateway
	var err error
	switch req.Action {
	case "status":
		if strings.EqualFold(strings.TrimSpace(req.Status), "deleted") {
			writeError(w, r, http.StatusBadRequest, "invalid_gateway_status", "Use bulk action delete to delete gateways")
			return
		}
		gateways, err = s.repo.BulkUpdateGateways(r.Context(), req.IDs, user.ID, req.Status)
	case "delete":
		gateways, err = s.repo.BulkDeleteGateways(r.Context(), req.IDs, user.ID)
	default:
		writeError(w, r, http.StatusBadRequest, "invalid_bulk_action", "Unsupported bulk action")
		return
	}
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, r, http.StatusNotFound, "gateway_not_found", "One or more gateways were not found")
		return
	}
	if err != nil {
		writeError(w, r, http.StatusBadRequest, "gateway_bulk_failed", err.Error())
		return
	}
	upstreams, upstreamErr := s.repo.AvailableGatewayUpstreams(r.Context())
	if upstreamErr != nil {
		writeError(w, r, http.StatusInternalServerError, "gateways_unavailable", "Could not load gateway upstreams")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": gateways, "upstreams": upstreams})
}

func (s *Server) createConsoleGateway(w http.ResponseWriter, r *http.Request) {
	var req createGatewayRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid_json", "Invalid JSON body")
		return
	}
	user, _ := s.userFromRequest(r)
	orgID, _, ok := s.consoleWorkspaceForOperate(w, r, user)
	if !ok {
		return
	}
	gateway, err := s.repo.CreateGateway(r.Context(), store.GatewayCreateInput{
		Name:          req.Name,
		Policy:        req.Policy,
		UpstreamIDs:   req.UpstreamIDs,
		QPSLimit:      req.QPSLimit,
		QuotaMonth:    req.QuotaMonth,
		BaseURL:       strings.TrimRight(s.cfg.PublicURL, "/") + "/gateway/v1",
		ActorID:       user.ID,
		OrgID:         orgID,
		PrivateOrgID:  orgID,
		PrivateUserID: user.ID,
		AllowPlatform: user.CanUsePlatformGatewayUpstreams(),
	})
	if err != nil {
		writeError(w, r, http.StatusBadRequest, "gateway_create_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"gateway": gateway})
}

func (s *Server) patchConsoleGateway(w http.ResponseWriter, r *http.Request) {
	var req patchGatewayRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid_json", "Invalid JSON body")
		return
	}
	if strings.EqualFold(strings.TrimSpace(req.Status), "deleted") {
		writeError(w, r, http.StatusBadRequest, "invalid_gateway_status", "Use DELETE /api/console/gateways/{gatewayID} to delete a gateway")
		return
	}
	user, _ := s.userFromRequest(r)
	orgID, _, ok := s.consoleWorkspaceForOperate(w, r, user)
	if !ok {
		return
	}
	gateway, err := s.repo.UpdateGatewayForOrg(r.Context(), chi.URLParam(r, "gatewayID"), orgID, store.GatewayUpdateInput{
		Name:          req.Name,
		Policy:        req.Policy,
		Status:        req.Status,
		UpstreamIDs:   req.UpstreamIDs,
		UpstreamsSet:  req.UpstreamIDs != nil,
		QPSLimit:      req.QPSLimit,
		QuotaMonth:    req.QuotaMonth,
		ActorID:       user.ID,
		PrivateOrgID:  orgID,
		PrivateUserID: user.ID,
		AllowPlatform: user.CanUsePlatformGatewayUpstreams(),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, r, http.StatusNotFound, "gateway_not_found", "Gateway was not found")
		return
	}
	if err != nil {
		writeError(w, r, http.StatusBadRequest, "gateway_update_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"gateway": gateway})
}

func (s *Server) deleteConsoleGateway(w http.ResponseWriter, r *http.Request) {
	user, _ := s.userFromRequest(r)
	orgID, _, ok := s.consoleWorkspaceForOperate(w, r, user)
	if !ok {
		return
	}
	err := s.repo.DeleteGatewayForOrg(r.Context(), chi.URLParam(r, "gatewayID"), user.ID, orgID)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, r, http.StatusNotFound, "gateway_not_found", "Gateway was not found")
		return
	}
	if err != nil {
		writeError(w, r, http.StatusBadRequest, "gateway_delete_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) bulkConsoleGateways(w http.ResponseWriter, r *http.Request) {
	user, _ := s.userFromRequest(r)
	orgID, _, ok := s.consoleWorkspaceForOperate(w, r, user)
	if !ok {
		return
	}
	var req adminBulkRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid_json", "Invalid JSON body")
		return
	}
	if !hasBulkSelection(req.IDs) {
		writeError(w, r, http.StatusBadRequest, "empty_bulk_selection", "Select at least one gateway")
		return
	}
	var gateways []store.Gateway
	var err error
	switch req.Action {
	case "status":
		if strings.EqualFold(strings.TrimSpace(req.Status), "deleted") {
			writeError(w, r, http.StatusBadRequest, "invalid_gateway_status", "Use bulk action delete to delete gateways")
			return
		}
		gateways, err = s.repo.BulkUpdateGatewaysForOrg(r.Context(), req.IDs, user.ID, orgID, req.Status)
	case "delete":
		gateways, err = s.repo.BulkDeleteGatewaysForOrg(r.Context(), req.IDs, user.ID, orgID)
	default:
		writeError(w, r, http.StatusBadRequest, "invalid_bulk_action", "Unsupported bulk action")
		return
	}
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, r, http.StatusNotFound, "gateway_not_found", "One or more gateways were not found")
		return
	}
	if err != nil {
		writeError(w, r, http.StatusBadRequest, "gateway_bulk_failed", err.Error())
		return
	}
	upstreams, upstreamErr := s.repo.AvailableGatewayUpstreamsForOrg(r.Context(), orgID, user.CanUsePlatformGatewayUpstreams())
	if upstreamErr != nil {
		writeError(w, r, http.StatusInternalServerError, "gateways_unavailable", "Could not load gateway upstreams")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": gateways, "upstreams": upstreams, "allowPlatformUpstreams": user.CanUsePlatformGatewayUpstreams()})
}

func (s *Server) adminGatewayKeys(w http.ResponseWriter, r *http.Request) {
	keys, err := s.repo.ListGatewayKeys(r.Context())
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "gateway_keys_unavailable", "Could not load gateway keys")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": keys})
}

func (s *Server) consoleGatewayKeys(w http.ResponseWriter, r *http.Request) {
	user, _ := s.userFromRequest(r)
	orgID, _, ok := s.consoleWorkspaceForRead(w, r, user)
	if !ok {
		return
	}
	keys, err := s.repo.ListGatewayKeysForOrg(r.Context(), orgID)
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "gateway_keys_unavailable", "Could not load gateway keys")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": keys})
}

func (s *Server) newOneTimeGatewayKey(w http.ResponseWriter, r *http.Request) (string, bool) {
	plain, err := store.NewGatewayPlainKey()
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "gateway_key_create_failed", "Could not create gateway key")
		return "", false
	}
	return plain, true
}

func (s *Server) createGatewayKey(w http.ResponseWriter, r *http.Request) {
	var req createGatewayKeyRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid_json", "Invalid JSON body")
		return
	}
	user, _ := s.userFromRequest(r)
	plain, ok := s.newOneTimeGatewayKey(w, r)
	if !ok {
		return
	}
	key, err := s.repo.CreateGatewayKey(r.Context(), store.GatewayKeyCreateInput{
		GatewayID:  req.GatewayID,
		Name:       req.Name,
		PlainKey:   plain,
		QuotaMonth: req.QuotaMonth,
		QPSLimit:   req.QPSLimit,
		ActorID:    user.ID,
	})
	if err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, pgx.ErrNoRows) {
			status = http.StatusNotFound
		}
		writeError(w, r, status, "gateway_key_create_failed", "Could not create gateway key")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"key": key})
}

func (s *Server) patchGatewayKey(w http.ResponseWriter, r *http.Request) {
	var req patchGatewayKeyRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid_json", "Invalid JSON body")
		return
	}
	user, _ := s.userFromRequest(r)
	key, err := s.repo.UpdateGatewayKey(r.Context(), chi.URLParam(r, "keyID"), store.GatewayKeyUpdateInput{
		Name:       req.Name,
		QuotaMonth: req.QuotaMonth,
		QPSLimit:   req.QPSLimit,
		Status:     req.Status,
		ActorID:    user.ID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, r, http.StatusNotFound, "gateway_key_not_found", "Gateway key was not found")
		return
	}
	if err != nil {
		writeError(w, r, http.StatusBadRequest, "gateway_key_update_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"key": key})
}

func (s *Server) createConsoleGatewayKey(w http.ResponseWriter, r *http.Request) {
	var req createGatewayKeyRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid_json", "Invalid JSON body")
		return
	}
	user, _ := s.userFromRequest(r)
	orgID, _, ok := s.consoleWorkspaceForOperate(w, r, user)
	if !ok {
		return
	}
	plain, ok := s.newOneTimeGatewayKey(w, r)
	if !ok {
		return
	}
	key, err := s.repo.CreateGatewayKey(r.Context(), store.GatewayKeyCreateInput{
		GatewayID:  req.GatewayID,
		Name:       req.Name,
		PlainKey:   plain,
		QuotaMonth: req.QuotaMonth,
		QPSLimit:   req.QPSLimit,
		ActorID:    user.ID,
		OrgID:      orgID,
	})
	if err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, pgx.ErrNoRows) {
			status = http.StatusNotFound
		}
		writeError(w, r, status, "gateway_key_create_failed", "Could not create gateway key")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"key": key})
}

func (s *Server) patchConsoleGatewayKey(w http.ResponseWriter, r *http.Request) {
	var req patchGatewayKeyRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid_json", "Invalid JSON body")
		return
	}
	user, _ := s.userFromRequest(r)
	orgID, _, ok := s.consoleWorkspaceForOperate(w, r, user)
	if !ok {
		return
	}
	key, err := s.repo.UpdateGatewayKeyForOrg(r.Context(), chi.URLParam(r, "keyID"), orgID, store.GatewayKeyUpdateInput{
		Name:       req.Name,
		QuotaMonth: req.QuotaMonth,
		QPSLimit:   req.QPSLimit,
		Status:     req.Status,
		ActorID:    user.ID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, r, http.StatusNotFound, "gateway_key_not_found", "Gateway key was not found")
		return
	}
	if err != nil {
		writeError(w, r, http.StatusBadRequest, "gateway_key_update_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"key": key})
}

func (s *Server) revokeGatewayKey(w http.ResponseWriter, r *http.Request) {
	user, _ := s.userFromRequest(r)
	keyID := chi.URLParam(r, "keyID")
	if err := s.repo.RevokeGatewayKey(r.Context(), keyID, user.ID); err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, pgx.ErrNoRows) {
			status = http.StatusNotFound
		}
		writeError(w, r, status, "gateway_key_revoke_failed", "Could not revoke gateway key")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "revoked"})
}

func (s *Server) deleteGatewayKey(w http.ResponseWriter, r *http.Request) {
	user, _ := s.userFromRequest(r)
	keyID := chi.URLParam(r, "keyID")
	if err := s.repo.DeleteGatewayKey(r.Context(), keyID, user.ID); err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, pgx.ErrNoRows) {
			status = http.StatusNotFound
		}
		writeError(w, r, status, "gateway_key_delete_failed", "Could not delete gateway key")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func (s *Server) bulkAdminGatewayKeys(w http.ResponseWriter, r *http.Request) {
	user, _ := s.userFromRequest(r)
	var req adminBulkRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid_json", "Invalid JSON body")
		return
	}
	if !hasBulkSelection(req.IDs) {
		writeError(w, r, http.StatusBadRequest, "empty_bulk_selection", "Select at least one gateway key")
		return
	}
	var keys []store.GatewayKey
	var err error
	switch req.Action {
	case "status":
		keys, err = s.repo.BulkUpdateGatewayKeys(r.Context(), req.IDs, user.ID, req.Status)
	case "revoke":
		keys, err = s.repo.BulkRevokeGatewayKeys(r.Context(), req.IDs, user.ID)
	case "delete":
		keys, err = s.repo.BulkDeleteGatewayKeys(r.Context(), req.IDs, user.ID)
	default:
		writeError(w, r, http.StatusBadRequest, "invalid_bulk_action", "Unsupported bulk action")
		return
	}
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, r, http.StatusNotFound, "gateway_key_not_found", "One or more gateway keys were not found")
		return
	}
	if err != nil {
		writeError(w, r, http.StatusBadRequest, "gateway_key_bulk_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": keys})
}

func (s *Server) bulkConsoleGatewayKeys(w http.ResponseWriter, r *http.Request) {
	user, _ := s.userFromRequest(r)
	orgID, _, ok := s.consoleWorkspaceForOperate(w, r, user)
	if !ok {
		return
	}
	var req adminBulkRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid_json", "Invalid JSON body")
		return
	}
	if !hasBulkSelection(req.IDs) {
		writeError(w, r, http.StatusBadRequest, "empty_bulk_selection", "Select at least one gateway key")
		return
	}
	var keys []store.GatewayKey
	var err error
	switch req.Action {
	case "status":
		keys, err = s.repo.BulkUpdateGatewayKeysForOrg(r.Context(), req.IDs, user.ID, orgID, req.Status)
	case "revoke":
		keys, err = s.repo.BulkRevokeGatewayKeysForOrg(r.Context(), req.IDs, user.ID, orgID)
	case "delete":
		keys, err = s.repo.BulkDeleteGatewayKeysForOrg(r.Context(), req.IDs, user.ID, orgID)
	default:
		writeError(w, r, http.StatusBadRequest, "invalid_bulk_action", "Unsupported bulk action")
		return
	}
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, r, http.StatusNotFound, "gateway_key_not_found", "One or more gateway keys were not found")
		return
	}
	if err != nil {
		writeError(w, r, http.StatusBadRequest, "gateway_key_bulk_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": keys})
}

func (s *Server) revokeConsoleGatewayKey(w http.ResponseWriter, r *http.Request) {
	user, _ := s.userFromRequest(r)
	orgID, _, ok := s.consoleWorkspaceForOperate(w, r, user)
	if !ok {
		return
	}
	keyID := chi.URLParam(r, "keyID")
	if err := s.repo.RevokeGatewayKeyForOrg(r.Context(), keyID, user.ID, orgID); err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, pgx.ErrNoRows) {
			status = http.StatusNotFound
		}
		writeError(w, r, status, "gateway_key_revoke_failed", "Could not revoke gateway key")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "revoked"})
}

func (s *Server) deleteConsoleGatewayKey(w http.ResponseWriter, r *http.Request) {
	user, _ := s.userFromRequest(r)
	orgID, _, ok := s.consoleWorkspaceForOperate(w, r, user)
	if !ok {
		return
	}
	keyID := chi.URLParam(r, "keyID")
	if err := s.repo.DeleteGatewayKeyForOrg(r.Context(), keyID, user.ID, orgID); err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, pgx.ErrNoRows) {
			status = http.StatusNotFound
		}
		writeError(w, r, status, "gateway_key_delete_failed", "Could not delete gateway key")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func (s *Server) adminMembers(w http.ResponseWriter, r *http.Request) {
	members, err := s.repo.GatewayMembers(r.Context())
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "members_unavailable", "Could not load members")
		return
	}
	keys, err := s.repo.ListGatewayKeys(r.Context())
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "gateway_keys_unavailable", "Could not load gateway keys")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"members": members, "keys": keys})
}

func (s *Server) inviteAdminMember(w http.ResponseWriter, r *http.Request) {
	user, _ := s.userFromRequest(r)
	var req workspaceMemberRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid_json", "Invalid JSON body")
		return
	}
	member, err := s.repo.UpsertWorkspaceMember(r.Context(), store.DefaultOrgID, user.ID, store.WorkspaceMemberInput{Email: req.Email, Role: req.Role, GroupName: req.GroupName})
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, r, http.StatusNotFound, "member_not_registered", "该邮箱需要先注册并完成邮箱验证")
		return
	}
	if errors.Is(err, store.ErrWorkspaceEmailUnverified) {
		writeError(w, r, http.StatusForbidden, "member_not_verified", "该邮箱需要完成邮箱验证后才能加入平台工作区")
		return
	}
	if errors.Is(err, store.ErrWorkspaceOwnerProtected) || errors.Is(err, store.ErrWorkspaceSelfRoleChange) {
		writeError(w, r, http.StatusBadRequest, "member_role_protected", err.Error())
		return
	}
	if err != nil {
		writeError(w, r, http.StatusBadRequest, "member_invite_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"member": member})
}

func (s *Server) patchAdminMember(w http.ResponseWriter, r *http.Request) {
	user, _ := s.userFromRequest(r)
	var req workspaceMemberRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid_json", "Invalid JSON body")
		return
	}
	member, err := s.repo.UpdateWorkspaceMember(r.Context(), store.DefaultOrgID, user.ID, chi.URLParam(r, "userID"), store.WorkspaceMemberInput{Role: req.Role, GroupName: req.GroupName})
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, r, http.StatusNotFound, "member_not_found", "Platform member was not found")
		return
	}
	if errors.Is(err, store.ErrWorkspaceOwnerProtected) || errors.Is(err, store.ErrWorkspaceSelfRoleChange) {
		writeError(w, r, http.StatusBadRequest, "member_role_protected", err.Error())
		return
	}
	if err != nil {
		writeError(w, r, http.StatusBadRequest, "member_update_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"member": member})
}

func (s *Server) deleteAdminMember(w http.ResponseWriter, r *http.Request) {
	user, _ := s.userFromRequest(r)
	err := s.repo.RemoveWorkspaceMember(r.Context(), store.DefaultOrgID, user.ID, chi.URLParam(r, "userID"))
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, r, http.StatusNotFound, "member_not_found", "Platform member was not found")
		return
	}
	if errors.Is(err, store.ErrWorkspaceOwnerProtected) || errors.Is(err, store.ErrWorkspaceSelfRoleChange) {
		writeError(w, r, http.StatusBadRequest, "member_role_protected", err.Error())
		return
	}
	if err != nil {
		writeError(w, r, http.StatusBadRequest, "member_remove_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "removed"})
}

func (s *Server) bulkAdminMembers(w http.ResponseWriter, r *http.Request) {
	user, _ := s.userFromRequest(r)
	var req adminBulkRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid_json", "Invalid JSON body")
		return
	}
	if !hasBulkSelection(req.IDs) {
		writeError(w, r, http.StatusBadRequest, "empty_bulk_selection", "Select at least one platform member")
		return
	}
	var members []store.GatewayMember
	var err error
	switch req.Action {
	case "role":
		members, err = s.repo.BulkUpdateWorkspaceMembersRole(r.Context(), store.DefaultOrgID, user.ID, req.IDs, req.Role)
	case "delete", "remove":
		members, err = s.repo.BulkRemoveWorkspaceMembers(r.Context(), store.DefaultOrgID, user.ID, req.IDs)
	default:
		writeError(w, r, http.StatusBadRequest, "invalid_bulk_action", "Unsupported bulk action")
		return
	}
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, r, http.StatusNotFound, "member_not_found", "One or more platform members were not found")
		return
	}
	if errors.Is(err, store.ErrWorkspaceOwnerProtected) || errors.Is(err, store.ErrWorkspaceSelfRoleChange) {
		writeError(w, r, http.StatusBadRequest, "member_role_protected", err.Error())
		return
	}
	if err != nil {
		writeError(w, r, http.StatusBadRequest, "member_bulk_failed", err.Error())
		return
	}
	members, err = s.repo.GatewayMembers(r.Context())
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "members_unavailable", "Could not load members")
		return
	}
	keys, keyErr := s.repo.ListGatewayKeys(r.Context())
	if keyErr != nil {
		writeError(w, r, http.StatusInternalServerError, "gateway_keys_unavailable", "Could not load gateway keys")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"members": members, "keys": keys})
}

func (s *Server) consoleMembers(w http.ResponseWriter, r *http.Request) {
	user, _ := s.userFromRequest(r)
	orgID, _, ok := s.consoleWorkspaceForRead(w, r, user)
	if !ok {
		return
	}
	members, err := s.repo.GatewayMembersForOrg(r.Context(), orgID)
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "members_unavailable", "Could not load members")
		return
	}
	keys, err := s.repo.ListGatewayKeysForOrg(r.Context(), orgID)
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "gateway_keys_unavailable", "Could not load gateway keys")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"members": members, "keys": keys})
}

func (s *Server) bulkConsoleMembers(w http.ResponseWriter, r *http.Request) {
	user, _ := s.userFromRequest(r)
	orgID, role, ok := s.consoleWorkspaceForWrite(w, r, user)
	if !ok {
		return
	}
	if !store.CanManageWorkspace(role) {
		writeError(w, r, http.StatusForbidden, "workspace_forbidden", "Only workspace admins can update members")
		return
	}
	var req adminBulkRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid_json", "Invalid JSON body")
		return
	}
	if !hasBulkSelection(req.IDs) {
		writeError(w, r, http.StatusBadRequest, "empty_bulk_selection", "Select at least one workspace member")
		return
	}
	var members []store.GatewayMember
	var err error
	switch req.Action {
	case "role":
		members, err = s.repo.BulkUpdateWorkspaceMembersRole(r.Context(), orgID, user.ID, req.IDs, req.Role)
	case "delete", "remove":
		members, err = s.repo.BulkRemoveWorkspaceMembers(r.Context(), orgID, user.ID, req.IDs)
	default:
		writeError(w, r, http.StatusBadRequest, "invalid_bulk_action", "Unsupported bulk action")
		return
	}
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, r, http.StatusNotFound, "member_not_found", "One or more workspace members were not found")
		return
	}
	if errors.Is(err, store.ErrWorkspaceOwnerProtected) || errors.Is(err, store.ErrWorkspaceSelfRoleChange) {
		writeError(w, r, http.StatusBadRequest, "member_role_protected", err.Error())
		return
	}
	if err != nil {
		writeError(w, r, http.StatusBadRequest, "member_bulk_failed", err.Error())
		return
	}
	keys, keyErr := s.repo.ListGatewayKeysForOrg(r.Context(), orgID)
	if keyErr != nil {
		writeError(w, r, http.StatusInternalServerError, "gateway_keys_unavailable", "Could not load gateway keys")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"members": members, "keys": keys})
}

func (s *Server) adminUsage(w http.ResponseWriter, r *http.Request) {
	usage, err := s.repo.GatewayUsageSummaryWithFilter(r.Context(), usageFilterFromRequest(r, ""))
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "usage_unavailable", "Could not load usage")
		return
	}
	writeJSON(w, http.StatusOK, usage)
}

func (s *Server) consoleUsage(w http.ResponseWriter, r *http.Request) {
	user, _ := s.userFromRequest(r)
	orgID, _, ok := s.consoleWorkspaceForRead(w, r, user)
	if !ok {
		return
	}
	usage, err := s.repo.GatewayUsageSummaryWithFilter(r.Context(), usageFilterFromRequest(r, orgID))
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "usage_unavailable", "Could not load usage")
		return
	}
	writeJSON(w, http.StatusOK, usage)
}

func (s *Server) exportAdminUsage(w http.ResponseWriter, r *http.Request) {
	s.exportUsageRollups(w, r, usageFilterFromRequest(r, ""))
}

func (s *Server) exportConsoleUsage(w http.ResponseWriter, r *http.Request) {
	user, _ := s.userFromRequest(r)
	orgID, _, ok := s.consoleWorkspaceForRead(w, r, user)
	if !ok {
		return
	}
	s.exportUsageRollups(w, r, usageFilterFromRequest(r, orgID))
}

func usageFilterFromRequest(r *http.Request, orgID string) store.GatewayUsageFilter {
	query := r.URL.Query()
	days, _ := strconv.Atoi(query.Get("days"))
	return store.GatewayUsageFilter{
		Days:                days,
		OrgID:               orgID,
		Source:              query.Get("source"),
		GatewayID:           query.Get("gatewayId"),
		ChannelID:           query.Get("channelId"),
		Model:               query.Get("model"),
		MemberUserID:        query.Get("memberUserId"),
		SkipRollupRecompute: shouldSkipUsageRollupRecompute(query.Get("recompute")),
	}
}

func shouldSkipUsageRollupRecompute(value string) bool {
	value = strings.ToLower(strings.TrimSpace(value))
	return value == "0" || value == "false"
}

func (s *Server) exportUsageRollups(w http.ResponseWriter, r *http.Request, filter store.GatewayUsageFilter) {
	rollups, err := s.repo.UsageRollupsWithFilter(r.Context(), filter)
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "usage_export_unavailable", "Could not export usage")
		return
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="tokhub-usage-rollups.csv"`)
	writer := csv.NewWriter(w)
	_ = writer.Write([]string{"day", "org_id", "source", "gateway_id", "channel_id", "model", "member_user_id", "requests", "tokens", "cost_usd", "errors", "probe_runs"})
	for _, item := range rollups {
		_ = writer.Write([]string{
			item.Day,
			item.OrgID,
			item.Source,
			item.GatewayID,
			item.ChannelID,
			item.Model,
			item.MemberUserID,
			strconv.Itoa(item.Requests),
			strconv.Itoa(item.Tokens),
			fmt.Sprintf("%.5f", item.CostUSD),
			strconv.Itoa(item.Errors),
			strconv.Itoa(item.ProbeRuns),
		})
	}
	writer.Flush()
}

func (s *Server) gatewayModels(w http.ResponseWriter, r *http.Request) {
	authn, ok := s.authenticateGatewayRequest(w, r)
	if !ok {
		return
	}
	start := time.Now()
	if s.cfg.UpstreamMode == "real" {
		candidates := s.availableGatewayCandidates(r.Context(), authn.Gateway)
		if len(candidates) == 0 {
			s.recordGatewayFailure(r, authn, "", "", http.StatusBadGateway, "no_upstream", start, false)
			writeError(w, r, http.StatusBadGateway, "no_upstream", "No healthy upstream is available")
			return
		}
		lastErrType := "upstream_failed"
		for _, upstream := range candidates {
			apiKey, err := s.gatewayUpstreamAPIKey(r.Context(), authn, upstream)
			if err != nil {
				lastErrType = "upstream_credential_unavailable"
				continue
			}
			result, err := s.upstreamClient.Models(r.Context(), gatewaycache.Upstream{
				Name: upstream.Name, Provider: upstream.Provider, Type: upstream.Type, Endpoint: upstream.Endpoint, Model: upstream.Model, ProviderConfig: upstream.ProviderConfig,
			}, apiKey)
			if err != nil {
				if result.ErrorType != "" {
					lastErrType = result.ErrorType
				}
				s.openCircuit(upstream.ChannelID)
				continue
			}
			_ = s.repo.RecordGatewayEvent(r.Context(), store.GatewayRequestEvent{
				GatewayID:         authn.Gateway.ID,
				GatewayKeyID:      authn.Key.ID,
				UpstreamChannelID: upstream.ChannelID,
				RequestPath:       "/gateway/v1/models",
				StatusCode:        result.StatusCode,
				LatencyMs:         int(time.Since(start).Milliseconds()),
			})
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(result.StatusCode)
			_, _ = w.Write(result.Body)
			return
		}
		s.recordGatewayFailure(r, authn, "", "", http.StatusBadGateway, lastErrType, start, false)
		writeError(w, r, http.StatusBadGateway, "upstream_failed", "All upstreams failed before first byte")
		return
	}
	models := []map[string]any{}
	seen := map[string]bool{}
	for _, upstream := range authn.Gateway.Upstreams {
		if !upstream.Enabled || seen[upstream.Model] {
			continue
		}
		seen[upstream.Model] = true
		models = append(models, map[string]any{
			"id":       upstream.Model,
			"object":   "model",
			"owned_by": upstream.Provider,
		})
	}
	_ = s.repo.RecordGatewayEvent(r.Context(), store.GatewayRequestEvent{
		GatewayID:    authn.Gateway.ID,
		GatewayKeyID: authn.Key.ID,
		RequestPath:  "/gateway/v1/models",
		StatusCode:   http.StatusOK,
		LatencyMs:    int(time.Since(start).Milliseconds()),
	})
	writeJSON(w, http.StatusOK, map[string]any{"object": "list", "data": models})
}

func (s *Server) gatewayChatCompletions(w http.ResponseWriter, r *http.Request) {
	s.handleGatewayGeneration(w, r, "chat")
}

func (s *Server) gatewayResponses(w http.ResponseWriter, r *http.Request) {
	s.handleGatewayGeneration(w, r, "responses")
}

func (s *Server) gatewayAnthropicMessages(w http.ResponseWriter, r *http.Request) {
	s.handleGatewayAnthropicMessages(w, r)
}

func (s *Server) handleGatewayGeneration(w http.ResponseWriter, r *http.Request, kind string) {
	authn, ok := s.authenticateGatewayRequest(w, r)
	if !ok {
		return
	}
	raw, payload, err := readGatewayPayload(r)
	if err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid_json", "Invalid JSON body")
		return
	}
	if strings.TrimSpace(payload.Model) == "" {
		payload.Model = firstGatewayModel(authn.Gateway)
	}
	start := time.Now()
	candidates := s.availableGatewayCandidates(r.Context(), authn.Gateway)
	if len(candidates) == 0 {
		s.recordGatewayFailure(r, authn, "", payload.Model, http.StatusBadGateway, "no_upstream", start, payload.Stream)
		writeError(w, r, http.StatusBadGateway, "no_upstream", "No healthy upstream is available")
		return
	}
	if s.cfg.UpstreamMode == "real" {
		s.handleRealGatewayGeneration(w, r, authn, candidates, kind, raw, payload, start)
		return
	}

	var lastErr error
	for _, upstream := range candidates {
		if payload.Stream {
			usage, err := s.writeMockGatewayStream(w, r, kind, payload, upstream)
			if err != nil {
				lastErr = err
				s.openCircuit(upstream.ChannelID)
				continue
			}
			s.recordGatewaySuccess(r, authn, upstream, payload.Model, usage, http.StatusOK, start, true)
			return
		}
		body, usage, err := mockGatewayJSON(kind, payload, upstream)
		if err != nil {
			lastErr = err
			s.openCircuit(upstream.ChannelID)
			continue
		}
		s.recordGatewaySuccess(r, authn, upstream, payload.Model, usage, http.StatusOK, start, false)
		writeJSON(w, http.StatusOK, body)
		return
	}

	s.recordGatewayFailure(r, authn, "", payload.Model, http.StatusBadGateway, "upstream_failed", start, payload.Stream)
	writeError(w, r, http.StatusBadGateway, "upstream_failed", fmt.Sprintf("All upstreams failed before first byte: %v", lastErr))
}

func (s *Server) handleGatewayAnthropicMessages(w http.ResponseWriter, r *http.Request) {
	authn, ok := s.authenticateGatewayRequest(w, r)
	if !ok {
		return
	}
	raw, payload, err := readAnthropicGatewayPayload(r)
	if err != nil {
		writeAnthropicError(w, http.StatusBadRequest, "invalid_request_error", "Invalid JSON body")
		return
	}
	if strings.TrimSpace(payload.Model) == "" {
		payload.Model = firstGatewayModel(authn.Gateway)
	}
	start := time.Now()
	candidates := s.availableGatewayCandidates(r.Context(), authn.Gateway)
	if len(candidates) == 0 {
		s.recordGatewayFailure(r, authn, "", payload.Model, http.StatusBadGateway, "no_upstream", start, payload.Stream)
		writeAnthropicError(w, http.StatusBadGateway, "api_error", "No healthy upstream is available")
		return
	}
	if s.cfg.UpstreamMode == "real" {
		s.handleRealGatewayAnthropicMessages(w, r, authn, candidates, raw, payload, start)
		return
	}

	var lastErr error
	for _, upstream := range candidates {
		body, usage, err := mockGatewayJSON("chat", payload, upstream)
		if err != nil {
			lastErr = err
			s.openCircuit(upstream.ChannelID)
			continue
		}
		encoded, _ := json.Marshal(body)
		message := anthropicMessageFromOpenAI(encoded, payload.Model)
		s.recordGatewaySuccess(r, authn, upstream, payload.Model, usage, http.StatusOK, start, payload.Stream)
		if payload.Stream {
			writeAnthropicMessageStream(w, message)
			return
		}
		writeJSON(w, http.StatusOK, message)
		return
	}

	s.recordGatewayFailure(r, authn, "", payload.Model, http.StatusBadGateway, "upstream_failed", start, payload.Stream)
	writeAnthropicError(w, http.StatusBadGateway, "api_error", fmt.Sprintf("All upstreams failed before first byte: %v", lastErr))
}

func (s *Server) handleRealGatewayGeneration(w http.ResponseWriter, r *http.Request, authn store.AuthenticatedGatewayKey, candidates []store.GatewayUpstream, kind string, raw []byte, payload gatewayPayload, start time.Time) {
	estimated := upstreamUsageFromGateway(estimateUsage(payload))
	raw, err := rawPayloadWithModel(raw, payload.Model)
	if err != nil {
		s.recordGatewayFailure(r, authn, "", payload.Model, http.StatusBadRequest, "invalid_json", start, payload.Stream)
		writeError(w, r, http.StatusBadRequest, "invalid_json", "Invalid JSON body")
		return
	}
	lastErrType := "upstream_failed"
	for _, upstream := range candidates {
		apiKey, err := s.gatewayUpstreamAPIKey(r.Context(), authn, upstream)
		if err != nil {
			lastErrType = "upstream_credential_unavailable"
			continue
		}
		clientUpstream := gatewaycache.Upstream{Name: upstream.Name, Provider: upstream.Provider, Type: upstream.Type, Endpoint: upstream.Endpoint, Model: upstream.Model, ProviderConfig: upstream.ProviderConfig}
		if payload.Stream {
			result, err := s.upstreamClient.Stream(r.Context(), clientUpstream, apiKey, kind, raw, estimated, w)
			usage := gatewayUsageFromUpstream(result.Usage)
			if err != nil {
				if result.ErrorType != "" {
					lastErrType = result.ErrorType
				}
				if result.Wrote {
					s.recordGatewayFailure(r, authn, upstream.ChannelID, payload.Model, http.StatusBadGateway, lastErrType, start, true)
					return
				}
				s.openCircuit(upstream.ChannelID)
				continue
			}
			s.recordGatewaySuccess(r, authn, upstream, payload.Model, usage, result.StatusCode, start, true)
			return
		}
		result, err := s.upstreamClient.JSON(r.Context(), clientUpstream, apiKey, kind, raw, estimated)
		if err != nil {
			if result.ErrorType != "" {
				lastErrType = result.ErrorType
			}
			s.openCircuit(upstream.ChannelID)
			continue
		}
		usage := gatewayUsageFromUpstream(result.Usage)
		s.recordGatewaySuccess(r, authn, upstream, payload.Model, usage, result.StatusCode, start, false)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(result.StatusCode)
		_, _ = w.Write(result.Body)
		return
	}
	s.recordGatewayFailure(r, authn, "", payload.Model, http.StatusBadGateway, lastErrType, start, payload.Stream)
	writeError(w, r, http.StatusBadGateway, "upstream_failed", "All upstreams failed before first byte")
}

func (s *Server) handleRealGatewayAnthropicMessages(w http.ResponseWriter, r *http.Request, authn store.AuthenticatedGatewayKey, candidates []store.GatewayUpstream, raw []byte, payload gatewayPayload, start time.Time) {
	estimated := upstreamUsageFromGateway(estimateUsage(payload))
	raw, err := rawPayloadWithModel(raw, payload.Model)
	if err != nil {
		s.recordGatewayFailure(r, authn, "", payload.Model, http.StatusBadRequest, "invalid_json", start, payload.Stream)
		writeAnthropicError(w, http.StatusBadRequest, "invalid_request_error", "Invalid JSON body")
		return
	}
	lastErrType := "upstream_failed"
	for _, upstream := range candidates {
		apiKey, err := s.gatewayUpstreamAPIKey(r.Context(), authn, upstream)
		if err != nil {
			lastErrType = "upstream_credential_unavailable"
			continue
		}
		clientUpstream := gatewaycache.Upstream{Name: upstream.Name, Provider: upstream.Provider, Type: upstream.Type, Endpoint: upstream.Endpoint, Model: upstream.Model, ProviderConfig: upstream.ProviderConfig}
		result, err := s.upstreamClient.JSON(r.Context(), clientUpstream, apiKey, "chat", raw, estimated)
		if err != nil {
			if result.ErrorType != "" {
				lastErrType = result.ErrorType
			}
			s.openCircuit(upstream.ChannelID)
			continue
		}
		usage := gatewayUsageFromUpstream(result.Usage)
		message := anthropicMessageFromOpenAI(result.Body, payload.Model)
		s.recordGatewaySuccess(r, authn, upstream, payload.Model, usage, result.StatusCode, start, payload.Stream)
		if payload.Stream {
			writeAnthropicMessageStream(w, message)
			return
		}
		writeJSON(w, result.StatusCode, message)
		return
	}
	s.recordGatewayFailure(r, authn, "", payload.Model, http.StatusBadGateway, lastErrType, start, payload.Stream)
	writeAnthropicError(w, http.StatusBadGateway, "api_error", "All upstreams failed before first byte")
}

func (s *Server) authenticateGatewayRequest(w http.ResponseWriter, r *http.Request) (store.AuthenticatedGatewayKey, bool) {
	header := strings.TrimSpace(r.Header.Get("Authorization"))
	plainKey := ""
	if strings.HasPrefix(strings.ToLower(header), "bearer ") {
		plainKey = strings.TrimSpace(header[len("Bearer "):])
	}
	if plainKey == "" {
		plainKey = strings.TrimSpace(r.Header.Get("X-API-Key"))
	}
	if plainKey == "" {
		writeError(w, r, http.StatusUnauthorized, "gateway_unauthorized", "Gateway API key is required")
		return store.AuthenticatedGatewayKey{}, false
	}
	authn, err := s.repo.AuthenticateGatewayKey(r.Context(), plainKey)
	if err != nil {
		writeError(w, r, http.StatusUnauthorized, "gateway_unauthorized", "Gateway API key is invalid or revoked")
		return store.AuthenticatedGatewayKey{}, false
	}
	qps := authn.Key.QPSLimit
	if qps <= 0 {
		qps = authn.Gateway.QPSLimit
	}
	if allowed, err := s.gatewayCache.AllowQPS(r.Context(), authn.Key.ID, qps); err == nil {
		if !allowed {
			writeError(w, r, http.StatusTooManyRequests, "gateway_rate_limited", "Gateway key QPS limit exceeded")
			return store.AuthenticatedGatewayKey{}, false
		}
	} else if !errors.Is(err, gatewaycache.ErrUnavailable) {
		s.logger.Warn("redis qps limiter failed; falling back to memory", "error", err)
		if !s.allowRate(s.gatewayLimiter, authn.Key.ID, qps, time.Second) {
			writeError(w, r, http.StatusTooManyRequests, "gateway_rate_limited", "Gateway key QPS limit exceeded")
			return store.AuthenticatedGatewayKey{}, false
		}
	} else if errors.Is(err, gatewaycache.ErrUnavailable) {
		if !s.allowRate(s.gatewayLimiter, authn.Key.ID, qps, time.Second) {
			writeError(w, r, http.StatusTooManyRequests, "gateway_rate_limited", "Gateway key QPS limit exceeded")
			return store.AuthenticatedGatewayKey{}, false
		}
	}
	reserved, err := s.repo.ReserveGatewayKeyRequest(r.Context(), authn.Key.ID)
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "gateway_quota_unavailable", "Gateway key quota could not be checked")
		return store.AuthenticatedGatewayKey{}, false
	}
	if !reserved {
		writeError(w, r, http.StatusTooManyRequests, "gateway_quota_exceeded", "Gateway key quota is exhausted")
		return store.AuthenticatedGatewayKey{}, false
	}
	return authn, true
}

func (s *Server) availableGatewayCandidates(ctx context.Context, gateway store.Gateway) []store.GatewayUpstream {
	candidates := s.repo.PlanGatewayRoute(ctx, gateway)
	out := []store.GatewayUpstream{}
	for _, upstream := range candidates {
		if s.circuitOpen(upstream.ChannelID) {
			continue
		}
		out = append(out, upstream)
	}
	route := make([]string, 0, len(out))
	for _, upstream := range out {
		route = append(route, upstream.ChannelID)
	}
	if err := s.gatewayCache.StoreRoutePlan(ctx, gateway.ID, route); err != nil && !errors.Is(err, gatewaycache.ErrUnavailable) {
		s.logger.Warn("store redis route plan failed", "gateway_id", gateway.ID, "error", err)
	}
	return out
}

func (s *Server) recordGatewaySuccess(r *http.Request, authn store.AuthenticatedGatewayKey, upstream store.GatewayUpstream, model string, usage gatewayUsage, statusCode int, start time.Time, stream bool) {
	if statusCode <= 0 {
		statusCode = http.StatusOK
	}
	costUSD := s.gatewayCostForUsage(r.Context(), upstream.ChannelID, upstream.Model, usage)
	_ = s.repo.RecordGatewayEvent(r.Context(), store.GatewayRequestEvent{
		GatewayID:         authn.Gateway.ID,
		GatewayKeyID:      authn.Key.ID,
		UpstreamChannelID: upstream.ChannelID,
		RequestPath:       r.URL.Path,
		Model:             model,
		StatusCode:        statusCode,
		RequestTokens:     usage.PromptTokens,
		ResponseTokens:    usage.CompletionTokens,
		CostUSD:           costUSD,
		LatencyMs:         int(time.Since(start).Milliseconds()),
		Stream:            stream,
		UsageEstimated:    usage.Estimated,
	})
}

func (s *Server) gatewayCostForUsage(ctx context.Context, channelID string, model string, usage gatewayUsage) float64 {
	cost, ok, err := s.repo.GatewayCostEstimate(ctx, channelID, model, usage.PromptTokens, usage.CompletionTokens)
	if err == nil && ok {
		return cost
	}
	return estimateGatewayCost(usage.TotalTokens)
}

func (s *Server) recordGatewayFailure(r *http.Request, authn store.AuthenticatedGatewayKey, upstreamChannelID string, model string, status int, errType string, start time.Time, stream bool) {
	_ = s.repo.RecordGatewayEvent(r.Context(), store.GatewayRequestEvent{
		GatewayID:         authn.Gateway.ID,
		GatewayKeyID:      authn.Key.ID,
		UpstreamChannelID: upstreamChannelID,
		RequestPath:       r.URL.Path,
		Model:             model,
		StatusCode:        status,
		LatencyMs:         int(time.Since(start).Milliseconds()),
		ErrorType:         errType,
		Stream:            stream,
	})
}

func (s *Server) gatewayUpstreamAPIKey(ctx context.Context, authn store.AuthenticatedGatewayKey, upstream store.GatewayUpstream) (string, error) {
	cred, err := s.repo.GatewayChannelCredential(ctx, authn.Key.OrgID, upstream.ChannelID)
	if err != nil {
		return "", err
	}
	if s.secretBox == nil {
		return "", errors.New("encryption is unavailable")
	}
	return s.secretBox.Decrypt(cred.Ciphertext, cred.Nonce)
}

func (s *Server) openCircuit(channelID string) {
	if err := s.gatewayCache.OpenCircuit(context.Background(), channelID, 30*time.Second); err != nil && !errors.Is(err, gatewaycache.ErrUnavailable) {
		s.logger.Warn("open redis circuit failed", "channel_id", channelID, "error", err)
	}
	s.circuitMu.Lock()
	defer s.circuitMu.Unlock()
	s.circuits[channelID] = time.Now().Add(30 * time.Second)
}

func (s *Server) circuitOpen(channelID string) bool {
	if open, err := s.gatewayCache.CircuitOpen(context.Background(), channelID); err == nil {
		return open
	} else if !errors.Is(err, gatewaycache.ErrUnavailable) {
		s.logger.Warn("read redis circuit failed", "channel_id", channelID, "error", err)
	}
	s.circuitMu.Lock()
	defer s.circuitMu.Unlock()
	until, ok := s.circuits[channelID]
	if !ok {
		return false
	}
	if time.Now().After(until) {
		delete(s.circuits, channelID)
		return false
	}
	return true
}

func mockGatewayJSON(kind string, payload gatewayPayload, upstream store.GatewayUpstream) (map[string]any, gatewayUsage, error) {
	if strings.Contains(upstream.Endpoint, "fail") {
		return nil, gatewayUsage{}, errors.New("mock upstream failure")
	}
	usage := estimateUsage(payload)
	text := fmt.Sprintf("TokHub gateway response via %s adapter on %s.", adapterName(upstream.Provider), upstream.Name)
	if kind == "responses" {
		return map[string]any{
			"id":         "resp_" + time.Now().Format("20060102150405"),
			"object":     "response",
			"created_at": time.Now().Unix(),
			"model":      payload.Model,
			"status":     "completed",
			"output": []map[string]any{{
				"type": "message",
				"role": "assistant",
				"content": []map[string]any{{
					"type": "output_text",
					"text": text,
				}},
			}},
			"usage":  usage,
			"tokhub": map[string]any{"upstream": upstream.Name, "provider": upstream.Provider},
		}, usage, nil
	}
	return map[string]any{
		"id":      "chatcmpl_" + time.Now().Format("20060102150405"),
		"object":  "chat.completion",
		"created": time.Now().Unix(),
		"model":   payload.Model,
		"choices": []map[string]any{{
			"index": 0,
			"message": map[string]any{
				"role":    "assistant",
				"content": text,
			},
			"finish_reason": "stop",
		}},
		"usage":  usage,
		"tokhub": map[string]any{"upstream": upstream.Name, "provider": upstream.Provider},
	}, usage, nil
}

func (s *Server) writeMockGatewayStream(w http.ResponseWriter, r *http.Request, kind string, payload gatewayPayload, upstream store.GatewayUpstream) (gatewayUsage, error) {
	if strings.Contains(upstream.Endpoint, "fail") {
		return gatewayUsage{}, errors.New("mock upstream failure")
	}
	usage := estimateUsage(payload)
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flusher, _ := w.(http.Flusher)
	chunks := []string{"TokHub ", "gateway ", "stream ", "ok"}
	for i, chunk := range chunks {
		var event map[string]any
		if kind == "responses" {
			event = map[string]any{
				"type":  "response.output_text.delta",
				"delta": chunk,
			}
		} else {
			event = map[string]any{
				"id":      "chatcmpl_stream",
				"object":  "chat.completion.chunk",
				"created": time.Now().Unix(),
				"model":   payloadModelOrDefault(payloadModel(payload), upstream.Model),
				"choices": []map[string]any{{
					"index": i,
					"delta": map[string]any{"content": chunk},
				}},
			}
		}
		bytes, _ := json.Marshal(event)
		_, _ = fmt.Fprintf(w, "data: %s\n\n", bytes)
		if flusher != nil {
			flusher.Flush()
		}
	}
	_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
	if flusher != nil {
		flusher.Flush()
	}
	return usage, nil
}

func readGatewayPayload(r *http.Request) ([]byte, gatewayPayload, error) {
	raw, err := io.ReadAll(io.LimitReader(r.Body, 4<<20))
	if err != nil {
		return nil, gatewayPayload{}, err
	}
	var payload gatewayPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, gatewayPayload{}, err
	}
	return raw, payload, nil
}

func readAnthropicGatewayPayload(r *http.Request) ([]byte, gatewayPayload, error) {
	raw, err := io.ReadAll(io.LimitReader(r.Body, 4<<20))
	if err != nil {
		return nil, gatewayPayload{}, err
	}
	var input map[string]any
	if err := json.Unmarshal(raw, &input); err != nil {
		return nil, gatewayPayload{}, err
	}
	openai := anthropicPayloadToOpenAI(input)
	encoded, err := json.Marshal(openai)
	if err != nil {
		return nil, gatewayPayload{}, err
	}
	var payload gatewayPayload
	if err := json.Unmarshal(encoded, &payload); err != nil {
		return nil, gatewayPayload{}, err
	}
	payload.Stream = boolFromAny(input["stream"])
	return encoded, payload, nil
}

func anthropicPayloadToOpenAI(input map[string]any) map[string]any {
	messages := []map[string]any{}
	if systemText := anthropicContentText(input["system"]); systemText != "" {
		messages = append(messages, map[string]any{"role": "system", "content": systemText})
	}
	if rawMessages, ok := input["messages"].([]any); ok {
		for _, rawMessage := range rawMessages {
			message, ok := rawMessage.(map[string]any)
			if !ok {
				continue
			}
			messages = append(messages, anthropicMessageToOpenAI(message)...)
		}
	}
	if len(messages) == 0 {
		messages = append(messages, map[string]any{"role": "user", "content": "Continue."})
	}
	maxTokens := intFromAny(input["max_tokens"])
	if maxTokens <= 0 {
		maxTokens = 2048
	}
	out := map[string]any{
		"model":      stringFromAny(input["model"]),
		"messages":   messages,
		"max_tokens": maxTokens,
		"stream":     false,
	}
	for _, key := range []string{"temperature", "top_p"} {
		if value, ok := input[key]; ok {
			out[key] = value
		}
	}
	if stops, ok := input["stop_sequences"]; ok {
		out["stop"] = stops
	}
	if tools := anthropicToolsToOpenAI(input["tools"]); len(tools) > 0 {
		out["tools"] = tools
		out["tool_choice"] = anthropicToolChoiceToOpenAI(input["tool_choice"])
	}
	return out
}

func anthropicMessageToOpenAI(message map[string]any) []map[string]any {
	role := stringFromAny(message["role"])
	content := message["content"]
	if role == "assistant" {
		texts := []string{}
		toolCalls := []map[string]any{}
		if blocks, ok := content.([]any); ok {
			for index, rawBlock := range blocks {
				block, ok := rawBlock.(map[string]any)
				if !ok {
					continue
				}
				switch stringFromAny(block["type"]) {
				case "tool_use":
					id := stringFromAny(block["id"])
					if id == "" {
						id = fmt.Sprintf("toolu_%d_%d", time.Now().UnixNano(), index)
					}
					toolCalls = append(toolCalls, map[string]any{
						"id":   id,
						"type": "function",
						"function": map[string]any{
							"name":      defaultString(stringFromAny(block["name"]), "tool"),
							"arguments": jsonString(block["input"]),
						},
					})
				default:
					if text := anthropicContentText(block); text != "" {
						texts = append(texts, text)
					}
				}
			}
		} else if text := anthropicContentText(content); text != "" {
			texts = append(texts, text)
		}
		out := map[string]any{"role": "assistant", "content": strings.Join(texts, "\n")}
		if len(toolCalls) > 0 {
			out["tool_calls"] = toolCalls
		}
		return []map[string]any{out}
	}
	if role != "user" {
		return nil
	}
	if blocks, ok := content.([]any); ok {
		out := []map[string]any{}
		texts := []string{}
		for index, rawBlock := range blocks {
			block, ok := rawBlock.(map[string]any)
			if !ok {
				continue
			}
			if stringFromAny(block["type"]) == "tool_result" {
				if len(texts) > 0 {
					out = append(out, map[string]any{"role": "user", "content": strings.Join(texts, "\n")})
					texts = nil
				}
				toolCallID := stringFromAny(block["tool_use_id"])
				if toolCallID == "" {
					toolCallID = fmt.Sprintf("toolu_%d_%d", time.Now().UnixNano(), index)
				}
				out = append(out, map[string]any{"role": "tool", "tool_call_id": toolCallID, "content": anthropicContentText(block["content"])})
				continue
			}
			if text := anthropicContentText(block); text != "" {
				texts = append(texts, text)
			}
		}
		if len(texts) > 0 {
			out = append(out, map[string]any{"role": "user", "content": strings.Join(texts, "\n")})
		}
		return out
	}
	if text := anthropicContentText(content); text != "" {
		return []map[string]any{{"role": "user", "content": text}}
	}
	return nil
}

func anthropicToolsToOpenAI(raw any) []map[string]any {
	tools, ok := raw.([]any)
	if !ok {
		return nil
	}
	out := []map[string]any{}
	for _, item := range tools {
		tool, ok := item.(map[string]any)
		if !ok || stringFromAny(tool["name"]) == "" {
			continue
		}
		parameters := tool["input_schema"]
		if parameters == nil {
			parameters = map[string]any{"type": "object", "properties": map[string]any{}}
		}
		out = append(out, map[string]any{
			"type": "function",
			"function": map[string]any{
				"name":        stringFromAny(tool["name"]),
				"description": stringFromAny(tool["description"]),
				"parameters":  parameters,
			},
		})
	}
	return out
}

func anthropicToolChoiceToOpenAI(raw any) any {
	choice, ok := raw.(map[string]any)
	if !ok {
		return "auto"
	}
	switch stringFromAny(choice["type"]) {
	case "tool":
		if name := stringFromAny(choice["name"]); name != "" {
			return map[string]any{"type": "function", "function": map[string]any{"name": name}}
		}
	case "any":
		return "required"
	}
	return "auto"
}

func anthropicContentText(content any) string {
	switch value := content.(type) {
	case nil:
		return ""
	case string:
		return value
	case []any:
		parts := []string{}
		for _, item := range value {
			if text := anthropicContentText(item); text != "" {
				parts = append(parts, text)
			}
		}
		return strings.Join(parts, "\n")
	case map[string]any:
		switch stringFromAny(value["type"]) {
		case "", "text", "input_text":
			return stringFromAny(value["text"])
		case "tool_result":
			id := defaultString(stringFromAny(value["tool_use_id"]), "unknown")
			return fmt.Sprintf("Tool result for %s:\n%s", id, anthropicContentText(value["content"]))
		default:
			return ""
		}
	default:
		return fmt.Sprint(value)
	}
}

func anthropicMessageFromOpenAI(body []byte, fallbackModel string) map[string]any {
	var raw map[string]any
	_ = json.Unmarshal(body, &raw)
	choices, _ := raw["choices"].([]any)
	choice := map[string]any{}
	if len(choices) > 0 {
		if first, ok := choices[0].(map[string]any); ok {
			choice = first
		}
	}
	message, _ := choice["message"].(map[string]any)
	contentBlocks := []map[string]any{}
	if text := anthropicContentText(message["content"]); text != "" {
		contentBlocks = append(contentBlocks, map[string]any{"type": "text", "text": text})
	}
	if toolCalls, ok := message["tool_calls"].([]any); ok {
		for index, rawCall := range toolCalls {
			call, ok := rawCall.(map[string]any)
			if !ok {
				continue
			}
			function, _ := call["function"].(map[string]any)
			id := stringFromAny(call["id"])
			if id == "" {
				id = fmt.Sprintf("toolu_%d_%d", time.Now().UnixNano(), index)
			}
			contentBlocks = append(contentBlocks, map[string]any{
				"type":  "tool_use",
				"id":    id,
				"name":  defaultString(stringFromAny(function["name"]), "tool"),
				"input": parseJSONMap(function["arguments"]),
			})
		}
	}
	if len(contentBlocks) == 0 {
		contentBlocks = append(contentBlocks, map[string]any{"type": "text", "text": ""})
	}
	usage, _ := raw["usage"].(map[string]any)
	stopReason := "end_turn"
	if stringFromAny(choice["finish_reason"]) == "length" {
		stopReason = "max_tokens"
	}
	for _, block := range contentBlocks {
		if block["type"] == "tool_use" {
			stopReason = "tool_use"
			break
		}
	}
	model := defaultString(stringFromAny(raw["model"]), fallbackModel)
	return map[string]any{
		"id":            "msg_" + strings.ReplaceAll(time.Now().Format("20060102150405.000000000"), ".", ""),
		"type":          "message",
		"role":          "assistant",
		"model":         model,
		"content":       contentBlocks,
		"stop_reason":   stopReason,
		"stop_sequence": nil,
		"usage": map[string]any{
			"input_tokens":  intFromAny(firstMapValue(usage, "prompt_tokens", "input_tokens", "PromptTokens")),
			"output_tokens": intFromAny(firstMapValue(usage, "completion_tokens", "output_tokens", "CompletionTokens")),
		},
	}
}

func writeAnthropicMessageStream(w http.ResponseWriter, message map[string]any) {
	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	start := cloneMap(message)
	start["content"] = []any{}
	start["stop_reason"] = nil
	start["stop_sequence"] = nil
	writeAnthropicEvent(w, "message_start", map[string]any{"type": "message_start", "message": start})
	blocks, _ := message["content"].([]map[string]any)
	if blocks == nil {
		if rawBlocks, ok := message["content"].([]any); ok {
			for _, rawBlock := range rawBlocks {
				if block, ok := rawBlock.(map[string]any); ok {
					blocks = append(blocks, block)
				}
			}
		}
	}
	for index, block := range blocks {
		if block["type"] == "tool_use" {
			startBlock := map[string]any{"type": "tool_use", "id": block["id"], "name": block["name"], "input": map[string]any{}}
			writeAnthropicEvent(w, "content_block_start", map[string]any{"type": "content_block_start", "index": index, "content_block": startBlock})
			writeAnthropicEvent(w, "content_block_delta", map[string]any{"type": "content_block_delta", "index": index, "delta": map[string]any{"type": "input_json_delta", "partial_json": jsonString(block["input"])}})
		} else {
			writeAnthropicEvent(w, "content_block_start", map[string]any{"type": "content_block_start", "index": index, "content_block": map[string]any{"type": "text", "text": ""}})
			writeAnthropicEvent(w, "content_block_delta", map[string]any{"type": "content_block_delta", "index": index, "delta": map[string]any{"type": "text_delta", "text": stringFromAny(block["text"])}})
		}
		writeAnthropicEvent(w, "content_block_stop", map[string]any{"type": "content_block_stop", "index": index})
	}
	usage, _ := message["usage"].(map[string]any)
	writeAnthropicEvent(w, "message_delta", map[string]any{
		"type":  "message_delta",
		"delta": map[string]any{"stop_reason": defaultString(stringFromAny(message["stop_reason"]), "end_turn"), "stop_sequence": nil},
		"usage": map[string]any{"output_tokens": intFromAny(usage["output_tokens"])},
	})
	writeAnthropicEvent(w, "message_stop", map[string]any{"type": "message_stop"})
}

func writeAnthropicEvent(w http.ResponseWriter, event string, payload map[string]any) {
	encoded, _ := json.Marshal(payload)
	_, _ = fmt.Fprintf(w, "event: %s\n", event)
	_, _ = fmt.Fprintf(w, "data: %s\n\n", encoded)
	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
	}
}

func writeAnthropicError(w http.ResponseWriter, status int, typ string, message string) {
	writeJSON(w, status, map[string]any{"type": "error", "error": map[string]any{"type": typ, "message": message}})
}

func rawPayloadWithModel(raw []byte, model string) ([]byte, error) {
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, err
	}
	if strings.TrimSpace(fmt.Sprint(payload["model"])) == "" || payload["model"] == nil {
		payload["model"] = model
	}
	return json.Marshal(payload)
}

func gatewayUsageFromUpstream(usage gatewaycache.UpstreamUsage) gatewayUsage {
	return gatewayUsage{
		PromptTokens:     usage.PromptTokens,
		CompletionTokens: usage.CompletionTokens,
		TotalTokens:      usage.TotalTokens,
		Estimated:        usage.Estimated,
	}
}

func upstreamUsageFromGateway(usage gatewayUsage) gatewaycache.UpstreamUsage {
	return gatewaycache.UpstreamUsage{
		PromptTokens:     usage.PromptTokens,
		CompletionTokens: usage.CompletionTokens,
		TotalTokens:      usage.TotalTokens,
		Estimated:        usage.Estimated,
	}
}

func estimateUsage(payload gatewayPayload) gatewayUsage {
	text := payload.Model + " " + fmt.Sprint(payload.Input)
	for _, msg := range payload.Messages {
		text += " " + msg.Role + " " + fmt.Sprint(msg.Content)
	}
	prompt := len([]rune(text))/4 + 8
	if prompt < 8 {
		prompt = 8
	}
	completion := 18
	return gatewayUsage{PromptTokens: prompt, CompletionTokens: completion, TotalTokens: prompt + completion, Estimated: true}
}

func estimateGatewayCost(tokens int) float64 {
	if tokens <= 0 {
		return 0
	}
	return float64(tokens) * 0.000002
}

func adapterName(provider string) string {
	switch strings.ToLower(provider) {
	case "anthropic":
		return "Anthropic Messages"
	case "google":
		return "Gemini generateContent"
	default:
		return "OpenAI compatible"
	}
}

func firstGatewayModel(gateway store.Gateway) string {
	for _, upstream := range gateway.Upstreams {
		if upstream.Model != "" {
			return upstream.Model
		}
	}
	return "gpt-4o-mini"
}

func payloadModel(payload gatewayPayload) string {
	return payload.Model
}

func stringFromAny(value any) string {
	switch typed := value.(type) {
	case nil:
		return ""
	case string:
		return typed
	default:
		return fmt.Sprint(typed)
	}
}

func intFromAny(value any) int {
	switch typed := value.(type) {
	case int:
		return typed
	case int64:
		return int(typed)
	case float64:
		return int(typed)
	case json.Number:
		n, _ := typed.Int64()
		return int(n)
	case string:
		n, _ := strconv.Atoi(strings.TrimSpace(typed))
		return n
	default:
		return 0
	}
}

func boolFromAny(value any) bool {
	switch typed := value.(type) {
	case bool:
		return typed
	case string:
		return strings.EqualFold(strings.TrimSpace(typed), "true")
	default:
		return false
	}
}

func defaultString(value string, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func jsonString(value any) string {
	if value == nil {
		return "{}"
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return "{}"
	}
	return string(encoded)
}

func parseJSONMap(value any) map[string]any {
	switch typed := value.(type) {
	case map[string]any:
		return typed
	case string:
		if strings.TrimSpace(typed) == "" {
			return map[string]any{}
		}
		var out map[string]any
		if err := json.Unmarshal([]byte(typed), &out); err == nil && out != nil {
			return out
		}
		return map[string]any{"input": typed}
	default:
		return map[string]any{}
	}
}

func firstMapValue(values map[string]any, keys ...string) any {
	for _, key := range keys {
		if value, ok := values[key]; ok {
			return value
		}
	}
	return nil
}

func cloneMap(values map[string]any) map[string]any {
	out := make(map[string]any, len(values))
	for key, value := range values {
		out[key] = value
	}
	return out
}

func payloadModelOrDefault(value string, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}
