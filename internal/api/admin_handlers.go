package api

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"tokhub/internal/auth"
	"tokhub/internal/store"
)

type adminPlatformChannelRequest struct {
	Name            string                     `json:"name"`
	Provider        string                     `json:"provider"`
	Type            string                     `json:"type"`
	Model           string                     `json:"model"`
	UpstreamModel   string                     `json:"upstreamModel"`
	Endpoint        string                     `json:"endpoint"`
	OfficialSiteURL string                     `json:"officialSiteUrl"`
	IntroTitle      string                     `json:"introTitle"`
	IntroSummary    string                     `json:"introSummary"`
	IntroBody       string                     `json:"introBody"`
	IntroHighlights []string                   `json:"introHighlights"`
	LogoURL         string                     `json:"logoUrl"`
	IntroSourceURL  string                     `json:"introSourceUrl"`
	APIKey          string                     `json:"apiKey"`
	ProbeDaily      int                        `json:"probeDaily"`
	PublicVisible   *bool                      `json:"publicVisible"`
	GatewayEnabled  *bool                      `json:"gatewayEnabled"`
	Enabled         *bool                      `json:"enabled"`
	InputPerMTok    *float64                   `json:"inputPerMtok"`
	OutputPerMTok   *float64                   `json:"outputPerMtok"`
	ProviderConfig  map[string]any             `json:"providerConfig"`
	MonitorModels   []store.MonitorModelConfig `json:"monitorModels"`
}

type adminCredentialRequest struct {
	APIKey string `json:"apiKey"`
}

type adminPasswordRequest struct {
	Password string `json:"password"`
}

type adminChannelSyncRequest struct {
	BaseURL  string `json:"baseUrl"`
	SiteKey  string `json:"siteKey"`
	Password string `json:"password"`
}

type channelSyncPayload struct {
	Version     string                        `json:"version"`
	Source      string                        `json:"source"`
	SyncedAt    time.Time                     `json:"syncedAt"`
	Items       []channelSyncItem             `json:"items"`
	Logs        []store.OpenAPIChannelSyncLog `json:"logs"`
	Credentials string                        `json:"credentials"`
}

type channelSyncItem struct {
	ID                  string         `json:"id"`
	Name                string         `json:"name"`
	Provider            string         `json:"provider"`
	Type                string         `json:"type"`
	Model               string         `json:"model"`
	UpstreamModel       string         `json:"upstreamModel"`
	Endpoint            string         `json:"endpoint"`
	OfficialSiteURL     string         `json:"officialSiteUrl"`
	IntroTitle          string         `json:"introTitle"`
	IntroSummary        string         `json:"introSummary"`
	IntroBody           string         `json:"introBody"`
	IntroHighlights     []string       `json:"introHighlights"`
	LogoURL             string         `json:"logoUrl"`
	IntroSourceURL      string         `json:"introSourceUrl"`
	Status              string         `json:"status"`
	Score               int            `json:"score"`
	ProbeDaily          int            `json:"probeDaily"`
	PublicVisible       bool           `json:"publicVisible"`
	GatewayEnabled      bool           `json:"gatewayEnabled"`
	InputPerMTok        float64        `json:"inputPerMtok"`
	OutputPerMTok       float64        `json:"outputPerMtok"`
	ProviderConfig      map[string]any `json:"providerConfig"`
	APIKey              string         `json:"apiKey,omitempty"`
	KeyMask             string         `json:"keyMask"`
	KeyFingerprint      string         `json:"keyFingerprint"`
	CredentialUpdatedAt *time.Time     `json:"credentialUpdatedAt,omitempty"`
	Uptime24h           float64        `json:"uptime24h"`
	SuccessRate         float64        `json:"successRate"`
	LatencyP95Ms        int            `json:"latencyP95Ms"`
	L1Status            string         `json:"l1Status"`
	L2Status            string         `json:"l2Status"`
	L3Status            string         `json:"l3Status"`
	L1LatencyMs         int            `json:"l1LatencyMs"`
	L2LatencyMs         int            `json:"l2LatencyMs"`
	L3LatencyMs         int            `json:"l3LatencyMs"`
	TokensUsed          int            `json:"tokensUsed"`
	CostUSD             float64        `json:"costUsd"`
	ErrorType           string         `json:"errorType,omitempty"`
	LastProbeAt         time.Time      `json:"lastProbeAt"`
	DataOrigin          string         `json:"dataOrigin"`
}

var adminChannelCSVHeaders = []string{
	"id",
	"name",
	"provider",
	"type",
	"model",
	"upstream_model",
	"endpoint",
	"official_site_url",
	"intro_title",
	"intro_summary",
	"intro_body",
	"intro_highlights_json",
	"logo_url",
	"intro_source_url",
	"status",
	"probe_daily",
	"public_visible",
	"gateway_enabled",
	"input_per_mtok",
	"output_per_mtok",
	"provider_config_json",
	"api_key",
	"key_mask",
	"key_fingerprint",
	"credential_updated_at",
	"data_origin",
}

func (s *Server) requireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isAdminAgentAuthorization(r) {
			s.authenticateAdminAgent(w, r, next)
			return
		}
		user, err := s.userFromRequest(r)
		if err != nil {
			writeError(w, r, http.StatusUnauthorized, "unauthorized", "Login required")
			return
		}
		if user.Role != "owner" && user.Role != "admin" {
			writeError(w, r, http.StatusForbidden, "forbidden", "Admin role required")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) requireUser(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, err := s.userFromRequest(r)
		if err != nil {
			writeError(w, r, http.StatusUnauthorized, "unauthorized", "Login required")
			return
		}
		if user.Role == "user" && !user.EmailVerified {
			emailVerificationRequired, err := s.emailVerificationRequired(r.Context())
			if err != nil {
				writeError(w, r, http.StatusInternalServerError, "site_config_unavailable", "Could not load site config")
				return
			}
			if emailVerificationRequired {
				writeError(w, r, http.StatusForbidden, "email_not_verified", "请先完成邮箱验证后再使用控制台")
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) emailVerificationRequired(ctx context.Context) (bool, error) {
	cfg, err := s.repo.SiteConfig(ctx)
	if err != nil {
		return false, err
	}
	return cfg.EmailVerificationRequired, nil
}

func (s *Server) userFromRequest(r *http.Request) (store.PublicUser, error) {
	if authn, ok := adminAgentFromContext(r.Context()); ok {
		return authn.User, nil
	}
	cookie, err := r.Cookie(auth.CookieName)
	if err != nil {
		return store.PublicUser{}, err
	}
	return s.auth.UserForSession(r.Context(), cookie.Value)
}

func (s *Server) adminChannels(w http.ResponseWriter, r *http.Request) {
	items, err := s.repo.AdminPlatformChannels(r.Context())
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "channels_unavailable", "Could not load admin channels")
		return
	}
	privateItems, err := s.repo.AdminPrivateChannelSummaries(r.Context())
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "private_channels_unavailable", "Could not load private channel summaries")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"items":   items,
		"total":   len(items),
		"private": privateItems,
		"summary": map[string]any{
			"platform": len(items),
			"private":  len(privateItems),
		},
	})
}

func (s *Server) exportAdminChannels(w http.ResponseWriter, r *http.Request) {
	user, _ := s.userFromRequest(r)
	var req adminPasswordRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid_json", "Invalid JSON body")
		return
	}
	if !s.verifyInteractiveAdminPassword(w, r, user, req.Password, "platform-channel-export") {
		return
	}
	items, err := s.repo.AdminPlatformChannels(r.Context())
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "channels_unavailable", "Could not load admin channels")
		return
	}
	rows, err := s.adminChannelExportRows(r.Context(), items)
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "channel_export_failed", "Could not prepare platform channel export")
		return
	}
	if err := s.repo.WriteAudit(r.Context(), store.AuditEvent{
		ActorType:  "user",
		ActorID:    user.ID,
		Action:     "platform_channel.exported",
		ObjectType: "channel",
		ObjectID:   "",
		IP:         clientIP(r),
		Result:     "success",
		Metadata:   map[string]any{"count": len(items), "format": "csv", "includes_plain_credentials": true},
	}); err != nil {
		writeError(w, r, http.StatusInternalServerError, "channel_export_audit_failed", "Could not record export audit")
		return
	}
	filename := fmt.Sprintf("tokhub-platform-channels-%s.csv", time.Now().Format("20060102-150405"))
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")
	_, _ = w.Write([]byte("\xEF\xBB\xBF"))
	writer := csv.NewWriter(w)
	_ = writer.Write(adminChannelCSVHeaders)
	for _, row := range rows {
		_ = writer.Write(row)
	}
	writer.Flush()
}

func (s *Server) importAdminChannels(w http.ResponseWriter, r *http.Request) {
	user, _ := s.userFromRequest(r)
	r.Body = http.MaxBytesReader(w, r.Body, 5<<20)
	if err := r.ParseMultipartForm(5 << 20); err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid_import_file", "CSV file is required and must be smaller than 5MB")
		return
	}
	if !s.verifyInteractiveAdminPassword(w, r, user, r.FormValue("password"), "platform-channel-import") {
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		writeError(w, r, http.StatusBadRequest, "import_file_required", "CSV file is required")
		return
	}
	defer file.Close()
	rows, err := s.parseAdminChannelImportCSV(file)
	if err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid_channel_import_csv", err.Error())
		return
	}
	results, err := s.repo.ImportPlatformChannels(r.Context(), user.ID, rows)
	if err != nil {
		writeError(w, r, http.StatusBadRequest, "channel_import_failed", err.Error())
		return
	}
	created := 0
	updated := 0
	for _, result := range results {
		switch result.Action {
		case "created":
			created++
		case "updated":
			updated++
		}
	}
	_ = s.repo.WriteAudit(r.Context(), store.AuditEvent{
		ActorType:  "user",
		ActorID:    user.ID,
		Action:     "platform_channel.imported",
		ObjectType: "channel",
		ObjectID:   "",
		IP:         clientIP(r),
		Result:     "success",
		Metadata:   map[string]any{"created": created, "updated": updated, "count": len(results), "file_name": header.Filename},
	})
	items, _ := s.repo.AdminPlatformChannels(r.Context())
	writeJSON(w, http.StatusOK, map[string]any{
		"created":  created,
		"updated":  updated,
		"rows":     results,
		"items":    items,
		"fileName": header.Filename,
	})
}

func (s *Server) syncAdminChannelsFromAPI(w http.ResponseWriter, r *http.Request) {
	user, _ := s.userFromRequest(r)
	var req adminChannelSyncRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid_json", "Invalid JSON body")
		return
	}
	if !s.verifyInteractiveAdminPassword(w, r, user, req.Password, "channels_sync") {
		return
	}
	endpoint, err := channelSyncEndpoint(req.BaseURL, true)
	if err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid_channel_sync_url", err.Error())
		return
	}
	if strings.TrimSpace(req.SiteKey) == "" {
		writeError(w, r, http.StatusBadRequest, "site_key_required", "请输入通道同步 API 的 Site Key")
		return
	}
	payload, err := fetchChannelSyncPayload(r.Context(), endpoint, req.SiteKey)
	if err != nil {
		writeError(w, r, http.StatusBadGateway, "channel_sync_fetch_failed", err.Error())
		return
	}
	rows, err := s.channelSyncImportRows(payload)
	if err != nil {
		writeError(w, r, http.StatusBadRequest, "channel_sync_invalid", err.Error())
		return
	}
	results, err := s.repo.ImportPlatformChannels(r.Context(), user.ID, rows)
	if err != nil {
		writeError(w, r, http.StatusBadRequest, "channel_sync_import_failed", err.Error())
		return
	}
	created := 0
	updated := 0
	for _, result := range results {
		switch result.Action {
		case "created":
			created++
		case "updated":
			updated++
		}
	}
	_ = s.repo.WriteAudit(r.Context(), store.AuditEvent{
		ActorType:  "user",
		ActorID:    user.ID,
		Action:     "platform_channel.api_synced",
		ObjectType: "channel",
		ObjectID:   "bulk",
		IP:         clientIP(r),
		Result:     "success",
		Metadata: map[string]any{
			"source":      payload.Source,
			"endpoint":    channelSyncAuditEndpoint(endpoint),
			"created":     created,
			"updated":     updated,
			"count":       len(results),
			"probe_logs":  len(payload.Logs),
			"credentials": payload.Credentials,
		},
	})
	items, err := s.repo.AdminPlatformChannels(r.Context())
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "channels_unavailable", "Could not reload channels")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"created":     created,
		"updated":     updated,
		"rows":        results,
		"items":       items,
		"source":      payload.Source,
		"syncedAt":    payload.SyncedAt,
		"logs":        payload.Logs,
		"endpoint":    endpoint,
		"credentials": payload.Credentials,
	})
}

func channelSyncAuditEndpoint(endpoint string) string {
	parsed, err := url.Parse(endpoint)
	if err != nil {
		return endpoint
	}
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed.String()
}

func channelSyncEndpoint(raw string, includeCredentials bool) (string, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return "", errors.New("请输入源站 Open API Base URL")
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "", errors.New("源站地址必须是 http 或 https URL")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", errors.New("源站地址必须使用 http 或 https")
	}
	if parsed.User != nil {
		return "", errors.New("源站地址不能包含用户名或密码")
	}
	path := strings.TrimRight(parsed.Path, "/")
	switch {
	case path == "" || path == "/":
		parsed.Path = "/v1/status/channel-sync"
	case strings.HasSuffix(path, "/v1/status/channel-sync"):
		parsed.Path = path
	case strings.HasSuffix(path, "/v1/status"):
		parsed.Path = path + "/channel-sync"
	default:
		parsed.Path = path + "/v1/status/channel-sync"
	}
	query := parsed.Query()
	if includeCredentials {
		query.Set("includeCredentials", "1")
	}
	parsed.RawQuery = query.Encode()
	parsed.Fragment = ""
	return parsed.String(), nil
}

func fetchChannelSyncPayload(ctx context.Context, endpoint string, siteKey string) (channelSyncPayload, error) {
	ctx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return channelSyncPayload{}, err
	}
	req.Header.Set("X-Site-Key", strings.TrimSpace(siteKey))
	req.Header.Set("Accept", "application/json")
	resp, err := (&http.Client{Timeout: 12 * time.Second}).Do(req)
	if err != nil {
		return channelSyncPayload{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		message := strings.TrimSpace(string(raw))
		if message == "" {
			message = resp.Status
		}
		return channelSyncPayload{}, fmt.Errorf("源站返回 %s: %s", resp.Status, message)
	}
	reader := io.LimitReader(resp.Body, (5<<20)+1)
	raw, err := io.ReadAll(reader)
	if err != nil {
		return channelSyncPayload{}, err
	}
	if len(raw) > 5<<20 {
		return channelSyncPayload{}, errors.New("源站响应超过 5MB")
	}
	var payload channelSyncPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		return channelSyncPayload{}, fmt.Errorf("源站响应不是有效 JSON: %w", err)
	}
	return payload, nil
}

func (s *Server) channelSyncImportRows(payload channelSyncPayload) ([]store.PlatformChannelImportRow, error) {
	if payload.Version != "tokhub.channel_sync.v1" {
		return nil, errors.New("源站不是兼容的 TokHub 通道同步 API")
	}
	if payload.Credentials != "included" {
		return nil, errors.New("源站没有返回凭据明文，请确认 Site Key 具备 channel_sync scope")
	}
	if len(payload.Items) == 0 {
		return nil, errors.New("源站没有可同步的平台通道")
	}
	if len(payload.Items) > 1000 {
		return nil, errors.New("一次最多同步 1000 个平台通道")
	}
	rows := make([]store.PlatformChannelImportRow, 0, len(payload.Items))
	seenIDs := map[string]int{}
	for index, item := range payload.Items {
		rowNumber := index + 1
		id := strings.TrimSpace(item.ID)
		if id != "" {
			if !strings.HasPrefix(id, "ch_") || len(id) > 128 || strings.ContainsAny(id, " \t\r\n") {
				return nil, fmt.Errorf("第 %d 个通道 id 必须为空或使用 ch_ 前缀且不包含空白字符", rowNumber)
			}
			if firstRow, ok := seenIDs[id]; ok {
				return nil, fmt.Errorf("第 %d 个通道与第 %d 个通道重复使用 id %s", rowNumber, firstRow, id)
			}
			seenIDs[id] = rowNumber
		}
		enabled := !strings.EqualFold(strings.TrimSpace(item.Status), "disabled")
		inputPerMTok := item.InputPerMTok
		outputPerMTok := item.OutputPerMTok
		req := adminPlatformChannelRequest{
			Name:            item.Name,
			Provider:        item.Provider,
			Type:            item.Type,
			Model:           item.Model,
			UpstreamModel:   item.UpstreamModel,
			Endpoint:        item.Endpoint,
			OfficialSiteURL: item.OfficialSiteURL,
			IntroTitle:      item.IntroTitle,
			IntroSummary:    item.IntroSummary,
			IntroBody:       item.IntroBody,
			IntroHighlights: item.IntroHighlights,
			LogoURL:         item.LogoURL,
			IntroSourceURL:  item.IntroSourceURL,
			APIKey:          item.APIKey,
			ProbeDaily:      item.ProbeDaily,
			PublicVisible:   &item.PublicVisible,
			GatewayEnabled:  &item.GatewayEnabled,
			Enabled:         &enabled,
			InputPerMTok:    &inputPerMTok,
			OutputPerMTok:   &outputPerMTok,
			ProviderConfig:  item.ProviderConfig,
		}
		input, err := s.adminPlatformChannelInputFromRequest(req, true)
		if err != nil {
			return nil, fmt.Errorf("第 %d 个通道无效: %w", rowNumber, err)
		}
		snapshot := &store.PlatformChannelImportSnapshot{
			Status:       item.Status,
			Score:        item.Score,
			Uptime24h:    item.Uptime24h,
			SuccessRate:  item.SuccessRate,
			LatencyP95Ms: item.LatencyP95Ms,
			L1Status:     item.L1Status,
			L2Status:     item.L2Status,
			L3Status:     item.L3Status,
			L1LatencyMs:  item.L1LatencyMs,
			L2LatencyMs:  item.L2LatencyMs,
			L3LatencyMs:  item.L3LatencyMs,
			TokensUsed:   item.TokensUsed,
			CostUSD:      item.CostUSD,
			ErrorType:    item.ErrorType,
		}
		if !item.LastProbeAt.IsZero() {
			snapshot.SampledAt = &item.LastProbeAt
		}
		rows = append(rows, store.PlatformChannelImportRow{RowNumber: rowNumber, ID: id, Input: input, Snapshot: snapshot})
	}
	return rows, nil
}

func (s *Server) createAdminChannel(w http.ResponseWriter, r *http.Request) {
	user, _ := s.userFromRequest(r)
	var req adminPlatformChannelRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid_platform_channel", "invalid JSON body")
		return
	}
	inputs, err := s.adminPlatformChannelInputsFromRequest(req)
	if err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid_platform_channel", err.Error())
		return
	}
	items, err := s.repo.CreatePlatformChannels(r.Context(), user.ID, inputs)
	if err != nil {
		writeError(w, r, http.StatusBadRequest, "platform_channel_create_failed", err.Error())
		return
	}
	var first any
	if len(items) > 0 {
		first = items[0]
	}
	writeJSON(w, http.StatusCreated, map[string]any{"channel": first, "channels": items})
}

func (s *Server) updateAdminChannel(w http.ResponseWriter, r *http.Request) {
	user, _ := s.userFromRequest(r)
	channelID := chi.URLParam(r, "channelID")
	input, err := s.adminPlatformChannelInput(r, false)
	if err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid_platform_channel", err.Error())
		return
	}
	item, err := s.repo.UpdatePlatformChannel(r.Context(), user.ID, channelID, input)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, r, http.StatusNotFound, "platform_channel_not_found", "Platform channel not found")
		return
	}
	if err != nil {
		writeError(w, r, http.StatusBadRequest, "platform_channel_update_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"channel": item})
}

func (s *Server) rotateAdminChannelCredential(w http.ResponseWriter, r *http.Request) {
	user, _ := s.userFromRequest(r)
	channelID := chi.URLParam(r, "channelID")
	var req adminCredentialRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid_json", "Invalid JSON body")
		return
	}
	credential, err := s.encryptAdminChannelKey(req.APIKey, true)
	if err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid_credential", err.Error())
		return
	}
	item, err := s.repo.RotatePlatformCredential(r.Context(), user.ID, channelID, credential)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, r, http.StatusNotFound, "platform_channel_not_found", "Platform channel not found")
		return
	}
	if err != nil {
		writeError(w, r, http.StatusBadRequest, "credential_rotate_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"channel": item})
}

func (s *Server) validateAdminChannel(w http.ResponseWriter, r *http.Request) {
	user, _ := s.userFromRequest(r)
	channelID := chi.URLParam(r, "channelID")
	if _, err := s.repo.AdminPlatformChannel(r.Context(), channelID); errors.Is(err, pgx.ErrNoRows) {
		writeError(w, r, http.StatusNotFound, "platform_channel_not_found", "Platform channel not found")
		return
	} else if err != nil {
		writeError(w, r, http.StatusInternalServerError, "channel_unavailable", "Could not load channel")
		return
	}
	apiKey, err := s.platformChannelAPIKey(r.Context(), channelID)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, r, http.StatusBadRequest, "credential_missing", "Platform channel credential is missing")
		return
	}
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "credential_unavailable", "Could not decrypt platform credential")
		return
	}
	if s.probeRunner == nil {
		writeError(w, r, http.StatusServiceUnavailable, "probe_unavailable", "Probe runtime is not available")
		return
	}
	if err := s.probeRunner.ProbeNowWithL3(r.Context(), channelID, "admin_validate", apiKey); err != nil {
		writeError(w, r, http.StatusInternalServerError, "probe_failed", "Probe failed")
		return
	}
	item, err := s.repo.AdminPlatformChannel(r.Context(), channelID)
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "channel_unavailable", "Could not load channel")
		return
	}
	_ = s.repo.WriteAudit(r.Context(), store.AuditEvent{
		ActorType:  "user",
		ActorID:    user.ID,
		Action:     "platform_channel.validated",
		ObjectType: "channel",
		ObjectID:   channelID,
		IP:         clientIP(r),
		Result:     "success",
		Metadata:   map[string]any{"status": item.Status, "l1": item.L1Status, "l2": item.L2Status, "l3": item.L3Status},
	})
	writeJSON(w, http.StatusOK, map[string]any{"channel": item})
}

func (s *Server) disableAdminChannel(w http.ResponseWriter, r *http.Request) {
	user, _ := s.userFromRequest(r)
	channelID := chi.URLParam(r, "channelID")
	item, err := s.repo.DisablePlatformChannel(r.Context(), user.ID, channelID)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, r, http.StatusNotFound, "platform_channel_not_found", "Platform channel not found")
		return
	}
	if err != nil {
		writeError(w, r, http.StatusBadRequest, "platform_channel_disable_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"channel": item})
}

func (s *Server) addAdminChannelRecommendation(w http.ResponseWriter, r *http.Request) {
	user, _ := s.userFromRequest(r)
	channelID := chi.URLParam(r, "channelID")
	item, err := s.repo.AddChannelRecommendation(r.Context(), user.ID, channelID)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, r, http.StatusNotFound, "platform_channel_not_found", "Platform channel not found")
		return
	}
	if errors.Is(err, store.ErrInvalidRecommendConfig) {
		writeError(w, r, http.StatusBadRequest, "invalid_recommendation", err.Error())
		return
	}
	if err != nil {
		writeError(w, r, http.StatusBadRequest, "recommendation_add_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"channel": item})
}

func (s *Server) removeAdminChannelRecommendation(w http.ResponseWriter, r *http.Request) {
	user, _ := s.userFromRequest(r)
	channelID := chi.URLParam(r, "channelID")
	item, err := s.repo.RemoveChannelRecommendation(r.Context(), user.ID, channelID)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, r, http.StatusNotFound, "platform_channel_not_found", "Platform channel not found")
		return
	}
	if errors.Is(err, store.ErrInvalidRecommendConfig) {
		writeError(w, r, http.StatusBadRequest, "invalid_recommendation", err.Error())
		return
	}
	if err != nil {
		writeError(w, r, http.StatusBadRequest, "recommendation_remove_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"channel": item})
}

func (s *Server) deleteAdminChannel(w http.ResponseWriter, r *http.Request) {
	user, _ := s.userFromRequest(r)
	channelID := chi.URLParam(r, "channelID")
	err := s.repo.DeletePlatformChannel(r.Context(), user.ID, channelID)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, r, http.StatusNotFound, "platform_channel_not_found", "Platform channel not found")
		return
	}
	if err != nil {
		writeError(w, r, http.StatusBadRequest, "platform_channel_delete_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func (s *Server) bulkAdminChannels(w http.ResponseWriter, r *http.Request) {
	user, _ := s.userFromRequest(r)
	var req adminBulkRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid_json", "Invalid JSON body")
		return
	}
	if !hasBulkSelection(req.IDs) {
		writeError(w, r, http.StatusBadRequest, "empty_bulk_selection", "Select at least one platform channel")
		return
	}
	var items []store.AdminPlatformChannel
	var err error
	switch req.Action {
	case "status":
		switch req.Status {
		case "disabled":
			items, err = s.repo.BulkDisablePlatformChannels(r.Context(), user.ID, req.IDs)
		case "active", "enabled", "unknown":
			items, err = s.repo.BulkEnablePlatformChannels(r.Context(), user.ID, req.IDs)
		default:
			writeError(w, r, http.StatusBadRequest, "invalid_channel_bulk_status", "Only disabled or active status is supported for platform channel bulk updates")
			return
		}
	case "disable":
		items, err = s.repo.BulkDisablePlatformChannels(r.Context(), user.ID, req.IDs)
	case "enable":
		items, err = s.repo.BulkEnablePlatformChannels(r.Context(), user.ID, req.IDs)
	case "delete":
		items, err = s.repo.BulkDeletePlatformChannels(r.Context(), user.ID, req.IDs)
	default:
		writeError(w, r, http.StatusBadRequest, "invalid_bulk_action", "Unsupported bulk action")
		return
	}
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, r, http.StatusNotFound, "platform_channel_not_found", "One or more platform channels were not found")
		return
	}
	if err != nil {
		writeError(w, r, http.StatusBadRequest, "platform_channel_bulk_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "total": len(items)})
}

func (s *Server) adminProbeNow(w http.ResponseWriter, r *http.Request) {
	channelID := chi.URLParam(r, "channelID")
	if s.probeRunner == nil {
		writeError(w, r, http.StatusServiceUnavailable, "probe_unavailable", "Probe runtime is not available")
		return
	}
	if _, err := s.repo.AdminPlatformChannel(r.Context(), channelID); errors.Is(err, pgx.ErrNoRows) {
		writeError(w, r, http.StatusNotFound, "platform_channel_not_found", "Platform channel not found")
		return
	} else if err != nil {
		writeError(w, r, http.StatusInternalServerError, "channel_unavailable", "Could not load channel")
		return
	}
	apiKey, err := s.platformChannelAPIKey(r.Context(), channelID)
	if errors.Is(err, pgx.ErrNoRows) {
		err = s.probeRunner.ProbeNow(r.Context(), channelID, "manual")
	} else if err == nil {
		err = s.probeRunner.ProbeNowWithL3(r.Context(), channelID, "manual", apiKey)
	}
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "probe_failed", "Probe failed")
		return
	}
	item, err := s.repo.AdminPlatformChannel(r.Context(), channelID)
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "channel_unavailable", "Could not load channel")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"channel": item})
}

func (s *Server) verifyInteractiveAdminPassword(w http.ResponseWriter, r *http.Request, user store.PublicUser, password string, purpose string) bool {
	if _, ok := adminAgentFromContext(r.Context()); ok {
		// Admin-agent requests have already passed bearer authentication, scope checks,
		// write guards, idempotency recording, and audit context enrichment.
		return true
	}
	if s.auth == nil {
		writeError(w, r, http.StatusServiceUnavailable, "auth_unavailable", "Password verification is unavailable")
		return false
	}
	if strings.TrimSpace(password) == "" {
		writeError(w, r, http.StatusBadRequest, "password_required", "请输入当前登录密码")
		return false
	}
	if s.authLimiter == nil {
		s.authLimiter = &rateLimiter{buckets: map[string]rateBucket{}}
	}
	if !s.allowRate(s.authLimiter, purpose+":"+user.ID+":"+clientIP(r), 10, time.Minute) {
		writeError(w, r, http.StatusTooManyRequests, "rate_limited", "密码验证尝试过于频繁，请稍后再试")
		return false
	}
	if err := s.auth.VerifyPassword(r.Context(), user.ID, password); err != nil {
		status := http.StatusUnauthorized
		if !errors.Is(err, auth.ErrInvalidPassword) {
			status = http.StatusForbidden
		}
		writeError(w, r, status, "invalid_password", "密码不正确")
		return false
	}
	return true
}

func (s *Server) adminChannelExportRows(ctx context.Context, items []store.AdminPlatformChannel) ([][]string, error) {
	rows := make([][]string, 0, len(items))
	for _, item := range items {
		apiKey := ""
		if item.KeyFingerprint != "" && item.KeyMask != "" && item.KeyMask != "deleted" {
			plain, err := s.platformChannelAPIKey(ctx, item.ID)
			if errors.Is(err, pgx.ErrNoRows) {
				plain = ""
			} else if err != nil {
				return nil, err
			}
			apiKey = plain
		}
		providerConfig, err := json.Marshal(item.ProviderConfig)
		if err != nil {
			return nil, err
		}
		rows = append(rows, []string{
			item.ID,
			item.Name,
			item.Provider,
			item.Type,
			item.Model,
			item.UpstreamModel,
			item.Endpoint,
			item.OfficialSiteURL,
			item.IntroTitle,
			item.IntroSummary,
			item.IntroBody,
			introHighlightsCSV(item.IntroHighlights),
			item.LogoURL,
			item.IntroSourceURL,
			item.Status,
			strconv.Itoa(item.ProbeDaily),
			strconv.FormatBool(item.PublicVisible),
			strconv.FormatBool(item.GatewayEnabled),
			strconv.FormatFloat(item.InputPerMTok, 'f', -1, 64),
			strconv.FormatFloat(item.OutputPerMTok, 'f', -1, 64),
			string(providerConfig),
			apiKey,
			item.KeyMask,
			item.KeyFingerprint,
			formatOptionalTime(item.CredentialUpdatedAt),
			item.DataOrigin,
		})
	}
	return rows, nil
}

func (s *Server) adminChannelSyncItems(ctx context.Context, items []store.AdminPlatformChannel, includeCredentials bool) ([]channelSyncItem, error) {
	out := make([]channelSyncItem, 0, len(items))
	for _, item := range items {
		apiKey := ""
		if includeCredentials && item.KeyFingerprint != "" && item.KeyMask != "" && item.KeyMask != "deleted" {
			plain, err := s.platformChannelAPIKey(ctx, item.ID)
			if errors.Is(err, pgx.ErrNoRows) {
				plain = ""
			} else if err != nil {
				return nil, err
			}
			apiKey = plain
		}
		providerConfig := item.ProviderConfig
		if providerConfig == nil {
			providerConfig = map[string]any{}
		}
		out = append(out, channelSyncItem{
			ID:                  item.ID,
			Name:                item.Name,
			Provider:            item.Provider,
			Type:                item.Type,
			Model:               item.Model,
			UpstreamModel:       item.UpstreamModel,
			Endpoint:            item.Endpoint,
			OfficialSiteURL:     item.OfficialSiteURL,
			IntroTitle:          item.IntroTitle,
			IntroSummary:        item.IntroSummary,
			IntroBody:           item.IntroBody,
			IntroHighlights:     item.IntroHighlights,
			LogoURL:             item.LogoURL,
			IntroSourceURL:      item.IntroSourceURL,
			Status:              item.Status,
			Score:               item.Score,
			ProbeDaily:          item.ProbeDaily,
			PublicVisible:       item.PublicVisible,
			GatewayEnabled:      item.GatewayEnabled,
			InputPerMTok:        item.InputPerMTok,
			OutputPerMTok:       item.OutputPerMTok,
			ProviderConfig:      providerConfig,
			APIKey:              apiKey,
			KeyMask:             item.KeyMask,
			KeyFingerprint:      item.KeyFingerprint,
			CredentialUpdatedAt: item.CredentialUpdatedAt,
			Uptime24h:           item.Uptime24h,
			SuccessRate:         item.SuccessRate,
			LatencyP95Ms:        item.LatencyP95Ms,
			L1Status:            item.L1Status,
			L2Status:            item.L2Status,
			L3Status:            item.L3Status,
			L1LatencyMs:         item.L1LatencyMs,
			L2LatencyMs:         item.L2LatencyMs,
			L3LatencyMs:         item.L3LatencyMs,
			TokensUsed:          item.TokensUsed,
			CostUSD:             item.CostUSD,
			ErrorType:           item.ErrorType,
			LastProbeAt:         item.LastProbeAt,
			DataOrigin:          item.DataOrigin,
		})
	}
	return out, nil
}

func (s *Server) parseAdminChannelImportCSV(reader io.Reader) ([]store.PlatformChannelImportRow, error) {
	csvReader := csv.NewReader(reader)
	csvReader.FieldsPerRecord = -1
	csvReader.TrimLeadingSpace = true
	header, err := csvReader.Read()
	if err == io.EOF {
		return nil, errors.New("CSV 文件为空")
	}
	if err != nil {
		return nil, fmt.Errorf("读取表头失败: %w", err)
	}
	headerIndex := adminChannelCSVHeaderIndex(header)
	for _, name := range []string{"name", "model", "endpoint", "api_key"} {
		if _, ok := headerIndex[name]; !ok {
			return nil, fmt.Errorf("缺少必填列 %s", name)
		}
	}
	rows := []store.PlatformChannelImportRow{}
	seenIDs := map[string]int{}
	for rowNumber := 2; ; rowNumber++ {
		record, err := csvReader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("第 %d 行 CSV 格式错误: %w", rowNumber, err)
		}
		if adminChannelCSVRecordBlank(record) {
			continue
		}
		if len(rows) >= 1000 {
			return nil, errors.New("一次最多导入 1000 个平台通道")
		}
		row, err := s.parseAdminChannelImportRecord(headerIndex, record, rowNumber)
		if err != nil {
			return nil, err
		}
		if row.ID != "" {
			if firstRow, ok := seenIDs[row.ID]; ok {
				return nil, fmt.Errorf("第 %d 行与第 %d 行重复使用 id %s", rowNumber, firstRow, row.ID)
			}
			seenIDs[row.ID] = rowNumber
		}
		rows = append(rows, row)
	}
	if len(rows) == 0 {
		return nil, errors.New("CSV 没有可导入的数据行")
	}
	return rows, nil
}

func (s *Server) parseAdminChannelImportRecord(headerIndex map[string]int, record []string, rowNumber int) (store.PlatformChannelImportRow, error) {
	cell := func(name string) string {
		index, ok := headerIndex[name]
		if !ok || index >= len(record) {
			return ""
		}
		return strings.TrimSpace(record[index])
	}
	id := cell("id")
	if id != "" && (!strings.HasPrefix(id, "ch_") || len(id) > 128 || strings.ContainsAny(id, " \t\r\n")) {
		return store.PlatformChannelImportRow{}, fmt.Errorf("第 %d 行 id 必须为空或使用 ch_ 前缀且不包含空白字符", rowNumber)
	}
	probeDaily, err := parseCSVInt(cell("probe_daily"), 2880, "probe_daily", rowNumber)
	if err != nil {
		return store.PlatformChannelImportRow{}, err
	}
	publicVisible, err := parseCSVBool(cell("public_visible"), true, "public_visible", rowNumber)
	if err != nil {
		return store.PlatformChannelImportRow{}, err
	}
	gatewayEnabled, err := parseCSVBool(cell("gateway_enabled"), true, "gateway_enabled", rowNumber)
	if err != nil {
		return store.PlatformChannelImportRow{}, err
	}
	inputPerMTok, err := parseCSVFloat(cell("input_per_mtok"), 0, "input_per_mtok", rowNumber)
	if err != nil {
		return store.PlatformChannelImportRow{}, err
	}
	outputPerMTok, err := parseCSVFloat(cell("output_per_mtok"), 0, "output_per_mtok", rowNumber)
	if err != nil {
		return store.PlatformChannelImportRow{}, err
	}
	providerConfig, err := parseCSVProviderConfig(cell("provider_config_json"), rowNumber)
	if err != nil {
		return store.PlatformChannelImportRow{}, err
	}
	introHighlights, err := parseCSVIntroHighlights(cell("intro_highlights_json"), rowNumber)
	if err != nil {
		return store.PlatformChannelImportRow{}, err
	}
	enabled, err := importStatusEnabled(cell("status"), rowNumber)
	if err != nil {
		return store.PlatformChannelImportRow{}, err
	}
	req := adminPlatformChannelRequest{
		Name:            cell("name"),
		Provider:        cell("provider"),
		Type:            cell("type"),
		Model:           cell("model"),
		UpstreamModel:   cell("upstream_model"),
		Endpoint:        cell("endpoint"),
		OfficialSiteURL: cell("official_site_url"),
		IntroTitle:      cell("intro_title"),
		IntroSummary:    cell("intro_summary"),
		IntroBody:       cell("intro_body"),
		IntroHighlights: introHighlights,
		LogoURL:         cell("logo_url"),
		IntroSourceURL:  cell("intro_source_url"),
		APIKey:          cell("api_key"),
		ProbeDaily:      probeDaily,
		PublicVisible:   &publicVisible,
		GatewayEnabled:  &gatewayEnabled,
		Enabled:         &enabled,
		InputPerMTok:    &inputPerMTok,
		OutputPerMTok:   &outputPerMTok,
		ProviderConfig:  providerConfig,
	}
	input, err := s.adminPlatformChannelInputFromRequest(req, true)
	if err != nil {
		return store.PlatformChannelImportRow{}, fmt.Errorf("第 %d 行无效: %w", rowNumber, err)
	}
	return store.PlatformChannelImportRow{RowNumber: rowNumber, ID: id, Input: input}, nil
}

func (s *Server) adminPlatformChannelInput(r *http.Request, requireKey bool) (store.PlatformChannelInput, error) {
	var req adminPlatformChannelRequest
	if err := decodeJSON(r, &req); err != nil {
		return store.PlatformChannelInput{}, errors.New("invalid JSON body")
	}
	return s.adminPlatformChannelInputFromRequest(req, requireKey)
}

func (s *Server) adminPlatformChannelInputsFromRequest(req adminPlatformChannelRequest) ([]store.PlatformChannelInput, error) {
	models := selectedAdminMonitorModels(req.MonitorModels)
	if len(models) == 0 {
		input, err := s.adminPlatformChannelInputFromRequest(req, true)
		if err != nil {
			return nil, err
		}
		return []store.PlatformChannelInput{input}, nil
	}
	inputs := make([]store.PlatformChannelInput, 0, len(models))
	for _, model := range models {
		modelReq := req
		modelReq.Name = platformChannelMonitorName(req.Name, model, len(models))
		if strings.TrimSpace(modelReq.Type) == "" {
			modelReq.Type = model.Type
		}
		modelReq.Model = model.Model
		modelReq.UpstreamModel = model.UpstreamModel
		inputPrice := model.InputPerMTok
		outputPrice := model.OutputPerMTok
		modelReq.InputPerMTok = &inputPrice
		modelReq.OutputPerMTok = &outputPrice
		modelReq.MonitorModels = nil
		input, err := s.adminPlatformChannelInputFromRequest(modelReq, true)
		if err != nil {
			return nil, err
		}
		inputs = append(inputs, input)
	}
	return inputs, nil
}

func selectedAdminMonitorModels(models []store.MonitorModelConfig) []store.MonitorModelConfig {
	out := make([]store.MonitorModelConfig, 0, len(models))
	seen := map[string]bool{}
	for _, model := range models {
		model.Model = strings.TrimSpace(model.Model)
		model.UpstreamModel = strings.TrimSpace(model.UpstreamModel)
		model.Type = strings.TrimSpace(model.Type)
		model.Label = strings.TrimSpace(model.Label)
		model.Key = strings.TrimSpace(model.Key)
		if model.Model == "" {
			continue
		}
		if model.UpstreamModel == "" {
			model.UpstreamModel = model.Model
		}
		if model.Type == "" {
			model.Type = "openai-compatible"
		}
		if model.Key == "" {
			model.Key = model.Model
		}
		if model.Label == "" {
			model.Label = model.Model
		}
		if model.InputPerMTok < 0 {
			model.InputPerMTok = 0
		}
		if model.OutputPerMTok < 0 {
			model.OutputPerMTok = 0
		}
		key := strings.ToLower(model.Key)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, model)
	}
	return out
}

func platformChannelMonitorName(base string, model store.MonitorModelConfig, total int) string {
	base = strings.TrimSpace(base)
	if base == "" {
		base = strings.TrimSpace(model.Label)
	}
	if total <= 1 {
		return base
	}
	suffix := strings.TrimSpace(model.Label)
	if suffix == "" {
		suffix = strings.TrimSpace(model.Model)
	}
	lowerBase := strings.ToLower(base)
	if strings.Contains(lowerBase, strings.ToLower(suffix)) || strings.Contains(lowerBase, strings.ToLower(model.Model)) {
		return base
	}
	return base + " · " + suffix
}

func (s *Server) adminPlatformChannelInputFromRequest(req adminPlatformChannelRequest, requireKey bool) (store.PlatformChannelInput, error) {
	name := strings.TrimSpace(req.Name)
	provider := strings.TrimSpace(req.Provider)
	typ := strings.TrimSpace(req.Type)
	model := strings.TrimSpace(req.Model)
	upstreamModel := strings.TrimSpace(req.UpstreamModel)
	endpoint, err := cleanBaseEndpoint(req.Endpoint)
	if err != nil {
		return store.PlatformChannelInput{}, err
	}
	officialSiteURL, err := cleanOptionalWebsiteURL(req.OfficialSiteURL)
	if err != nil {
		return store.PlatformChannelInput{}, err
	}
	introTitle, err := cleanOptionalTextField(req.IntroTitle, "introTitle", 160, false)
	if err != nil {
		return store.PlatformChannelInput{}, err
	}
	introSummary, err := cleanOptionalTextField(req.IntroSummary, "introSummary", 360, false)
	if err != nil {
		return store.PlatformChannelInput{}, err
	}
	introBody, err := cleanOptionalTextField(req.IntroBody, "introBody", 4000, true)
	if err != nil {
		return store.PlatformChannelInput{}, err
	}
	introHighlights, err := cleanIntroHighlights(req.IntroHighlights)
	if err != nil {
		return store.PlatformChannelInput{}, err
	}
	logoURL, err := cleanOptionalHTTPURL(req.LogoURL, "logoUrl")
	if err != nil {
		return store.PlatformChannelInput{}, err
	}
	introSourceURL, err := cleanOptionalHTTPURL(req.IntroSourceURL, "introSourceUrl")
	if err != nil {
		return store.PlatformChannelInput{}, err
	}
	if provider == "" {
		provider = "OpenAI"
	}
	if typ == "" {
		typ = "openai-compatible"
	}
	if upstreamModel == "" {
		upstreamModel = model
	}
	if name == "" || model == "" || endpoint == "" {
		return store.PlatformChannelInput{}, errors.New("name, endpoint and model are required")
	}
	publicVisible := true
	if req.PublicVisible != nil {
		publicVisible = *req.PublicVisible
	}
	gatewayEnabled := true
	if req.GatewayEnabled != nil {
		gatewayEnabled = *req.GatewayEnabled
	}
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	inputPerMTok := 0.0
	if req.InputPerMTok != nil {
		inputPerMTok = *req.InputPerMTok
	}
	outputPerMTok := 0.0
	if req.OutputPerMTok != nil {
		outputPerMTok = *req.OutputPerMTok
	}
	providerConfig, err := sanitizeProviderConfig(req.ProviderConfig)
	if err != nil {
		return store.PlatformChannelInput{}, err
	}
	if !requireKey && strings.TrimSpace(req.APIKey) != "" {
		return store.PlatformChannelInput{}, errors.New("use the credentials endpoint to rotate apiKey")
	}
	credential, err := s.encryptAdminChannelKey(req.APIKey, requireKey)
	if err != nil {
		return store.PlatformChannelInput{}, err
	}
	return store.PlatformChannelInput{
		Name:            name,
		Provider:        provider,
		Type:            typ,
		Model:           model,
		UpstreamModel:   upstreamModel,
		Endpoint:        endpoint,
		OfficialSiteURL: officialSiteURL,
		IntroTitle:      introTitle,
		IntroSummary:    introSummary,
		IntroBody:       introBody,
		IntroHighlights: introHighlights,
		LogoURL:         logoURL,
		IntroSourceURL:  introSourceURL,
		ProbeDaily:      req.ProbeDaily,
		PublicVisible:   publicVisible,
		GatewayEnabled:  gatewayEnabled,
		Enabled:         enabled,
		InputPerMTok:    inputPerMTok,
		OutputPerMTok:   outputPerMTok,
		ProviderConfig:  providerConfig,
		Credential:      credential,
	}, nil
}

func adminChannelCSVHeaderIndex(header []string) map[string]int {
	out := map[string]int{}
	for i, raw := range header {
		name := strings.TrimSpace(raw)
		if i == 0 {
			name = strings.TrimPrefix(name, "\ufeff")
		}
		if name != "" {
			out[name] = i
		}
	}
	return out
}

func adminChannelCSVRecordBlank(record []string) bool {
	for _, value := range record {
		if strings.TrimSpace(value) != "" {
			return false
		}
	}
	return true
}

func parseCSVBool(value string, fallback bool, name string, rowNumber int) (bool, error) {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return fallback, nil
	}
	switch value {
	case "true", "1", "yes", "y", "on", "是", "启用":
		return true, nil
	case "false", "0", "no", "n", "off", "否", "禁用":
		return false, nil
	default:
		return false, fmt.Errorf("第 %d 行 %s 必须是 true/false", rowNumber, name)
	}
}

func parseCSVInt(value string, fallback int, name string, rowNumber int) (int, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback, nil
	}
	n, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("第 %d 行 %s 必须是整数", rowNumber, name)
	}
	return n, nil
}

func parseCSVFloat(value string, fallback float64, name string, rowNumber int) (float64, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback, nil
	}
	n, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return 0, fmt.Errorf("第 %d 行 %s 必须是数字", rowNumber, name)
	}
	return n, nil
}

func parseCSVProviderConfig(value string, rowNumber int) (map[string]any, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return map[string]any{}, nil
	}
	var raw map[string]any
	if err := json.Unmarshal([]byte(value), &raw); err != nil {
		return nil, fmt.Errorf("第 %d 行 provider_config_json 必须是 JSON object", rowNumber)
	}
	if raw == nil {
		return map[string]any{}, nil
	}
	return raw, nil
}

func parseCSVIntroHighlights(value string, rowNumber int) ([]string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return []string{}, nil
	}
	var raw []string
	if err := json.Unmarshal([]byte(value), &raw); err != nil {
		return nil, fmt.Errorf("第 %d 行 intro_highlights_json 必须是 JSON string array", rowNumber)
	}
	highlights, err := cleanIntroHighlights(raw)
	if err != nil {
		return nil, fmt.Errorf("第 %d 行 %w", rowNumber, err)
	}
	return highlights, nil
}

func introHighlightsCSV(values []string) string {
	if len(values) == 0 {
		return ""
	}
	raw, err := json.Marshal(values)
	if err != nil {
		return ""
	}
	return string(raw)
}

func importStatusEnabled(value string, rowNumber int) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "unknown", "healthy", "degraded", "auth_error", "connectivity_down", "functional_down", "active", "enabled":
		return true, nil
	case "disabled":
		return false, nil
	default:
		return false, fmt.Errorf("第 %d 行 status 不支持导入为 %q", rowNumber, value)
	}
}

func formatOptionalTime(value *time.Time) string {
	if value == nil {
		return ""
	}
	return value.UTC().Format(time.RFC3339)
}

func cleanOptionalWebsiteURL(raw string) (string, error) {
	return cleanOptionalHTTPURL(raw, "officialSiteUrl")
}

func cleanOptionalHTTPURL(raw string, field string) (string, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return "", nil
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return "", fmt.Errorf("%s must be an http or https URL", field)
	}
	if parsed.User != nil {
		return "", fmt.Errorf("%s must not include credentials", field)
	}
	parsed.Fragment = ""
	return parsed.String(), nil
}

func cleanOptionalTextField(raw string, field string, maxRunes int, multiline bool) (string, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return "", nil
	}
	if multiline {
		value = normalizeAdminMultilineText(value)
	} else {
		value = strings.Join(strings.Fields(value), " ")
	}
	if len([]rune(value)) > maxRunes {
		return "", fmt.Errorf("%s must be at most %d characters", field, maxRunes)
	}
	return value, nil
}

func cleanIntroHighlights(raw []string) ([]string, error) {
	if len(raw) > 8 {
		return nil, errors.New("introHighlights supports at most 8 items")
	}
	out := make([]string, 0, len(raw))
	seen := map[string]bool{}
	for _, value := range raw {
		value = strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
		if value == "" {
			continue
		}
		if len([]rune(value)) > 120 {
			return nil, errors.New("introHighlights item must be at most 120 characters")
		}
		key := strings.ToLower(value)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, value)
	}
	return out, nil
}

func normalizeAdminMultilineText(value string) string {
	value = strings.ReplaceAll(value, "\r\n", "\n")
	value = strings.ReplaceAll(value, "\r", "\n")
	lines := strings.Split(value, "\n")
	out := make([]string, 0, len(lines))
	blank := false
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			if !blank && len(out) > 0 {
				out = append(out, "")
				blank = true
			}
			continue
		}
		out = append(out, line)
		blank = false
	}
	for len(out) > 0 && out[len(out)-1] == "" {
		out = out[:len(out)-1]
	}
	return strings.Join(out, "\n")
}

func cleanBaseEndpoint(raw string) (string, error) {
	endpoint := strings.TrimRight(strings.TrimSpace(raw), "/")
	parsed, err := url.Parse(endpoint)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return "", errors.New("endpoint must be an http or https URL")
	}
	if parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", errors.New("endpoint must be a base URL without credentials, query or fragment")
	}
	return endpoint, nil
}

func sanitizeProviderConfig(raw map[string]any) (map[string]any, error) {
	if len(raw) == 0 {
		return map[string]any{}, nil
	}
	out := map[string]any{}
	for key, value := range raw {
		switch strings.TrimSpace(key) {
		case "temperature":
			n, ok := boundedFloat(value, 0, 2)
			if !ok {
				return nil, errors.New("providerConfig.temperature must be between 0 and 2")
			}
			out["temperature"] = n
		case "topP":
			n, ok := boundedFloat(value, 0, 1)
			if !ok {
				return nil, errors.New("providerConfig.topP must be between 0 and 1")
			}
			out["topP"] = n
		case "topK":
			n, ok := boundedInt(value, 1, 1000)
			if !ok {
				return nil, errors.New("providerConfig.topK must be between 1 and 1000")
			}
			out["topK"] = n
		case "maxTokens":
			n, ok := boundedInt(value, 1, 200000)
			if !ok {
				return nil, errors.New("providerConfig.maxTokens must be between 1 and 200000")
			}
			out["maxTokens"] = n
		case "timeoutMs":
			n, ok := boundedInt(value, 1000, 300000)
			if !ok {
				return nil, errors.New("providerConfig.timeoutMs must be between 1000 and 300000")
			}
			out["timeoutMs"] = n
		case "authHeader":
			text, ok := value.(string)
			if !ok {
				return nil, errors.New("providerConfig.authHeader must be authorization or x-api-key")
			}
			switch strings.ToLower(strings.TrimSpace(text)) {
			case "authorization", "bearer":
				out["authHeader"] = "authorization"
			case "x-api-key", "x-api-key-header":
				out["authHeader"] = "x-api-key"
			default:
				return nil, errors.New("providerConfig.authHeader must be authorization or x-api-key")
			}
		case "clientProfile":
			text, err := cleanProviderConfigString(value, "providerConfig.clientProfile", 64)
			if err != nil {
				return nil, err
			}
			switch strings.ToLower(strings.ReplaceAll(text, "_", "-")) {
			case "claude-code":
				out["clientProfile"] = "claude-code"
			default:
				return nil, errors.New("providerConfig.clientProfile must be claude-code")
			}
		case "clientVersion":
			text, err := cleanProviderConfigString(value, "providerConfig.clientVersion", 32)
			if err != nil {
				return nil, err
			}
			if strings.ContainsAny(text, " \t\r\n/") {
				return nil, errors.New("providerConfig.clientVersion must not include whitespace or slash")
			}
			out["clientVersion"] = text
		case "l2ProbeMode", "modelsProbeMode":
			text, err := cleanProviderConfigString(value, "providerConfig."+strings.TrimSpace(key), 32)
			if err != nil {
				return nil, err
			}
			mode := strings.ToLower(strings.ReplaceAll(text, "-", "_"))
			switch mode {
			case "skip", "disabled", "off", "l3_only", "l3only":
				out[strings.TrimSpace(key)] = "skip"
			case "required", "on", "enabled":
				out[strings.TrimSpace(key)] = "required"
			default:
				return nil, errors.New("providerConfig.modelsProbeMode must be skip or required")
			}
		case "l3ProbeMode":
			text, err := cleanProviderConfigString(value, "providerConfig.l3ProbeMode", 32)
			if err != nil {
				return nil, err
			}
			mode := strings.ToLower(strings.ReplaceAll(text, "-", "_"))
			switch mode {
			case "generate":
				out["l3ProbeMode"] = "generate"
			case "l2_only", "skip":
				out["l3ProbeMode"] = "l2_only"
			default:
				return nil, errors.New("providerConfig.l3ProbeMode must be generate or l2_only")
			}
		case "l3ProbeMaxTokens":
			n, ok := boundedInt(value, 1, 8)
			if !ok {
				return nil, errors.New("providerConfig.l3ProbeMaxTokens must be between 1 and 8")
			}
			out["l3ProbeMaxTokens"] = n
		case "l3ContentPolicy":
			text, err := cleanProviderConfigString(value, "providerConfig.l3ContentPolicy", 32)
			if err != nil {
				return nil, err
			}
			switch strings.ToLower(strings.TrimSpace(text)) {
			case "exact":
				out["l3ContentPolicy"] = "exact"
			case "non_empty", "non-empty":
				out["l3ContentPolicy"] = "non_empty"
			default:
				return nil, errors.New("providerConfig.l3ContentPolicy must be exact or non_empty")
			}
		case "l3ExpectedContent":
			text, err := cleanProviderConfigString(value, "providerConfig.l3ExpectedContent", 256)
			if err != nil {
				return nil, err
			}
			out["l3ExpectedContent"] = text
		case "l3WarnMs", "l3WarnThresholdMs":
			n, ok := boundedInt(value, 500, 600000)
			if !ok {
				return nil, errors.New("providerConfig.l3WarnMs must be between 500 and 600000")
			}
			out[strings.TrimSpace(key)] = n
		case "stop":
			stop, ok := stringList(value, 16, 200)
			if !ok {
				return nil, errors.New("providerConfig.stop must be a string or string array")
			}
			if len(stop) > 0 {
				out["stop"] = stop
			}
		default:
			return nil, errors.New("providerConfig supports only temperature, topP, topK, maxTokens, timeoutMs, authHeader, clientProfile, clientVersion, modelsProbeMode, l2ProbeMode, l3ProbeMode, l3ProbeMaxTokens, l3ContentPolicy, l3ExpectedContent, l3WarnMs, l3WarnThresholdMs and stop")
		}
	}
	return out, nil
}

func cleanProviderConfigString(value any, field string, maxRunes int) (string, error) {
	text, ok := value.(string)
	if !ok {
		return "", fmt.Errorf("%s must be a string", field)
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return "", fmt.Errorf("%s must not be empty", field)
	}
	if len([]rune(text)) > maxRunes {
		return "", fmt.Errorf("%s must be at most %d characters", field, maxRunes)
	}
	return text, nil
}

func boundedFloat(value any, min float64, max float64) (float64, bool) {
	var n float64
	switch v := value.(type) {
	case float64:
		n = v
	case float32:
		n = float64(v)
	case int:
		n = float64(v)
	case int64:
		n = float64(v)
	default:
		return 0, false
	}
	if n < min || n > max {
		return 0, false
	}
	return n, true
}

func boundedInt(value any, min int, max int) (int, bool) {
	var n int
	switch v := value.(type) {
	case float64:
		if v != float64(int(v)) {
			return 0, false
		}
		n = int(v)
	case int:
		n = v
	case int64:
		n = int(v)
	default:
		return 0, false
	}
	if n < min || n > max {
		return 0, false
	}
	return n, true
}

func stringList(value any, maxItems int, maxLen int) ([]string, bool) {
	if text, ok := value.(string); ok {
		text = strings.TrimSpace(text)
		if text == "" {
			return nil, true
		}
		if len([]rune(text)) > maxLen {
			return nil, false
		}
		return []string{text}, true
	}
	values, ok := value.([]any)
	if !ok || len(values) > maxItems {
		return nil, false
	}
	out := []string{}
	for _, item := range values {
		text, ok := item.(string)
		if !ok {
			return nil, false
		}
		text = strings.TrimSpace(text)
		if text == "" {
			continue
		}
		if len([]rune(text)) > maxLen {
			return nil, false
		}
		out = append(out, text)
	}
	return out, true
}

func (s *Server) encryptAdminChannelKey(apiKey string, required bool) (store.EncryptedCredential, error) {
	apiKey = strings.TrimSpace(apiKey)
	if apiKey == "" {
		if required {
			return store.EncryptedCredential{}, errors.New("apiKey is required")
		}
		return store.EncryptedCredential{}, nil
	}
	if s.secretBox == nil {
		return store.EncryptedCredential{}, errors.New("encryption is unavailable")
	}
	encrypted, err := s.secretBox.Encrypt(apiKey)
	if err != nil {
		return store.EncryptedCredential{}, err
	}
	return store.EncryptedCredential{Ciphertext: encrypted.Ciphertext, Nonce: encrypted.Nonce, Fingerprint: encrypted.Fingerprint, Mask: encrypted.Mask}, nil
}

func (s *Server) platformChannelAPIKey(ctx context.Context, channelID string) (string, error) {
	cred, err := s.repo.PlatformCredential(ctx, channelID)
	if err != nil {
		return "", err
	}
	if s.secretBox == nil {
		return "", errors.New("encryption is unavailable")
	}
	return s.secretBox.Decrypt(cred.Ciphertext, cred.Nonce)
}
