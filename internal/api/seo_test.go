package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"tokhub/internal/store"
)

func TestRenderFrontendHTMLKeepsReactRootEmpty(t *testing.T) {
	index := `<!doctype html><html><head><title>TokHub · API 中转站监控工作台</title></head><body><div id="root"></div><script src="/assets/app.js"></script></body></html>`
	page := seoPage{
		Title:       "TokHub SEO",
		Description: "Semantic fallback",
		Canonical:   "http://localhost:28125/",
		BodyHTML:    `<main class="seo-fallback"><h1>AI API 中转站监控</h1></main>`,
	}

	out := renderFrontendHTML(index, page)

	if !strings.Contains(out, `<div id="root"></div>`) {
		t.Fatalf("expected React root to stay empty, got %s", out)
	}
	if strings.Contains(out, `<div id="root">`+"\n"+page.BodyHTML) {
		t.Fatalf("SEO fallback must not be rendered inside React root")
	}
	if !strings.Contains(out, `<noscript>`) || !strings.Contains(out, "AI API 中转站监控") {
		t.Fatalf("expected SEO fallback to remain available in noscript, got %s", out)
	}
	if !strings.Contains(out, `<title>TokHub SEO</title>`) {
		t.Fatalf("expected SEO title injection, got %s", out)
	}
}

func TestRenderFrontendHTMLInjectsAnalyticsCodeIntoHead(t *testing.T) {
	index := `<!doctype html><html><head><title>TokHub</title></head><body><div id="root"></div></body></html>`
	page := seoPage{
		Title:         "TokHub SEO",
		AnalyticsCode: `<script>window._hmt=window._hmt||[];</script>`,
	}

	out := renderFrontendHTML(index, page)

	if !strings.Contains(out, "<!-- TokHub analytics -->") {
		t.Fatalf("expected analytics marker, got %s", out)
	}
	if !strings.Contains(out, `<script>window._hmt=window._hmt||[];</script>`) {
		t.Fatalf("expected analytics code to be injected, got %s", out)
	}
	if strings.Index(out, `<script>window._hmt=window._hmt||[];</script>`) > strings.Index(out, "</head>") {
		t.Fatalf("expected analytics code before closing head, got %s", out)
	}
	if strings.Contains(out, `<div id="root"><script>`) {
		t.Fatalf("analytics code must not be injected inside React root, got %s", out)
	}

	withoutAnalytics := renderFrontendHTML(index, seoPage{Title: "TokHub SEO"})
	if strings.Contains(withoutAnalytics, "TokHub analytics") || strings.Contains(withoutAnalytics, "window._hmt") {
		t.Fatalf("analytics code should be omitted when page has no analytics code, got %s", withoutAnalytics)
	}
}

func TestPublicSiteConfigRedactsAnalyticsCode(t *testing.T) {
	cfg := store.SiteConfig{AnalyticsCode: `<script>window._hmt=window._hmt||[];</script>`}
	publicCfg := publicSiteConfig(cfg)

	if publicCfg.AnalyticsCode != "" {
		t.Fatalf("public analytics code = %q, want empty", publicCfg.AnalyticsCode)
	}
	if cfg.AnalyticsCode == "" {
		t.Fatal("publicSiteConfig should not mutate the source config")
	}
}

func TestWithAnalyticsAppliesToPublicAndPrivateRoutes(t *testing.T) {
	site := store.SiteConfig{AdminPath: "/ops-admin", AnalyticsCode: `<script>window._hmt=window._hmt||[];</script>`}
	paths := []string{"/", "/dashboard", "/pricing", "/recommend", "/channels/claude", "/missing", "/admin/settings", "/ops-admin/settings", "/console", "/console/keys", "/login"}
	for _, path := range paths {
		page := withAnalytics(seoPage{Title: path}, site)
		if page.AnalyticsCode == "" {
			t.Fatalf("path %s analytics code = empty, want configured code", path)
		}
	}
}

func TestPrivateSEOPagePathClassifiesPrivateRoutes(t *testing.T) {
	site := store.SiteConfig{AdminPath: "/ops-admin"}
	for _, path := range []string{"/admin/settings", "/ops-admin/settings", "/console", "/console/keys", "/login"} {
		if !isPrivateSEOPagePath(path, site) {
			t.Fatalf("path %s should be private", path)
		}
	}
	for _, path := range []string{"/", "/dashboard", "/pricing", "/recommend", "/channels/claude", "/missing"} {
		if isPrivateSEOPagePath(path, site) {
			t.Fatalf("path %s should be public", path)
		}
	}
}

func TestCleanSiteConfigInputRejectsOversizedAnalyticsCode(t *testing.T) {
	cfg := store.SiteConfig{
		RegistrationOpen: true,
		ShowRegisterCTA:  true,
		AdminPath:        "/admin",
		BrandName:        "TokHub",
		LogoMark:         "T",
		NavItems:         []store.NavItem{{Label: "首页", Href: "/"}},
		FooterLinks:      []store.NavItem{{Label: "首页", Href: "/"}},
		AnalyticsCode:    strings.Repeat("x", 20001),
	}

	if _, errText := cleanSiteConfigInput(cfg); errText != "统计代码不能超过 20000 个字符" {
		t.Fatalf("expected oversized analytics code to be rejected, got %q", errText)
	}
}

func TestPricingSEOHasDedicatedMetadata(t *testing.T) {
	server := &Server{cfg: Config{PublicURL: "https://tokhub.example"}}
	req := httptest.NewRequest(http.MethodGet, "/pricing", nil)

	page := server.seoForRequest(req)
	out := renderFrontendHTML(`<html><head><title>TokHub</title></head><body><div id="root"></div></body></html>`, page)

	for _, needle := range []string{
		`<title>TokHub 成本预估 | AI API 月成本估算与官方价格表</title>`,
		`<meta name="description" content="TokHub 成本预估工作台`,
		`<link rel="canonical" href="https://tokhub.example/pricing"`,
		`<h2>AI 摘要要点</h2>`,
		`AI 模型成本预估基准表`,
		`BreadcrumbList`,
	} {
		if !strings.Contains(out, needle) {
			t.Fatalf("expected pricing SEO output to contain %q, got %s", needle, out)
		}
	}
}

func TestAdminSEOPathDetectionHonorsCustomAdminPath(t *testing.T) {
	site := store.SiteConfig{AdminPath: "/ops-admin"}
	cases := map[string]bool{
		"/admin":                true,
		"/admin/settings":       true,
		"/ops-admin":            true,
		"/ops-admin/settings":   true,
		"/ops-administer":       false,
		"/console":              false,
		"/dashboard":            false,
		"/api/admin/settings":   false,
		"/assets/index.js":      false,
		"/manifest.webmanifest": false,
	}

	for path, want := range cases {
		if got := isAdminPagePath(path, site); got != want {
			t.Fatalf("isAdminPagePath(%q) = %v, want %v", path, got, want)
		}
	}
}

func TestPublicPagesExposeStaticModuleOutlines(t *testing.T) {
	server := &Server{cfg: Config{PublicURL: "https://tokhub.example"}}
	index := `<html><head><title>TokHub</title></head><body><div id="root"></div></body></html>`
	cases := []struct {
		path    string
		needles []string
	}{
		{
			path:    "/",
			needles: []string{"首页主视觉", "实时监控快照", "桌面工作台入口", "精选推荐预览", "使用流程", "常见问题"},
		},
		{
			path:    "/dashboard",
			needles: []string{"通道明细看板", "全部通道", "品牌维度", "模型维度", "我的关注", "我的私有通道", "状态判断矩阵"},
		},
		{
			path:    "/pricing",
			needles: []string{"模型成本预估", "成本估算参数", "当前成本结果", "官方价格表", "价格排行", "官方来源"},
		},
		{
			path:    "/recommend",
			needles: []string{"推荐统计", "本周编辑精选", "多维度榜单", "创建个人网关", "看你怎么用", "开放 API 同步推荐状态"},
		},
	}

	for _, tc := range cases {
		req := httptest.NewRequest(http.MethodGet, tc.path, nil)
		out := renderFrontendHTML(index, server.seoForRequest(req))
		for _, needle := range tc.needles {
			if !strings.Contains(out, needle) {
				t.Fatalf("%s SEO output should contain %q, got %s", tc.path, needle, out)
			}
		}
	}
}

func TestSitemapAndLLMSTxtIncludePricing(t *testing.T) {
	server := &Server{cfg: Config{PublicURL: "https://tokhub.example"}}

	sitemap := httptest.NewRecorder()
	server.sitemapXML(sitemap, httptest.NewRequest(http.MethodGet, "/sitemap.xml", nil))
	if !strings.Contains(sitemap.Body.String(), "https://tokhub.example/pricing") {
		t.Fatalf("expected sitemap to include pricing URL, got %s", sitemap.Body.String())
	}

	llms := httptest.NewRecorder()
	server.llmsTxt(llms, httptest.NewRequest(http.MethodGet, "/llms.txt", nil))
	for _, needle := range []string{"Cost estimation: https://tokhub.example/pricing", "## Cost Estimation Workbench"} {
		if !strings.Contains(llms.Body.String(), needle) {
			t.Fatalf("expected llms.txt to contain %q, got %s", needle, llms.Body.String())
		}
	}
}

func TestChannelSEOBodyUsesStructuredIntro(t *testing.T) {
	out := renderChannelSEOBody(store.SiteConfig{BrandName: "TokHub"}, store.PublicChannelDetail{
		Channel: store.PublicChannel{
			ID:              "ch_test",
			PublicSlug:      "abc123",
			Name:            "AIGoCode",
			Provider:        "AIGoCode",
			Type:            "openai-compatible",
			Model:           "claude-sonnet-4-6",
			UpstreamModel:   "claude-sonnet-4-6",
			Endpoint:        "https://api.example.com/v1",
			OfficialSiteURL: "https://aigocode.example",
			IntroTitle:      "AIGoCode 官方介绍",
			IntroSummary:    "官方摘要",
			IntroBody:       "第一段 **加粗** 介绍。\n\n- 要点一\n- 要点二",
			IntroHighlights: []string{"亮点 A", "亮点 B"},
			LogoURL:         "https://aigocode.example/logo.png",
			StatusLabel:     "Healthy",
			Score:           99,
			Uptime24h:       99.5,
			SuccessRate:     98.5,
			LatencyP95Ms:    320,
		},
	})
	for _, needle := range []string{
		"<h2>AIGoCode 官方介绍</h2>",
		"<strong>官方摘要</strong>",
		"<strong>加粗</strong>",
		"<li>要点一</li>",
		"<li>亮点 A</li>",
		`href="#tokhub-index"`,
		`id="tokhub-index"`,
		"综合健康指数 · TokHub Index",
	} {
		if !strings.Contains(out, needle) {
			t.Fatalf("expected channel SEO body to contain %q, got %s", needle, out)
		}
	}
}
