package connections

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestAuthRegistryPublishesOnlyEnabledProviderMethods(t *testing.T) {
	registry := NewAuthRegistry(AdapterConfig{
		WebAuthEnabled:           true,
		GeminiOAuthEnabled:       true,
		DeepSeekGuidedEnabled:    true,
		ChatGPTCodexExperimental: true,
		ExperimentalBridgeAck:    ExperimentalBridgeAcknowledgement,
		GoogleClientID:           "google-client",
		GoogleClientSecret:       "google-secret",
		PublicURL:                "https://tokhub.example.test",
	})
	if _, ok := registry.Adapter("gemini", "oauth"); !ok {
		t.Fatal("Gemini OAuth adapter is missing")
	}
	if _, ok := registry.Adapter("deepseek", "api_key_guided"); !ok {
		t.Fatal("DeepSeek guided adapter is missing")
	}
	if _, ok := registry.Adapter("openai", "codex_oauth"); !ok {
		t.Fatal("ChatGPT Codex adapter is missing")
	}

	disabled := NewAuthRegistry(AdapterConfig{
		WebAuthEnabled:           true,
		ChatGPTCodexExperimental: true,
		ExperimentalBridgeAck:    "wrong-value",
	})
	if _, ok := disabled.Adapter("openai", "codex_oauth"); ok {
		t.Fatal("ChatGPT Codex adapter ignored the deployment acknowledgement")
	}
}

func TestGeminiOAuthExchangesAndRefreshesOfficialBearerMaterial(t *testing.T) {
	var refreshCalls int
	var revokeCalls int
	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Fatal(err)
		}
		if token := r.Form.Get("token"); token != "" {
			revokeCalls++
			if token != "refresh-1" {
				t.Fatalf("unexpected revoke token: %q", token)
			}
			w.WriteHeader(http.StatusOK)
			return
		}
		switch r.Form.Get("grant_type") {
		case "authorization_code":
			if r.Form.Get("code") != "authorization-code" || r.Form.Get("code_verifier") != "verifier" {
				t.Fatalf("unexpected exchange form: %v", r.Form)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"access_token":  "access-1",
				"refresh_token": "refresh-1",
				"id_token": fakeJWT(map[string]any{
					"iss":   "https://accounts.google.com",
					"aud":   "google-client",
					"sub":   "google-subject",
					"email": "person@example.test",
					"nonce": "nonce",
					"exp":   time.Now().Add(time.Hour).Unix(),
				}),
				"token_type": "Bearer",
				"expires_in": 3600,
				"scope":      "openid https://www.googleapis.com/auth/cloud-platform",
			})
		case "refresh_token":
			refreshCalls++
			_ = json.NewEncoder(w).Encode(map[string]any{
				"access_token": "access-2",
				"token_type":   "Bearer",
				"expires_in":   3600,
			})
		default:
			http.Error(w, "unexpected grant", http.StatusBadRequest)
		}
	}))
	defer tokenServer.Close()

	adapter := NewGeminiOAuthAdapter(AdapterConfig{
		GoogleClientID:        "google-client",
		GoogleClientSecret:    "google-secret",
		GoogleProjectID:       "project-default",
		GoogleTokenURL:        tokenServer.URL,
		GoogleAuthorizeURL:    "https://accounts.google.com/o/oauth2/v2/auth",
		GoogleRevokeURL:       tokenServer.URL,
		PublicURL:             "https://tokhub.example.test",
		HTTPClient:            tokenServer.Client(),
		OIDCSignatureVerifier: allowTestOIDCSignatureVerifier{},
		GeminiOAuthEnabled:    true,
		WebAuthEnabled:        true,
	})
	start, err := adapter.Start(context.Background(), AuthorizationTransaction{
		State: "state", Nonce: "nonce", CodeVerifier: "verifier",
		RedirectURI: "https://tokhub.example.test/api/me/ai-authorizations/google/callback",
		ProjectID:   "project-user",
	}, "challenge")
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	authorizeURL, err := url.Parse(start.AuthorizationURL)
	if err != nil {
		t.Fatal(err)
	}
	if authorizeURL.Query().Get("access_type") != "offline" || authorizeURL.Query().Get("code_challenge_method") != "S256" {
		t.Fatalf("authorization URL query = %v", authorizeURL.Query())
	}

	bundle, profile, err := adapter.Exchange(context.Background(), AuthorizationTransaction{
		Nonce: "nonce", CodeVerifier: "verifier", ProjectID: "project-user",
		RedirectURI: "https://tokhub.example.test/api/me/ai-authorizations/google/callback",
	}, "authorization-code")
	if err != nil {
		t.Fatalf("Exchange() error = %v", err)
	}
	if profile.Subject != "google-subject" || bundle.ProjectID != "project-user" || bundle.RefreshToken != "refresh-1" {
		t.Fatalf("exchange output bundle=%#v profile=%#v", bundle, profile)
	}
	material, err := adapter.ResolveAuthMaterial(context.Background(), bundle)
	if err != nil {
		t.Fatalf("ResolveAuthMaterial() error = %v", err)
	}
	if material.Headers.Get("Authorization") != "Bearer access-1" || material.Headers.Get("X-Goog-User-Project") != "project-user" {
		t.Fatalf("material headers = %v", material.Headers)
	}

	refreshed, err := adapter.Refresh(context.Background(), bundle)
	if err != nil {
		t.Fatalf("Refresh() error = %v", err)
	}
	if refreshCalls != 1 || refreshed.AccessToken != "access-2" || refreshed.RefreshToken != "refresh-1" {
		t.Fatalf("refreshed bundle = %#v calls=%d", refreshed, refreshCalls)
	}
	if err := adapter.Revoke(context.Background(), refreshed); err != nil {
		t.Fatalf("Revoke() error = %v", err)
	}
	if revokeCalls != 1 {
		t.Fatalf("revoke calls = %d, want 1", revokeCalls)
	}
}

func TestChatGPTCodexAdapterParsesFixedCallbackAndPinsPrivateEndpoint(t *testing.T) {
	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Fatal(err)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token":  fakeJWT(map[string]any{"exp": time.Now().Add(time.Hour).Unix()}),
			"refresh_token": "refresh",
			"id_token": fakeJWT(map[string]any{
				"iss": "https://auth.openai.com",
				"aud": CodexOAuthClientID,
				"sub": "openai-subject",
				"exp": time.Now().Add(time.Hour).Unix(),
				"https://api.openai.com/auth": map[string]any{
					"chatgpt_account_id": "account-1",
				},
			}),
			"token_type": "Bearer",
			"expires_in": 3600,
		})
	}))
	defer tokenServer.Close()

	adapter := NewChatGPTCodexAdapter(AdapterConfig{
		WebAuthEnabled:           true,
		ChatGPTCodexExperimental: true,
		ExperimentalBridgeAck:    ExperimentalBridgeAcknowledgement,
		OpenAITokenURL:           tokenServer.URL,
		OpenAIAuthorizeURL:       "https://auth.openai.com/oauth/authorize",
		HTTPClient:               tokenServer.Client(),
		OIDCSignatureVerifier:    allowTestOIDCSignatureVerifier{},
	})
	bundle, profile, err := adapter.Exchange(context.Background(), AuthorizationTransaction{
		CodeVerifier: "verifier", RedirectURI: CodexOAuthRedirectURI,
	}, "authorization-code")
	if err != nil {
		t.Fatalf("Exchange() error = %v", err)
	}
	if profile.AccountID != "account-1" {
		t.Fatalf("profile = %#v", profile)
	}
	material, err := adapter.ResolveAuthMaterial(context.Background(), bundle)
	if err != nil {
		t.Fatalf("ResolveAuthMaterial() error = %v", err)
	}
	if material.Endpoint != "https://chatgpt.com/backend-api/codex" ||
		material.Headers.Get("ChatGPT-Account-Id") != "account-1" ||
		material.Headers.Get("Originator") != "codex_cli_rs" ||
		material.Headers.Get("Version") != CodexBridgeVersion ||
		!strings.Contains(material.Headers.Get("User-Agent"), "codex_cli_rs/"+CodexBridgeVersion) {
		t.Fatalf("Codex material = %#v", material)
	}
}

type allowTestOIDCSignatureVerifier struct{}

func (allowTestOIDCSignatureVerifier) Verify(context.Context, string, string) error {
	return nil
}

func fakeJWT(claims map[string]any) string {
	header, _ := json.Marshal(map[string]any{"alg": "none", "typ": "JWT"})
	payload, _ := json.Marshal(claims)
	return strings.Join([]string{
		base64.RawURLEncoding.EncodeToString(header),
		base64.RawURLEncoding.EncodeToString(payload),
		"",
	}, ".")
}
