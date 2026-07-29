package connections

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

const (
	CodexOAuthClientID    = "app_EMoamEEZ73f0CkXaXp7hrann"
	CodexOAuthRedirectURI = "http://localhost:1455/auth/callback"
	CodexBridgeVersion    = "0.146.0"
	defaultOpenAIAuthURL  = "https://auth.openai.com/oauth/authorize"
	defaultOpenAITokenURL = "https://auth.openai.com/oauth/token"
	chatGPTCodexEndpoint  = "https://chatgpt.com/backend-api/codex"
	codexOAuthScope       = "openid profile email offline_access api.connectors.read api.connectors.invoke"
	codexBridgeUserAgent  = "codex_cli_rs/" + CodexBridgeVersion + " (Ubuntu 22.4.0; x86_64) xterm-256color"
)

type ChatGPTCodexAdapter struct {
	cfg      AdapterConfig
	verifier OIDCSignatureVerifier
}

func NewChatGPTCodexAdapter(cfg AdapterConfig) *ChatGPTCodexAdapter {
	if strings.TrimSpace(cfg.OpenAIAuthorizeURL) == "" {
		cfg.OpenAIAuthorizeURL = defaultOpenAIAuthURL
	}
	if strings.TrimSpace(cfg.OpenAITokenURL) == "" {
		cfg.OpenAITokenURL = defaultOpenAITokenURL
	}
	return &ChatGPTCodexAdapter{cfg: cfg, verifier: newOIDCSignatureVerifier(cfg)}
}

func (a *ChatGPTCodexAdapter) enabled() bool {
	return a.cfg.WebAuthEnabled && a.cfg.ChatGPTCodexExperimental &&
		strings.TrimSpace(a.cfg.ExperimentalBridgeAck) == ExperimentalBridgeAcknowledgement
}

func (a *ChatGPTCodexAdapter) Provider() string { return "openai" }

func (a *ChatGPTCodexAdapter) Method() AuthMethodManifest {
	return AuthMethodManifest{
		Code: "codex_oauth", Label: "登录 ChatGPT", Release: "experimental",
		SharingScope: "personal", CompletionMode: "paste_callback", Enabled: a.enabled(),
		Description: "使用 Codex OAuth 登录后，仅为当前用户创建受严格限额的实验个人中转。",
		RiskNotice:  "依赖 ChatGPT Codex 私有接口，接口与条款可能变化，部署端可随时关闭。",
		DocsURL:     "https://github.com/openai/codex",
	}
}

func (a *ChatGPTCodexAdapter) Start(_ context.Context, transaction AuthorizationTransaction, challenge string) (AuthorizationStart, error) {
	if !a.enabled() {
		return AuthorizationStart{}, ErrAdapterDisabled
	}
	values := url.Values{
		"response_type":              {"code"},
		"client_id":                  {CodexOAuthClientID},
		"redirect_uri":               {CodexOAuthRedirectURI},
		"scope":                      {codexOAuthScope},
		"state":                      {transaction.State},
		"code_challenge":             {challenge},
		"code_challenge_method":      {"S256"},
		"id_token_add_organizations": {"true"},
		"codex_cli_simplified_flow":  {"true"},
		"originator":                 {"codex_cli_rs"},
	}
	return AuthorizationStart{
		AuthorizationURL: a.cfg.OpenAIAuthorizeURL + "?" + values.Encode(),
		CompletionMode:   "paste_callback",
	}, nil
}

func (a *ChatGPTCodexAdapter) Exchange(ctx context.Context, transaction AuthorizationTransaction, code string) (CredentialBundle, AccountProfile, error) {
	if !a.enabled() {
		return CredentialBundle{}, AccountProfile{}, ErrAdapterDisabled
	}
	token, err := exchangeOAuthForm(ctx, a.cfg.client(), a.cfg.OpenAITokenURL, url.Values{
		"grant_type":    {"authorization_code"},
		"client_id":     {CodexOAuthClientID},
		"redirect_uri":  {CodexOAuthRedirectURI},
		"code":          {code},
		"code_verifier": {transaction.CodeVerifier},
	})
	if err != nil {
		return CredentialBundle{}, AccountProfile{}, err
	}
	if err := a.verifier.Verify(ctx, token.IDToken, "https://auth.openai.com"); err != nil {
		return CredentialBundle{}, AccountProfile{}, err
	}
	claims, err := parseOIDCClaims(token.IDToken, "https://auth.openai.com", CodexOAuthClientID, "", false, a.cfg.now())
	if err != nil {
		return CredentialBundle{}, AccountProfile{}, err
	}
	accountID := nestedStringClaim(claims.Raw, "https://api.openai.com/auth", "chatgpt_account_id")
	if accountID == "" {
		accountID = stringClaim(claims.Raw, "chatgpt_account_id")
	}
	if accountID == "" {
		return CredentialBundle{}, AccountProfile{}, fmt.Errorf("ChatGPT account ID is missing")
	}
	bundle := CredentialBundle{
		Schema: CredentialBundleSchemaV1, AccessToken: token.AccessToken,
		RefreshToken: token.RefreshToken, IDToken: token.IDToken, TokenType: token.TokenType,
		Scopes: scopesFromToken(token.Scope), ExpiresAt: tokenExpiry(token, a.cfg.now()),
		ProviderSubject: claims.Subject, AccountID: accountID,
	}
	return bundle, AccountProfile{
		Subject: claims.Subject, AccountID: accountID, DisplayName: "ChatGPT 账号",
		EmailMask: maskEmail(claims.Email),
	}, nil
}

func (a *ChatGPTCodexAdapter) Refresh(ctx context.Context, bundle CredentialBundle) (CredentialBundle, error) {
	if !a.enabled() {
		return CredentialBundle{}, ErrAdapterDisabled
	}
	if strings.TrimSpace(bundle.RefreshToken) == "" {
		return CredentialBundle{}, ErrCredentialReauth
	}
	token, err := exchangeOAuthJSON(ctx, a.cfg.client(), a.cfg.OpenAITokenURL, map[string]string{
		"grant_type":    "refresh_token",
		"client_id":     CodexOAuthClientID,
		"refresh_token": bundle.RefreshToken,
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
	return bundle, nil
}

func (a *ChatGPTCodexAdapter) Revoke(context.Context, CredentialBundle) error {
	return ErrCredentialUnsupported
}

func (a *ChatGPTCodexAdapter) ResolveAuthMaterial(_ context.Context, bundle CredentialBundle) (AuthMaterial, error) {
	if !a.enabled() {
		return AuthMaterial{}, ErrAdapterDisabled
	}
	if strings.TrimSpace(bundle.AccessToken) == "" || strings.TrimSpace(bundle.AccountID) == "" {
		return AuthMaterial{}, ErrCredentialReauth
	}
	headers := make(http.Header)
	headers.Set("Authorization", "Bearer "+bundle.AccessToken)
	headers.Set("ChatGPT-Account-Id", bundle.AccountID)
	headers.Set("OpenAI-Beta", "responses=experimental")
	headers.Set("Originator", "codex_cli_rs")
	headers.Set("User-Agent", codexBridgeUserAgent)
	headers.Set("Version", CodexBridgeVersion)
	material := AuthMaterial{
		Mode: AuthModeCodexOAuth, Endpoint: chatGPTCodexEndpoint, ExpiresAt: bundle.ExpiresAt,
		Headers: headers,
	}
	return material, material.Validate()
}
