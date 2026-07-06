package api

import (
	"bytes"
	"encoding/csv"
	"strings"
	"testing"
	"time"

	secretcrypto "tokhub/internal/crypto"
	"tokhub/internal/store"
)

func TestParseAdminChannelImportCSVEncryptsCredentials(t *testing.T) {
	box, err := secretcrypto.NewSecretBox("test-admin-channel-import-secret-key-32")
	if err != nil {
		t.Fatal(err)
	}
	server := &Server{secretBox: box}
	body := adminChannelCSVFixture(t, []string{
		"ch_existing",
		"OpenAI 主线路",
		"OpenAI",
		"openai-compatible",
		"gpt-4o-mini",
		"gpt-4o-mini",
		"https://api.example.com/v1",
		"https://example.com",
		"OpenAI 主线路官方介绍",
		"面向开发者的 OpenAI 兼容入口",
		"**OpenAI 主线路** 提供稳定的 API 中转能力。\n\n- 支持真实探测\n- 支持企业网关候选",
		`["低延迟","可用于网关"]`,
		"https://example.com/logo.png",
		"https://example.com/about",
		"healthy",
		"1440",
		"true",
		"false",
		"0.15",
		"0.6",
		`{"temperature":0.2,"maxTokens":64}`,
		"sk-live-test",
		"",
		"",
		"",
		"",
	})

	rows, err := server.parseAdminChannelImportCSV(strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("len(rows) = %d, want 1", len(rows))
	}
	row := rows[0]
	if row.RowNumber != 2 || row.ID != "ch_existing" {
		t.Fatalf("row identity = (%d, %q)", row.RowNumber, row.ID)
	}
	if row.Input.Name != "OpenAI 主线路" || row.Input.Endpoint != "https://api.example.com/v1" {
		t.Fatalf("input = %#v", row.Input)
	}
	if row.Input.GatewayEnabled {
		t.Fatal("expected gateway_enabled false from CSV")
	}
	if row.Input.InputPerMTok != 0.15 || row.Input.OutputPerMTok != 0.6 {
		t.Fatalf("prices = %.4f / %.4f", row.Input.InputPerMTok, row.Input.OutputPerMTok)
	}
	if row.Input.ProviderConfig["temperature"] != 0.2 || row.Input.ProviderConfig["maxTokens"] != 64 {
		t.Fatalf("providerConfig = %#v", row.Input.ProviderConfig)
	}
	if row.Input.IntroTitle != "OpenAI 主线路官方介绍" || row.Input.IntroSummary != "面向开发者的 OpenAI 兼容入口" {
		t.Fatalf("intro title/summary = %q / %q", row.Input.IntroTitle, row.Input.IntroSummary)
	}
	if !strings.Contains(row.Input.IntroBody, "**OpenAI 主线路**") || len(row.Input.IntroHighlights) != 2 {
		t.Fatalf("intro body/highlights = %q / %#v", row.Input.IntroBody, row.Input.IntroHighlights)
	}
	if row.Input.LogoURL != "https://example.com/logo.png" || row.Input.IntroSourceURL != "https://example.com/about" {
		t.Fatalf("intro urls = %q / %q", row.Input.LogoURL, row.Input.IntroSourceURL)
	}
	plain, err := box.Decrypt(row.Input.Credential.Ciphertext, row.Input.Credential.Nonce)
	if err != nil {
		t.Fatal(err)
	}
	if plain != "sk-live-test" {
		t.Fatalf("decrypted credential = %q", plain)
	}
	if row.Input.Credential.Mask == "" || row.Input.Credential.Fingerprint == "" {
		t.Fatalf("credential metadata missing: %#v", row.Input.Credential)
	}
}

func TestAdminPlatformChannelInputsFromRequestUsesFormAdapterForMonitorModels(t *testing.T) {
	box, err := secretcrypto.NewSecretBox("test-admin-channel-import-secret-key-32")
	if err != nil {
		t.Fatal(err)
	}
	server := &Server{secretBox: box}

	inputs, err := server.adminPlatformChannelInputsFromRequest(adminPlatformChannelRequest{
		Name:     "Relay 主线路",
		Provider: "Relay",
		Type:     "openai-compatible",
		Endpoint: "https://api.example.com/v1",
		APIKey:   "sk-live-test",
		MonitorModels: []store.MonitorModelConfig{
			{
				Key:           "claude-sonnet-4-6",
				Label:         "Claude Sonnet 4.6",
				Model:         "claude-sonnet-4-6",
				UpstreamModel: "claude-sonnet-4-6",
				Type:          "anthropic",
				InputPerMTok:  3,
				OutputPerMTok: 15,
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(inputs) != 1 {
		t.Fatalf("len(inputs) = %d, want 1", len(inputs))
	}
	if inputs[0].Type != "openai-compatible" {
		t.Fatalf("expected form adapter to win, got %q", inputs[0].Type)
	}
	if inputs[0].Model != "claude-sonnet-4-6" || inputs[0].InputPerMTok != 3 || inputs[0].OutputPerMTok != 15 {
		t.Fatalf("monitor model fields were not applied: %#v", inputs[0])
	}
}

func TestParseAdminChannelImportCSVRejectsEmptyAPIKey(t *testing.T) {
	box, err := secretcrypto.NewSecretBox("test-admin-channel-import-secret-key-32")
	if err != nil {
		t.Fatal(err)
	}
	server := &Server{secretBox: box}
	body := adminChannelCSVFixture(t, []string{
		"",
		"Missing Key",
		"OpenAI",
		"openai-compatible",
		"gpt-4o-mini",
		"gpt-4o-mini",
		"https://api.example.com/v1",
		"",
		"",
		"",
		"",
		"[]",
		"",
		"",
		"unknown",
		"2880",
		"true",
		"true",
		"0",
		"0",
		"{}",
		"",
		"",
		"",
		"",
		"",
	})

	_, err = server.parseAdminChannelImportCSV(strings.NewReader(body))
	if err == nil || !strings.Contains(err.Error(), "apiKey is required") {
		t.Fatalf("parse error = %v, want apiKey rejection", err)
	}
}

func TestParseAdminChannelImportCSVRejectsDuplicateIDs(t *testing.T) {
	box, err := secretcrypto.NewSecretBox("test-admin-channel-import-secret-key-32")
	if err != nil {
		t.Fatal(err)
	}
	server := &Server{secretBox: box}
	body := adminChannelCSVFixture(t,
		[]string{
			"ch_dup", "Channel A", "OpenAI", "openai-compatible", "gpt-4o-mini", "gpt-4o-mini",
			"https://a.example.com/v1", "", "", "", "", "[]", "", "", "unknown", "2880", "true", "true", "0", "0", "{}", "sk-a", "", "", "", "",
		},
		[]string{
			"ch_dup", "Channel B", "OpenAI", "openai-compatible", "gpt-4o-mini", "gpt-4o-mini",
			"https://b.example.com/v1", "", "", "", "", "[]", "", "", "unknown", "2880", "true", "true", "0", "0", "{}", "sk-b", "", "", "", "",
		},
	)

	_, err = server.parseAdminChannelImportCSV(strings.NewReader(body))
	if err == nil || !strings.Contains(err.Error(), "重复使用 id ch_dup") {
		t.Fatalf("parse error = %v, want duplicate id rejection", err)
	}
}

func TestChannelSyncEndpointNormalizesBaseURL(t *testing.T) {
	got, err := channelSyncEndpoint("http://localhost:28125", true)
	if err != nil {
		t.Fatal(err)
	}
	if got != "http://localhost:28125/v1/status/channel-sync?includeCredentials=1" {
		t.Fatalf("endpoint = %q", got)
	}
	got, err = channelSyncEndpoint("https://source.example.com/v1/status", false)
	if err != nil {
		t.Fatal(err)
	}
	if got != "https://source.example.com/v1/status/channel-sync" {
		t.Fatalf("endpoint = %q", got)
	}
}

func TestChannelSyncAuditEndpointStripsCredentialQuery(t *testing.T) {
	got := channelSyncAuditEndpoint("https://source.example.com/v1/status/channel-sync?includeCredentials=1&x=y#frag")
	if got != "https://source.example.com/v1/status/channel-sync" {
		t.Fatalf("audit endpoint = %q", got)
	}
}

func TestChannelSyncImportRowsRequiresIncludedCredentials(t *testing.T) {
	box, err := secretcrypto.NewSecretBox("test-admin-channel-import-secret-key-32")
	if err != nil {
		t.Fatal(err)
	}
	server := &Server{secretBox: box}
	_, err = server.channelSyncImportRows(channelSyncPayload{Version: "tokhub.channel_sync.v1", Credentials: "omitted", Items: []channelSyncItem{{Name: "A"}}})
	if err == nil || !strings.Contains(err.Error(), "没有返回凭据明文") {
		t.Fatalf("sync import error = %v, want credentials rejection", err)
	}
}

func TestChannelSyncImportRowsMapsCredentialAndSnapshot(t *testing.T) {
	box, err := secretcrypto.NewSecretBox("test-admin-channel-import-secret-key-32")
	if err != nil {
		t.Fatal(err)
	}
	server := &Server{secretBox: box}
	sampledAt := time.Date(2026, 7, 5, 10, 30, 0, 0, time.UTC)
	rows, err := server.channelSyncImportRows(channelSyncPayload{
		Version:     "tokhub.channel_sync.v1",
		Credentials: "included",
		Items: []channelSyncItem{{
			ID:              "ch_source",
			Name:            "Source Channel",
			Provider:        "OpenAI",
			Type:            "openai-compatible",
			Model:           "gpt-4o-mini",
			UpstreamModel:   "gpt-4o-mini",
			Endpoint:        "https://api.example.com/v1",
			OfficialSiteURL: "https://example.com",
			IntroTitle:      "Source Channel 官方介绍",
			IntroSummary:    "源站同步的结构化介绍",
			IntroBody:       "第一段介绍。\n\n**TokHub** 监控段落。",
			IntroHighlights: []string{"同步亮点", "公开可见"},
			LogoURL:         "https://example.com/logo.svg",
			IntroSourceURL:  "https://example.com/source",
			Status:          "healthy",
			Score:           98,
			ProbeDaily:      1440,
			PublicVisible:   true,
			GatewayEnabled:  true,
			InputPerMTok:    0.15,
			OutputPerMTok:   0.6,
			ProviderConfig:  map[string]any{"temperature": 0.2, "maxTokens": 64},
			APIKey:          "sk-sync-secret",
			Uptime24h:       99.9,
			SuccessRate:     100,
			LatencyP95Ms:    888,
			L1Status:        "ok",
			L2Status:        "ok",
			L3Status:        "ok",
			L1LatencyMs:     11,
			L2LatencyMs:     22,
			L3LatencyMs:     333,
			TokensUsed:      42,
			CostUSD:         0.012,
			LastProbeAt:     sampledAt,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("len(rows) = %d, want 1", len(rows))
	}
	row := rows[0]
	if row.ID != "ch_source" || row.RowNumber != 1 {
		t.Fatalf("row identity = %#v", row)
	}
	plain, err := box.Decrypt(row.Input.Credential.Ciphertext, row.Input.Credential.Nonce)
	if err != nil {
		t.Fatal(err)
	}
	if plain != "sk-sync-secret" {
		t.Fatalf("decrypted credential = %q", plain)
	}
	if row.Input.IntroTitle != "Source Channel 官方介绍" || row.Input.LogoURL != "https://example.com/logo.svg" || len(row.Input.IntroHighlights) != 2 {
		t.Fatalf("intro sync fields = %#v", row.Input)
	}
	if row.Snapshot == nil || row.Snapshot.Status != "healthy" || row.Snapshot.LatencyP95Ms != 888 || row.Snapshot.SampledAt == nil || !row.Snapshot.SampledAt.Equal(sampledAt) {
		t.Fatalf("snapshot = %#v", row.Snapshot)
	}
}

func adminChannelCSVFixture(t *testing.T, rows ...[]string) string {
	t.Helper()
	var buf bytes.Buffer
	buf.WriteString("\ufeff")
	writer := csv.NewWriter(&buf)
	if err := writer.Write(adminChannelCSVHeaders); err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		if err := writer.Write(row); err != nil {
			t.Fatal(err)
		}
	}
	writer.Flush()
	if err := writer.Error(); err != nil {
		t.Fatal(err)
	}
	return buf.String()
}
