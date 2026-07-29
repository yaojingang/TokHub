package events

import (
	"context"
	"errors"
	"testing"
	"time"

	"tokhub/internal/connections"
)

func TestCredentialRefreshBackoffIsBounded(t *testing.T) {
	want := []time.Duration{time.Minute, 5 * time.Minute, 15 * time.Minute, time.Hour, time.Hour}
	for index, expected := range want {
		if got := credentialRefreshBackoff(index + 1); got != expected {
			t.Fatalf("credentialRefreshBackoff(%d) = %s, want %s", index+1, got, expected)
		}
	}
}

func TestCredentialRefreshErrorClassificationRequiresLoginOnlyForPermanentGrantFailure(t *testing.T) {
	code, reauth := classifyCredentialRefreshError(connections.ErrCredentialReauth)
	if code != "invalid_grant" || !reauth {
		t.Fatalf("reauth classification = %q %v", code, reauth)
	}
	code, reauth = classifyCredentialRefreshError(errors.New("network timeout"))
	if code != "refresh_temporary" || reauth {
		t.Fatalf("temporary classification = %q %v", code, reauth)
	}
}

func TestCredentialRefreshProviderGateSpacesAttemptsAndHonorsCancellation(t *testing.T) {
	gate := newCredentialRefreshProviderGate(1, 2)
	now := time.Unix(1_700_000_000, 0)
	if first := gate.reserveRateSlot(now); !first.Equal(now) {
		t.Fatalf("first rate slot = %s, want %s", first, now)
	}
	if second := gate.reserveRateSlot(now); !second.Equal(now.Add(500 * time.Millisecond)) {
		t.Fatalf("second rate slot = %s, want %s", second, now.Add(500*time.Millisecond))
	}

	release, err := gate.acquireConcurrency(context.Background())
	if err != nil {
		t.Fatalf("acquire first concurrency slot: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := gate.acquireConcurrency(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("second concurrency acquire error = %v, want context cancellation", err)
	}
	release()
}

func TestCredentialRefreshMutationContextPersistsTimeoutAndHonorsShutdown(t *testing.T) {
	timedOut, cancelTimedOut := context.WithTimeout(context.Background(), time.Nanosecond)
	defer cancelTimedOut()
	<-timedOut.Done()

	mutationCtx, cancelMutation, err := credentialRefreshMutationContext(timedOut)
	if err != nil {
		t.Fatalf("credentialRefreshMutationContext(timeout) error = %v", err)
	}
	defer cancelMutation()
	if mutationCtx.Err() != nil {
		t.Fatalf("timeout mutation context is already done: %v", mutationCtx.Err())
	}

	shutdown, cancelShutdown := context.WithCancel(context.Background())
	cancelShutdown()
	mutationCtx, cancelMutation, err = credentialRefreshMutationContext(shutdown)
	cancelMutation()
	if mutationCtx != nil {
		t.Fatal("shutdown mutation context should be nil")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("credentialRefreshMutationContext(shutdown) error = %v, want context.Canceled", err)
	}
}
