package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"tokhub/internal/store"
)

func TestDecodeBrowserConnectorJSONRejectsTrailingDocument(t *testing.T) {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/ai-browser-connectors/pair",
		strings.NewReader(`{"pairingCode":"first"}{"pairingCode":"second"}`),
	)
	var input pairBrowserConnectorRequest
	if decodeBrowserConnectorJSON(recorder, request, &input) {
		t.Fatal("decodeBrowserConnectorJSON accepted multiple JSON documents")
	}
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusBadRequest)
	}
}

func TestDecodeBrowserConnectorJSONRejectsOversizedBody(t *testing.T) {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/ai-browser-connectors/pair",
		strings.NewReader(`{"pairingCode":"`+strings.Repeat("x", browserConnectorRequestBodyLimit)+`"}`),
	)
	var input pairBrowserConnectorRequest
	if decodeBrowserConnectorJSON(recorder, request, &input) {
		t.Fatal("decodeBrowserConnectorJSON accepted an oversized request")
	}
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusBadRequest)
	}
}

func TestBrowserConnectorRateLimitAllowsPollingAndCapsFlood(t *testing.T) {
	server := &Server{authLimiter: &rateLimiter{buckets: map[string]rateBucket{}}}
	accepted := 0
	handler := server.browserConnectorRateLimit(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		accepted++
		w.WriteHeader(http.StatusNoContent)
	}))
	for index := 0; index < 100; index++ {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodPost, "/api/ai-browser-connectors/tasks/claim", nil)
		request.RemoteAddr = "192.0.2.15:12345"
		handler.ServeHTTP(recorder, request)
		if recorder.Code != http.StatusNoContent {
			t.Fatalf("normal connector poll %d returned %d", index+1, recorder.Code)
		}
	}
	for index := 100; index < 600; index++ {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodPost, "/api/ai-browser-connectors/tasks/claim", nil)
		request.RemoteAddr = "192.0.2.15:12345"
		handler.ServeHTTP(recorder, request)
		if recorder.Code != http.StatusNoContent {
			t.Fatalf("connector request %d returned %d before flood limit", index+1, recorder.Code)
		}
	}
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/ai-browser-connectors/tasks/claim", nil)
	request.RemoteAddr = "192.0.2.15:12345"
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusTooManyRequests {
		t.Fatalf("request after flood limit returned %d, want %d", recorder.Code, http.StatusTooManyRequests)
	}
	if accepted != 600 {
		t.Fatalf("accepted requests = %d, want 600", accepted)
	}
}

func TestOpenCLIBrowserProviderSwitchesAndPolicies(t *testing.T) {
	server := &Server{cfg: Config{
		AIOpenCLIBrowserEnabled:      true,
		AIOpenCLIChatGPTEnabled:      true,
		AIOpenCLIGeminiEnabled:       false,
		AIOpenCLIDeepSeekEnabled:     true,
		AIOpenCLIChatGPTMinInterval:  10 * time.Second,
		AIOpenCLIDeepSeekMinInterval: 15 * time.Second,
		AIOpenCLIChatGPTHourlyLimit:  30,
		AIOpenCLIDeepSeekHourlyLimit: 20,
		AIOpenCLIChatGPTDailyLimit:   120,
		AIOpenCLIDeepSeekDailyLimit:  80,
	}}
	if !server.openCLIBrowserProviderEnabled("openai") ||
		server.openCLIBrowserProviderEnabled("gemini") ||
		!server.openCLIBrowserProviderEnabled("deepseek") {
		t.Fatal("provider switches were not enforced")
	}
	deepseek := server.openCLIBrowserRiskPolicy("deepseek")
	if deepseek.MinimumInterval != 15*time.Second || deepseek.HourlyLimit != 20 || deepseek.DailyLimit != 80 {
		t.Fatalf("DeepSeek risk policy = %#v", deepseek)
	}
}

func TestBrowserRiskRejectionReturnsStableHTTPContract(t *testing.T) {
	retryAt := time.Now().Add(time.Minute)
	tests := []struct {
		reason string
		status int
		code   string
	}{
		{"minimum_interval", http.StatusTooManyRequests, "browser_minimum_interval"},
		{"hourly_limit", http.StatusTooManyRequests, "browser_hourly_limit"},
		{"security_locked", http.StatusLocked, "browser_security_locked"},
		{"reauth_required", http.StatusConflict, "browser_reauthorization_required"},
		{"adapter_blocked", http.StatusServiceUnavailable, "browser_adapter_blocked"},
		{"paused", http.StatusLocked, "browser_account_paused"},
	}
	for _, test := range tests {
		status, code, message := browserRiskRejection(store.AIBrowserRiskDecision{
			Reason: test.reason, RetryAt: &retryAt,
		})
		if status != test.status || code != test.code || message == "" {
			t.Fatalf("%s rejection = (%d,%q,%q)", test.reason, status, code, message)
		}
	}
}
