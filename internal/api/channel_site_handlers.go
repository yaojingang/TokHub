package api

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	gatewaycache "tokhub/internal/gateway"
	"tokhub/internal/store"
)

type channelSiteRuntimeAuth struct {
	site   store.ChannelSite
	status int
	start  time.Time
}

func (s *Server) adminChannelSites(w http.ResponseWriter, r *http.Request) {
	summary, err := s.repo.ChannelSitesSummary(r.Context(), s.cfg.PublicURL)
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "channel_sites_unavailable", "Could not load channel sites")
		return
	}
	writeJSON(w, http.StatusOK, summary)
}

func (s *Server) createChannelSite(w http.ResponseWriter, r *http.Request) {
	user, _ := s.userFromRequest(r)
	var input store.ChannelSiteInput
	if err := decodeJSON(r, &input); err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid_json", "Invalid channel site payload")
		return
	}
	input.ActorID = user.ID
	site, err := s.repo.CreateChannelSite(r.Context(), input)
	if err != nil {
		s.writeChannelSiteError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"site": site})
}

func (s *Server) patchChannelSite(w http.ResponseWriter, r *http.Request) {
	user, _ := s.userFromRequest(r)
	var input store.ChannelSiteInput
	if err := decodeJSON(r, &input); err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid_json", "Invalid channel site payload")
		return
	}
	input.ActorID = user.ID
	site, err := s.repo.UpdateChannelSite(r.Context(), chi.URLParam(r, "siteID"), input)
	if err != nil {
		s.writeChannelSiteError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"site": site})
}

func (s *Server) deleteChannelSite(w http.ResponseWriter, r *http.Request) {
	user, _ := s.userFromRequest(r)
	site, err := s.repo.DeleteChannelSite(r.Context(), chi.URLParam(r, "siteID"), user.ID)
	if err != nil {
		s.writeChannelSiteError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"site": site})
}

func (s *Server) rotateChannelSiteKey(w http.ResponseWriter, r *http.Request) {
	user, _ := s.userFromRequest(r)
	site, err := s.repo.RotateChannelSiteKey(r.Context(), chi.URLParam(r, "siteID"), user.ID)
	if err != nil {
		s.writeChannelSiteError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"site": site})
}

func (s *Server) buildChannelSitePackage(w http.ResponseWriter, r *http.Request) {
	user, _ := s.userFromRequest(r)
	site, err := s.repo.RotateChannelSiteKey(r.Context(), chi.URLParam(r, "siteID"), user.ID)
	if err != nil {
		s.writeChannelSiteError(w, r, err)
		return
	}
	zipBytes, fileName, err := s.buildChannelSiteZip(r.Context(), site)
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "channel_site_package_failed", "Could not build channel site package")
		return
	}
	export, err := s.repo.CreateChannelSitePackageExport(r.Context(), site.ID, fileName, len(zipBytes), user.ID)
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "channel_site_package_export_failed", "Could not record channel site package export")
		return
	}
	site.PackageExports = append([]store.ChannelSitePackageExport{export}, site.PackageExports...)
	writeJSON(w, http.StatusOK, map[string]any{"site": site, "export": export})
}

func (s *Server) downloadChannelSitePackage(w http.ResponseWriter, r *http.Request) {
	user, _ := s.userFromRequest(r)
	site, err := s.repo.RotateChannelSiteKey(r.Context(), chi.URLParam(r, "siteID"), user.ID)
	if err != nil {
		s.writeChannelSiteError(w, r, err)
		return
	}
	zipBytes, fileName, err := s.buildChannelSiteZip(r.Context(), site)
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "channel_site_package_failed", "Could not build channel site package")
		return
	}
	if _, err := s.repo.CreateChannelSitePackageExport(r.Context(), site.ID, fileName, len(zipBytes), user.ID); err != nil {
		writeError(w, r, http.StatusInternalServerError, "channel_site_package_export_failed", "Could not record channel site package export")
		return
	}
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, fileName))
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(zipBytes)
}

func (s *Server) writeChannelSiteError(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, store.ErrInvalidChannelSite) {
		writeError(w, r, http.StatusBadRequest, "channel_site_invalid", err.Error())
		return
	}
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, r, http.StatusNotFound, "channel_site_not_found", "Channel site not found")
		return
	}
	writeError(w, r, http.StatusInternalServerError, "channel_site_unavailable", "Channel site operation failed")
}

func (s *Server) channelSiteConfig(w http.ResponseWriter, r *http.Request) {
	authn, ok := s.authenticateChannelSiteRuntime(w, r)
	if !ok {
		return
	}
	defer s.finishChannelSiteRequest(r, authn, "/config")
	payload := channelSiteSafeConfig(authn.site, s.channelSiteRuntimeBaseURL())
	authn.status = http.StatusOK
	writeJSON(w, http.StatusOK, payload)
}

func (s *Server) channelSiteOverview(w http.ResponseWriter, r *http.Request) {
	authn, ok := s.authenticateChannelSiteRuntime(w, r)
	if !ok {
		return
	}
	defer s.finishChannelSiteRequest(r, authn, "/overview")
	overview, err := s.repo.PublicOverview(r.Context())
	if err != nil {
		authn.status = http.StatusInternalServerError
		writeJSON(w, authn.status, errorPayload("overview_unavailable", "Could not load overview"))
		return
	}
	authn.status = http.StatusOK
	writeJSON(w, http.StatusOK, overview)
}

func (s *Server) channelSiteChannels(w http.ResponseWriter, r *http.Request) {
	authn, ok := s.authenticateChannelSiteRuntime(w, r)
	if !ok {
		return
	}
	defer s.finishChannelSiteRequest(r, authn, "/channels")
	filter := publicChannelFilter(r)
	if filter.PageSize <= 0 || filter.PageSize > 100 {
		filter.PageSize = 100
	}
	list, err := s.repo.PublicChannels(r.Context(), filter)
	if err != nil {
		authn.status = http.StatusInternalServerError
		writeJSON(w, authn.status, errorPayload("channels_unavailable", "Could not load channels"))
		return
	}
	authn.status = http.StatusOK
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) channelSiteRecommend(w http.ResponseWriter, r *http.Request) {
	authn, ok := s.authenticateChannelSiteRuntime(w, r)
	if !ok {
		return
	}
	defer s.finishChannelSiteRequest(r, authn, "/recommend")
	recommend, err := s.repo.RecommendConfig(r.Context())
	if err != nil {
		authn.status = http.StatusInternalServerError
		writeJSON(w, authn.status, errorPayload("recommend_unavailable", "Could not load recommend data"))
		return
	}
	authn.status = http.StatusOK
	writeJSON(w, http.StatusOK, recommend)
}

func (s *Server) authenticateChannelSiteRuntime(w http.ResponseWriter, r *http.Request) (channelSiteRuntimeAuth, bool) {
	authn := channelSiteRuntimeAuth{start: time.Now(), status: http.StatusForbidden}
	key := channelSiteRuntimeKey(r)
	if key == "" {
		writeError(w, r, http.StatusUnauthorized, "channel_site_unauthorized", "Channel site key is required")
		return authn, false
	}
	siteID := strings.TrimSpace(chi.URLParam(r, "siteID"))
	site, err := s.repo.AuthenticateChannelSite(r.Context(), siteID, key)
	if err != nil {
		writeError(w, r, http.StatusForbidden, "channel_site_forbidden", "Channel site key is invalid")
		return authn, false
	}
	authn.site = site.Site
	if !s.channelSiteOriginAllowed(r, authn.site) {
		writeError(w, r, http.StatusForbidden, "channel_site_origin_forbidden", "Origin is not allowed for this channel site")
		return authn, false
	}
	limit := authn.site.QPSLimit
	if limit <= 0 {
		limit = 60
	}
	limiterKey := "channel-site:" + authn.site.ID
	if s.gatewayCache != nil {
		if allowed, err := s.gatewayCache.AllowQPS(r.Context(), limiterKey, limit); err == nil {
			if !allowed {
				authn.status = http.StatusTooManyRequests
				s.finishChannelSiteRequest(r, authn, r.URL.Path)
				writeError(w, r, http.StatusTooManyRequests, "channel_site_rate_limited", "Channel site QPS limit exceeded")
				return authn, false
			}
		} else if errors.Is(err, gatewaycache.ErrUnavailable) || err != nil {
			if !s.allowRate(s.publicLimiter, limiterKey, limit, time.Second) {
				authn.status = http.StatusTooManyRequests
				s.finishChannelSiteRequest(r, authn, r.URL.Path)
				writeError(w, r, http.StatusTooManyRequests, "channel_site_rate_limited", "Channel site QPS limit exceeded")
				return authn, false
			}
		}
	} else if !s.allowRate(s.publicLimiter, limiterKey, limit, time.Second) {
		authn.status = http.StatusTooManyRequests
		s.finishChannelSiteRequest(r, authn, r.URL.Path)
		writeError(w, r, http.StatusTooManyRequests, "channel_site_rate_limited", "Channel site QPS limit exceeded")
		return authn, false
	}
	_ = s.repo.TouchChannelSite(r.Context(), authn.site.ID)
	return authn, true
}

func (s *Server) finishChannelSiteRequest(r *http.Request, authn channelSiteRuntimeAuth, endpoint string) {
	if authn.site.ID == "" {
		return
	}
	status := authn.status
	if status == 0 {
		status = http.StatusOK
	}
	_ = s.repo.RecordChannelSiteRuntimeCall(
		r.Context(),
		authn.site.ID,
		channelSiteRuntimeEndpoint(r, endpoint),
		status,
		int(time.Since(authn.start).Milliseconds()),
		channelSiteRequestOrigin(r),
		clientIP(r),
		r.UserAgent(),
	)
}

func channelSiteRuntimeKey(r *http.Request) string {
	if key := strings.TrimSpace(r.Header.Get("X-Channel-Site-Key")); key != "" {
		return key
	}
	if key := strings.TrimSpace(r.Header.Get("X-Site-Key")); key != "" {
		return key
	}
	header := strings.TrimSpace(r.Header.Get("Authorization"))
	if strings.HasPrefix(strings.ToLower(header), "bearer ") {
		return strings.TrimSpace(header[len("Bearer "):])
	}
	return strings.TrimSpace(r.URL.Query().Get("key"))
}

func (s *Server) channelSiteOriginAllowed(r *http.Request, site store.ChannelSite) bool {
	raw := channelSiteRequestOrigin(r)
	if raw == "" || raw == "null" {
		return true
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Hostname() == "" {
		return false
	}
	host := strings.ToLower(parsed.Hostname())
	if host == "localhost" || host == "127.0.0.1" || host == "::1" {
		return true
	}
	allowed := strings.ToLower(strings.TrimPrefix(strings.TrimSpace(site.Domain), "www."))
	if allowed == "" {
		return true
	}
	return host == allowed || strings.TrimPrefix(host, "www.") == allowed
}

func channelSiteRequestOrigin(r *http.Request) string {
	if origin := strings.TrimSpace(r.Header.Get("Origin")); origin != "" {
		return origin
	}
	if referer := strings.TrimSpace(r.Header.Get("Referer")); referer != "" {
		parsed, err := url.Parse(referer)
		if err == nil && parsed.Scheme != "" && parsed.Host != "" {
			return parsed.Scheme + "://" + parsed.Host
		}
	}
	return ""
}

func channelSiteRuntimeEndpoint(r *http.Request, fallback string) string {
	siteID := chi.URLParam(r, "siteID")
	prefix := "/site/v1/" + siteID
	if strings.HasPrefix(r.URL.Path, prefix) {
		return strings.TrimPrefix(r.URL.Path, prefix)
	}
	return fallback
}

func (s *Server) buildChannelSiteZip(ctx context.Context, site store.ChannelSite) ([]byte, string, error) {
	config, err := s.channelSitePackageConfig(ctx, site)
	if err != nil {
		return nil, "", err
	}
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	files := map[string]string{
		"index.html":              channelSiteHTML(site, config, false),
		"recommend/index.html":    channelSiteHTML(site, config, true),
		"assets/channel-site.css": channelSiteCSS(),
		"assets/channel-site.js":  channelSiteJS(),
		"site-config.json":        mustJSONIndent(config),
		"robots.txt":              channelSiteRobots(site),
		"sitemap.xml":             channelSiteSitemap(site),
		"llms.txt":                channelSiteLLMS(site),
		"README_DEPLOY.md":        channelSiteReadme(site, config),
	}
	for name, body := range files {
		f, err := zw.Create(name)
		if err != nil {
			_ = zw.Close()
			return nil, "", err
		}
		if _, err := f.Write([]byte(body)); err != nil {
			_ = zw.Close()
			return nil, "", err
		}
	}
	if err := zw.Close(); err != nil {
		return nil, "", err
	}
	fileName := fmt.Sprintf("tokhub-channel-site-%s-%s.zip", safeFilePart(site.Slug), time.Now().Format("20060102150405"))
	return buf.Bytes(), fileName, nil
}

func (s *Server) channelSitePackageConfig(ctx context.Context, site store.ChannelSite) (map[string]any, error) {
	overview, err := s.repo.PublicOverview(ctx)
	if err != nil {
		return nil, err
	}
	channels, err := s.repo.PublicChannels(ctx, store.ChannelFilter{Page: 1, PageSize: 100})
	if err != nil {
		return nil, err
	}
	providers, _ := s.repo.PublicProviderRank(ctx)
	errorsSummary, _ := s.repo.PublicErrorsSummary(ctx)
	recommend, _ := s.repo.RecommendConfig(ctx)
	return map[string]any{
		"site":        channelSiteSafeConfig(site, s.channelSiteRuntimeBaseURL()),
		"runtimeKey":  site.PlainRuntimeKey,
		"generatedAt": time.Now(),
		"initialData": map[string]any{
			"overview":   overview,
			"channels":   channels,
			"providers":  providers,
			"errors":     errorsSummary,
			"recommend":  recommend,
			"runtimeURL": strings.TrimRight(s.channelSiteRuntimeBaseURL(), "/") + "/" + site.ID,
		},
	}, nil
}

func channelSiteSafeConfig(site store.ChannelSite, runtimeBaseURL string) map[string]any {
	return map[string]any{
		"id":             site.ID,
		"name":           site.Name,
		"slug":           site.Slug,
		"domain":         site.Domain,
		"publicUrl":      site.PublicURL,
		"runtimeBaseUrl": strings.TrimRight(runtimeBaseURL, "/"),
		"runtimeUrl":     strings.TrimRight(runtimeBaseURL, "/") + "/" + site.ID,
		"title":          site.Title,
		"description":    site.Description,
		"logoMark":       site.LogoMark,
		"overviewLabel":  site.OverviewLabel,
		"recommendLabel": site.RecommendLabel,
		"modules":        site.Modules,
		"copy":           site.Copy,
		"seo":            site.SEO,
		"navItems":       site.NavItems,
		"qpsLimit":       site.QPSLimit,
		"status":         site.Status,
	}
}

func (s *Server) channelSiteRuntimeBaseURL() string {
	if strings.TrimSpace(s.cfg.PublicURL) != "" {
		return strings.TrimRight(s.cfg.PublicURL, "/") + "/site/v1"
	}
	return "/site/v1"
}

func channelSiteHTML(site store.ChannelSite, config map[string]any, recommend bool) string {
	tab := "home"
	title := channelSitePageTitle(site, false)
	if recommend {
		tab = "recommend"
		title = channelSitePageTitle(site, true)
	}
	cfg := scriptJSON(config)
	description := html.EscapeString(channelSitePageDescription(site))
	return fmt.Sprintf(`<!doctype html>
<html lang="zh-CN">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>%s</title>
  <meta name="description" content="%s">
  <link rel="stylesheet" href="%sassets/channel-site.css">
</head>
<body data-page="%s">
  <script id="site-config" type="application/json">%s</script>
  <header class="nav">
    <a class="brand" href="%s"><span>%s</span><b>%s</b></a>
    <nav id="site-nav"></nav>
  </header>
  <main>
    <section class="hero">
      <h1>%s</h1>
      <p>%s</p>
    </section>
    <section id="overview" class="stats"></section>
    <section id="channels" class="card"></section>
    <section id="recommend" class="card"></section>
    <section id="providers" class="grid"></section>
  </main>
  <footer id="footer"></footer>
  <script src="%sassets/channel-site.js"></script>
</body>
</html>`, html.EscapeString(title), description, assetPrefix(recommend), tab, cfg, linkPrefix(recommend), html.EscapeString(site.LogoMark), html.EscapeString(site.Name), html.EscapeString(site.Title), html.EscapeString(site.Copy.HomeIntro), assetPrefix(recommend))
}

func channelSitePageTitle(site store.ChannelSite, recommend bool) string {
	if recommend {
		label := strings.TrimSpace(site.RecommendLabel)
		if label == "" {
			label = "精选推荐"
		}
		name := strings.TrimSpace(site.Name)
		if name == "" {
			name = strings.TrimSpace(site.Title)
		}
		if name == "" {
			return label
		}
		return label + " · " + name
	}
	if title := strings.TrimSpace(site.SEO.Title); title != "" {
		return title
	}
	if title := strings.TrimSpace(site.Title); title != "" {
		return title
	}
	return strings.TrimSpace(site.Name)
}

func channelSitePageDescription(site store.ChannelSite) string {
	if description := strings.TrimSpace(site.SEO.Description); description != "" {
		return description
	}
	return strings.TrimSpace(site.Description)
}

func channelSiteCSS() string {
	return `:root{font-family:Inter,-apple-system,BlinkMacSystemFont,"Segoe UI",sans-serif;color:#181b25;background:#f8fafc}body{margin:0}.nav{height:64px;display:flex;align-items:center;justify-content:space-between;padding:0 40px;background:#fff;border-bottom:1px solid #e6e9f0;position:sticky;top:0;z-index:10}.brand{display:flex;align-items:center;gap:12px;color:inherit;text-decoration:none}.brand span{width:36px;height:36px;border-radius:10px;background:#2563eb;color:#fff;display:grid;place-items:center;font-weight:800}.brand b{font-size:20px}.nav nav{display:flex;gap:10px}.nav a:not(.brand){color:#555b6e;text-decoration:none;padding:9px 12px;border-radius:10px}.nav a:hover{background:#eef4ff;color:#2563eb}main{max-width:1200px;margin:0 auto;padding:38px 24px 64px}.hero{display:flex;align-items:flex-end;justify-content:space-between;gap:24px;margin-bottom:24px}.hero h1{font-size:38px;margin:0 0 10px}.hero p{font-size:16px;color:#697084;margin:0;max-width:720px}.stats{display:grid;grid-template-columns:repeat(5,minmax(0,1fr));gap:16px;margin:24px 0}.stat,.card{background:#fff;border:1px solid #e6e9f0;border-radius:16px;box-shadow:0 10px 28px rgba(16,24,40,.06)}.stat{padding:20px}.stat .k{color:#9298aa;font-weight:700}.stat .v{font-size:32px;font-weight:800;margin:8px 0}.stat .d{color:#9298aa}.card{padding:22px;margin:18px 0}.table{width:100%;border-collapse:collapse}.table th{color:#9298aa;text-align:left;font-size:13px;background:#fbfcff}.table th,.table td{padding:14px 12px;border-bottom:1px solid #eef1f6}.status{display:inline-flex;align-items:center;gap:6px;border-radius:999px;padding:4px 10px;font-weight:800}.healthy{color:#16a34a;background:#eafaf0}.degraded{color:#d97706;background:#fff7ed}.down{color:#e11d48;background:#fff1f2}.unknown{color:#64748b;background:#f1f5f9}.grid{display:grid;grid-template-columns:1fr 1fr;gap:18px}.rank-row{display:grid;grid-template-columns:42px 1fr 90px 90px;gap:12px;align-items:center;padding:12px;border:1px solid #eef1f6;border-radius:12px;margin-bottom:10px}.recommend-list{display:grid;grid-template-columns:repeat(3,minmax(0,1fr));gap:16px}.recommend-card{border:1px solid #eef1f6;border-radius:14px;padding:16px}.recommend-card h3{margin:8px 0}.btn{display:inline-flex;align-items:center;justify-content:center;border:1px solid #d8deeb;border-radius:10px;padding:10px 14px;text-decoration:none;color:#111827;font-weight:700}.btn-primary{background:#2563eb;color:#fff;border-color:#2563eb}footer{max-width:1200px;margin:0 auto;padding:24px;color:#9298aa;border-top:1px solid #e6e9f0}@media(max-width:800px){.nav{padding:0 18px}.nav nav{display:none}main{padding:28px 16px}.hero{display:block}.stats,.grid,.recommend-list{grid-template-columns:1fr}.table-wrap{overflow:auto}.hero h1{font-size:30px}}`
}

func channelSiteJS() string {
	return `const cfg=JSON.parse(document.getElementById("site-config").textContent);
const site=cfg.site;
const initial=cfg.initialData||{};
const key=cfg.runtimeKey;
const runtime=initial.runtimeURL||site.runtimeUrl;
const page=document.body.dataset.page;
const modules=site.modules||{};
const $=(id)=>document.getElementById(id);
function enabled(name){return modules[name]!==false;}
function remove(id){const el=$(id);if(el)el.remove();}
function esc(value){return String(value==null?"":value).replace(/[&<>"']/g,(ch)=>({"&":"&amp;","<":"&lt;",">":"&gt;","\"":"&quot;","'":"&#39;"}[ch]));}
function nav(){
  const items=[];
  if(enabled("overview")||enabled("channelBoard"))items.push({label:site.overviewLabel,href:"/"});
  if(enabled("recommend"))items.push({label:site.recommendLabel,href:"/recommend/"});
  (site.navItems||[]).forEach((item)=>items.push(item));
  $("site-nav").innerHTML=items.map((i)=>"<a href=\""+esc(i.href)+"\">"+esc(i.label)+"</a>").join("");
  $("footer").textContent=(site.copy&&site.copy.footerText)||(site.name+" · Powered by TokHub");
}
async function get(path,fallback){
  try{
    const res=await fetch(runtime+path,{headers:{"X-Channel-Site-Key":key}});
    if(!res.ok)throw new Error(res.status);
    return await res.json();
  }catch{return fallback;}
}
function statusClass(s){return s==="healthy"?"healthy":s==="degraded"?"degraded":s==="unknown"?"unknown":"down";}
function renderOverview(o){
  if(!$("overview"))return;
  const items=[
    ["健康通道",(o.healthy||0)+" / "+(o.total||0),"链路与生成均正常"],
    ["功能性故障",o.functionalDown||0,"入口正常但模型不可用"],
    ["连通异常",o.connectivityDown||0,"基础链路故障"],
    ["真实调用 P95",((o.p95LatencySeconds||0)).toFixed(2)+"s","真实生成延迟"],
    ["今日探测成本","$"+(o.probeCostToday||0).toFixed(2),(o.probeTokensToday||0)+" tokens"]
  ];
  $("overview").innerHTML=items.map(([k,v,d])=>"<div class=\"stat\"><div class=\"k\">"+esc(k)+"</div><div class=\"v\">"+esc(v)+"</div><div class=\"d\">"+esc(d)+"</div></div>").join("");
}
function renderChannels(list){
  if(!$("channels"))return;
  const rows=(list.items||[]).map((ch)=>"<tr><td><b>"+esc(ch.name)+"</b><div>"+esc(ch.provider)+" · "+esc(ch.upstreamModel||ch.model)+"</div></td><td><span class=\"status "+statusClass(ch.status)+"\">"+esc(ch.statusLabel||ch.status)+"</span></td><td>"+esc(ch.l1Status)+" "+esc(ch.l1LatencyMs)+"ms / "+esc(ch.l2Status)+" "+esc(ch.l2LatencyMs)+"ms</td><td>"+esc(ch.l3Status)+" "+esc(ch.l3LatencyMs)+"ms</td><td>"+esc(ch.latencyP95Ms)+"ms</td><td>"+Number(ch.uptime24h||0).toFixed(1)+"%</td><td>"+esc(ch.score)+"</td></tr>").join("");
  $("channels").innerHTML="<h2>通道明细看板</h2><div class=\"table-wrap\"><table class=\"table\"><thead><tr><th>服务商 / 通道</th><th>综合状态</th><th>L1/L2</th><th>L3</th><th>真实延迟</th><th>24H 可用率</th><th>质量评分</th></tr></thead><tbody>"+rows+"</tbody></table></div>";
}
function renderRecommend(data){
  if(!$("recommend"))return;
  const picks=(data.picks||[]).filter((x)=>x.enabled!==false);
  $("recommend").innerHTML="<h2>"+esc(site.recommendLabel)+"</h2><p>"+esc((site.copy&&site.copy.recommendIntro)||"")+"</p><div class=\"recommend-list\">"+picks.map((p)=>"<div class=\"recommend-card\"><small>"+esc(p.ribbon||"推荐")+"</small><h3>"+esc(p.title)+"</h3><p>"+esc(p.summary)+"</p><a class=\"btn btn-primary\" href=\""+esc(p.ctaUrl||"#")+"\" target=\"_blank\" rel=\"noreferrer\">"+esc(p.ctaLabel||"去官方体验")+"</a></div>").join("")+"</div>";
}
function renderProviders(items){
  if(!$("providers"))return;
  const sections=[];
  if(enabled("providerRank")){
    sections.push("<section class=\"card\"><h2>供应商排行</h2>"+(items||[]).map((p,i)=>"<div class=\"rank-row\"><b>#"+(i+1)+"</b><span>"+esc(p.provider)+"</span><span>"+Number(p.successRate||0).toFixed(1)+"%</span><span>"+esc(p.score)+"</span></div>").join("")+"</section>");
  }
  if(enabled("strategy")){
    sections.push("<section class=\"card\"><h2>监控策略</h2><p>基础监控负责入口是否健康，真实 API 监控负责模型是否真的可用。状态由 L1/L2/L3 合成，帮助用户避开只连通但不可生成的通道。</p></section>");
  }
  if(!sections.length){remove("providers");return;}
  $("providers").innerHTML=sections.join("");
}
async function boot(){
  nav();
  const [overview,channels,recommend]=await Promise.all([get("/overview",initial.overview),get("/channels?pageSize=100",initial.channels),get("/recommend",initial.recommend)]);
  if(enabled("overview")){renderOverview(overview||{});}else{remove("overview");}
  if(page==="recommend"){
    remove("channels");
    if(enabled("recommend")){renderRecommend(recommend||{});}else{remove("recommend");}
  }else{
    if(enabled("channelBoard")){renderChannels(channels||{items:[]});}else{remove("channels");}
    if(enabled("recommend")){renderRecommend(recommend||{});}else{remove("recommend");}
  }
  renderProviders(initial.providers||[]);
}
boot();`
}

func channelSiteRobots(site store.ChannelSite) string {
	base := strings.TrimRight(site.PublicURL, "/")
	sitemap := "sitemap.xml"
	if base != "" {
		sitemap = base + "/sitemap.xml"
	}
	return fmt.Sprintf("User-agent: *\nAllow: /\nSitemap: %s\n", sitemap)
}

func channelSiteSitemap(site store.ChannelSite) string {
	base := strings.TrimRight(site.PublicURL, "/")
	if base == "" {
		base = "."
	}
	urls := []string{
		fmt.Sprintf("  <url><loc>%s/</loc><changefreq>hourly</changefreq><priority>1.0</priority></url>", html.EscapeString(base)),
	}
	if site.Modules.Recommend {
		urls = append(urls, fmt.Sprintf("  <url><loc>%s/recommend/</loc><changefreq>daily</changefreq><priority>0.8</priority></url>", html.EscapeString(base)))
	}
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">
%s
</urlset>
`, strings.Join(urls, "\n"))
}

func channelSiteLLMS(site store.ChannelSite) string {
	lines := []string{
		fmt.Sprintf("# %s", site.Title),
		"",
		site.Description,
		"",
		"- 首页: /",
	}
	if site.Modules.Recommend {
		lines = append(lines, "- 精选推荐: /recommend/")
	}
	lines = append(lines, "- 数据来源: TokHub 渠道站点运行时 API")
	return strings.Join(lines, "\n") + "\n"
}

func channelSiteReadme(site store.ChannelSite, config map[string]any) string {
	runtimeURL := ""
	if siteConfig, ok := config["site"].(map[string]any); ok {
		runtimeURL, _ = siteConfig["runtimeUrl"].(string)
	}
	return fmt.Sprintf(`# %s 站点包

把 zip 解压到绑定域名对应的网站目录即可。页面是静态 HTML/CSS/JS，实时监控数据通过中心服务的只读 runtime API 获取。

- 站点域名：%s
- 站点地址：%s
- 运行时接口：%s
- 密钥规则：runtime key 只允许读取本渠道站点的公开数据，不具备后台、网关或密钥读取权限

如需更新菜单、SEO 或模块文案，请在 TokHub 管理后台重新生成并下载站点包。
`, site.Name, site.Domain, site.PublicURL, runtimeURL)
}

func assetPrefix(recommend bool) string {
	if recommend {
		return "../"
	}
	return ""
}

func linkPrefix(recommend bool) string {
	if recommend {
		return "../"
	}
	return "./"
}

func mustJSON(value any) string {
	payload, _ := json.Marshal(value)
	return string(payload)
}

func scriptJSON(value any) string {
	payload := mustJSON(value)
	return strings.ReplaceAll(payload, "</", "<\\/")
}

func mustJSONIndent(value any) string {
	payload, _ := json.MarshalIndent(value, "", "  ")
	return string(payload)
}

func safeFilePart(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	if value == "" {
		return "site"
	}
	return regexp.MustCompile(`[^a-z0-9_-]+`).ReplaceAllString(value, "-")
}
