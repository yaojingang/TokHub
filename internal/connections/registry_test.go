package connections

import "testing"

func TestProviderRegistryExposesSevenOfficialDeveloperProducts(t *testing.T) {
	items := ProviderRegistry()
	if len(items) != 7 {
		t.Fatalf("ProviderRegistry() returned %d providers, want 7", len(items))
	}
	want := map[string]bool{
		"openai": true, "gemini": true, "kimi": true, "deepseek": true,
		"doubao": true, "claude": true, "qwen": true,
	}
	for _, item := range items {
		if !want[item.Code] {
			t.Fatalf("unexpected provider code %q", item.Code)
		}
		if item.AuthMethod != "api_key" || item.ProductLine == "" || item.Protocol == "" {
			t.Fatalf("provider %q is missing its official product contract: %#v", item.Code, item)
		}
		delete(want, item.Code)
	}
	if len(want) != 0 {
		t.Fatalf("missing providers: %#v", want)
	}
}

func TestResolveProviderBuildsQwenWorkspaceEndpointFromAllowlistedRegion(t *testing.T) {
	resolved, err := ResolveProvider(ResolveProviderInput{
		Code:        "qwen",
		Region:      "cn-beijing",
		WorkspaceID: "llm-prod-92",
	})
	if err != nil {
		t.Fatalf("ResolveProvider() error = %v", err)
	}
	want := "https://llm-prod-92.cn-beijing.maas.aliyuncs.com/compatible-mode/v1"
	if resolved.Endpoint != want {
		t.Fatalf("ResolveProvider() endpoint = %q, want %q", resolved.Endpoint, want)
	}
}

func TestProviderRegistryAvoidsRetiredClaudeDefaults(t *testing.T) {
	for _, provider := range ProviderRegistry() {
		if provider.Code != "claude" {
			continue
		}
		want := []string{"claude-opus-4-8", "claude-sonnet-5"}
		if len(provider.RecommendedModels) != len(want) {
			t.Fatalf("Claude recommended models = %#v, want %#v", provider.RecommendedModels, want)
		}
		for index := range want {
			if provider.RecommendedModels[index] != want[index] {
				t.Fatalf("Claude recommended models = %#v, want %#v", provider.RecommendedModels, want)
			}
		}
		return
	}
	t.Fatal("Claude provider is missing")
}
