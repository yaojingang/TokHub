package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"tokhub/internal/connections"
)

func TestJoinEndpointPathAddsV1ForRootProviderEndpoints(t *testing.T) {
	tests := []struct {
		name     string
		provider string
		endpoint string
		path     string
		want     string
	}{
		{
			name:     "openai compatible root",
			provider: "OpenAI",
			endpoint: "https://api.aigocode.com",
			path:     "/chat/completions",
			want:     "https://api.aigocode.com/v1/chat/completions",
		},
		{
			name:     "anthropic root",
			provider: "Anthropic",
			endpoint: "https://api.aigocode.com",
			path:     "/messages",
			want:     "https://api.aigocode.com/v1/messages",
		},
		{
			name:     "already versioned",
			provider: "OpenAI",
			endpoint: "https://api.aigocode.com/v1",
			path:     "/models",
			want:     "https://api.aigocode.com/v1/models",
		},
		{
			name:     "converter prefix",
			provider: "Anthropic",
			endpoint: "https://cc-api.pipellm.ai/anthropic",
			path:     "/messages",
			want:     "https://cc-api.pipellm.ai/anthropic/v1/messages",
		},
		{
			name:     "gemini keeps configured version root",
			provider: "Gemini",
			endpoint: "https://generativelanguage.googleapis.com/v1beta",
			path:     "/models/gemini-pro:generateContent",
			want:     "https://generativelanguage.googleapis.com/v1beta/models/gemini-pro:generateContent",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := joinEndpointPath(tt.provider, tt.endpoint, tt.path); got != tt.want {
				t.Fatalf("joinEndpointPath() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestAdaptRequestBodyAppliesOpenAIProviderConfig(t *testing.T) {
	_, body, err := adaptRequestBody(Upstream{
		Provider: "OpenAI",
		Model:    "gpt-4o-mini",
		ProviderConfig: map[string]any{
			"temperature": 0.2,
			"topP":        0.8,
			"maxTokens":   64,
			"stop":        []string{"END"},
		},
	}, "chat", []byte(`{"messages":[{"role":"user","content":"ping"}]}`), false)
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatal(err)
	}
	if payload["temperature"] != 0.2 || payload["top_p"] != 0.8 || payload["max_tokens"] != float64(64) {
		t.Fatalf("provider config was not applied: %#v", payload)
	}
	if stops, ok := payload["stop"].([]any); !ok || len(stops) != 1 || stops[0] != "END" {
		t.Fatalf("stop config was not applied: %#v", payload["stop"])
	}
}

func TestAdaptRequestBodyAppliesAnthropicProviderConfig(t *testing.T) {
	_, body, err := adaptRequestBody(Upstream{
		Provider: "Anthropic",
		Model:    "claude-sonnet-4-6",
		ProviderConfig: map[string]any{
			"temperature": 0.1,
			"topK":        40,
			"maxTokens":   32,
			"stop":        []any{"STOP"},
		},
	}, "chat", []byte(`{"messages":[{"role":"user","content":"ping"}]}`), false)
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatal(err)
	}
	if payload["temperature"] != 0.1 || payload["top_k"] != float64(40) || payload["max_tokens"] != float64(32) {
		t.Fatalf("provider config was not applied: %#v", payload)
	}
	if stops, ok := payload["stop_sequences"].([]any); !ok || len(stops) != 1 || stops[0] != "STOP" {
		t.Fatalf("stop config was not applied: %#v", payload["stop_sequences"])
	}
}

func TestUpstreamTypeSelectsAdapterOverProviderName(t *testing.T) {
	client := NewUpstreamClient()
	req, err := client.newRequest(context.Background(), Upstream{
		Provider: "AIGoCode",
		Type:     "anthropic",
		Endpoint: "https://api.aigocode.com",
		Model:    "claude-sonnet-4-6",
	}, "test-key", http.MethodPost, "/messages", []byte(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	if got := req.URL.String(); got != "https://api.aigocode.com/v1/messages" {
		t.Fatalf("newRequest URL = %q, want anthropic messages URL", got)
	}
	if got := req.Header.Get("X-API-Key"); got != "test-key" {
		t.Fatalf("X-API-Key header = %q, want test-key", got)
	}
	if got := req.Header.Get("Authorization"); got != "" {
		t.Fatalf("Authorization header = %q, want empty for anthropic", got)
	}

	path, _, err := adaptRequestBody(Upstream{
		Provider: "AIGoCode",
		Type:     "anthropic",
		Model:    "claude-sonnet-4-6",
	}, "chat", []byte(`{"messages":[{"role":"user","content":"ping"}]}`), false)
	if err != nil {
		t.Fatal(err)
	}
	if path != "/messages" {
		t.Fatalf("adaptRequestBody path = %q, want /messages", path)
	}
}

func TestProviderPathModeDirectKeepsOfficialVersionedPrefix(t *testing.T) {
	client := NewUpstreamClient()
	req, err := client.newRequest(context.Background(), Upstream{
		Provider: "豆包",
		Type:     "openai-compatible",
		Endpoint: "https://ark.cn-beijing.volces.com/api/v3",
		Model:    "doubao-seed-2-0-lite-260215",
		ProviderConfig: map[string]any{
			"pathMode": "direct",
		},
	}, "test-key", http.MethodPost, "/responses", []byte(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	want := "https://ark.cn-beijing.volces.com/api/v3/responses"
	if got := req.URL.String(); got != want {
		t.Fatalf("newRequest URL = %q, want %q", got, want)
	}
}

func TestAnthropicProviderConfigCanUseBearerAuth(t *testing.T) {
	client := NewUpstreamClient()
	req, err := client.newRequest(context.Background(), Upstream{
		Provider: "PackyCode",
		Type:     "anthropic",
		Endpoint: "https://www.packyapi.com",
		Model:    "claude-sonnet-4-6",
		ProviderConfig: map[string]any{
			"authHeader": "authorization",
		},
	}, "test-key", http.MethodGet, "/models", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := req.Header.Get("Authorization"); got != "Bearer test-key" {
		t.Fatalf("Authorization header = %q, want bearer token", got)
	}
	if got := req.Header.Get("X-API-Key"); got != "" {
		t.Fatalf("X-API-Key header = %q, want empty when bearer auth is configured", got)
	}
}

func TestClientProfileCanSetClaudeCodeUserAgent(t *testing.T) {
	client := NewUpstreamClient()
	req, err := client.newRequest(context.Background(), Upstream{
		Provider: "AIGoCode",
		Type:     "anthropic",
		Endpoint: "https://api.aigocode.app",
		Model:    "claude-sonnet-4-6",
		ProviderConfig: map[string]any{
			"clientProfile": "claude-code",
			"clientVersion": "2.1.114",
		},
	}, "test-key", http.MethodPost, "/messages", []byte(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	if got := req.UserAgent(); got != "claude-code/2.1.114" {
		t.Fatalf("User-Agent = %q, want claude-code/2.1.114", got)
	}
}

func TestMapModelsResponseIncludesConfiguredModelFallback(t *testing.T) {
	body := mapModelsResponse(Upstream{Provider: "AIGoCode", Model: "gpt-5.5"}, []byte(`{"object":"list","data":[]}`), true)
	var payload struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Data) != 1 || payload.Data[0].ID != "gpt-5.5" {
		t.Fatalf("mapped models = %#v, want configured model fallback", payload.Data)
	}
}

func TestMapModelsResponseCanOmitConfiguredModelFallback(t *testing.T) {
	body := mapModelsResponse(Upstream{Provider: "AIGoCode", Model: "gpt-5.5"}, []byte(`{"object":"list","data":[]}`), false)
	var payload struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Data) != 0 {
		t.Fatalf("mapped models = %#v, want no fallback models", payload.Data)
	}
}

func TestJSONPreservesNon2xxResponseBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"error":"No available channel for model gpt-5.5"}`))
	}))
	defer server.Close()

	client := NewUpstreamClient()
	result, err := client.JSON(context.Background(), Upstream{
		Type:     "openai-compatible",
		Endpoint: server.URL,
		Model:    "gpt-5.5",
	}, "sk-test", "chat", []byte(`{"messages":[{"role":"user","content":"ping"}]}`), UpstreamUsage{})
	if !errors.Is(err, ErrUpstreamUnavailable) {
		t.Fatalf("err=%v, want ErrUpstreamUnavailable", err)
	}
	if result.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status=%d, want 503", result.StatusCode)
	}
	if !strings.Contains(string(result.Body), "No available channel") {
		t.Fatalf("body=%q, want preserved upstream error", string(result.Body))
	}
}

func TestAdaptRequestBodyAppliesGeminiProviderConfig(t *testing.T) {
	_, body, err := adaptRequestBody(Upstream{
		Provider: "Gemini",
		Model:    "gemini-2.5-pro",
		ProviderConfig: map[string]any{
			"temperature": 0.3,
			"topP":        0.7,
			"topK":        30,
			"maxTokens":   24,
		},
	}, "chat", []byte(`{"messages":[{"role":"user","content":"ping"}]}`), false)
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatal(err)
	}
	generation, ok := payload["generationConfig"].(map[string]any)
	if !ok {
		t.Fatalf("generationConfig missing: %#v", payload)
	}
	if generation["temperature"] != 0.3 || generation["topP"] != 0.7 || generation["topK"] != float64(30) || generation["maxOutputTokens"] != float64(24) {
		t.Fatalf("provider config was not applied: %#v", generation)
	}
}

func TestAdaptRequestBodyUsesGeminiPayloadMaxTokens(t *testing.T) {
	_, body, err := adaptRequestBody(Upstream{
		Provider: "Gemini",
		Model:    "gemini-2.5-pro",
		ProviderConfig: map[string]any{
			"maxTokens": 24,
		},
	}, "chat", []byte(`{"messages":[{"role":"user","content":"ping"}],"max_tokens":2}`), false)
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatal(err)
	}
	generation, ok := payload["generationConfig"].(map[string]any)
	if !ok {
		t.Fatalf("generationConfig missing: %#v", payload)
	}
	if generation["maxOutputTokens"] != float64(2) {
		t.Fatalf("maxOutputTokens=%v, want payload max_tokens 2", generation["maxOutputTokens"])
	}
}

func TestJSONAppliesProviderTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(120 * time.Millisecond)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"late"}}],"usage":{"total_tokens":1}}`))
	}))
	t.Cleanup(server.Close)

	started := time.Now()
	result, err := NewUpstreamClient().JSON(context.Background(), Upstream{
		Provider: "OpenAI",
		Endpoint: server.URL,
		Model:    "gpt-test",
		ProviderConfig: map[string]any{
			"timeoutMs": 20,
		},
	}, "test-key", "chat", []byte(`{"messages":[{"role":"user","content":"ping"}]}`), UpstreamUsage{})
	if !errors.Is(err, ErrUpstreamUnavailable) {
		t.Fatalf("JSON error = %v, want ErrUpstreamUnavailable", err)
	}
	if result.ErrorType != "upstream_timeout" {
		t.Fatalf("ErrorType = %q, want upstream_timeout", result.ErrorType)
	}
	if elapsed := time.Since(started); elapsed > 500*time.Millisecond {
		t.Fatalf("provider timeout was not applied quickly enough: %s", elapsed)
	}
}

func TestOAuthAuthMaterialPinsEndpointAndTrustedHeaders(t *testing.T) {
	client := NewUpstreamClient()
	request, err := client.newRequestWithAuth(context.Background(), Upstream{
		Provider: "gemini",
		Type:     "gemini",
		Endpoint: "https://user-controlled.example.test/v1beta",
	}, connections.AuthMaterial{
		Mode:     connections.AuthModeOAuthBearer,
		Endpoint: "https://generativelanguage.googleapis.com/v1beta",
		Headers: http.Header{
			"Authorization":       []string{"Bearer access-token"},
			"X-Goog-User-Project": []string{"project-1"},
		},
	}, http.MethodGet, "/models", nil)
	if err != nil {
		t.Fatalf("newRequestWithAuth() error = %v", err)
	}
	if request.URL.String() != "https://generativelanguage.googleapis.com/v1beta/models" {
		t.Fatalf("request URL = %q", request.URL.String())
	}
	if request.Header.Get("Authorization") != "Bearer access-token" ||
		request.Header.Get("X-Goog-User-Project") != "project-1" ||
		request.Header.Get("X-Goog-Api-Key") != "" {
		t.Fatalf("request headers = %v", request.Header)
	}
}

func TestCodexOAuthAdaptsChatRequestToStreamingResponsesProtocol(t *testing.T) {
	path, body, err := adaptRequestBody(Upstream{
		Provider: "openai",
		Type:     "openai",
		Model:    "gpt-5.1-codex",
		ProviderConfig: map[string]any{
			"authMethod": "codex_oauth",
		},
	}, "chat", []byte(`{
		"messages":[
			{"role":"system","content":"Follow the project rules."},
			{"role":"user","content":"ping"}
		],
		"stream":false,
		"max_tokens":12,
		"temperature":0.8,
		"tools":[{"type":"function","function":{"name":"lookup","description":"Find data","parameters":{"type":"object"}}}]
	}`), false)
	if err != nil {
		t.Fatal(err)
	}
	if path != "/responses" {
		t.Fatalf("path = %q, want /responses", path)
	}
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatal(err)
	}
	if payload["stream"] != true || payload["store"] != false ||
		payload["instructions"] != "Follow the project rules." ||
		payload["max_output_tokens"] != float64(12) {
		t.Fatalf("Codex payload = %#v", payload)
	}
	if _, exists := payload["temperature"]; exists {
		t.Fatalf("Codex payload retained unsupported temperature: %#v", payload)
	}
	tools, ok := payload["tools"].([]any)
	if !ok || len(tools) != 1 {
		t.Fatalf("Codex tools = %#v", payload["tools"])
	}
	tool, _ := tools[0].(map[string]any)
	if tool["name"] != "lookup" {
		t.Fatalf("Codex tool = %#v", tool)
	}
}

func TestCodexOAuthMapsCompletedSSEForResponsesAndChatClients(t *testing.T) {
	sse := []byte("event: response.output_text.delta\n" +
		"data: {\"type\":\"response.output_text.delta\",\"delta\":\"hello\"}\n\n" +
		"event: response.completed\n" +
		"data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_1\",\"object\":\"response\",\"status\":\"completed\",\"output\":[{\"type\":\"message\",\"content\":[{\"type\":\"output_text\",\"text\":\"hello\"}]}],\"usage\":{\"input_tokens\":2,\"output_tokens\":1,\"total_tokens\":3}}}\n\n")
	upstream := Upstream{Model: "gpt-5.1-codex"}

	responseBody, usage, err := mapCodexEventResponse(upstream, "responses", sse)
	if err != nil {
		t.Fatalf("responses mapping error = %v", err)
	}
	if !strings.Contains(string(responseBody), `"id":"resp_1"`) || usage.TotalTokens != 3 {
		t.Fatalf("responses body=%s usage=%#v", responseBody, usage)
	}

	chatBody, usage, err := mapCodexEventResponse(upstream, "chat", sse)
	if err != nil {
		t.Fatalf("chat mapping error = %v", err)
	}
	if !strings.Contains(string(chatBody), `"content":"hello"`) || usage.TotalTokens != 3 {
		t.Fatalf("chat body=%s usage=%#v", chatBody, usage)
	}
}

func TestGeminiStreamingUsesSSEQueryAndPreservesItInTargetURL(t *testing.T) {
	upstream := Upstream{
		Provider: "gemini",
		Type:     "gemini",
		Endpoint: "https://generativelanguage.googleapis.com/v1beta",
		Model:    "gemini-2.5-pro",
	}
	path, body, err := adaptRequestBody(upstream, "chat", []byte(`{"messages":[{"role":"user","content":"ping"}]}`), true)
	if err != nil {
		t.Fatal(err)
	}
	if path != "/models/gemini-2.5-pro:streamGenerateContent?alt=sse" {
		t.Fatalf("stream path = %q", path)
	}
	request, err := NewUpstreamClient().newRequest(context.Background(), upstream, "key", http.MethodPost, path, body)
	if err != nil {
		t.Fatal(err)
	}
	if got := request.URL.String(); got != "https://generativelanguage.googleapis.com/v1beta/models/gemini-2.5-pro:streamGenerateContent?alt=sse" {
		t.Fatalf("request URL = %q", got)
	}
}

func TestContentsForGeminiExtractsTextFromOpenAIContentParts(t *testing.T) {
	contents := contentsForGemini(map[string]any{
		"messages": []any{map[string]any{
			"role": "user",
			"content": []any{
				map[string]any{"type": "text", "text": "first"},
				map[string]any{"type": "text", "text": "second"},
			},
		}},
	})
	if len(contents) != 1 {
		t.Fatalf("contents = %#v", contents)
	}
	parts, _ := contents[0]["parts"].([]map[string]any)
	if len(parts) != 1 || parts[0]["text"] != "first\nsecond" {
		t.Fatalf("Gemini parts = %#v", parts)
	}
}

func TestGeminiRequestSeparatesSystemInstructionFromConversation(t *testing.T) {
	_, body, err := adaptRequestBody(Upstream{
		Provider: "gemini",
		Type:     "gemini",
		Model:    "gemini-2.5-pro",
	}, "chat", []byte(`{
		"messages":[
			{"role":"system","content":"Follow policy."},
			{"role":"developer","content":"Return concise text."},
			{"role":"user","content":"ping"}
		]
	}`), false)
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatal(err)
	}
	instruction, _ := payload["systemInstruction"].(map[string]any)
	parts, _ := instruction["parts"].([]any)
	if len(parts) != 1 {
		t.Fatalf("systemInstruction = %#v", instruction)
	}
	part, _ := parts[0].(map[string]any)
	if part["text"] != "Follow policy.\n\nReturn concise text." {
		t.Fatalf("system instruction text = %#v", part["text"])
	}
	contents, _ := payload["contents"].([]any)
	if len(contents) != 1 {
		t.Fatalf("Gemini contents retained system messages: %#v", contents)
	}
}

func TestCopyHeadersDropsCredentialAndServerHeaders(t *testing.T) {
	source := http.Header{
		"Content-Type":          {"text/event-stream"},
		"Retry-After":           {"3"},
		"X-Request-Id":          {"req_1"},
		"X-Ratelimit-Remaining": {"5"},
		"Set-Cookie":            {"session=secret"},
		"Www-Authenticate":      {`Bearer realm="upstream"`},
		"Server":                {"upstream-internal"},
	}
	target := make(http.Header)
	copyHeaders(target, source)
	if target.Get("Content-Type") != "text/event-stream" ||
		target.Get("Retry-After") != "3" ||
		target.Get("X-Request-Id") != "req_1" ||
		target.Get("X-Ratelimit-Remaining") != "5" {
		t.Fatalf("safe headers were dropped: %v", target)
	}
	for _, name := range []string{"Set-Cookie", "Www-Authenticate", "Server"} {
		if target.Get(name) != "" {
			t.Fatalf("copyHeaders() forwarded %s", name)
		}
	}
}

func TestUpstreamClientReliesOnPerRequestTimeout(t *testing.T) {
	client := NewUpstreamClient()
	if client.httpClient.Timeout != 0 {
		t.Fatalf("HTTP client timeout = %s, want per-request context only", client.httpClient.Timeout)
	}
	upstream := Upstream{ProviderConfig: map[string]any{"timeoutMs": 120000}}
	if got := upstreamTimeout(upstream); got != 120*time.Second {
		t.Fatalf("upstreamTimeout() = %s", got)
	}
	if got := upstreamTimeout(withManagedAuthTimeout(Upstream{})); got != managedAuthTimeout {
		t.Fatalf("managed OAuth timeout = %s", got)
	}
}

func TestStreamGeminiResponseMapsChunksAndUsageToOpenAIChatSSE(t *testing.T) {
	response := &http.Response{
		StatusCode: http.StatusOK,
		Header: http.Header{
			"Content-Type": {"text/event-stream"},
			"Set-Cookie":   {"session=secret"},
		},
		Body: io.NopCloser(strings.NewReader(
			"data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"hel\"}]}}]}\n\n" +
				"data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"lo\"}]},\"finishReason\":\"STOP\"}],\"usageMetadata\":{\"promptTokenCount\":2,\"candidatesTokenCount\":1,\"totalTokenCount\":3}}\n\n",
		)),
	}
	recorder := httptest.NewRecorder()
	result, err := streamGeminiResponse(response, "chat", "gemini-2.5-pro", UpstreamUsage{}, recorder)
	if err != nil {
		t.Fatalf("streamGeminiResponse() error = %v", err)
	}
	body := recorder.Body.String()
	if !strings.Contains(body, `"content":"hel"`) ||
		!strings.Contains(body, `"content":"lo"`) ||
		!strings.Contains(body, `"finish_reason":"stop"`) ||
		!strings.Contains(body, "data: [DONE]") {
		t.Fatalf("mapped stream = %s", body)
	}
	if result.Usage.TotalTokens != 3 || !result.Wrote {
		t.Fatalf("result = %#v", result)
	}
	if recorder.Header().Get("Set-Cookie") != "" {
		t.Fatal("Gemini stream forwarded Set-Cookie")
	}
}

func TestStreamGeminiResponseEmitsCompleteResponsesEventLifecycle(t *testing.T) {
	response := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": {"text/event-stream"}},
		Body: io.NopCloser(strings.NewReader(
			"data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"hello\"}]},\"finishReason\":\"STOP\"}],\"usageMetadata\":{\"promptTokenCount\":2,\"candidatesTokenCount\":1,\"totalTokenCount\":3}}\n\n",
		)),
	}
	recorder := httptest.NewRecorder()
	result, err := streamGeminiResponse(response, "responses", "gemini-2.5-pro", UpstreamUsage{}, recorder)
	if err != nil {
		t.Fatal(err)
	}
	body := recorder.Body.String()
	for _, event := range []string{
		"response.created",
		"response.output_item.added",
		"response.content_part.added",
		"response.output_text.delta",
		"response.output_text.done",
		"response.content_part.done",
		"response.output_item.done",
		"response.completed",
	} {
		if !strings.Contains(body, "event: "+event) {
			t.Fatalf("Responses stream omitted %s: %s", event, body)
		}
	}
	if !strings.Contains(body, `"input_tokens":2`) ||
		!strings.Contains(body, `"output_tokens":1`) ||
		result.Usage.TotalTokens != 3 {
		t.Fatalf("Responses stream usage mismatch: body=%s result=%#v", body, result)
	}
}

func TestStreamGeminiResponseTreatsSSEErrorAsFailure(t *testing.T) {
	response := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": {"text/event-stream"}},
		Body: io.NopCloser(strings.NewReader(
			"data: {\"error\":{\"code\":429,\"message\":\"provider detail\"}}\n\n",
		)),
	}
	recorder := httptest.NewRecorder()
	result, err := streamGeminiResponse(response, "chat", "gemini-2.5-pro", UpstreamUsage{}, recorder)
	if !errors.Is(err, ErrUpstreamUnavailable) || !result.Wrote ||
		result.ErrorType != "upstream_stream_interrupted" {
		t.Fatalf("streamGeminiResponse() result=%#v err=%v", result, err)
	}
	if strings.Contains(recorder.Body.String(), "provider detail") {
		t.Fatal("Gemini stream exposed provider error detail")
	}
}

func TestStreamWithAuthRunsGeminiOAuthThroughMappedSSEPath(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1beta/models/gemini-2.5-pro:streamGenerateContent" ||
			r.URL.Query().Get("alt") != "sse" {
			t.Fatalf("Gemini upstream target = %s", r.URL.String())
		}
		if r.Header.Get("Authorization") != "Bearer access" ||
			r.Header.Get("X-Goog-User-Project") != "project-1" ||
			r.Header.Get("X-Goog-Api-Key") != "" {
			t.Fatalf("Gemini auth headers = %v", r.Header)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w,
			"data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"ok\"}]},\"finishReason\":\"STOP\"}],\"usageMetadata\":{\"totalTokenCount\":2}}\n\n",
		)
	}))
	t.Cleanup(server.Close)
	client := NewUpstreamClient()
	client.httpClient = server.Client()
	recorder := httptest.NewRecorder()
	result, err := client.StreamWithAuth(context.Background(), Upstream{
		Provider: "gemini",
		Type:     "gemini",
		Endpoint: "https://user-controlled.example.test/v1beta",
		Model:    "gemini-2.5-pro",
	}, connections.AuthMaterial{
		Mode:     connections.AuthModeOAuthBearer,
		Endpoint: server.URL + "/v1beta",
		Headers: http.Header{
			"Authorization":       {"Bearer access"},
			"X-Goog-User-Project": {"project-1"},
		},
	}, "chat", []byte(`{"messages":[{"role":"user","content":"ping"}]}`), UpstreamUsage{}, recorder)
	if err != nil {
		t.Fatalf("StreamWithAuth() error = %v", err)
	}
	if !strings.Contains(recorder.Body.String(), `"content":"ok"`) ||
		!strings.Contains(recorder.Body.String(), "data: [DONE]") ||
		result.Usage.TotalTokens != 2 {
		t.Fatalf("StreamWithAuth() body=%s result=%#v", recorder.Body.String(), result)
	}
}

func TestStreamWithAuthRunsDeepSeekWebSessionThroughPinnedBridge(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Fatalf("DeepSeek bridge target = %s", r.URL.String())
		}
		if r.Header.Get("Authorization") != "Bearer deepseek-web-token" {
			t.Fatalf("DeepSeek bridge auth headers = %v", r.Header)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w,
			"data: {\"id\":\"chat_1\",\"choices\":[{\"delta\":{\"content\":\"ok\"}}]}\n\n"+
				"data: [DONE]\n\n",
		)
	}))
	t.Cleanup(server.Close)

	recorder := httptest.NewRecorder()
	result, err := NewUpstreamClient().StreamWithAuth(context.Background(), Upstream{
		Provider: "deepseek",
		Type:     "openai",
		Endpoint: "https://user-controlled.example.test",
		Model:    "deepseek-chat",
		ProviderConfig: map[string]any{
			// Existing DeepSeek web connections inherited this official-API
			// routing mode before the bridge-specific override was added.
			"pathMode": "direct",
		},
	}, connections.AuthMaterial{
		Mode:     connections.AuthModeDeepSeekWeb,
		Endpoint: server.URL,
		Headers:  http.Header{"Authorization": {"Bearer deepseek-web-token"}},
	}, "chat", []byte(`{"messages":[{"role":"user","content":"ping"}]}`), UpstreamUsage{}, recorder)
	if err != nil {
		t.Fatalf("StreamWithAuth() error = %v", err)
	}
	if result.StatusCode != http.StatusOK || !result.Wrote ||
		!strings.Contains(recorder.Body.String(), `"content":"ok"`) ||
		!strings.Contains(recorder.Body.String(), "data: [DONE]") {
		t.Fatalf("StreamWithAuth() body=%s result=%#v", recorder.Body.String(), result)
	}
}

func TestCodexChatRequestMapsToolResultsAndChoice(t *testing.T) {
	_, body, err := adaptRequestBody(Upstream{
		Provider: "openai",
		Type:     "openai",
		Model:    "gpt-5.1-codex",
		ProviderConfig: map[string]any{
			"authMethod": "codex_oauth",
		},
	}, "chat", []byte(`{
		"messages":[
			{"role":"assistant","content":null,"tool_calls":[{"id":"call_1","type":"function","function":{"name":"lookup","arguments":"{\"q\":\"x\"}"}}]},
			{"role":"tool","tool_call_id":"call_1","content":"result"}
		],
		"tool_choice":{"type":"function","function":{"name":"lookup"}},
		"parallel_tool_calls":false
	}`), false)
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatal(err)
	}
	input, _ := payload["input"].([]any)
	if len(input) != 2 {
		t.Fatalf("Codex input = %#v", input)
	}
	call, _ := input[0].(map[string]any)
	output, _ := input[1].(map[string]any)
	if call["type"] != "function_call" || call["call_id"] != "call_1" ||
		output["type"] != "function_call_output" || output["output"] != "result" {
		t.Fatalf("Codex tool input = %#v", input)
	}
	choice, _ := payload["tool_choice"].(map[string]any)
	if choice["type"] != "function" || choice["name"] != "lookup" || payload["parallel_tool_calls"] != false {
		t.Fatalf("Codex tool controls = %#v", payload)
	}
}

func TestCodexCompletedResponseMapsFunctionCallsToChat(t *testing.T) {
	sse := []byte("event: response.completed\n" +
		"data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_1\",\"object\":\"response\",\"status\":\"completed\",\"output\":[{\"type\":\"function_call\",\"call_id\":\"call_1\",\"name\":\"lookup\",\"arguments\":\"{\\\"q\\\":\\\"x\\\"}\"}],\"usage\":{\"input_tokens\":2,\"output_tokens\":3,\"total_tokens\":5}}}\n\n")
	body, usage, err := mapCodexEventResponse(Upstream{Model: "gpt-5.1-codex"}, "chat", sse)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), `"finish_reason":"tool_calls"`) ||
		!strings.Contains(string(body), `"id":"call_1"`) ||
		!strings.Contains(string(body), `"name":"lookup"`) {
		t.Fatalf("mapped response = %s", body)
	}
	if usage.TotalTokens != 5 {
		t.Fatalf("usage = %#v", usage)
	}
}

func TestCodexAuthMaterialForwardsPairedClientIdentityHeaders(t *testing.T) {
	request, err := NewUpstreamClient().newRequestWithAuth(context.Background(), Upstream{
		Provider: "openai",
		Type:     "openai",
		Endpoint: "https://user-controlled.example.test",
		Model:    "gpt-5.1-codex",
	}, connections.AuthMaterial{
		Mode:     connections.AuthModeCodexOAuth,
		Endpoint: "https://chatgpt.com/backend-api/codex",
		Headers: http.Header{
			"Authorization":      {"Bearer access"},
			"ChatGPT-Account-Id": {"account-1"},
			"OpenAI-Beta":        {"responses=experimental"},
			"Originator":         {"codex_cli_rs"},
			"User-Agent":         {"codex_cli_rs/0.144.1 (Ubuntu 22.4.0; x86_64) xterm-256color"},
			"Version":            {"0.144.1"},
		},
	}, http.MethodPost, "/responses", []byte(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	if request.URL.String() != "https://chatgpt.com/backend-api/codex/responses" ||
		request.Header.Get("Version") != "0.144.1" ||
		request.Header.Get("Originator") != "codex_cli_rs" ||
		!strings.HasPrefix(request.UserAgent(), "codex_cli_rs/0.144.1") {
		t.Fatalf("Codex request = %s headers=%v", request.URL, request.Header)
	}
}

func TestCodexChatStreamKeepsFunctionItemAndCallOnOneToolIndex(t *testing.T) {
	response := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": {"text/event-stream"}},
		Body: io.NopCloser(strings.NewReader(
			"event: response.output_item.added\n" +
				"data: {\"type\":\"response.output_item.added\",\"item\":{\"id\":\"fc_item_1\",\"type\":\"function_call\",\"call_id\":\"call_1\",\"name\":\"lookup\",\"arguments\":\"\"}}\n\n" +
				"event: response.function_call_arguments.delta\n" +
				"data: {\"type\":\"response.function_call_arguments.delta\",\"item_id\":\"fc_item_1\",\"delta\":\"{\\\"q\\\":\"}\n\n" +
				"event: response.function_call_arguments.delta\n" +
				"data: {\"type\":\"response.function_call_arguments.delta\",\"item_id\":\"fc_item_1\",\"delta\":\"\\\"x\\\"}\"}\n\n" +
				"event: response.completed\n" +
				"data: {\"type\":\"response.completed\",\"response\":{\"object\":\"response\",\"output\":[{\"id\":\"fc_item_1\",\"type\":\"function_call\",\"call_id\":\"call_1\",\"name\":\"lookup\",\"arguments\":\"{\\\"q\\\":\\\"x\\\"}\"}],\"usage\":{\"input_tokens\":2,\"output_tokens\":3,\"total_tokens\":5}}}\n\n",
		)),
	}
	recorder := httptest.NewRecorder()
	result, err := streamCodexChatResponse(response, "gpt-5.1-codex", UpstreamUsage{}, recorder)
	if err != nil {
		t.Fatalf("streamCodexChatResponse() error = %v", err)
	}
	body := recorder.Body.String()
	if strings.Contains(body, `"index":1`) ||
		!strings.Contains(body, `"id":"call_1"`) ||
		!strings.Contains(body, `"arguments":"{\"q\":"`) ||
		!strings.Contains(body, `"finish_reason":"tool_calls"`) {
		t.Fatalf("mapped tool stream = %s", body)
	}
	if result.Usage.TotalTokens != 5 {
		t.Fatalf("result = %#v", result)
	}
}
