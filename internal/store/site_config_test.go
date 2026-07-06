package store

import "testing"

func TestNormalizeSiteConfigPreservesExplicitEmptyOptionalText(t *testing.T) {
	cfg := normalizeSiteConfig(SiteConfig{
		RegistrationOpen:     true,
		ShowRegisterCTA:      true,
		BrandName:            "TokHub",
		LogoMark:             "T",
		Subtitle:             "",
		FooterText:           "",
		DefaultGatewayPolicy: "success",
		Timezone:             "UTC",
		NavItems:             defaultNavItems(),
		FooterLinks:          defaultFooterLinks(),
	})

	if cfg.Subtitle != "" {
		t.Fatalf("expected explicit empty subtitle to be preserved, got %q", cfg.Subtitle)
	}
	if cfg.FooterText != "" {
		t.Fatalf("expected explicit empty footer text to be preserved, got %q", cfg.FooterText)
	}
}

func TestDefaultSiteConfigStillProvidesOptionalTextDefaults(t *testing.T) {
	cfg := DefaultSiteConfig("http://localhost:8080", true)
	if cfg.Subtitle == "" {
		t.Fatal("expected default site config to include subtitle")
	}
	if cfg.FooterText == "" {
		t.Fatal("expected default site config to include footer text")
	}
	if cfg.AdminPath != "/admin" {
		t.Fatalf("expected default admin path /admin, got %q", cfg.AdminPath)
	}
	if cfg.AnalyticsCode != "" {
		t.Fatalf("expected default analytics code to be empty, got %q", cfg.AnalyticsCode)
	}
}

func TestNormalizeSiteConfigCleansAnalyticsCodeLineEndings(t *testing.T) {
	cfg := normalizeSiteConfig(SiteConfig{
		RegistrationOpen: true,
		ShowRegisterCTA:  true,
		AnalyticsCode:    " \r\n<script>window._hmt=[];</script>\r\n ",
	})

	if cfg.AnalyticsCode != "<script>window._hmt=[];</script>" {
		t.Fatalf("expected analytics code to be trimmed and normalized, got %q", cfg.AnalyticsCode)
	}
}

func TestNormalizeSiteConfigAdminPath(t *testing.T) {
	cfg := normalizeSiteConfig(SiteConfig{
		RegistrationOpen: true,
		ShowRegisterCTA:  true,
		AdminPath:        "/ops-admin/",
		FooterLinks: []NavItem{
			{Label: "平台管理", Href: "/admin"},
			{Label: "后台设置", Href: "/admin/settings"},
		},
	})

	if cfg.AdminPath != "/ops-admin" {
		t.Fatalf("expected trailing slash to be trimmed, got %q", cfg.AdminPath)
	}
	if cfg.FooterLinks[0].Href != "/ops-admin" || cfg.FooterLinks[1].Href != "/ops-admin/settings" {
		t.Fatalf("expected legacy admin footer links to follow admin path, got %#v", cfg.FooterLinks)
	}
}

func TestDefaultSiteConfigPublicNavUsesThreePrimaryPages(t *testing.T) {
	cfg := DefaultSiteConfig("http://localhost:8080", true)
	expected := []NavItem{
		{Label: "首页", Href: "/"},
		{Label: "监控总览", Href: "/dashboard"},
		{Label: "精选推荐", Href: "/recommend"},
	}
	if len(cfg.NavItems) < len(expected) {
		t.Fatalf("expected at least %d nav items, got %d", len(expected), len(cfg.NavItems))
	}
	for index, item := range expected {
		if cfg.NavItems[index] != item {
			t.Fatalf("expected nav item %d to be %#v, got %#v", index, item, cfg.NavItems[index])
		}
	}
}

func TestDefaultSiteConfigIncludesDefaultMonitorModels(t *testing.T) {
	cfg := DefaultSiteConfig("http://localhost:8080", true)
	if len(cfg.MonitorModels) != 2 {
		t.Fatalf("expected 2 default monitor models, got %#v", cfg.MonitorModels)
	}
	if cfg.MonitorModels[0].Model != "claude-sonnet-4-6" || cfg.MonitorModels[1].Model != "gpt-5.5" {
		t.Fatalf("unexpected default monitor model order: %#v", cfg.MonitorModels)
	}
	for _, item := range cfg.MonitorModels {
		if !item.Enabled || !item.DefaultSelected {
			t.Fatalf("expected default monitor model %s to be enabled and selected", item.Model)
		}
	}
}

func TestNormalizeSiteConfigFillsMonitorModelDefaults(t *testing.T) {
	cfg := normalizeSiteConfig(SiteConfig{
		RegistrationOpen: true,
		ShowRegisterCTA:  true,
		MonitorModels: []MonitorModelConfig{
			{Model: "custom-model", InputPerMTok: -1, OutputPerMTok: -2},
		},
	})

	if len(cfg.MonitorModels) != 1 {
		t.Fatalf("expected one monitor model, got %#v", cfg.MonitorModels)
	}
	item := cfg.MonitorModels[0]
	if item.Key != "custom-model" || item.Label != "custom-model" || item.UpstreamModel != "custom-model" {
		t.Fatalf("expected monitor defaults from model ID, got %#v", item)
	}
	if item.Type != "openai-compatible" {
		t.Fatalf("expected default adapter, got %q", item.Type)
	}
	if item.InputPerMTok != 0 || item.OutputPerMTok != 0 {
		t.Fatalf("expected negative prices to clamp to zero, got %#v", item)
	}
	if len(item.Aliases) != 1 || item.Aliases[0] != "custom-model" {
		t.Fatalf("expected model alias to be included, got %#v", item.Aliases)
	}
}

func TestNormalizeSiteConfigUpgradesLegacyMonitorHomeLinks(t *testing.T) {
	cfg := normalizeSiteConfig(SiteConfig{
		RegistrationOpen: true,
		ShowRegisterCTA:  true,
		NavItems: []NavItem{
			{Label: "监控总览", Href: "/"},
			{Label: "精选推荐", Href: "/recommend"},
			{Label: "文档", Href: "https://example.com/docs"},
		},
		FooterLinks: []NavItem{
			{Label: "监控总览", Href: "/"},
			{Label: "精选推荐", Href: "/recommend"},
			{Label: "控制台", Href: "/console"},
		},
	})

	expectedNav := []NavItem{
		{Label: "首页", Href: "/"},
		{Label: "监控总览", Href: "/dashboard"},
		{Label: "精选推荐", Href: "/recommend"},
		{Label: "文档", Href: "https://example.com/docs"},
	}
	if len(cfg.NavItems) != len(expectedNav) {
		t.Fatalf("expected %d nav items, got %d: %#v", len(expectedNav), len(cfg.NavItems), cfg.NavItems)
	}
	for index, item := range expectedNav {
		if cfg.NavItems[index] != item {
			t.Fatalf("expected nav item %d to be %#v, got %#v", index, item, cfg.NavItems[index])
		}
	}
	if cfg.FooterLinks[0] != (NavItem{Label: "首页", Href: "/"}) || cfg.FooterLinks[1] != (NavItem{Label: "监控总览", Href: "/dashboard"}) {
		t.Fatalf("expected legacy footer links to include homepage and dashboard first, got %#v", cfg.FooterLinks)
	}
}

func TestNormalizeSiteConfigFixesLegacyHomeLabelWhenDashboardExists(t *testing.T) {
	cfg := normalizeSiteConfig(SiteConfig{
		RegistrationOpen: true,
		ShowRegisterCTA:  true,
		NavItems: []NavItem{
			{Label: "监控总览", Href: "/"},
			{Label: "监控总览", Href: "/dashboard"},
			{Label: "精选推荐", Href: "/recommend"},
		},
		FooterLinks: defaultFooterLinks(),
	})

	if cfg.NavItems[0] != (NavItem{Label: "首页", Href: "/"}) {
		t.Fatalf("expected root nav label to be upgraded to 首页, got %#v", cfg.NavItems[0])
	}
	if len(cfg.NavItems) != 3 {
		t.Fatalf("expected dashboard link not to be duplicated, got %#v", cfg.NavItems)
	}
}
