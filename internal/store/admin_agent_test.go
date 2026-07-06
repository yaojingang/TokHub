package store

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestNormalizeAdminAgentScopes(t *testing.T) {
	got, err := NormalizeAdminAgentScopes([]string{" admin:write ", "ADMIN:READ", "admin:write"})
	if err != nil {
		t.Fatalf("NormalizeAdminAgentScopes returned error: %v", err)
	}
	want := []string{"admin:read", "admin:write"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("NormalizeAdminAgentScopes = %#v, want %#v", got, want)
	}
}

func TestNormalizeAdminAgentScopesExpandsWildcard(t *testing.T) {
	got, err := NormalizeAdminAgentScopes([]string{"admin:*"})
	if err != nil {
		t.Fatalf("NormalizeAdminAgentScopes returned error: %v", err)
	}
	want := []string{"admin:read", "admin:write", "admin:dangerous", "admin:secrets", "admin:export"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("NormalizeAdminAgentScopes = %#v, want %#v", got, want)
	}
}

func TestNormalizeAdminAgentScopesRejectsInvalidInput(t *testing.T) {
	for _, scopes := range [][]string{nil, {}, {"admin:unknown"}} {
		if _, err := NormalizeAdminAgentScopes(scopes); !errors.Is(err, ErrInvalidAdminAgentTokenScope) {
			t.Fatalf("NormalizeAdminAgentScopes(%#v) error = %v, want ErrInvalidAdminAgentTokenScope", scopes, err)
		}
	}
}

func TestNewAdminAgentPlainTokenAndMask(t *testing.T) {
	token, err := NewAdminAgentPlainToken()
	if err != nil {
		t.Fatalf("NewAdminAgentPlainToken returned error: %v", err)
	}
	if !strings.HasPrefix(token, "aat_") {
		t.Fatalf("token prefix = %q, want aat_", token[:4])
	}
	if got := AdminAgentTokenPrefix(token); len(got) != 16 {
		t.Fatalf("AdminAgentTokenPrefix length = %d, want 16", len(got))
	}
	if mask := MaskAdminAgentToken(token); strings.Contains(mask, token[16:]) {
		t.Fatalf("MaskAdminAgentToken leaked token suffix: %q", mask)
	}
}

func TestEnrichAgentAuditEvent(t *testing.T) {
	ctx := WithAgentAudit(context.Background(), AgentAuditContext{
		TokenID:            "aat_tok_1",
		TokenName:          "ops",
		DelegatedUserID:    "usr_1",
		DelegatedUserEmail: "owner@example.com",
		Reason:             "rotate key",
		IdempotencyKey:     "idem-1",
	})

	got := enrichAgentAuditEvent(ctx, AuditEvent{
		ActorType: "user",
		ActorID:   "usr_1",
		Action:    "gateway.key.created",
		Result:    "success",
		Metadata:  map[string]any{"existing": "value"},
	})

	if got.ActorType != "agent" || got.ActorID != "aat_tok_1" {
		t.Fatalf("actor = %s/%s, want agent/aat_tok_1", got.ActorType, got.ActorID)
	}
	for _, key := range []string{"agent_token_id", "agent_token_name", "delegated_user_id", "delegated_user_email", "agent_reason", "idempotency_key", "delegated_actor_type", "delegated_actor_id", "existing"} {
		if _, ok := got.Metadata[key]; !ok {
			t.Fatalf("expected metadata key %q in %#v", key, got.Metadata)
		}
	}
}
