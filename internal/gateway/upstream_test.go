package gateway

import (
	"context"
	"encoding/json"
	"errors"
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
