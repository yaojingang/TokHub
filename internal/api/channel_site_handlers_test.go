package api

import (
	"strings"
	"testing"

	"tokhub/internal/store"
)

func TestChannelSiteStaticFilesHonorSEOAndModules(t *testing.T) {
	site := store.ChannelSite{
		Name:           "Partner Site",
		PublicURL:      "https://partner.example.com/status",
		Title:          "Default Title",
		Description:    "Default description",
		LogoMark:       "P",
		OverviewLabel:  "监控总览",
		RecommendLabel: "精选推荐",
		Modules: store.ChannelSiteModules{
			Overview:     true,
			ChannelBoard: true,
			Recommend:    false,
			ProviderRank: true,
			Strategy:     true,
		},
		Copy: store.ChannelSiteCopy{HomeIntro: "Home intro"},
		SEO:  store.ChannelSiteSEO{Title: "SEO Title", Description: "SEO description"},
	}

	html := channelSiteHTML(site, map[string]any{}, false)
	if !strings.Contains(html, "<title>SEO Title</title>") {
		t.Fatalf("home HTML did not use SEO title: %s", html)
	}
	if !strings.Contains(html, `name="description" content="SEO description"`) {
		t.Fatalf("home HTML did not use SEO description: %s", html)
	}

	sitemap := channelSiteSitemap(site)
	if strings.Contains(sitemap, "/recommend/") {
		t.Fatalf("sitemap included disabled recommend page: %s", sitemap)
	}

	robots := channelSiteRobots(site)
	if !strings.Contains(robots, "Sitemap: https://partner.example.com/status/sitemap.xml") {
		t.Fatalf("robots.txt did not use absolute sitemap URL: %s", robots)
	}

	js := channelSiteJS()
	for _, needle := range []string{`enabled("recommend")`, `enabled("channelBoard")`, `enabled("providerRank")`, `enabled("strategy")`} {
		if !strings.Contains(js, needle) {
			t.Fatalf("channel site JS missing module guard %q", needle)
		}
	}
}
