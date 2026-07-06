package gateway

import (
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
)

const defaultTimeout = 60 * time.Second

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
	PromptTokens     int
	CompletionTokens int
	TotalTokens      int
	Estimated        bool
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
	return &UpstreamClient{httpClient: &http.Client{Timeout: defaultTimeout}}
}

func (c *UpstreamClient) Models(ctx context.Context, upstream Upstream, apiKey string) (UpstreamResult, error) {
	return c.models(ctx, upstream, apiKey, true)
}

func (c *UpstreamClient) ModelsStrict(ctx context.Context, upstream Upstream, apiKey string) (UpstreamResult, error) {
	return c.models(ctx, upstream, apiKey, false)
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
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return UpstreamResult{ErrorType: classifyHTTPError(err)}, ErrUpstreamUnavailable
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
		return UpstreamResult{StatusCode: resp.StatusCode, ErrorType: upstreamStatusError(resp.StatusCode)}, ErrUpstreamUnavailable
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
	target := joinEndpointPath(upstream.adapterKind(), endpoint, path)
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

func upstreamRequestContext(ctx context.Context, upstream Upstream) (context.Context, context.CancelFunc) {
	timeout := defaultTimeout
	if timeoutMs, ok := configInt(upstream.ProviderConfig, "timeoutMs", 1, 300000); ok {
		timeout = time.Duration(timeoutMs) * time.Millisecond
	}
	return context.WithTimeout(ctx, timeout)
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
		applyGeminiRequestConfig(body, payload, upstream.ProviderConfig)
		encoded, err := json.Marshal(body)
		path := "/models/" + url.PathEscape(modelFor(payload, upstream)) + ":generateContent"
		if stream {
			path = "/models/" + url.PathEscape(modelFor(payload, upstream)) + ":streamGenerateContent"
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
	text := ""
	if candidates, ok := raw["candidates"].([]any); ok && len(candidates) > 0 {
		if first, ok := candidates[0].(map[string]any); ok {
			if content, ok := first["content"].(map[string]any); ok {
				if parts, ok := content["parts"].([]any); ok {
					values := []string{}
					for _, part := range parts {
						if m, ok := part.(map[string]any); ok {
							values = append(values, asString(m["text"]))
						}
					}
					text = strings.Join(values, "")
				}
			}
		}
	}
	if kind == "responses" {
		return responseObject(upstream.Model, text, parseUsage(body))
	}
	return chatObject(upstream.Model, text, parseUsage(body))
}

func chatObject(model string, text string, usage UpstreamUsage) []byte {
	raw, _ := json.Marshal(map[string]any{
		"id":      "chatcmpl_" + time.Now().Format("20060102150405"),
		"object":  "chat.completion",
		"created": time.Now().Unix(),
		"model":   model,
		"choices": []map[string]any{{
			"index":         0,
			"message":       map[string]any{"role": "assistant", "content": text},
			"finish_reason": "stop",
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
		"usage": usage,
	})
	return raw
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
		for _, item := range messages {
			if m, ok := item.(map[string]any); ok {
				role := asString(m["role"])
				if role == "assistant" {
					role = "model"
				} else if role == "" || role == "system" {
					role = "user"
				}
				out = append(out, map[string]any{"role": role, "parts": []map[string]any{{"text": fmt.Sprint(m["content"])}}})
			}
		}
	}
	if len(out) == 0 {
		out = append(out, map[string]any{"role": "user", "parts": []map[string]any{{"text": fmt.Sprint(payload["input"])}}})
	}
	return out
}

func copyHeaders(dst http.Header, src http.Header) {
	for key, values := range src {
		lower := strings.ToLower(key)
		if lower == "content-length" || lower == "connection" || lower == "transfer-encoding" {
			continue
		}
		for _, value := range values {
			dst.Add(key, value)
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
