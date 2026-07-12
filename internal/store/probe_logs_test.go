package store

import (
	"strings"
	"testing"
)

func TestSafeProbeLogMetadataOnlyReturnsDiagnosticFields(t *testing.T) {
	got := safeProbeLogMetadata(map[string]any{
		"models_count":           12,
		"model_found":            true,
		"upstream_error_summary": "code=ip_not_allowed",
		"authorization":          "Bearer secret",
		"api_key":                "sk-secret",
		"request_body":           map[string]any{"prompt": "private"},
	})

	if got["models_count"] != 12 || got["model_found"] != true || got["upstream_error_summary"] != "code=ip_not_allowed" {
		t.Fatalf("diagnostic fields missing: %#v", got)
	}
	for _, forbidden := range []string{"authorization", "api_key", "request_body"} {
		if _, ok := got[forbidden]; ok {
			t.Fatalf("unsafe field %q returned: %#v", forbidden, got)
		}
	}
}

func TestProbeLogAbnormalFilterDoesNotTreatRunningAsFailure(t *testing.T) {
	where, _ := probeLogWhere(ProbeLogQuery{OnlyAbnormal: true})
	if !strings.Contains(where, "('warn','auth_error','down','failed')") {
		t.Fatalf("abnormal filter = %q, want explicit terminal and warning statuses", where)
	}
	if strings.Contains(where, "running") {
		t.Fatalf("abnormal filter = %q, running probes must not be failures", where)
	}
}

func TestProbeLogQueryPreservesHistoricalIdentityAndClassifiesStaleRuns(t *testing.T) {
	for _, snapshotField := range []string{"channel_name", "provider", "type", "model", "endpoint"} {
		if !strings.Contains(probeLogBaseSQL, "pr.metadata->>'"+snapshotField+"'") {
			t.Fatalf("probe log query does not read %s from the run snapshot", snapshotField)
		}
	}
	if !strings.Contains(probeLogBaseSQL, "pr.started_at < now() - interval '10 minutes'") ||
		!strings.Contains(probeLogBaseSQL, "'probe_interrupted'") {
		t.Fatal("probe log query does not classify stale running probes as interrupted")
	}
	if strings.Contains(probeLogBaseSQL, "c.deleted_at is null") {
		t.Fatal("soft-deleted channels must retain their historical probe logs")
	}
}

func TestProbeLogQueryKeepsIntentionallyEmptySnapshotFields(t *testing.T) {
	for _, snapshotField := range []string{"channel_name", "provider", "type", "model", "endpoint"} {
		expected := "case when pr.metadata ? '" + snapshotField + "' then pr.metadata->>'" + snapshotField + "' else c."
		if !strings.Contains(probeLogBaseSQL, expected) {
			t.Fatalf("probe log query does not distinguish an empty %s snapshot from a legacy row", snapshotField)
		}
	}
}
