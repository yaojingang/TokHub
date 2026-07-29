package connections

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestAuthorizationTransactionIsBoundAndConsumedOnce(t *testing.T) {
	now := time.Date(2026, 7, 29, 10, 0, 0, 0, time.UTC)
	store := NewMemoryAuthorizationStore(func() time.Time { return now })
	transaction := AuthorizationTransaction{
		ID:           "authz_1",
		UserID:       "usr_1",
		SessionHash:  "session_hash_1",
		Provider:     "gemini",
		Method:       "oauth",
		State:        "state_1",
		CodeVerifier: "verifier_1",
		Nonce:        "nonce_1",
		ExpiresAt:    now.Add(10 * time.Minute),
	}
	if err := store.Put(context.Background(), transaction); err != nil {
		t.Fatalf("Put() error = %v", err)
	}

	if _, err := store.Consume(context.Background(), transaction.ID, "usr_2", transaction.SessionHash); !errors.Is(err, ErrAuthorizationBinding) {
		t.Fatalf("Consume() binding error = %v, want ErrAuthorizationBinding", err)
	}
	got, err := store.Consume(context.Background(), transaction.ID, transaction.UserID, transaction.SessionHash)
	if err != nil {
		t.Fatalf("Consume() error = %v", err)
	}
	if got.State != transaction.State || got.CodeVerifier != transaction.CodeVerifier {
		t.Fatalf("Consume() = %#v", got)
	}
	if _, err := store.Consume(context.Background(), transaction.ID, transaction.UserID, transaction.SessionHash); !errors.Is(err, ErrAuthorizationNotFound) {
		t.Fatalf("second Consume() error = %v, want ErrAuthorizationNotFound", err)
	}
}

func TestStepUpGrantIsSingleUseAndSessionBound(t *testing.T) {
	now := time.Date(2026, 7, 29, 10, 0, 0, 0, time.UTC)
	store := NewMemoryAuthorizationStore(func() time.Time { return now })
	grant := StepUpGrant{
		Token:       "step_1",
		UserID:      "usr_1",
		SessionHash: "session_hash_1",
		ExpiresAt:   now.Add(10 * time.Minute),
	}
	if err := store.PutStepUp(context.Background(), grant); err != nil {
		t.Fatalf("PutStepUp() error = %v", err)
	}
	if err := store.ConsumeStepUp(context.Background(), grant.Token, grant.UserID, "other_session"); !errors.Is(err, ErrAuthorizationBinding) {
		t.Fatalf("ConsumeStepUp() binding error = %v", err)
	}
	if err := store.ConsumeStepUp(context.Background(), grant.Token, grant.UserID, grant.SessionHash); err != nil {
		t.Fatalf("ConsumeStepUp() error = %v", err)
	}
	if err := store.ConsumeStepUp(context.Background(), grant.Token, grant.UserID, grant.SessionHash); !errors.Is(err, ErrAuthorizationNotFound) {
		t.Fatalf("second ConsumeStepUp() error = %v", err)
	}
}

func TestGenerateOAuthProofUsesS256AndStrongRandomValues(t *testing.T) {
	proof, err := GenerateOAuthProof()
	if err != nil {
		t.Fatalf("GenerateOAuthProof() error = %v", err)
	}
	if len(proof.State) < 43 || len(proof.Nonce) < 43 || len(proof.CodeVerifier) < 43 {
		t.Fatalf("OAuth proof values are too short: %#v", proof)
	}
	if proof.CodeChallenge == proof.CodeVerifier || len(proof.CodeChallenge) < 43 {
		t.Fatalf("invalid S256 challenge: %#v", proof)
	}
}

func TestAuthorizationIDFromStateExtractsOnlyTokHubTransactionPrefix(t *testing.T) {
	id, err := AuthorizationIDFromState("authz_123e4567-e89b-12d3-a456-426614174000.random-state")
	if err != nil || id != "authz_123e4567-e89b-12d3-a456-426614174000" {
		t.Fatalf("AuthorizationIDFromState() = %q, %v", id, err)
	}
	for _, value := range []string{"", "state", "other_123.random", "authz_bad/path.random"} {
		if _, err := AuthorizationIDFromState(value); err == nil {
			t.Fatalf("AuthorizationIDFromState(%q) succeeded", value)
		}
	}
}

func TestParseCodexCallbackRejectsEveryURLOutsideFixedLoopback(t *testing.T) {
	for _, raw := range []string{
		"https://localhost:1455/auth/callback?code=abc&state=state",
		"http://127.0.0.1:1455/auth/callback?code=abc&state=state",
		"http://localhost:1455/other?code=abc&state=state",
		"http://localhost:1455/auth/callback?code=abc&code=def&state=state",
		"http://localhost:1455/auth/callback?code=abc&state=state&state=other",
	} {
		if _, _, err := ParseCodexCallback(raw); err == nil {
			t.Fatalf("ParseCodexCallback(%q) succeeded", raw)
		}
	}
	code, state, err := ParseCodexCallback("http://localhost:1455/auth/callback?code=abc&state=state")
	if err != nil || code != "abc" || state != "state" {
		t.Fatalf("valid callback parsed as code=%q state=%q err=%v", code, state, err)
	}
}

func TestAuthMaterialRejectsUntrustedHeaders(t *testing.T) {
	material := AuthMaterial{
		Mode:     AuthModeOAuthBearer,
		Endpoint: "https://generativelanguage.googleapis.com/v1beta",
		Headers: http.Header{
			"Authorization":       []string{"Bearer access"},
			"X-Goog-User-Project": []string{"project-1"},
		},
	}
	if err := material.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	material.Headers.Set("Cookie", "session=secret")
	if err := material.Validate(); err == nil {
		t.Fatal("Validate() accepted a Cookie header")
	}
}

func TestCredentialBundleRoundTripsWithoutDroppingRefreshToken(t *testing.T) {
	expiresAt := time.Date(2026, 7, 29, 11, 0, 0, 0, time.UTC)
	bundle := CredentialBundle{
		Schema:          CredentialBundleSchemaV1,
		AccessToken:     "access",
		RefreshToken:    "refresh",
		IDToken:         "id",
		TokenType:       "Bearer",
		Scopes:          []string{"scope-a"},
		ExpiresAt:       expiresAt,
		ProviderSubject: "subject",
		AccountID:       "account",
		ProjectID:       "project",
	}
	raw, err := bundle.Marshal()
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	decoded, err := ParseCredentialBundle(raw)
	if err != nil {
		t.Fatalf("ParseCredentialBundle() error = %v", err)
	}
	if decoded.RefreshToken != bundle.RefreshToken || !decoded.ExpiresAt.Equal(expiresAt) {
		t.Fatalf("decoded bundle = %#v", decoded)
	}
}

func TestParseCredentialBundleRejectsTrailingDocuments(t *testing.T) {
	bundle := CredentialBundle{
		Schema:          CredentialBundleSchemaV1,
		AccessToken:     "access",
		TokenType:       "Bearer",
		ProviderSubject: "subject",
	}
	raw, err := bundle.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ParseCredentialBundle(raw + `{"accessToken":"replacement"}`); err == nil {
		t.Fatal("ParseCredentialBundle() accepted a trailing JSON document")
	}
}

func TestParseOIDCClaimsAcceptsBothDocumentedGoogleIssuers(t *testing.T) {
	now := time.Date(2026, 7, 29, 10, 0, 0, 0, time.UTC)
	for _, issuer := range []string{"https://accounts.google.com", "accounts.google.com"} {
		token := unsignedClaimsToken(t, map[string]any{
			"iss": issuer, "aud": "google-client", "sub": "subject-1",
			"nonce": "nonce-1", "exp": now.Add(time.Hour).Unix(),
		})
		claims, err := parseOIDCClaims(token, "https://accounts.google.com", "google-client", "nonce-1", true, now)
		if err != nil {
			t.Fatalf("parseOIDCClaims() rejected issuer %q: %v", issuer, err)
		}
		if claims.Subject != "subject-1" {
			t.Fatalf("claims = %#v", claims)
		}
	}

	token := unsignedClaimsToken(t, map[string]any{
		"iss": "https://attacker.example.test", "aud": "google-client", "sub": "subject-1",
		"nonce": "nonce-1", "exp": now.Add(time.Hour).Unix(),
	})
	if _, err := parseOIDCClaims(token, "https://accounts.google.com", "google-client", "nonce-1", true, now); err == nil {
		t.Fatal("parseOIDCClaims() accepted an unrelated issuer")
	}
}

func TestParseOIDCClaimsRequiresAuthorizedPartyForMultipleAudiences(t *testing.T) {
	now := time.Date(2026, 7, 29, 10, 0, 0, 0, time.UTC)
	base := map[string]any{
		"iss": "https://accounts.google.com", "aud": []string{"google-client", "other-client"},
		"sub": "subject-1", "nonce": "nonce-1", "exp": now.Add(time.Hour).Unix(),
	}
	if _, err := parseOIDCClaims(unsignedClaimsToken(t, base), "https://accounts.google.com", "google-client", "nonce-1", true, now); err == nil {
		t.Fatal("parseOIDCClaims() accepted multiple audiences without azp")
	}
	base["azp"] = "google-client"
	if _, err := parseOIDCClaims(unsignedClaimsToken(t, base), "https://accounts.google.com", "google-client", "nonce-1", true, now); err != nil {
		t.Fatalf("parseOIDCClaims() rejected matching azp: %v", err)
	}
}

func TestExchangeOAuthFormClassifiesPermanentRefreshFailures(t *testing.T) {
	for _, code := range []string{
		"invalid_grant", "invalid_token", "invalid_refresh_token", "token_expired",
		"app_session_terminated", "refresh_token_reused", "refresh_token_invalidated",
	} {
		t.Run(code, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusBadRequest)
				_ = json.NewEncoder(w).Encode(map[string]any{
					"error": code, "error_description": "sensitive provider detail",
				})
			}))
			t.Cleanup(server.Close)
			_, err := exchangeOAuthForm(context.Background(), server.Client(), server.URL, nil)
			if !errors.Is(err, ErrCredentialReauth) {
				t.Fatalf("exchangeOAuthForm() error = %v, want ErrCredentialReauth", err)
			}
			if strings.Contains(err.Error(), "sensitive provider detail") {
				t.Fatalf("exchangeOAuthForm() leaked provider detail: %v", err)
			}
		})
	}
}

func TestExchangeOAuthFormClassifiesNestedProviderErrorWithoutLeakingBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{
			"error":{
				"code":"refresh_token_reused",
				"message":"refresh-token-secret must not leak"
			}
		}`))
	}))
	t.Cleanup(server.Close)
	_, err := exchangeOAuthForm(context.Background(), server.Client(), server.URL, nil)
	if !errors.Is(err, ErrCredentialReauth) {
		t.Fatalf("exchangeOAuthForm() error = %v, want ErrCredentialReauth", err)
	}
	if strings.Contains(err.Error(), "refresh-token-secret") {
		t.Fatalf("exchangeOAuthForm() leaked provider body: %v", err)
	}
}

func TestSameCredentialIdentityLocksSubjectAndOptionalAccount(t *testing.T) {
	current := CredentialBundle{ProviderSubject: "subject-1", AccountID: "account-1"}
	for _, test := range []struct {
		name        string
		replacement CredentialBundle
		want        bool
	}{
		{name: "same identity", replacement: CredentialBundle{ProviderSubject: "subject-1", AccountID: "account-1"}, want: true},
		{name: "different subject", replacement: CredentialBundle{ProviderSubject: "subject-2", AccountID: "account-1"}},
		{name: "different account", replacement: CredentialBundle{ProviderSubject: "subject-1", AccountID: "account-2"}},
		{name: "missing account", replacement: CredentialBundle{ProviderSubject: "subject-1"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := SameCredentialIdentity(current, test.replacement); got != test.want {
				t.Fatalf("SameCredentialIdentity() = %v, want %v", got, test.want)
			}
		})
	}
	if !SameCredentialIdentity(
		CredentialBundle{ProviderSubject: "google-subject", ProjectID: "project-a"},
		CredentialBundle{ProviderSubject: "google-subject", ProjectID: "project-b"},
	) {
		t.Fatal("SameCredentialIdentity() treated a Gemini project change as an account change")
	}
}

func unsignedClaimsToken(t *testing.T, claims map[string]any) string {
	t.Helper()
	header, err := json.Marshal(map[string]any{"alg": "none"})
	if err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(claims)
	if err != nil {
		t.Fatal(err)
	}
	return base64.RawURLEncoding.EncodeToString(header) + "." +
		base64.RawURLEncoding.EncodeToString(payload) + "."
}
