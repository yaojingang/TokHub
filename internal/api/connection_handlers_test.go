package api

import (
	"context"
	"encoding/json"
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
	for _, method := range []string{"oauth", "codex_oauth"} {
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
