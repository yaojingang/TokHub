package clientconnector

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"testing"
	"time"
)

func TestSignedRequestVerification(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Unix(1_800_000_000, 0)
	body := []byte(`{"status":"online"}`)
	signature, err := SignRequest(privateKey, "POST", "/api/ai-client-connectors/heartbeat", body, now.Unix(), "nonce-1")
	if err != nil {
		t.Fatal(err)
	}
	encodedPublicKey := base64.RawURLEncoding.EncodeToString(publicKey)
	if err := VerifyRequest(encodedPublicKey, signature, "POST", "/api/ai-client-connectors/heartbeat", body, now.Unix(), "nonce-1", now); err != nil {
		t.Fatal(err)
	}
	if err := VerifyRequest(encodedPublicKey, signature, "POST", "/api/ai-client-connectors/heartbeat", []byte("changed"), now.Unix(), "nonce-1", now); !errors.Is(err, ErrInvalidSignature) {
		t.Fatalf("expected invalid signature, got %v", err)
	}
	if err := VerifyRequest(encodedPublicKey, signature, "POST", "/api/ai-client-connectors/heartbeat", body, now.Unix(), "nonce-1", now.Add(61*time.Second)); !errors.Is(err, ErrStaleRequest) {
		t.Fatalf("expected stale request, got %v", err)
	}
}
