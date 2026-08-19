package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5/middleware"

	"tokhub/internal/clientconnector"
)

func TestSanitizeAIClientHeartbeatUsesBoundedLabels(t *testing.T) {
	heartbeat := clientconnector.Heartbeat{
		ConnectorVersion: " 2.0.0-rc.2 ",
		Capabilities:     []string{"chatgpt", "chatgpt", "grok"},
		Identity: map[string]clientconnector.IdentityStatus{
			"openai": {LoggedIn: true, AccountMask: "a***@example.com", IdentityAssurance: "account", CheckedAt: time.Now()},
		},
	}
	if err := sanitizeAIClientHeartbeat(&heartbeat); err != nil {
		t.Fatal(err)
	}
	if heartbeat.ConnectorVersion != "2.0.0-rc.2" || len(heartbeat.Capabilities) != 2 {
		t.Fatalf("unexpected sanitized heartbeat: %#v", heartbeat)
	}
	heartbeat.Capabilities = []string{"browser-cookie"}
	if err := sanitizeAIClientHeartbeat(&heartbeat); err == nil {
		t.Fatal("expected unknown capability to fail closed")
	}
}

func TestOfficialClientTransportTrustsOnlyConfiguredProxy(t *testing.T) {
	server := &Server{cfg: Config{Env: "production", AIOfficialClientTrustedProxyCIDRs: []string{"10.0.0.0/8"}}}

	request := httptest.NewRequest("POST", "http://tokhub.example/api/ai-client-connectors/heartbeat", nil)
	request.Header.Set("X-Forwarded-Proto", "https")
	request.RemoteAddr = "203.0.113.8:41234"
	recorder := httptest.NewRecorder()
	if server.requireSecureClientTransport(recorder, request) {
		t.Fatal("untrusted forwarded HTTPS header must be rejected")
	}
	if recorder.Code != 426 {
		t.Fatalf("unexpected response code: %d", recorder.Code)
	}

	request = httptest.NewRequest("POST", "http://tokhub.example/api/ai-client-connectors/heartbeat", nil)
	request.Header.Set("X-Forwarded-Proto", "https")
	request.RemoteAddr = "10.12.0.4:41234"
	if !server.requireSecureClientTransport(httptest.NewRecorder(), request) {
		t.Fatal("configured TLS proxy must be accepted")
	}
}

func TestOfficialClientTransportUsesOriginalPeerBeforeRealIP(t *testing.T) {
	server := &Server{cfg: Config{Env: "production", AIOfficialClientTrustedProxyCIDRs: []string{"10.0.0.0/8"}}}
	handler := captureOriginalPeerAddr(middleware.RealIP(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if server.requireSecureClientTransport(w, r) {
			w.WriteHeader(http.StatusNoContent)
		}
	})))

	request := httptest.NewRequest("POST", "http://tokhub.example/api/ai-client-connectors/heartbeat", nil)
	request.RemoteAddr = "203.0.113.8:41234"
	request.Header.Set("X-Forwarded-For", "10.12.0.4")
	request.Header.Set("X-Forwarded-Proto", "https")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusUpgradeRequired {
		t.Fatalf("spoofed trusted proxy address bypassed HTTPS enforcement: %d", recorder.Code)
	}

	request = httptest.NewRequest("POST", "http://tokhub.example/api/ai-client-connectors/heartbeat", nil)
	request.RemoteAddr = "10.12.0.4:41234"
	request.Header.Set("X-Forwarded-For", "203.0.113.8")
	request.Header.Set("X-Forwarded-Proto", "https")
	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusNoContent {
		t.Fatalf("configured TLS proxy was rejected after RealIP processing: %d", recorder.Code)
	}
}

func TestOfficialClientDevelopmentHTTPRequiresLoopbackPeer(t *testing.T) {
	server := &Server{cfg: Config{Env: "development"}}
	request := httptest.NewRequest("POST", "http://localhost:8080/api/ai-client-connectors/heartbeat", nil)
	request.RemoteAddr = "203.0.113.8:41234"
	if server.requireSecureClientTransport(httptest.NewRecorder(), request) {
		t.Fatal("loopback Host from a remote peer must be rejected")
	}
	request.RemoteAddr = "127.0.0.1:41234"
	if !server.requireSecureClientTransport(httptest.NewRecorder(), request) {
		t.Fatal("loopback development request must be accepted")
	}
}

func TestSanitizeAIClientResultMasksAccountAndBoundsModels(t *testing.T) {
	result, err := sanitizeAIClientResult(clientconnector.TaskResult{
		OK: true, AccountID: "alice@example.com", IdentityAssurance: "account",
		Models: []string{"gpt-5", "gpt-5"},
	})
	if err != nil {
		t.Fatal(err)
	}
	result.AccountMask = maskAIClientAccount(result.AccountID)
	if result.AccountMask != "a***@example.com" || len(result.Models) != 1 {
		t.Fatalf("unexpected sanitized result: %#v", result)
	}
	if _, err := sanitizeAIClientResult(clientconnector.TaskResult{OK: true, Models: []string{strings.Repeat("x", 161)}}); err == nil {
		t.Fatal("expected oversized model label to fail")
	}
	if _, err := sanitizeAIClientResult(clientconnector.TaskResult{OK: true, SessionRef: "plaintext-thread"}); err == nil {
		t.Fatal("expected plaintext session reference to fail")
	}
}
