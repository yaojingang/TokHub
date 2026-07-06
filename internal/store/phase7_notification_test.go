package store

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestNotificationChannelJSONDoesNotExposeTarget(t *testing.T) {
	channel := NotificationChannel{
		ID:                "ntc_test",
		Type:              "webhook",
		Target:            "https://notify.example.com/hook/super-secret?token=super-secret",
		TargetFingerprint: "fingerprint-secret",
		TargetMask:        MaskNotificationTarget("webhook", "https://notify.example.com/hook/super-secret?token=super-secret"),
	}

	raw, err := json.Marshal(channel)
	if err != nil {
		t.Fatal(err)
	}
	payload := string(raw)
	if strings.Contains(payload, "super-secret") || strings.Contains(payload, "hook/super-secret") {
		t.Fatalf("notification channel JSON leaked target: %s", payload)
	}
	if strings.Contains(payload, "fingerprint-secret") || strings.Contains(payload, "targetFingerprint") {
		t.Fatalf("notification channel JSON leaked target fingerprint: %s", payload)
	}
	if !strings.Contains(payload, `"targetMask":"https://notify.example.com/***"`) {
		t.Fatalf("notification channel JSON did not include target mask: %s", payload)
	}
}

func TestMaskNotificationTarget(t *testing.T) {
	cases := []struct {
		name        string
		channelType string
		target      string
		want        string
	}{
		{name: "email", channelType: "email", target: "ops@example.com", want: "o***@example.com"},
		{name: "single letter email", channelType: "email", target: "a@example.com", want: "a***@example.com"},
		{name: "webhook", channelType: "webhook", target: "https://notify.example.com/hook/token-123?secret=token-123", want: "https://notify.example.com/***"},
		{name: "feishu", channelType: "feishu", target: "https://open.feishu.cn/open-apis/bot/v2/hook/token-123", want: "https://open.feishu.cn/***"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := MaskNotificationTarget(tc.channelType, tc.target); got != tc.want {
				t.Fatalf("MaskNotificationTarget() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestValidateNotificationTargetRejectsUnsafeInput(t *testing.T) {
	if err := validateNotificationTarget("email", "ops@example.com\r\nBcc: attacker@example.com"); err == nil {
		t.Fatal("expected CRLF email target to be rejected")
	}
	if err := validateNotificationTarget("email", "TokHub <ops@example.com>"); err == nil {
		t.Fatal("expected display-name email target to be rejected")
	}
	if err := validateNotificationTarget("webhook", "ftp://notify.example.com/hook"); err == nil {
		t.Fatal("expected non-http webhook target to be rejected")
	}
	if err := validateNotificationTarget("webhook", "https://notify.example.com/hook"); err != nil {
		t.Fatalf("expected https webhook target to be accepted: %v", err)
	}
}
