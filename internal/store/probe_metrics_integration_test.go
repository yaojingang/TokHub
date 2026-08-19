package store

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestPublicChannelProbeMetricsReflectRollingHistory(t *testing.T) {
	databaseURL := strings.TrimSpace(os.Getenv("TOKHUB_TEST_DATABASE_URL"))
	if databaseURL == "" {
		t.Skip("TOKHUB_TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	db, err := Open(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(db.Close)
	repo := NewRepository(db)
	channelID := "ch_metrics_" + uuid.NewString()
	if _, err := db.Exec(ctx, `
		insert into channels(id,owner_type,name,provider,type,model,upstream_model,endpoint,status,score,probe_daily,probes_used_today,data_origin)
		values($1,'platform','Rolling Metrics Canary','Review Provider','openai-compatible','gpt-canary','gpt-canary','https://metrics.invalid/v1','healthy',90,100,0,'runtime')
	`, channelID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := db.Exec(context.Background(), `delete from channels where id=$1`, channelID); err != nil {
			t.Errorf("delete metrics channel: %v", err)
		}
	})

	now := time.Now()
	for index, status := range []string{"success", "success", "failed"} {
		runID := fmt.Sprintf("pr_l1_%s_%d", channelID, index)
		startedAt := now.Add(-time.Duration(index+1) * time.Hour)
		if _, err := db.Exec(ctx, `
			insert into probe_runs(id,channel_id,layer,source,status,started_at,finished_at)
			values($1,$2,'l1','integration',$3,$4::timestamptz,$4::timestamptz + interval '1 second')
		`, runID, channelID, status, startedAt); err != nil {
			t.Fatal(err)
		}
		resultStatus := "ok"
		if status == "failed" {
			resultStatus = "down"
		}
		if _, err := db.Exec(ctx, `
			insert into probe_results(id,probe_run_id,channel_id,layer,step,status,latency_ms,created_at)
			values($1,$2,$3,'l1','http',$4,100,$5)
		`, "pres_"+runID, runID, channelID, resultStatus, startedAt); err != nil {
			t.Fatal(err)
		}
	}
	for index, status := range []string{"ok", "ok", "ok", "down"} {
		runID := fmt.Sprintf("pr_l3_%s_%d", channelID, index)
		startedAt := now.Add(-time.Duration(index+1) * time.Hour)
		runStatus := "success"
		if status == "down" {
			runStatus = "failed"
		}
		if _, err := db.Exec(ctx, `
			insert into probe_runs(id,channel_id,layer,source,status,started_at,finished_at)
			values($1,$2,'l3','integration',$3,$4::timestamptz,$4::timestamptz + interval '1 second')
		`, runID, channelID, runStatus, startedAt); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(ctx, `
			insert into probe_results(id,probe_run_id,channel_id,layer,step,status,latency_ms,metadata,created_at)
			values($1,$2,$3,'l3','generate',$4,500,jsonb_build_object('content_valid',$4='ok'),$5)
		`, "pres_"+runID, runID, channelID, status, startedAt); err != nil {
			t.Fatal(err)
		}
	}

	if err := repo.ApplyProbeStatusWithL3(ctx, channelID, "healthy", "", ProbeLayerSummary{Status: "ok"}, ProbeLayerSummary{Status: "ok"}, ProbeLayerSummary{Status: "ok", LatencyMs: 500}); err != nil {
		t.Fatal(err)
	}
	detail, err := repo.PublicChannel(ctx, channelID)
	if err != nil {
		t.Fatal(err)
	}
	if detail.Channel.Uptime24h != 66.7 {
		t.Fatalf("uptime24h = %.1f, want rolling L1 availability 66.7", detail.Channel.Uptime24h)
	}
	if detail.Channel.SuccessRate != 75 {
		t.Fatalf("successRate = %.1f, want rolling L3 success rate 75.0", detail.Channel.SuccessRate)
	}
}

func TestPublicChannelLayersKeepLastCompletedProbeWhileNextRunIsPending(t *testing.T) {
	databaseURL := strings.TrimSpace(os.Getenv("TOKHUB_TEST_DATABASE_URL"))
	if databaseURL == "" {
		t.Skip("TOKHUB_TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	db, err := Open(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(db.Close)
	repo := NewRepository(db)
	channelID := "ch_layers_" + uuid.NewString()
	if _, err := db.Exec(ctx, `
		insert into channels(id,owner_type,name,provider,type,model,upstream_model,endpoint,status,score,probe_daily,probes_used_today,data_origin)
		values($1,'platform','Layer Probe Canary','Review Provider','openai-compatible','gpt-canary','gpt-canary','https://layers.invalid/v1','healthy',90,100,0,'runtime')
	`, channelID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := db.Exec(context.Background(), `delete from channels where id=$1`, channelID); err != nil {
			t.Errorf("delete layer channel: %v", err)
		}
	})

	completedRunID := "pr_l1_completed_" + uuid.NewString()
	if _, err := db.Exec(ctx, `
		insert into probe_runs(id,channel_id,layer,source,status,started_at,finished_at)
		values($1,$2,'l1','integration','success',now() - interval '1 minute',now() - interval '59 seconds')
	`, completedRunID, channelID); err != nil {
		t.Fatal(err)
	}
	for index, step := range []string{"dns", "tcp", "tls", "http"} {
		if _, err := db.Exec(ctx, `
			insert into probe_results(id,probe_run_id,channel_id,layer,step,status,latency_ms,created_at)
			values($1,$2,$3,'l1',$4,'ok',10,now() - interval '59 seconds' + ($5::int * interval '1 millisecond'))
		`, "pres_"+uuid.NewString(), completedRunID, channelID, step, index); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Exec(ctx, `
		insert into probe_runs(id,channel_id,layer,source,status,started_at)
		values($1,$2,'l1','scheduler','running',now())
	`, "pr_l1_running_"+uuid.NewString(), channelID); err != nil {
		t.Fatal(err)
	}

	detail, err := repo.PublicChannel(ctx, channelID)
	if err != nil {
		t.Fatal(err)
	}
	if len(detail.Layers) != 4 {
		t.Fatalf("layers = %d, want 4 steps from the latest completed probe", len(detail.Layers))
	}
}
