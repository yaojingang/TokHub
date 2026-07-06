package api

import (
	"net"
	"strings"
	"testing"

	"golang.org/x/net/html"

	"tokhub/internal/store"
)

func TestBuildChannelIntroDraftExtractsStructuredMetadata(t *testing.T) {
	doc, err := html.Parse(strings.NewReader(`<!doctype html>
<html>
<head>
  <meta name="description" content="AIGoCode 面向 Claude Code 用户提供 API 接入、模型能力和开发者工作流。">
  <meta property="og:image" content="/brand.png">
  <link rel="icon" href="/favicon.ico">
</head>
<body>
  <main>
    <p>AIGoCode 提供面向开发者的 AI 编程服务入口，聚合模型访问、账号注册、试用体验和 API 接入说明。</p>
    <p>平台强调稳定接入、快速注册和多模型工作流，适合个人开发者和团队在上线前完成服务验证。</p>
  </main>
</body>
</html>`))
	if err != nil {
		t.Fatal(err)
	}
	draft := buildChannelIntroDraft("https://aigocode.example/path", doc, store.PublicChannel{
		Name:     "AIGoCode",
		Provider: "AIGoCode",
		Type:     "openai-compatible",
		Model:    "claude-sonnet-4-6",
		Endpoint: "https://api.aigocode.example/v1",
	})
	if draft.IntroTitle != "AIGoCode 官方介绍" {
		t.Fatalf("intro title = %q", draft.IntroTitle)
	}
	if !strings.Contains(draft.IntroSummary, "Claude Code 用户") {
		t.Fatalf("intro summary = %q", draft.IntroSummary)
	}
	if !strings.Contains(draft.IntroBody, "开发者的 AI 编程服务入口") || !strings.Contains(draft.IntroBody, "**AIGoCode**") {
		t.Fatalf("intro body = %q", draft.IntroBody)
	}
	if draft.LogoURL != "https://aigocode.example/brand.png" {
		t.Fatalf("logo url = %q", draft.LogoURL)
	}
	if len(draft.IntroHighlights) < 4 {
		t.Fatalf("intro highlights = %#v", draft.IntroHighlights)
	}
}

func TestPublicFetchIPRejectsPrivateAndReservedAddresses(t *testing.T) {
	for _, raw := range []string{"127.0.0.1", "10.0.0.8", "192.168.1.20", "172.16.0.1", "100.64.0.1", "169.254.1.1", "::1", "fc00::1"} {
		if publicFetchIP(net.ParseIP(raw)) {
			t.Fatalf("expected %s to be blocked", raw)
		}
	}
	if !publicFetchIP(net.ParseIP("8.8.8.8")) {
		t.Fatal("expected public IP to be allowed")
	}
}

func TestAbsoluteHTMLURLOnlyReturnsHTTPAssets(t *testing.T) {
	base := "https://aigocode.example/path/page"
	cases := map[string]string{
		"/brand.png#fragment":                 "https://aigocode.example/brand.png",
		"//cdn.example.com/logo.svg":          "https://cdn.example.com/logo.svg",
		"data:image/svg+xml;base64,PHN2Zy8+":  "",
		"javascript:alert(1)":                 "",
		"https://user:pass@example.com/a.png": "",
	}
	for input, want := range cases {
		if got := absoluteHTMLURL(base, input); got != want {
			t.Fatalf("absoluteHTMLURL(%q) = %q, want %q", input, got, want)
		}
	}
}
