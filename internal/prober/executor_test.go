package prober

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestExecutorMockL1L2(t *testing.T) {
	executor := NewExecutor()
	ctx := context.Background()

	healthy := ProbeTarget{ID: "ch_ok", Endpoint: "https://ok.example/v1", Status: "healthy"}
	l1 := Summarize(executor.L1(ctx, healthy))
	l2 := Summarize(executor.L2(ctx, healthy, ""))
	if l1.Status != "ok" || l2.Status != "ok" {
		t.Fatalf("healthy target l1=%q l2=%q, want ok/ok", l1.Status, l2.Status)
	}

	down := ProbeTarget{ID: "ch_down", Endpoint: "https://down.example/v1", Status: "connectivity_down"}
	l1 = Summarize(executor.L1(ctx, down))
	if l1.Status != "down" || l1.ErrorType != "dns_failed" {
		t.Fatalf("down target l1=%q error=%q, want down/dns_failed", l1.Status, l1.ErrorType)
	}

	auth := ProbeTarget{ID: "ch_auth", Endpoint: "https://auth.example/v1", Status: "auth_error"}
	l2 = Summarize(executor.L2(ctx, auth, ""))
	if l2.Status != "auth_error" {
		t.Fatalf("auth target l2=%q, want auth_error", l2.Status)
	}
}

func TestExecutorL2UsesOpenAICompatibleAdapter(t *testing.T) {
	var authHeader string
	var path string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		authHeader = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"gpt-5.5"}]}`))
	}))
	defer server.Close()

	executor := NewExecutorWithMockEndpoints(false)
	target := ProbeTarget{ID: "ch_real", Endpoint: server.URL, Status: "unknown", Provider: "AIGoCode", Type: "openai-compatible", Model: "gpt-5.5"}
	summary := Summarize(executor.L2(context.Background(), target, "sk-admin-secret"))

	if path != "/v1/models" {
		t.Fatalf("models path = %q, want /v1/models", path)
	}
	if authHeader != "Bearer sk-admin-secret" {
		t.Fatalf("Authorization header = %q, want bearer key", authHeader)
	}
	if summary.Status != "ok" {
		t.Fatalf("summary = %+v, want ok", summary)
	}
}

func TestExecutorL2DoesNotInjectConfiguredModelFallback(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	defer server.Close()

	executor := NewExecutorWithMockEndpoints(false)
	target := ProbeTarget{ID: "ch_real", Endpoint: server.URL, Status: "unknown", Provider: "AIGoCode", Type: "openai-compatible", Model: "gpt-5.5"}
	summary := Summarize(executor.L2(context.Background(), target, "sk-admin-secret"))

	if summary.Status != "down" || summary.ErrorType != "model_not_found" {
		t.Fatalf("summary = %+v, want down/model_not_found", summary)
	}
}

func TestExecutorL2CapturesSanitizedUpstreamErrorSummary(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"code":"ip_not_allowed","type":"auth_error","message":"source IP 47.251.246.63 is denied; token=sk-upstream-secret; details={\"token\":\"jwt-secret-value\"}; Authorization: Bearer bearer-secret; Authorization: Basic basic-secret; session eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiJ0b2todWItdXNlciJ9.signaturevalue; Google key AIzaSyD1234567890abcdefghijklmnopqrstuvwxyz"}}`))
	}))
	defer server.Close()

	executor := NewExecutorWithMockEndpoints(false)
	target := ProbeTarget{ID: "ch_real", Endpoint: server.URL, Type: "openai-compatible", Model: "gpt-5.5"}
	result := executor.L2(context.Background(), target, "sk-request-secret")[0]

	if result.Status != "auth_error" || result.HTTPStatus != http.StatusUnauthorized {
		t.Fatalf("result=%+v, want auth_error/401", result)
	}
	summary, _ := result.Metadata["upstream_error_summary"].(string)
	if summary != "code=ip_not_allowed · type=auth_error" {
		t.Fatalf("summary=%q, want structured diagnostic fields only", summary)
	}
	for _, secret := range []string{"sk-upstream-secret", "jwt-secret-value", "bearer-secret", "basic-secret", "sk-request-secret", "eyJhbGciOiJIUzI1NiJ9", "AIzaSyD1234567890abcdefghijklmnopqrstuvwxyz"} {
		if strings.Contains(summary, secret) {
			t.Fatalf("summary leaked secret %q: %q", secret, summary)
		}
	}
}

func TestUpstreamErrorSummaryRedactsQuotedAuthorizationValues(t *testing.T) {
	summary := upstreamErrorSummary([]byte(`{"error":{"message":"request rejected; Authorization: \"Basic cXVvdGVkLXNlY3JldA==\""}}`))
	if strings.Contains(summary, "cXVvdGVkLXNlY3JldA==") {
		t.Fatalf("summary leaked quoted authorization secret: %q", summary)
	}
	if summary != "" {
		t.Fatalf("summary=%q, want free-form message omitted", summary)
	}
}

func TestUpstreamErrorSummaryDropsFreeFormMessages(t *testing.T) {
	summary := upstreamErrorSummary([]byte(`{"error":{"code":"request_denied","type":"auth_error","message":"credential=unrecognised-secret"}}`))
	if strings.Contains(summary, "unrecognised-secret") || strings.Contains(summary, "credential=") {
		t.Fatalf("summary leaked free-form upstream message: %q", summary)
	}
	if summary != "code=request_denied · type=auth_error" {
		t.Fatalf("summary=%q, want structured diagnostic fields only", summary)
	}
}

func TestExecutorL2L3UseAnthropicAdapter(t *testing.T) {
	var modelsAPIKey string
	var generationAPIKey string
	var generationPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/models":
			modelsAPIKey = r.Header.Get("X-API-Key")
			_, _ = w.Write([]byte(`{"data":[{"id":"claude-sonnet-4-6"}]}`))
		case "/v1/messages":
			generationPath = r.URL.Path
			generationAPIKey = r.Header.Get("X-API-Key")
			_, _ = w.Write([]byte(`{"content":[{"type":"text","text":"K"}],"usage":{"input_tokens":4,"output_tokens":1}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	executor := NewExecutorWithMockEndpoints(false)
	target := ProbeTarget{ID: "ch_anthropic", Endpoint: server.URL, Status: "unknown", Provider: "AIGoCode Claude", Type: "anthropic", Model: "claude-sonnet-4-6"}
	l2 := Summarize(executor.L2(context.Background(), target, "sk-ant-secret"))
	l3 := Summarize(executor.L3(context.Background(), target, "sk-ant-secret"))

	if modelsAPIKey != "sk-ant-secret" || generationAPIKey != "sk-ant-secret" {
		t.Fatalf("anthropic api keys = models %q generation %q, want X-API-Key", modelsAPIKey, generationAPIKey)
	}
	if generationPath != "/v1/messages" {
		t.Fatalf("generation path = %q, want /v1/messages", generationPath)
	}
	if l2.Status != "ok" {
		t.Fatalf("l2 = %+v, want ok", l2)
	}
	if l3.Status != "ok" {
		t.Fatalf("l3 = %+v, want ok", l3)
	}
}

func TestExecutorL2CanSkipModelsProbeByConfig(t *testing.T) {
	called := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		http.Error(w, "should not be called", http.StatusInternalServerError)
	}))
	defer server.Close()

	executor := NewExecutorWithMockEndpoints(false)
	target := ProbeTarget{
		ID:             "ch_skip_models",
		Endpoint:       server.URL,
		Type:           "anthropic",
		Model:          "claude-sonnet-4-6",
		ProviderConfig: map[string]any{"modelsProbeMode": "skip"},
	}
	result := executor.L2(context.Background(), target, "sk-secret")[0]
	if called {
		t.Fatalf("models endpoint was called despite skip mode")
	}
	if result.Status != "na" || result.ErrorType != "models_probe_skipped" {
		t.Fatalf("result=%+v, want na/models_probe_skipped", result)
	}
	summary := Summarize([]StepResult{result})
	if summary.Status != "na" || summary.ErrorType != "models_probe_skipped" {
		t.Fatalf("summary=%+v, want na/models_probe_skipped", summary)
	}
}

func TestExecutorL2SkipsClaudeCodeProfileModelsProbe(t *testing.T) {
	executor := NewExecutorWithMockEndpoints(false)
	target := ProbeTarget{
		ID:       "ch_claudecode",
		Name:     "AICodeMirror ClaudeCode CN",
		Endpoint: "https://api.claudecode.example/api/claudecode",
		Type:     "anthropic",
		Model:    "claude-sonnet-4-6",
	}
	result := executor.L2(context.Background(), target, "sk-secret")[0]
	if result.Status != "na" || result.ErrorType != "models_probe_skipped" {
		t.Fatalf("result=%+v, want na/models_probe_skipped", result)
	}
}

func TestExecutorL3UsesEstimatedUsageWhenProviderOmitsUsage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"content":[{"type":"text","text":"K"}]}`))
	}))
	defer server.Close()

	executor := NewExecutorWithMockEndpoints(false)
	target := ProbeTarget{ID: "ch_anthropic", Endpoint: server.URL, Status: "unknown", Provider: "AIGoCode Claude", Type: "anthropic", Model: "claude-sonnet-4-6"}
	results := executor.L3(context.Background(), target, "sk-ant-secret")
	if len(results) != 1 {
		t.Fatalf("results len = %d, want 1", len(results))
	}
	if results[0].Status != "ok" {
		t.Fatalf("status=%q error=%q, want ok", results[0].Status, results[0].ErrorType)
	}
	if results[0].Metadata["tokens_used"] != 5 {
		t.Fatalf("tokens_used=%v, want estimated 5", results[0].Metadata["tokens_used"])
	}
	if results[0].Metadata["usage_estimated"] != true {
		t.Fatalf("usage_estimated=%v, want true", results[0].Metadata["usage_estimated"])
	}
}

func TestExecutorL3UsesCheapCanaryRequest(t *testing.T) {
	var maxTokens float64
	var hasInput bool
	var prompt string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		maxTokens, _ = payload["max_tokens"].(float64)
		_, hasInput = payload["input"]
		messages, _ := payload["messages"].([]any)
		if len(messages) > 0 {
			first, _ := messages[0].(map[string]any)
			prompt, _ = first["content"].(string)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"K"}}]}`))
	}))
	defer server.Close()

	executor := NewExecutorWithMockEndpoints(false)
	target := ProbeTarget{ID: "ch_real", Endpoint: server.URL, Type: "openai-compatible", Model: "gpt-5-mini", ProviderConfig: map[string]any{"maxTokens": 16}}
	results := executor.L3(context.Background(), target, "sk-secret")
	if results[0].Status != "ok" {
		t.Fatalf("status=%q error=%q, want ok", results[0].Status, results[0].ErrorType)
	}
	if maxTokens != 2 {
		t.Fatalf("max_tokens=%v, want cheap default 2", maxTokens)
	}
	if hasInput {
		t.Fatalf("input field should not be sent for chat L3 probe")
	}
	if prompt != "Reply exactly: K" {
		t.Fatalf("prompt=%q, want canary prompt", prompt)
	}
}

func TestExecutorL3AllowsProbeSpecificMaxTokens(t *testing.T) {
	var maxTokens float64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		maxTokens, _ = payload["max_tokens"].(float64)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"K"}}]}`))
	}))
	defer server.Close()

	executor := NewExecutorWithMockEndpoints(false)
	target := ProbeTarget{ID: "ch_real", Endpoint: server.URL, Type: "openai-compatible", Model: "gpt-5-mini", ProviderConfig: map[string]any{"l3ProbeMaxTokens": 7}}
	results := executor.L3(context.Background(), target, "sk-secret")
	if results[0].Status != "ok" {
		t.Fatalf("status=%q error=%q, want ok", results[0].Status, results[0].ErrorType)
	}
	if maxTokens != 7 {
		t.Fatalf("max_tokens=%v, want configured 7", maxTokens)
	}
}

func TestExecutorL3ClampsProbeSpecificMaxTokens(t *testing.T) {
	if got := l3MaxTokens(ProbeTarget{ProviderConfig: map[string]any{"l3ProbeMaxTokens": 99}}); got != 8 {
		t.Fatalf("high l3ProbeMaxTokens=%d, want clamp 8", got)
	}
	if got := l3MaxTokens(ProbeTarget{ProviderConfig: map[string]any{"l3ProbeMaxTokens": 0}}); got != 1 {
		t.Fatalf("low l3ProbeMaxTokens=%d, want clamp 1", got)
	}
}

func TestExecutorL3DefaultsGPT55ProbeMaxTokensToEight(t *testing.T) {
	if got := l3MaxTokens(ProbeTarget{Model: "gpt-5.5"}); got != 8 {
		t.Fatalf("gpt-5.5 default l3 max tokens=%d, want 8", got)
	}
	if got := l3MaxTokens(ProbeTarget{Model: "gpt-5-mini"}); got != l3ProbeDefaultTokens {
		t.Fatalf("gpt-5-mini default l3 max tokens=%d, want %d", got, l3ProbeDefaultTokens)
	}
}

func TestProbeUpstreamErrorTypeDetectsModelUnavailableResponse(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{name: "english no channel", body: `{"error":"No available channel for model gpt-5.5 under group"}`},
		{name: "english missing model", body: `{"error":{"message":"model gpt-5.5 does not exist"}}`},
		{name: "chinese no channel", body: `{"error":"分组 cc 下模型 gpt-5.5 无可用渠道"}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := probeUpstreamErrorType(context.DeadlineExceeded, "upstream_unavailable", http.StatusServiceUnavailable, []byte(tt.body))
			if got != "model_unavailable" {
				t.Fatalf("error type=%q, want model_unavailable", got)
			}
		})
	}
}

func TestExecutorL3RequiresExactCanaryContent(t *testing.T) {
	tests := []struct {
		name      string
		body      string
		want      string
		wantError string
	}{
		{name: "exact canary", body: `{"choices":[{"message":{"content":" K \n"}}]}`, want: "ok"},
		{name: "empty content", body: `{"choices":[{"message":{"content":"   "}}]}`, want: "down", wantError: "empty_content"},
		{name: "reasoning only", body: `{"choices":[{"message":{"reasoning_content":"We need answer K"}}]}`, want: "down", wantError: "reasoning_only"},
		{name: "mismatched content", body: `{"choices":[{"message":{"content":"tokhub-ok"}}]}`, want: "down", wantError: "content_mismatch"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(tt.body))
			}))
			defer server.Close()

			executor := NewExecutorWithMockEndpoints(false)
			target := ProbeTarget{ID: "ch_real", Endpoint: server.URL, Type: "openai-compatible", Model: "gpt-5-mini"}
			result := executor.L3(context.Background(), target, "sk-secret")[0]
			if result.Status != tt.want || result.ErrorType != tt.wantError {
				t.Fatalf("status=%q error=%q, want %q/%q", result.Status, result.ErrorType, tt.want, tt.wantError)
			}
		})
	}
}

func TestExecutorL3SupportsNonEmptyContentPolicy(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"content":[{"type":"text","text":"好"}],"usage":{"input_tokens":10,"output_tokens":1}}`))
	}))
	defer server.Close()

	executor := NewExecutorWithMockEndpoints(false)
	target := ProbeTarget{
		ID:             "ch_aigocode",
		Endpoint:       server.URL,
		Type:           "anthropic",
		Model:          "claude-sonnet-4-6",
		ProviderConfig: map[string]any{"l3ContentPolicy": "non_empty"},
	}
	result := executor.L3(context.Background(), target, "sk-secret")[0]
	if result.Status != "ok" || result.ErrorType != "" {
		t.Fatalf("status=%q error=%q, want ok", result.Status, result.ErrorType)
	}
	if result.Metadata["content_valid"] != true {
		t.Fatalf("content_valid=%v, want true", result.Metadata["content_valid"])
	}
}

func TestExecutorL3CapturesSanitizedUpstreamErrorSummary(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error":{"code":"region_denied","message":"Alibaba US egress is not allowed; api_key=sk-response-secret"}}`))
	}))
	defer server.Close()

	executor := NewExecutorWithMockEndpoints(false)
	target := ProbeTarget{ID: "ch_real", Endpoint: server.URL, Type: "openai-compatible", Model: "gpt-5.5"}
	result := executor.L3(context.Background(), target, "sk-request-secret")[0]

	if result.Status != "auth_error" || result.HTTPStatus != http.StatusForbidden {
		t.Fatalf("result=%+v, want auth_error/403", result)
	}
	summary, _ := result.Metadata["upstream_error_summary"].(string)
	if summary != "code=region_denied" || strings.Contains(summary, "sk-response-secret") {
		t.Fatalf("summary=%q, want structured region diagnostic", summary)
	}
}

func TestExecutorL3SupportsL2OnlyProbeMode(t *testing.T) {
	executor := NewExecutorWithMockEndpoints(false)
	target := ProbeTarget{
		ID:             "ch_packy",
		Endpoint:       "https://www.packyapi.com",
		Type:           "anthropic",
		Model:          "claude-sonnet-4-6",
		ProviderConfig: map[string]any{"l3ProbeMode": "l2_only"},
	}
	result := executor.L3(context.Background(), target, "sk-secret")[0]
	if result.Status != "na" || result.ErrorType != "l3_probe_skipped" {
		t.Fatalf("status=%q error=%q, want na/l3_probe_skipped", result.Status, result.ErrorType)
	}
	if result.Metadata["probe_mode"] != "l2_only" {
		t.Fatalf("probe_mode=%v, want l2_only", result.Metadata["probe_mode"])
	}
}

func TestL3WarnThresholdUsesProviderDefaultsAndConfigOverride(t *testing.T) {
	anthropic := ProbeTarget{Type: "anthropic"}
	if got := l3WarnThresholdMs(anthropic); got != 6000 {
		t.Fatalf("anthropic threshold=%d, want 6000", got)
	}
	openai := ProbeTarget{Type: "openai-compatible"}
	if got := l3WarnThresholdMs(openai); got != 4000 {
		t.Fatalf("openai threshold=%d, want 4000", got)
	}
	overridden := ProbeTarget{Type: "anthropic", ProviderConfig: map[string]any{"l3WarnMs": "7500"}}
	if got := l3WarnThresholdMs(overridden); got != 7500 {
		t.Fatalf("override threshold=%d, want 7500", got)
	}
}

func TestExecutorCanDisableExampleEndpointMocks(t *testing.T) {
	executor := NewExecutorWithMockEndpoints(false)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	healthy := ProbeTarget{ID: "ch_ok", Endpoint: "https://ok.example/v1", Status: "healthy"}
	l1 := Summarize(executor.L1(ctx, healthy))
	if l1.Status == "ok" {
		t.Fatalf("disabled mock endpoint returned ok; want real network failure for .example")
	}
}

func TestExecutorMockL3(t *testing.T) {
	executor := NewExecutor()
	ctx := context.Background()

	healthy := ProbeTarget{ID: "pch_ok", Endpoint: "https://ok.example/v1", Status: "healthy", Model: "gpt-4o-mini"}
	results := executor.L3(ctx, healthy, "sk-private")
	l3 := Summarize(results)
	if l3.Status != "ok" {
		t.Fatalf("healthy l3 status=%q error=%q, want ok", l3.Status, l3.ErrorType)
	}
	if len(results) != 1 || results[0].LatencyMs == 0 || results[0].Metadata["tokens_used"] == 0 {
		t.Fatalf("healthy l3 metrics missing: %+v", results)
	}
	if results[0].Metadata["content_valid"] != true {
		t.Fatalf("content_valid = %v, want true", results[0].Metadata["content_valid"])
	}

	auth := ProbeTarget{ID: "pch_auth", Endpoint: "https://auth.example/v1", Status: "auth_error", Model: "gpt-4o-mini"}
	l3 = Summarize(executor.L3(ctx, auth, "bad-key"))
	if l3.Status != "auth_error" || l3.ErrorType != "auth_error" {
		t.Fatalf("auth l3 status=%q error=%q, want auth_error", l3.Status, l3.ErrorType)
	}
}
