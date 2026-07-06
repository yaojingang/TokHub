package prober

import "testing"

func TestSynthesizeStatus(t *testing.T) {
	tests := []struct {
		name string
		l1   LayerSummary
		l2   LayerSummary
		want string
	}{
		{
			name: "healthy",
			l1:   LayerSummary{Status: "ok"},
			l2:   LayerSummary{Status: "ok"},
			want: "healthy",
		},
		{
			name: "connectivity down when l1 down",
			l1:   LayerSummary{Status: "down", ErrorType: "tcp_timeout"},
			l2:   LayerSummary{Status: "na"},
			want: "connectivity_down",
		},
		{
			name: "auth error from l2",
			l1:   LayerSummary{Status: "ok"},
			l2:   LayerSummary{Status: "auth_error", ErrorType: "auth_error"},
			want: "auth_error",
		},
		{
			name: "unknown without both layers",
			l1:   LayerSummary{Status: "ok"},
			l2:   LayerSummary{Status: "na"},
			want: "unknown",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SynthesizeStatus(tt.l1, tt.l2)
			if got.Status != tt.want {
				t.Fatalf("status = %q, want %q", got.Status, tt.want)
			}
		})
	}
}

func TestSynthesizeStatusWithL3(t *testing.T) {
	tests := []struct {
		name string
		l3   LayerSummary
		want string
	}{
		{name: "healthy when l3 ok", l3: LayerSummary{Status: "ok"}, want: "healthy"},
		{name: "healthy when l3 absent", l3: LayerSummary{Status: "na"}, want: "healthy"},
		{name: "auth error from l3", l3: LayerSummary{Status: "auth_error", ErrorType: "auth_error"}, want: "auth_error"},
		{name: "degraded from l3 warn", l3: LayerSummary{Status: "warn", ErrorType: "slow_response"}, want: "degraded"},
		{name: "functional down from l3 down", l3: LayerSummary{Status: "down", ErrorType: "empty_content"}, want: "functional_down"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SynthesizeStatusWithL3(LayerSummary{Status: "ok"}, LayerSummary{Status: "ok"}, tt.l3)
			if got.Status != tt.want {
				t.Fatalf("status = %q, want %q", got.Status, tt.want)
			}
		})
	}
}

func TestSynthesizeStatusWithL3DegradesWhenGenerationWorksButModelsProbeFails(t *testing.T) {
	got := SynthesizeStatusWithL3(
		LayerSummary{Status: "ok"},
		LayerSummary{Status: "auth_error", ErrorType: "auth_error"},
		LayerSummary{Status: "ok"},
	)
	if got.Status != "degraded" || got.ErrorType != "models_auth_error" {
		t.Fatalf("decision = %+v, want degraded/models_auth_error", got)
	}
}

func TestSynthesizeStatusWithL3DegradesWhenGenerationWorksButL1Fails(t *testing.T) {
	got := SynthesizeStatusWithL3(
		LayerSummary{Status: "down", ErrorType: "timeout"},
		LayerSummary{Status: "ok"},
		LayerSummary{Status: "ok"},
	)
	if got.Status != "degraded" || got.ErrorType != "timeout" {
		t.Fatalf("decision = %+v, want degraded/timeout", got)
	}
}

func TestSynthesizeStatusWithL3PreservesModelNotFoundWhenGenerationIsUnavailable(t *testing.T) {
	got := SynthesizeStatusWithL3(
		LayerSummary{Status: "ok"},
		LayerSummary{Status: "down", ErrorType: "model_not_found"},
		LayerSummary{Status: "down", ErrorType: "upstream_unavailable"},
	)
	if got.Status != "functional_down" || got.ErrorType != "model_not_found" {
		t.Fatalf("decision = %+v, want functional_down/model_not_found", got)
	}
}
