package officialconnector

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"tokhub/internal/clientconnector"
)

func TestValidateServerURLRequiresHTTPSOutsideLoopback(t *testing.T) {
	for _, value := range []string{"http://example.com", "http://192.0.2.10:8080", "ftp://localhost"} {
		if _, err := validateServerURL(value); err == nil {
			t.Fatalf("unsafe server URL was accepted: %s", value)
		}
	}
	for _, value := range []string{"https://tokhub.example", "http://127.0.0.1:8080", "http://[::1]:8080", "http://localhost:8080"} {
		if _, err := validateServerURL(value); err != nil {
			t.Fatalf("valid server URL %s was rejected: %v", value, err)
		}
	}
	normalized, err := validateServerURL("https://tokhub.example/path?query=1#fragment")
	if err != nil || normalized != "https://tokhub.example" {
		t.Fatalf("server URL was not normalized: %q err=%v", normalized, err)
	}
}

func TestSessionReferenceIsBoundToPairedDevice(t *testing.T) {
	first := newTestConnectorConfig(t)
	second := newTestConnectorConfig(t)
	sealed, err := SealSessionReference(first, "thread-sensitive-123")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(sealed, "thread-sensitive-123") {
		t.Fatal("sealed reference contains plaintext")
	}
	opened, err := OpenSessionReference(first, sealed)
	if err != nil || opened != "thread-sensitive-123" {
		t.Fatalf("open session reference = %q, %v", opened, err)
	}
	if _, err := OpenSessionReference(second, sealed); err == nil {
		t.Fatal("another paired device opened the session reference")
	}
	if _, err := OpenSessionReference(first, "thread-sensitive-123"); err == nil {
		t.Fatal("plaintext session reference was accepted")
	}
}

func newTestConnectorConfig(t *testing.T) Config {
	t.Helper()
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return Config{
		ServerURL: "https://tokhub.example", DeviceToken: "device-token",
		PublicKey:  base64.RawURLEncoding.EncodeToString(publicKey),
		PrivateKey: base64.RawURLEncoding.EncodeToString(privateKey),
	}
}

func TestConfigSaveUsesOwnerOnlyPermissions(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	cfg := Config{
		ServerURL: "https://tokhub.example", DeviceToken: "device-token",
		PublicKey:  base64.RawURLEncoding.EncodeToString(publicKey),
		PrivateKey: base64.RawURLEncoding.EncodeToString(privateKey),
	}
	path := filepath.Join(t.TempDir(), "nested", "config.json")
	if err := SaveConfig(path, cfg); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("config permissions=%#o, want 0600", info.Mode().Perm())
	}
	loaded, err := LoadConfig(path)
	if err != nil || loaded != cfg {
		t.Fatalf("saved config did not round-trip: cfg=%#v err=%v", loaded, err)
	}
	bad := cfg
	bad.PublicKey = base64.RawURLEncoding.EncodeToString(make([]byte, ed25519.PublicKeySize))
	if err := bad.Validate(); err == nil {
		t.Fatal("mismatched key pair was accepted")
	}
}

func TestPairAndHeartbeatUseSignedDeviceBinding(t *testing.T) {
	const pairingCode = "pairing-code-with-more-than-thirty-two-characters"
	const deviceToken = "device-token-with-more-than-forty-random-characters"
	var mu sync.Mutex
	nonces := map[string]struct{}{}
	var pairedPublicKey ed25519.PublicKey
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(io.LimitReader(r.Body, 128<<10))
		if err != nil {
			t.Errorf("read request body: %v", err)
			http.Error(w, "read failed", http.StatusBadRequest)
			return
		}
		switch r.URL.Path {
		case "/api/ai-client-connectors/pair":
			var request map[string]string
			if err := json.Unmarshal(body, &request); err != nil {
				t.Errorf("decode pair request: %v", err)
			}
			if request["pairingCode"] != pairingCode {
				t.Errorf("pairing code=%q", request["pairingCode"])
			}
			decoded, err := base64.RawURLEncoding.DecodeString(request["publicKey"])
			if err != nil || len(decoded) != ed25519.PublicKeySize {
				t.Errorf("invalid paired public key")
			}
			pairedPublicKey = append(ed25519.PublicKey(nil), decoded...)
			_ = json.NewEncoder(w).Encode(map[string]string{"deviceToken": deviceToken})
		case "/api/ai-client-connectors/heartbeat":
			if r.Header.Get("Authorization") != "Bearer "+deviceToken {
				t.Errorf("missing device token")
			}
			timestamp, err := strconv.ParseInt(r.Header.Get("X-TokHub-Timestamp"), 10, 64)
			if err != nil || time.Since(time.Unix(timestamp, 0)) > time.Minute {
				t.Errorf("invalid timestamp")
			}
			nonce := r.Header.Get("X-TokHub-Nonce")
			mu.Lock()
			if _, exists := nonces[nonce]; exists {
				t.Errorf("nonce was reused")
			}
			nonces[nonce] = struct{}{}
			mu.Unlock()
			if err := clientconnector.VerifyRequest(
				base64.RawURLEncoding.EncodeToString(pairedPublicKey), r.Header.Get("X-TokHub-Signature"),
				r.Method, r.URL.Path, body, timestamp, nonce, time.Now(),
			); err != nil {
				t.Errorf("device signature verification failed: %v", err)
			}
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	cfg, err := Pair(context.Background(), server.URL, pairingCode)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DeviceToken != deviceToken || cfg.ServerURL != server.URL || len(pairedPublicKey) != ed25519.PublicKeySize {
		t.Fatalf("pairing result is incomplete: %#v", cfg)
	}
	client := Client{Config: cfg, HTTP: server.Client()}
	for index := 0; index < 2; index++ {
		if err := client.Heartbeat(context.Background(), clientconnector.Heartbeat{
			ConnectorVersion: "2.0.0-rc.2", Capabilities: []string{"chatgpt", "grok"},
		}); err != nil {
			t.Fatal(err)
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if len(nonces) != 2 {
		t.Fatalf("heartbeat nonce count=%d, want 2", len(nonces))
	}
}
