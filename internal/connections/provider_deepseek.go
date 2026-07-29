package connections

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	DeepSeekAPIKeysURL        = "https://platform.deepseek.com/api_keys"
	DeepSeekChatURL           = "https://chat.deepseek.com"
	DeepSeekWebTermsVersion   = "deepseek-web-session-experimental-v1"
	deepSeekWebAdapterVersion = "ds2api-v4.6.1"
)

var deepSeekWebTokenPattern = regexp.MustCompile(`^[A-Za-z0-9._~+/=-]+$`)

type DeepSeekGuidedAdapter struct{}

func NewDeepSeekGuidedAdapter() *DeepSeekGuidedAdapter {
	return &DeepSeekGuidedAdapter{}
}

func (a *DeepSeekGuidedAdapter) Provider() string { return "deepseek" }

func (a *DeepSeekGuidedAdapter) Method() AuthMethodManifest {
	return AuthMethodManifest{
		Code: "api_key_guided", Label: "前往 DeepSeek 开放平台", Release: "stable",
		SharingScope: "personal", CompletionMode: "guided_api_key", Enabled: true,
		Description: "打开 DeepSeek 官方开放平台创建 API Key，返回 TokHub 后完成加密保存、验证和个人中转。",
		DocsURL:     "https://api-docs.deepseek.com/",
	}
}

func (a *DeepSeekGuidedAdapter) Start(context.Context, AuthorizationTransaction, string) (AuthorizationStart, error) {
	return AuthorizationStart{AuthorizationURL: DeepSeekAPIKeysURL, CompletionMode: "guided_api_key"}, nil
}

func (a *DeepSeekGuidedAdapter) Exchange(context.Context, AuthorizationTransaction, string) (CredentialBundle, AccountProfile, error) {
	return CredentialBundle{}, AccountProfile{}, ErrCredentialUnsupported
}

func (a *DeepSeekGuidedAdapter) Refresh(context.Context, CredentialBundle) (CredentialBundle, error) {
	return CredentialBundle{}, ErrCredentialUnsupported
}

func (a *DeepSeekGuidedAdapter) Revoke(context.Context, CredentialBundle) error {
	return ErrCredentialUnsupported
}

func (a *DeepSeekGuidedAdapter) ResolveAuthMaterial(_ context.Context, bundle CredentialBundle) (AuthMaterial, error) {
	material := AuthMaterial{
		Mode: AuthModeAPIKey, Endpoint: "https://api.deepseek.com",
		Headers: http.Header{"Authorization": {"Bearer " + bundle.AccessToken}},
	}
	return material, material.Validate()
}

type DeepSeekWebAdapter struct {
	cfg AdapterConfig
}

func NewDeepSeekWebAdapter(cfg AdapterConfig) *DeepSeekWebAdapter {
	if strings.TrimSpace(cfg.DeepSeekWebBridgeURL) == "" {
		cfg.DeepSeekWebBridgeURL = DefaultDeepSeekWebBridgeURL
	}
	return &DeepSeekWebAdapter{cfg: cfg}
}

func (a *DeepSeekWebAdapter) enabled() bool {
	return a.cfg.WebAuthEnabled &&
		a.cfg.DeepSeekWebExperimental &&
		strings.TrimSpace(a.cfg.DeepSeekWebBridgeAck) == DeepSeekWebExperimentalAcknowledgement &&
		validDeepSeekWebBridgeURL(a.cfg.DeepSeekWebBridgeURL)
}

func (a *DeepSeekWebAdapter) Provider() string { return "deepseek" }

func (a *DeepSeekWebAdapter) Method() AuthMethodManifest {
	return AuthMethodManifest{
		Code: "deepseek_web_token", Label: "登录 DeepSeek 网页账号", Release: "experimental",
		SharingScope: "personal", CompletionMode: "paste_token", Enabled: a.enabled(),
		Description: "登录 DeepSeek 网页版后导入当前账号的 userToken，用于创建严格限额的个人中转。",
		RiskNotice:  "依赖 DeepSeek 网页私有协议和独立桥接服务。登录态可能失效，平台规则与接口可能变化。",
		DocsURL:     DeepSeekChatURL,
	}
}

func (a *DeepSeekWebAdapter) Start(ctx context.Context, _ AuthorizationTransaction, _ string) (AuthorizationStart, error) {
	if !a.enabled() {
		return AuthorizationStart{}, ErrAdapterDisabled
	}
	healthURL := strings.TrimRight(a.cfg.DeepSeekWebBridgeURL, "/") + "/healthz"
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, healthURL, nil)
	if err != nil {
		return AuthorizationStart{}, fmt.Errorf("%w: bridge health request is invalid", ErrCredentialTemporary)
	}
	response, err := a.cfg.client().Do(request)
	if err != nil {
		return AuthorizationStart{}, fmt.Errorf("%w: DeepSeek bridge is unavailable", ErrCredentialTemporary)
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 8<<10))
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return AuthorizationStart{}, fmt.Errorf("%w: DeepSeek bridge health status %d", ErrCredentialTemporary, response.StatusCode)
	}
	return AuthorizationStart{AuthorizationURL: DeepSeekChatURL, CompletionMode: "paste_token"}, nil
}

func (a *DeepSeekWebAdapter) Exchange(_ context.Context, _ AuthorizationTransaction, rawToken string) (CredentialBundle, AccountProfile, error) {
	if !a.enabled() {
		return CredentialBundle{}, AccountProfile{}, ErrAdapterDisabled
	}
	token, err := NormalizeDeepSeekWebToken(rawToken)
	if err != nil {
		return CredentialBundle{}, AccountProfile{}, err
	}
	subject, accountID, expiresAt := deepSeekWebTokenIdentity(token)
	if !expiresAt.IsZero() && !expiresAt.After(a.cfg.now().Add(time.Minute)) {
		return CredentialBundle{}, AccountProfile{}, ErrCredentialReauth
	}
	bundle := CredentialBundle{
		Schema: CredentialBundleSchemaV1, AccessToken: token, TokenType: "Bearer",
		ExpiresAt: expiresAt, ProviderSubject: subject, AccountID: accountID,
	}
	return bundle, AccountProfile{
		Subject: subject, AccountID: accountID,
		DisplayName: "DeepSeek 网页账号", EmailMask: "DeepSeek 网页账号",
	}, nil
}

func (a *DeepSeekWebAdapter) Refresh(context.Context, CredentialBundle) (CredentialBundle, error) {
	return CredentialBundle{}, ErrCredentialReauth
}

func (a *DeepSeekWebAdapter) Revoke(context.Context, CredentialBundle) error {
	return ErrCredentialUnsupported
}

func (a *DeepSeekWebAdapter) ResolveAuthMaterial(_ context.Context, bundle CredentialBundle) (AuthMaterial, error) {
	if !a.enabled() {
		return AuthMaterial{}, ErrAdapterDisabled
	}
	if strings.TrimSpace(bundle.AccessToken) == "" {
		return AuthMaterial{}, ErrCredentialReauth
	}
	material := AuthMaterial{
		Mode: AuthModeDeepSeekWeb, Endpoint: strings.TrimRight(a.cfg.DeepSeekWebBridgeURL, "/"),
		ExpiresAt: bundle.ExpiresAt,
		Headers:   http.Header{"Authorization": {"Bearer " + bundle.AccessToken}},
	}
	return material, material.Validate()
}

func NormalizeDeepSeekWebToken(raw string) (string, error) {
	token := strings.TrimSpace(raw)
	if len(token) >= 7 && strings.EqualFold(token[:7], "Bearer ") {
		token = strings.TrimSpace(token[7:])
	}
	if len(token) < 32 || len(token) > 8192 || !deepSeekWebTokenPattern.MatchString(token) {
		return "", fmt.Errorf("%w: DeepSeek userToken format is invalid", ErrCredentialReauth)
	}
	return token, nil
}

func deepSeekWebTokenIdentity(token string) (string, string, time.Time) {
	sum := sha256.Sum256([]byte(token))
	fallback := "token:" + hex.EncodeToString(sum[:16])
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return fallback, fallback, time.Time{}
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return fallback, fallback, time.Time{}
	}
	claims := map[string]any{}
	if json.Unmarshal(payload, &claims) != nil {
		return fallback, fallback, time.Time{}
	}
	subject := firstDeepSeekClaim(claims, "sub", "user_id", "userId", "id")
	if subject == "" {
		subject = fallback
	}
	accountID := firstDeepSeekClaim(claims, "user_id", "userId", "id", "sub")
	if accountID == "" {
		accountID = subject
	}
	var expiresAt time.Time
	switch value := claims["exp"].(type) {
	case float64:
		expiresAt = time.Unix(int64(value), 0)
	case json.Number:
		if unix, parseErr := value.Int64(); parseErr == nil {
			expiresAt = time.Unix(unix, 0)
		}
	case string:
		if unix, parseErr := strconv.ParseInt(value, 10, 64); parseErr == nil {
			expiresAt = time.Unix(unix, 0)
		}
	}
	return subject, accountID, expiresAt
}

func firstDeepSeekClaim(claims map[string]any, names ...string) string {
	for _, name := range names {
		switch value := claims[name].(type) {
		case string:
			if trimmed := strings.TrimSpace(value); trimmed != "" {
				return trimmed
			}
		case float64:
			return strconv.FormatInt(int64(value), 10)
		case json.Number:
			return value.String()
		}
	}
	return ""
}

func validDeepSeekWebBridgeURL(raw string) bool {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Hostname() == "" || parsed.User != nil ||
		parsed.RawQuery != "" || parsed.Fragment != "" ||
		(parsed.Path != "" && parsed.Path != "/") {
		return false
	}
	material := AuthMaterial{Mode: AuthModeDeepSeekWeb, Endpoint: strings.TrimRight(strings.TrimSpace(raw), "/")}
	return material.Validate() == nil
}

func DeepSeekWebAdapterVersion() string {
	return deepSeekWebAdapterVersion
}
