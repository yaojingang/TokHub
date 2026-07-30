package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"tokhub/internal/browserconnector"
	"tokhub/internal/buildinfo"
	"tokhub/internal/opencliconnector"
)

func TestHelpAndVersionCommands(t *testing.T) {
	for _, test := range []struct {
		args []string
		want string
	}{
		{args: []string{"--help"}, want: "tokhub-opencli-connector pair"},
		{args: []string{"version"}, want: buildinfo.Version},
	} {
		var output bytes.Buffer
		logger := log.New(&output, "", 0)
		if err := run(test.args, logger); err != nil {
			t.Fatalf("run(%v): %v", test.args, err)
		}
		if !strings.Contains(output.String(), test.want) {
			t.Fatalf("run(%v) output = %q, want %q", test.args, output.String(), test.want)
		}
	}
}

func TestHeartbeatRequiresHealthyBrowserBridge(t *testing.T) {
	heartbeatCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		heartbeatCount++
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"connector":{"id":"aibc_test"}}`))
	}))
	defer server.Close()

	client := opencliconnector.Client{Config: opencliconnector.Config{
		ServerURL: server.URL, ConnectorID: "aibc_test", DeviceToken: strings.Repeat("a", 43),
	}}
	unhealthy := opencliconnector.Executor{Run: func(_ context.Context, _ string, _ ...string) ([]byte, error) {
		return []byte("[MISSING] Extension: not connected"), nil
	}}
	if err := heartbeatIfBrowserHealthy(context.Background(), client, unhealthy, "1.8.6"); err == nil {
		t.Fatal("heartbeat accepted a disconnected browser bridge")
	}
	if heartbeatCount != 0 {
		t.Fatalf("disconnected bridge sent %d heartbeat requests", heartbeatCount)
	}

	healthy := opencliconnector.Executor{Run: func(_ context.Context, _ string, _ ...string) ([]byte, error) {
		return []byte("[OK] Extension: connected\n[OK] Connectivity: passed"), nil
	}}
	if err := heartbeatIfBrowserHealthy(context.Background(), client, healthy, "1.8.6"); err != nil {
		t.Fatalf("healthy bridge heartbeat failed: %v", err)
	}
	if heartbeatCount != 1 {
		t.Fatalf("healthy bridge sent %d heartbeat requests, want 1", heartbeatCount)
	}
}

func TestServerHeartbeatLoopRunsIndependently(t *testing.T) {
	var heartbeatCount atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		heartbeatCount.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"connector":{"id":"aibc_test"}}`))
	}))
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var serverHealthy atomic.Bool
	serverHealthy.Store(true)
	var browserHealthy atomic.Bool
	browserHealthy.Store(true)
	client := opencliconnector.Client{Config: opencliconnector.Config{
		ServerURL: server.URL, ConnectorID: "aibc_test", DeviceToken: strings.Repeat("a", 43),
	}}
	done := make(chan struct{})
	go func() {
		maintainServerHeartbeat(
			ctx,
			client,
			"1.8.6",
			5*time.Millisecond,
			&browserHealthy,
			&serverHealthy,
			log.New(io.Discard, "", 0),
		)
		close(done)
	}()

	deadline := time.After(time.Second)
	for heartbeatCount.Load() < 2 {
		select {
		case <-deadline:
			t.Fatalf("heartbeat loop sent %d requests, want at least 2", heartbeatCount.Load())
		case <-time.After(5 * time.Millisecond):
		}
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("heartbeat loop did not stop with its context")
	}
	if !serverHealthy.Load() {
		t.Fatal("successful heartbeat loop marked the server unhealthy")
	}
}

func TestServerHeartbeatLoopStopsAdvertisingDisconnectedBrowser(t *testing.T) {
	var heartbeatCount atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		heartbeatCount.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"connector":{"id":"aibc_test"}}`))
	}))
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var browserHealthy atomic.Bool
	browserHealthy.Store(false)
	var serverHealthy atomic.Bool
	serverHealthy.Store(true)
	done := make(chan struct{})
	go func() {
		maintainServerHeartbeat(
			ctx,
			opencliconnector.Client{Config: opencliconnector.Config{
				ServerURL: server.URL, ConnectorID: "aibc_test", DeviceToken: strings.Repeat("a", 43),
			}},
			"1.8.6",
			5*time.Millisecond,
			&browserHealthy,
			&serverHealthy,
			log.New(io.Discard, "", 0),
		)
		close(done)
	}()

	time.Sleep(25 * time.Millisecond)
	if heartbeatCount.Load() != 0 {
		t.Fatalf("disconnected browser sent %d heartbeat requests", heartbeatCount.Load())
	}
	browserHealthy.Store(true)
	deadline := time.After(time.Second)
	for heartbeatCount.Load() == 0 {
		select {
		case <-deadline:
			t.Fatal("heartbeat did not resume after browser recovery")
		case <-time.After(5 * time.Millisecond):
		}
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("heartbeat loop did not stop with its context")
	}
}

func TestProcessOneTaskStopsWhenBrowserAccountChanges(t *testing.T) {
	original, err := browserconnector.NormalizeOpenCLIResult(
		browserconnector.ActionStatus,
		[]byte(`[{"logged_in":true,"site":"deepseek","user_id":"original-user","name":"Original"}]`),
	)
	if err != nil {
		t.Fatal(err)
	}
	deviceToken := strings.Repeat("a", 43)
	original.AccountFingerprint = browserconnector.BindAccountFingerprint(deviceToken, original.AccountFingerprint)
	var completed browserconnector.Result
	claimed := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/tasks/claim"):
			if claimed {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			claimed = true
			_ = json.NewEncoder(w).Encode(map[string]any{"task": map[string]any{
				"id": "aibt_test", "provider": "deepseek", "action": "ask",
				"request": map[string]any{
					"prompt":             "hello",
					"accountFingerprint": original.AccountFingerprint,
				},
				"leaseToken": "lease-test",
			}})
		case strings.HasSuffix(r.URL.Path, "/complete"):
			var request struct {
				Result browserconnector.Result `json:"result"`
			}
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				t.Errorf("decode completion: %v", err)
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			completed = request.Result
			_, _ = w.Write([]byte(`{"status":"accepted"}`))
		default:
			t.Errorf("unexpected connector path %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	askCalls := 0
	executor := opencliconnector.Executor{Run: func(_ context.Context, _ string, args ...string) ([]byte, error) {
		if len(args) > 1 && args[1] == "whoami" {
			return []byte(`[{"logged_in":true,"site":"deepseek","user_id":"different-user","name":"Different"}]`), nil
		}
		askCalls++
		return []byte(`[{"response":"must not be sent"}]`), nil
	}}
	client := opencliconnector.Client{Config: opencliconnector.Config{
		ServerURL: server.URL, ConnectorID: "aibc_test", DeviceToken: deviceToken,
	}}
	if err := processOneTask(context.Background(), client, executor); err != nil {
		t.Fatal(err)
	}
	if askCalls != 0 {
		t.Fatalf("account mismatch still sent %d model requests", askCalls)
	}
	if completed.OK || completed.ErrorCode != "identity_mismatch" {
		t.Fatalf("completion = %#v", completed)
	}
}
