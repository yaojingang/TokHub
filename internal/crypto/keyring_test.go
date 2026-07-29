package crypto

import (
	"strings"
	"testing"
)

func TestCredentialKeyringEncryptsAndDecryptsForConnectionContext(t *testing.T) {
	ring, err := NewCredentialKeyring(CredentialKeyringConfig{
		ActiveEncryptionKeyID:  "enc-v2",
		EncryptionKeys:         map[string]string{"enc-v2": strings.Repeat("e", 32)},
		ActiveFingerprintKeyID: "fp-v1",
		FingerprintKeys:        map[string]string{"fp-v1": strings.Repeat("f", 32)},
	})
	if err != nil {
		t.Fatalf("NewCredentialKeyring() error = %v", err)
	}

	encrypted, err := ring.Encrypt("usr_1", "openai", "sk-live-private-value")
	if err != nil {
		t.Fatalf("Encrypt() error = %v", err)
	}
	if encrypted.EncryptionKeyID != "enc-v2" || encrypted.FingerprintKeyID != "fp-v1" {
		t.Fatalf("encrypted key ids = %q/%q", encrypted.EncryptionKeyID, encrypted.FingerprintKeyID)
	}
	if strings.Contains(encrypted.Ciphertext, "sk-live-private-value") || encrypted.Fingerprint == "" {
		t.Fatalf("encrypted envelope leaked plaintext or omitted fingerprint: %#v", encrypted)
	}

	plain, err := ring.Decrypt("usr_1", "openai", encrypted)
	if err != nil {
		t.Fatalf("Decrypt() error = %v", err)
	}
	if plain != "sk-live-private-value" {
		t.Fatalf("Decrypt() = %q", plain)
	}
	if _, err := ring.Decrypt("usr_2", "openai", encrypted); err == nil {
		t.Fatal("Decrypt() accepted a credential under a different owner")
	}
	if _, err := ring.Decrypt("usr_1", "gemini", encrypted); err == nil {
		t.Fatal("Decrypt() accepted a credential under a different provider")
	}

	rotated, err := NewCredentialKeyring(CredentialKeyringConfig{
		ActiveEncryptionKeyID: "enc-v3",
		EncryptionKeys: map[string]string{
			"enc-v2": strings.Repeat("e", 32),
			"enc-v3": strings.Repeat("n", 32),
		},
		ActiveFingerprintKeyID: "fp-v2",
		FingerprintKeys: map[string]string{
			"fp-v1": strings.Repeat("f", 32),
			"fp-v2": strings.Repeat("g", 32),
		},
	})
	if err != nil {
		t.Fatalf("rotated keyring error = %v", err)
	}
	plain, err = rotated.Decrypt("usr_1", "openai", encrypted)
	if err != nil || plain != "sk-live-private-value" {
		t.Fatalf("rotated keyring could not read an older envelope: plain=%q err=%v", plain, err)
	}
}

func TestCredentialKeyringRejectsSharedEncryptionAndFingerprintMaterial(t *testing.T) {
	shared := strings.Repeat("s", 32)
	_, err := NewCredentialKeyring(CredentialKeyringConfig{
		ActiveEncryptionKeyID:  "enc-v1",
		EncryptionKeys:         map[string]string{"enc-v1": shared},
		ActiveFingerprintKeyID: "fp-v1",
		FingerprintKeys:        map[string]string{"fp-v1": shared},
	})
	if err == nil {
		t.Fatal("NewCredentialKeyring() accepted shared encryption and fingerprint key material")
	}
}

func TestCredentialKeyringCanKeepStableSubjectFingerprintAcrossOAuthRefresh(t *testing.T) {
	ring, err := NewCredentialKeyring(CredentialKeyringConfig{
		ActiveEncryptionKeyID:  "enc-v1",
		EncryptionKeys:         map[string]string{"enc-v1": strings.Repeat("e", 32)},
		ActiveFingerprintKeyID: "fp-v1",
		FingerprintKeys:        map[string]string{"fp-v1": strings.Repeat("f", 32)},
	})
	if err != nil {
		t.Fatal(err)
	}
	first, err := ring.EncryptWithFingerprint("usr_1", "gemini", `{"accessToken":"first"}`, "oauth\x00google-subject")
	if err != nil {
		t.Fatal(err)
	}
	second, err := ring.EncryptWithFingerprint("usr_1", "gemini", `{"accessToken":"second"}`, "oauth\x00google-subject")
	if err != nil {
		t.Fatal(err)
	}
	if first.Fingerprint != second.Fingerprint {
		t.Fatalf("OAuth subject fingerprint changed across refresh: %q != %q", first.Fingerprint, second.Fingerprint)
	}
	if first.Ciphertext == second.Ciphertext {
		t.Fatal("OAuth refresh reused ciphertext")
	}
}
