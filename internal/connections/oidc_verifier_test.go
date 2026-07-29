package connections

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRemoteOIDCSignatureVerifierAcceptsOnlyValidRS256Signature(t *testing.T) {
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	var server *httptest.Server
	server = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/.well-known/openid-configuration":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"issuer": server.URL, "jwks_uri": server.URL + "/jwks",
			})
		case "/jwks":
			_ = json.NewEncoder(w).Encode(map[string]any{"keys": []map[string]any{{
				"kid": "key-1", "kty": "RSA", "use": "sig", "alg": "RS256",
				"n": base64.RawURLEncoding.EncodeToString(privateKey.PublicKey.N.Bytes()),
				"e": base64.RawURLEncoding.EncodeToString(big.NewInt(int64(privateKey.PublicKey.E)).Bytes()),
			}}})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	token := signedTestJWT(t, privateKey, "key-1", map[string]any{"sub": "subject"})
	verifier := &remoteOIDCSignatureVerifier{
		client: server.Client(),
		keys:   map[string]cachedOIDCKey{},
	}
	if err := verifier.Verify(context.Background(), token, server.URL); err != nil {
		t.Fatalf("Verify() error = %v", err)
	}
	parts := strings.Split(token, ".")
	tamperedPayload := base64.RawURLEncoding.EncodeToString([]byte(`{"sub":"attacker"}`))
	if err := verifier.Verify(context.Background(), parts[0]+"."+tamperedPayload+"."+parts[2], server.URL); err == nil {
		t.Fatal("Verify() accepted a token with a tampered payload")
	}
	if err := verifier.Verify(context.Background(), fakeJWT(map[string]any{"sub": "subject"}), server.URL); err == nil {
		t.Fatal("Verify() accepted an unsigned token")
	}
}

func signedTestJWT(t *testing.T, privateKey *rsa.PrivateKey, keyID string, claims map[string]any) string {
	t.Helper()
	header, _ := json.Marshal(map[string]any{"alg": "RS256", "typ": "JWT", "kid": keyID})
	payload, _ := json.Marshal(claims)
	unsigned := base64.RawURLEncoding.EncodeToString(header) + "." + base64.RawURLEncoding.EncodeToString(payload)
	digest := sha256.Sum256([]byte(unsigned))
	signature, err := rsa.SignPKCS1v15(rand.Reader, privateKey, crypto.SHA256, digest[:])
	if err != nil {
		t.Fatal(err)
	}
	return unsigned + "." + base64.RawURLEncoding.EncodeToString(signature)
}
