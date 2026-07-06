package api

import (
	"net/http/httptest"
	"strings"
	"testing"

	secretcrypto "tokhub/internal/crypto"
)

func TestPrivateChannelInputRejectsEndpointCredentialsQueryAndFragment(t *testing.T) {
	box, err := secretcrypto.NewSecretBox("test-private-channel-secret-key-32-bytes")
	if err != nil {
		t.Fatal(err)
	}
	server := &Server{secretBox: box}
	req := httptest.NewRequest("POST", "/api/me/private-channels", strings.NewReader(`{
		"name":"Private",
		"provider":"OpenAI",
		"type":"openai-compatible",
		"model":"gpt-test",
		"endpoint":"https://user:pass@example.com/v1?token=bad#frag",
		"apiKey":"sk-test"
	}`))

	if _, err := server.privateChannelInput(req, true); err == nil || !strings.Contains(err.Error(), "without credentials") {
		t.Fatalf("privateChannelInput error = %v, want endpoint credential rejection", err)
	}
}

func TestPrivateChannelInputRejectsInvalidEndpointScheme(t *testing.T) {
	box, err := secretcrypto.NewSecretBox("test-private-channel-secret-key-32-bytes")
	if err != nil {
		t.Fatal(err)
	}
	server := &Server{secretBox: box}
	req := httptest.NewRequest("POST", "/api/me/private-channels", strings.NewReader(`{
		"name":"Private",
		"provider":"OpenAI",
		"type":"openai-compatible",
		"model":"gpt-test",
		"endpoint":"httpx://example.com/v1",
		"apiKey":"sk-test"
	}`))

	if _, err := server.privateChannelInput(req, true); err == nil || !strings.Contains(err.Error(), "http or https") {
		t.Fatalf("privateChannelInput error = %v, want endpoint scheme rejection", err)
	}
}

func TestPrivateChannelInputNormalizesEndpoint(t *testing.T) {
	box, err := secretcrypto.NewSecretBox("test-private-channel-secret-key-32-bytes")
	if err != nil {
		t.Fatal(err)
	}
	server := &Server{secretBox: box}
	req := httptest.NewRequest("POST", "/api/me/private-channels", strings.NewReader(`{
		"name":"Private",
		"provider":"OpenAI",
		"type":"openai-compatible",
		"model":"gpt-test",
		"endpoint":"https://example.com/v1/",
		"apiKey":"sk-test"
	}`))

	input, err := server.privateChannelInput(req, true)
	if err != nil {
		t.Fatal(err)
	}
	if input.Endpoint != "https://example.com/v1" {
		t.Fatalf("Endpoint = %q, want normalized base URL", input.Endpoint)
	}
}

func TestSanitizeProviderConfigAllowsL3WarnThreshold(t *testing.T) {
	config, err := sanitizeProviderConfig(map[string]any{
		"l3WarnMs":          float64(7500),
		"l3WarnThresholdMs": float64(8000),
	})
	if err != nil {
		t.Fatal(err)
	}
	if config["l3WarnMs"] != 7500 || config["l3WarnThresholdMs"] != 8000 {
		t.Fatalf("config = %#v, want l3 thresholds preserved", config)
	}
}

func TestSanitizeProviderConfigAllowsProbeOverrides(t *testing.T) {
	config, err := sanitizeProviderConfig(map[string]any{
		"clientProfile":     "claude_code",
		"clientVersion":     "2.1.114",
		"modelsProbeMode":   "l3-only",
		"l3ProbeMode":       "skip",
		"l3ProbeMaxTokens":  float64(7),
		"l3ContentPolicy":   "non-empty",
		"l3ExpectedContent": "K",
	})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]any{
		"clientProfile":     "claude-code",
		"clientVersion":     "2.1.114",
		"modelsProbeMode":   "skip",
		"l3ProbeMode":       "l2_only",
		"l3ProbeMaxTokens":  7,
		"l3ContentPolicy":   "non_empty",
		"l3ExpectedContent": "K",
	}
	for key, value := range want {
		if config[key] != value {
			t.Fatalf("config[%s] = %#v, want %#v in %#v", key, config[key], value, config)
		}
	}
}
