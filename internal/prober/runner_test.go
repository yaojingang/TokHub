package prober

import (
	"context"
	"testing"
	"time"

	"tokhub/internal/store"
)

func TestProbeLayerCredentialRequirement(t *testing.T) {
	tests := []struct {
		layer string
		want  bool
	}{
		{layer: "l1", want: false},
		{layer: "l2", want: true},
		{layer: "l3", want: true},
	}

	for _, tt := range tests {
		t.Run(tt.layer, func(t *testing.T) {
			if got := probeLayerNeedsCredential(tt.layer); got != tt.want {
				t.Fatalf("probeLayerNeedsCredential(%q) = %v, want %v", tt.layer, got, tt.want)
			}
		})
	}
}

func TestProbePersistenceContextSurvivesCanceledRunContext(t *testing.T) {
	runCtx, cancelRun := context.WithCancel(context.Background())
	cancelRun()

	writeCtx, cancelWrite := probePersistenceContext(runCtx)
	defer cancelWrite()

	select {
	case <-writeCtx.Done():
		t.Fatalf("persistence context was canceled with run context: %v", writeCtx.Err())
	default:
	}
	if _, ok := writeCtx.Deadline(); !ok {
		t.Fatal("persistence context must keep a bounded write timeout")
	}
}

func TestCredentialFailureStepsProduceAuthErrorSummary(t *testing.T) {
	for _, layer := range []string{"l2", "l3"} {
		t.Run(layer, func(t *testing.T) {
			steps := credentialFailureSteps(layer)
			summary := Summarize(steps)
			if summary.Status != "auth_error" || summary.ErrorType != "auth_error" {
				t.Fatalf("summary = %+v, want auth_error/auth_error", summary)
			}
			if len(steps) != 1 || steps[0].Metadata["reason"] != "credential_invalid" {
				t.Fatalf("credential failure metadata missing: %+v", steps)
			}
		})
	}
}

func TestProbePersistenceContextHasShortBoundedTimeout(t *testing.T) {
	writeCtx, cancelWrite := probePersistenceContext(context.Background())
	defer cancelWrite()

	deadline, ok := writeCtx.Deadline()
	if !ok {
		t.Fatal("persistence context missing deadline")
	}
	remaining := time.Until(deadline)
	if remaining <= 0 || remaining > probePersistenceTimeout {
		t.Fatalf("persistence timeout = %s, want within %s", remaining, probePersistenceTimeout)
	}
}

func TestProbeRunSnapshotCapturesIdentityAtExecutionTime(t *testing.T) {
	target := store.ProbeTarget{
		Name: "PackyCode", Provider: "PackyCode", Type: "anthropic",
		Model: "claude-sonnet-4-6", Endpoint: "https://user:pass@api.example.com/v1?api_key=secret#fragment",
	}
	snapshot := probeRunSnapshot(target)
	if snapshot.ChannelName != target.Name || snapshot.Provider != target.Provider || snapshot.Type != target.Type ||
		snapshot.Model != target.Model || snapshot.Endpoint != "https://api.example.com/v1" {
		t.Fatalf("snapshot = %+v, want execution-time target identity", snapshot)
	}
}
