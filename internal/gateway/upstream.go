package gateway

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"tokhub/internal/connections"
)

const (
	defaultTimeout     = 60 * time.Second
	managedAuthTimeout = 300 * time.Second
)

var ErrUpstreamUnavailable = errors.New("upstream unavailable")

type Upstream struct {
	Name           string
	Provider       string
	Type           string
	Endpoint       string
	Model          string
	ProviderConfig map[string]any
}

type UpstreamUsage struct {
	PromptTokens     int  `json:"prompt_tokens"`
	CompletionTokens int  `json:"completion_tokens"`
	TotalTokens      int  `json:"total_tokens"`
	Estimated        bool `json:"estimated,omitempty"`
}

type UpstreamResult struct {
	Body       []byte
	StatusCode int
	Usage      UpstreamUsage
	ErrorType  string
	Wrote      bool
}

type UpstreamClient struct {
	httpClient *http.Client
}

func NewUpstreamClient() *UpstreamClient {
	return &UpstreamClient{httpClient: &http.Client{}}
}

func (c *UpstreamClient) Models(ctx context.Context, upstream Upstream, apiKey string) (UpstreamResult, error) {
	return c.models(ctx, upstream, apiKey, true)
}

func (c *UpstreamClient) ModelsStrict(ctx context.Context, upstream Upstream, apiKey string) (UpstreamResult, error) {
	return c.models(ctx, upstream, apiKey, false)
}

func (c *UpstreamClient) ModelsWithAuth(ctx context.Context, upstream Upstream, material connections.AuthMaterial, includeConfiguredFallback bool) (UpstreamResult, error) {
	reqCtx, cancel := upstreamRequestContext(ctx, upstream)
	defer cancel()
	req, err := c.newRequestWithAuth(reqCtx, upstream, material, http.MethodGet, "/models", nil)
	if err != nil {
		return UpstreamResult{ErrorType: "upstream_request_invalid"}, err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return UpstreamResult{ErrorType: classifyHTTPError(err)}, ErrUpstreamUnavailable
	}
	defer resp.Body.Close()
	body, readErr := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if readErr != nil {
		return UpstreamResult{Body: body, StatusCode: resp.StatusCode, ErrorType: "upstream_read_failed"}, ErrUpstreamUnavailable
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return UpstreamResult{Body: body, StatusCode: resp.StatusCode, ErrorType: upstreamStatusError(resp.StatusCode)}, ErrUpstreamUnavailable
	}
	mapped := mapModelsResponse(upstream, body, includeConfiguredFallback)
	return UpstreamResult{Body: mapped, StatusCode: resp.StatusCode}, nil
}

func (c *UpstreamClient) models(ctx context.Context, upstream Upstream, apiKey string, includeConfiguredFallback bool) (UpstreamResult, error) {
	reqCtx, cancel := upstreamRequestContext(ctx, upstream)
	defer cancel()
	req, err := c.newRequest(reqCtx, upstream, apiKey, http.MethodGet, "/models", nil)
	if err != nil {
		return UpstreamResult{ErrorType: "upstream_request_invalid"}, err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return UpstreamResult{ErrorType: classifyHTTPError(err)}, ErrUpstreamUnavailable
	}
	defer resp.Body.Close()
	body, readErr := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if readErr != nil {
		return UpstreamResult{Body: body, StatusCode: resp.StatusCode, ErrorType: "upstream_read_failed"}, ErrUpstreamUnavailable
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return UpstreamResult{Body: body, StatusCode: resp.StatusCode, ErrorType: upstreamStatusError(resp.StatusCode)}, ErrUpstreamUnavailable
	}
	mapped := mapModelsResponse(upstream, body, includeConfiguredFallback)
	return UpstreamResult{Body: mapped, StatusCode: resp.StatusCode}, nil
}

func (c *UpstreamClient) JSON(ctx context.Context, upstream Upstream, apiKey string, kind string, raw []byte, estimate UpstreamUsage) (UpstreamResult, error) {
	path, body, err := adaptRequestBody(upstream, kind, raw, false)
	if err != nil {
		return UpstreamResult{ErrorType: "upstream_request_invalid"}, err
	}
	reqCtx, cancel := upstreamRequestContext(ctx, upstream)
	defer cancel()
	req, err := c.newRequest(reqCtx, upstream, apiKey, http.MethodPost, path, body)
	if err != nil {
		return UpstreamResult{ErrorType: "upstream_request_invalid"}, err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return UpstreamResult{ErrorType: classifyHTTPError(err)}, ErrUpstreamUnavailable
	}
	defer resp.Body.Close()
	respBody, readErr := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if readErr != nil {
		return UpstreamResult{Body: respBody, StatusCode: resp.StatusCode, ErrorType: "upstream_read_failed"}, ErrUpstreamUnavailable
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return UpstreamResult{Body: respBody, StatusCode: resp.StatusCode, ErrorType: upstreamStatusError(resp.StatusCode)}, ErrUpstreamUnavailable
	}
	mapped, usage := adaptResponseBody(upstream, kind, respBody)
	if usage.TotalTokens <= 0 {
		usage = estimate
		usage.Estimated = true
	}
	return UpstreamResult{Body: mapped, StatusCode: resp.StatusCode, Usage: usage}, nil
}

func (c *UpstreamClient) JSONWithAuth(ctx context.Context, upstream Upstream, material connections.AuthMaterial, kind string, raw []byte, estimate UpstreamUsage) (UpstreamResult, error) {
	upstream = withManagedAuthTimeout(upstream)
	path, body, err := adaptRequestBody(upstream, kind, raw, false)
	if err != nil {
		return UpstreamResult{ErrorType: "upstream_request_invalid"}, err
	}
	reqCtx, cancel := upstreamRequestContext(ctx, upstream)
	defer cancel()
	req, err := c.newRequestWithAuth(reqCtx, upstream, material, http.MethodPost, path, body)
	if err != nil {
		return UpstreamResult{ErrorType: "upstream_request_invalid"}, err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return UpstreamResult{ErrorType: classifyHTTPError(err)}, ErrUpstreamUnavailable
	}
	defer resp.Body.Close()
	respBody, readErr := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if readErr != nil {
		return UpstreamResult{Body: respBody, StatusCode: resp.StatusCode, ErrorType: "upstream_read_failed"}, ErrUpstreamUnavailable
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return UpstreamResult{Body: respBody, StatusCode: resp.StatusCode, ErrorType: upstreamStatusError(resp.StatusCode)}, ErrUpstreamUnavailable
	}
	if material.Mode == connections.AuthModeCodexOAuth {
		mapped, usage, mapErr := mapCodexEventResponse(upstream, kind, respBody)
		if mapErr != nil {
			return UpstreamResult{Body: respBody, StatusCode: resp.StatusCode, ErrorType: "upstream_stream_interrupted"}, ErrUpstreamUnavailable
		}
		if usage.TotalTokens <= 0 {
			usage = estimate
			usage.Estimated = true
		}
		return UpstreamResult{Body: mapped, StatusCode: resp.StatusCode, Usage: usage}, nil
	}
	mapped, usage := adaptResponseBody(upstream, kind, respBody)
	if usage.TotalTokens <= 0 {
		usage = estimate
		usage.Estimated = true
	}
	return UpstreamResult{Body: mapped, StatusCode: resp.StatusCode, Usage: usage}, nil
}

func (c *UpstreamClient) Stream(ctx context.Context, upstream Upstream, apiKey string, kind string, raw []byte, estimate UpstreamUsage, w http.ResponseWriter) (UpstreamResult, error) {
	path, body, err := adaptRequestBody(upstream, kind, raw, true)
	if err != nil {
		return UpstreamResult{ErrorType: "upstream_request_invalid"}, err
	}
	reqCtx, cancel := upstreamRequestContext(ctx, upstream)
	defer cancel()
	req, err := c.newRequest(reqCtx, upstream, apiKey, http.MethodPost, path, body)
	if err != nil {
		return UpstreamResult{ErrorType: "upstream_request_invalid"}, err
	}
	extendStreamWriteDeadline(w, upstreamTimeout(upstream))
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return UpstreamResult{ErrorType: classifyHTTPError(err)}, ErrUpstreamUnavailable
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
		return UpstreamResult{StatusCode: resp.StatusCode, ErrorType: upstreamStatusError(resp.StatusCode)}, ErrUpstreamUnavailable
	}
	if isGeminiUpstream(upstream) {
		return streamGeminiResponse(resp, kind, upstream.Model, estimate, w)
	}
	copyHeaders(w.Header(), resp.Header)
	if w.Header().Get("Content-Type") == "" {
		w.Header().Set("Content-Type", "text/event-stream")
	}
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(resp.StatusCode)
	flusher, _ := w.(http.Flusher)
	buf := make([]byte, 32*1024)
	for {
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			if _, writeErr := w.Write(buf[:n]); writeErr != nil {
				estimate.Estimated = true
				return UpstreamResult{StatusCode: resp.StatusCode, Usage: estimate, Wrote: true, ErrorType: "client_disconnected"}, writeErr
			}
			if flusher != nil {
				flusher.Flush()
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			estimate.Estimated = true
			return UpstreamResult{StatusCode: resp.StatusCode, Usage: estimate, Wrote: true, ErrorType: "upstream_stream_interrupted"}, readErr
		}
	}
	estimate.Estimated = true
	return UpstreamResult{StatusCode: resp.StatusCode, Usage: estimate, Wrote: true}, nil
}

func (c *UpstreamClient) StreamWithAuth(ctx context.Context, upstream Upstream, material connections.AuthMaterial, kind string, raw []byte, estimate UpstreamUsage, w http.ResponseWriter) (UpstreamResult, error) {
	upstream = withManagedAuthTimeout(upstream)
	path, body, err := adaptRequestBody(upstream, kind, raw, true)
	if err != nil {
		return UpstreamResult{ErrorType: "upstream_request_invalid"}, err
	}
	reqCtx, cancel := upstreamRequestContext(ctx, upstream)
	defer cancel()
	req, err := c.newRequestWithAuth(reqCtx, upstream, material, http.MethodPost, path, body)
	if err != nil {
		return UpstreamResult{ErrorType: "upstream_request_invalid"}, err
	}
	extendStreamWriteDeadline(w, upstreamTimeout(upstream))
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return UpstreamResult{ErrorType: classifyHTTPError(err)}, ErrUpstreamUnavailable
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
		return UpstreamResult{StatusCode: resp.StatusCode, ErrorType: upstreamStatusError(resp.StatusCode)}, ErrUpstreamUnavailable
	}
	if material.Mode == connections.AuthModeCodexOAuth && kind == "chat" {
		return streamCodexChatResponse(resp, upstream.Model, estimate, w)
	}
	if isGeminiUpstream(upstream) {
		return streamGeminiResponse(resp, kind, upstream.Model, estimate, w)
	}
	copyHeaders(w.Header(), resp.Header)
	if w.Header().Get("Content-Type") == "" {
		w.Header().Set("Content-Type", "text/event-stream")
	}
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(resp.StatusCode)
	flusher, _ := w.(http.Flusher)
	buf := make([]byte, 32*1024)
	for {
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			if _, writeErr := w.Write(buf[:n]); writeErr != nil {
				estimate.Estimated = true
				return UpstreamResult{StatusCode: resp.StatusCode, Usage: estimate, Wrote: true, ErrorType: "client_disconnected"}, writeErr
			}
			if flusher != nil {
				flusher.Flush()
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			estimate.Estimated = true
			return UpstreamResult{StatusCode: resp.StatusCode, Usage: estimate, Wrote: true, ErrorType: "upstream_stream_interrupted"}, readErr
		}
	}
	estimate.Estimated = true
	return UpstreamResult{StatusCode: resp.StatusCode, Usage: estimate, Wrote: true}, nil
}

func (c *UpstreamClient) newRequest(ctx context.Context, upstream Upstream, apiKey string, method string, path string, body []byte) (*http.Request, error) {
	endpoint := strings.TrimRight(strings.TrimSpace(upstream.Endpoint), "/")
	if endpoint == "" {
		return nil, fmt.Errorf("endpoint is required")
	}
	target := joinEndpointPathForUpstream(upstream, endpoint, path)
	req, err := http.NewRequestWithContext(ctx, method, target, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")
	applyClientProfileHeaders(req, upstream.ProviderConfig)
	provider := upstream.adapterKind()
	switch {
	case strings.Contains(provider, "anthropic"):
		if authHeader, ok := configString(upstream.ProviderConfig, "authHeader"); ok && strings.EqualFold(authHeader, "authorization") {
			req.Header.Set("Authorization", "Bearer "+apiKey)
		} else {
			req.Header.Set("X-API-Key", apiKey)
		}
		req.Header.Set("Anthropic-Version", "2023-06-01")
	case strings.Contains(provider, "google"), strings.Contains(provider, "gemini"):
		req.Header.Set("X-Goog-Api-Key", apiKey)
	default:
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
	return req, nil
}

func (c *UpstreamClient) newRequestWithAuth(ctx context.Context, upstream Upstream, material connections.AuthMaterial, method string, path string, body []byte) (*http.Request, error) {
	if err := material.Validate(); err != nil {
		return nil, err
	}
	if strings.TrimSpace(material.Endpoint) != "" {
		upstream.Endpoint = material.Endpoint
	}
	if material.Mode == connections.AuthModeCodexOAuth || material.Mode == connections.AuthModeDeepSeekWeb {
		config := make(map[string]any, len(upstream.ProviderConfig)+1)
		for key, value := range upstream.ProviderConfig {
			config[key] = value
		}
		if material.Mode == connections.AuthModeCodexOAuth {
			config["pathMode"] = "direct"
		} else {
			// DeepSeek's official API uses root-level OpenAI paths, while the
			// isolated DS2API bridge exposes the standard /v1 routes. Removing
			// an inherited direct mode also repairs already-saved web sessions.
			delete(config, "pathMode")
		}
		upstream.ProviderConfig = config
	}
	request, err := c.newRequest(ctx, upstream, "", method, path, body)
	if err != nil {
		return nil, err
	}
	for _, name := range []string{
		"Authorization", "X-API-Key", "X-Goog-Api-Key", "X-Goog-User-Project",
		"ChatGPT-Account-Id", "OpenAI-Beta", "Originator", "User-Agent", "Version",
	} {
		request.Header.Del(name)
	}
	for name, values := range material.Headers {
		request.Header.Set(name, values[0])
	}
	if material.Mode == connections.AuthModeCodexOAuth {
		request.Header.Set("Accept", "text/event-stream")
	}
	return request, nil
}

func joinEndpointPathForUpstream(upstream Upstream, endpoint string, path string) string {
	path, rawQuery := splitUpstreamPathQuery(path)
	if mode, ok := configString(upstream.ProviderConfig, "pathMode"); ok && strings.EqualFold(mode, "direct") {
		endpoint = strings.TrimRight(strings.TrimSpace(endpoint), "/")
		path = "/" + strings.TrimLeft(path, "/")
		parsed, err := url.Parse(endpoint)
		if err != nil || parsed.Scheme == "" || parsed.Host == "" {
			return appendRawQuery(endpoint+path, rawQuery)
		}
		parsed.Path = strings.TrimRight(parsed.Path, "/") + path
		if rawQuery != "" {
			parsed.RawQuery = rawQuery
		}
		return parsed.String()
	}
	return appendRawQuery(joinEndpointPath(upstream.adapterKind(), endpoint, path), rawQuery)
}

func applyClientProfileHeaders(req *http.Request, config map[string]any) {
	profile, ok := configString(config, "clientProfile")
	if !ok {
		return
	}
	switch strings.ToLower(profile) {
	case "claude-code", "claude_code":
		version := "2.1.114"
		if configured, ok := configString(config, "clientVersion"); ok {
			version = configured
		}
		req.Header.Set("User-Agent", "claude-code/"+version)
	}
}

func (upstream Upstream) adapterKind() string {
	kind := strings.ToLower(strings.TrimSpace(upstream.Type))
	switch kind {
	case "anthropic", "gemini", "google", "openai", "openai-compatible":
		return kind
	}
	return strings.ToLower(strings.TrimSpace(upstream.Provider))
}

func isGeminiUpstream(upstream Upstream) bool {
	provider := upstream.adapterKind()
	return strings.Contains(provider, "google") || strings.Contains(provider, "gemini")
}

func upstreamRequestContext(ctx context.Context, upstream Upstream) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, upstreamTimeout(upstream))
}

func upstreamTimeout(upstream Upstream) time.Duration {
	timeout := defaultTimeout
	if timeoutMs, ok := configInt(upstream.ProviderConfig, "timeoutMs", 1, 300000); ok {
		timeout = time.Duration(timeoutMs) * time.Millisecond
	}
	return timeout
}

func withManagedAuthTimeout(upstream Upstream) Upstream {
	if _, ok := configInt(upstream.ProviderConfig, "timeoutMs", 1, 300000); ok {
		return upstream
	}
	config := make(map[string]any, len(upstream.ProviderConfig)+1)
	for key, value := range upstream.ProviderConfig {
		config[key] = value
	}
	config["timeoutMs"] = int(managedAuthTimeout / time.Millisecond)
	upstream.ProviderConfig = config
	return upstream
}

func extendStreamWriteDeadline(w http.ResponseWriter, timeout time.Duration) {
	if w == nil {
		return
	}
	_ = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(timeout + 5*time.Second))
}

func splitUpstreamPathQuery(path string) (string, string) {
	path, rawQuery, _ := strings.Cut(path, "?")
	return path, rawQuery
}

func appendRawQuery(target string, rawQuery string) string {
	if rawQuery == "" {
		return target
	}
	if strings.Contains(target, "?") {
		return target + "&" + rawQuery
	}
	return target + "?" + rawQuery
}

func joinEndpointPath(provider string, endpoint string, path string) string {
	endpoint = strings.TrimRight(strings.TrimSpace(endpoint), "/")
	path = "/" + strings.TrimLeft(path, "/")
	if endpoint == "" {
		return path
	}
	parsed, err := url.Parse(endpoint)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return endpoint + path
	}
	cleanPath := strings.TrimRight(parsed.Path, "/")
	lowerPath := strings.ToLower(cleanPath)
	if lowerPath == "" {
		parsed.Path = "/v1" + path
		return parsed.String()
	}
	if strings.HasSuffix(lowerPath, "/v1") || strings.HasSuffix(lowerPath, "/v1beta") {
		parsed.Path = cleanPath + path
		return parsed.String()
	}
	if strings.Contains(strings.ToLower(provider), "gemini") || strings.Contains(strings.ToLower(provider), "google") {
		parsed.Path = cleanPath + path
		return parsed.String()
	}
	parsed.Path = cleanPath + "/v1" + path
	return parsed.String()
}

func adaptRequestBody(upstream Upstream, kind string, raw []byte, stream bool) (string, []byte, error) {
	provider := upstream.adapterKind()
	payload := map[string]any{}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &payload); err != nil {
			return "", nil, err
		}
	}
	if strings.TrimSpace(asString(payload["model"])) == "" {
		payload["model"] = upstream.Model
	}
	payload["stream"] = stream
	applyOpenAIProviderConfig(payload, upstream.ProviderConfig)
	if isCodexOAuthUpstream(upstream) {
		return adaptCodexRequestBody(upstream, kind, payload)
	}

	switch {
	case strings.Contains(provider, "anthropic"):
		maxTokens := numberOrDefault(firstPresent(payload, "max_tokens", "maxTokens"), providerMaxTokens(upstream.ProviderConfig, 1024))
		body := map[string]any{
			"model":      modelFor(payload, upstream),
			"max_tokens": maxTokens,
			"messages":   messagesForAnthropic(payload),
			"stream":     stream,
		}
		if system := payload["system"]; system != nil {
			body["system"] = system
		}
		applyAnthropicProviderConfig(body, upstream.ProviderConfig)
		encoded, err := json.Marshal(body)
		return "/messages", encoded, err
	case strings.Contains(provider, "google"), strings.Contains(provider, "gemini"):
		body := map[string]any{"contents": contentsForGemini(payload)}
		if instruction := systemInstructionForGemini(payload); instruction != "" {
			body["systemInstruction"] = map[string]any{
				"parts": []map[string]any{{"text": instruction}},
			}
		}
		applyGeminiRequestConfig(body, payload, upstream.ProviderConfig)
		encoded, err := json.Marshal(body)
		path := "/models/" + url.PathEscape(modelFor(payload, upstream)) + ":generateContent"
		if stream {
			path = "/models/" + url.PathEscape(modelFor(payload, upstream)) + ":streamGenerateContent?alt=sse"
		}
		return path, encoded, err
	default:
		encoded, err := json.Marshal(payload)
		if kind == "responses" {
			return "/responses", encoded, err
		}
		return "/chat/completions", encoded, err
	}
}

func isCodexOAuthUpstream(upstream Upstream) bool {
	method, ok := configString(upstream.ProviderConfig, "authMethod")
	return ok && strings.EqualFold(method, "codex_oauth")
}

func adaptCodexRequestBody(upstream Upstream, kind string, payload map[string]any) (string, []byte, error) {
	body := map[string]any{
		"model":        modelFor(payload, upstream),
		"stream":       true,
		"store":        false,
		"instructions": "You are a helpful assistant.",
	}
	if kind == "responses" {
		for _, key := range []string{
			"input", "instructions", "tools", "tool_choice", "reasoning", "text",
			"include", "previous_response_id", "max_output_tokens", "parallel_tool_calls",
		} {
			if value, exists := payload[key]; exists && value != nil {
				body[key] = value
			}
		}
	} else {
		input := make([]map[string]any, 0)
		instructions := make([]string, 0)
		if messages, ok := payload["messages"].([]any); ok {
			for _, item := range messages {
				message, ok := item.(map[string]any)
				if !ok {
					continue
				}
				role := strings.ToLower(strings.TrimSpace(asString(message["role"])))
				content := message["content"]
				if role == "system" || role == "developer" {
					if text := contentText(content); text != "" {
						instructions = append(instructions, text)
					}
					continue
				}
				if role == "tool" {
					callID := strings.TrimSpace(asString(message["tool_call_id"]))
					if callID != "" {
						input = append(input, map[string]any{
							"type": "function_call_output", "call_id": callID,
							"output": contentText(content),
						})
					}
					continue
				}
				if role == "" {
					role = "user"
				}
				if text := contentText(content); text != "" {
					input = append(input, map[string]any{"role": role, "content": content})
				}
				if role == "assistant" {
					input = append(input, codexAssistantToolCalls(message["tool_calls"])...)
				}
			}
		}
		if len(input) == 0 {
			input = append(input, map[string]any{"role": "user", "content": payload["input"]})
		}
		body["input"] = input
		if len(instructions) > 0 {
			body["instructions"] = strings.Join(instructions, "\n\n")
		}
		if maxTokens := firstPresent(payload, "max_completion_tokens", "max_tokens", "maxTokens"); maxTokens != nil {
			body["max_output_tokens"] = maxTokens
		}
		if effort := strings.TrimSpace(asString(payload["reasoning_effort"])); effort != "" {
			body["reasoning"] = map[string]any{"effort": effort}
		}
		if tools, ok := payload["tools"].([]any); ok {
			if adapted := codexResponseTools(tools); len(adapted) > 0 {
				body["tools"] = adapted
			}
		}
		if choice := codexToolChoice(payload["tool_choice"]); choice != nil {
			body["tool_choice"] = choice
		}
		if parallel, exists := payload["parallel_tool_calls"]; exists {
			body["parallel_tool_calls"] = parallel
		}
	}
	if _, exists := body["input"]; !exists {
		body["input"] = []map[string]any{{"role": "user", "content": ""}}
	}
	encoded, err := json.Marshal(body)
	return "/responses", encoded, err
}

func codexResponseTools(tools []any) []map[string]any {
	out := make([]map[string]any, 0, len(tools))
	for _, item := range tools {
		tool, ok := item.(map[string]any)
		if !ok || asString(tool["type"]) != "function" {
			continue
		}
		function, ok := tool["function"].(map[string]any)
		if !ok {
			continue
		}
		name := strings.TrimSpace(asString(function["name"]))
		if name == "" {
			continue
		}
		adapted := map[string]any{"type": "function", "name": name}
		for _, key := range []string{"description", "parameters", "strict"} {
			if value, exists := function[key]; exists {
				adapted[key] = value
			}
		}
		out = append(out, adapted)
	}
	return out
}

func codexAssistantToolCalls(value any) []map[string]any {
	tools, ok := value.([]any)
	if !ok {
		return nil
	}
	out := make([]map[string]any, 0, len(tools))
	for _, raw := range tools {
		tool, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		function, ok := tool["function"].(map[string]any)
		if !ok {
			continue
		}
		callID := strings.TrimSpace(asString(firstPresent(tool, "id", "call_id")))
		name := strings.TrimSpace(asString(function["name"]))
		if callID == "" || name == "" {
			continue
		}
		out = append(out, map[string]any{
			"type": "function_call", "call_id": callID, "name": name,
			"arguments": asString(function["arguments"]),
		})
	}
	return out
}

func codexToolChoice(value any) any {
	switch typed := value.(type) {
	case string:
		switch strings.ToLower(strings.TrimSpace(typed)) {
		case "auto", "none", "required":
			return strings.ToLower(strings.TrimSpace(typed))
		}
	case map[string]any:
		if !strings.EqualFold(asString(typed["type"]), "function") {
			return nil
		}
		function, ok := typed["function"].(map[string]any)
		if !ok {
			return nil
		}
		if name := strings.TrimSpace(asString(function["name"])); name != "" {
			return map[string]any{"type": "function", "name": name}
		}
	}
	return nil
}

func contentText(value any) string {
	if value == nil {
		return ""
	}
	switch typed := value.(type) {
	case string:
		return typed
	case []any:
		parts := make([]string, 0, len(typed))
		for _, item := range typed {
			if part, ok := item.(map[string]any); ok {
				if text := strings.TrimSpace(asString(firstPresent(part, "text", "content"))); text != "" {
					parts = append(parts, text)
				}
			}
		}
		return strings.Join(parts, "\n")
	default:
		return strings.TrimSpace(fmt.Sprint(value))
	}
}

func mapCodexEventResponse(upstream Upstream, kind string, body []byte) ([]byte, UpstreamUsage, error) {
	responseBody := body
	var direct map[string]any
	if err := json.Unmarshal(body, &direct); err != nil || asString(direct["object"]) != "response" {
		completed, err := completedResponseFromSSE(body)
		if err != nil {
			return nil, UpstreamUsage{}, err
		}
		responseBody, err = json.Marshal(completed)
		if err != nil {
			return nil, UpstreamUsage{}, err
		}
		direct = completed
	}
	usage := parseUsage(responseBody)
	if kind == "responses" {
		return responseBody, usage, nil
	}
	return chatObjectWithTools(
		upstream.Model,
		responseOutputText(direct),
		responseToolCalls(direct),
		usage,
	), usage, nil
}

func completedResponseFromSSE(body []byte) (map[string]any, error) {
	scanner := bufio.NewScanner(bytes.NewReader(body))
	scanner.Buffer(make([]byte, 64*1024), 16<<20)
	event := ""
	for scanner.Scan() {
		line := scanner.Text()
		switch {
		case strings.HasPrefix(line, "event:"):
			event = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		case strings.HasPrefix(line, "data:"):
			data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			if data == "" || data == "[DONE]" {
				continue
			}
			var payload map[string]any
			if json.Unmarshal([]byte(data), &payload) != nil {
				continue
			}
			eventType := event
			if typed := strings.TrimSpace(asString(payload["type"])); typed != "" {
				eventType = typed
			}
			if eventType == "response.completed" || eventType == "response.done" {
				if response, ok := payload["response"].(map[string]any); ok {
					return response, nil
				}
				if asString(payload["object"]) == "response" {
					return payload, nil
				}
			}
			if eventType == "error" || eventType == "response.failed" {
				return nil, ErrUpstreamUnavailable
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return nil, fmt.Errorf("Codex response did not include a completed event")
}

func responseOutputText(response map[string]any) string {
	parts := make([]string, 0)
	if output, ok := response["output"].([]any); ok {
		for _, item := range output {
			message, ok := item.(map[string]any)
			if !ok {
				continue
			}
			if content, ok := message["content"].([]any); ok {
				for _, rawPart := range content {
					part, ok := rawPart.(map[string]any)
					if ok && (asString(part["type"]) == "output_text" || asString(part["type"]) == "text") {
						parts = append(parts, asString(part["text"]))
					}
				}
			}
		}
	}
	return strings.Join(parts, "")
}

func responseToolCalls(response map[string]any) []map[string]any {
	output, ok := response["output"].([]any)
	if !ok {
		return nil
	}
	calls := make([]map[string]any, 0)
	for _, rawItem := range output {
		item, ok := rawItem.(map[string]any)
		if !ok || asString(item["type"]) != "function_call" {
			continue
		}
		callID := strings.TrimSpace(asString(firstPresent(item, "call_id", "id")))
		name := strings.TrimSpace(asString(item["name"]))
		if callID == "" || name == "" {
			continue
		}
		calls = append(calls, map[string]any{
			"id": callID, "type": "function",
			"function": map[string]any{
				"name": name, "arguments": asString(item["arguments"]),
			},
		})
	}
	return calls
}

func streamCodexChatResponse(resp *http.Response, model string, estimate UpstreamUsage, w http.ResponseWriter) (UpstreamResult, error) {
	copyHeaders(w.Header(), resp.Header)
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(resp.StatusCode)
	flusher, _ := w.(http.Flusher)
	id := "chatcmpl_" + time.Now().Format("20060102150405")
	writeChunk := func(delta map[string]any, finish any) error {
		chunk, _ := json.Marshal(map[string]any{
			"id": id, "object": "chat.completion.chunk", "created": time.Now().Unix(), "model": model,
			"choices": []map[string]any{{"index": 0, "delta": delta, "finish_reason": finish}},
		})
		if _, err := fmt.Fprintf(w, "data: %s\n\n", chunk); err != nil {
			return err
		}
		if flusher != nil {
			flusher.Flush()
		}
		return nil
	}
	if err := writeChunk(map[string]any{"role": "assistant"}, nil); err != nil {
		return UpstreamResult{StatusCode: resp.StatusCode, Wrote: true, ErrorType: "client_disconnected"}, err
	}
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 64*1024), 16<<20)
	event := ""
	wroteText := false
	wroteTools := false
	toolIndexes := map[string]int{}
	toolArgumentDeltas := map[string]bool{}
	nextToolIndex := 0
	usage := UpstreamUsage{}
	for scanner.Scan() {
		line := scanner.Text()
		switch {
		case strings.HasPrefix(line, "event:"):
			event = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		case strings.HasPrefix(line, "data:"):
			data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			if data == "" || data == "[DONE]" {
				continue
			}
			var payload map[string]any
			if json.Unmarshal([]byte(data), &payload) != nil {
				continue
			}
			eventType := event
			if typed := strings.TrimSpace(asString(payload["type"])); typed != "" {
				eventType = typed
			}
			if eventType == "response.output_text.delta" {
				if delta := asString(payload["delta"]); delta != "" {
					if err := writeChunk(map[string]any{"content": delta}, nil); err != nil {
						return UpstreamResult{StatusCode: resp.StatusCode, Usage: estimate, Wrote: true, ErrorType: "client_disconnected"}, err
					}
					wroteText = true
				}
			}
			if eventType == "response.output_item.added" {
				if item, ok := payload["item"].(map[string]any); ok && asString(item["type"]) == "function_call" {
					callID := strings.TrimSpace(asString(firstPresent(item, "call_id", "id")))
					itemID := strings.TrimSpace(asString(item["id"]))
					name := strings.TrimSpace(asString(item["name"]))
					if callID != "" && name != "" {
						index, exists := toolIndexes[callID]
						if !exists {
							index = nextToolIndex
							nextToolIndex++
							toolIndexes[callID] = index
						}
						if itemID != "" {
							toolIndexes[itemID] = index
						}
						delta := map[string]any{"tool_calls": []map[string]any{{
							"index": index, "id": callID, "type": "function",
							"function": map[string]any{"name": name, "arguments": ""},
						}}}
						if err := writeChunk(delta, nil); err != nil {
							return UpstreamResult{StatusCode: resp.StatusCode, Usage: estimate, Wrote: true, ErrorType: "client_disconnected"}, err
						}
						wroteTools = true
					}
				}
			}
			if eventType == "response.function_call_arguments.delta" {
				callID := strings.TrimSpace(asString(firstPresent(payload, "item_id", "call_id")))
				if deltaText := asString(payload["delta"]); callID != "" && deltaText != "" {
					index, exists := toolIndexes[callID]
					if !exists {
						index = nextToolIndex
						nextToolIndex++
						toolIndexes[callID] = index
					}
					delta := map[string]any{"tool_calls": []map[string]any{{
						"index":    index,
						"function": map[string]any{"arguments": deltaText},
					}}}
					if err := writeChunk(delta, nil); err != nil {
						return UpstreamResult{StatusCode: resp.StatusCode, Usage: estimate, Wrote: true, ErrorType: "client_disconnected"}, err
					}
					toolArgumentDeltas[callID] = true
					wroteTools = true
				}
			}
			if eventType == "response.function_call_arguments.done" {
				callID := strings.TrimSpace(asString(firstPresent(payload, "item_id", "call_id")))
				if arguments := asString(payload["arguments"]); callID != "" && arguments != "" && !toolArgumentDeltas[callID] {
					index, exists := toolIndexes[callID]
					if !exists {
						index = nextToolIndex
						nextToolIndex++
						toolIndexes[callID] = index
					}
					delta := map[string]any{"tool_calls": []map[string]any{{
						"index":    index,
						"function": map[string]any{"arguments": arguments},
					}}}
					if err := writeChunk(delta, nil); err != nil {
						return UpstreamResult{StatusCode: resp.StatusCode, Usage: estimate, Wrote: true, ErrorType: "client_disconnected"}, err
					}
					wroteTools = true
				}
			}
			if eventType == "response.completed" || eventType == "response.done" {
				if response, ok := payload["response"].(map[string]any); ok {
					rawResponse, _ := json.Marshal(response)
					usage = parseUsage(rawResponse)
					if !wroteText {
						if text := responseOutputText(response); text != "" {
							if err := writeChunk(map[string]any{"content": text}, nil); err != nil {
								return UpstreamResult{StatusCode: resp.StatusCode, Usage: estimate, Wrote: true, ErrorType: "client_disconnected"}, err
							}
						}
					}
					if !wroteTools {
						for index, tool := range responseToolCalls(response) {
							tool["index"] = index
							if err := writeChunk(map[string]any{"tool_calls": []map[string]any{tool}}, nil); err != nil {
								return UpstreamResult{StatusCode: resp.StatusCode, Usage: estimate, Wrote: true, ErrorType: "client_disconnected"}, err
							}
							wroteTools = true
						}
					}
				}
				finishReason := "stop"
				if wroteTools {
					finishReason = "tool_calls"
				}
				if err := writeChunk(map[string]any{}, finishReason); err != nil {
					return UpstreamResult{StatusCode: resp.StatusCode, Usage: estimate, Wrote: true, ErrorType: "client_disconnected"}, err
				}
				if _, err := io.WriteString(w, "data: [DONE]\n\n"); err != nil {
					return UpstreamResult{StatusCode: resp.StatusCode, Usage: estimate, Wrote: true, ErrorType: "client_disconnected"}, err
				}
				if flusher != nil {
					flusher.Flush()
				}
				if usage.TotalTokens <= 0 {
					usage = estimate
					usage.Estimated = true
				}
				return UpstreamResult{StatusCode: resp.StatusCode, Usage: usage, Wrote: true}, nil
			}
			if eventType == "error" || eventType == "response.failed" {
				return UpstreamResult{StatusCode: resp.StatusCode, Usage: estimate, Wrote: true, ErrorType: "upstream_stream_interrupted"}, ErrUpstreamUnavailable
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return UpstreamResult{StatusCode: resp.StatusCode, Usage: estimate, Wrote: true, ErrorType: "upstream_stream_interrupted"}, err
	}
	return UpstreamResult{StatusCode: resp.StatusCode, Usage: estimate, Wrote: true, ErrorType: "upstream_stream_interrupted"}, ErrUpstreamUnavailable
}

func streamGeminiResponse(
	resp *http.Response,
	kind string,
	model string,
	estimate UpstreamUsage,
	w http.ResponseWriter,
) (UpstreamResult, error) {
	copyHeaders(w.Header(), resp.Header)
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(resp.StatusCode)
	flusher, _ := w.(http.Flusher)
	idSuffix := time.Now().Format("20060102150405")
	created := time.Now().Unix()
	sequence := 0
	responseID := "resp_" + idSuffix
	messageID := "msg_" + idSuffix

	writeEvent := func(event string, payload map[string]any) error {
		encoded, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		if event != "" {
			if _, err := fmt.Fprintf(w, "event: %s\n", event); err != nil {
				return err
			}
		}
		if _, err := fmt.Fprintf(w, "data: %s\n\n", encoded); err != nil {
			return err
		}
		if flusher != nil {
			flusher.Flush()
		}
		return nil
	}
	writeChatChunk := func(delta map[string]any, finish any) error {
		return writeEvent("", map[string]any{
			"id": "chatcmpl_" + idSuffix, "object": "chat.completion.chunk",
			"created": created, "model": model,
			"choices": []map[string]any{{
				"index": 0, "delta": delta, "finish_reason": finish,
			}},
		})
	}
	if kind == "responses" {
		sequence++
		if err := writeEvent("response.created", map[string]any{
			"type": "response.created", "sequence_number": sequence,
			"response": map[string]any{
				"id": responseID, "object": "response", "created_at": created,
				"model": model, "status": "in_progress", "output": []any{},
			},
		}); err != nil {
			return UpstreamResult{StatusCode: resp.StatusCode, Wrote: true, ErrorType: "client_disconnected"}, err
		}
		sequence++
		if err := writeEvent("response.output_item.added", map[string]any{
			"type": "response.output_item.added", "sequence_number": sequence, "output_index": 0,
			"item": map[string]any{
				"id": messageID, "type": "message", "role": "assistant",
				"status": "in_progress", "content": []any{},
			},
		}); err != nil {
			return UpstreamResult{StatusCode: resp.StatusCode, Wrote: true, ErrorType: "client_disconnected"}, err
		}
		sequence++
		if err := writeEvent("response.content_part.added", map[string]any{
			"type": "response.content_part.added", "sequence_number": sequence,
			"item_id": messageID, "output_index": 0, "content_index": 0,
			"part": map[string]any{"type": "output_text", "text": ""},
		}); err != nil {
			return UpstreamResult{StatusCode: resp.StatusCode, Wrote: true, ErrorType: "client_disconnected"}, err
		}
	} else if err := writeChatChunk(map[string]any{"role": "assistant"}, nil); err != nil {
		return UpstreamResult{StatusCode: resp.StatusCode, Wrote: true, ErrorType: "client_disconnected"}, err
	}

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 64*1024), 16<<20)
	usage := UpstreamUsage{}
	textParts := make([]string, 0)
	finishReason := "stop"
	seenPayload := false
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "" || data == "[DONE]" {
			continue
		}
		var payload map[string]any
		if json.Unmarshal([]byte(data), &payload) != nil {
			continue
		}
		seenPayload = true
		if _, failed := payload["error"].(map[string]any); failed {
			estimate.Estimated = true
			return UpstreamResult{
				StatusCode: resp.StatusCode, Usage: estimate, Wrote: true,
				ErrorType: "upstream_stream_interrupted",
			}, ErrUpstreamUnavailable
		}
		if rawUsage, err := json.Marshal(payload); err == nil {
			if parsed := parseUsage(rawUsage); parsed.TotalTokens > 0 {
				usage = parsed
			}
		}
		if mapped := geminiFinishReason(payload); mapped != "" {
			finishReason = mapped
		}
		text := geminiResponseText(payload)
		if text == "" {
			continue
		}
		textParts = append(textParts, text)
		if kind == "responses" {
			sequence++
			if err := writeEvent("response.output_text.delta", map[string]any{
				"type": "response.output_text.delta", "sequence_number": sequence,
				"item_id": messageID, "output_index": 0, "content_index": 0, "delta": text,
			}); err != nil {
				return UpstreamResult{StatusCode: resp.StatusCode, Usage: estimate, Wrote: true, ErrorType: "client_disconnected"}, err
			}
		} else if err := writeChatChunk(map[string]any{"content": text}, nil); err != nil {
			return UpstreamResult{StatusCode: resp.StatusCode, Usage: estimate, Wrote: true, ErrorType: "client_disconnected"}, err
		}
	}
	if err := scanner.Err(); err != nil {
		estimate.Estimated = true
		return UpstreamResult{StatusCode: resp.StatusCode, Usage: estimate, Wrote: true, ErrorType: "upstream_stream_interrupted"}, err
	}
	if !seenPayload {
		estimate.Estimated = true
		return UpstreamResult{StatusCode: resp.StatusCode, Usage: estimate, Wrote: true, ErrorType: "upstream_stream_interrupted"}, ErrUpstreamUnavailable
	}
	if usage.TotalTokens <= 0 {
		usage = estimate
		usage.Estimated = true
	}
	fullText := strings.Join(textParts, "")
	if kind == "responses" {
		sequence++
		if err := writeEvent("response.output_text.done", map[string]any{
			"type": "response.output_text.done", "sequence_number": sequence,
			"item_id": messageID, "output_index": 0, "content_index": 0, "text": fullText,
		}); err != nil {
			return UpstreamResult{StatusCode: resp.StatusCode, Usage: usage, Wrote: true, ErrorType: "client_disconnected"}, err
		}
		response := map[string]any{
			"id": responseID, "object": "response", "created_at": created,
			"model": model, "status": "completed",
			"output": []map[string]any{{
				"id": messageID, "type": "message", "role": "assistant", "status": "completed",
				"content": []map[string]any{{"type": "output_text", "text": fullText}},
			}},
			"usage": responsesUsage(usage),
		}
		sequence++
		if err := writeEvent("response.content_part.done", map[string]any{
			"type": "response.content_part.done", "sequence_number": sequence,
			"item_id": messageID, "output_index": 0, "content_index": 0,
			"part": map[string]any{"type": "output_text", "text": fullText},
		}); err != nil {
			return UpstreamResult{StatusCode: resp.StatusCode, Usage: usage, Wrote: true, ErrorType: "client_disconnected"}, err
		}
		sequence++
		if err := writeEvent("response.output_item.done", map[string]any{
			"type": "response.output_item.done", "sequence_number": sequence, "output_index": 0,
			"item": response["output"].([]map[string]any)[0],
		}); err != nil {
			return UpstreamResult{StatusCode: resp.StatusCode, Usage: usage, Wrote: true, ErrorType: "client_disconnected"}, err
		}
		sequence++
		if err := writeEvent("response.completed", map[string]any{
			"type": "response.completed", "sequence_number": sequence, "response": response,
		}); err != nil {
			return UpstreamResult{StatusCode: resp.StatusCode, Usage: usage, Wrote: true, ErrorType: "client_disconnected"}, err
		}
	} else {
		if err := writeChatChunk(map[string]any{}, finishReason); err != nil {
			return UpstreamResult{StatusCode: resp.StatusCode, Usage: usage, Wrote: true, ErrorType: "client_disconnected"}, err
		}
		if _, err := io.WriteString(w, "data: [DONE]\n\n"); err != nil {
			return UpstreamResult{StatusCode: resp.StatusCode, Usage: usage, Wrote: true, ErrorType: "client_disconnected"}, err
		}
		if flusher != nil {
			flusher.Flush()
		}
	}
	return UpstreamResult{StatusCode: resp.StatusCode, Usage: usage, Wrote: true}, nil
}

func geminiResponseText(raw map[string]any) string {
	candidates, ok := raw["candidates"].([]any)
	if !ok || len(candidates) == 0 {
		return ""
	}
	first, ok := candidates[0].(map[string]any)
	if !ok {
		return ""
	}
	content, ok := first["content"].(map[string]any)
	if !ok {
		return ""
	}
	parts, ok := content["parts"].([]any)
	if !ok {
		return ""
	}
	values := make([]string, 0, len(parts))
	for _, rawPart := range parts {
		if part, ok := rawPart.(map[string]any); ok {
			if text := asString(part["text"]); text != "" {
				values = append(values, text)
			}
		}
	}
	return strings.Join(values, "")
}

func geminiFinishReason(raw map[string]any) string {
	if feedback, ok := raw["promptFeedback"].(map[string]any); ok &&
		strings.TrimSpace(asString(feedback["blockReason"])) != "" {
		return "content_filter"
	}
	candidates, ok := raw["candidates"].([]any)
	if !ok || len(candidates) == 0 {
		return ""
	}
	first, ok := candidates[0].(map[string]any)
	if !ok {
		return ""
	}
	switch strings.ToUpper(strings.TrimSpace(asString(first["finishReason"]))) {
	case "", "FINISH_REASON_UNSPECIFIED":
		return ""
	case "MAX_TOKENS":
		return "length"
	case "SAFETY", "RECITATION", "BLOCKLIST", "PROHIBITED_CONTENT", "SPII":
		return "content_filter"
	default:
		return "stop"
	}
}

func applyOpenAIProviderConfig(payload map[string]any, config map[string]any) {
	if _, exists := payload["temperature"]; !exists {
		if value, ok := configFloat(config, "temperature", 0, 2); ok {
			payload["temperature"] = value
		}
	}
	if firstPresent(payload, "max_tokens", "maxTokens") == nil {
		if value, ok := configInt(config, "maxTokens", 1, 200000); ok {
			payload["max_tokens"] = value
		}
	}
	if firstPresent(payload, "top_p", "topP") == nil {
		if value, ok := configFloat(config, "topP", 0, 1); ok {
			payload["top_p"] = value
		}
	}
	if _, exists := payload["stop"]; !exists {
		if values, ok := configStringList(config, "stop"); ok && len(values) > 0 {
			payload["stop"] = values
		}
	}
}

func applyAnthropicProviderConfig(body map[string]any, config map[string]any) {
	if _, exists := body["temperature"]; !exists {
		if value, ok := configFloat(config, "temperature", 0, 2); ok {
			body["temperature"] = value
		}
	}
	if _, exists := body["top_p"]; !exists {
		if value, ok := configFloat(config, "topP", 0, 1); ok {
			body["top_p"] = value
		}
	}
	if _, exists := body["top_k"]; !exists {
		if value, ok := configInt(config, "topK", 1, 1000); ok {
			body["top_k"] = value
		}
	}
	if _, exists := body["stop_sequences"]; !exists {
		if values, ok := configStringList(config, "stop"); ok && len(values) > 0 {
			body["stop_sequences"] = values
		}
	}
}

func applyGeminiProviderConfig(body map[string]any, config map[string]any) {
	applyGeminiRequestConfig(body, nil, config)
}

func applyGeminiRequestConfig(body map[string]any, payload map[string]any, config map[string]any) {
	generationConfig := map[string]any{}
	if value, ok := configFloat(config, "temperature", 0, 2); ok {
		generationConfig["temperature"] = value
	}
	if value, ok := configFloat(config, "topP", 0, 1); ok {
		generationConfig["topP"] = value
	}
	if value, ok := configInt(config, "topK", 1, 1000); ok {
		generationConfig["topK"] = value
	}
	if value := firstPresent(payload, "max_tokens", "maxTokens"); value != nil {
		if maxTokens := numberOrDefault(value, 0); maxTokens > 0 {
			generationConfig["maxOutputTokens"] = maxTokens
		}
	} else if value, ok := configInt(config, "maxTokens", 1, 200000); ok {
		generationConfig["maxOutputTokens"] = value
	}
	if values, ok := configStringList(config, "stop"); ok && len(values) > 0 {
		generationConfig["stopSequences"] = values
	}
	if len(generationConfig) > 0 {
		body["generationConfig"] = generationConfig
	}
}

func providerMaxTokens(config map[string]any, fallback int) int {
	if value, ok := configInt(config, "maxTokens", 1, 200000); ok {
		return value
	}
	return fallback
}

func adaptResponseBody(upstream Upstream, kind string, body []byte) ([]byte, UpstreamUsage) {
	provider := upstream.adapterKind()
	usage := parseUsage(body)
	switch {
	case strings.Contains(provider, "anthropic"):
		return mapAnthropicResponse(upstream, kind, body), usage
	case strings.Contains(provider, "google"), strings.Contains(provider, "gemini"):
		return mapGeminiResponse(upstream, kind, body), usage
	default:
		return body, usage
	}
}

func mapModelsResponse(upstream Upstream, body []byte, includeConfiguredFallback bool) []byte {
	var raw map[string]any
	seen := map[string]bool{}
	if err := json.Unmarshal(body, &raw); err == nil {
		if data, ok := raw["data"].([]any); ok {
			out := make([]map[string]any, 0, len(data)+1)
			for _, row := range data {
				if item, ok := row.(map[string]any); ok {
					id := asString(item["id"])
					if id == "" {
						id = asString(item["name"])
					}
					if id != "" && !seen[id] {
						seen[id] = true
						out = append(out, modelItem(id, upstream.Provider))
					}
				}
			}
			if model := strings.TrimSpace(upstream.Model); includeConfiguredFallback && model != "" && !seen[model] {
				out = append(out, modelItem(model, upstream.Provider))
			}
			encoded, _ := json.Marshal(map[string]any{"object": "list", "data": out})
			return encoded
		}
		if models, ok := raw["models"].([]any); ok {
			data := make([]map[string]any, 0, len(models))
			for _, model := range models {
				if m, ok := model.(map[string]any); ok {
					id := asString(m["id"])
					if id == "" {
						id = asString(m["name"])
					}
					if id != "" && !seen[id] {
						seen[id] = true
						data = append(data, modelItem(id, upstream.Provider))
					}
				}
			}
			if model := strings.TrimSpace(upstream.Model); includeConfiguredFallback && model != "" && !seen[model] {
				data = append(data, modelItem(model, upstream.Provider))
			}
			if len(data) > 0 {
				encoded, _ := json.Marshal(map[string]any{"object": "list", "data": data})
				return encoded
			}
		}
	}
	if !includeConfiguredFallback {
		encoded, _ := json.Marshal(map[string]any{"object": "list", "data": []map[string]any{}})
		return encoded
	}
	out, _ := json.Marshal(map[string]any{"object": "list", "data": []map[string]any{modelItem(upstream.Model, upstream.Provider)}})
	return out
}

func mapAnthropicResponse(upstream Upstream, kind string, body []byte) []byte {
	var raw map[string]any
	_ = json.Unmarshal(body, &raw)
	text := ""
	if content, ok := raw["content"].([]any); ok {
		parts := []string{}
		for _, item := range content {
			if m, ok := item.(map[string]any); ok && asString(m["type"]) == "text" {
				parts = append(parts, asString(m["text"]))
			}
		}
		text = strings.Join(parts, "")
	}
	if text == "" {
		text = asString(raw["completion"])
	}
	if kind == "responses" {
		return responseObject(upstream.Model, text, parseUsage(body))
	}
	return chatObject(upstream.Model, text, parseUsage(body))
}

func mapGeminiResponse(upstream Upstream, kind string, body []byte) []byte {
	var raw map[string]any
	_ = json.Unmarshal(body, &raw)
	text := geminiResponseText(raw)
	if kind == "responses" {
		return responseObject(upstream.Model, text, parseUsage(body))
	}
	return chatObject(upstream.Model, text, parseUsage(body))
}

func chatObject(model string, text string, usage UpstreamUsage) []byte {
	return chatObjectWithTools(model, text, nil, usage)
}

func chatObjectWithTools(model string, text string, toolCalls []map[string]any, usage UpstreamUsage) []byte {
	message := map[string]any{"role": "assistant", "content": text}
	finishReason := "stop"
	if len(toolCalls) > 0 {
		message["tool_calls"] = toolCalls
		finishReason = "tool_calls"
		if text == "" {
			message["content"] = nil
		}
	}
	raw, _ := json.Marshal(map[string]any{
		"id":      "chatcmpl_" + time.Now().Format("20060102150405"),
		"object":  "chat.completion",
		"created": time.Now().Unix(),
		"model":   model,
		"choices": []map[string]any{{
			"index":         0,
			"message":       message,
			"finish_reason": finishReason,
		}},
		"usage": usage,
	})
	return raw
}

func responseObject(model string, text string, usage UpstreamUsage) []byte {
	raw, _ := json.Marshal(map[string]any{
		"id":         "resp_" + time.Now().Format("20060102150405"),
		"object":     "response",
		"created_at": time.Now().Unix(),
		"model":      model,
		"status":     "completed",
		"output": []map[string]any{{
			"type": "message",
			"role": "assistant",
			"content": []map[string]any{{
				"type": "output_text",
				"text": text,
			}},
		}},
		"usage": responsesUsage(usage),
	})
	return raw
}

func responsesUsage(usage UpstreamUsage) map[string]any {
	return map[string]any{
		"input_tokens":  usage.PromptTokens,
		"output_tokens": usage.CompletionTokens,
		"total_tokens":  usage.TotalTokens,
	}
}

func parseUsage(body []byte) UpstreamUsage {
	var raw map[string]any
	if err := json.Unmarshal(body, &raw); err != nil {
		return UpstreamUsage{}
	}
	if usageMap, ok := raw["usage"].(map[string]any); ok {
		return usageFromMap(usageMap)
	}
	if usageMap, ok := raw["usageMetadata"].(map[string]any); ok {
		return usageFromMap(usageMap)
	}
	return UpstreamUsage{}
}

func usageFromMap(values map[string]any) UpstreamUsage {
	prompt := intFromAny(firstPresent(values, "prompt_tokens", "input_tokens", "inputTokens", "promptTokenCount"))
	completion := intFromAny(firstPresent(values, "completion_tokens", "output_tokens", "outputTokens", "candidatesTokenCount"))
	total := intFromAny(firstPresent(values, "total_tokens", "totalTokens", "totalTokenCount"))
	if total == 0 && (prompt > 0 || completion > 0) {
		total = prompt + completion
	}
	return UpstreamUsage{PromptTokens: prompt, CompletionTokens: completion, TotalTokens: total}
}

func messagesForAnthropic(payload map[string]any) []map[string]any {
	if messages, ok := payload["messages"].([]any); ok {
		out := []map[string]any{}
		for _, item := range messages {
			if m, ok := item.(map[string]any); ok {
				role := asString(m["role"])
				if role == "system" {
					continue
				}
				if role == "" {
					role = "user"
				}
				out = append(out, map[string]any{"role": role, "content": m["content"]})
			}
		}
		if len(out) > 0 {
			return out
		}
	}
	return []map[string]any{{"role": "user", "content": fmt.Sprint(payload["input"])}}
}

func contentsForGemini(payload map[string]any) []map[string]any {
	out := []map[string]any{}
	if messages, ok := payload["messages"].([]any); ok {
		out = appendGeminiMessages(out, messages)
	}
	if len(out) == 0 {
		if input, ok := payload["input"].([]any); ok {
			out = appendGeminiMessages(out, input)
		}
	}
	if len(out) == 0 {
		out = append(out, map[string]any{"role": "user", "parts": []map[string]any{{"text": contentText(payload["input"])}}})
	}
	return out
}

func appendGeminiMessages(out []map[string]any, messages []any) []map[string]any {
	for _, item := range messages {
		message, ok := item.(map[string]any)
		if !ok {
			continue
		}
		role := strings.ToLower(strings.TrimSpace(asString(message["role"])))
		if role == "system" || role == "developer" {
			continue
		}
		if role == "assistant" {
			role = "model"
		} else {
			role = "user"
		}
		content := message["content"]
		if content == nil {
			content = firstPresent(message, "text", "input_text")
		}
		out = append(out, map[string]any{
			"role": role, "parts": []map[string]any{{"text": contentText(content)}},
		})
	}
	return out
}

func systemInstructionForGemini(payload map[string]any) string {
	instructions := make([]string, 0)
	for _, key := range []string{"instructions", "system"} {
		if text := contentText(payload[key]); text != "" {
			instructions = append(instructions, text)
		}
	}
	for _, key := range []string{"messages", "input"} {
		items, ok := payload[key].([]any)
		if !ok {
			continue
		}
		for _, item := range items {
			message, ok := item.(map[string]any)
			if !ok {
				continue
			}
			role := strings.ToLower(strings.TrimSpace(asString(message["role"])))
			if role != "system" && role != "developer" {
				continue
			}
			if text := contentText(message["content"]); text != "" {
				instructions = append(instructions, text)
			}
		}
	}
	return strings.Join(instructions, "\n\n")
}

func copyHeaders(dst http.Header, src http.Header) {
	for key, values := range src {
		canonical := http.CanonicalHeaderKey(key)
		lower := strings.ToLower(canonical)
		if canonical != "Content-Type" &&
			canonical != "Content-Language" &&
			canonical != "Content-Encoding" &&
			canonical != "Retry-After" &&
			canonical != "X-Request-Id" &&
			canonical != "Request-Id" &&
			canonical != "Openai-Processing-Ms" &&
			canonical != "Openai-Version" &&
			!strings.HasPrefix(lower, "x-ratelimit-") {
			continue
		}
		for _, value := range values {
			if !strings.ContainsAny(value, "\r\n") {
				dst.Add(canonical, value)
			}
		}
	}
}

func modelItem(id string, owner string) map[string]any {
	if id == "" {
		id = "default"
	}
	return map[string]any{"id": id, "object": "model", "owned_by": owner}
}

func modelFor(payload map[string]any, upstream Upstream) string {
	if model := asString(payload["model"]); model != "" {
		return model
	}
	if strings.TrimSpace(upstream.Model) != "" {
		return strings.TrimSpace(upstream.Model)
	}
	return "default"
}

func numberOrDefault(value any, fallback int) int {
	n := intFromAny(value)
	if n <= 0 {
		return fallback
	}
	return n
}

func firstPresent(values map[string]any, keys ...string) any {
	for _, key := range keys {
		if value, ok := values[key]; ok {
			return value
		}
	}
	return nil
}

func configFloat(config map[string]any, key string, min float64, max float64) (float64, bool) {
	if len(config) == 0 {
		return 0, false
	}
	value, ok := config[key]
	if !ok {
		return 0, false
	}
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
	case json.Number:
		parsed, err := v.Float64()
		if err != nil {
			return 0, false
		}
		n = parsed
	default:
		return 0, false
	}
	if n < min || n > max {
		return 0, false
	}
	return n, true
}

func configInt(config map[string]any, key string, min int, max int) (int, bool) {
	if len(config) == 0 {
		return 0, false
	}
	value, ok := config[key]
	if !ok {
		return 0, false
	}
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
	case json.Number:
		parsed, err := v.Int64()
		if err != nil {
			return 0, false
		}
		n = int(parsed)
	default:
		return 0, false
	}
	if n < min || n > max {
		return 0, false
	}
	return n, true
}

func configString(config map[string]any, key string) (string, bool) {
	if len(config) == 0 {
		return "", false
	}
	value, ok := config[key]
	if !ok {
		return "", false
	}
	text, ok := value.(string)
	if !ok {
		return "", false
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return "", false
	}
	return text, true
}

func configStringList(config map[string]any, key string) ([]string, bool) {
	if len(config) == 0 {
		return nil, false
	}
	value, ok := config[key]
	if !ok {
		return nil, false
	}
	if text, ok := value.(string); ok {
		text = strings.TrimSpace(text)
		if text == "" {
			return nil, true
		}
		return []string{text}, true
	}
	switch values := value.(type) {
	case []string:
		out := make([]string, 0, len(values))
		for _, text := range values {
			if text = strings.TrimSpace(text); text != "" {
				out = append(out, text)
			}
		}
		return out, true
	case []any:
		out := make([]string, 0, len(values))
		for _, item := range values {
			text, ok := item.(string)
			if !ok {
				return nil, false
			}
			if text = strings.TrimSpace(text); text != "" {
				out = append(out, text)
			}
		}
		return out, true
	default:
		return nil, false
	}
}

func intFromAny(value any) int {
	switch v := value.(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	case json.Number:
		n, _ := v.Int64()
		return int(n)
	default:
		return 0
	}
}

func asString(value any) string {
	switch v := value.(type) {
	case string:
		return strings.TrimSpace(v)
	default:
		return ""
	}
}

func upstreamStatusError(status int) string {
	switch status {
	case http.StatusUnauthorized, http.StatusForbidden:
		return "upstream_auth_error"
	case http.StatusTooManyRequests:
		return "upstream_rate_limited"
	default:
		if status >= 500 {
			return "upstream_unavailable"
		}
		return "upstream_rejected"
	}
}

func classifyHTTPError(err error) string {
	if err == nil {
		return ""
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "upstream_timeout"
	}
	return "upstream_unreachable"
}
