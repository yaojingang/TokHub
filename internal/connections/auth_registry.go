package connections

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

const (
	ExperimentalBridgeAcknowledgement      = "I_ACCEPT_CHATGPT_CODEX_EXPERIMENTAL_RISK"
	DeepSeekWebExperimentalAcknowledgement = "I_ACCEPT_DEEPSEEK_WEB_SESSION_EXPERIMENTAL_RISK"
	DefaultDeepSeekWebBridgeURL            = "http://deepseek-web-bridge:5001"
)

type AuthMethodManifest struct {
	Code              string `json:"code"`
	Label             string `json:"label"`
	Release           string `json:"release"`
	SharingScope      string `json:"sharingScope"`
	CompletionMode    string `json:"completionMode"`
	Enabled           bool   `json:"enabled"`
	Description       string `json:"description"`
	UnavailableReason string `json:"unavailableReason,omitempty"`
	RiskNotice        string `json:"riskNotice,omitempty"`
	DocsURL           string `json:"docsUrl,omitempty"`
}

type AuthorizationStart struct {
	AuthorizationURL string
	CompletionMode   string
}

type AuthAdapter interface {
	Provider() string
	Method() AuthMethodManifest
	Start(context.Context, AuthorizationTransaction, string) (AuthorizationStart, error)
	Exchange(context.Context, AuthorizationTransaction, string) (CredentialBundle, AccountProfile, error)
	Refresh(context.Context, CredentialBundle) (CredentialBundle, error)
	Revoke(context.Context, CredentialBundle) error
	ResolveAuthMaterial(context.Context, CredentialBundle) (AuthMaterial, error)
}

type AdapterConfig struct {
	WebAuthEnabled           bool
	GeminiOAuthEnabled       bool
	DeepSeekGuidedEnabled    bool
	DeepSeekWebExperimental  bool
	DeepSeekWebBridgeURL     string
	DeepSeekWebBridgeAck     string
	ChatGPTCodexExperimental bool
	ExperimentalBridgeAck    string
	PublicURL                string
	GoogleClientID           string
	GoogleClientSecret       string
	GoogleProjectID          string
	GoogleAuthorizeURL       string
	GoogleTokenURL           string
	GoogleRevokeURL          string
	OpenAIAuthorizeURL       string
	OpenAITokenURL           string
	HTTPClient               *http.Client
	OIDCSignatureVerifier    OIDCSignatureVerifier
	Now                      func() time.Time
}

func (c AdapterConfig) client() *http.Client {
	if c.HTTPClient != nil {
		return c.HTTPClient
	}
	return &http.Client{Timeout: 15 * time.Second}
}

func (c AdapterConfig) now() time.Time {
	if c.Now != nil {
		return c.Now()
	}
	return time.Now()
}

type AuthRegistry struct {
	adapters map[string]AuthAdapter
	methods  map[string][]AuthMethodManifest
}

func NewAuthRegistry(cfg AdapterConfig) *AuthRegistry {
	registry := &AuthRegistry{
		adapters: map[string]AuthAdapter{},
		methods:  map[string][]AuthMethodManifest{},
	}

	gemini := NewGeminiOAuthAdapter(cfg)
	registry.publish(gemini, geminiUnavailableReason(cfg))

	deepSeek := NewDeepSeekGuidedAdapter()
	registry.publish(deepSeek, deepSeekUnavailableReason(cfg))
	deepSeekWeb := NewDeepSeekWebAdapter(cfg)
	registry.publish(deepSeekWeb, deepSeekWebUnavailableReason(cfg))

	chatGPT := NewChatGPTCodexAdapter(cfg)
	registry.publish(chatGPT, chatGPTUnavailableReason(cfg))
	return registry
}

func (r *AuthRegistry) publish(adapter AuthAdapter, unavailableReason string) {
	if r == nil || adapter == nil {
		return
	}
	method := adapter.Method()
	method.Enabled = strings.TrimSpace(unavailableReason) == ""
	method.UnavailableReason = strings.TrimSpace(unavailableReason)
	r.catalog(adapter.Provider(), method)
	if method.Enabled {
		r.Register(adapter)
	}
}

func (r *AuthRegistry) Register(adapter AuthAdapter) {
	if r == nil || adapter == nil {
		return
	}
	if r.adapters == nil {
		r.adapters = map[string]AuthAdapter{}
	}
	r.adapters[authAdapterKey(adapter.Provider(), adapter.Method().Code)] = adapter
	method := adapter.Method()
	method.Enabled = true
	method.UnavailableReason = ""
	r.catalog(adapter.Provider(), method)
}

func (r *AuthRegistry) Adapter(provider string, method string) (AuthAdapter, bool) {
	if r == nil {
		return nil, false
	}
	adapter, ok := r.adapters[authAdapterKey(provider, method)]
	return adapter, ok
}

func (r *AuthRegistry) Methods(provider string) []AuthMethodManifest {
	if r == nil {
		return nil
	}
	provider = strings.ToLower(strings.TrimSpace(provider))
	items := append([]AuthMethodManifest(nil), r.methods[provider]...)
	sort.Slice(items, func(i, j int) bool { return items[i].Code < items[j].Code })
	return items
}

func (r *AuthRegistry) MustAdapter(provider string, method string) (AuthAdapter, error) {
	adapter, ok := r.Adapter(provider, method)
	if !ok {
		return nil, fmt.Errorf("authorization method %s/%s is disabled", provider, method)
	}
	return adapter, nil
}

func authAdapterKey(provider string, method string) string {
	return strings.ToLower(strings.TrimSpace(provider)) + "\x00" + strings.ToLower(strings.TrimSpace(method))
}

func (r *AuthRegistry) catalog(provider string, method AuthMethodManifest) {
	if r.methods == nil {
		r.methods = map[string][]AuthMethodManifest{}
	}
	provider = strings.ToLower(strings.TrimSpace(provider))
	for index := range r.methods[provider] {
		if strings.EqualFold(r.methods[provider][index].Code, method.Code) {
			r.methods[provider][index] = method
			return
		}
	}
	r.methods[provider] = append(r.methods[provider], method)
}

func geminiUnavailableReason(cfg AdapterConfig) string {
	switch {
	case !cfg.WebAuthEnabled:
		return "管理员尚未开启网页登录授权。"
	case !cfg.GeminiOAuthEnabled:
		return "管理员尚未开启 Gemini Google OAuth。"
	case strings.TrimSpace(cfg.PublicURL) == "":
		return "部署端需要配置公开回调地址。"
	case !validOAuthCallbackBaseURL(cfg.PublicURL):
		return "部署端需要配置 HTTPS 公共回调地址；本地开发仅允许 loopback HTTP。"
	case strings.TrimSpace(cfg.GoogleClientID) == "" || strings.TrimSpace(cfg.GoogleClientSecret) == "":
		return "部署端需要配置 Google OAuth Client ID 与 Secret。"
	default:
		return ""
	}
}

func validOAuthCallbackBaseURL(rawURL string) bool {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || parsed.Host == "" || parsed.Opaque != "" || parsed.User != nil ||
		(parsed.Path != "" && parsed.Path != "/") || parsed.RawQuery != "" || parsed.Fragment != "" {
		return false
	}
	switch strings.ToLower(parsed.Scheme) {
	case "https":
		return true
	case "http":
		host := strings.TrimSuffix(strings.ToLower(parsed.Hostname()), ".")
		if host == "localhost" {
			return true
		}
		ip := net.ParseIP(host)
		return ip != nil && ip.IsLoopback()
	default:
		return false
	}
}

func deepSeekUnavailableReason(cfg AdapterConfig) string {
	switch {
	case !cfg.WebAuthEnabled:
		return "管理员尚未开启网页登录授权。"
	case !cfg.DeepSeekGuidedEnabled:
		return "管理员尚未开启 DeepSeek 开放平台引导。"
	default:
		return ""
	}
}

func deepSeekWebUnavailableReason(cfg AdapterConfig) string {
	switch {
	case !cfg.WebAuthEnabled:
		return "管理员尚未开启网页登录授权。"
	case !cfg.DeepSeekWebExperimental:
		return "管理员尚未开启 DeepSeek 网页账号实验能力。"
	case strings.TrimSpace(cfg.DeepSeekWebBridgeAck) != DeepSeekWebExperimentalAcknowledgement:
		return "部署端尚未完成 DeepSeek 网页账号实验风险确认。"
	case !validDeepSeekWebBridgeURL(cfg.DeepSeekWebBridgeURL):
		return "部署端需要配置安全的 DeepSeek 网页协议桥地址。"
	default:
		return ""
	}
}

func chatGPTUnavailableReason(cfg AdapterConfig) string {
	switch {
	case !cfg.WebAuthEnabled:
		return "管理员尚未开启网页登录授权。"
	case !cfg.ChatGPTCodexExperimental:
		return "管理员尚未开启 ChatGPT 登录实验能力。"
	case strings.TrimSpace(cfg.ExperimentalBridgeAck) != ExperimentalBridgeAcknowledgement:
		return "部署端尚未完成 ChatGPT 实验风险部署确认。"
	default:
		return ""
	}
}
