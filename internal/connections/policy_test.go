package connections

import (
	"errors"
	"testing"
	"time"
)

func TestProviderPolicyMatrix(t *testing.T) {
	now := time.Date(2026, 8, 19, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		provider string
		method   string
		lab      bool
		allowed  bool
	}{
		{"openai", "api_key", false, true},
		{"openai", "official_client", false, true},
		{"openai", "codex_oauth", false, false},
		{"openai", "opencli_browser", true, true},
		{"gemini", "oauth", false, true},
		{"gemini", "official_client", false, false},
		{"deepseek", "api_key", false, true},
		{"deepseek", "deepseek_web_token", false, false},
		{"deepseek", "deepseek_web_token", true, true},
		{"grok", "official_client", false, true},
		{"grok", "opencli_browser", true, false},
		{"claude", "api_key", false, true},
	}
	for _, test := range tests {
		err := ProviderModeAllowed(test.provider, test.method, test.lab, now, false)
		if (err == nil) != test.allowed {
			t.Fatalf("%s/%s lab=%v err=%v", test.provider, test.method, test.lab, err)
		}
	}
}

func TestOfficialClientPolicyExpiresClosed(t *testing.T) {
	err := ProviderModeAllowed("openai", "official_client", false, time.Date(2026, 11, 19, 0, 0, 0, 0, time.UTC), false)
	if !errors.Is(err, ErrProviderPolicyExpired) {
		t.Fatalf("expected expired policy, got %v", err)
	}
	if err := ProviderModeAllowed("openai", "api_key", false, time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC), false); err != nil {
		t.Fatalf("official API path should remain available: %v", err)
	}
}

func TestProviderKillSwitchFailsClosed(t *testing.T) {
	err := ProviderModeAllowed("grok", "official_client", false, time.Date(2026, 8, 19, 0, 0, 0, 0, time.UTC), true)
	if !errors.Is(err, ErrProviderPolicyKilled) {
		t.Fatalf("expected kill switch denial, got %v", err)
	}
	if err := ProviderModeAllowed("grok", "api_key", false, time.Date(2026, 8, 19, 0, 0, 0, 0, time.UTC), true); err != nil {
		t.Fatalf("official API key must remain available during delegated-client kill switch: %v", err)
	}
}

func TestProviderTermsDigestStopsDelegatedModeOnly(t *testing.T) {
	if err := ValidateProviderTermsDigest("openai", "official_client", "changed"); !errors.Is(err, ErrProviderTermsChanged) {
		t.Fatalf("expected terms digest mismatch, got %v", err)
	}
	if err := ValidateProviderTermsDigest("openai", "api_key", "changed"); err != nil {
		t.Fatalf("official API key must remain available after terms digest change: %v", err)
	}
	policy, _ := ProviderPolicyFor("openai")
	if err := ValidateProviderTermsDigest("openai", "official_client", policy.TermsDigest); err != nil {
		t.Fatalf("reviewed digest must be accepted: %v", err)
	}
}
