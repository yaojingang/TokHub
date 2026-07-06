package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	gatewaycache "tokhub/internal/gateway"
	"tokhub/internal/store"
)

type workspaceSettingsRequest struct {
	Name                         string `json:"name"`
	Timezone                     string `json:"timezone"`
	DefaultGatewayPolicy         string `json:"defaultGatewayPolicy"`
	DefaultNotificationChannelID string `json:"defaultNotificationChannelId"`
}

type workspaceMemberRequest struct {
	Email     string `json:"email"`
	Role      string `json:"role"`
	GroupName string `json:"groupName"`
}

type connectionValidationResult struct {
	OK             bool   `json:"ok"`
	Provider       string `json:"provider"`
	Type           string `json:"type"`
	Endpoint       string `json:"endpoint"`
	Model          string `json:"model"`
	Stage          string `json:"stage"`
	StatusCode     int    `json:"statusCode"`
	LatencyMs      int    `json:"latencyMs"`
	ModelCount     int    `json:"modelCount"`
	Tokens         int    `json:"tokens"`
	UsageEstimated bool   `json:"usageEstimated"`
	ErrorType      string `json:"errorType,omitempty"`
	Message        string `json:"message"`
}

type gatewayDebugRequest struct {
	Model  string `json:"model"`
	Prompt string `json:"prompt"`
	Kind   string `json:"kind"`
}

type gatewayDebugResult struct {
	OK             bool   `json:"ok"`
	GatewayID      string `json:"gatewayId"`
	Gateway        string `json:"gateway"`
	UpstreamID     string `json:"upstreamId"`
	Upstream       string `json:"upstream"`
	Model          string `json:"model"`
	StatusCode     int    `json:"statusCode"`
	LatencyMs      int    `json:"latencyMs"`
	Tokens         int    `json:"tokens"`
	UsageEstimated bool   `json:"usageEstimated"`
	ErrorType      string `json:"errorType,omitempty"`
	Message        string `json:"message"`
	Preview        string `json:"preview,omitempty"`
}

func (s *Server) consoleSettings(w http.ResponseWriter, r *http.Request) {
	user, _ := s.userFromRequest(r)
	orgID, role, ok := s.consoleWorkspaceForRead(w, r, user)
	if !ok {
		return
	}
	settings, err := s.repo.WorkspaceSettingsForOrg(r.Context(), orgID)
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "workspace_unavailable", "Could not load workspace settings")
		return
	}
	settings.Role = role
	workspaces, err := s.repo.UserWorkspaces(r.Context(), user)
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "workspaces_unavailable", "Could not load workspaces")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"workspace": settings, "workspaces": workspaces})
}

func (s *Server) updateConsoleSettings(w http.ResponseWriter, r *http.Request) {
	user, _ := s.userFromRequest(r)
	orgID, role, ok := s.consoleWorkspaceForWrite(w, r, user)
	if !ok {
		return
	}
	if !store.CanManageWorkspace(role) {
		writeError(w, r, http.StatusForbidden, "workspace_forbidden", "Only workspace admins can change settings")
		return
	}
	var req workspaceSettingsRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid_json", "Invalid JSON body")
		return
	}
	settings, err := s.repo.UpdateWorkspaceSettings(r.Context(), orgID, user.ID, store.WorkspaceSettingsInput{
		Name:                         req.Name,
		Timezone:                     req.Timezone,
		DefaultGatewayPolicy:         req.DefaultGatewayPolicy,
		DefaultNotificationChannelID: req.DefaultNotificationChannelID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, r, http.StatusNotFound, "workspace_target_not_found", "Workspace setting target was not found")
		return
	}
	if err != nil {
		writeError(w, r, http.StatusBadRequest, "workspace_update_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"workspace": settings})
}

func (s *Server) resetConsoleSettings(w http.ResponseWriter, r *http.Request) {
	user, _ := s.userFromRequest(r)
	orgID, role, ok := s.consoleWorkspaceForWrite(w, r, user)
	if !ok {
		return
	}
	if !store.CanManageWorkspace(role) {
		writeError(w, r, http.StatusForbidden, "workspace_forbidden", "Only workspace admins can reset settings")
		return
	}
	settings, err := s.repo.ResetWorkspaceSettings(r.Context(), orgID, user)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, r, http.StatusNotFound, "workspace_target_not_found", "Workspace setting target was not found")
		return
	}
	if err != nil {
		writeError(w, r, http.StatusBadRequest, "workspace_reset_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"workspace": settings})
}

func (s *Server) inviteConsoleMember(w http.ResponseWriter, r *http.Request) {
	user, _ := s.userFromRequest(r)
	orgID, role, ok := s.consoleWorkspaceForWrite(w, r, user)
	if !ok {
		return
	}
	if !store.CanManageWorkspace(role) {
		writeError(w, r, http.StatusForbidden, "workspace_forbidden", "Only workspace admins can invite members")
		return
	}
	var req workspaceMemberRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid_json", "Invalid JSON body")
		return
	}
	member, err := s.repo.UpsertWorkspaceMember(r.Context(), orgID, user.ID, store.WorkspaceMemberInput{Email: req.Email, Role: req.Role, GroupName: req.GroupName})
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, r, http.StatusNotFound, "member_not_registered", "该邮箱需要先注册并完成邮箱验证")
		return
	}
	if errors.Is(err, store.ErrWorkspaceEmailUnverified) {
		writeError(w, r, http.StatusForbidden, "member_not_verified", "该邮箱需要完成邮箱验证后才能加入工作区")
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

func (s *Server) patchConsoleMember(w http.ResponseWriter, r *http.Request) {
	user, _ := s.userFromRequest(r)
	orgID, role, ok := s.consoleWorkspaceForWrite(w, r, user)
	if !ok {
		return
	}
	if !store.CanManageWorkspace(role) {
		writeError(w, r, http.StatusForbidden, "workspace_forbidden", "Only workspace admins can update members")
		return
	}
	var req workspaceMemberRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid_json", "Invalid JSON body")
		return
	}
	memberID := chi.URLParam(r, "userID")
	member, err := s.repo.UpdateWorkspaceMember(r.Context(), orgID, user.ID, memberID, store.WorkspaceMemberInput{Role: req.Role, GroupName: req.GroupName})
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, r, http.StatusNotFound, "member_not_found", "Workspace member was not found")
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

func (s *Server) deleteConsoleMember(w http.ResponseWriter, r *http.Request) {
	user, _ := s.userFromRequest(r)
	orgID, role, ok := s.consoleWorkspaceForWrite(w, r, user)
	if !ok {
		return
	}
	if !store.CanManageWorkspace(role) {
		writeError(w, r, http.StatusForbidden, "workspace_forbidden", "Only workspace admins can remove members")
		return
	}
	memberID := chi.URLParam(r, "userID")
	err := s.repo.RemoveWorkspaceMember(r.Context(), orgID, user.ID, memberID)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, r, http.StatusNotFound, "member_not_found", "Workspace member was not found")
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

func (s *Server) validatePrivateChannelDraft(w http.ResponseWriter, r *http.Request) {
	user, _ := s.userFromRequest(r)
	if _, _, ok := s.consoleWorkspaceForOperate(w, r, user); !ok {
		return
	}
	var req privateChannelRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid_json", "Invalid JSON body")
		return
	}
	result, err := s.validateConnection(r.Context(), req.Provider, req.Type, req.Endpoint, req.Model, req.APIKey)
	if err != nil {
		writeError(w, r, http.StatusBadRequest, "connection_validation_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"result": result})
}

func (s *Server) validatePrivateChannel(w http.ResponseWriter, r *http.Request) {
	user, _ := s.userFromRequest(r)
	orgID, _, ok := s.consoleWorkspaceForOperate(w, r, user)
	if !ok {
		return
	}
	channelID := chi.URLParam(r, "channelID")
	channel, err := s.repo.PrivateChannelForOrg(r.Context(), orgID, channelID)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, r, http.StatusNotFound, "private_channel_not_found", "Private channel not found")
		return
	}
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "private_channel_unavailable", "Could not load private channel")
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
	result, err := s.validateConnection(r.Context(), channel.Provider, channel.Type, channel.Endpoint, channel.Model, apiKey)
	if err != nil {
		writeError(w, r, http.StatusBadRequest, "connection_validation_failed", err.Error())
		return
	}
	_ = s.repo.WriteAudit(r.Context(), store.AuditEvent{
		ActorType:  "user",
		ActorID:    user.ID,
		Action:     "private_channel.validated",
		ObjectType: "channel",
		ObjectID:   channelID,
		IP:         clientIP(r),
		Result:     mapBool(result.OK, "success", "failed"),
		Metadata:   map[string]any{"stage": result.Stage, "error_type": result.ErrorType, "status_code": result.StatusCode},
	})
	writeJSON(w, http.StatusOK, map[string]any{"result": result})
}

func (s *Server) debugConsoleGateway(w http.ResponseWriter, r *http.Request) {
	user, _ := s.userFromRequest(r)
	if !s.allowRate(s.authLimiter, "gateway-debug:"+user.ID+":"+clientIP(r), 12, time.Minute) {
		writeError(w, r, http.StatusTooManyRequests, "rate_limited", "网关测试过于频繁，请稍后再试")
		return
	}
	orgID, _, ok := s.consoleWorkspaceForOperate(w, r, user)
	if !ok {
		return
	}
	var req gatewayDebugRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid_json", "Invalid JSON body")
		return
	}
	gatewayID := chi.URLParam(r, "gatewayID")
	gateway, err := s.repo.GatewayByIDForOrg(r.Context(), gatewayID, orgID)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, r, http.StatusNotFound, "gateway_not_found", "Gateway was not found")
		return
	}
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "gateway_unavailable", "Could not load gateway")
		return
	}
	result := s.runGatewayDebug(r.Context(), gateway, req)
	_ = s.repo.WriteAudit(r.Context(), store.AuditEvent{
		ActorType:  "user",
		ActorID:    user.ID,
		Action:     "gateway.debug.called",
		ObjectType: "gateway",
		ObjectID:   gateway.ID,
		IP:         clientIP(r),
		Result:     mapBool(result.OK, "success", "failed"),
		Metadata:   map[string]any{"upstream_id": result.UpstreamID, "status_code": result.StatusCode, "error_type": result.ErrorType},
	})
	writeJSON(w, http.StatusOK, map[string]any{"result": result})
}

func (s *Server) validateConnection(ctx context.Context, provider string, typ string, endpoint string, model string, apiKey string) (connectionValidationResult, error) {
	provider = strings.TrimSpace(provider)
	if provider == "" {
		provider = "OpenAI"
	}
	typ = strings.TrimSpace(typ)
	if typ == "" {
		typ = "openai-compatible"
	}
	endpoint = strings.TrimSpace(endpoint)
	model = strings.TrimSpace(model)
	apiKey = strings.TrimSpace(apiKey)
	if endpoint == "" || model == "" || !strings.HasPrefix(endpoint, "http") {
		return connectionValidationResult{}, fmt.Errorf("endpoint and model are required")
	}
	if apiKey == "" {
		return connectionValidationResult{}, fmt.Errorf("apiKey is required for connection validation")
	}
	upstream := gatewaycache.Upstream{Provider: provider, Type: typ, Endpoint: endpoint, Model: model}
	start := time.Now()
	models, err := s.upstreamClient.Models(ctx, upstream, apiKey)
	modelCount := countModelList(models.Body)
	if err != nil {
		return connectionValidationResult{OK: false, Provider: provider, Type: typ, Endpoint: endpoint, Model: model, Stage: "models", StatusCode: models.StatusCode, LatencyMs: int(time.Since(start).Milliseconds()), ModelCount: modelCount, ErrorType: nonEmpty(models.ErrorType, "models_unavailable"), Message: "模型列表验证失败"}, nil
	}
	raw, _ := json.Marshal(map[string]any{
		"model":    model,
		"messages": []map[string]any{{"role": "user", "content": "Reply exactly: OK"}},
		"input":    "Reply exactly: OK",
	})
	estimate := upstreamUsageFromGateway(estimateUsage(gatewayPayload{Model: model, Input: "Reply exactly: OK", Messages: []gatewayMessage{{Role: "user", Content: "Reply exactly: OK"}}}))
	generation, err := s.upstreamClient.JSON(ctx, upstream, apiKey, "chat", raw, estimate)
	usage := gatewayUsageFromUpstream(generation.Usage)
	result := connectionValidationResult{
		Provider:       provider,
		Type:           typ,
		Endpoint:       endpoint,
		Model:          model,
		Stage:          "generation",
		StatusCode:     generation.StatusCode,
		LatencyMs:      int(time.Since(start).Milliseconds()),
		ModelCount:     modelCount,
		Tokens:         usage.TotalTokens,
		UsageEstimated: usage.Estimated,
	}
	if err != nil {
		result.OK = false
		result.ErrorType = nonEmpty(generation.ErrorType, "generation_failed")
		result.Message = "最小生成请求失败"
		return result, nil
	}
	result.OK = true
	result.Message = "连接、模型列表和最小生成均可用"
	return result, nil
}

func (s *Server) runGatewayDebug(ctx context.Context, gateway store.Gateway, req gatewayDebugRequest) gatewayDebugResult {
	kind := strings.TrimSpace(req.Kind)
	if kind != "responses" {
		kind = "chat"
	}
	model := strings.TrimSpace(req.Model)
	if model == "" {
		model = firstGatewayModel(gateway)
	}
	prompt := strings.TrimSpace(req.Prompt)
	if prompt == "" {
		prompt = "Reply exactly: OK"
	}
	payload := gatewayPayload{Model: model, Input: prompt, Messages: []gatewayMessage{{Role: "user", Content: prompt}}}
	raw, _ := json.Marshal(map[string]any{"model": model, "input": prompt, "messages": []map[string]any{{"role": "user", "content": prompt}}})
	raw, _ = rawPayloadWithModel(raw, model)
	start := time.Now()
	candidates := s.availableGatewayCandidates(ctx, gateway)
	if len(candidates) == 0 {
		s.recordDebugGatewayEvent(ctx, gateway.ID, "", model, http.StatusBadGateway, start, gatewayUsage{}, "no_upstream")
		return gatewayDebugResult{OK: false, GatewayID: gateway.ID, Gateway: gateway.Name, Model: model, StatusCode: http.StatusBadGateway, LatencyMs: int(time.Since(start).Milliseconds()), ErrorType: "no_upstream", Message: "没有可用上游"}
	}
	lastErrType := "upstream_failed"
	for _, upstream := range candidates {
		if s.cfg.UpstreamMode == "mock" {
			body, usage, err := mockGatewayJSON(kind, payload, upstream)
			if err != nil {
				lastErrType = "mock_upstream_failed"
				s.openCircuit(upstream.ChannelID)
				continue
			}
			s.recordDebugGatewayEvent(ctx, gateway.ID, upstream.ChannelID, model, http.StatusOK, start, usage, "")
			return gatewayDebugResult{OK: true, GatewayID: gateway.ID, Gateway: gateway.Name, UpstreamID: upstream.ChannelID, Upstream: upstream.Name, Model: model, StatusCode: http.StatusOK, LatencyMs: int(time.Since(start).Milliseconds()), Tokens: usage.TotalTokens, UsageEstimated: usage.Estimated, Message: "调试调用成功", Preview: truncateText(fmt.Sprint(body["id"])+" "+responsePreviewFromMap(body), 220)}
		}
		apiKey, err := s.gatewayUpstreamAPIKey(ctx, store.AuthenticatedGatewayKey{Gateway: gateway, Key: store.GatewayKey{OrgID: gateway.OrgID}}, upstream)
		if err != nil {
			lastErrType = "upstream_credential_unavailable"
			continue
		}
		estimate := upstreamUsageFromGateway(estimateUsage(payload))
		result, err := s.upstreamClient.JSON(ctx, gatewaycache.Upstream{Name: upstream.Name, Provider: upstream.Provider, Type: upstream.Type, Endpoint: upstream.Endpoint, Model: upstream.Model, ProviderConfig: upstream.ProviderConfig}, apiKey, kind, raw, estimate)
		usage := gatewayUsageFromUpstream(result.Usage)
		if err != nil {
			lastErrType = nonEmpty(result.ErrorType, "upstream_failed")
			s.openCircuit(upstream.ChannelID)
			continue
		}
		s.recordDebugGatewayEvent(ctx, gateway.ID, upstream.ChannelID, model, result.StatusCode, start, usage, "")
		return gatewayDebugResult{OK: true, GatewayID: gateway.ID, Gateway: gateway.Name, UpstreamID: upstream.ChannelID, Upstream: upstream.Name, Model: model, StatusCode: result.StatusCode, LatencyMs: int(time.Since(start).Milliseconds()), Tokens: usage.TotalTokens, UsageEstimated: usage.Estimated, Message: "调试调用成功", Preview: truncateText(responsePreview(result.Body), 220)}
	}
	s.recordDebugGatewayEvent(ctx, gateway.ID, "", model, http.StatusBadGateway, start, gatewayUsage{}, lastErrType)
	return gatewayDebugResult{OK: false, GatewayID: gateway.ID, Gateway: gateway.Name, Model: model, StatusCode: http.StatusBadGateway, LatencyMs: int(time.Since(start).Milliseconds()), ErrorType: lastErrType, Message: "所有上游在首字节前失败"}
}

func (s *Server) recordDebugGatewayEvent(ctx context.Context, gatewayID string, upstreamID string, model string, status int, start time.Time, usage gatewayUsage, errType string) {
	_ = s.repo.RecordGatewayEvent(ctx, store.GatewayRequestEvent{
		GatewayID:         gatewayID,
		UpstreamChannelID: upstreamID,
		RequestPath:       "/api/console/gateways/debug",
		Model:             model,
		StatusCode:        status,
		RequestTokens:     usage.PromptTokens,
		ResponseTokens:    usage.CompletionTokens,
		CostUSD:           s.gatewayCostForUsage(ctx, upstreamID, model, usage),
		LatencyMs:         int(time.Since(start).Milliseconds()),
		ErrorType:         errType,
		UsageEstimated:    usage.Estimated,
	})
}

func (s *Server) consoleWorkspaceForRead(w http.ResponseWriter, r *http.Request, user store.PublicUser) (string, string, bool) {
	personalOrgID, err := s.repo.EnsurePersonalWorkspace(r.Context(), user)
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "workspace_unavailable", "Could not prepare workspace")
		return "", "", false
	}
	orgID := requestedConsoleWorkspaceID(r)
	if orgID == "" {
		orgID = personalOrgID
	}
	role, err := s.repo.WorkspaceRole(r.Context(), orgID, user.ID)
	if err != nil {
		writeError(w, r, http.StatusForbidden, "workspace_forbidden", "Workspace membership is required")
		return "", "", false
	}
	settings, err := s.repo.WorkspaceSettingsForOrg(r.Context(), orgID)
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "workspace_unavailable", "Could not load workspace settings")
		return "", "", false
	}
	if settings.Status != "active" {
		writeError(w, r, http.StatusForbidden, "workspace_inactive", "Workspace is not active")
		return "", "", false
	}
	return orgID, role, true
}

func (s *Server) consoleWorkspaceForWrite(w http.ResponseWriter, r *http.Request, user store.PublicUser) (string, string, bool) {
	return s.consoleWorkspaceForRead(w, r, user)
}

func (s *Server) consoleWorkspaceForOperate(w http.ResponseWriter, r *http.Request, user store.PublicUser) (string, string, bool) {
	orgID, role, ok := s.consoleWorkspaceForRead(w, r, user)
	if !ok {
		return "", "", false
	}
	if !store.CanOperateWorkspace(role) {
		writeError(w, r, http.StatusForbidden, "workspace_forbidden", "Only workspace operators can perform this action")
		return "", "", false
	}
	return orgID, role, true
}

func requestedConsoleWorkspaceID(r *http.Request) string {
	if r == nil {
		return ""
	}
	orgID := strings.TrimSpace(r.URL.Query().Get("orgId"))
	if orgID == "" {
		orgID = strings.TrimSpace(r.URL.Query().Get("workspaceId"))
	}
	if orgID == "" {
		orgID = strings.TrimSpace(r.Header.Get("X-TokHub-Workspace"))
	}
	return orgID
}

func countModelList(body []byte) int {
	var payload struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return 0
	}
	return len(payload.Data)
}

func responsePreview(body []byte) string {
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return string(body)
	}
	return responsePreviewFromMap(payload)
}

func responsePreviewFromMap(payload map[string]any) string {
	if choices, ok := payload["choices"].([]any); ok && len(choices) > 0 {
		if first, ok := choices[0].(map[string]any); ok {
			if message, ok := first["message"].(map[string]any); ok {
				if content, ok := message["content"].(string); ok {
					return content
				}
			}
		}
	}
	if output, ok := payload["output"].([]any); ok && len(output) > 0 {
		return fmt.Sprint(output[0])
	}
	return fmt.Sprint(payload)
}

func truncateText(value string, max int) string {
	value = strings.TrimSpace(value)
	if len([]rune(value)) <= max {
		return value
	}
	runes := []rune(value)
	return string(runes[:max]) + "..."
}

func nonEmpty(value string, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func mapBool(ok bool, yes string, no string) string {
	if ok {
		return yes
	}
	return no
}

func boolValue(value *bool, fallback bool) bool {
	if value == nil {
		return fallback
	}
	return *value
}
