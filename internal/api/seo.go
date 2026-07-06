package api

import (
	"encoding/json"
	"fmt"
	"html"
	"net/http"
	"net/url"
	"os"
	pathpkg "path"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"tokhub/internal/store"
)

type seoPage struct {
	Title         string
	Description   string
	Canonical     string
	Robots        string
	Alternate     string
	BodyHTML      string
	JSONLD        []any
	AnalyticsCode string
}

func (s *Server) frontendIndex(w http.ResponseWriter, r *http.Request, file string) {
	raw, err := os.ReadFile(file)
	if err != nil {
		http.ServeFile(w, r, file)
		return
	}
	page := s.seoForRequest(r)
	out := renderFrontendHTML(string(raw), page)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=10")
	w.WriteHeader(http.StatusOK)
	if r.Method != http.MethodHead {
		_, _ = w.Write([]byte(out))
	}
}

func renderFrontendHTML(index string, page seoPage) string {
	title := "TokHub"
	if page.Title != "" {
		title = page.Title
	}
	htmlText := replaceHTMLTitle(index, title)
	if !strings.Contains(htmlText, "<title>") {
		htmlText = strings.Replace(htmlText, "</head>", "<title>"+esc(title)+"</title>\n  </head>", 1)
	}
	head := renderSEOMeta(page)
	if head != "" {
		htmlText = strings.Replace(htmlText, "</head>", head+"\n  </head>", 1)
	}
	if analytics := renderAnalyticsCode(page.AnalyticsCode); analytics != "" {
		htmlText = injectBeforeHeadClose(htmlText, analytics)
	}
	if page.BodyHTML != "" {
		if strings.Contains(htmlText, `<div id="root"></div>`) {
			fallback := `<div id="root"></div>` + "\n    <noscript>\n" + page.BodyHTML + "\n    </noscript>"
			htmlText = strings.Replace(htmlText, `<div id="root"></div>`, fallback, 1)
		}
	}
	return htmlText
}

func renderAnalyticsCode(code string) string {
	code = strings.TrimSpace(code)
	if code == "" {
		return ""
	}
	return "\n    <!-- TokHub analytics -->\n" + code
}

func injectBeforeHeadClose(htmlText string, snippet string) string {
	if !strings.Contains(htmlText, "</head>") {
		return htmlText + snippet
	}
	return strings.Replace(htmlText, "</head>", snippet+"\n  </head>", 1)
}

func replaceHTMLTitle(index string, title string) string {
	start := strings.Index(index, "<title>")
	if start < 0 {
		return index
	}
	end := strings.Index(index[start:], "</title>")
	if end < 0 {
		return index
	}
	end += start + len("</title>")
	return index[:start] + "<title>" + esc(title) + "</title>" + index[end:]
}

func renderSEOMeta(page seoPage) string {
	var b strings.Builder
	desc := strings.TrimSpace(page.Description)
	if desc != "" {
		fmt.Fprintf(&b, "\n    <meta name=\"description\" content=\"%s\" />", attr(desc))
	}
	robots := strings.TrimSpace(page.Robots)
	if robots == "" {
		robots = "index,follow,max-snippet:-1,max-image-preview:large,max-video-preview:-1"
	}
	fmt.Fprintf(&b, "\n    <meta name=\"robots\" content=\"%s\" />", attr(robots))
	if page.Canonical != "" {
		fmt.Fprintf(&b, "\n    <link rel=\"canonical\" href=\"%s\" />", attr(page.Canonical))
	}
	if page.Alternate != "" {
		fmt.Fprintf(&b, "\n    <link rel=\"alternate\" type=\"application/json\" href=\"%s\" />", attr(page.Alternate))
	}
	if page.Title != "" {
		fmt.Fprintf(&b, "\n    <meta property=\"og:title\" content=\"%s\" />", attr(page.Title))
	}
	if desc != "" {
		fmt.Fprintf(&b, "\n    <meta property=\"og:description\" content=\"%s\" />", attr(desc))
	}
	if page.Canonical != "" {
		fmt.Fprintf(&b, "\n    <meta property=\"og:url\" content=\"%s\" />", attr(page.Canonical))
	}
	fmt.Fprintf(&b, "\n    <meta property=\"og:type\" content=\"website\" />")
	fmt.Fprintf(&b, "\n    <meta property=\"og:locale\" content=\"zh_CN\" />")
	fmt.Fprintf(&b, "\n    <meta name=\"twitter:card\" content=\"summary\" />")
	if page.Title != "" {
		fmt.Fprintf(&b, "\n    <meta name=\"twitter:title\" content=\"%s\" />", attr(page.Title))
	}
	if desc != "" {
		fmt.Fprintf(&b, "\n    <meta name=\"twitter:description\" content=\"%s\" />", attr(desc))
	}
	for _, item := range page.JSONLD {
		raw, err := json.Marshal(item)
		if err != nil {
			continue
		}
		jsonText := strings.ReplaceAll(string(raw), "</script", "<\\/script")
		fmt.Fprintf(&b, "\n    <script type=\"application/ld+json\">%s</script>", jsonText)
	}
	return b.String()
}

func (s *Server) seoForRequest(r *http.Request) seoPage {
	site := s.seoSiteConfig(r)
	baseURL := s.publicBaseURL(r, site)
	canonical := absoluteSiteURL(baseURL, canonicalPath(r.URL.Path))
	pagePath := cleanPagePath(r.URL.Path)
	var page seoPage
	switch {
	case pagePath == "/":
		page = s.homeSEO(r, site, baseURL, canonical)
	case pagePath == "/dashboard":
		page = s.dashboardSEO(r, site, baseURL, canonical)
	case pagePath == "/pricing":
		page = s.pricingSEO(site, baseURL, canonical)
	case pagePath == "/recommend":
		page = s.recommendSEO(r, site, baseURL, canonical)
	case strings.HasPrefix(pagePath, "/channels/"):
		channelID := strings.TrimPrefix(pagePath, "/channels/")
		page = s.channelSEO(r, site, baseURL, canonical, channelID)
	case isPrivateSEOPagePath(pagePath, site):
		page = seoPage{
			Title:       site.BrandName + " 控制台",
			Description: site.BrandName + " 用户和管理员控制台入口。",
			Canonical:   canonical,
			Robots:      "noindex,nofollow",
			JSONLD:      []any{s.webSiteJSONLD(site, baseURL)},
		}
	default:
		page = seoPage{
			Title:       site.BrandName + " API 中转站监控",
			Description: site.FooterText,
			Canonical:   canonical,
			JSONLD:      []any{s.webSiteJSONLD(site, baseURL)},
		}
	}
	return withAnalytics(page, site)
}

func isPrivateSEOPagePath(pagePath string, site store.SiteConfig) bool {
	return isAdminPagePath(pagePath, site) || strings.HasPrefix(pagePath, "/console") || pagePath == "/login"
}

func withAnalytics(page seoPage, site store.SiteConfig) seoPage {
	page.AnalyticsCode = site.AnalyticsCode
	return page
}

func isAdminPagePath(pagePath string, site store.SiteConfig) bool {
	adminPath := store.NormalizeAdminPath(site.AdminPath)
	return pagePath == adminPath ||
		strings.HasPrefix(pagePath, adminPath+"/") ||
		pagePath == "/admin" ||
		strings.HasPrefix(pagePath, "/admin/")
}

func (s *Server) homeSEO(r *http.Request, site store.SiteConfig, baseURL string, canonical string) seoPage {
	overview := store.PublicOverview{UpdatedAt: time.Now()}
	recommend := store.RecommendConfig{UpdatedAt: time.Now()}
	if s.repo != nil {
		if data, err := s.repo.PublicOverview(r.Context()); err == nil {
			overview = data
		}
		if data, err := s.repo.RecommendConfig(r.Context()); err == nil {
			recommend = data
		}
	}
	title := fmt.Sprintf("%s | AI API 中转站监控、精选推荐和专属网关", site.BrandName)
	description := fmt.Sprintf("%s 提供 AI API 中转站首页入口，整合实时监控总览、精选推荐、个人关注、私有通道和专属中转站流程。当前公开监控 %d 个通道，健康率 %.1f%%。",
		site.BrandName, overview.Total, overview.HealthyRate)
	return seoPage{
		Title:       title,
		Description: trimMeta(description),
		Canonical:   canonical,
		Alternate:   absoluteSiteURL(baseURL, "/api/public/overview"),
		BodyHTML:    renderProductHomeSEOBody(site, overview, recommend),
		JSONLD: []any{
			s.webSiteJSONLD(site, baseURL),
			webPageJSONLD(site, canonical, title, description),
			recommendItemListJSONLD(baseURL, recommend),
			siteHomeFAQJSONLD(site),
		},
	}
}

func (s *Server) dashboardSEO(r *http.Request, site store.SiteConfig, baseURL string, canonical string) seoPage {
	overview := store.PublicOverview{UpdatedAt: time.Now()}
	channels := []store.PublicChannel{}
	ranks := []store.ProviderRank{}
	errorsSummary := []store.ErrorSummary{}
	if s.repo != nil {
		if data, err := s.repo.PublicOverview(r.Context()); err == nil {
			overview = data
		}
		if list, err := s.repo.PublicChannels(r.Context(), store.ChannelFilter{Page: 1, PageSize: 20}); err == nil {
			channels = list.Items
		}
		if data, err := s.repo.PublicProviderRank(r.Context()); err == nil {
			ranks = data
		}
		if data, err := s.repo.PublicErrorsSummary(r.Context()); err == nil {
			errorsSummary = data
		}
	}
	title := fmt.Sprintf("%s 监控总览 | 真实 L1/L2/L3 状态看板", site.BrandName)
	description := fmt.Sprintf("%s 实时追踪 %d 个 AI API 中转站，覆盖基础链路 L1/L2、真实生成 L3、P95 延迟、24H 可用率、错误分类和公开状态 API。当前健康通道 %d 个，健康率 %.1f%%。",
		site.BrandName, overview.Total, overview.Healthy, overview.HealthyRate)
	body := renderHomeSEOBody(site, overview, channels, ranks, errorsSummary)
	return seoPage{
		Title:       title,
		Description: trimMeta(description),
		Canonical:   canonical,
		Alternate:   absoluteSiteURL(baseURL, "/api/public/channels"),
		BodyHTML:    body,
		JSONLD: []any{
			s.webSiteJSONLD(site, baseURL),
			webPageJSONLD(site, canonical, title, description),
			homeDatasetJSONLD(site, canonical, overview),
			channelItemListJSONLD(baseURL, channels),
			homeFAQJSONLD(site),
			breadcrumbJSONLD(baseURL, []seoCrumb{
				{Name: "首页", Path: "/"},
				{Name: "监控总览", Path: "/dashboard"},
			}),
		},
	}
}

func (s *Server) pricingSEO(site store.SiteConfig, baseURL string, canonical string) seoPage {
	title := fmt.Sprintf("%s 成本预估 | AI API 月成本估算与官方价格表", site.BrandName)
	description := fmt.Sprintf("%s 成本预估工作台汇总 OpenAI、Anthropic、Google、xAI、Mistral、DeepSeek、Cohere 等模型的输入价、输出价、缓存读价、上下文窗口和典型场景月成本。",
		site.BrandName)
	return seoPage{
		Title:       title,
		Description: trimMeta(description),
		Canonical:   canonical,
		BodyHTML:    renderPricingSEOBody(site),
		JSONLD: []any{
			s.webSiteJSONLD(site, baseURL),
			webPageJSONLD(site, canonical, title, description),
			pricingDatasetJSONLD(site, canonical),
			pricingFAQJSONLD(site),
			breadcrumbJSONLD(baseURL, []seoCrumb{
				{Name: "首页", Path: "/"},
				{Name: "成本预估", Path: "/pricing"},
			}),
		},
	}
}

func (s *Server) recommendSEO(r *http.Request, site store.SiteConfig, baseURL string, canonical string) seoPage {
	cfg := store.RecommendConfig{UpdatedAt: time.Now()}
	if s.repo != nil {
		if data, err := s.repo.RecommendConfig(r.Context()); err == nil {
			cfg = data
		}
	}
	names := make([]string, 0, len(cfg.Picks))
	for _, pick := range cfg.Picks {
		if pick.Title != "" {
			names = append(names, pick.Title)
		}
	}
	title := fmt.Sprintf("%s 精选推荐 | AI API 中转站榜单、监控和场景", site.BrandName)
	description := fmt.Sprintf("%s 精选 AI API 中转站，汇总真实监控评分、生成成功率、P95 延迟、接入入口和使用场景。当前推荐 %d 个，接入说明 %d 项，场景 %d 项。",
		site.BrandName, len(cfg.Picks), len(cfg.Rewards), len(cfg.Scenarios))
	if len(names) > 0 {
		description += " 覆盖 " + strings.Join(names, "、") + "。"
	}
	return seoPage{
		Title:       title,
		Description: trimMeta(description),
		Canonical:   canonical,
		Alternate:   absoluteSiteURL(baseURL, "/api/public/recommend"),
		BodyHTML:    renderRecommendSEOBody(site, cfg),
		JSONLD: []any{
			s.webSiteJSONLD(site, baseURL),
			webPageJSONLD(site, canonical, title, description),
			recommendItemListJSONLD(baseURL, cfg),
			recommendFAQJSONLD(site),
			breadcrumbJSONLD(baseURL, []seoCrumb{
				{Name: "首页", Path: "/"},
				{Name: "精选推荐", Path: "/recommend"},
			}),
		},
	}
}

func (s *Server) channelSEO(r *http.Request, site store.SiteConfig, baseURL string, canonical string, channelID string) seoPage {
	if s.repo == nil || channelID == "" {
		return noIndexChannelSEO(site, canonical)
	}
	detail, err := s.repo.PublicChannel(r.Context(), channelID)
	if err != nil {
		if err != pgx.ErrNoRows {
			s.logger.Warn("seo channel detail unavailable", "channel_id", channelID, "error", err)
		}
		return noIndexChannelSEO(site, canonical)
	}
	ch := detail.Channel
	canonical = absoluteSiteURL(baseURL, channelPublicPath(ch))
	title := fmt.Sprintf("%s %s 状态详情 | %s", ch.Name, ch.Model, site.BrandName)
	description := fmt.Sprintf("%s 是 %s 的 %s API 中转站。当前综合状态 %s，健康评分 %d，P95 延迟 %dms，24H 可用率 %.1f%%，L1 %s，L2 %s，L3 %s。",
		ch.Name, ch.Provider, ch.Model, ch.StatusLabel, ch.Score, ch.LatencyP95Ms, ch.Uptime24h, ch.L1Status, ch.L2Status, ch.L3Status)
	return seoPage{
		Title:       title,
		Description: trimMeta(description),
		Canonical:   canonical,
		Alternate:   absoluteSiteURL(baseURL, "/api/public/channels/"+url.PathEscape(channelPublicRef(ch))),
		BodyHTML:    renderChannelSEOBody(site, detail),
		JSONLD: []any{
			s.webSiteJSONLD(site, baseURL),
			channelServiceJSONLD(baseURL, detail),
			channelFAQJSONLD(site, detail),
			breadcrumbJSONLD(baseURL, []seoCrumb{
				{Name: "监控总览", Path: "/dashboard"},
				{Name: ch.Name, Path: channelPublicPath(ch)},
			}),
		},
	}
}

func noIndexChannelSEO(site store.SiteConfig, canonical string) seoPage {
	return seoPage{
		Title:       site.BrandName + " 通道详情",
		Description: "该通道详情暂不可用。",
		Canonical:   canonical,
		Robots:      "noindex,follow",
		BodyHTML: `<main class="seo-fallback" aria-label="通道详情">
      <h1>通道详情暂不可用</h1>
      <p>该 AI API 中转站通道不存在、尚未公开，或当前状态数据暂不可读。</p>
    </main>`,
	}
}

func renderProductHomeSEOBody(site store.SiteConfig, overview store.PublicOverview, recommend store.RecommendConfig) string {
	var b strings.Builder
	fmt.Fprintf(&b, `<main class="seo-fallback" aria-label="%s 首页语义摘要">`, attr(site.BrandName))
	fmt.Fprintf(&b, "\n      <h1>%s AI API 中转站监控与选择入口</h1>", esc(site.BrandName))
	fmt.Fprintf(&b, "\n      <p>%s 首页汇总监控亮点、精选推荐、用户控制台和专属中转站流程，帮助开发者先判断真实可用性，再选择或接入中转站。</p>", esc(site.BrandName))
	fmt.Fprintf(&b, "\n      <section><h2>首页主视觉</h2><p>主视觉模块说明 %s 是 API 中转站监控、精选推荐和专属网关入口，适合从公开监控进入成本预估、推荐榜单和个人工作区。</p></section>", esc(site.BrandName))
	fmt.Fprintf(&b, "\n      <section><h2>实时监控快照</h2><dl>")
	seoMetric(&b, "综合健康率", fmt.Sprintf("%.1f%%", overview.HealthyRate))
	seoMetric(&b, "健康通道", fmt.Sprintf("%d 个", overview.Healthy))
	seoMetric(&b, "功能性故障", fmt.Sprintf("%d 个", overview.FunctionalDown))
	seoMetric(&b, "连通异常", fmt.Sprintf("%d 个", overview.ConnectivityDown))
	seoMetric(&b, "今日探测次数", fmt.Sprintf("%d", overview.ProbeRunsToday))
	fmt.Fprintf(&b, "</dl></section>")
	fmt.Fprintf(&b, "\n      <section><h2>AI 摘要要点</h2><ul>")
	fmt.Fprintf(&b, "<li>本页是 %s 的公开工作台首页，负责分流到监控总览、成本预估、精选推荐和用户控制台。</li>", esc(site.BrandName))
	fmt.Fprintf(&b, "<li>核心判断链路是先查看真实可用性，再比较成本和推荐入口，最后进入个人工作区接入私有 API Key。</li>")
	fmt.Fprintf(&b, "<li>公开监控当前覆盖 %d 个通道，健康率 %.1f%%，数据来自公开监控 API 和后台推荐配置。</li>", overview.Total, overview.HealthyRate)
	fmt.Fprintf(&b, "</ul></section>")
	fmt.Fprintf(&b, "\n      <section><h2>首页指标条</h2><dl>")
	seoMetric(&b, "公开监控通道", fmt.Sprintf("%d 个", overview.Total))
	seoMetric(&b, "综合健康率", fmt.Sprintf("%.1f%%", overview.HealthyRate))
	seoMetric(&b, "真实调用延迟", fmt.Sprintf("%.2fs", overview.P95LatencySeconds))
	seoMetric(&b, "精选推荐位", fmt.Sprintf("%d 个", recommend.Stats.Picks))
	seoMetric(&b, "今日探测次数", fmt.Sprintf("%d", overview.ProbeRunsToday))
	fmt.Fprintf(&b, "</dl></section>")
	fmt.Fprintf(&b, "\n      <section><h2>桌面工作台入口</h2><ul>")
	fmt.Fprintf(&b, `<li><a href="/dashboard">监控总览</a>：查看 %d 个公开通道的 L1/L2/L3 状态、P95 延迟、健康率、错误分类和供应商排行。</li>`, overview.Total)
	fmt.Fprintf(&b, `<li><a href="/pricing">成本预估</a>：按请求量和 Token 结构估算月成本，并比较官方模型输入价、输出价、缓存读价和上下文窗口。</li>`)
	fmt.Fprintf(&b, `<li><a href="/recommend">精选推荐</a>：查看后台运营维护的 TOP3、榜单规则、官方入口和场景化推荐。</li>`)
	fmt.Fprintf(&b, `<li><a href="/console">用户控制台</a>：登录后关注公开通道、添加私有通道、创建专属中转站并查看告警和用量。</li>`)
	fmt.Fprintf(&b, "</ul></section>")
	fmt.Fprintf(&b, "\n      <section><h2>监控亮点</h2><dl>")
	seoMetric(&b, "公开通道", fmt.Sprintf("%d 个", overview.Total))
	seoMetric(&b, "健康率", fmt.Sprintf("%.1f%%", overview.HealthyRate))
	seoMetric(&b, "真实调用 P95 延迟", fmt.Sprintf("%.2fs", overview.P95LatencySeconds))
	seoMetric(&b, "今日探测次数", fmt.Sprintf("%d", overview.ProbeRunsToday))
	fmt.Fprintf(&b, "</dl></section>")
	fmt.Fprintf(&b, "\n      <section><h2>首页精选预览</h2>")
	if len(recommend.Picks) > 0 {
		fmt.Fprintf(&b, "<ol>")
		for _, pick := range recommend.Picks {
			fmt.Fprintf(&b, `<li><a href="%s">%s</a>：%s</li>`, attr(defaultText(pick.CTAURL, "/recommend")), esc(pick.Title), esc(pick.Summary))
		}
		fmt.Fprintf(&b, "</ol>")
	} else {
		fmt.Fprintf(&b, "<p>后台配置 TOP3 后，本模块会输出首页精选推荐卡片、推荐摘要、推荐入口和完整推荐页链接。</p>")
	}
	fmt.Fprintf(&b, "</section>")
	fmt.Fprintf(&b, "\n      <section><h2>使用流程</h2><ol>")
	fmt.Fprintf(&b, "<li>先进入监控总览，排除异常通道。</li>")
	fmt.Fprintf(&b, "<li>再进入精选推荐，按场景和榜单缩小选择范围。</li>")
	fmt.Fprintf(&b, "<li>登录用户控制台，关注公开通道或添加自己的私有 API Key。</li>")
	fmt.Fprintf(&b, "<li>创建专属中转站，把可用通道收敛为统一 OpenAI 兼容入口。</li>")
	fmt.Fprintf(&b, "</ol></section>")
	fmt.Fprintf(&b, "\n      <section><h2>常见问题</h2><dl>")
	seoMetric(&b, "TokHub 首页提供什么", site.BrandName+" 首页集中展示监控优势、核心入口、精选推荐预览、使用流程和用户控制台入口。")
	seoMetric(&b, "监控总览在哪里", "监控总览是公开页面 /dashboard，负责承载实时通道看板、供应商排行、错误分布和监控策略。")
	seoMetric(&b, "精选推荐和监控数据是什么关系", "精选推荐由后台运营维护，推荐卡片和榜单会引用真实监控评分、成功率、P95 延迟和状态数据。")
	fmt.Fprintf(&b, "</dl></section>")
	fmt.Fprintf(&b, "\n    </main>")
	return b.String()
}

func renderHomeSEOBody(site store.SiteConfig, overview store.PublicOverview, channels []store.PublicChannel, ranks []store.ProviderRank, errorsSummary []store.ErrorSummary) string {
	var b strings.Builder
	fmt.Fprintf(&b, `<main class="seo-fallback" aria-label="%s 公开监控语义摘要">`, attr(site.BrandName))
	fmt.Fprintf(&b, "\n      <h1>%s API 中转站监控</h1>", esc(site.BrandName))
	fmt.Fprintf(&b, "\n      <p>%s 使用真实 Token 探测和三层链路监控评估 AI API 中转站，覆盖 DNS、TCP、TLS、HTTP、模型列表和最小生成调用。</p>", esc(site.BrandName))
	fmt.Fprintf(&b, "\n      <section><h2>监控总览首屏</h2><p>首屏展示公开通道总数、基础链路探测、真实生成探测和最近更新时间，帮助搜索引擎理解本页是 API 中转站状态看板。</p></section>")
	fmt.Fprintf(&b, "\n      <section><h2>AI 摘要要点</h2><ul>")
	fmt.Fprintf(&b, "<li>本页是公开监控总览，适合查询 AI API 中转站是否健康、是否慢响应、是否出现认证或功能性故障。</li>")
	fmt.Fprintf(&b, "<li>L1/L2 负责基础链路和模型入口，L3 使用真实生成请求验证业务可用性。</li>")
	fmt.Fprintf(&b, "<li>当前公开通道 %d 个，健康通道 %d 个，综合健康率 %.1f%%，最近更新时间 %s。</li>", overview.Total, overview.Healthy, overview.HealthyRate, esc(formatSEOTime(overview.UpdatedAt)))
	fmt.Fprintf(&b, "</ul></section>")
	fmt.Fprintf(&b, "\n      <section><h2>关键指标</h2><dl>")
	seoMetric(&b, "监控通道", fmt.Sprintf("%d 个", overview.Total))
	seoMetric(&b, "健康通道", fmt.Sprintf("%d 个", overview.Healthy))
	seoMetric(&b, "健康率", fmt.Sprintf("%.1f%%", overview.HealthyRate))
	seoMetric(&b, "真实调用 P95 延迟", fmt.Sprintf("%.2fs", overview.P95LatencySeconds))
	seoMetric(&b, "今日探测 Token", fmt.Sprintf("%d", overview.ProbeTokensToday))
	seoMetric(&b, "最近更新时间", formatSEOTime(overview.UpdatedAt))
	fmt.Fprintf(&b, "</dl></section>")
	fmt.Fprintf(&b, "\n      <section><h2>通道明细看板</h2>")
	fmt.Fprintf(&b, "<p>通道明细看板是 /dashboard 的核心模块，包含全部通道、品牌维度、模型维度、我的关注、我的私有通道、状态筛选、服务商筛选、通道筛选、关键词搜索、时间范围和 CSV 导出入口。</p>")
	fmt.Fprintf(&b, "<ul>")
	fmt.Fprintf(&b, "<li>全部通道：按品牌或模型聚合公开 AI API 中转站。</li>")
	fmt.Fprintf(&b, "<li>品牌维度：展示服务商、模型、综合状态、L1/L2/L3、P95 延迟、24H 可用率和近 90 次趋势。</li>")
	fmt.Fprintf(&b, "<li>模型维度：按模型聚合同类中转站表现，适合比较同一模型的可用入口。</li>")
	fmt.Fprintf(&b, "<li>我的关注：登录后展示收藏的公开平台通道。</li>")
	fmt.Fprintf(&b, "<li>我的私有通道：登录后展示用户自有 Endpoint、模型、探测结果和配额状态。</li>")
	fmt.Fprintf(&b, "</ul>")
	if len(channels) > 0 {
		fmt.Fprintf(&b, "<h3>通道明细数据</h3><ul>")
		for _, ch := range channels {
			fmt.Fprintf(&b, `<li><a href="/channels/%s">%s</a>，服务商 %s，模型 %s，状态 %s，评分 %d，P95 %dms，24H 可用率 %.1f%%，L1 %s，L2 %s，L3 %s。</li>`,
				attr(channelPublicRef(ch)), esc(ch.Name), esc(ch.Provider), esc(ch.Model), esc(ch.StatusLabel), ch.Score, ch.LatencyP95Ms, ch.Uptime24h, esc(ch.L1Status), esc(ch.L2Status), esc(ch.L3Status))
		}
		fmt.Fprintf(&b, "</ul>")
	} else {
		fmt.Fprintf(&b, "<h3>通道明细数据</h3><p>公开通道数据暂未加载，管理员录入平台通道并完成探测后，本模块会输出每个通道的服务商、模型、状态、评分、P95、可用率和 L1/L2/L3 结果。</p>")
	}
	fmt.Fprintf(&b, "</section>")
	fmt.Fprintf(&b, "\n      <section><h2>监控策略</h2><p>监控策略模块解释基础状态与真实可用性的分层关系，方便搜索引擎理解通道状态不是单一 HTTP 检查。</p>")
	fmt.Fprintf(&b, "<h3>两类策略 · 三层探测</h3><ul>")
	fmt.Fprintf(&b, "<li>基础监控 L1：DNS、TCP、TLS、HTTP 是否正常，高频且不消耗 Token。</li>")
	fmt.Fprintf(&b, "<li>基础监控 L2：API Key 是否有效、/v1/models 是否可访问、目标模型是否存在。</li>")
	fmt.Fprintf(&b, "<li>真实 API 监控 L3：用最小生成请求校验 content、finish_reason、usage、tokens/sec 和首 token。</li>")
	fmt.Fprintf(&b, "</ul>")
	fmt.Fprintf(&b, "<h3>状态判断矩阵</h3><dl>")
	seoMetric(&b, "基础正常 + 真实正常", "Healthy")
	seoMetric(&b, "基础异常 + 真实未执行", "Connectivity Down")
	seoMetric(&b, "基础正常 + 真实异常", "Functional Down")
	seoMetric(&b, "基础正常 + 慢响应", "Degraded")
	seoMetric(&b, "基础正常 + 额度异常", "Quota Risk")
	seoMetric(&b, "基础正常 + 认证异常", "Auth Error")
	seoMetric(&b, "无数据", "Unknown")
	fmt.Fprintf(&b, "</dl></section>")
	fmt.Fprintf(&b, "\n      <section><h2>供应商排行</h2><p>供应商排行模块基于真实调用成功率、P95 延迟与故障次数综合排序。</p>")
	if len(ranks) > 0 {
		fmt.Fprintf(&b, "<h3>真实调用成功率 TOP</h3><ol>")
		for _, rank := range ranks {
			fmt.Fprintf(&b, "<li>%s：%d 个通道，%d 个健康，成功率 %.1f%%，P95 %dms，评分 %d。</li>",
				esc(rank.Provider), rank.Channels, rank.Healthy, rank.SuccessRate, rank.LatencyP95Ms, rank.Score)
		}
		fmt.Fprintf(&b, "</ol>")
	} else {
		fmt.Fprintf(&b, "<h3>真实调用成功率 TOP</h3><p>暂无供应商排行数据，完成探测后会展示服务商通道数、健康通道数、成功率、P95 和评分。</p>")
	}
	fmt.Fprintf(&b, "</section>")
	fmt.Fprintf(&b, "\n      <section><h2>错误分类分布</h2><p>错误分类模块展示失败探测的错误类型占比，用于区分认证异常、连通异常、模型不可用、慢响应和未知故障。</p>")
	if len(errorsSummary) > 0 {
		fmt.Fprintf(&b, "<ul>")
		for _, item := range errorsSummary {
			fmt.Fprintf(&b, "<li>%s：%d 次。</li>", esc(item.Label), item.Count)
		}
		fmt.Fprintf(&b, "</ul>")
	} else {
		fmt.Fprintf(&b, "<p>暂无错误分类数据，失败探测出现后会输出错误类型和次数。</p>")
	}
	fmt.Fprintf(&b, "</section>")
	fmt.Fprintf(&b, "\n      <section><h2>公开状态 API</h2><p>第三方可以通过 /v1/status/channels、/v1/status/channels/{channelID}、/v1/status/uptime、/v1/status/incidents 和 /v1/status/overview 读取只读状态数据。</p></section>")
	fmt.Fprintf(&b, "\n    </main>")
	return b.String()
}

func renderPricingSEOBody(site store.SiteConfig) string {
	var b strings.Builder
	fmt.Fprintf(&b, `<main class="seo-fallback" aria-label="%s 成本预估语义摘要">`, attr(site.BrandName))
	fmt.Fprintf(&b, "\n      <h1>%s 成本预估与 AI API 月成本估算</h1>", esc(site.BrandName))
	fmt.Fprintf(&b, "\n      <p>本页按每日请求数、平均输入输出 Token 和缓存命中率预估月度账单，并汇总主流 AI 模型的官方输入价、输出价、缓存读价和上下文窗口，用于在接入中转站前先做成本基准判断。</p>")
	fmt.Fprintf(&b, "\n      <section><h2>模型成本预估</h2><p>首屏工作台模块用于输入每日请求量、平均输入 Token、平均输出 Token 和缓存命中率，并即时估算 30 天账单。</p></section>")
	fmt.Fprintf(&b, "\n      <section><h2>价格数据范围</h2><dl>")
	seoMetric(&b, "覆盖供应商", "OpenAI、Anthropic、Google、xAI、Mistral、DeepSeek、Cohere")
	seoMetric(&b, "价格单位", "USD / 1M tokens")
	seoMetric(&b, "估算周期", "30 天")
	seoMetric(&b, "核对时间", pricingLastModified().Format("2006-01-02"))
	fmt.Fprintf(&b, "</dl></section>")
	fmt.Fprintf(&b, "\n      <section><h2>AI 摘要要点</h2><ul>")
	fmt.Fprintf(&b, "<li>本页是模型成本预估工作台，不是网关调用接口，适合比较模型价格和估算月度 Token 成本。</li>")
	fmt.Fprintf(&b, "<li>价格维度包括供应商、模型、模型类型、输入价、输出价、缓存读价、上下文窗口、最大输出和官方来源。</li>")
	fmt.Fprintf(&b, "<li>成本估算按每日请求数、平均输入 Token、平均输出 Token、缓存命中率和 30 天周期计算。</li>")
	fmt.Fprintf(&b, "</ul></section>")
	fmt.Fprintf(&b, "\n      <section><h2>成本估算参数</h2><ul>")
	fmt.Fprintf(&b, "<li>快速场景：代码 Agent、RAG 问答、批量分类、长文总结。</li>")
	fmt.Fprintf(&b, "<li>参数输入：每日请求数、平均输入 Token、平均输出 Token、缓存命中率。</li>")
	fmt.Fprintf(&b, "<li>筛选条件：供应商、模型类型、模型或用途关键词。</li>")
	fmt.Fprintf(&b, "</ul></section>")
	fmt.Fprintf(&b, "\n      <section><h2>当前成本结果</h2><p>结果模块展示当前参数下的预估月成本、输入成本、输出成本、缓存读成本和低价候选模型，帮助用户在进入官方价格表前先形成预算范围。</p></section>")
	fmt.Fprintf(&b, "\n      <section><h2>覆盖供应商</h2><ul>")
	for _, provider := range []string{"OpenAI", "Anthropic", "Google", "xAI", "Mistral", "DeepSeek", "Cohere"} {
		fmt.Fprintf(&b, "<li>%s</li>", esc(provider))
	}
	fmt.Fprintf(&b, "</ul></section>")
	fmt.Fprintf(&b, "\n      <section><h2>官方价格表</h2><p>官方价格表模块按 per 1M tokens 展示模型、类型、输入价、输出价、缓存读、上下文窗口、最大输出、估算月成本、价格指数和官方来源。</p>")
	fmt.Fprintf(&b, "<table><thead><tr><th>供应商</th><th>模型</th><th>类型</th><th>输入价</th><th>输出价</th><th>缓存读</th><th>上下文</th><th>来源</th></tr></thead><tbody>")
	for _, row := range pricingSEOModelRows() {
		fmt.Fprintf(&b, "<tr><td>%s</td><td>%s</td><td>%s</td><td>$%.3g</td><td>$%.3g</td><td>%s</td><td>%s</td><td><a href=\"%s\">官方价格</a></td></tr>",
			esc(row.Provider), esc(row.Model), esc(row.Category), row.Input, row.Output, esc(row.CacheRead), esc(row.Context), attr(row.Source))
	}
	fmt.Fprintf(&b, "</tbody></table></section>")
	fmt.Fprintf(&b, "\n      <section><h2>表格筛选</h2><p>表格筛选模块提供供应商、模型类型和关键词筛选，方便搜索引擎理解页面支持按模型、厂商和用途定位价格信息。</p></section>")
	fmt.Fprintf(&b, "\n      <section><h2>价格排行</h2><ul>")
	fmt.Fprintf(&b, "<li>当前场景低价优先：按当前 Token 参数排序。</li>")
	fmt.Fprintf(&b, "<li>旗舰模型成本对比：对比高能力模型的月成本。</li>")
	fmt.Fprintf(&b, "<li>高频任务候选：筛选适合批量分类、抽取、路由等任务的低价模型。</li>")
	fmt.Fprintf(&b, "</ul></section>")
	fmt.Fprintf(&b, "\n      <section><h2>典型估算场景</h2><ul>")
	for _, scenario := range []string{"代码 Agent：多轮上下文、工具调用和中等输出", "RAG 问答：输入较长、输出稳定", "批量分类：高频短输入短输出", "长文总结：长输入、中等输出并受缓存影响"} {
		fmt.Fprintf(&b, "<li>%s</li>", esc(scenario))
	}
	fmt.Fprintf(&b, "</ul></section>")
	fmt.Fprintf(&b, "\n      <section><h2>读表口径</h2><p>输入价、输出价、缓存读价均按 USD / 1M tokens 展示。页面估算不含税、最低消费、区域价、搜索、工具、图片等附加费用，正式接入前应回到供应商官网核对最新条款。</p></section>")
	fmt.Fprintf(&b, "\n      <section><h2>官方来源</h2><p>官方来源模块记录各供应商价格页链接和最后核对时间，第一版为静态数据，后续建议由后台维护来源 URL 和生效时间。</p><ul>")
	for _, row := range pricingSEOModelRows() {
		fmt.Fprintf(&b, "<li>%s：<a href=\"%s\">%s 官方价格</a>，最后核对 %s。</li>", esc(row.Provider), attr(row.Source), esc(row.Provider), esc(row.CheckedAt))
	}
	fmt.Fprintf(&b, "</ul></section>")
	fmt.Fprintf(&b, "\n    </main>")
	return b.String()
}

func renderRecommendSEOBody(site store.SiteConfig, cfg store.RecommendConfig) string {
	var b strings.Builder
	fmt.Fprintf(&b, `<main class="seo-fallback" aria-label="%s 推荐页语义摘要">`, attr(site.BrandName))
	fmt.Fprintf(&b, "\n      <h1>%s 精选 AI API 中转站推荐</h1>", esc(site.BrandName))
	fmt.Fprintf(&b, "\n      <p>本页汇总真实监控评分、P95 延迟、成功率、接入入口和使用场景，帮助开发者选择适合 Claude Code、Cursor、Open WebUI 和企业网关的 API 中转站。</p>")
	fmt.Fprintf(&b, "\n      <section><h2>推荐首屏摘要</h2><p>首屏模块说明推荐页不是简单广告位，而是由真实监控数据、后台运营配置、官方入口和场景化推荐共同驱动。</p></section>")
	fmt.Fprintf(&b, "\n      <section><h2>推荐统计</h2><dl>")
	seoMetric(&b, "编辑精选位", fmt.Sprintf("%d", maxIntSEO(cfg.Stats.Picks, len(cfg.Picks))))
	seoMetric(&b, "上榜中转站", fmt.Sprintf("%d", maxIntSEO(cfg.Stats.Ranked, len(cfg.Ranks))))
	seoMetric(&b, "接入说明", fmt.Sprintf("%d", maxIntSEO(cfg.Stats.Rewards, len(cfg.Rewards))))
	seoMetric(&b, "场景推荐", fmt.Sprintf("%d", maxIntSEO(cfg.Stats.Scenarios, len(cfg.Scenarios))))
	seoMetric(&b, "精选平均评分", fmt.Sprintf("%d", cfg.Stats.AverageScore))
	fmt.Fprintf(&b, "</dl></section>")
	fmt.Fprintf(&b, "\n      <section><h2>AI 摘要要点</h2><ul>")
	fmt.Fprintf(&b, "<li>本页是精选推荐页，适合从真实监控数据和运营配置中筛选更适合试用或接入的 AI API 中转站。</li>")
	fmt.Fprintf(&b, "<li>推荐依据包括状态评分、生成成功率、P95 延迟、官方入口、场景适配和后台运营规则。</li>")
	fmt.Fprintf(&b, "<li>当前推荐 %d 个，榜单 %d 个，场景 %d 个，最后更新时间 %s。</li>", len(cfg.Picks), len(cfg.Ranks), len(cfg.Scenarios), esc(formatSEOTime(cfg.UpdatedAt)))
	fmt.Fprintf(&b, "</ul></section>")
	fmt.Fprintf(&b, "\n      <section><h2>本周编辑精选</h2>")
	if len(cfg.Picks) > 0 {
		fmt.Fprintf(&b, "<ol>")
		for _, pick := range cfg.Picks {
			fmt.Fprintf(&b, `<li><h3><a href="%s">%s</a></h3><p>%s</p>`, attr(defaultText(pick.CTAURL, channelPublicPath(pick.Channel))), esc(pick.Title), esc(pick.Summary))
			if len(pick.Points) > 0 {
				fmt.Fprintf(&b, "<ul>")
				for _, point := range pick.Points {
					fmt.Fprintf(&b, "<li>%s</li>", esc(point))
				}
				fmt.Fprintf(&b, "</ul>")
			}
			if pick.Channel.ID != "" {
				ch := pick.Channel
				fmt.Fprintf(&b, "<p>通道状态 %s，评分 %d，P95 %dms，24H 可用率 %.1f%%。</p>", esc(ch.StatusLabel), ch.Score, ch.LatencyP95Ms, ch.Uptime24h)
			}
			fmt.Fprintf(&b, "</li>")
		}
		fmt.Fprintf(&b, "</ol>")
	} else {
		fmt.Fprintf(&b, "<p>后台保存 TOP3 配置后，本模块会输出推荐标题、推荐摘要、推荐理由、状态评分、P95 延迟和 24H 可用率。</p>")
	}
	fmt.Fprintf(&b, "</section>")
	fmt.Fprintf(&b, "\n      <section><h2>多维度榜单</h2>")
	if len(cfg.RankRules) > 0 {
		fmt.Fprintf(&b, "<h3>榜单规则</h3><ul>")
		for _, rule := range cfg.RankRules {
			fmt.Fprintf(&b, "<li>%s：%s，指标 %s。</li>", esc(rule.Label), esc(rule.Description), esc(rule.Metric))
		}
		fmt.Fprintf(&b, "</ul>")
	} else {
		fmt.Fprintf(&b, "<p>榜单规则同步后，本模块会展示综合榜、速度优先、稳定优先或后台启用的自定义维度。</p>")
	}
	if len(cfg.Ranks) > 0 {
		fmt.Fprintf(&b, "<h3>上榜通道</h3><ol>")
		for _, rank := range cfg.Ranks {
			ch := rank.Channel
			fmt.Fprintf(&b, `<li><a href="/channels/%s">%s</a>，%s，状态 %s，评分 %d，P95 %dms，成功率 %.1f%%。</li>`,
				attr(channelPublicRef(ch)), esc(rank.Title), esc(ch.Model), esc(ch.StatusLabel), ch.Score, ch.LatencyP95Ms, ch.SuccessRate)
		}
		fmt.Fprintf(&b, "</ol>")
	} else {
		fmt.Fprintf(&b, "<p>暂无榜单数据，后台配置推荐位或录入公开平台通道后会在这里显示。</p>")
	}
	fmt.Fprintf(&b, "</section>")
	fmt.Fprintf(&b, "\n      <section><h2>创建个人网关</h2><ol>")
	fmt.Fprintf(&b, "<li>登录后生成专属端点，Endpoint 和 API Key 在控制台创建网关后显示。</li>")
	fmt.Fprintf(&b, "<li>粘到 Claude Code、Cursor、Open WebUI 等常用工具，使用 OpenAI 兼容协议。</li>")
	fmt.Fprintf(&b, "<li>配置后再接入业务，活动、价格和条款以供应商官网实时页面为准。</li>")
	fmt.Fprintf(&b, "</ol></section>")
	if len(cfg.Rewards) > 0 {
		fmt.Fprintf(&b, "\n      <section><h2>接入入口说明</h2><ul>")
		for _, reward := range cfg.Rewards {
			fmt.Fprintf(&b, "<li>%s：%s%s，推荐入口、活动信息和价格条款以供应商官网实时页面为准。</li>",
				esc(reward.ProviderName), esc(reward.RewardValue), esc(optionalSEOText("，代码 "+reward.Code, reward.Code)))
		}
		fmt.Fprintf(&b, "</ul></section>")
	} else {
		fmt.Fprintf(&b, "\n      <section><h2>接入入口说明</h2><p>后台配置推荐渠道后，本模块会展示供应商名称、权益说明、代码和官网入口提示。</p></section>")
	}
	fmt.Fprintf(&b, "\n      <section><h2>看你怎么用</h2>")
	if len(cfg.Scenarios) > 0 {
		fmt.Fprintf(&b, "<ul>")
		for _, scenario := range cfg.Scenarios {
			fmt.Fprintf(&b, "<li>%s：%s", esc(scenario.Title), esc(scenario.Summary))
			if scenario.Channel.Name != "" {
				fmt.Fprintf(&b, " 推荐通道 %s，当前状态 %s。", esc(scenario.Channel.Name), esc(scenario.Channel.StatusLabel))
			}
			fmt.Fprintf(&b, "</li>")
		}
		fmt.Fprintf(&b, "</ul>")
	} else {
		fmt.Fprintf(&b, "<p>后台配置场景化推荐后，本模块会输出代码 Agent、低成本任务、长上下文、企业网关等场景和关联通道状态。</p>")
	}
	fmt.Fprintf(&b, "</section>")
	fmt.Fprintf(&b, "\n      <section><h2>凭什么靠谱？看认证逻辑</h2><ol>")
	for _, item := range []string{"读取真实公开状态 API", "对比 L1/L2/L3 探测结果", "后台运营配置推荐入口", "记录点击和推荐位变更", "推荐页和首页 TOP3 保持一致"} {
		fmt.Fprintf(&b, "<li>%s。</li>", esc(item))
	}
	fmt.Fprintf(&b, "</ol></section>")
	fmt.Fprintf(&b, "\n      <section><h2>开放 API 同步推荐状态</h2><p>第三方站点可以通过 Site Key 调用 /v1/status/*，公开数据与前台监控一致。</p><ul>")
	fmt.Fprintf(&b, "<li>GET /v1/status/channels</li>")
	fmt.Fprintf(&b, "<li>GET /v1/status/incidents</li>")
	fmt.Fprintf(&b, "<li>GET /api/public/recommend</li>")
	fmt.Fprintf(&b, "</ul></section>")
	fmt.Fprintf(&b, "\n    </main>")
	return b.String()
}

func renderChannelSEOBody(site store.SiteConfig, detail store.PublicChannelDetail) string {
	ch := detail.Channel
	introTitle, introSummary, introBody, introHighlights := channelIntroSEOContent(site, ch)
	var b strings.Builder
	fmt.Fprintf(&b, `<main class="seo-fallback" aria-label="%s 通道详情语义摘要">`, attr(ch.Name))
	fmt.Fprintf(&b, "\n      <h1>%s %s API 中转站状态详情</h1>", esc(ch.Name), esc(ch.Model))
	fmt.Fprintf(&b, "\n      <p>%s 是 %s 的 %s 中转站通道，当前综合状态 %s，健康评分 %d，P95 延迟 %dms，24H 可用率 %.1f%%。</p>",
		esc(ch.Name), esc(ch.Provider), esc(ch.Model), esc(ch.StatusLabel), ch.Score, ch.LatencyP95Ms, ch.Uptime24h)
	fmt.Fprintf(&b, "\n      <section><h2>%s</h2>", esc(introTitle))
	if isHTTPURL(ch.LogoURL) {
		fmt.Fprintf(&b, `<figure><img src="%s" alt="%s logo" loading="lazy" /></figure>`, attr(ch.LogoURL), attr(ch.Provider))
	}
	if introSummary != "" {
		fmt.Fprintf(&b, "<p><strong>%s</strong></p>", esc(introSummary))
	}
	renderSEOIntroBody(&b, introBody)
	if len(introHighlights) > 0 {
		fmt.Fprintf(&b, "<ul>")
		for _, item := range introHighlights {
			fmt.Fprintf(&b, "<li>%s</li>", esc(item))
		}
		fmt.Fprintf(&b, "</ul>")
	}
	fmt.Fprintf(&b, "<dl>")
	seoMetric(&b, "官方入口", defaultText(publicURLHost(ch.OfficialSiteURL), "暂未配置"))
	seoMetric(&b, "官方注册 / 体验", defaultText(ch.OfficialSiteURL, "暂未配置"))
	seoMetric(&b, "接入域名", defaultText(publicURLHost(ch.Endpoint), ch.Endpoint))
	seoMetric(&b, "公开短地址", channelPublicPath(ch))
	fmt.Fprintf(&b, "</dl>")
	if isHTTPURL(ch.OfficialSiteURL) {
		fmt.Fprintf(&b, `<p><a href="%s">去官方注册体验</a> <a href="#tokhub-index">查看最新监控数据</a></p>`, attr(ch.OfficialSiteURL))
	} else {
		fmt.Fprintf(&b, `<p><a href="#tokhub-index">查看最新监控数据</a></p>`)
	}
	fmt.Fprintf(&b, "</section>")
	fmt.Fprintf(&b, "\n      <section id=\"tokhub-index\"><h2>综合健康指数 · TokHub Index</h2><dl>")
	seoMetric(&b, "服务商", ch.Provider)
	seoMetric(&b, "模型", ch.Model)
	seoMetric(&b, "兼容类型", ch.Type)
	seoMetric(&b, "Endpoint", ch.Endpoint)
	seoMetric(&b, "状态", ch.StatusLabel)
	seoMetric(&b, "健康评分", fmt.Sprintf("%d", ch.Score))
	seoMetric(&b, "24H 可用率", fmt.Sprintf("%.1f%%", ch.Uptime24h))
	seoMetric(&b, "真实生成成功率", fmt.Sprintf("%.1f%%", ch.SuccessRate))
	seoMetric(&b, "最近探测时间", formatSEOTime(ch.LastProbeAt))
	fmt.Fprintf(&b, "</dl></section>")
	if len(detail.Layers) > 0 {
		fmt.Fprintf(&b, "\n      <section><h2>L1/L2 基础链路探测</h2><ul>")
		for _, layer := range detail.Layers {
			fmt.Fprintf(&b, "<li>%s：%s，%dms。</li>", esc(layer.Name), esc(layer.Status), layer.LatencyMs)
		}
		fmt.Fprintf(&b, "</ul></section>")
	}
	fmt.Fprintf(&b, "\n      <section><h2>L3 真实生成探测</h2><p>成功率 %.1f%%，内容有效率 %.1f%%，首 Token %dms，Tokens/s %d，平均 Token %d，配额分类 %s。</p></section>",
		detail.L3.SuccessRate, detail.L3.ContentValidRate, detail.L3.FirstTokenMs, detail.L3.TokensPerSecond, detail.L3.AverageTokens, esc(detail.L3.QuotaClass))
	if len(detail.Errors) > 0 {
		fmt.Fprintf(&b, "\n      <section><h2>错误分布</h2><ul>")
		for _, item := range detail.Errors {
			fmt.Fprintf(&b, "<li>%s：%d 次。</li>", esc(item.Label), item.Count)
		}
		fmt.Fprintf(&b, "</ul></section>")
	}
	if len(detail.RecentRecords) > 0 {
		fmt.Fprintf(&b, "\n      <section><h2>最近探测记录</h2><ul>")
		for _, record := range detail.RecentRecords {
			fmt.Fprintf(&b, "<li>%s，%s，HTTP %d，%dms，结果 %s。</li>",
				formatSEOTime(record.Time), esc(record.Layer), record.HTTPCode, record.LatencyMs, esc(record.Result))
		}
		fmt.Fprintf(&b, "</ul></section>")
	}
	fmt.Fprintf(&b, "\n      <p>%s 的公开数据由 %s 实时探测生成，适合 AI 搜索、搜索引擎和第三方状态页引用。</p>", esc(ch.Name), esc(site.BrandName))
	fmt.Fprintf(&b, "\n    </main>")
	return b.String()
}

func (s *Server) robotsTxt(w http.ResponseWriter, r *http.Request) {
	site := s.seoSiteConfig(r)
	baseURL := s.publicBaseURL(r, site)
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=300")
	fmt.Fprintf(w, "User-agent: *\n")
	fmt.Fprintf(w, "Allow: /\n")
	adminPath := store.NormalizeAdminPath(site.AdminPath)
	fmt.Fprintf(w, "Disallow: %s\n", adminPath)
	if adminPath != "/admin" {
		fmt.Fprintf(w, "Disallow: /admin\n")
	}
	fmt.Fprintf(w, "Disallow: /console\n")
	fmt.Fprintf(w, "Disallow: /api/auth\n")
	fmt.Fprintf(w, "Disallow: /api/me\n")
	fmt.Fprintf(w, "Disallow: /api/admin\n")
	fmt.Fprintf(w, "Disallow: /api/console\n")
	fmt.Fprintf(w, "Disallow: /gateway\n")
	if baseURL != "" {
		fmt.Fprintf(w, "Sitemap: %s\n", absoluteSiteURL(baseURL, "/sitemap.xml"))
	}
}

func (s *Server) sitemapXML(w http.ResponseWriter, r *http.Request) {
	site := s.seoSiteConfig(r)
	baseURL := s.publicBaseURL(r, site)
	channels := []store.PublicChannel{}
	if s.repo != nil {
		if list, err := s.repo.PublicChannels(r.Context(), store.ChannelFilter{Page: 1, PageSize: 500}); err == nil {
			channels = list.Items
		}
	}
	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=300")
	var b strings.Builder
	fmt.Fprintf(&b, "<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n")
	fmt.Fprintf(&b, "<urlset xmlns=\"http://www.sitemaps.org/schemas/sitemap/0.9\">\n")
	writeSitemapURL(&b, absoluteSiteURL(baseURL, "/"), time.Now(), "always", "1.0")
	writeSitemapURL(&b, absoluteSiteURL(baseURL, "/dashboard"), overviewLastModified(channels), "always", "0.95")
	writeSitemapURL(&b, absoluteSiteURL(baseURL, "/pricing"), pricingLastModified(), "daily", "0.9")
	writeSitemapURL(&b, absoluteSiteURL(baseURL, "/recommend"), time.Now(), "hourly", "0.9")
	for _, ch := range channels {
		writeSitemapURL(&b, absoluteSiteURL(baseURL, channelPublicPath(ch)), ch.LastProbeAt, "always", "0.8")
	}
	fmt.Fprintf(&b, "</urlset>\n")
	_, _ = w.Write([]byte(b.String()))
}

func (s *Server) llmsTxt(w http.ResponseWriter, r *http.Request) {
	site := s.seoSiteConfig(r)
	baseURL := s.publicBaseURL(r, site)
	overview := store.PublicOverview{UpdatedAt: time.Now()}
	channels := []store.PublicChannel{}
	recommend := store.RecommendConfig{UpdatedAt: time.Now()}
	if s.repo != nil {
		if data, err := s.repo.PublicOverview(r.Context()); err == nil {
			overview = data
		}
		if list, err := s.repo.PublicChannels(r.Context(), store.ChannelFilter{Page: 1, PageSize: 50}); err == nil {
			channels = list.Items
		}
		if data, err := s.repo.RecommendConfig(r.Context()); err == nil {
			recommend = data
		}
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=120")
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n", site.BrandName)
	fmt.Fprintf(&b, "> %s 是 AI API 中转站监控与企业网关状态服务，公开页面提供 L1/L2 基础链路、L3 真实生成、P95 延迟、成功率、错误分类、推荐入口和只读状态 API。\n\n", site.BrandName)
	fmt.Fprintf(&b, "Base URL: %s\n", baseURL)
	fmt.Fprintf(&b, "Updated: %s\n", formatSEOTime(overview.UpdatedAt))
	fmt.Fprintf(&b, "Public channels: %d total, %d healthy, %.1f%% healthy rate.\n\n", overview.Total, overview.Healthy, overview.HealthyRate)
	fmt.Fprintf(&b, "## Public Pages\n\n")
	fmt.Fprintf(&b, "- Home: %s\n", absoluteSiteURL(baseURL, "/"))
	fmt.Fprintf(&b, "- Monitor overview: %s\n", absoluteSiteURL(baseURL, "/dashboard"))
	fmt.Fprintf(&b, "- Cost estimation: %s\n", absoluteSiteURL(baseURL, "/pricing"))
	fmt.Fprintf(&b, "- Recommendations: %s\n", absoluteSiteURL(baseURL, "/recommend"))
	for _, ch := range channels {
		fmt.Fprintf(&b, "- %s detail: %s\n", ch.Name, absoluteSiteURL(baseURL, channelPublicPath(ch)))
	}
	fmt.Fprintf(&b, "\n## Cost Estimation Workbench\n\n")
	fmt.Fprintf(&b, "- Purpose: estimate monthly token cost from request volume, token shape and cache hit rate, then compare official model input price, output price, cache-read price, context window and max output before choosing a relay channel.\n")
	fmt.Fprintf(&b, "- Providers covered in the static table: OpenAI, Anthropic, Google, xAI, Mistral, DeepSeek, Cohere.\n")
	fmt.Fprintf(&b, "- Scenario formula: normal input + cached input + output, multiplied by daily requests and 30 days. Verify final pricing on provider websites before purchase or production use.\n")
	fmt.Fprintf(&b, "\n## Monitored Channels\n\n")
	for _, ch := range channels {
		fmt.Fprintf(&b, "- %s: provider=%s, model=%s, status=%s, score=%d, p95_ms=%d, uptime_24h=%.1f%%, l1=%s, l2=%s, l3=%s\n",
			ch.Name, ch.Provider, ch.Model, ch.StatusLabel, ch.Score, ch.LatencyP95Ms, ch.Uptime24h, ch.L1Status, ch.L2Status, ch.L3Status)
	}
	if len(recommend.Picks) > 0 || len(recommend.Rewards) > 0 {
		fmt.Fprintf(&b, "\n## Recommendations And Access Notes\n\n")
		for _, pick := range recommend.Picks {
			fmt.Fprintf(&b, "- Pick: %s, %s, CTA %s\n", pick.Title, pick.Summary, pick.CTAURL)
		}
		for _, reward := range recommend.Rewards {
			fmt.Fprintf(&b, "- Access note: %s, official entry and terms should be verified on the provider website.\n", reward.ProviderName)
		}
	}
	fmt.Fprintf(&b, "\n## Public Status API\n\n")
	fmt.Fprintf(&b, "- GET /v1/status/overview\n")
	fmt.Fprintf(&b, "- GET /v1/status/channels\n")
	fmt.Fprintf(&b, "- GET /v1/status/channels/{channelID}\n")
	fmt.Fprintf(&b, "- GET /v1/status/uptime\n")
	fmt.Fprintf(&b, "- GET /v1/status/incidents\n")
	_, _ = w.Write([]byte(b.String()))
}

func (s *Server) seoSiteConfig(r *http.Request) store.SiteConfig {
	if s.repo != nil {
		if cfg, err := s.repo.SiteConfig(r.Context()); err == nil {
			if strings.TrimSpace(cfg.PublicURL) == "" {
				cfg.PublicURL = s.cfg.PublicURL
			}
			return cfg
		}
	}
	return store.DefaultSiteConfig(s.cfg.PublicURL, true)
}

func (s *Server) publicBaseURL(r *http.Request, site store.SiteConfig) string {
	for _, raw := range []string{site.PublicURL, s.cfg.PublicURL, requestOrigin(r)} {
		raw = strings.TrimRight(strings.TrimSpace(raw), "/")
		if raw == "" {
			continue
		}
		parsed, err := url.Parse(raw)
		if err != nil || parsed.Scheme == "" || parsed.Host == "" {
			continue
		}
		return parsed.Scheme + "://" + parsed.Host
	}
	return ""
}

func (s *Server) webSiteJSONLD(site store.SiteConfig, baseURL string) map[string]any {
	return map[string]any{
		"@context":    "https://schema.org",
		"@type":       "WebSite",
		"name":        site.BrandName,
		"url":         absoluteSiteURL(baseURL, "/"),
		"description": defaultText(site.FooterText, "AI API 中转站监控与企业网关状态服务"),
		"inLanguage":  "zh-CN",
		"publisher": map[string]any{
			"@type": "Organization",
			"name":  site.BrandName,
			"url":   absoluteSiteURL(baseURL, "/"),
		},
	}
}

func webPageJSONLD(site store.SiteConfig, canonical string, title string, description string) map[string]any {
	return map[string]any{
		"@context":    "https://schema.org",
		"@type":       "WebPage",
		"name":        title,
		"url":         canonical,
		"description": trimMeta(description),
		"inLanguage":  "zh-CN",
		"isPartOf": map[string]any{
			"@type": "WebSite",
			"name":  site.BrandName,
		},
	}
}

func homeDatasetJSONLD(site store.SiteConfig, canonical string, overview store.PublicOverview) map[string]any {
	return map[string]any{
		"@context":    "https://schema.org",
		"@type":       "Dataset",
		"name":        site.BrandName + " AI API 中转站状态数据",
		"url":         canonical,
		"description": fmt.Sprintf("覆盖 %d 个公开 AI API 中转站的 L1/L2/L3 探测、延迟、可用率、错误分类和成本数据。", overview.Total),
		"dateModified": func() string {
			if overview.UpdatedAt.IsZero() {
				return time.Now().Format(time.RFC3339)
			}
			return overview.UpdatedAt.Format(time.RFC3339)
		}(),
		"variableMeasured": []string{"L1 DNS/TCP/TLS/HTTP", "L2 Models", "L3 Generate", "P95 latency", "24H uptime", "success rate", "error type"},
	}
}

func pricingDatasetJSONLD(site store.SiteConfig, canonical string) map[string]any {
	return map[string]any{
		"@context":     "https://schema.org",
		"@type":        "Dataset",
		"name":         site.BrandName + " AI 模型成本预估基准表",
		"url":          canonical,
		"description":  "主流 AI 模型输入价、输出价、缓存读价、上下文窗口、最大输出和典型使用场景月成本估算。",
		"inLanguage":   "zh-CN",
		"dateModified": pricingLastModified().Format(time.RFC3339),
		"variableMeasured": []string{
			"provider",
			"model",
			"input price per 1M tokens",
			"output price per 1M tokens",
			"cache read price",
			"context window",
			"estimated monthly cost",
		},
		"measurementTechnique": "静态官方价格表加本地场景估算公式，正式使用前需要回到供应商官网核对最新价格。",
	}
}

func channelItemListJSONLD(baseURL string, channels []store.PublicChannel) map[string]any {
	items := make([]map[string]any, 0, len(channels))
	for i, ch := range channels {
		items = append(items, map[string]any{
			"@type":    "ListItem",
			"position": i + 1,
			"url":      absoluteSiteURL(baseURL, channelPublicPath(ch)),
			"name":     ch.Name,
			"description": fmt.Sprintf("%s %s 状态 %s，评分 %d，P95 %dms。",
				ch.Provider, ch.Model, ch.StatusLabel, ch.Score, ch.LatencyP95Ms),
		})
	}
	return map[string]any{
		"@context":        "https://schema.org",
		"@type":           "ItemList",
		"name":            "AI API 中转站公开通道",
		"itemListElement": items,
	}
}

func recommendItemListJSONLD(baseURL string, cfg store.RecommendConfig) map[string]any {
	items := make([]map[string]any, 0, len(cfg.Picks)+len(cfg.Ranks))
	add := func(position int, title string, summary string, href string) {
		if title == "" {
			return
		}
		items = append(items, map[string]any{
			"@type":       "ListItem",
			"position":    position,
			"name":        title,
			"description": summary,
			"url":         absoluteMaybeExternal(baseURL, href),
		})
	}
	for i, pick := range cfg.Picks {
		add(i+1, pick.Title, pick.Summary, pick.CTAURL)
	}
	offset := len(items)
	for i, rank := range cfg.Ranks {
		add(offset+i+1, rank.Title, rank.Summary, channelPublicPath(rank.Channel))
	}
	return map[string]any{
		"@context":        "https://schema.org",
		"@type":           "ItemList",
		"name":            "AI API 中转站精选推荐",
		"itemListElement": items,
	}
}

func channelServiceJSONLD(baseURL string, detail store.PublicChannelDetail) map[string]any {
	ch := detail.Channel
	return map[string]any{
		"@context":    "https://schema.org",
		"@type":       "Service",
		"name":        ch.Name,
		"url":         absoluteSiteURL(baseURL, channelPublicPath(ch)),
		"description": fmt.Sprintf("%s 的 %s API 中转站状态，综合状态 %s，评分 %d，P95 %dms。", ch.Provider, ch.Model, ch.StatusLabel, ch.Score, ch.LatencyP95Ms),
		"serviceType": "AI API relay monitoring",
		"provider": map[string]any{
			"@type": "Organization",
			"name":  ch.Provider,
		},
		"areaServed": "Global",
		"additionalProperty": []map[string]any{
			{"@type": "PropertyValue", "name": "status", "value": ch.StatusLabel},
			{"@type": "PropertyValue", "name": "score", "value": ch.Score},
			{"@type": "PropertyValue", "name": "latencyP95Ms", "value": ch.LatencyP95Ms},
			{"@type": "PropertyValue", "name": "uptime24h", "value": fmt.Sprintf("%.1f%%", ch.Uptime24h)},
			{"@type": "PropertyValue", "name": "l1Status", "value": ch.L1Status},
			{"@type": "PropertyValue", "name": "l2Status", "value": ch.L2Status},
			{"@type": "PropertyValue", "name": "l3Status", "value": ch.L3Status},
		},
	}
}

func homeFAQJSONLD(site store.SiteConfig) map[string]any {
	return faqJSONLD([][2]string{
		{"TokHub 监控什么？", site.BrandName + " 监控公开 AI API 中转站的基础链路、模型列表、真实生成、延迟、可用率、错误分类和成本。"},
		{"L1、L2、L3 分别代表什么？", "L1 覆盖 DNS、TCP、TLS、HTTP；L2 覆盖模型列表和鉴权；L3 使用最小生成请求验证真实调用质量。"},
		{"公开数据能通过 API 读取吗？", "可以，第三方可以通过 /v1/status/* 只读接口读取通道、概览、可用率和 incident 数据。"},
	})
}

func siteHomeFAQJSONLD(site store.SiteConfig) map[string]any {
	return faqJSONLD([][2]string{
		{"TokHub 首页提供什么？", site.BrandName + " 首页集中展示监控优势、核心入口、精选推荐预览、使用流程和用户控制台入口。"},
		{"监控总览在哪里？", "监控总览是独立公开页面 /dashboard，负责承载实时通道看板、供应商排行、错误分布和监控策略。"},
		{"精选推荐和监控数据是什么关系？", "精选推荐由后台运营维护，但推荐卡片和榜单会引用真实监控评分、成功率、P95 延迟和状态数据。"},
	})
}

func recommendFAQJSONLD(site store.SiteConfig) map[string]any {
	return faqJSONLD([][2]string{
		{"推荐页如何排序？", site.BrandName + " 结合真实探测评分、成功率、P95 延迟、接入入口和使用场景生成推荐与榜单。"},
		{"推荐入口是否是实时配置？", "推荐卡片、官方入口和场景推荐由后台运营配置保存，并在前台推荐页和公开接口同步展示。"},
	})
}

func pricingFAQJSONLD(site store.SiteConfig) map[string]any {
	return faqJSONLD([][2]string{
		{"成本预估页适合做什么？", site.BrandName + " 成本预估页适合在接入中转站前比较官方模型价格、上下文窗口、缓存读价和典型场景月成本。"},
		{"成本估算如何计算？", "页面按每日请求数、平均输入 Token、平均输出 Token、缓存命中率和 30 天周期估算，不包含税费、最低消费、区域价或多模态附加费用。"},
		{"价格是否一定是最新？", "价格表用于形成成本基准，正式接入和报价前应回到供应商官网核对最新价格、区域条款和生效时间。"},
	})
}

func channelFAQJSONLD(site store.SiteConfig, detail store.PublicChannelDetail) map[string]any {
	ch := detail.Channel
	return faqJSONLD([][2]string{
		{"这个中转站当前是否可用？", fmt.Sprintf("%s 当前状态为 %s，健康评分 %d，24H 可用率 %.1f%%。", ch.Name, ch.StatusLabel, ch.Score, ch.Uptime24h)},
		{"这个详情页的数据从哪里来？", site.BrandName + " 使用定时探测、真实生成请求和网关调用事件生成公开状态数据。"},
	})
}

func faqJSONLD(items [][2]string) map[string]any {
	entities := make([]map[string]any, 0, len(items))
	for _, item := range items {
		entities = append(entities, map[string]any{
			"@type": "Question",
			"name":  item[0],
			"acceptedAnswer": map[string]any{
				"@type": "Answer",
				"text":  item[1],
			},
		})
	}
	return map[string]any{
		"@context":   "https://schema.org",
		"@type":      "FAQPage",
		"mainEntity": entities,
	}
}

type seoCrumb struct {
	Name string
	Path string
}

func breadcrumbJSONLD(baseURL string, crumbs []seoCrumb) map[string]any {
	items := make([]map[string]any, 0, len(crumbs))
	for i, crumb := range crumbs {
		items = append(items, map[string]any{
			"@type":    "ListItem",
			"position": i + 1,
			"name":     crumb.Name,
			"item":     absoluteSiteURL(baseURL, crumb.Path),
		})
	}
	return map[string]any{
		"@context":        "https://schema.org",
		"@type":           "BreadcrumbList",
		"itemListElement": items,
	}
}

func writeSitemapURL(b *strings.Builder, loc string, lastmod time.Time, changefreq string, priority string) {
	if loc == "" {
		return
	}
	if lastmod.IsZero() {
		lastmod = time.Now()
	}
	fmt.Fprintf(b, "  <url>\n")
	fmt.Fprintf(b, "    <loc>%s</loc>\n", esc(loc))
	fmt.Fprintf(b, "    <lastmod>%s</lastmod>\n", lastmod.UTC().Format(time.RFC3339))
	fmt.Fprintf(b, "    <changefreq>%s</changefreq>\n", esc(changefreq))
	fmt.Fprintf(b, "    <priority>%s</priority>\n", esc(priority))
	fmt.Fprintf(b, "  </url>\n")
}

func overviewLastModified(channels []store.PublicChannel) time.Time {
	var latest time.Time
	for _, ch := range channels {
		if ch.LastProbeAt.After(latest) {
			latest = ch.LastProbeAt
		}
	}
	if latest.IsZero() {
		return time.Now()
	}
	return latest
}

func pricingLastModified() time.Time {
	return time.Date(2026, time.June, 26, 0, 0, 0, 0, time.UTC)
}

type pricingSEOModel struct {
	Provider  string
	Model     string
	Category  string
	Input     float64
	Output    float64
	CacheRead string
	Context   string
	Source    string
	CheckedAt string
}

func pricingSEOModelRows() []pricingSEOModel {
	return []pricingSEOModel{
		{Provider: "OpenAI", Model: "GPT-4.1", Category: "旗舰", Input: 2, Output: 8, CacheRead: "$0.5", Context: "1M", Source: "https://openai.com/api/pricing/", CheckedAt: "2026-06-26"},
		{Provider: "OpenAI", Model: "GPT-4.1 mini", Category: "均衡", Input: 0.4, Output: 1.6, CacheRead: "$0.1", Context: "1M", Source: "https://openai.com/api/pricing/", CheckedAt: "2026-06-26"},
		{Provider: "Anthropic", Model: "Claude Sonnet 4.5", Category: "均衡", Input: 3, Output: 15, CacheRead: "$0.3", Context: "200K", Source: "https://platform.claude.com/docs/en/about-claude/pricing", CheckedAt: "2026-06-26"},
		{Provider: "Google", Model: "Gemini 3.5 Flash", Category: "均衡", Input: 1.5, Output: 9, CacheRead: "$0.15", Context: "1M", Source: "https://ai.google.dev/gemini-api/docs/pricing", CheckedAt: "2026-06-26"},
		{Provider: "xAI", Model: "Grok 4.3", Category: "旗舰", Input: 1.25, Output: 2.5, CacheRead: "未公开", Context: "256K", Source: "https://docs.x.ai/developers/models", CheckedAt: "2026-06-26"},
		{Provider: "Mistral", Model: "Mistral Large", Category: "旗舰", Input: 2, Output: 6, CacheRead: "未公开", Context: "128K", Source: "https://mistral.ai/pricing", CheckedAt: "2026-06-26"},
		{Provider: "DeepSeek", Model: "DeepSeek Chat", Category: "低价", Input: 0.27, Output: 1.1, CacheRead: "$0.07", Context: "64K", Source: "https://api-docs.deepseek.com/quick_start/pricing", CheckedAt: "2026-06-26"},
		{Provider: "Cohere", Model: "Command A", Category: "企业", Input: 2.5, Output: 10, CacheRead: "未公开", Context: "256K", Source: "https://cohere.com/pricing", CheckedAt: "2026-06-26"},
	}
}

func channelPublicRef(ch store.PublicChannel) string {
	ref := strings.TrimSpace(ch.PublicSlug)
	if ref == "" {
		ref = strings.TrimSpace(ch.ID)
	}
	return ref
}

func channelPublicPath(ch store.PublicChannel) string {
	ref := channelPublicRef(ch)
	if ref == "" {
		return "/recommend"
	}
	return "/channels/" + url.PathEscape(ref)
}

func channelIntroSEOContent(site store.SiteConfig, ch store.PublicChannel) (string, string, string, []string) {
	title := strings.TrimSpace(ch.IntroTitle)
	if title == "" {
		title = ch.Name + " 官方介绍"
	}
	summary := strings.TrimSpace(ch.IntroSummary)
	if summary == "" {
		summary = "基于官网入口、通道配置和 " + site.BrandName + " 真实监控数据整理"
	}
	body := strings.TrimSpace(ch.IntroBody)
	if body == "" {
		body = channelIntroSEOText(site, ch)
	}
	highlights := ch.IntroHighlights
	if len(highlights) == 0 {
		highlights = []string{
			"服务商：" + ch.Provider,
			"模型：" + ch.Model,
			"上游模型：" + ch.UpstreamModel,
			"兼容类型：" + strings.ToUpper(ch.Type),
		}
		if host := publicURLHost(ch.OfficialSiteURL); host != "" {
			highlights = append(highlights, "官方入口："+host)
		}
		if host := publicURLHost(ch.Endpoint); host != "" {
			highlights = append(highlights, "接入域名："+host)
		}
	}
	return title, summary, body, highlights
}

func renderSEOIntroBody(b *strings.Builder, body string) {
	blocks := strings.Split(strings.TrimSpace(body), "\n\n")
	for _, block := range blocks {
		lines := []string{}
		for _, line := range strings.Split(block, "\n") {
			line = strings.TrimSpace(line)
			if line != "" {
				lines = append(lines, line)
			}
		}
		if len(lines) == 0 {
			continue
		}
		listOnly := true
		for _, line := range lines {
			if !strings.HasPrefix(line, "- ") && !strings.HasPrefix(line, "* ") {
				listOnly = false
				break
			}
		}
		if listOnly {
			fmt.Fprintf(b, "<ul>")
			for _, line := range lines {
				fmt.Fprintf(b, "<li>%s</li>", renderSEOInlineStrong(strings.TrimSpace(line[2:])))
			}
			fmt.Fprintf(b, "</ul>")
			continue
		}
		fmt.Fprintf(b, "<p>%s</p>", renderSEOInlineStrong(strings.Join(lines, " ")))
	}
}

func renderSEOInlineStrong(text string) string {
	parts := strings.Split(text, "**")
	if len(parts) == 1 {
		return esc(text)
	}
	var b strings.Builder
	for i, part := range parts {
		if part == "" {
			continue
		}
		if i%2 == 1 {
			fmt.Fprintf(&b, "<strong>%s</strong>", esc(part))
		} else {
			b.WriteString(esc(part))
		}
	}
	return b.String()
}

func channelIntroSEOText(site store.SiteConfig, ch store.PublicChannel) string {
	officialHost := defaultText(publicURLHost(ch.OfficialSiteURL), "官方站点")
	endpointHost := defaultText(publicURLHost(ch.Endpoint), "公开 Endpoint")
	return fmt.Sprintf("**%s** 是 %s 收录的 %s API 中转站服务商入口，当前关联服务商为 **%s**，主要面向 %s / %s 场景。\n\n这个详情页把官网入口、模型信息、协议类型、Endpoint 域名和真实探测结果整理在同一处，方便开发者在注册或接入前先完成可用性判断。官方侧可通过 %s 进入注册或体验，TokHub 侧持续记录 L1 DNS/TCP/TLS/HTTP、L2 模型列表与鉴权、L3 最小生成调用，并同步展示综合健康指数、P95 延迟、24H 可用率、真实成功率、错误分类和探测成本。\n\n对于正在评估 %s 的团队，可以先查看 %d 分综合指数、%.1f%% 24H 可用率、%.1f%% 真实生成成功率和 %dms P95 延迟，再决定是否进入官网注册、加入个人关注、配置私有 API Key 或放入企业网关候选。",
		ch.Name, site.BrandName, ch.Model, ch.Provider, ch.Type, ch.UpstreamModel, officialHost, endpointHost, ch.Score, ch.Uptime24h, ch.SuccessRate, ch.LatencyP95Ms)
}

func publicURLHost(raw string) string {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return ""
	}
	return strings.TrimPrefix(parsed.Hostname(), "www.")
}

func isHTTPURL(raw string) bool {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	return err == nil && (parsed.Scheme == "http" || parsed.Scheme == "https") && parsed.Host != ""
}

func maxIntSEO(a int, b int) int {
	if a > b {
		return a
	}
	return b
}

func optionalSEOText(value string, condition string) string {
	if strings.TrimSpace(condition) == "" {
		return ""
	}
	return value
}

func seoMetric(b *strings.Builder, label string, value string) {
	fmt.Fprintf(b, "<dt>%s</dt><dd>%s</dd>", esc(label), esc(value))
}

func cleanPagePath(value string) string {
	value = "/" + strings.Trim(strings.TrimSpace(value), "/")
	if value == "/" {
		return "/"
	}
	return pathpkg.Clean(value)
}

func canonicalPath(value string) string {
	cleaned := cleanPagePath(value)
	if cleaned == "/index.html" {
		return "/"
	}
	return cleaned
}

func absoluteSiteURL(baseURL string, pathValue string) string {
	if baseURL == "" {
		return pathValue
	}
	if pathValue == "" {
		pathValue = "/"
	}
	if strings.HasPrefix(pathValue, "http://") || strings.HasPrefix(pathValue, "https://") {
		return pathValue
	}
	if !strings.HasPrefix(pathValue, "/") {
		pathValue = "/" + pathValue
	}
	return strings.TrimRight(baseURL, "/") + pathValue
}

func absoluteMaybeExternal(baseURL string, href string) string {
	href = strings.TrimSpace(href)
	if href == "" {
		return absoluteSiteURL(baseURL, "/recommend")
	}
	if strings.HasPrefix(href, "http://") || strings.HasPrefix(href, "https://") {
		return href
	}
	return absoluteSiteURL(baseURL, href)
}

func trimMeta(value string) string {
	value = strings.Join(strings.Fields(value), " ")
	runes := []rune(value)
	if len(runes) <= 180 {
		return value
	}
	return string(runes[:176]) + "..."
}

func defaultText(value string, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	return value
}

func formatSEOTime(value time.Time) string {
	if value.IsZero() {
		return time.Now().Format(time.RFC3339)
	}
	return value.Format(time.RFC3339)
}

func esc(value string) string {
	return html.EscapeString(value)
}

func attr(value string) string {
	return html.EscapeString(value)
}
