package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"tokhub/internal/connections"
	gatewaycache "tokhub/internal/gateway"
	"tokhub/internal/store"
)

func TestNormalizeConnectionModelsRejectsURLShapedModelIDs(t *testing.T) {
	_, err := normalizeConnectionModels([]string{"https://evil.example/model"})
	if err == nil {
		t.Fatal("normalizeConnectionModels() accepted a URL-shaped model id")
	}
}

func TestOAuthConnectionDisconnectRequiresPasswordStepUp(t *testing.T) {
	for _, method := range []string{"oauth", "codex_oauth", "deepseek_web_token"} {
		if !requiresAIConnectionDisconnectStepUp(method) {
			t.Fatalf("%s disconnect did not require password step-up", method)
		}
	}
	for _, method := range []string{"", "api_key", "api_key_guided"} {
		if requiresAIConnectionDisconnectStepUp(method) {
			t.Fatalf("%s disconnect unexpectedly required password step-up", method)
		}
	}
}

func TestStoredOAuthValidationRejectsMalformedBundleBeforeUpstream(t *testing.T) {
	server := &Server{authRegistry: connections.NewAuthRegistry(connections.AdapterConfig{})}
	_, err := server.validateStoredOAuthCredentialSet(
		context.Background(),
		store.AIConnection{Provider: "gemini", AuthMethod: "oauth"},
		connections.ResolvedProvider{},
		[]string{"gemini-test"},
		`{"accessToken":"plaintext-key-shape"}`,
	)
	if err == nil {
		t.Fatal("malformed OAuth bundle reached the upstream validation path")
	}
}

func TestOfficialValidationPayloadUsesProviderGenerationContract(t *testing.T) {
	responsesRaw := officialValidationPayload("responses", "gpt-test")
	var responses map[string]any
	if err := json.Unmarshal(responsesRaw, &responses); err != nil {
		t.Fatal(err)
	}
	if responses["input"] == nil || responses["max_output_tokens"] == nil || responses["messages"] != nil {
		t.Fatalf("unexpected responses payload: %#v", responses)
	}

	chatRaw := officialValidationPayload("chat", "chat-test")
	var chat map[string]any
	if err := json.Unmarshal(chatRaw, &chat); err != nil {
		t.Fatal(err)
	}
	if chat["messages"] == nil || chat["max_tokens"] == nil || chat["input"] != nil {
		t.Fatalf("unexpected chat payload: %#v", chat)
	}
}

func TestValidateOfficialCredentialSetDiscoversOnceAndGeneratesEveryModel(t *testing.T) {
	var modelLists atomic.Int32
	var generations atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/models":
			modelLists.Add(1)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": []map[string]any{{"id": "model-a"}, {"id": "model-b"}},
			})
		case "/v1/chat/completions":
			generations.Add(1)
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body["model"] == "model-b" {
				http.Error(w, `{"error":{"message":"denied"}}`, http.StatusForbidden)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"choices": []map[string]any{{"message": map[string]any{"content": "OK"}}},
				"usage":   map[string]any{"total_tokens": 2},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer upstream.Close()

	server := &Server{upstreamClient: gatewaycache.NewUpstreamClient()}
	result := server.validateOfficialCredentialSet(context.Background(), connections.ResolvedProvider{
		Manifest: connections.ProviderManifest{
			Code: "test", Name: "Test", Type: "openai-compatible",
			ValidationMode: "models_then_generation", GenerationKind: "chat",
		},
		Endpoint: upstream.URL + "/v1",
	}, []string{"model-a", "model-b"}, "test-key")

	if result.OK || result.Model != "model-b" || result.Stage != "generation" {
		t.Fatalf("unexpected multi-model validation result: %#v", result)
	}
	if len(result.Models) != 2 || !result.Models[0].OK || result.Models[1].OK {
		t.Fatalf("model-level validation results were not preserved: %#v", result.Models)
	}
	if modelLists.Load() != 1 || generations.Load() != 2 {
		t.Fatalf("validation calls: model lists=%d generations=%d", modelLists.Load(), generations.Load())
	}
}

func TestDeepSeekWebCredentialValidationUsesPinnedBridgeAndBearerToken(t *testing.T) {
	token := strings.Repeat("deepseek-token-", 4)
	var requests atomic.Int32
	bridge := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.URL.Path != "/v1/chat/completions" {
			t.Errorf("bridge path = %q", r.URL.Path)
			http.NotFound(w, r)
			return
		}
		if r.Header.Get("Authorization") != "Bearer "+token {
			t.Errorf("bridge Authorization header was not the imported DeepSeek token")
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode request body: %v", err)
			http.Error(w, "invalid", http.StatusBadRequest)
			return
		}
		if body["model"] != "deepseek-v4-flash" {
			t.Errorf("bridge model = %#v", body["model"])
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{"message": map[string]any{"content": "OK"}}},
			"usage":   map[string]any{"prompt_tokens": 2, "completion_tokens": 1, "total_tokens": 3},
		})
	}))
	defer bridge.Close()

	adapter := connections.NewDeepSeekWebAdapter(connections.AdapterConfig{
		WebAuthEnabled:          true,
		DeepSeekWebExperimental: true,
		DeepSeekWebBridgeURL:    bridge.URL,
		DeepSeekWebBridgeAck:    connections.DeepSeekWebExperimentalAcknowledgement,
	})
	bundle, _, err := adapter.Exchange(context.Background(), connections.AuthorizationTransaction{}, token)
	if err != nil {
		t.Fatalf("Exchange() error = %v", err)
	}
	material, err := adapter.ResolveAuthMaterial(context.Background(), bundle)
	if err != nil {
		t.Fatalf("ResolveAuthMaterial() error = %v", err)
	}
	server := &Server{upstreamClient: gatewaycache.NewUpstreamClient()}
	result := server.validateAuthorizedCredentialSet(context.Background(), connections.ResolvedProvider{
		Manifest: connections.ProviderManifest{
			Code: "deepseek", Name: "DeepSeek", ProductLine: "DeepSeek Web",
			Type: "openai-compatible", Protocol: "openai",
			ValidationMode: "generation", GenerationKind: "chat",
		},
		Endpoint: bridge.URL,
		ProviderConfig: map[string]any{
			"authMethod": "deepseek_web_token", "experimental": true,
			"pathMode": "direct",
		},
	}, []string{"deepseek-v4-flash"}, material)
	if !result.OK || requests.Load() != 1 {
		t.Fatalf("DeepSeek web validation = %#v, requests = %d", result, requests.Load())
	}
}

func TestDecodeAIConnectionJSONRejectsOversizedBody(t *testing.T) {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/me/ai-connections", strings.NewReader(
		`{"apiKey":"`+strings.Repeat("x", aiConnectionRequestBodyLimit)+`"}`,
	))
	var input createAIConnectionRequest
	if err := decodeAIConnectionJSON(recorder, request, &input); err == nil {
		t.Fatal("decodeAIConnectionJSON() accepted an oversized request body")
	}
}

func TestReadyzRejectsMissingCredentialVaultBeforeDatabase(t *testing.T) {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	(&Server{}).readyz(recorder, request)
	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("readyz status = %d, want %d", recorder.Code, http.StatusServiceUnavailable)
	}
}

func TestGatewaySupportsOnlyConfiguredModel(t *testing.T) {
	gateway := store.Gateway{Upstreams: []store.GatewayUpstream{
		{Model: "model-a", Enabled: true},
		{Model: "model-b", Enabled: false},
	}}
	if !gatewaySupportsModel(gateway, "model-a") {
		t.Fatal("configured model was rejected")
	}
	if gatewaySupportsModel(gateway, "model-b") || gatewaySupportsModel(gateway, "model-c") {
		t.Fatal("disabled or unconfigured model was accepted")
	}
}

func TestExperimentalGatewayLimitsRemainPinnedAfterMutableGatewayEdits(t *testing.T) {
	tests := []struct {
		name        string
		authMethods []string
		want        experimentalGatewayPolicy
		wantOK      bool
	}{
		{
			name:        "deepseek web session",
			authMethods: []string{"deepseek_web_token"},
			want:        experimentalGatewayPolicy{QPS: 1, Concurrency: 1},
			wantOK:      true,
		},
		{
			name:        "codex oauth",
			authMethods: []string{"codex_oauth"},
			want:        experimentalGatewayPolicy{QPS: 1, Concurrency: 2},
			wantOK:      true,
		},
		{
			name:        "deepseek limit wins for mixed experimental upstreams",
			authMethods: []string{"codex_oauth", "deepseek_web_token"},
			want:        experimentalGatewayPolicy{QPS: 1, Concurrency: 1},
			wantOK:      true,
		},
		{
			name:        "official api",
			authMethods: []string{"api_key"},
			want:        experimentalGatewayPolicy{},
			wantOK:      false,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			gateway := store.Gateway{QPSLimit: 999}
			for _, authMethod := range test.authMethods {
				gateway.Upstreams = append(gateway.Upstreams, store.GatewayUpstream{
					ProviderConfig: map[string]any{"authMethod": authMethod},
				})
			}
			got, ok := experimentalGatewayLimits(gateway)
			if ok != test.wantOK || got != test.want {
				t.Fatalf("experimentalGatewayLimits() = (%#v, %t), want (%#v, %t)", got, ok, test.want, test.wantOK)
			}
		})
	}
}

func TestExperimentalGatewayRateLimitIsSharedAcrossGatewayKeys(t *testing.T) {
	gateway := store.Gateway{
		ID:       "gw_deepseek",
		QPSLimit: 999,
		Upstreams: []store.GatewayUpstream{{
			ProviderConfig: map[string]any{"authMethod": "deepseek_web_token"},
		}},
	}
	firstKey, firstQPS := gatewayRateLimitPolicy(store.AuthenticatedGatewayKey{
		Key:     store.GatewayKey{ID: "key_one", QPSLimit: 500},
		Gateway: gateway,
	})
	secondKey, secondQPS := gatewayRateLimitPolicy(store.AuthenticatedGatewayKey{
		Key:     store.GatewayKey{ID: "key_two", QPSLimit: 700},
		Gateway: gateway,
	})
	if firstKey != "experimental:gw_deepseek" || secondKey != firstKey || firstQPS != 1 || secondQPS != 1 {
		t.Fatalf("experimental rate limit policies = (%q, %d) and (%q, %d)", firstKey, firstQPS, secondKey, secondQPS)
	}
}

func TestManagedAuthorizationRejectDetectionIncludesForbidden(t *testing.T) {
	material := &connections.AuthMaterial{Mode: connections.AuthModeDeepSeekWeb}
	for _, status := range []int{http.StatusUnauthorized, http.StatusForbidden} {
		if !managedAuthorizationRejected(gatewaycache.UpstreamResult{StatusCode: status}, material) {
			t.Fatalf("managed authorization status %d was not detected", status)
		}
	}
	if managedAuthorizationRejected(gatewaycache.UpstreamResult{StatusCode: http.StatusTooManyRequests}, material) ||
		managedAuthorizationRejected(gatewaycache.UpstreamResult{StatusCode: http.StatusUnauthorized}, nil) {
		t.Fatal("non-authentication failure was classified as a managed authorization rejection")
	}
}

func TestDeepSeekValidationFailureClassificationKeepsProtocolErrorsSeparateFromExpiredLogin(t *testing.T) {
	tests := []struct {
		errorType string
		want      error
		wantCode  string
	}{
		{errorType: "upstream_auth_error", want: connections.ErrCredentialReauth, wantCode: "reauth_required"},
		{errorType: "upstream_rejected", want: connections.ErrCredentialRejected, wantCode: "provider_rejected"},
		{errorType: "upstream_rate_limited", want: connections.ErrCredentialTemporary, wantCode: "provider_temporary"},
		{errorType: "upstream_unavailable", want: connections.ErrCredentialTemporary, wantCode: "provider_temporary"},
	}
	for _, test := range tests {
		got := deepSeekValidationCredentialError(test.errorType)
		if !errors.Is(got, test.want) {
			t.Fatalf("deepSeekValidationCredentialError(%q) = %v, want %v", test.errorType, got, test.want)
		}
		if gotCode := authorizationErrorCode(got); gotCode != test.wantCode {
			t.Fatalf("authorizationErrorCode(%q) = %q, want %q", test.errorType, gotCode, test.wantCode)
		}
	}
}

func TestNormalizeQuickRelayRequestRejectsUnsafeLimits(t *testing.T) {
	for _, request := range []quickRelayRequest{
		{Policy: "random", QPSLimit: 20, QuotaMonth: 1000},
		{Policy: "latency", QPSLimit: 1001, QuotaMonth: 1000},
		{Policy: "latency", QPSLimit: 20, QuotaMonth: 1_000_000_001},
	} {
		if err := normalizeQuickRelayRequest(&request); err == nil {
			t.Fatalf("normalizeQuickRelayRequest() accepted %#v", request)
		}
	}

	request := quickRelayRequest{}
	if err := normalizeQuickRelayRequest(&request); err != nil {
		t.Fatal(err)
	}
	if request.Policy != "latency" || request.QPSLimit != 20 || request.QuotaMonth != 100000 {
		t.Fatalf("quick relay defaults = %#v", request)
	}
}
