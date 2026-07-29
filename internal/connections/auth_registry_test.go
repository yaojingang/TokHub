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

func TestAuthRegistryPublishesAvailableAndUnavailableProviderMethods(t *testing.T) {
	registry := NewAuthRegistry(AdapterConfig{
		WebAuthEnabled:           true,
		GeminiOAuthEnabled:       true,
		DeepSeekGuidedEnabled:    true,
		DeepSeekWebExperimental:  true,
		DeepSeekWebBridgeURL:     "http://deepseek-web-bridge:5001",
		DeepSeekWebBridgeAck:     DeepSeekWebExperimentalAcknowledgement,
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
	consumerLogin := authMethodByCode(registry.Methods("deepseek"), "deepseek_web_token")
	if consumerLogin == nil {
		t.Fatal("DeepSeek consumer login capability is missing from the catalog")
	}
	if !consumerLogin.Enabled {
		t.Fatalf("DeepSeek consumer login is disabled: %q", consumerLogin.UnavailableReason)
	}
	if _, ok := registry.Adapter("deepseek", "deepseek_web_token"); !ok {
		t.Fatal("DeepSeek consumer login adapter is missing")
	}

	disabled := NewAuthRegistry(AdapterConfig{
		WebAuthEnabled:           true,
		ChatGPTCodexExperimental: true,
		ExperimentalBridgeAck:    "wrong-value",
	})
	if _, ok := disabled.Adapter("openai", "codex_oauth"); ok {
		t.Fatal("ChatGPT Codex adapter ignored the deployment acknowledgement")
	}
	for _, test := range []struct {
		provider string
		method   string
		reason   string
	}{
		{provider: "openai", method: "codex_oauth", reason: "部署确认"},
		{provider: "gemini", method: "oauth", reason: "Google OAuth"},
		{provider: "deepseek", method: "api_key_guided", reason: "管理员"},
		{provider: "deepseek", method: "deepseek_web_token", reason: "管理员"},
	} {
		method := authMethodByCode(disabled.Methods(test.provider), test.method)
		if method == nil {
			t.Fatalf("%s method %s is missing from the capability catalog", test.provider, test.method)
		}
		if method.Enabled {
			t.Fatalf("%s method %s was unexpectedly enabled", test.provider, test.method)
		}
		if !strings.Contains(method.UnavailableReason, test.reason) {
			t.Fatalf("%s method %s reason %q does not contain %q", test.provider, test.method, method.UnavailableReason, test.reason)
		}
	}

	manual := &AuthRegistry{}
	manual.Register(NewDeepSeekGuidedAdapter())
	method := authMethodByCode(manual.Methods("deepseek"), "api_key_guided")
	if method == nil || !method.Enabled {
		t.Fatal("manually registered adapters must remain visible in the method catalog")
	}
}

func TestGeminiOAuthRequiresSafeCallbackBaseURL(t *testing.T) {
	base := AdapterConfig{
		WebAuthEnabled:     true,
		GeminiOAuthEnabled: true,
		GoogleClientID:     "google-client",
		GoogleClientSecret: "google-secret",
	}
	for _, publicURL := range []string{
		"http://tokhub.example.test",
		"https://user@tokhub.example.test",
		"https://tokhub.example.test/custom/path",
		"https://tokhub.example.test?next=callback",
		"https://tokhub.example.test#callback",
	} {
		cfg := base
		cfg.PublicURL = publicURL
		registry := NewAuthRegistry(cfg)
		if _, ok := registry.Adapter("gemini", "oauth"); ok {
			t.Fatalf("Gemini OAuth accepted unsafe public URL %q", publicURL)
		}
	}
	for _, publicURL := range []string{
		"https://tokhub.example.test",
		"https://tokhub.example.test/",
		"http://localhost:8125",
		"http://127.0.0.1:8125",
		"http://[::1]:8125",
	} {
		cfg := base
		cfg.PublicURL = publicURL
		registry := NewAuthRegistry(cfg)
		if _, ok := registry.Adapter("gemini", "oauth"); !ok {
			t.Fatalf("Gemini OAuth rejected safe public URL %q", publicURL)
		}
	}
}

func TestDeepSeekWebAdapterGuidesLoginAndPinsDirectTokenBridge(t *testing.T) {
	bridge := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/healthz" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	}))
	defer bridge.Close()

	adapter := NewDeepSeekWebAdapter(AdapterConfig{
		WebAuthEnabled:          true,
		DeepSeekWebExperimental: true,
		DeepSeekWebBridgeURL:    bridge.URL,
		DeepSeekWebBridgeAck:    DeepSeekWebExperimentalAcknowledgement,
		HTTPClient:              bridge.Client(),
	})
	start, err := adapter.Start(context.Background(), AuthorizationTransaction{}, "")
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if start.AuthorizationURL != DeepSeekChatURL || start.CompletionMode != "paste_token" {
		t.Fatalf("unexpected DeepSeek web authorization start: %#v", start)
	}
	token := fakeJWT(map[string]any{
		"sub": "deepseek-user", "user_id": "deepseek-account",
		"exp": time.Now().Add(time.Hour).Unix(),
	})
	bundle, profile, err := adapter.Exchange(context.Background(), AuthorizationTransaction{}, "Bearer "+token)
	if err != nil {
		t.Fatalf("Exchange() error = %v", err)
	}
	if bundle.ProviderSubject != "deepseek-user" || bundle.AccountID != "deepseek-account" ||
		profile.DisplayName != "DeepSeek 网页账号" {
		t.Fatalf("unexpected bundle/profile: %#v %#v", bundle, profile)
	}
	material, err := adapter.ResolveAuthMaterial(context.Background(), bundle)
	if err != nil {
		t.Fatalf("ResolveAuthMaterial() error = %v", err)
	}
	if material.Mode != AuthModeDeepSeekWeb || material.Endpoint != bridge.URL ||
		material.Headers.Get("Authorization") != "Bearer "+token {
		t.Fatalf("unexpected DeepSeek web material: %#v", material)
	}
	for _, header := range []string{"Cookie", "X-CSRF-Token", "X-Requested-With"} {
		if material.Headers.Get(header) != "" {
			t.Fatalf("DeepSeek web material unexpectedly contains %s", header)
		}
	}
}

func TestDeepSeekWebTokenInputRejectsCookieAndUnsafeBridge(t *testing.T) {
	if _, err := NormalizeDeepSeekWebToken("userToken=abc; cf_clearance=secret"); err == nil {
		t.Fatal("cookie-shaped input was accepted as a DeepSeek userToken")
	}
	expired := fakeJWT(map[string]any{
		"sub": "deepseek-user",
		"exp": time.Now().Add(-time.Minute).Unix(),
	})
	adapter := NewDeepSeekWebAdapter(AdapterConfig{
		WebAuthEnabled:          true,
		DeepSeekWebExperimental: true,
		DeepSeekWebBridgeURL:    "http://deepseek-web-bridge:5001",
		DeepSeekWebBridgeAck:    DeepSeekWebExperimentalAcknowledgement,
	})
	if _, _, err := adapter.Exchange(context.Background(), AuthorizationTransaction{}, expired); err == nil {
		t.Fatal("expired DeepSeek userToken was accepted")
	}
	if validDeepSeekWebBridgeURL("http://bridge.example.com:5001") {
		t.Fatal("public plaintext bridge URL was accepted")
	}
	if validDeepSeekWebBridgeURL("https://bridge.example.com/custom/path") {
		t.Fatal("bridge URL with a custom path was accepted")
	}
	if !validDeepSeekWebBridgeURL("https://bridge.example.com") {
		t.Fatal("HTTPS bridge URL was rejected")
	}
}

func TestDeepSeekGuidedAdapterUsesOfficialAPIKeyBearerMaterial(t *testing.T) {
	adapter := NewDeepSeekGuidedAdapter()
	start, err := adapter.Start(context.Background(), AuthorizationTransaction{}, "")
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if start.AuthorizationURL != DeepSeekAPIKeysURL || start.CompletionMode != "guided_api_key" {
		t.Fatalf("unexpected DeepSeek authorization start: %#v", start)
	}

	material, err := adapter.ResolveAuthMaterial(context.Background(), CredentialBundle{AccessToken: "deepseek-key"})
	if err != nil {
		t.Fatalf("ResolveAuthMaterial() error = %v", err)
	}
	if material.Mode != AuthModeAPIKey || material.Endpoint != "https://api.deepseek.com" {
		t.Fatalf("unexpected DeepSeek auth material: %#v", material)
	}
	if got := material.Headers.Get("Authorization"); got != "Bearer deepseek-key" {
		t.Fatalf("DeepSeek Authorization header = %q", got)
	}
	for _, header := range []string{"Cookie", "X-CSRF-Token", "X-Requested-With"} {
		if got := material.Headers.Get(header); got != "" {
			t.Fatalf("DeepSeek auth material unexpectedly contains %s", header)
		}
	}
}

func authMethodByCode(items []AuthMethodManifest, code string) *AuthMethodManifest {
	for index := range items {
		if items[index].Code == code {
			return &items[index]
		}
	}
	return nil
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

func TestGeminiOAuthValidatesQuotaProjectBeforeOpeningGoogleAuthorization(t *testing.T) {
	adapter := NewGeminiOAuthAdapter(AdapterConfig{
		GoogleClientID:     "google-client",
		GoogleClientSecret: "google-secret",
		WebAuthEnabled:     true,
		GeminiOAuthEnabled: true,
	})
	for _, projectID := range []string{"", "Project Name", "123456", "short", "ends-with-hyphen-"} {
		if _, err := adapter.Start(context.Background(), AuthorizationTransaction{
			ProjectID: projectID,
		}, "challenge"); err == nil {
			t.Fatalf("Start() accepted invalid Google Cloud project ID %q", projectID)
		}
	}
	start, err := adapter.Start(context.Background(), AuthorizationTransaction{
		State: "state", Nonce: "nonce", ProjectID: "gemini-project-123",
		RedirectURI: "https://tokhub.example.test/api/me/ai-authorizations/google/callback",
	}, "challenge")
	if err != nil {
		t.Fatalf("Start() rejected valid Google Cloud project ID: %v", err)
	}
	if parsed, parseErr := url.Parse(start.AuthorizationURL); parseErr != nil ||
		parsed.Query().Get("scope") != geminiOAuthScope {
		t.Fatalf("unexpected Gemini authorization URL: %q error=%v", start.AuthorizationURL, parseErr)
	}
}

func TestChatGPTCodexAdapterParsesFixedCallbackAndPinsPrivateEndpoint(t *testing.T) {
	var refreshCalls int
	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
			var refreshRequest map[string]string
			if err := json.NewDecoder(r.Body).Decode(&refreshRequest); err != nil {
				t.Fatal(err)
			}
			refreshCalls++
			if refreshRequest["grant_type"] != "refresh_token" ||
				refreshRequest["client_id"] != CodexOAuthClientID ||
				refreshRequest["refresh_token"] != "refresh" ||
				refreshRequest["scope"] != "" {
				t.Fatalf("unexpected ChatGPT refresh JSON: %v", refreshRequest)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"access_token":  "access-refreshed",
				"refresh_token": "refresh-rotated",
				"token_type":    "Bearer",
				"expires_in":    3600,
			})
			return
		}
		if err := r.ParseForm(); err != nil {
			t.Fatal(err)
		}
		if r.Form.Get("grant_type") == "refresh_token" {
			t.Fatalf("ChatGPT refresh used legacy form encoding: %v", r.Form)
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
	start, err := adapter.Start(context.Background(), AuthorizationTransaction{
		State: "authz.state",
	}, "challenge")
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	authorizeURL, err := url.Parse(start.AuthorizationURL)
	if err != nil {
		t.Fatal(err)
	}
	if authorizeURL.Query().Get("redirect_uri") != CodexOAuthRedirectURI ||
		authorizeURL.Query().Get("scope") != codexOAuthScope ||
		authorizeURL.Query().Get("code_challenge") != "challenge" ||
		authorizeURL.Query().Get("originator") != "codex_cli_rs" {
		t.Fatalf("unexpected ChatGPT authorization query: %v", authorizeURL.Query())
	}
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
	refreshed, err := adapter.Refresh(context.Background(), bundle)
	if err != nil {
		t.Fatalf("Refresh() error = %v", err)
	}
	if refreshCalls != 1 || refreshed.AccessToken != "access-refreshed" ||
		refreshed.RefreshToken != "refresh-rotated" || refreshed.AccountID != "account-1" {
		t.Fatalf("refreshed ChatGPT bundle = %#v calls=%d", refreshed, refreshCalls)
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
