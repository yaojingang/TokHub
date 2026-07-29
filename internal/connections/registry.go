package connections

import (
	"fmt"
	"regexp"
	"strings"
)

type ProviderRegion struct {
	Code        string `json:"code"`
	Name        string `json:"name"`
	Endpoint    string `json:"endpoint,omitempty"`
	WorkspaceID bool   `json:"workspaceId"`
}

type ProviderManifest struct {
	Code              string               `json:"code"`
	Name              string               `json:"name"`
	ProductLine       string               `json:"productLine"`
	Protocol          string               `json:"protocol"`
	Type              string               `json:"type"`
	AuthMethod        string               `json:"authMethod"`
	CredentialLabel   string               `json:"credentialLabel"`
	DefaultRegion     string               `json:"defaultRegion"`
	Regions           []ProviderRegion     `json:"regions"`
	ValidationMode    string               `json:"validationMode"`
	GenerationKind    string               `json:"generationKind"`
	RecommendedModels []string             `json:"recommendedModels"`
	DocsURL           string               `json:"docsUrl"`
	AuthMethods       []AuthMethodManifest `json:"authMethods"`
}

type ResolveProviderInput struct {
	Code        string
	Region      string
	WorkspaceID string
}

type ResolvedProvider struct {
	Manifest       ProviderManifest
	Region         string
	WorkspaceID    string
	Endpoint       string
	ProviderConfig map[string]any
}

var workspaceIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9-]{1,62}[A-Za-z0-9]$`)

func ProviderRegistry() []ProviderManifest {
	return []ProviderManifest{
		{
			Code: "openai", Name: "ChatGPT / OpenAI", ProductLine: "OpenAI Platform",
			Protocol: "openai", Type: "openai", AuthMethod: "api_key", CredentialLabel: "Project API Key",
			DefaultRegion: "global", Regions: []ProviderRegion{{Code: "global", Name: "Global", Endpoint: "https://api.openai.com/v1"}},
			ValidationMode: "models_then_generation", GenerationKind: "responses", RecommendedModels: []string{"gpt-5.5", "gpt-5.4-mini"},
			DocsURL: "https://platform.openai.com/docs/api-reference",
		},
		{
			Code: "gemini", Name: "Gemini", ProductLine: "Google AI Studio",
			Protocol: "gemini", Type: "gemini", AuthMethod: "api_key", CredentialLabel: "Authorization API Key",
			DefaultRegion: "global", Regions: []ProviderRegion{{Code: "global", Name: "Global", Endpoint: "https://generativelanguage.googleapis.com/v1beta"}},
			ValidationMode: "models_then_generation", GenerationKind: "chat", RecommendedModels: []string{"gemini-3.6-flash", "gemini-3.5-flash"},
			DocsURL: "https://ai.google.dev/gemini-api/docs/api-key",
		},
		{
			Code: "kimi", Name: "Kimi", ProductLine: "Moonshot Open Platform",
			Protocol: "openai_compatible", Type: "openai-compatible", AuthMethod: "api_key", CredentialLabel: "API Key",
			DefaultRegion: "cn", Regions: []ProviderRegion{
				{Code: "cn", Name: "中国大陆", Endpoint: "https://api.moonshot.cn/v1"},
				{Code: "global", Name: "International", Endpoint: "https://api.moonshot.ai/v1"},
			},
			ValidationMode: "models_then_generation", GenerationKind: "chat", RecommendedModels: []string{"kimi-k2.5"},
			DocsURL: "https://platform.moonshot.cn/docs",
		},
		{
			Code: "deepseek", Name: "DeepSeek", ProductLine: "DeepSeek Open Platform",
			Protocol: "openai_compatible", Type: "openai-compatible", AuthMethod: "api_key", CredentialLabel: "API Key",
			DefaultRegion: "global", Regions: []ProviderRegion{{Code: "global", Name: "Global", Endpoint: "https://api.deepseek.com"}},
			ValidationMode: "generation", GenerationKind: "chat", RecommendedModels: []string{"deepseek-v4-flash", "deepseek-v4-pro"},
			DocsURL: "https://api-docs.deepseek.com/",
		},
		{
			Code: "doubao", Name: "豆包", ProductLine: "火山方舟按量调用",
			Protocol: "openai_compatible", Type: "openai-compatible", AuthMethod: "api_key", CredentialLabel: "ARK API Key",
			DefaultRegion: "cn-beijing", Regions: []ProviderRegion{{Code: "cn-beijing", Name: "华北 2（北京）", Endpoint: "https://ark.cn-beijing.volces.com/api/v3"}},
			ValidationMode: "generation", GenerationKind: "responses", RecommendedModels: []string{"doubao-seed-2-0-lite-260215"},
			DocsURL: "https://www.volcengine.com/docs/82379/1795150",
		},
		{
			Code: "claude", Name: "Claude", ProductLine: "Anthropic Console",
			Protocol: "anthropic", Type: "anthropic", AuthMethod: "api_key", CredentialLabel: "API Key",
			DefaultRegion: "global", Regions: []ProviderRegion{{Code: "global", Name: "Global", Endpoint: "https://api.anthropic.com/v1"}},
			ValidationMode: "generation", GenerationKind: "chat", RecommendedModels: []string{"claude-opus-4-8", "claude-sonnet-5"},
			DocsURL: "https://docs.anthropic.com/en/api/messages",
		},
		{
			Code: "qwen", Name: "千问", ProductLine: "阿里云百炼按量付费",
			Protocol: "openai_compatible", Type: "openai-compatible", AuthMethod: "api_key", CredentialLabel: "Model Studio API Key",
			DefaultRegion: "cn-beijing", Regions: []ProviderRegion{
				{Code: "cn-beijing", Name: "华北 2（北京）", Endpoint: "https://dashscope.aliyuncs.com/compatible-mode/v1", WorkspaceID: true},
				{Code: "ap-southeast-1", Name: "新加坡", Endpoint: "https://dashscope-intl.aliyuncs.com/compatible-mode/v1", WorkspaceID: true},
				{Code: "us-east-1", Name: "美国（弗吉尼亚）", Endpoint: "https://dashscope-us.aliyuncs.com/compatible-mode/v1"},
				{Code: "ap-northeast-1", Name: "日本（东京）", WorkspaceID: true},
				{Code: "eu-central-1", Name: "德国（法兰克福）", WorkspaceID: true},
			},
			ValidationMode: "generation", GenerationKind: "chat", RecommendedModels: []string{"qwen3.7-plus", "qwen3.7-max"},
			DocsURL: "https://help.aliyun.com/en/model-studio/base-url",
		},
	}
}

func ResolveProvider(input ResolveProviderInput) (ResolvedProvider, error) {
	code := strings.ToLower(strings.TrimSpace(input.Code))
	var manifest ProviderManifest
	found := false
	for _, item := range ProviderRegistry() {
		if item.Code == code {
			manifest = item
			found = true
			break
		}
	}
	if !found {
		return ResolvedProvider{}, fmt.Errorf("unsupported provider %q", input.Code)
	}
	region := strings.ToLower(strings.TrimSpace(input.Region))
	if region == "" {
		region = manifest.DefaultRegion
	}
	var selected ProviderRegion
	found = false
	for _, item := range manifest.Regions {
		if item.Code == region {
			selected = item
			found = true
			break
		}
	}
	if !found {
		return ResolvedProvider{}, fmt.Errorf("unsupported region %q for %s", input.Region, manifest.Name)
	}
	workspaceID := strings.TrimSpace(input.WorkspaceID)
	endpoint := selected.Endpoint
	if code == "qwen" && workspaceID != "" {
		if !workspaceIDPattern.MatchString(workspaceID) {
			return ResolvedProvider{}, fmt.Errorf("workspaceId has an invalid format")
		}
		hostRegion := map[string]string{
			"cn-beijing":     "cn-beijing",
			"ap-southeast-1": "ap-southeast-1",
			"ap-northeast-1": "ap-northeast-1",
			"eu-central-1":   "eu-central-1",
		}[region]
		if hostRegion == "" {
			return ResolvedProvider{}, fmt.Errorf("workspace endpoint is unavailable for region %q", region)
		}
		endpoint = "https://" + workspaceID + "." + hostRegion + ".maas.aliyuncs.com/compatible-mode/v1"
	}
	if endpoint == "" {
		return ResolvedProvider{}, fmt.Errorf("workspaceId is required for region %q", region)
	}
	providerConfig := map[string]any{"connectionProvider": code, "productLine": manifest.ProductLine}
	if code == "deepseek" || code == "doubao" {
		providerConfig["pathMode"] = "direct"
	}
	return ResolvedProvider{
		Manifest:       manifest,
		Region:         region,
		WorkspaceID:    workspaceID,
		Endpoint:       endpoint,
		ProviderConfig: providerConfig,
	}, nil
}
