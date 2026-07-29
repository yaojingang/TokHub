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
		Description: "打开 DeepSeek 官方开放平台创建 API Key，返回 TokHub 后完成加密保存、验证和个人中转。",
		DocsURL:     "https://api-docs.deepseek.com/",
	}
}

func deepSeekConsumerLoginManifest() AuthMethodManifest {
	return AuthMethodManifest{
		Code: "consumer_web_login", Label: "登录 DeepSeek 消费者账号", Release: "unavailable",
		SharingScope: "personal", CompletionMode: "unavailable", Enabled: false,
		Description:       "DeepSeek 当前未公开消费者 OAuth 或网页会话委托接口。",
		UnavailableReason: "官方暂未开放消费者账号授权。TokHub 不读取 Cookie、Session、密码或验证码。",
		DocsURL:           "https://api-docs.deepseek.com/api/deepseek-api/",
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
