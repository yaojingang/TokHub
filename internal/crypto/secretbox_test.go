package crypto

import (
	"strings"
	"testing"
)

func TestSecretBoxEncryptDecryptAndMask(t *testing.T) {
	box, err := NewSecretBox("phase4-test-master-key-with-32-bytes")
	if err != nil {
		t.Fatalf("NewSecretBox() error = %v", err)
	}
	secret := "sk-test-private-channel-secret"
	encrypted, err := box.Encrypt(secret)
	if err != nil {
		t.Fatalf("Encrypt() error = %v", err)
	}
	if encrypted.Ciphertext == "" || encrypted.Nonce == "" || encrypted.Fingerprint == "" {
		t.Fatalf("encrypted secret missing fields: %+v", encrypted)
	}
	if strings.Contains(encrypted.Ciphertext, secret) || strings.Contains(encrypted.Nonce, secret) {
		t.Fatalf("encrypted payload leaked plaintext")
	}
	if encrypted.Mask == secret || !strings.Contains(encrypted.Mask, "****") {
		t.Fatalf("mask = %q, want redacted value", encrypted.Mask)
	}
	plain, err := box.Decrypt(encrypted.Ciphertext, encrypted.Nonce)
	if err != nil {
		t.Fatalf("Decrypt() error = %v", err)
	}
	if plain != secret {
		t.Fatalf("Decrypt() = %q, want original secret", plain)
	}
}

func TestNewSecretBoxRejectsShortKey(t *testing.T) {
	if _, err := NewSecretBox("too-short"); err == nil {
		t.Fatalf("NewSecretBox() with short key succeeded")
	}
}
