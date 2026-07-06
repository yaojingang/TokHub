package api

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	gatewaycache "tokhub/internal/gateway"
	"tokhub/internal/store"
)

type recommendClickRequest struct {
	ItemType  string `json:"itemType"`
	ItemID    string `json:"itemId"`
	ChannelID string `json:"channelId"`
}

type createOpenAPISiteRequest struct {
	Name     string   `json:"name"`
	Scopes   []string `json:"scopes"`
	QPSLimit int      `json:"qpsLimit"`
}

func (s *Server) publicRecommend(w http.ResponseWriter, r *http.Request) {
	cfg, err := s.repo.RecommendConfig(r.Context())
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "recommend_unavailable", "Could not load recommendations")
		return
	}
	writeJSON(w, http.StatusOK, cfg)
}

func (s *Server) trackRecommendClick(w http.ResponseWriter, r *http.Request) {
	var req recommendClickRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid_json", "Invalid JSON body")
		return
	}
	var userID string
	if user, err := s.userFromRequest(r); err == nil {
		userID = user.ID
	}
	if err := s.repo.TrackRecommendClick(r.Context(), req.ItemType, req.ItemID, req.ChannelID, userID, clientIP(r), r.UserAgent()); err != nil {
		writeError(w, r, http.StatusInternalServerError, "recommend_click_failed", "Could not track recommendation click")
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]string{"status": "tracked"})
}

func (s *Server) adminRecommend(w http.ResponseWriter, r *http.Request) {
	data, err := s.repo.RecommendAdminData(r.Context())
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "recommend_unavailable", "Could not load recommendation config")
		return
	}
	writeJSON(w, http.StatusOK, data)
}

func (s *Server) saveAdminRecommend(w http.ResponseWriter, r *http.Request) {
	user, _ := s.userFromRequest(r)
	var req store.RecommendSaveInput
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid_json", "Invalid JSON body")
		return
	}
	if err := s.repo.SaveRecommendConfig(r.Context(), req, user.ID); err != nil {
		if errors.Is(err, store.ErrInvalidRecommendConfig) {
			writeError(w, r, http.StatusBadRequest, "recommend_config_invalid", err.Error())
			return
		}
		writeError(w, r, http.StatusInternalServerError, "recommend_save_failed", "Could not save recommendation config")
		return
	}
	data, err := s.repo.RecommendAdminData(r.Context())
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "recommend_unavailable", "Could not reload recommendation config")
		return
	}
	writeJSON(w, http.StatusOK, data)
}

func (s *Server) adminWebConfig(w http.ResponseWriter, r *http.Request) {
	cfg, err := s.repo.SiteConfig(r.Context())
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "web_config_unavailable", "Could not load site config")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"site": cfg})
}

func (s *Server) saveAdminWebConfig(w http.ResponseWriter, r *http.Request) {
	user, _ := s.userFromRequest(r)
	var req store.SiteConfig
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid_json", "Invalid JSON body")
		return
	}
	current, err := s.repo.SiteConfig(r.Context())
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "web_config_unavailable", "Could not load site config")
		return
	}
	if req.PublicURL == "" {
		req.PublicURL = current.PublicURL
	}
	if req.DefaultGatewayPolicy == "" {
		req.DefaultGatewayPolicy = current.DefaultGatewayPolicy
	}
	if req.Timezone == "" {
		req.Timezone = current.Timezone
	}
	if len(req.MonitorModels) == 0 {
		req.MonitorModels = current.MonitorModels
	}
	req.RegistrationOpen = current.RegistrationOpen
	if req.ShowRegisterCTA && !req.RegistrationOpen {
		req.ShowRegisterCTA = false
	}
	req, validationError := cleanSiteConfigInput(req)
	if validationError != "" {
		writeError(w, r, http.StatusBadRequest, "web_config_invalid", validationError)
		return
	}
	if err := s.repo.SetSiteConfigBy(r.Context(), req, user.ID); err != nil {
		writeError(w, r, http.StatusInternalServerError, "web_config_save_failed", "Could not save site config")
		return
	}
	_ = s.repo.WriteAudit(r.Context(), store.AuditEvent{
		ActorType:  "user",
		ActorID:    user.ID,
		Action:     "site.web_config.updated",
		ObjectType: "site_config",
		ObjectID:   "site",
		IP:         clientIP(r),
		Result:     "success",
		Metadata:   map[string]any{"nav_items": len(req.NavItems), "footer_links": len(req.FooterLinks), "brand": req.BrandName},
	})
	cfg, _ := s.repo.SiteConfig(r.Context())
	writeJSON(w, http.StatusOK, map[string]any{"site": cfg})
}

func (s *Server) resetAdminWebConfig(w http.ResponseWriter, r *http.Request) {
	user, _ := s.userFromRequest(r)
	current, err := s.repo.SiteConfig(r.Context())
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "web_config_unavailable", "Could not load site config")
		return
	}
	cfg := store.DefaultSiteConfig(current.PublicURL, current.RegistrationOpen)
	cfg.EmailVerificationRequired = current.EmailVerificationRequired
	cfg.DefaultGatewayPolicy = current.DefaultGatewayPolicy
	cfg.Timezone = current.Timezone
	cfg.MonitorModels = current.MonitorModels
	if err := s.repo.SetSiteConfigBy(r.Context(), cfg, user.ID); err != nil {
		writeError(w, r, http.StatusInternalServerError, "web_config_reset_failed", "Could not reset site config")
		return
	}
	_ = s.repo.WriteAudit(r.Context(), store.AuditEvent{
		ActorType:  "user",
		ActorID:    user.ID,
		Action:     "site.web_config.reset",
		ObjectType: "site_config",
		ObjectID:   "site",
		IP:         clientIP(r),
		Result:     "success",
		Metadata:   map[string]any{"nav_items": len(cfg.NavItems), "footer_links": len(cfg.FooterLinks), "brand": cfg.BrandName},
	})
	writeJSON(w, http.StatusOK, map[string]any{"site": cfg})
}

func cleanSiteConfigInput(cfg store.SiteConfig) (store.SiteConfig, string) {
	cfg.BrandName = strings.TrimSpace(cfg.BrandName)
	cfg.LogoMark = strings.TrimSpace(cfg.LogoMark)
	cfg.Subtitle = strings.TrimSpace(cfg.Subtitle)
	cfg.FooterText = strings.TrimSpace(cfg.FooterText)
	cfg.DefaultGatewayPolicy = strings.TrimSpace(cfg.DefaultGatewayPolicy)
	cfg.Timezone = strings.TrimSpace(cfg.Timezone)
	if cfg.BrandName == "" {
		return cfg, "站点名称不能为空"
	}
	if len([]rune(cfg.BrandName)) > 40 {
		return cfg, "站点名称不能超过 40 个字符"
	}
	if cfg.LogoMark == "" || len([]rune(cfg.LogoMark)) > 2 {
		return cfg, "Logo 标记必须是 1-2 个字符"
	}
	if len([]rune(cfg.Subtitle)) > 80 {
		return cfg, "副标题不能超过 80 个字符"
	}
	switch cfg.DefaultGatewayPolicy {
	case "", "latency", "success", "cost":
		if cfg.DefaultGatewayPolicy == "" {
			cfg.DefaultGatewayPolicy = "latency"
		}
	default:
		return cfg, "默认路由策略不合法"
	}
	if cfg.Timezone == "" {
		cfg.Timezone = "Asia/Shanghai"
	}
	if len([]rune(cfg.Timezone)) > 64 || strings.ContainsAny(cfg.Timezone, " \t\r\n") {
		return cfg, "时区格式不合法"
	}
	navItems, errText := cleanNavItems(cfg.NavItems, 1, 5, "顶部导航")
	if errText != "" {
		return cfg, errText
	}
	footerLinks, errText := cleanNavItems(cfg.FooterLinks, 1, 8, "页脚链接")
	if errText != "" {
		return cfg, errText
	}
	monitorModels, errText := cleanMonitorModels(cfg.MonitorModels)
	if errText != "" {
		return cfg, errText
	}
	cfg.NavItems = navItems
	cfg.FooterLinks = footerLinks
	cfg.MonitorModels = monitorModels
	return cfg, ""
}

func cleanMonitorModels(items []store.MonitorModelConfig) ([]store.MonitorModelConfig, string) {
	if len(items) == 0 {
		return store.DefaultMonitorModels(), ""
	}
	if len(items) > 8 {
		return nil, "监控模型最多配置 8 个"
	}
	out := make([]store.MonitorModelConfig, 0, len(items))
	seen := map[string]bool{}
	for i, item := range items {
		item.Key = strings.TrimSpace(item.Key)
		item.Label = strings.TrimSpace(item.Label)
		item.Model = strings.TrimSpace(item.Model)
		item.UpstreamModel = strings.TrimSpace(item.UpstreamModel)
		item.Type = strings.TrimSpace(item.Type)
		if item.Model == "" {
			return nil, "监控模型第 " + strconv.Itoa(i+1) + " 项缺少模型 ID"
		}
		if item.Key == "" {
			item.Key = item.Model
		}
		if item.Label == "" {
			item.Label = item.Model
		}
		if item.UpstreamModel == "" {
			item.UpstreamModel = item.Model
		}
		if len([]rune(item.Label)) > 40 {
			return nil, "监控模型第 " + strconv.Itoa(i+1) + " 项名称过长"
		}
		if !validMonitorModelKey(item.Key) || !validMonitorModelKey(item.Model) || !validMonitorModelKey(item.UpstreamModel) {
			return nil, "监控模型第 " + strconv.Itoa(i+1) + " 项模型 ID 格式不合法"
		}
		switch item.Type {
		case "openai-compatible", "openai", "anthropic", "gemini", "google":
		default:
			return nil, "监控模型第 " + strconv.Itoa(i+1) + " 项 Adapter 不合法"
		}
		if item.InputPerMTok < 0 || item.OutputPerMTok < 0 {
			return nil, "监控模型第 " + strconv.Itoa(i+1) + " 项价格不能为负数"
		}
		aliases, errText := cleanMonitorModelAliases(item.Aliases, item.Model, i+1)
		if errText != "" {
			return nil, errText
		}
		item.Aliases = aliases
		key := strings.ToLower(item.Key)
		if seen[key] {
			return nil, "监控模型 key 不能重复"
		}
		seen[key] = true
		out = append(out, item)
	}
	return out, ""
}

func cleanMonitorModelAliases(items []string, model string, row int) ([]string, string) {
	if len(items) > 12 {
		return nil, "监控模型第 " + strconv.Itoa(row) + " 项别名过多"
	}
	seen := map[string]bool{}
	out := []string{}
	for _, item := range append([]string{model}, items...) {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if !validMonitorModelKey(item) {
			return nil, "监控模型第 " + strconv.Itoa(row) + " 项别名格式不合法"
		}
		key := strings.ToLower(item)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, item)
	}
	return out, ""
}

func validMonitorModelKey(value string) bool {
	if value == "" || len(value) > 120 || strings.ContainsAny(value, " \t\r\n") {
		return false
	}
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' || r == '.' || r == ':' || r == '/' {
			continue
		}
		return false
	}
	return true
}

func cleanNavItems(items []store.NavItem, minItems int, maxItems int, label string) ([]store.NavItem, string) {
	if len(items) < minItems || len(items) > maxItems {
		return nil, label + "数量不合法"
	}
	out := make([]store.NavItem, 0, len(items))
	for i, item := range items {
		item.Label = strings.TrimSpace(item.Label)
		item.Href = strings.TrimSpace(item.Href)
		if item.Label == "" {
			return nil, label + "第 " + strconv.Itoa(i+1) + " 项缺少名称"
		}
		if len([]rune(item.Label)) > 24 {
			return nil, label + "第 " + strconv.Itoa(i+1) + " 项名称过长"
		}
		if !validSiteHref(item.Href) {
			return nil, label + "第 " + strconv.Itoa(i+1) + " 项链接必须是站内路径或 http(s) 链接"
		}
		out = append(out, item)
	}
	return out, ""
}

func validSiteHref(href string) bool {
	if strings.HasPrefix(href, "/") && !strings.HasPrefix(href, "//") {
		return true
	}
	lower := strings.ToLower(href)
	return strings.HasPrefix(lower, "https://") || strings.HasPrefix(lower, "http://")
}

func (s *Server) adminOpenAPI(w http.ResponseWriter, r *http.Request) {
	summary, err := s.repo.OpenAPISummary(r.Context(), s.cfg.PublicURL)
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "open_api_unavailable", "Could not load Open API config")
		return
	}
	writeJSON(w, http.StatusOK, summary)
}

func (s *Server) createOpenAPISite(w http.ResponseWriter, r *http.Request) {
	user, _ := s.userFromRequest(r)
	var req createOpenAPISiteRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid_json", "Invalid JSON body")
		return
	}
	site, err := s.repo.CreateOpenAPISite(r.Context(), store.OpenAPISiteCreateInput{
		Name:     req.Name,
		Scopes:   req.Scopes,
		QPSLimit: req.QPSLimit,
		ActorID:  user.ID,
	})
	if errors.Is(err, store.ErrInvalidOpenAPIScope) {
		writeError(w, r, http.StatusBadRequest, "invalid_open_api_scope", err.Error())
		return
	}
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "open_api_site_failed", "Could not create Open API site")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"site": site})
}

func (s *Server) openAPIChannels(w http.ResponseWriter, r *http.Request) {
	authn, ok := s.authenticateOpenAPIRequest(w, r, "channels")
	if !ok {
		return
	}
	start := time.Now()
	list, err := s.repo.PublicChannels(r.Context(), store.ChannelFilter{Page: 1, PageSize: 100})
	status := http.StatusOK
	var payload any = list
	if err != nil {
		status = http.StatusInternalServerError
		payload = errorPayload("channels_unavailable", "Could not load channels")
	}
	s.finishOpenAPIRequest(r, authn.Site.ID, "/v1/status/channels", status, start)
	writeJSON(w, status, payload)
}

func (s *Server) openAPIChannel(w http.ResponseWriter, r *http.Request) {
	authn, ok := s.authenticateOpenAPIRequest(w, r, "channels")
	if !ok {
		return
	}
	start := time.Now()
	detail, err := s.repo.PublicChannel(r.Context(), chi.URLParam(r, "channelID"))
	status := http.StatusOK
	var payload any = detail
	if errors.Is(err, pgx.ErrNoRows) {
		status = http.StatusNotFound
		payload = errorPayload("channel_not_found", "Channel not found")
	} else if err != nil {
		status = http.StatusInternalServerError
		payload = errorPayload("channel_unavailable", "Could not load channel")
	}
	s.finishOpenAPIRequest(r, authn.Site.ID, "/v1/status/channels/{channelID}", status, start)
	writeJSON(w, status, payload)
}

func (s *Server) openAPIChannelSync(w http.ResponseWriter, r *http.Request) {
	authn, ok := s.authenticateOpenAPIRequest(w, r, "channel_sync")
	if !ok {
		return
	}
	start := time.Now()
	w.Header().Set("Cache-Control", "no-store")
	includeCredentials := r.URL.Query().Get("includeCredentials") == "1"
	channels, err := s.repo.AdminPlatformChannels(r.Context())
	status := http.StatusOK
	var payload any
	if err != nil {
		status = http.StatusInternalServerError
		payload = errorPayload("channel_sync_unavailable", "Could not load channel sync data")
	} else {
		items, err := s.adminChannelSyncItems(r.Context(), channels, includeCredentials)
		if err != nil {
			status = http.StatusInternalServerError
			payload = errorPayload("channel_sync_credentials_unavailable", "Could not load channel credentials")
		} else {
			logs, err := s.repo.OpenAPIChannelSyncLogs(r.Context(), 100)
			if err != nil {
				status = http.StatusInternalServerError
				payload = errorPayload("channel_sync_logs_unavailable", "Could not load channel sync logs")
			} else {
				source := strings.TrimRight(s.cfg.PublicURL, "/")
				if source == "" {
					source = requestOrigin(r)
				}
				credentials := "omitted"
				if includeCredentials {
					credentials = "included"
				}
				payload = channelSyncPayload{
					Version:     "tokhub.channel_sync.v1",
					Source:      source,
					SyncedAt:    time.Now().UTC(),
					Items:       items,
					Logs:        logs,
					Credentials: credentials,
				}
			}
		}
	}
	s.finishOpenAPIRequest(r, authn.Site.ID, "/v1/status/channel-sync", status, start)
	writeJSON(w, status, payload)
}

func (s *Server) openAPIOverview(w http.ResponseWriter, r *http.Request) {
	authn, ok := s.authenticateOpenAPIRequest(w, r, "overview")
	if !ok {
		return
	}
	start := time.Now()
	overview, err := s.repo.PublicOverview(r.Context())
	status := http.StatusOK
	var payload any = overview
	if err != nil {
		status = http.StatusInternalServerError
		payload = errorPayload("overview_unavailable", "Could not load overview")
	}
	s.finishOpenAPIRequest(r, authn.Site.ID, "/v1/status/overview", status, start)
	writeJSON(w, status, payload)
}

func (s *Server) openAPIUptime(w http.ResponseWriter, r *http.Request) {
	authn, ok := s.authenticateOpenAPIRequest(w, r, "uptime")
	if !ok {
		return
	}
	start := time.Now()
	uptime, err := s.repo.OpenAPIUptime(r.Context())
	status := http.StatusOK
	var payload any = uptime
	if err != nil {
		status = http.StatusInternalServerError
		payload = errorPayload("uptime_unavailable", "Could not load uptime")
	}
	s.finishOpenAPIRequest(r, authn.Site.ID, "/v1/status/uptime", status, start)
	writeJSON(w, status, payload)
}

func (s *Server) openAPIIncidents(w http.ResponseWriter, r *http.Request) {
	authn, ok := s.authenticateOpenAPIRequest(w, r, "incidents")
	if !ok {
		return
	}
	start := time.Now()
	incidents, err := s.repo.OpenAPIIncidents(r.Context())
	status := http.StatusOK
	var payload any = map[string]any{"items": incidents}
	if err != nil {
		status = http.StatusInternalServerError
		payload = errorPayload("incidents_unavailable", "Could not load incidents")
	}
	s.finishOpenAPIRequest(r, authn.Site.ID, "/v1/status/incidents", status, start)
	writeJSON(w, status, payload)
}

func (s *Server) authenticateOpenAPIRequest(w http.ResponseWriter, r *http.Request, scope string) (store.AuthenticatedOpenAPISite, bool) {
	key := strings.TrimSpace(r.Header.Get("X-Site-Key"))
	if key == "" {
		header := strings.TrimSpace(r.Header.Get("Authorization"))
		if strings.HasPrefix(strings.ToLower(header), "bearer ") {
			key = strings.TrimSpace(header[len("Bearer "):])
		}
	}
	if key == "" {
		writeError(w, r, http.StatusUnauthorized, "open_api_unauthorized", "Site Key is required")
		return store.AuthenticatedOpenAPISite{}, false
	}
	authn, err := s.repo.AuthenticateOpenAPISite(r.Context(), key, scope)
	if err != nil {
		writeError(w, r, http.StatusForbidden, "open_api_forbidden", "Site Key is invalid or scope is not allowed")
		return store.AuthenticatedOpenAPISite{}, false
	}
	limit := authn.Site.QPSLimit
	if limit <= 0 {
		limit = 60
	}
	limiterKey := "openapi:" + authn.Site.ID
	if allowed, err := s.gatewayCache.AllowQPS(r.Context(), limiterKey, limit); err == nil {
		if !allowed {
			s.finishOpenAPIRequest(r, authn.Site.ID, r.URL.Path, http.StatusTooManyRequests, time.Now())
			writeError(w, r, http.StatusTooManyRequests, "open_api_rate_limited", "Open API QPS limit exceeded")
			return store.AuthenticatedOpenAPISite{}, false
		}
	} else if errors.Is(err, gatewaycache.ErrUnavailable) || err != nil {
		if !s.allowRate(s.publicLimiter, limiterKey, limit, time.Second) {
			s.finishOpenAPIRequest(r, authn.Site.ID, r.URL.Path, http.StatusTooManyRequests, time.Now())
			writeError(w, r, http.StatusTooManyRequests, "open_api_rate_limited", "Open API QPS limit exceeded")
			return store.AuthenticatedOpenAPISite{}, false
		}
	}
	_ = s.repo.TouchOpenAPISite(r.Context(), authn.Site.ID)
	return authn, true
}

func (s *Server) finishOpenAPIRequest(r *http.Request, siteID string, endpoint string, status int, start time.Time) {
	_ = s.repo.RecordOpenAPICall(r.Context(), siteID, endpoint, status, int(time.Since(start).Milliseconds()), clientIP(r), r.UserAgent())
}

func errorPayload(code string, message string) map[string]any {
	return map[string]any{"error": map[string]string{"code": code, "message": message}}
}
