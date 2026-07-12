package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"tokhub/internal/store"
)

func TestProbeLogQueryFromRequestParsesFiltersAndPagination(t *testing.T) {
	now := time.Date(2026, 7, 12, 10, 0, 0, 0, time.UTC)
	req := httptest.NewRequest(http.MethodGet, "/api/admin/probe-logs?range=7d&provider=PackyCode&channelId=ch_packy&layer=l3&status=failed&httpStatus=401&errorType=auth_error&source=scheduler&onlyAbnormal=true&page=2&pageSize=25", nil)

	query, err := probeLogQueryFromRequest(req, now)
	if err != nil {
		t.Fatal(err)
	}
	if !query.From.Equal(now.Add(-7*24*time.Hour)) || !query.To.Equal(now) {
		t.Fatalf("time range = %s to %s, want trailing 7 days", query.From, query.To)
	}
	if query.Provider != "PackyCode" || query.ChannelID != "ch_packy" || query.Layer != "l3" || query.Status != "failed" {
		t.Fatalf("identity filters not parsed: %+v", query)
	}
	if query.HTTPStatus != 401 || query.ErrorType != "auth_error" || query.Source != "scheduler" || !query.OnlyAbnormal {
		t.Fatalf("diagnostic filters not parsed: %+v", query)
	}
	if query.Page != 2 || query.PageSize != 25 {
		t.Fatalf("pagination = %d/%d, want 2/25", query.Page, query.PageSize)
	}
}

func TestProbeLogQueryFromRequestRejectsInvalidDiagnosticFilters(t *testing.T) {
	now := time.Date(2026, 7, 12, 10, 0, 0, 0, time.UTC)
	for _, target := range []string{
		"/api/admin/probe-logs?range=90d",
		"/api/admin/probe-logs?range=all",
		"/api/admin/probe-logs?page=0",
		"/api/admin/probe-logs?page=100001",
		"/api/admin/probe-logs?pageSize=500",
		"/api/admin/probe-logs?httpStatus=999",
		"/api/admin/probe-logs?provider=" + strings.Repeat("x", 129),
	} {
		req := httptest.NewRequest(http.MethodGet, target, nil)
		if _, err := probeLogQueryFromRequest(req, now); err == nil {
			t.Fatalf("target %q accepted invalid filters", target)
		}
	}
}

func TestProbeLogExportQueryAllowsAllAndPreservesFilters(t *testing.T) {
	now := time.Date(2026, 7, 12, 10, 0, 0, 0, time.UTC)
	req := httptest.NewRequest(http.MethodGet, "/api/admin/probe-logs/export?range=all&channelId=ch_packy&layer=l3&onlyAbnormal=true", nil)

	query, err := probeLogExportQueryFromRequest(req, now)
	if err != nil {
		t.Fatal(err)
	}
	if !query.From.IsZero() || !query.To.IsZero() {
		t.Fatalf("full export range = %s to %s, want unbounded", query.From, query.To)
	}
	if query.ChannelID != "ch_packy" || query.Layer != "l3" || !query.OnlyAbnormal {
		t.Fatalf("export filters not preserved: %+v", query)
	}
}

func TestProbeCSVCellNeutralizesSpreadsheetFormula(t *testing.T) {
	for _, input := range []string{"=HYPERLINK(\"https://example.com\")", "+cmd", "-2+3", "@SUM(A1:A2)"} {
		if got := probeCSVCell(input); got != "'"+input {
			t.Fatalf("probeCSVCell(%q) = %q, want apostrophe prefix", input, got)
		}
	}
	if got := probeCSVCell("PackyCode"); got != "PackyCode" {
		t.Fatalf("normal CSV cell changed to %q", got)
	}
}

func TestProbeExportEndpointDropsCredentialsAndQuery(t *testing.T) {
	got := probeExportEndpoint("https://user:pass@example.com/v1?token=secret#fragment")
	if got != "https://example.com/v1" {
		t.Fatalf("sanitized endpoint = %q, want origin and path only", got)
	}
}

func TestSanitizeProbeLogItemDropsEndpointCredentials(t *testing.T) {
	item := store.ProbeLogItem{Endpoint: "https://user:pass@example.com/v1?api_key=secret#fragment"}
	sanitizeProbeLogItem(&item)
	if item.Endpoint != "https://example.com/v1" {
		t.Fatalf("API endpoint = %q, want credentials and query removed", item.Endpoint)
	}
}

func TestProbeLogCSVRecordIncludesChineseMeaningsAndSafeText(t *testing.T) {
	status := http.StatusUnauthorized
	item := store.ProbeLogExportItem{
		ProbeLogItem: store.ProbeLogItem{
			ID: "pr_l3_packy", ChannelName: "=PackyCode", Provider: "PackyCode", Model: "claude-sonnet-4-6",
			Endpoint: "https://user:pass@example.com/v1?token=secret", Layer: "l3", Source: "scheduler",
			Status: "auth_error", Step: "generate", HTTPStatus: &status, ErrorType: "auth_error", LatencyMs: 238,
			StepCount: 1, StartedAt: time.Date(2026, 7, 12, 10, 0, 0, 0, time.UTC),
		},
		UpstreamErrorSummary: "@source IP denied",
	}
	record := probeLogCSVRecord(item)
	if len(record) != len(probeLogCSVHeaders) {
		t.Fatalf("CSV columns = %d, headers = %d", len(record), len(probeLogCSVHeaders))
	}
	if record[2] != "'=PackyCode" || record[5] != "https://example.com/v1" || record[19] != "'@source IP denied" {
		t.Fatalf("CSV safety fields incorrect: %#v", record)
	}
	if record[10] != "认证失败" || !strings.Contains(record[14], "来源 IP") || !strings.Contains(record[16], "来源授权") {
		t.Fatalf("Chinese meanings missing: %#v", record)
	}
}
