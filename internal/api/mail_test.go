package api

import (
	"strings"
	"testing"
)

func TestParseSMTPURL(t *testing.T) {
	cfg, err := parseSMTPURL("smtp://user:pass@smtp.example.com:587?from=noreply@example.com&starttls=true")
	if err != nil {
		t.Fatalf("parseSMTPURL returned error: %v", err)
	}
	if cfg.Address != "smtp.example.com:587" || cfg.Host != "smtp.example.com" {
		t.Fatalf("unexpected address/host: %#v", cfg)
	}
	if cfg.Username != "user" || cfg.Password != "pass" {
		t.Fatalf("unexpected auth fields: %#v", cfg)
	}
	if cfg.From != "noreply@example.com" || !cfg.StartTLS || cfg.Implicit {
		t.Fatalf("unexpected smtp flags: %#v", cfg)
	}
}

func TestParseSMTPURLRequiresFrom(t *testing.T) {
	if _, err := parseSMTPURL("smtp://smtp.example.com:587"); err == nil {
		t.Fatal("expected missing from to fail")
	}
}

func TestParseSMTPURLRejectsHeaderInjection(t *testing.T) {
	if _, err := parseSMTPURL("smtp://smtp.example.com:587?from=noreply@example.com%0ABcc:attacker@example.com"); err == nil {
		t.Fatal("expected header injection in from to fail")
	}
}

func TestValidEmailTargetRejectsHeaderInjection(t *testing.T) {
	if validEmailTarget("user@example.com\r\nBcc: attacker@example.com") {
		t.Fatal("expected CRLF email target to be rejected")
	}
	if validEmailTarget("TokHub <user@example.com>") {
		t.Fatal("expected display-name email target to be rejected")
	}
	if !validEmailTarget("user@example.com") {
		t.Fatal("expected plain email target to be accepted")
	}
}

func TestBuildEmailMessageDoesNotExposeSMTPPassword(t *testing.T) {
	msg := string(buildEmailMessage("noreply@example.com", "user@example.com", "TokHub 验证", "body"))
	if strings.Contains(msg, "pass") {
		t.Fatalf("message leaked password-like content: %q", msg)
	}
	if !strings.Contains(msg, "Content-Type: text/plain; charset=UTF-8") {
		t.Fatalf("message missing content type: %q", msg)
	}
}
