package store

import (
	"context"
	"strings"
	"testing"
)

func TestGatewayKeyHelpersDoNotExposePlainKey(t *testing.T) {
	key, err := NewGatewayPlainKey()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(key, "sk-th-") {
		t.Fatalf("expected gateway key prefix, got %q", key)
	}
	hash := HashGatewayKey(key)
	if hash == key || strings.Contains(hash, key) {
		t.Fatal("hash must not contain the plaintext key")
	}
	mask := MaskGatewayKey(key)
	if mask == key || strings.Contains(mask, key[12:len(key)-4]) {
		t.Fatal("mask must not expose the plaintext key body")
	}
	if GatewayKeyPrefix(key) == key {
		t.Fatal("prefix must be a short lookup hint, not the full key")
	}
}

func TestPlanGatewayRouteSkipsBrokenUpstreamsAndAppliesPolicy(t *testing.T) {
	repo := &Repository{}
	gateway := Gateway{
		Policy: "latency",
		Upstreams: []GatewayUpstream{
			{ChannelID: "slow", Enabled: true, Status: "healthy", LatencyP95Ms: 900, SuccessRate: 99, Score: 90, CostUSD: 0.2},
			{ChannelID: "fast", Enabled: true, Status: "healthy", LatencyP95Ms: 120, SuccessRate: 94, Score: 70, CostUSD: 0.5},
			{ChannelID: "warn", Enabled: true, Status: "degraded", LatencyP95Ms: 250, SuccessRate: 91, Score: 65, CostUSD: 0.4},
			{ChannelID: "unknown", Enabled: true, Status: "unknown", LatencyP95Ms: 1, SuccessRate: 100, Score: 100, CostUSD: 0.1},
			{ChannelID: "down", Enabled: true, Status: "connectivity_down", LatencyP95Ms: 1, SuccessRate: 100, Score: 100, CostUSD: 0.1},
		},
	}

	latency := repo.PlanGatewayRoute(context.Background(), gateway)
	if len(latency) != 3 {
		t.Fatalf("expected untested and broken upstreams to be skipped, got %d candidates", len(latency))
	}
	if latency[0].ChannelID != "fast" {
		t.Fatalf("expected fastest healthy upstream first, got %q", latency[0].ChannelID)
	}

	gateway.Policy = "success"
	success := repo.PlanGatewayRoute(context.Background(), gateway)
	if success[0].ChannelID != "slow" {
		t.Fatalf("expected highest success upstream first, got %q", success[0].ChannelID)
	}

	gateway.Policy = "cost"
	cost := repo.PlanGatewayRoute(context.Background(), gateway)
	if cost[0].ChannelID != "slow" {
		t.Fatalf("expected lowest cost healthy upstream first, got %q", cost[0].ChannelID)
	}
}

func TestPlanGatewayRouteDoesNotFallbackToIneligibleUpstreams(t *testing.T) {
	repo := &Repository{}
	gateway := Gateway{
		Policy: "latency",
		Upstreams: []GatewayUpstream{
			{ChannelID: "unknown", Enabled: true, Status: "unknown"},
			{ChannelID: "auth", Enabled: true, Status: "auth_error"},
			{ChannelID: "down", Enabled: true, Status: "connectivity_down"},
			{ChannelID: "functional", Enabled: true, Status: "functional_down"},
			{ChannelID: "disabled", Enabled: false, Status: "healthy"},
		},
	}
	if got := repo.PlanGatewayRoute(context.Background(), gateway); len(got) != 0 {
		t.Fatalf("expected no route candidates for ineligible upstreams, got %#v", got)
	}
}

func TestGatewayEligibilityPredicatesRequireEnabledTestedChannels(t *testing.T) {
	platform := gatewayEligiblePlatformPredicate("c")
	for _, want := range []string{
		"c.owner_type='platform'",
		"c.gateway_enabled is true",
		"c.status in ('healthy','degraded')",
		"c.deleted_at is null",
	} {
		if !strings.Contains(platform, want) {
			t.Fatalf("platform eligibility predicate %q does not contain %q", platform, want)
		}
	}

	private := gatewayEligiblePrivatePredicate("c", "$1")
	for _, want := range []string{
		"c.owner_type='user'",
		"c.gateway_enabled is true",
		"coalesce(c.org_id,'org_' || coalesce(c.owner_id,''))=$1",
		"c.status in ('healthy','degraded')",
		"c.deleted_at is null",
	} {
		if !strings.Contains(private, want) {
			t.Fatalf("private eligibility predicate %q does not contain %q", private, want)
		}
	}
}

func TestGatewayRuntimePlatformCredentialPredicateRequiresGatewayEnabled(t *testing.T) {
	predicate := gatewayRuntimePlatformCredentialPredicate("c", "$2", "$3")
	for _, want := range []string{
		"c.owner_type='platform'",
		"c.gateway_enabled is true",
		"$2=$3",
		"u.plan='super_vip'",
		"u.status='active'",
		"u.deleted_at is null",
	} {
		if !strings.Contains(predicate, want) {
			t.Fatalf("runtime credential predicate %q does not contain %q", predicate, want)
		}
	}
}

func TestCalculateModelCostUSD(t *testing.T) {
	cost := CalculateModelCostUSD(1000, 500, 2.50, 10.00)
	if cost != 0.0075 {
		t.Fatalf("cost = %.6f, want 0.007500", cost)
	}
	if negative := CalculateModelCostUSD(-100, -50, 2.50, 10.00); negative != 0 {
		t.Fatalf("negative token cost = %.6f, want 0", negative)
	}
}

func TestGatewayUsageWindowPredicateUsesNaturalDays(t *testing.T) {
	got := gatewayUsageWindowPredicate("e")
	want := "e.created_at::date >= current_date - (($1::int - 1) * interval '1 day')"
	if got != want {
		t.Fatalf("usage window predicate = %q, want %q", got, want)
	}
	if strings.Contains(got, "now()") {
		t.Fatalf("usage window should not use rolling 24h windows: %q", got)
	}
}
