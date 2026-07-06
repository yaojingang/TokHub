package api

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"golang.org/x/net/html"

	"tokhub/internal/store"
)

type adminChannelIntroFetchRequest struct {
	URL string `json:"url"`
}

type channelIntroDraft struct {
	IntroTitle      string   `json:"introTitle"`
	IntroSummary    string   `json:"introSummary"`
	IntroBody       string   `json:"introBody"`
	IntroHighlights []string `json:"introHighlights"`
	LogoURL         string   `json:"logoUrl"`
	IntroSourceURL  string   `json:"introSourceUrl"`
}

func (s *Server) fetchAdminChannelIntro(w http.ResponseWriter, r *http.Request) {
	user, _ := s.userFromRequest(r)
	channelID := chi.URLParam(r, "channelID")
	var req adminChannelIntroFetchRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid_json", "Invalid JSON body")
		return
	}
	item, err := s.repo.AdminPlatformChannel(r.Context(), channelID)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, r, http.StatusNotFound, "platform_channel_not_found", "Platform channel not found")
		return
	}
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "channel_unavailable", "Could not load channel")
		return
	}
	targetURL := strings.TrimSpace(req.URL)
	if targetURL == "" {
		targetURL = item.OfficialSiteURL
	}
	targetURL, err = cleanOptionalHTTPURL(targetURL, "url")
	if err != nil || targetURL == "" {
		writeError(w, r, http.StatusBadRequest, "invalid_intro_source_url", "请输入有效的官网 http/https 地址")
		return
	}
	draft, err := fetchChannelIntroDraft(r.Context(), targetURL, item.PublicChannel)
	if err != nil {
		writeError(w, r, http.StatusBadGateway, "intro_fetch_failed", err.Error())
		return
	}
	_ = s.repo.WriteAudit(r.Context(), store.AuditEvent{
		ActorType:  "user",
		ActorID:    user.ID,
		Action:     "platform_channel.intro_fetched",
		ObjectType: "channel",
		ObjectID:   channelID,
		IP:         clientIP(r),
		Result:     "success",
		Metadata: map[string]any{
			"url":      fetchAuditURL(targetURL),
			"has_logo": draft.LogoURL != "",
		},
	})
	writeJSON(w, http.StatusOK, map[string]any{"draft": draft})
}

func fetchChannelIntroDraft(ctx context.Context, sourceURL string, ch store.PublicChannel) (channelIntroDraft, error) {
	if err := validateExternalFetchURL(ctx, sourceURL); err != nil {
		return channelIntroDraft{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, sourceURL, nil)
	if err != nil {
		return channelIntroDraft{}, err
	}
	req.Header.Set("Accept", "text/html,application/xhtml+xml")
	req.Header.Set("User-Agent", "TokHubBot/0.1 (+https://www.tokhub.me/)")
	client := &http.Client{
		Timeout:   10 * time.Second,
		Transport: safeFetchTransport(),
		CheckRedirect: func(next *http.Request, via []*http.Request) error {
			if len(via) >= 3 {
				return errors.New("官网跳转次数过多")
			}
			return validateExternalFetchURL(next.Context(), next.URL.String())
		},
	}
	resp, err := client.Do(req)
	if err != nil {
		return channelIntroDraft{}, fmt.Errorf("官网请求失败: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return channelIntroDraft{}, fmt.Errorf("官网返回 %s", resp.Status)
	}
	contentType := strings.ToLower(resp.Header.Get("Content-Type"))
	if contentType != "" && !strings.Contains(contentType, "html") {
		return channelIntroDraft{}, errors.New("官网响应不是 HTML")
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, (1<<20)+1))
	if err != nil {
		return channelIntroDraft{}, fmt.Errorf("读取官网内容失败: %w", err)
	}
	if len(raw) > 1<<20 {
		return channelIntroDraft{}, errors.New("官网 HTML 超过 1MB")
	}
	doc, err := html.Parse(strings.NewReader(string(raw)))
	if err != nil {
		return channelIntroDraft{}, fmt.Errorf("解析官网 HTML 失败: %w", err)
	}
	return buildChannelIntroDraft(sourceURL, doc, ch), nil
}

func buildChannelIntroDraft(sourceURL string, doc *html.Node, ch store.PublicChannel) channelIntroDraft {
	data := extractHTMLIntroData(sourceURL, doc)
	summary := firstNonEmpty(data.MetaDescription, data.OGDescription, firstString(data.Paragraphs))
	if summary == "" {
		summary = fmt.Sprintf("%s 是 %s 关联的 %s API 中转站入口，适合在注册或接入前先查看官网信息和 TokHub 真实监控数据。", ch.Name, ch.Provider, ch.Model)
	}
	summary = trimTextRunes(summary, 180)
	bodyParts := []string{}
	for _, paragraph := range data.Paragraphs {
		paragraph = trimTextRunes(paragraph, 260)
		if paragraph == "" || sameLooseText(paragraph, summary) {
			continue
		}
		bodyParts = append(bodyParts, paragraph)
		if len(bodyParts) >= 2 {
			break
		}
	}
	if len(bodyParts) == 0 {
		bodyParts = append(bodyParts, summary)
	}
	bodyParts = append(bodyParts, fmt.Sprintf("在 TokHub 中，**%s** 会和接入域名、兼容协议、上游模型与真实探测结果一起展示。你可以先查看综合健康指数、P95 延迟、24H 可用率、真实成功率和错误分类，再决定是否进入官网注册体验或放入企业网关候选。", ch.Name))
	highlights := []string{
		"服务商：" + ch.Provider,
		"模型：" + ch.Model,
		"兼容类型：" + strings.ToUpper(ch.Type),
	}
	if host := publicURLHost(ch.Endpoint); host != "" {
		highlights = append(highlights, "接入域名："+host)
	}
	if host := publicURLHost(sourceURL); host != "" {
		highlights = append(highlights, "官网来源："+host)
	}
	return channelIntroDraft{
		IntroTitle:      ch.Name + " 官方介绍",
		IntroSummary:    summary,
		IntroBody:       strings.Join(bodyParts, "\n\n"),
		IntroHighlights: introDraftHighlights(highlights),
		LogoURL:         firstNonEmpty(data.OGImage, data.TwitterImage, data.IconURL),
		IntroSourceURL:  sourceURL,
	}
}

type htmlIntroData struct {
	MetaDescription string
	OGDescription   string
	OGImage         string
	TwitterImage    string
	IconURL         string
	Paragraphs      []string
}

func extractHTMLIntroData(sourceURL string, doc *html.Node) htmlIntroData {
	var data htmlIntroData
	walkHTML(doc, func(n *html.Node) {
		if n.Type != html.ElementNode {
			return
		}
		switch strings.ToLower(n.Data) {
		case "meta":
			key := strings.ToLower(firstNonEmpty(htmlAttr(n, "name"), htmlAttr(n, "property")))
			content := cleanVisibleText(htmlAttr(n, "content"))
			switch key {
			case "description":
				data.MetaDescription = firstNonEmpty(data.MetaDescription, content)
			case "og:description":
				data.OGDescription = firstNonEmpty(data.OGDescription, content)
			case "og:image":
				data.OGImage = firstNonEmpty(data.OGImage, absoluteHTMLURL(sourceURL, content))
			case "twitter:image":
				data.TwitterImage = firstNonEmpty(data.TwitterImage, absoluteHTMLURL(sourceURL, content))
			}
		case "link":
			rel := strings.ToLower(htmlAttr(n, "rel"))
			if strings.Contains(rel, "icon") {
				data.IconURL = firstNonEmpty(data.IconURL, absoluteHTMLURL(sourceURL, htmlAttr(n, "href")))
			}
		case "p":
			text := cleanVisibleText(nodeText(n))
			if len([]rune(text)) >= 36 {
				data.Paragraphs = append(data.Paragraphs, text)
			}
		}
	})
	if data.IconURL == "" {
		data.IconURL = absoluteHTMLURL(sourceURL, "/favicon.ico")
	}
	if len(data.Paragraphs) > 6 {
		data.Paragraphs = data.Paragraphs[:6]
	}
	return data
}

func safeFetchTransport() *http.Transport {
	return &http.Transport{
		Proxy: nil,
		DialContext: func(ctx context.Context, network string, address string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(address)
			if err != nil {
				return nil, err
			}
			ip, err := resolvePublicIP(ctx, host)
			if err != nil {
				return nil, err
			}
			return (&net.Dialer{Timeout: 6 * time.Second}).DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
		},
		TLSHandshakeTimeout:   6 * time.Second,
		ResponseHeaderTimeout: 6 * time.Second,
	}
}

func validateExternalFetchURL(ctx context.Context, raw string) error {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Hostname() == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return errors.New("官网地址必须是 http 或 https URL")
	}
	if parsed.User != nil {
		return errors.New("官网地址不能包含用户名或密码")
	}
	_, err = resolvePublicIP(ctx, parsed.Hostname())
	return err
}

func resolvePublicIP(ctx context.Context, host string) (net.IP, error) {
	addrs, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, fmt.Errorf("官网域名解析失败: %w", err)
	}
	for _, item := range addrs {
		if publicFetchIP(item.IP) {
			return item.IP, nil
		}
	}
	return nil, errors.New("官网地址不能指向内网、localhost 或保留地址")
}

func publicFetchIP(ip net.IP) bool {
	addr, ok := netip.AddrFromSlice(ip)
	if !ok {
		return false
	}
	addr = addr.Unmap()
	if !addr.IsGlobalUnicast() || addr.IsPrivate() || addr.IsLoopback() || addr.IsLinkLocalUnicast() || addr.IsMulticast() || addr.IsUnspecified() {
		return false
	}
	for _, prefix := range blockedFetchIPPrefixes {
		if prefix.Contains(addr) {
			return false
		}
	}
	return true
}

var blockedFetchIPPrefixes = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("10.0.0.0/8"),
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("127.0.0.0/8"),
	netip.MustParsePrefix("169.254.0.0/16"),
	netip.MustParsePrefix("172.16.0.0/12"),
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("192.168.0.0/16"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"),
	netip.MustParsePrefix("224.0.0.0/4"),
	netip.MustParsePrefix("240.0.0.0/4"),
	netip.MustParsePrefix("::/128"),
	netip.MustParsePrefix("::1/128"),
	netip.MustParsePrefix("fc00::/7"),
	netip.MustParsePrefix("fe80::/10"),
	netip.MustParsePrefix("ff00::/8"),
	netip.MustParsePrefix("2001:db8::/32"),
}

func walkHTML(n *html.Node, fn func(*html.Node)) {
	if n == nil {
		return
	}
	fn(n)
	for child := n.FirstChild; child != nil; child = child.NextSibling {
		walkHTML(child, fn)
	}
}

func nodeText(n *html.Node) string {
	if n == nil {
		return ""
	}
	if n.Type == html.ElementNode {
		switch strings.ToLower(n.Data) {
		case "script", "style", "noscript", "svg":
			return ""
		}
	}
	if n.Type == html.TextNode {
		return n.Data
	}
	parts := []string{}
	for child := n.FirstChild; child != nil; child = child.NextSibling {
		if text := nodeText(child); text != "" {
			parts = append(parts, text)
		}
	}
	return strings.Join(parts, " ")
}

func htmlAttr(n *html.Node, name string) string {
	for _, attr := range n.Attr {
		if strings.EqualFold(attr.Key, name) {
			return strings.TrimSpace(attr.Val)
		}
	}
	return ""
}

func cleanVisibleText(value string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
}

func absoluteHTMLURL(baseURL string, ref string) string {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return ""
	}
	base, err := url.Parse(baseURL)
	if err != nil {
		return ""
	}
	parsed, err := url.Parse(ref)
	if err != nil {
		return ""
	}
	resolved := base.ResolveReference(parsed)
	if resolved.Scheme != "http" && resolved.Scheme != "https" {
		return ""
	}
	if resolved.Hostname() == "" || resolved.User != nil {
		return ""
	}
	resolved.Fragment = ""
	return resolved.String()
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func firstString(values []string) string {
	if len(values) == 0 {
		return ""
	}
	return values[0]
}

func sameLooseText(a string, b string) bool {
	return strings.EqualFold(cleanVisibleText(a), cleanVisibleText(b))
}

func trimTextRunes(value string, limit int) string {
	value = cleanVisibleText(value)
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit])
}

func introDraftHighlights(values []string) []string {
	out := []string{}
	seen := map[string]bool{}
	for _, value := range values {
		value = cleanVisibleText(value)
		if value == "" {
			continue
		}
		key := strings.ToLower(value)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, value)
		if len(out) >= 5 {
			break
		}
	}
	return out
}

func fetchAuditURL(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed.String()
}
