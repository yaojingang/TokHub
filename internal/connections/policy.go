package connections

import (
	"errors"
	"strings"
	"time"
)

const (
	ConnectionModeOfficialAPI    = "official_api"
	ConnectionModeOfficialOAuth  = "official_oauth"
	ConnectionModeOfficialClient = "official_client"
	ConnectionModeConsumerWeb    = "consumer_web"
	ConnectionModeBrowserLab     = "browser_lab"

	ProviderPolicyVersion = "provider-policy-2026-08-19"
)

var (
	ErrProviderPolicyDenied  = errors.New("provider connection mode is denied by policy")
	ErrProviderPolicyExpired = errors.New("provider policy review has expired")
	ErrProviderPolicyKilled  = errors.New("provider connection mode is disabled by kill switch")
	ErrProviderTermsChanged  = errors.New("provider terms digest differs from the reviewed digest")
)

// ProviderPolicy is intentionally compiled into the release. An operator may
// stop a mode through a kill switch, but cannot widen an allowed mode at
// runtime without a reviewed code change.
type ProviderPolicy struct {
	Provider               string   `json:"provider"`
	AllowedProductionModes []string `json:"allowedProductionModes"`
	AllowedLabModes        []string `json:"allowedLabModes"`
	TermsURL               string   `json:"termsUrl"`
	TermsDigest            string   `json:"termsDigest"`
	ReviewedAt             string   `json:"reviewedAt"`
	ReviewExpiresAt        string   `json:"reviewExpiresAt"`
	IdentityRequirement    string   `json:"identityRequirement"`
	KillSwitch             bool     `json:"killSwitch"`
}

func ProviderPolicies() []ProviderPolicy {
	return []ProviderPolicy{
		{
			Provider: "openai", AllowedProductionModes: []string{ConnectionModeOfficialAPI, ConnectionModeOfficialClient},
			AllowedLabModes: []string{ConnectionModeBrowserLab}, TermsURL: "https://openai.com/policies/terms-of-use/",
			TermsDigest: "c1184229e7814acc940adca0b5e9e2df828b303fe4029c139187063fc5bc7f75",
			ReviewedAt:  "2026-08-19", ReviewExpiresAt: "2026-11-17", IdentityRequirement: "provider_account",
		},
		{
			Provider: "gemini", AllowedProductionModes: []string{ConnectionModeOfficialAPI, ConnectionModeOfficialOAuth},
			AllowedLabModes: []string{ConnectionModeBrowserLab}, TermsURL: "https://github.com/google-gemini/gemini-cli/blob/main/docs/resources/tos-privacy.md",
			TermsDigest: "e0a6a6c5972820d34de8a79cff7072ccafdd53eb54ac304dee8963cd719e899c",
			ReviewedAt:  "2026-08-19", ReviewExpiresAt: "2026-11-17", IdentityRequirement: "provider_account",
		},
		{
			Provider: "deepseek", AllowedProductionModes: []string{ConnectionModeOfficialAPI},
			AllowedLabModes: []string{ConnectionModeBrowserLab, ConnectionModeConsumerWeb}, TermsURL: "https://cdn.deepseek.com/policies/en-US/deepseek-terms-of-use.html",
			TermsDigest: "4fc6b485ad042d51ad42a0bf57ddbe49e7afc4648b3b3a4b2446706390a61a48",
			ReviewedAt:  "2026-08-19", ReviewExpiresAt: "2026-11-17", IdentityRequirement: "provider_account",
		},
		{
			Provider: "grok", AllowedProductionModes: []string{ConnectionModeOfficialAPI, ConnectionModeOfficialClient},
			AllowedLabModes: nil, TermsURL: "https://x.ai/legal/acceptable-use-policy",
			TermsDigest: "3c902032e65649b00faf37fea43f61d3904b20b627ebccd03a719528fbad0f7c",
			ReviewedAt:  "2026-08-19", ReviewExpiresAt: "2026-11-17", IdentityRequirement: "device_or_provider_account",
		},
	}
}

func ProviderPolicyFor(provider string) (ProviderPolicy, bool) {
	provider = strings.ToLower(strings.TrimSpace(provider))
	for _, policy := range ProviderPolicies() {
		if policy.Provider == provider {
			return policy, true
		}
	}
	return ProviderPolicy{}, false
}

func ConnectionModeForAuthMethod(authMethod string) string {
	switch strings.ToLower(strings.TrimSpace(authMethod)) {
	case "api_key", "api_key_guided":
		return ConnectionModeOfficialAPI
	case "oauth":
		return ConnectionModeOfficialOAuth
	case "official_client":
		return ConnectionModeOfficialClient
	case "deepseek_web_token", "codex_oauth":
		return ConnectionModeConsumerWeb
	case "opencli_browser":
		return ConnectionModeBrowserLab
	default:
		return ""
	}
}

func (p ProviderPolicy) Allows(mode string, lab bool, now time.Time) error {
	mode = strings.ToLower(strings.TrimSpace(mode))
	// Provider kill switches govern delegated account modes. Official API keys
	// remain available so an incident in the local-client path cannot disable
	// the provider's supported developer API.
	if p.KillSwitch && mode != ConnectionModeOfficialAPI {
		return ErrProviderPolicyKilled
	}
	if mode == ConnectionModeOfficialClient || mode == ConnectionModeBrowserLab || mode == ConnectionModeConsumerWeb {
		expiresAt, err := time.Parse("2006-01-02", p.ReviewExpiresAt)
		if err != nil || !now.Before(expiresAt.Add(24*time.Hour)) {
			return ErrProviderPolicyExpired
		}
	}
	allowed := p.AllowedProductionModes
	if lab {
		allowed = append(append([]string{}, allowed...), p.AllowedLabModes...)
	}
	for _, candidate := range allowed {
		if candidate == mode {
			return nil
		}
	}
	return ErrProviderPolicyDenied
}

func ProviderModeAllowed(provider, authMethod string, lab bool, now time.Time, killed bool) error {
	policy, ok := ProviderPolicyFor(provider)
	if !ok {
		// Providers without consumer-account integrations remain API-key only.
		if ConnectionModeForAuthMethod(authMethod) == ConnectionModeOfficialAPI {
			return nil
		}
		return ErrProviderPolicyDenied
	}
	policy.KillSwitch = killed
	return policy.Allows(ConnectionModeForAuthMethod(authMethod), lab, now)
}

func ValidateProviderTermsDigest(provider, authMethod, observedDigest string) error {
	mode := ConnectionModeForAuthMethod(authMethod)
	if mode == ConnectionModeOfficialAPI || strings.TrimSpace(observedDigest) == "" {
		return nil
	}
	policy, ok := ProviderPolicyFor(provider)
	if !ok {
		return ErrProviderPolicyDenied
	}
	if !strings.EqualFold(strings.TrimSpace(observedDigest), policy.TermsDigest) {
		return ErrProviderTermsChanged
	}
	return nil
}
