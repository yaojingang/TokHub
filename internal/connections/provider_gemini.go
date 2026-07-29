package connections

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
)

const (
	defaultGoogleAuthorizeURL = "https://accounts.google.com/o/oauth2/v2/auth"
	defaultGoogleTokenURL     = "https://oauth2.googleapis.com/token"
	defaultGoogleRevokeURL    = "https://oauth2.googleapis.com/revoke"
	geminiOAuthScope          = "openid email https://www.googleapis.com/auth/cloud-platform"
	geminiAPIEndpoint         = "https://generativelanguage.googleapis.com/v1beta"
)

var googleCloudProjectIDPattern = regexp.MustCompile(`^[a-z][a-z0-9-]{4,28}[a-z0-9]$`)

type GeminiOAuthAdapter struct {
	cfg      AdapterConfig
	verifier OIDCSignatureVerifier
}

func NewGeminiOAuthAdapter(cfg AdapterConfig) *GeminiOAuthAdapter {
	if strings.TrimSpace(cfg.GoogleAuthorizeURL) == "" {
		cfg.GoogleAuthorizeURL = defaultGoogleAuthorizeURL
	}
	if strings.TrimSpace(cfg.GoogleTokenURL) == "" {
		cfg.GoogleTokenURL = defaultGoogleTokenURL
	}
	if strings.TrimSpace(cfg.GoogleRevokeURL) == "" {
		cfg.GoogleRevokeURL = defaultGoogleRevokeURL
	}
	return &GeminiOAuthAdapter{cfg: cfg, verifier: newOIDCSignatureVerifier(cfg)}
}

func (a *GeminiOAuthAdapter) Provider() string { return "gemini" }

func (a *GeminiOAuthAdapter) Method() AuthMethodManifest {
	return AuthMethodManifest{
		Code: "oauth", Label: "使用 Google 账号授权", Release: "stable",
		SharingScope: "personal", CompletionMode: "redirect_callback", Enabled: true,
		Description: "通过 Google 官方 OAuth 授权有权访问 Gemini API 的 Cloud 项目。",
		DocsURL:     "https://ai.google.dev/gemini-api/docs/oauth",
	}
}

func (a *GeminiOAuthAdapter) Start(_ context.Context, transaction AuthorizationTransaction, challenge string) (AuthorizationStart, error) {
	if strings.TrimSpace(a.cfg.GoogleClientID) == "" || strings.TrimSpace(a.cfg.GoogleClientSecret) == "" {
		return AuthorizationStart{}, ErrAdapterDisabled
	}
	if _, err := NormalizeGoogleCloudProjectID(firstNonEmpty(transaction.ProjectID, a.cfg.GoogleProjectID)); err != nil {
		return AuthorizationStart{}, err
	}
	values := url.Values{
		"response_type":          {"code"},
		"client_id":              {a.cfg.GoogleClientID},
		"redirect_uri":           {transaction.RedirectURI},
		"scope":                  {geminiOAuthScope},
		"state":                  {transaction.State},
		"nonce":                  {transaction.Nonce},
		"code_challenge":         {challenge},
		"code_challenge_method":  {"S256"},
		"access_type":            {"offline"},
		"include_granted_scopes": {"true"},
		"prompt":                 {"consent"},
	}
	return AuthorizationStart{
		AuthorizationURL: a.cfg.GoogleAuthorizeURL + "?" + values.Encode(),
		CompletionMode:   "redirect_callback",
	}, nil
}

func (a *GeminiOAuthAdapter) Exchange(ctx context.Context, transaction AuthorizationTransaction, code string) (CredentialBundle, AccountProfile, error) {
	token, err := exchangeOAuthForm(ctx, a.cfg.client(), a.cfg.GoogleTokenURL, url.Values{
		"grant_type":    {"authorization_code"},
		"client_id":     {a.cfg.GoogleClientID},
		"client_secret": {a.cfg.GoogleClientSecret},
		"redirect_uri":  {transaction.RedirectURI},
		"code":          {code},
		"code_verifier": {transaction.CodeVerifier},
	})
	if err != nil {
		return CredentialBundle{}, AccountProfile{}, err
	}
	if err := a.verifier.Verify(ctx, token.IDToken, "https://accounts.google.com"); err != nil {
		return CredentialBundle{}, AccountProfile{}, err
	}
	claims, err := parseOIDCClaims(token.IDToken, "https://accounts.google.com", a.cfg.GoogleClientID, transaction.Nonce, true, a.cfg.now())
	if err != nil {
		return CredentialBundle{}, AccountProfile{}, err
	}
	projectID, err := NormalizeGoogleCloudProjectID(firstNonEmpty(transaction.ProjectID, a.cfg.GoogleProjectID))
	if err != nil {
		return CredentialBundle{}, AccountProfile{}, err
	}
	bundle := CredentialBundle{
		Schema: CredentialBundleSchemaV1, AccessToken: token.AccessToken,
		RefreshToken: token.RefreshToken, IDToken: token.IDToken,
		TokenType: token.TokenType, Scopes: scopesFromToken(token.Scope),
		ExpiresAt: tokenExpiry(token, a.cfg.now()), ProviderSubject: claims.Subject,
		ProjectID: projectID,
	}
	return bundle, AccountProfile{
		Subject: claims.Subject, DisplayName: "Google 账号", EmailMask: maskEmail(claims.Email),
	}, nil
}

func (a *GeminiOAuthAdapter) Refresh(ctx context.Context, bundle CredentialBundle) (CredentialBundle, error) {
	if strings.TrimSpace(bundle.RefreshToken) == "" {
		return CredentialBundle{}, ErrCredentialReauth
	}
	token, err := exchangeOAuthForm(ctx, a.cfg.client(), a.cfg.GoogleTokenURL, url.Values{
		"grant_type":    {"refresh_token"},
		"client_id":     {a.cfg.GoogleClientID},
		"client_secret": {a.cfg.GoogleClientSecret},
		"refresh_token": {bundle.RefreshToken},
	})
	if err != nil {
		return CredentialBundle{}, err
	}
	bundle.AccessToken = token.AccessToken
	bundle.ExpiresAt = tokenExpiry(token, a.cfg.now())
	if token.RefreshToken != "" {
		bundle.RefreshToken = token.RefreshToken
	}
	if token.IDToken != "" {
		bundle.IDToken = token.IDToken
	}
	if token.Scope != "" {
		bundle.Scopes = scopesFromToken(token.Scope)
	}
	return bundle, nil
}

func (a *GeminiOAuthAdapter) Revoke(ctx context.Context, bundle CredentialBundle) error {
	token := bundle.RefreshToken
	if token == "" {
		token = bundle.AccessToken
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, a.cfg.GoogleRevokeURL, strings.NewReader(url.Values{"token": {token}}.Encode()))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response, err := a.cfg.client().Do(request)
	if err != nil {
		return ErrCredentialTemporary
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("%w: revoke status %d", ErrCredentialTemporary, response.StatusCode)
	}
	return nil
}

func (a *GeminiOAuthAdapter) ResolveAuthMaterial(_ context.Context, bundle CredentialBundle) (AuthMaterial, error) {
	projectID, err := NormalizeGoogleCloudProjectID(bundle.ProjectID)
	if strings.TrimSpace(bundle.AccessToken) == "" || err != nil {
		return AuthMaterial{}, ErrCredentialReauth
	}
	material := AuthMaterial{
		Mode: AuthModeOAuthBearer, Endpoint: geminiAPIEndpoint, ExpiresAt: bundle.ExpiresAt,
		Headers: http.Header{
			"Authorization":       {"Bearer " + bundle.AccessToken},
			"X-Goog-User-Project": {projectID},
		},
	}
	return material, material.Validate()
}

func NormalizeGoogleCloudProjectID(value string) (string, error) {
	projectID := strings.TrimSpace(value)
	if !googleCloudProjectIDPattern.MatchString(projectID) {
		return "", fmt.Errorf("Google Cloud project ID must contain 6-30 lowercase letters, digits, or hyphens, start with a letter, and end with a letter or digit")
	}
	return projectID, nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
