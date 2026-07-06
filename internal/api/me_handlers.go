package api

import (
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"tokhub/internal/store"
)

type privateChannelRequest struct {
	Name       string `json:"name"`
	Provider   string `json:"provider"`
	Type       string `json:"type"`
	Model      string `json:"model"`
	Endpoint   string `json:"endpoint"`
	APIKey     string `json:"apiKey"`
	ProbeDaily int    `json:"probeDaily"`
}

type meProfileRequest struct {
	Name string `json:"name"`
}

func (s *Server) updateMeProfile(w http.ResponseWriter, r *http.Request) {
	user, _ := s.userFromRequest(r)
	var req meProfileRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid_json", "Invalid JSON body")
		return
	}
	name := strings.TrimSpace(req.Name)
	runes := []rune(name)
	if len(runes) == 0 {
		writeError(w, r, http.StatusBadRequest, "invalid_profile_name", "请输入显示名称")
		return
	}
	if len(runes) > 40 {
		writeError(w, r, http.StatusBadRequest, "invalid_profile_name", "显示名称不能超过 40 个字符")
		return
	}
	updated, err := s.repo.UpdateUserProfile(r.Context(), user.ID, name)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, r, http.StatusNotFound, "profile_not_found", "Current user was not found")
		return
	}
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "profile_update_failed", "Could not update profile")
		return
	}
	_ = s.repo.WriteAudit(r.Context(), store.AuditEvent{
		ActorType:  "user",
		ActorID:    user.ID,
		Action:     "me.profile.updated",
		ObjectType: "user",
		ObjectID:   user.ID,
		IP:         clientIP(r),
		Result:     "success",
		Metadata:   map[string]any{},
	})
	writeJSON(w, http.StatusOK, map[string]any{"user": updated.Public()})
}

func (s *Server) meFavorites(w http.ResponseWriter, r *http.Request) {
	user, _ := s.userFromRequest(r)
	items, err := s.repo.FavoriteChannels(r.Context(), user.ID)
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "favorites_unavailable", "Could not load favorites")
		return
	}
	ids, err := s.repo.FavoriteIDs(r.Context(), user.ID)
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "favorites_unavailable", "Could not load favorites")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "ids": ids})
}

func (s *Server) putFavorite(w http.ResponseWriter, r *http.Request) {
	user, _ := s.userFromRequest(r)
	channelID := chi.URLParam(r, "channelID")
	if err := s.repo.AddFavorite(r.Context(), user.ID, channelID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, r, http.StatusNotFound, "favorite_channel_not_found", "Favorite target channel was not found")
			return
		}
		writeError(w, r, http.StatusInternalServerError, "favorite_failed", "Could not save favorite")
		return
	}
	_ = s.repo.WriteAudit(r.Context(), store.AuditEvent{
		ActorType:  "user",
		ActorID:    user.ID,
		Action:     "favorite.added",
		ObjectType: "channel",
		ObjectID:   channelID,
		IP:         clientIP(r),
		Result:     "success",
		Metadata:   map[string]any{},
	})
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) deleteFavorite(w http.ResponseWriter, r *http.Request) {
	user, _ := s.userFromRequest(r)
	channelID := chi.URLParam(r, "channelID")
	if err := s.repo.RemoveFavorite(r.Context(), user.ID, channelID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, r, http.StatusNotFound, "favorite_not_found", "Favorite was not found")
			return
		}
		writeError(w, r, http.StatusInternalServerError, "favorite_failed", "Could not remove favorite")
		return
	}
	_ = s.repo.WriteAudit(r.Context(), store.AuditEvent{
		ActorType:  "user",
		ActorID:    user.ID,
		Action:     "favorite.removed",
		ObjectType: "channel",
		ObjectID:   channelID,
		IP:         clientIP(r),
		Result:     "success",
		Metadata:   map[string]any{},
	})
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) bulkFavorites(w http.ResponseWriter, r *http.Request) {
	user, _ := s.userFromRequest(r)
	var req adminBulkRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid_json", "Invalid JSON body")
		return
	}
	if !hasBulkSelection(req.IDs) {
		writeError(w, r, http.StatusBadRequest, "empty_bulk_selection", "Select at least one favorite")
		return
	}
	switch req.Action {
	case "delete", "remove":
	default:
		writeError(w, r, http.StatusBadRequest, "invalid_bulk_action", "Unsupported bulk action")
		return
	}
	items, ids, err := s.repo.BulkRemoveFavorites(r.Context(), user.ID, req.IDs)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, r, http.StatusNotFound, "favorite_not_found", "One or more favorites were not found")
		return
	}
	if err != nil {
		writeError(w, r, http.StatusBadRequest, "favorite_bulk_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "ids": ids})
}

func (s *Server) mePrivateChannels(w http.ResponseWriter, r *http.Request) {
	user, _ := s.userFromRequest(r)
	orgID, _, ok := s.consoleWorkspaceForRead(w, r, user)
	if !ok {
		return
	}
	items, err := s.repo.PrivateChannelsForOrg(r.Context(), orgID)
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "private_channels_unavailable", "Could not load private channels")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) createPrivateChannel(w http.ResponseWriter, r *http.Request) {
	user, _ := s.userFromRequest(r)
	orgID, _, ok := s.consoleWorkspaceForOperate(w, r, user)
	if !ok {
		return
	}
	input, err := s.privateChannelInput(r, true)
	if err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid_private_channel", err.Error())
		return
	}
	item, err := s.repo.CreatePrivateChannelForOrg(r.Context(), orgID, user.ID, input)
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "private_channel_failed", "Could not create private channel")
		return
	}
	_ = s.repo.WriteAudit(r.Context(), store.AuditEvent{
		ActorType:  "user",
		ActorID:    user.ID,
		Action:     "private_channel.created",
		ObjectType: "channel",
		ObjectID:   item.ID,
		IP:         clientIP(r),
		Result:     "success",
		Metadata:   map[string]any{"provider": item.Provider, "model": item.Model, "key_fingerprint": item.KeyFingerprint},
	})
	writeJSON(w, http.StatusCreated, map[string]any{"channel": item})
}

func (s *Server) updatePrivateChannel(w http.ResponseWriter, r *http.Request) {
	user, _ := s.userFromRequest(r)
	orgID, _, ok := s.consoleWorkspaceForOperate(w, r, user)
	if !ok {
		return
	}
	channelID := chi.URLParam(r, "channelID")
	input, updateCredential, err := s.privateChannelPatchInput(r)
	if err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid_private_channel", err.Error())
		return
	}
	item, err := s.repo.UpdatePrivateChannelForOrg(r.Context(), orgID, user.ID, channelID, input, updateCredential)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, r, http.StatusNotFound, "private_channel_not_found", "Private channel not found")
		return
	}
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "private_channel_failed", "Could not update private channel")
		return
	}
	_ = s.repo.WriteAudit(r.Context(), store.AuditEvent{
		ActorType:  "user",
		ActorID:    user.ID,
		Action:     "private_channel.updated",
		ObjectType: "channel",
		ObjectID:   item.ID,
		IP:         clientIP(r),
		Result:     "success",
		Metadata:   map[string]any{"provider": item.Provider, "model": item.Model, "key_rotated": updateCredential},
	})
	writeJSON(w, http.StatusOK, map[string]any{"channel": item})
}

func (s *Server) deletePrivateChannel(w http.ResponseWriter, r *http.Request) {
	user, _ := s.userFromRequest(r)
	orgID, _, ok := s.consoleWorkspaceForOperate(w, r, user)
	if !ok {
		return
	}
	channelID := chi.URLParam(r, "channelID")
	if err := s.repo.DeletePrivateChannelForOrg(r.Context(), orgID, user.ID, channelID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, r, http.StatusNotFound, "private_channel_not_found", "Private channel not found")
			return
		}
		writeError(w, r, http.StatusInternalServerError, "private_channel_failed", "Could not delete private channel")
		return
	}
	_ = s.repo.WriteAudit(r.Context(), store.AuditEvent{
		ActorType:  "user",
		ActorID:    user.ID,
		Action:     "private_channel.deleted",
		ObjectType: "channel",
		ObjectID:   channelID,
		IP:         clientIP(r),
		Result:     "success",
		Metadata:   map[string]any{},
	})
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func (s *Server) bulkPrivateChannels(w http.ResponseWriter, r *http.Request) {
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
		writeError(w, r, http.StatusBadRequest, "empty_bulk_selection", "Select at least one private channel")
		return
	}
	var items []store.PrivateChannel
	var err error
	switch req.Action {
	case "status":
		items, err = s.repo.BulkUpdatePrivateChannelsStatusForOrg(r.Context(), orgID, user.ID, req.IDs, req.Status)
	case "disable":
		items, err = s.repo.BulkUpdatePrivateChannelsStatusForOrg(r.Context(), orgID, user.ID, req.IDs, "disabled")
	case "enable":
		items, err = s.repo.BulkUpdatePrivateChannelsStatusForOrg(r.Context(), orgID, user.ID, req.IDs, "unknown")
	case "delete":
		items, err = s.repo.BulkDeletePrivateChannelsForOrg(r.Context(), orgID, user.ID, req.IDs)
	default:
		writeError(w, r, http.StatusBadRequest, "invalid_bulk_action", "Unsupported bulk action")
		return
	}
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, r, http.StatusNotFound, "private_channel_not_found", "One or more private channels were not found")
		return
	}
	if errors.Is(err, store.ErrInvalidAdminStatus) {
		writeError(w, r, http.StatusBadRequest, "invalid_private_channel_status", err.Error())
		return
	}
	if err != nil {
		writeError(w, r, http.StatusBadRequest, "private_channel_bulk_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) probePrivateChannelNow(w http.ResponseWriter, r *http.Request) {
	user, _ := s.userFromRequest(r)
	orgID, _, ok := s.consoleWorkspaceForOperate(w, r, user)
	if !ok {
		return
	}
	channelID := chi.URLParam(r, "channelID")
	if _, err := s.repo.PrivateChannelForOrg(r.Context(), orgID, channelID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, r, http.StatusNotFound, "private_channel_not_found", "Private channel not found")
			return
		}
		writeError(w, r, http.StatusInternalServerError, "private_channel_unavailable", "Could not load private channel")
		return
	}
	if s.probeRunner == nil {
		writeError(w, r, http.StatusServiceUnavailable, "probe_unavailable", "Probe runtime is not available")
		return
	}
	reserved, err := s.repo.ReservePrivateL3ProbeForOrg(r.Context(), orgID, channelID)
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "quota_unavailable", "Could not reserve probe quota")
		return
	}
	if !reserved {
		_ = s.probeRunner.ProbeNow(r.Context(), channelID, "private_quota_l1l2")
		writeError(w, r, http.StatusTooManyRequests, "quota_exhausted", "Daily L3 quota is exhausted")
		return
	}
	cred, err := s.repo.PrivateCredentialForOrg(r.Context(), orgID, channelID)
	if err != nil {
		writeError(w, r, http.StatusNotFound, "private_channel_not_found", "Private channel not found")
		return
	}
	if s.secretBox == nil {
		writeError(w, r, http.StatusInternalServerError, "encryption_unavailable", "Credential encryption is unavailable")
		return
	}
	apiKey, err := s.secretBox.Decrypt(cred.Ciphertext, cred.Nonce)
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "credential_unavailable", "Could not decrypt credential")
		return
	}
	if err := s.probeRunner.ProbeNowWithL3(r.Context(), channelID, "private_manual", apiKey); err != nil {
		writeError(w, r, http.StatusInternalServerError, "probe_failed", "Probe failed")
		return
	}
	item, err := s.repo.PrivateChannelForOrg(r.Context(), orgID, channelID)
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "private_channel_unavailable", "Could not load private channel")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"channel": item})
}

func (s *Server) privateChannelInput(r *http.Request, requireKey bool) (store.PrivateChannelInput, error) {
	var req privateChannelRequest
	if err := decodeJSON(r, &req); err != nil {
		return store.PrivateChannelInput{}, errors.New("invalid JSON body")
	}
	req.Name = strings.TrimSpace(req.Name)
	req.Provider = strings.TrimSpace(req.Provider)
	req.Type = strings.TrimSpace(req.Type)
	req.Model = strings.TrimSpace(req.Model)
	endpoint, err := cleanBaseEndpoint(req.Endpoint)
	if err != nil {
		return store.PrivateChannelInput{}, err
	}
	if req.Provider == "" {
		req.Provider = "OpenAI"
	}
	if req.Type == "" {
		req.Type = "openai-compatible"
	}
	if req.ProbeDaily <= 0 {
		req.ProbeDaily = 50
	}
	if req.ProbeDaily > 500 {
		req.ProbeDaily = 500
	}
	if req.Name == "" || req.Model == "" {
		return store.PrivateChannelInput{}, errors.New("name, endpoint and model are required")
	}
	if requireKey && strings.TrimSpace(req.APIKey) == "" {
		return store.PrivateChannelInput{}, errors.New("apiKey is required")
	}
	if s.secretBox == nil {
		return store.PrivateChannelInput{}, errors.New("encryption is unavailable")
	}
	encrypted, err := s.secretBox.Encrypt(req.APIKey)
	if err != nil {
		return store.PrivateChannelInput{}, err
	}
	return store.PrivateChannelInput{
		Name:       req.Name,
		Provider:   req.Provider,
		Type:       req.Type,
		Model:      req.Model,
		Endpoint:   endpoint,
		ProbeDaily: req.ProbeDaily,
		Credential: store.EncryptedCredential{
			Ciphertext:  encrypted.Ciphertext,
			Nonce:       encrypted.Nonce,
			Fingerprint: encrypted.Fingerprint,
			Mask:        encrypted.Mask,
		},
	}, nil
}

func (s *Server) privateChannelPatchInput(r *http.Request) (store.PrivateChannelInput, bool, error) {
	var req privateChannelRequest
	if err := decodeJSON(r, &req); err != nil {
		return store.PrivateChannelInput{}, false, errors.New("invalid JSON body")
	}
	req.Name = strings.TrimSpace(req.Name)
	req.Provider = strings.TrimSpace(req.Provider)
	req.Type = strings.TrimSpace(req.Type)
	req.Model = strings.TrimSpace(req.Model)
	endpoint, err := cleanBaseEndpoint(req.Endpoint)
	if err != nil {
		return store.PrivateChannelInput{}, false, err
	}
	if req.Provider == "" {
		req.Provider = "OpenAI"
	}
	if req.Type == "" {
		req.Type = "openai-compatible"
	}
	if req.ProbeDaily <= 0 {
		req.ProbeDaily = 50
	}
	if req.Name == "" || req.Model == "" {
		return store.PrivateChannelInput{}, false, errors.New("name, endpoint and model are required")
	}
	input := store.PrivateChannelInput{Name: req.Name, Provider: req.Provider, Type: req.Type, Model: req.Model, Endpoint: endpoint, ProbeDaily: req.ProbeDaily}
	if strings.TrimSpace(req.APIKey) == "" {
		return input, false, nil
	}
	if s.secretBox == nil {
		return store.PrivateChannelInput{}, false, errors.New("encryption is unavailable")
	}
	encrypted, err := s.secretBox.Encrypt(req.APIKey)
	if err != nil {
		return store.PrivateChannelInput{}, false, err
	}
	input.Credential = store.EncryptedCredential{Ciphertext: encrypted.Ciphertext, Nonce: encrypted.Nonce, Fingerprint: encrypted.Fingerprint, Mask: encrypted.Mask}
	return input, true, nil
}
