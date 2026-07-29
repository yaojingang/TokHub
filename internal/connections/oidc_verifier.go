package connections

import (
	"context"
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

type OIDCSignatureVerifier interface {
	Verify(context.Context, string, string) error
}

type cachedOIDCKey struct {
	key       *rsa.PublicKey
	expiresAt time.Time
}

type remoteOIDCSignatureVerifier struct {
	client *http.Client
	now    func() time.Time
	mu     sync.Mutex
	keys   map[string]cachedOIDCKey
}

func newOIDCSignatureVerifier(cfg AdapterConfig) OIDCSignatureVerifier {
	if cfg.OIDCSignatureVerifier != nil {
		return cfg.OIDCSignatureVerifier
	}
	return &remoteOIDCSignatureVerifier{
		client: cfg.client(),
		now:    cfg.now,
		keys:   map[string]cachedOIDCKey{},
	}
}

func (v *remoteOIDCSignatureVerifier) Verify(ctx context.Context, rawToken string, expectedIssuer string) error {
	parts := strings.Split(rawToken, ".")
	if len(parts) != 3 || parts[2] == "" {
		return fmt.Errorf("id token signature is missing")
	}
	headerRaw, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return fmt.Errorf("id token header is invalid")
	}
	var header struct {
		Algorithm string `json:"alg"`
		KeyID     string `json:"kid"`
	}
	if err := json.Unmarshal(headerRaw, &header); err != nil {
		return fmt.Errorf("id token header is invalid")
	}
	if header.Algorithm != "RS256" || strings.TrimSpace(header.KeyID) == "" {
		return fmt.Errorf("id token signing algorithm is not supported")
	}
	key, err := v.signingKey(ctx, expectedIssuer, header.KeyID)
	if err != nil {
		return err
	}
	signature, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return fmt.Errorf("id token signature is invalid")
	}
	digest := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	if err := rsa.VerifyPKCS1v15(key, crypto.SHA256, digest[:], signature); err != nil {
		return fmt.Errorf("id token signature verification failed")
	}
	return nil
}

func (v *remoteOIDCSignatureVerifier) signingKey(ctx context.Context, issuer string, keyID string) (*rsa.PublicKey, error) {
	cacheKey := issuer + "\x00" + keyID
	now := time.Now()
	if v.now != nil {
		now = v.now()
	}
	v.mu.Lock()
	if cached, ok := v.keys[cacheKey]; ok && cached.expiresAt.After(now) {
		v.mu.Unlock()
		return cached.key, nil
	}
	v.mu.Unlock()

	issuerURL, err := url.Parse(strings.TrimRight(strings.TrimSpace(issuer), "/"))
	if err != nil || issuerURL.Scheme != "https" || issuerURL.Host == "" {
		return nil, fmt.Errorf("OIDC issuer is invalid")
	}
	discoveryURL := issuerURL.String() + "/.well-known/openid-configuration"
	var discovery struct {
		Issuer  string `json:"issuer"`
		JWKSURL string `json:"jwks_uri"`
	}
	if err := v.readJSON(ctx, discoveryURL, 128<<10, &discovery); err != nil {
		return nil, fmt.Errorf("OIDC discovery failed: %w", err)
	}
	if strings.TrimRight(discovery.Issuer, "/") != strings.TrimRight(issuer, "/") {
		return nil, fmt.Errorf("OIDC discovery issuer is invalid")
	}
	jwksURL, err := url.Parse(discovery.JWKSURL)
	if err != nil || jwksURL.Scheme != "https" || jwksURL.Host == "" {
		return nil, fmt.Errorf("OIDC JWKS URL is invalid")
	}
	var document struct {
		Keys []struct {
			KeyID     string `json:"kid"`
			KeyType   string `json:"kty"`
			Use       string `json:"use"`
			Algorithm string `json:"alg"`
			Modulus   string `json:"n"`
			Exponent  string `json:"e"`
		} `json:"keys"`
	}
	if err := v.readJSON(ctx, jwksURL.String(), 512<<10, &document); err != nil {
		return nil, fmt.Errorf("OIDC JWKS fetch failed: %w", err)
	}
	for _, item := range document.Keys {
		if item.KeyID != keyID || item.KeyType != "RSA" ||
			(item.Use != "" && item.Use != "sig") ||
			(item.Algorithm != "" && item.Algorithm != "RS256") {
			continue
		}
		modulus, decodeErr := base64.RawURLEncoding.DecodeString(item.Modulus)
		if decodeErr != nil || len(modulus) < 256 {
			continue
		}
		exponentBytes, decodeErr := base64.RawURLEncoding.DecodeString(item.Exponent)
		if decodeErr != nil || len(exponentBytes) == 0 || len(exponentBytes) > 4 {
			continue
		}
		exponent := 0
		for _, value := range exponentBytes {
			exponent = exponent<<8 | int(value)
		}
		if exponent < 3 {
			continue
		}
		key := &rsa.PublicKey{N: new(big.Int).SetBytes(modulus), E: exponent}
		v.mu.Lock()
		v.keys[cacheKey] = cachedOIDCKey{key: key, expiresAt: now.Add(time.Hour)}
		v.mu.Unlock()
		return key, nil
	}
	return nil, fmt.Errorf("OIDC signing key was not found")
}

func (v *remoteOIDCSignatureVerifier) readJSON(ctx context.Context, endpoint string, limit int64, target any) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	request.Header.Set("Accept", "application/json")
	response, err := v.client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.Request == nil || response.Request.URL == nil || response.Request.URL.Scheme != "https" {
		return fmt.Errorf("OIDC endpoint did not use HTTPS")
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("OIDC endpoint returned status %d", response.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil {
		return err
	}
	if int64(len(body)) > limit {
		return fmt.Errorf("OIDC response exceeded the size limit")
	}
	if err := json.Unmarshal(body, target); err != nil {
		return err
	}
	return nil
}
