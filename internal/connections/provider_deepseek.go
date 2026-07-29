package connections

import (
	"context"
	"net/http"
)

const DeepSeekAPIKeysURL = "https://platform.deepseek.com/api_keys"

type DeepSeekGuidedAdapter struct{}

func NewDeepSeekGuidedAdapter() *DeepSeekGuidedAdapter {
	return &DeepSeekGuidedAdapter{}
}

func (a *DeepSeekGuidedAdapter) Provider() string { return "deepseek" }

func (a *DeepSeekGuidedAdapter) Method() AuthMethodManifest {
	return AuthMethodManifest{
		Code: "api_key_guided", Label: "前往 DeepSeek 开放平台", Release: "stable",
		SharingScope: "personal", CompletionMode: "guided_api_key", Enabled: true,
		Description: "打开 DeepSeek 官方开放平台创建 API Key，返回 TokHub 后完成加密保存与验证。",
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
