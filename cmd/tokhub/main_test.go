package main

import "testing"

func TestAIQuickRelayRevealExpiryRunsOnlyOnAllOrWorkerRoles(t *testing.T) {
	for _, role := range []string{"all", "worker"} {
		if !shouldExpireAIQuickRelayReveals(role) {
			t.Fatalf("role %q did not own quick relay reveal expiry", role)
		}
	}
	for _, role := range []string{"api", "gateway", "prober", "migrate", "seed"} {
		if shouldExpireAIQuickRelayReveals(role) {
			t.Fatalf("role %q unexpectedly owned quick relay reveal expiry", role)
		}
	}
}
