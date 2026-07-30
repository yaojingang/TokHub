package store

import (
	"strings"
	"testing"
	"time"
)

func TestBrowserRiskAccountKeyIsOwnerProviderScopedAndRejectsMalformedIdentity(t *testing.T) {
	fingerprint := strings.Repeat("a", 64)
	deepseek := browserRiskAccountKey("usr_one", "deepseek", fingerprint)
	deepseekOtherDevice := browserRiskAccountKey("usr_one", "deepseek", strings.Repeat("b", 64))
	openai := browserRiskAccountKey("usr_one", "openai", fingerprint)
	otherOwner := browserRiskAccountKey("usr_two", "deepseek", fingerprint)
	if len(deepseek) != 64 || len(openai) != 64 || deepseek == openai || deepseek == otherOwner {
		t.Fatalf("account risk keys are not owner/provider scoped: deepseek=%q openai=%q other=%q", deepseek, openai, otherOwner)
	}
	if deepseek != deepseekOtherDevice {
		t.Fatalf("changing connector identity reset owner/provider protection: first=%q second=%q", deepseek, deepseekOtherDevice)
	}
	for _, invalid := range []string{"", "short", strings.Repeat("z", 64)} {
		if key := browserRiskAccountKey("usr_one", "deepseek", invalid); key != "" {
			t.Fatalf("malformed fingerprint produced account key %q", key)
		}
	}
	if key := browserRiskAccountKey("", "deepseek", fingerprint); key != "" {
		t.Fatalf("empty owner produced account key %q", key)
	}
}

func TestNormalizeBrowserRiskWindowsAndPolicy(t *testing.T) {
	now := time.Date(2026, time.July, 30, 12, 0, 0, 0, time.UTC)
	state := AIBrowserRiskState{
		HourWindowStartedAt:      now.Add(-2 * time.Hour),
		DayWindowStartedAt:       now.Add(-25 * time.Hour),
		RateLimitWindowStartedAt: now.Add(-25 * time.Hour),
		RequestsHour:             9,
		RequestsDay:              11,
		RateLimitEvents:          3,
	}
	if !normalizeBrowserRiskWindows(&state, now) || state.RequestsHour != 0 ||
		state.RequestsDay != 0 || state.RateLimitEvents != 0 {
		t.Fatalf("expired browser risk windows were not reset: %#v", state)
	}
	applyBrowserRiskPolicy(&state, AIBrowserRiskPolicy{
		MinimumInterval: 15 * time.Second,
		HourlyLimit:     20,
		DailyLimit:      80,
	})
	if state.MinimumIntervalSecond != 15 || state.HourlyLimit != 20 || state.DailyLimit != 80 {
		t.Fatalf("browser risk policy was not exposed: %#v", state)
	}
}
