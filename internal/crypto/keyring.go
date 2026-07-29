package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"strings"
)

type CredentialKeyringConfig struct {
	ActiveEncryptionKeyID  string
	EncryptionKeys         map[string]string
	ActiveFingerprintKeyID string
	FingerprintKeys        map[string]string
}

type CredentialEnvelope struct {
	Ciphertext       string
	Nonce            string
	EncryptionKeyID  string
	Fingerprint      string
	FingerprintKeyID string
	Mask             string
	Algorithm        string
}

type CredentialKeyring struct {
	activeEncryptionKeyID  string
	encryptionKeys         map[string]cipher.AEAD
	activeFingerprintKeyID string
	fingerprintKeys        map[string][]byte
}

func NewCredentialKeyring(cfg CredentialKeyringConfig) (*CredentialKeyring, error) {
	activeEncryptionKeyID := strings.TrimSpace(cfg.ActiveEncryptionKeyID)
	activeFingerprintKeyID := strings.TrimSpace(cfg.ActiveFingerprintKeyID)
	if activeEncryptionKeyID == "" || activeFingerprintKeyID == "" {
		return nil, fmt.Errorf("active credential key ids are required")
	}
	encryptionKeys := make(map[string]cipher.AEAD, len(cfg.EncryptionKeys))
	encryptionKeyMaterial := make(map[[sha256.Size]byte]struct{}, len(cfg.EncryptionKeys))
	for id, secret := range cfg.EncryptionKeys {
		id = strings.TrimSpace(id)
		if id == "" || len(strings.TrimSpace(secret)) < 32 {
			return nil, fmt.Errorf("credential encryption key %q must be at least 32 bytes", id)
		}
		key := sha256.Sum256([]byte(secret))
		encryptionKeyMaterial[key] = struct{}{}
		block, err := aes.NewCipher(key[:])
		if err != nil {
			return nil, err
		}
		gcm, err := cipher.NewGCM(block)
		if err != nil {
			return nil, err
		}
		encryptionKeys[id] = gcm
	}
	if encryptionKeys[activeEncryptionKeyID] == nil {
		return nil, fmt.Errorf("active credential encryption key %q is unavailable", activeEncryptionKeyID)
	}
	fingerprintKeys := make(map[string][]byte, len(cfg.FingerprintKeys))
	for id, secret := range cfg.FingerprintKeys {
		id = strings.TrimSpace(id)
		if id == "" || len(strings.TrimSpace(secret)) < 32 {
			return nil, fmt.Errorf("credential fingerprint key %q must be at least 32 bytes", id)
		}
		key := sha256.Sum256([]byte(secret))
		if _, reused := encryptionKeyMaterial[key]; reused {
			return nil, fmt.Errorf("credential encryption and fingerprint keys must use different secret material")
		}
		fingerprintKeys[id] = key[:]
	}
	if len(fingerprintKeys[activeFingerprintKeyID]) == 0 {
		return nil, fmt.Errorf("active credential fingerprint key %q is unavailable", activeFingerprintKeyID)
	}
	return &CredentialKeyring{
		activeEncryptionKeyID:  activeEncryptionKeyID,
		encryptionKeys:         encryptionKeys,
		activeFingerprintKeyID: activeFingerprintKeyID,
		fingerprintKeys:        fingerprintKeys,
	}, nil
}

func (r *CredentialKeyring) Encrypt(ownerID string, provider string, plain string) (CredentialEnvelope, error) {
	return r.EncryptWithFingerprint(ownerID, provider, plain, plain)
}

func (r *CredentialKeyring) EncryptWithFingerprint(ownerID string, provider string, plain string, fingerprintSource string) (CredentialEnvelope, error) {
	plain = strings.TrimSpace(plain)
	if plain == "" {
		return CredentialEnvelope{}, fmt.Errorf("credential is required")
	}
	fingerprintSource = strings.TrimSpace(fingerprintSource)
	if fingerprintSource == "" {
		return CredentialEnvelope{}, fmt.Errorf("credential fingerprint source is required")
	}
	gcm := r.encryptionKeys[r.activeEncryptionKeyID]
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return CredentialEnvelope{}, err
	}
	context := credentialContext(ownerID, provider)
	ciphertext := gcm.Seal(nil, nonce, []byte(plain), []byte(context))
	mac := hmac.New(sha256.New, r.fingerprintKeys[r.activeFingerprintKeyID])
	_, _ = mac.Write([]byte(context))
	_, _ = mac.Write([]byte{0})
	_, _ = mac.Write([]byte(fingerprintSource))
	return CredentialEnvelope{
		Ciphertext:       base64.StdEncoding.EncodeToString(ciphertext),
		Nonce:            base64.StdEncoding.EncodeToString(nonce),
		EncryptionKeyID:  r.activeEncryptionKeyID,
		Fingerprint:      hex.EncodeToString(mac.Sum(nil))[:24],
		FingerprintKeyID: r.activeFingerprintKeyID,
		Mask:             MaskSecret(plain),
		Algorithm:        "aes-256-gcm",
	}, nil
}

func (r *CredentialKeyring) Decrypt(ownerID string, provider string, envelope CredentialEnvelope) (string, error) {
	gcm := r.encryptionKeys[strings.TrimSpace(envelope.EncryptionKeyID)]
	if gcm == nil {
		return "", fmt.Errorf("credential encryption key %q is unavailable", envelope.EncryptionKeyID)
	}
	ciphertext, err := base64.StdEncoding.DecodeString(envelope.Ciphertext)
	if err != nil {
		return "", err
	}
	nonce, err := base64.StdEncoding.DecodeString(envelope.Nonce)
	if err != nil {
		return "", err
	}
	plain, err := gcm.Open(nil, nonce, ciphertext, []byte(credentialContext(ownerID, provider)))
	if err != nil {
		return "", err
	}
	return string(plain), nil
}

func credentialContext(ownerID string, provider string) string {
	return strings.TrimSpace(ownerID) + "\x00" + strings.ToLower(strings.TrimSpace(provider))
}
