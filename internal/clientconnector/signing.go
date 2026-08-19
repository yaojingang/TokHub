package clientconnector

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

const MaximumClockSkew = 60 * time.Second

var (
	ErrInvalidPublicKey = errors.New("invalid connector public key")
	ErrInvalidSignature = errors.New("invalid connector request signature")
	ErrStaleRequest     = errors.New("connector request timestamp is outside the allowed window")
)

func CanonicalRequest(method, path string, body []byte, timestamp int64, nonce string) string {
	hash := sha256.Sum256(body)
	return strings.Join([]string{
		strings.ToUpper(strings.TrimSpace(method)),
		path,
		hex.EncodeToString(hash[:]),
		strconv.FormatInt(timestamp, 10),
		strings.TrimSpace(nonce),
	}, "\n")
}

func SignRequest(privateKey ed25519.PrivateKey, method, path string, body []byte, timestamp int64, nonce string) (string, error) {
	if len(privateKey) != ed25519.PrivateKeySize {
		return "", ErrInvalidPublicKey
	}
	signature := ed25519.Sign(privateKey, []byte(CanonicalRequest(method, path, body, timestamp, nonce)))
	return base64.RawURLEncoding.EncodeToString(signature), nil
}

func VerifyRequest(publicKeyText, signatureText, method, path string, body []byte, timestamp int64, nonce string, now time.Time) error {
	if now.IsZero() {
		now = time.Now()
	}
	requestTime := time.Unix(timestamp, 0)
	if requestTime.Before(now.Add(-MaximumClockSkew)) || requestTime.After(now.Add(MaximumClockSkew)) {
		return ErrStaleRequest
	}
	publicKey, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(publicKeyText))
	if err != nil || len(publicKey) != ed25519.PublicKeySize {
		return ErrInvalidPublicKey
	}
	signature, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(signatureText))
	if err != nil || len(signature) != ed25519.SignatureSize {
		return ErrInvalidSignature
	}
	if strings.TrimSpace(nonce) == "" || len(nonce) > 128 {
		return fmt.Errorf("%w: invalid nonce", ErrInvalidSignature)
	}
	if !ed25519.Verify(ed25519.PublicKey(publicKey), []byte(CanonicalRequest(method, path, body, timestamp, nonce)), signature) {
		return ErrInvalidSignature
	}
	return nil
}
