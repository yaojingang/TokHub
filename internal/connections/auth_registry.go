package connections

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"
)

const ExperimentalBridgeAcknowledgement = "I_ACCEPT_CHATGPT_CODEX_EXPERIMENTAL_RISK"

type AuthMethodManifest struct {
	Code           string `json:"code"`
	Label          string `json:"label"`
	Release        string `json:"release"`
	SharingScope   string `json:"sharingScope"`
	CompletionMode string `json:"completionMode"`
	Enabled        bool   `json:"enabled"`
	Description    string `json:"description"`
	RiskNotice     string `json:"riskNotice,omitempty"`
	DocsURL        string `json:"docsUrl,omitempty"`
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
}

func NewAuthRegistry(cfg AdapterConfig) *AuthRegistry {
	registry := &AuthRegistry{adapters: map[string]AuthAdapter{}}
	if cfg.WebAuthEnabled && cfg.GeminiOAuthEnabled &&
		strings.TrimSpace(cfg.GoogleClientID) != "" && strings.TrimSpace(cfg.GoogleClientSecret) != "" &&
		strings.TrimSpace(cfg.PublicURL) != "" {
		registry.Register(NewGeminiOAuthAdapter(cfg))
	}
	if cfg.WebAuthEnabled && cfg.DeepSeekGuidedEnabled {
		registry.Register(NewDeepSeekGuidedAdapter())
	}
	if cfg.WebAuthEnabled && cfg.ChatGPTCodexExperimental &&
		strings.TrimSpace(cfg.ExperimentalBridgeAck) == ExperimentalBridgeAcknowledgement {
		registry.Register(NewChatGPTCodexAdapter(cfg))
	}
	return registry
}

func (r *AuthRegistry) Register(adapter AuthAdapter) {
	if r == nil || adapter == nil {
		return
	}
	r.adapters[authAdapterKey(adapter.Provider(), adapter.Method().Code)] = adapter
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
	items := []AuthMethodManifest{}
	for _, adapter := range r.adapters {
		if adapter.Provider() == provider {
			method := adapter.Method()
			method.Enabled = true
			items = append(items, method)
		}
	}
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
