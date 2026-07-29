package connections

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"
)

const CredentialBundleSchemaV1 = "oauth_bundle_v1"

const (
	AuthModeAPIKey      = "api_key"
	AuthModeOAuthBearer = "oauth_bearer"
	AuthModeCodexOAuth  = "codex_oauth"
	AuthModeDeepSeekWeb = "deepseek_web_token"
)

var (
	ErrAuthorizationNotFound = errors.New("authorization transaction was not found")
	ErrAuthorizationExpired  = errors.New("authorization transaction expired")
	ErrAuthorizationBinding  = errors.New("authorization transaction binding mismatch")
)

var (
	authorizationIDPattern = regexp.MustCompile(`^authz_[A-Za-z0-9-]{16,80}$`)
	internalDNSNamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,62}$`)
)

type OAuthProof struct {
	State         string
	Nonce         string
	CodeVerifier  string
	CodeChallenge string
}

type CredentialBundle struct {
	Schema          string    `json:"schema"`
	AccessToken     string    `json:"accessToken"`
	RefreshToken    string    `json:"refreshToken,omitempty"`
	IDToken         string    `json:"idToken,omitempty"`
	TokenType       string    `json:"tokenType"`
	Scopes          []string  `json:"scopes,omitempty"`
	ExpiresAt       time.Time `json:"expiresAt"`
	ProviderSubject string    `json:"providerSubject"`
	AccountID       string    `json:"accountId,omitempty"`
	ProjectID       string    `json:"projectId,omitempty"`
}

func (b CredentialBundle) Marshal() (string, error) {
	if strings.TrimSpace(b.Schema) == "" {
		b.Schema = CredentialBundleSchemaV1
	}
	if b.Schema != CredentialBundleSchemaV1 {
		return "", fmt.Errorf("unsupported credential bundle schema %q", b.Schema)
	}
	if strings.TrimSpace(b.AccessToken) == "" || strings.TrimSpace(b.ProviderSubject) == "" {
		return "", fmt.Errorf("credential bundle is incomplete")
	}
	raw, err := json.Marshal(b)
	return string(raw), err
}

func ParseCredentialBundle(raw string) (CredentialBundle, error) {
	var bundle CredentialBundle
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&bundle); err != nil {
		return CredentialBundle{}, fmt.Errorf("invalid credential bundle: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return CredentialBundle{}, fmt.Errorf("invalid credential bundle: trailing data")
	}
	if bundle.Schema != CredentialBundleSchemaV1 {
		return CredentialBundle{}, fmt.Errorf("unsupported credential bundle schema %q", bundle.Schema)
	}
	if strings.TrimSpace(bundle.AccessToken) == "" || strings.TrimSpace(bundle.ProviderSubject) == "" {
		return CredentialBundle{}, fmt.Errorf("credential bundle is incomplete")
	}
	return bundle, nil
}

func SameCredentialIdentity(current CredentialBundle, replacement CredentialBundle) bool {
	currentSubject := strings.TrimSpace(current.ProviderSubject)
	replacementSubject := strings.TrimSpace(replacement.ProviderSubject)
	if currentSubject == "" || replacementSubject == "" || currentSubject != replacementSubject {
		return false
	}
	currentAccount := strings.TrimSpace(current.AccountID)
	replacementAccount := strings.TrimSpace(replacement.AccountID)
	if currentAccount != "" || replacementAccount != "" {
		return currentAccount != "" && currentAccount == replacementAccount
	}
	return true
}

type AccountProfile struct {
	Subject     string
	AccountID   string
	DisplayName string
	EmailMask   string
}

type AuthMaterial struct {
	Mode      string
	Endpoint  string
	Headers   http.Header
	ExpiresAt time.Time
}

var trustedAuthHeaders = map[string]bool{
	"Authorization":       true,
	"X-Api-Key":           true,
	"X-Goog-Api-Key":      true,
	"X-Goog-User-Project": true,
	"Chatgpt-Account-Id":  true,
	"Openai-Beta":         true,
	"Originator":          true,
	"User-Agent":          true,
	"Version":             true,
}

func (m AuthMaterial) Validate() error {
	switch m.Mode {
	case AuthModeAPIKey, AuthModeOAuthBearer, AuthModeCodexOAuth, AuthModeDeepSeekWeb:
	default:
		return fmt.Errorf("unsupported auth material mode %q", m.Mode)
	}
	if strings.TrimSpace(m.Endpoint) != "" {
		parsed, err := url.Parse(m.Endpoint)
		if err != nil || parsed.Hostname() == "" || parsed.User != nil ||
			(parsed.Scheme != "https" && (m.Mode != AuthModeDeepSeekWeb || !safeInternalHTTPEndpoint(parsed))) {
			return fmt.Errorf("auth material endpoint is invalid")
		}
	}
	for name, values := range m.Headers {
		canonical := http.CanonicalHeaderKey(name)
		if !trustedAuthHeaders[canonical] || len(values) != 1 || strings.ContainsAny(values[0], "\r\n") {
			return fmt.Errorf("auth material header %q is not allowed", name)
		}
	}
	return nil
}

func safeInternalHTTPEndpoint(parsed *url.URL) bool {
	if parsed == nil || parsed.Scheme != "http" || parsed.Hostname() == "" ||
		parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" ||
		(parsed.Path != "" && parsed.Path != "/") {
		return false
	}
	host := strings.ToLower(strings.TrimSuffix(parsed.Hostname(), "."))
	if ip := net.ParseIP(host); ip != nil {
		return ip.IsLoopback() || ip.IsPrivate()
	}
	if host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return true
	}
	return !strings.Contains(host, ".") && internalDNSNamePattern.MatchString(host)
}

type AuthorizationTransaction struct {
	ID               string    `json:"id"`
	UserID           string    `json:"userId"`
	OrgID            string    `json:"orgId"`
	SessionHash      string    `json:"sessionHash"`
	Provider         string    `json:"provider"`
	Method           string    `json:"method"`
	State            string    `json:"state"`
	CodeVerifier     string    `json:"codeVerifier"`
	Nonce            string    `json:"nonce"`
	RedirectURI      string    `json:"redirectUri"`
	DisplayName      string    `json:"displayName"`
	ProjectID        string    `json:"projectId,omitempty"`
	Models           []string  `json:"models"`
	TermsVersion     string    `json:"termsVersion"`
	ExistingID       string    `json:"existingId,omitempty"`
	CreatedAt        time.Time `json:"createdAt"`
	ExpiresAt        time.Time `json:"expiresAt"`
	AuthorizationURL string    `json:"-"`
}

type StepUpGrant struct {
	Token       string    `json:"token"`
	UserID      string    `json:"userId"`
	SessionHash string    `json:"sessionHash"`
	ExpiresAt   time.Time `json:"expiresAt"`
}

type AuthorizationStore interface {
	Put(context.Context, AuthorizationTransaction) error
	Get(context.Context, string) (AuthorizationTransaction, error)
	Consume(context.Context, string, string, string) (AuthorizationTransaction, error)
	Delete(context.Context, string, string, string) error
	PutStepUp(context.Context, StepUpGrant) error
	ConsumeStepUp(context.Context, string, string, string) error
	AcquireRefreshLock(context.Context, string, time.Duration) (string, bool, error)
	ReleaseRefreshLock(context.Context, string, string) error
	Close() error
}

type MemoryAuthorizationStore struct {
	mu           sync.Mutex
	now          func() time.Time
	transactions map[string]AuthorizationTransaction
	grants       map[string]StepUpGrant
	refreshLocks map[string]memoryRefreshLock
}

type memoryRefreshLock struct {
	token     string
	expiresAt time.Time
}

func NewMemoryAuthorizationStore(now func() time.Time) *MemoryAuthorizationStore {
	if now == nil {
		now = time.Now
	}
	return &MemoryAuthorizationStore{
		now:          now,
		transactions: map[string]AuthorizationTransaction{},
		grants:       map[string]StepUpGrant{},
		refreshLocks: map[string]memoryRefreshLock{},
	}
}

func (s *MemoryAuthorizationStore) Put(_ context.Context, transaction AuthorizationTransaction) error {
	if transaction.ID == "" || transaction.UserID == "" || transaction.SessionHash == "" || transaction.ExpiresAt.IsZero() {
		return fmt.Errorf("authorization transaction is incomplete")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.transactions[transaction.ID] = transaction
	return nil
}

func (s *MemoryAuthorizationStore) Get(_ context.Context, id string) (AuthorizationTransaction, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	transaction, ok := s.transactions[id]
	if !ok {
		return AuthorizationTransaction{}, ErrAuthorizationNotFound
	}
	if !transaction.ExpiresAt.After(s.now()) {
		delete(s.transactions, id)
		return AuthorizationTransaction{}, ErrAuthorizationExpired
	}
	return transaction, nil
}

func (s *MemoryAuthorizationStore) Consume(_ context.Context, id string, userID string, sessionHash string) (AuthorizationTransaction, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	transaction, ok := s.transactions[id]
	if !ok {
		return AuthorizationTransaction{}, ErrAuthorizationNotFound
	}
	if !transaction.ExpiresAt.After(s.now()) {
		delete(s.transactions, id)
		return AuthorizationTransaction{}, ErrAuthorizationExpired
	}
	if !secureEqual(transaction.UserID, userID) || !secureEqual(transaction.SessionHash, sessionHash) {
		return AuthorizationTransaction{}, ErrAuthorizationBinding
	}
	delete(s.transactions, id)
	return transaction, nil
}

func (s *MemoryAuthorizationStore) Delete(_ context.Context, id string, userID string, sessionHash string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	transaction, ok := s.transactions[id]
	if !ok {
		return ErrAuthorizationNotFound
	}
	if !secureEqual(transaction.UserID, userID) || !secureEqual(transaction.SessionHash, sessionHash) {
		return ErrAuthorizationBinding
	}
	delete(s.transactions, id)
	return nil
}

func (s *MemoryAuthorizationStore) PutStepUp(_ context.Context, grant StepUpGrant) error {
	if grant.Token == "" || grant.UserID == "" || grant.SessionHash == "" || grant.ExpiresAt.IsZero() {
		return fmt.Errorf("step-up grant is incomplete")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.grants[grant.Token] = grant
	return nil
}

func (s *MemoryAuthorizationStore) ConsumeStepUp(_ context.Context, token string, userID string, sessionHash string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	grant, ok := s.grants[token]
	if !ok {
		return ErrAuthorizationNotFound
	}
	if !grant.ExpiresAt.After(s.now()) {
		delete(s.grants, token)
		return ErrAuthorizationExpired
	}
	if !secureEqual(grant.UserID, userID) || !secureEqual(grant.SessionHash, sessionHash) {
		return ErrAuthorizationBinding
	}
	delete(s.grants, token)
	return nil
}

func (s *MemoryAuthorizationStore) Close() error {
	return nil
}

func (s *MemoryAuthorizationStore) AcquireRefreshLock(_ context.Context, connectionID string, ttl time.Duration) (string, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if current, ok := s.refreshLocks[connectionID]; ok && current.expiresAt.After(s.now()) {
		return "", false, nil
	}
	token, err := GenerateOpaqueToken("lock_")
	if err != nil {
		return "", false, err
	}
	s.refreshLocks[connectionID] = memoryRefreshLock{token: token, expiresAt: s.now().Add(ttl)}
	return token, true, nil
}

func (s *MemoryAuthorizationStore) ReleaseRefreshLock(_ context.Context, connectionID string, token string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	current, ok := s.refreshLocks[connectionID]
	if ok && secureEqual(current.token, token) {
		delete(s.refreshLocks, connectionID)
	}
	return nil
}

func GenerateOAuthProof() (OAuthProof, error) {
	state, err := randomBase64URL(32)
	if err != nil {
		return OAuthProof{}, err
	}
	nonce, err := randomBase64URL(32)
	if err != nil {
		return OAuthProof{}, err
	}
	verifier, err := randomBase64URL(64)
	if err != nil {
		return OAuthProof{}, err
	}
	challengeRaw := sha256.Sum256([]byte(verifier))
	return OAuthProof{
		State:         state,
		Nonce:         nonce,
		CodeVerifier:  verifier,
		CodeChallenge: base64.RawURLEncoding.EncodeToString(challengeRaw[:]),
	}, nil
}

func GenerateOpaqueToken(prefix string) (string, error) {
	value, err := randomBase64URL(32)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(prefix) + value, nil
}

func ParseCodexCallback(raw string) (string, string, error) {
	if len(raw) == 0 || len(raw) > 8192 {
		return "", "", fmt.Errorf("callback URL length is invalid")
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme != "http" || parsed.Host != "localhost:1455" || parsed.Path != "/auth/callback" || parsed.Fragment != "" || parsed.User != nil {
		return "", "", fmt.Errorf("callback URL is not the fixed Codex loopback URL")
	}
	query := parsed.Query()
	if len(query["code"]) != 1 || len(query["state"]) != 1 {
		return "", "", fmt.Errorf("callback URL must contain one code and one state")
	}
	code := strings.TrimSpace(query.Get("code"))
	state := strings.TrimSpace(query.Get("state"))
	if code == "" || state == "" {
		return "", "", fmt.Errorf("callback URL code and state are required")
	}
	return code, state, nil
}

func SecureStateEqual(expected string, actual string) bool {
	return secureEqual(expected, actual)
}

func AuthorizationIDFromState(state string) (string, error) {
	id, _, ok := strings.Cut(strings.TrimSpace(state), ".")
	if !ok || !authorizationIDPattern.MatchString(id) {
		return "", fmt.Errorf("authorization state is invalid")
	}
	return id, nil
}

func randomBase64URL(size int) (string, error) {
	raw := make([]byte, size)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func secureEqual(left string, right string) bool {
	if len(left) != len(right) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(left), []byte(right)) == 1
}
