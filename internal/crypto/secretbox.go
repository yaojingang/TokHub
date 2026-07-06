package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"strings"
)

type SecretBox struct {
	gcm cipher.AEAD
}

type EncryptedSecret struct {
	Ciphertext  string
	Nonce       string
	Fingerprint string
	Mask        string
}

func NewSecretBox(masterKey string) (*SecretBox, error) {
	if len(strings.TrimSpace(masterKey)) < 32 {
		return nil, fmt.Errorf("master key must be at least 32 bytes")
	}
	key := sha256.Sum256([]byte(masterKey))
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &SecretBox{gcm: gcm}, nil
}

func (b *SecretBox) Encrypt(plain string) (EncryptedSecret, error) {
	plain = strings.TrimSpace(plain)
	if plain == "" {
		return EncryptedSecret{}, fmt.Errorf("secret is required")
	}
	nonce := make([]byte, b.gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return EncryptedSecret{}, err
	}
	ciphertext := b.gcm.Seal(nil, nonce, []byte(plain), nil)
	sum := sha256.Sum256([]byte(plain))
	return EncryptedSecret{
		Ciphertext:  base64.StdEncoding.EncodeToString(ciphertext),
		Nonce:       base64.StdEncoding.EncodeToString(nonce),
		Fingerprint: hex.EncodeToString(sum[:])[:16],
		Mask:        MaskSecret(plain),
	}, nil
}

func (b *SecretBox) Decrypt(ciphertext string, nonceText string) (string, error) {
	raw, err := base64.StdEncoding.DecodeString(ciphertext)
	if err != nil {
		return "", err
	}
	nonce, err := base64.StdEncoding.DecodeString(nonceText)
	if err != nil {
		return "", err
	}
	plain, err := b.gcm.Open(nil, nonce, raw, nil)
	if err != nil {
		return "", err
	}
	return string(plain), nil
}

func MaskSecret(secret string) string {
	secret = strings.TrimSpace(secret)
	if len(secret) <= 10 {
		return "****" + suffix(secret, 4)
	}
	return secret[:min(6, len(secret))] + "****" + suffix(secret, 4)
}

func suffix(value string, n int) string {
	if len(value) <= n {
		return value
	}
	return value[len(value)-n:]
}
