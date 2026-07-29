package connections

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestRedisAuthorizationStorePreservesBoundValuesAfterMismatch(t *testing.T) {
	redisURL := os.Getenv("TOKHUB_TEST_REDIS_URL")
	if redisURL == "" {
		t.Skip("TOKHUB_TEST_REDIS_URL is not configured")
	}
	ctx := context.Background()
	store, err := NewRedisAuthorizationStore(ctx, redisURL)
	if err != nil {
		t.Fatalf("NewRedisAuthorizationStore() error = %v", err)
	}
	defer store.Close()

	transaction := AuthorizationTransaction{
		ID:          "authz_" + uuid.NewString(),
		UserID:      "usr_redis_test",
		SessionHash: "session_redis_test",
		Provider:    "gemini",
		Method:      "oauth",
		ExpiresAt:   time.Now().Add(time.Minute),
	}
	if err := store.Put(ctx, transaction); err != nil {
		t.Fatalf("Put() error = %v", err)
	}
	if _, err := store.Consume(ctx, transaction.ID, "usr_other", transaction.SessionHash); !errors.Is(err, ErrAuthorizationBinding) {
		t.Fatalf("mismatched Consume() error = %v", err)
	}
	if _, err := store.Consume(ctx, transaction.ID, transaction.UserID, transaction.SessionHash); err != nil {
		t.Fatalf("bound Consume() after mismatch error = %v", err)
	}

	grant := StepUpGrant{
		Token:       "step_" + uuid.NewString(),
		UserID:      transaction.UserID,
		SessionHash: transaction.SessionHash,
		ExpiresAt:   time.Now().Add(time.Minute),
	}
	if err := store.PutStepUp(ctx, grant); err != nil {
		t.Fatalf("PutStepUp() error = %v", err)
	}
	if err := store.ConsumeStepUp(ctx, grant.Token, grant.UserID, "session_other"); !errors.Is(err, ErrAuthorizationBinding) {
		t.Fatalf("mismatched ConsumeStepUp() error = %v", err)
	}
	if err := store.ConsumeStepUp(ctx, grant.Token, grant.UserID, grant.SessionHash); err != nil {
		t.Fatalf("bound ConsumeStepUp() after mismatch error = %v", err)
	}
}
