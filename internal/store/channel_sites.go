package store

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

var ErrInvalidChannelSite = errors.New("invalid channel site")

type ChannelSiteValidationError struct {
	Message string
}

func (e ChannelSiteValidationError) Error() string {
	if strings.TrimSpace(e.Message) == "" {
		return ErrInvalidChannelSite.Error()
	}
	return e.Message
}

func (e ChannelSiteValidationError) Unwrap() error {
	return ErrInvalidChannelSite
}

type ChannelSiteNavItem struct {
	ID       string `json:"id"`
	Label    string `json:"label"`
	Href     string `json:"href"`
	Position int    `json:"position"`
}

type ChannelSiteModules struct {
	Overview     bool `json:"overview"`
	ChannelBoard bool `json:"channelBoard"`
	Recommend    bool `json:"recommend"`
	ProviderRank bool `json:"providerRank"`
	Strategy     bool `json:"strategy"`
}

type ChannelSiteCopy struct {
	HomeIntro      string `json:"homeIntro"`
	RecommendIntro string `json:"recommendIntro"`
	FooterText     string `json:"footerText"`
}

type ChannelSiteSEO struct {
	Title        string `json:"title"`
	Description  string `json:"description"`
	CanonicalURL string `json:"canonicalUrl"`
}

type ChannelSitePackageExport struct {
	ID        string    `json:"id"`
	SiteID    string    `json:"siteId"`
	Version   string    `json:"version"`
	FileName  string    `json:"fileName"`
	FileSize  int       `json:"fileSize"`
	CreatedAt time.Time `json:"createdAt"`
}

type ChannelSiteRuntimeLog struct {
	ID         string    `json:"id"`
	SiteID     string    `json:"siteId"`
	SiteName   string    `json:"siteName"`
	Endpoint   string    `json:"endpoint"`
	StatusCode int       `json:"statusCode"`
	LatencyMs  int       `json:"latencyMs"`
	Origin     string    `json:"origin"`
	IP         string    `json:"ip"`
	CreatedAt  time.Time `json:"createdAt"`
}

type ChannelSite struct {
	ID               string                     `json:"id"`
	Name             string                     `json:"name"`
	Slug             string                     `json:"slug"`
	Domain           string                     `json:"domain"`
	PublicURL        string                     `json:"publicUrl"`
	RuntimeKeyPrefix string                     `json:"runtimeKeyPrefix"`
	RuntimeKeyMask   string                     `json:"runtimeKeyMask"`
	PlainRuntimeKey  string                     `json:"plainRuntimeKey,omitempty"`
	Title            string                     `json:"title"`
	Description      string                     `json:"description"`
	LogoMark         string                     `json:"logoMark"`
	OverviewLabel    string                     `json:"overviewLabel"`
	RecommendLabel   string                     `json:"recommendLabel"`
	Modules          ChannelSiteModules         `json:"modules"`
	Copy             ChannelSiteCopy            `json:"copy"`
	SEO              ChannelSiteSEO             `json:"seo"`
	NavItems         []ChannelSiteNavItem       `json:"navItems"`
	QPSLimit         int                        `json:"qpsLimit"`
	Status           string                     `json:"status"`
	CallsToday       int                        `json:"callsToday"`
	PackageExports   []ChannelSitePackageExport `json:"packageExports,omitempty"`
	CreatedAt        time.Time                  `json:"createdAt"`
	UpdatedAt        time.Time                  `json:"updatedAt"`
	LastUsedAt       *time.Time                 `json:"lastUsedAt,omitempty"`
}

type ChannelSiteInput struct {
	Name           string               `json:"name"`
	Domain         string               `json:"domain"`
	PublicURL      string               `json:"publicUrl"`
	Title          string               `json:"title"`
	Description    string               `json:"description"`
	LogoMark       string               `json:"logoMark"`
	OverviewLabel  string               `json:"overviewLabel"`
	RecommendLabel string               `json:"recommendLabel"`
	Modules        ChannelSiteModules   `json:"modules"`
	Copy           ChannelSiteCopy      `json:"copy"`
	SEO            ChannelSiteSEO       `json:"seo"`
	NavItems       []ChannelSiteNavItem `json:"navItems"`
	QPSLimit       int                  `json:"qpsLimit"`
	Status         string               `json:"status"`
	ActorID        string               `json:"-"`
}

type ChannelSitesSummary struct {
	BaseURL string                  `json:"baseUrl"`
	Sites   []ChannelSite           `json:"sites"`
	Logs    []ChannelSiteRuntimeLog `json:"logs"`
	Stats   map[string]int          `json:"stats"`
}

type AuthenticatedChannelSite struct {
	Site ChannelSite
}

func (r *Repository) ChannelSitesSummary(ctx context.Context, baseURL string) (ChannelSitesSummary, error) {
	sites, err := r.ListChannelSites(ctx)
	if err != nil {
		return ChannelSitesSummary{}, err
	}
	logs, err := r.ChannelSiteRuntimeLogs(ctx)
	if err != nil {
		return ChannelSitesSummary{}, err
	}
	active := 0
	callsToday := 0
	exports := 0
	for _, site := range sites {
		if site.Status == "active" {
			active++
		}
		callsToday += site.CallsToday
		exports += len(site.PackageExports)
	}
	return ChannelSitesSummary{
		BaseURL: strings.TrimRight(baseURL, "/") + "/site/v1",
		Sites:   sites,
		Logs:    logs,
		Stats: map[string]int{
			"sites":      len(sites),
			"active":     active,
			"callsToday": callsToday,
			"exports":    exports,
		},
	}, nil
}

func (r *Repository) ListChannelSites(ctx context.Context) ([]ChannelSite, error) {
	rows, err := r.db.Query(ctx, `
		select s.id,s.name,s.slug,s.domain,s.public_url,s.runtime_key_prefix,s.runtime_key_mask,
			s.title,s.description,s.logo_mark,s.overview_label,s.recommend_label,
			s.modules,s.copy_json,s.seo_json,s.qps_limit,s.status,s.created_at,s.updated_at,s.last_used_at,
			coalesce((select count(*) from channel_site_runtime_logs l where l.site_id=s.id and l.created_at::date=current_date),0)
		from channel_sites s
		where s.deleted_at is null
		order by s.created_at desc
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ChannelSite{}
	for rows.Next() {
		site, err := scanChannelSiteRow(rows)
		if err != nil {
			return nil, err
		}
		site.NavItems, err = r.channelSiteNavItems(ctx, site.ID)
		if err != nil {
			return nil, err
		}
		site.PackageExports, err = r.channelSitePackageExports(ctx, site.ID)
		if err != nil {
			return nil, err
		}
		out = append(out, site)
	}
	return out, rows.Err()
}

func (r *Repository) ChannelSiteByID(ctx context.Context, siteID string) (ChannelSite, error) {
	rows, err := r.db.Query(ctx, `
		select s.id,s.name,s.slug,s.domain,s.public_url,s.runtime_key_prefix,s.runtime_key_mask,
			s.title,s.description,s.logo_mark,s.overview_label,s.recommend_label,
			s.modules,s.copy_json,s.seo_json,s.qps_limit,s.status,s.created_at,s.updated_at,s.last_used_at,
			coalesce((select count(*) from channel_site_runtime_logs l where l.site_id=s.id and l.created_at::date=current_date),0)
		from channel_sites s
		where s.id=$1 and s.deleted_at is null
	`, siteID)
	if err != nil {
		return ChannelSite{}, err
	}
	defer rows.Close()
	if !rows.Next() {
		return ChannelSite{}, pgx.ErrNoRows
	}
	site, err := scanChannelSiteRow(rows)
	if err != nil {
		return ChannelSite{}, err
	}
	if rows.Err() != nil {
		return ChannelSite{}, rows.Err()
	}
	site.NavItems, err = r.channelSiteNavItems(ctx, site.ID)
	if err != nil {
		return ChannelSite{}, err
	}
	site.PackageExports, err = r.channelSitePackageExports(ctx, site.ID)
	return site, err
}

func (r *Repository) CreateChannelSite(ctx context.Context, input ChannelSiteInput) (ChannelSite, error) {
	site, err := normalizeChannelSiteInput(input, nil)
	if err != nil {
		return ChannelSite{}, err
	}
	plain, err := NewChannelSiteRuntimeKey()
	if err != nil {
		return ChannelSite{}, err
	}
	site.ID = "chs_" + uuid.NewString()
	site.Slug = UniqueSlug(site.Name)
	site.RuntimeKeyPrefix = ChannelSiteRuntimeKeyPrefix(plain)
	site.RuntimeKeyMask = MaskGatewayKey(plain)
	site.PlainRuntimeKey = plain
	modules, _ := json.Marshal(site.Modules)
	copyJSON, _ := json.Marshal(site.Copy)
	seoJSON, _ := json.Marshal(site.SEO)
	err = r.db.QueryRow(ctx, `
		insert into channel_sites(
			id,name,slug,domain,public_url,runtime_key_hash,runtime_key_prefix,runtime_key_mask,
			title,description,logo_mark,overview_label,recommend_label,modules,copy_json,seo_json,qps_limit,status,created_by,created_at,updated_at
		)
		values($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,now(),now())
		returning created_at,updated_at
	`, site.ID, site.Name, site.Slug, site.Domain, site.PublicURL, HashOpaqueToken(plain), site.RuntimeKeyPrefix, site.RuntimeKeyMask,
		site.Title, site.Description, site.LogoMark, site.OverviewLabel, site.RecommendLabel, modules, copyJSON, seoJSON, site.QPSLimit, site.Status, nullableText(input.ActorID)).Scan(&site.CreatedAt, &site.UpdatedAt)
	if err != nil {
		return ChannelSite{}, err
	}
	if err := r.replaceChannelSiteNavItems(ctx, site.ID, site.NavItems); err != nil {
		return ChannelSite{}, err
	}
	_ = r.WriteAudit(ctx, AuditEvent{
		ActorType:  "user",
		ActorID:    input.ActorID,
		Action:     "channel_site.created",
		ObjectType: "channel_site",
		ObjectID:   site.ID,
		Result:     "success",
		Metadata:   map[string]any{"name": site.Name, "domain": site.Domain, "runtime_key_prefix": site.RuntimeKeyPrefix},
	})
	return site, nil
}

func (r *Repository) UpdateChannelSite(ctx context.Context, siteID string, input ChannelSiteInput) (ChannelSite, error) {
	current, err := r.ChannelSiteByID(ctx, siteID)
	if err != nil {
		return ChannelSite{}, err
	}
	site, err := normalizeChannelSiteInput(input, &current)
	if err != nil {
		return ChannelSite{}, err
	}
	modules, _ := json.Marshal(site.Modules)
	copyJSON, _ := json.Marshal(site.Copy)
	seoJSON, _ := json.Marshal(site.SEO)
	err = r.db.QueryRow(ctx, `
		update channel_sites
		set name=$2,domain=$3,public_url=$4,title=$5,description=$6,logo_mark=$7,overview_label=$8,recommend_label=$9,
			modules=$10,copy_json=$11,seo_json=$12,qps_limit=$13,status=$14,updated_at=now()
		where id=$1 and deleted_at is null
		returning updated_at
	`, siteID, site.Name, site.Domain, site.PublicURL, site.Title, site.Description, site.LogoMark, site.OverviewLabel, site.RecommendLabel,
		modules, copyJSON, seoJSON, site.QPSLimit, site.Status).Scan(&site.UpdatedAt)
	if err != nil {
		return ChannelSite{}, err
	}
	if err := r.replaceChannelSiteNavItems(ctx, siteID, site.NavItems); err != nil {
		return ChannelSite{}, err
	}
	site.ID = current.ID
	site.Slug = current.Slug
	site.RuntimeKeyPrefix = current.RuntimeKeyPrefix
	site.RuntimeKeyMask = current.RuntimeKeyMask
	site.CreatedAt = current.CreatedAt
	site.LastUsedAt = current.LastUsedAt
	site.PackageExports = current.PackageExports
	_ = r.WriteAudit(ctx, AuditEvent{
		ActorType:  "user",
		ActorID:    input.ActorID,
		Action:     "channel_site.updated",
		ObjectType: "channel_site",
		ObjectID:   site.ID,
		Result:     "success",
		Metadata:   map[string]any{"name": site.Name, "domain": site.Domain, "status": site.Status},
	})
	return site, nil
}

func (r *Repository) DeleteChannelSite(ctx context.Context, siteID string, actorID string) (ChannelSite, error) {
	site, err := r.ChannelSiteByID(ctx, siteID)
	if err != nil {
		return ChannelSite{}, err
	}
	_, err = r.db.Exec(ctx, `update channel_sites set status='revoked', deleted_at=now(), updated_at=now() where id=$1 and deleted_at is null`, siteID)
	if err != nil {
		return ChannelSite{}, err
	}
	site.Status = "revoked"
	_ = r.WriteAudit(ctx, AuditEvent{
		ActorType:  "user",
		ActorID:    actorID,
		Action:     "channel_site.deleted",
		ObjectType: "channel_site",
		ObjectID:   site.ID,
		Result:     "success",
		Metadata:   map[string]any{"name": site.Name, "domain": site.Domain},
	})
	return site, nil
}

func (r *Repository) RotateChannelSiteKey(ctx context.Context, siteID string, actorID string) (ChannelSite, error) {
	plain, err := NewChannelSiteRuntimeKey()
	if err != nil {
		return ChannelSite{}, err
	}
	prefix := ChannelSiteRuntimeKeyPrefix(plain)
	mask := MaskGatewayKey(plain)
	_, err = r.db.Exec(ctx, `
		update channel_sites
		set runtime_key_hash=$2,runtime_key_prefix=$3,runtime_key_mask=$4,updated_at=now()
		where id=$1 and deleted_at is null
	`, siteID, HashOpaqueToken(plain), prefix, mask)
	if err != nil {
		return ChannelSite{}, err
	}
	site, err := r.ChannelSiteByID(ctx, siteID)
	if err != nil {
		return ChannelSite{}, err
	}
	site.PlainRuntimeKey = plain
	_ = r.WriteAudit(ctx, AuditEvent{
		ActorType:  "user",
		ActorID:    actorID,
		Action:     "channel_site.key_rotated",
		ObjectType: "channel_site",
		ObjectID:   site.ID,
		Result:     "success",
		Metadata:   map[string]any{"runtime_key_prefix": site.RuntimeKeyPrefix},
	})
	return site, nil
}

func (r *Repository) AuthenticateChannelSite(ctx context.Context, siteID string, plainKey string) (AuthenticatedChannelSite, error) {
	hash := HashOpaqueToken(strings.TrimSpace(plainKey))
	var site ChannelSite
	rows, err := r.db.Query(ctx, `
		select s.id,s.name,s.slug,s.domain,s.public_url,s.runtime_key_prefix,s.runtime_key_mask,
			s.title,s.description,s.logo_mark,s.overview_label,s.recommend_label,
			s.modules,s.copy_json,s.seo_json,s.qps_limit,s.status,s.created_at,s.updated_at,s.last_used_at,
			coalesce((select count(*) from channel_site_runtime_logs l where l.site_id=s.id and l.created_at::date=current_date),0)
		from channel_sites s
		where s.id=$1 and s.runtime_key_hash=$2 and s.status='active' and s.deleted_at is null
	`, siteID, hash)
	if err != nil {
		return AuthenticatedChannelSite{}, err
	}
	defer rows.Close()
	if !rows.Next() {
		return AuthenticatedChannelSite{}, pgx.ErrNoRows
	}
	site, err = scanChannelSiteRow(rows)
	if err != nil {
		return AuthenticatedChannelSite{}, err
	}
	site.NavItems, _ = r.channelSiteNavItems(ctx, site.ID)
	return AuthenticatedChannelSite{Site: site}, rows.Err()
}

func (r *Repository) TouchChannelSite(ctx context.Context, siteID string) error {
	_, err := r.db.Exec(ctx, `update channel_sites set last_used_at=now(), updated_at=now() where id=$1 and deleted_at is null`, siteID)
	return err
}

func (r *Repository) RecordChannelSiteRuntimeCall(ctx context.Context, siteID string, endpoint string, status int, latencyMs int, origin string, ip string, ua string) error {
	_, err := r.db.Exec(ctx, `
		insert into channel_site_runtime_logs(id,site_id,endpoint,status_code,latency_ms,origin,ip,user_agent,created_at)
		values($1,$2,$3,$4,$5,$6,$7,$8,now())
	`, "csl_"+uuid.NewString(), nullableText(siteID), endpoint, status, latencyMs, origin, ip, ua)
	return err
}

func (r *Repository) ChannelSiteRuntimeLogs(ctx context.Context) ([]ChannelSiteRuntimeLog, error) {
	rows, err := r.db.Query(ctx, `
		select l.id,coalesce(l.site_id,''),coalesce(s.name,'-'),l.endpoint,l.status_code,l.latency_ms,coalesce(l.origin,''),coalesce(l.ip,''),l.created_at
		from channel_site_runtime_logs l
		left join channel_sites s on s.id=l.site_id
		order by l.created_at desc
		limit 50
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ChannelSiteRuntimeLog{}
	for rows.Next() {
		var item ChannelSiteRuntimeLog
		if err := rows.Scan(&item.ID, &item.SiteID, &item.SiteName, &item.Endpoint, &item.StatusCode, &item.LatencyMs, &item.Origin, &item.IP, &item.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (r *Repository) CreateChannelSitePackageExport(ctx context.Context, siteID string, fileName string, fileSize int, actorID string) (ChannelSitePackageExport, error) {
	export := ChannelSitePackageExport{
		ID:       "cse_" + uuid.NewString(),
		SiteID:   siteID,
		Version:  "v1",
		FileName: fileName,
		FileSize: fileSize,
	}
	err := r.db.QueryRow(ctx, `
		insert into channel_site_package_exports(id,site_id,version,file_name,file_size,created_by,created_at)
		values($1,$2,$3,$4,$5,$6,now())
		returning created_at
	`, export.ID, export.SiteID, export.Version, export.FileName, export.FileSize, nullableText(actorID)).Scan(&export.CreatedAt)
	if err != nil {
		return ChannelSitePackageExport{}, err
	}
	_ = r.WriteAudit(ctx, AuditEvent{
		ActorType:  "user",
		ActorID:    actorID,
		Action:     "channel_site.package_exported",
		ObjectType: "channel_site",
		ObjectID:   siteID,
		Result:     "success",
		Metadata:   map[string]any{"file_name": fileName, "file_size": fileSize},
	})
	return export, nil
}

func (r *Repository) channelSiteNavItems(ctx context.Context, siteID string) ([]ChannelSiteNavItem, error) {
	rows, err := r.db.Query(ctx, `
		select id,label,href,position
		from channel_site_nav_items
		where site_id=$1
		order by position asc
	`, siteID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ChannelSiteNavItem{}
	for rows.Next() {
		var item ChannelSiteNavItem
		if err := rows.Scan(&item.ID, &item.Label, &item.Href, &item.Position); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (r *Repository) replaceChannelSiteNavItems(ctx context.Context, siteID string, items []ChannelSiteNavItem) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `delete from channel_site_nav_items where site_id=$1`, siteID); err != nil {
		return err
	}
	for i, item := range items {
		position := item.Position
		if position <= 0 {
			position = i + 1
		}
		if _, err := tx.Exec(ctx, `
			insert into channel_site_nav_items(id,site_id,label,href,position,created_at,updated_at)
			values($1,$2,$3,$4,$5,now(),now())
		`, idOrNew(item.ID, "csn_"), siteID, item.Label, item.Href, position); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func (r *Repository) channelSitePackageExports(ctx context.Context, siteID string) ([]ChannelSitePackageExport, error) {
	rows, err := r.db.Query(ctx, `
		select id,site_id,version,file_name,file_size,created_at
		from channel_site_package_exports
		where site_id=$1
		order by created_at desc
		limit 5
	`, siteID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ChannelSitePackageExport{}
	for rows.Next() {
		var item ChannelSitePackageExport
		if err := rows.Scan(&item.ID, &item.SiteID, &item.Version, &item.FileName, &item.FileSize, &item.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func scanChannelSiteRow(rows pgx.Rows) (ChannelSite, error) {
	var site ChannelSite
	var modulesRaw, copyRaw, seoRaw []byte
	if err := rows.Scan(&site.ID, &site.Name, &site.Slug, &site.Domain, &site.PublicURL, &site.RuntimeKeyPrefix, &site.RuntimeKeyMask,
		&site.Title, &site.Description, &site.LogoMark, &site.OverviewLabel, &site.RecommendLabel,
		&modulesRaw, &copyRaw, &seoRaw, &site.QPSLimit, &site.Status, &site.CreatedAt, &site.UpdatedAt, nullableTimePtr(&site.LastUsedAt), &site.CallsToday); err != nil {
		return ChannelSite{}, err
	}
	site.Modules = defaultChannelSiteModules()
	site.Copy = defaultChannelSiteCopy(site.Name)
	site.SEO = defaultChannelSiteSEO(site)
	_ = json.Unmarshal(modulesRaw, &site.Modules)
	_ = json.Unmarshal(copyRaw, &site.Copy)
	_ = json.Unmarshal(seoRaw, &site.SEO)
	return site, nil
}

func normalizeChannelSiteInput(input ChannelSiteInput, current *ChannelSite) (ChannelSite, error) {
	site := ChannelSite{}
	if current != nil {
		site = *current
	}
	site.Name = strings.TrimSpace(firstNonEmpty(input.Name, site.Name, "渠道站点"))
	site.Domain = normalizeChannelSiteDomain(firstNonEmpty(input.Domain, site.Domain, ""))
	site.PublicURL = normalizeChannelSitePublicURL(firstNonEmpty(input.PublicURL, site.PublicURL, site.Domain))
	site.Title = strings.TrimSpace(firstNonEmpty(input.Title, site.Title, site.Name+" API 中转站监控"))
	site.Description = strings.TrimSpace(firstNonEmpty(input.Description, site.Description, "实时查看中转站状态、精选推荐和供应商表现"))
	site.LogoMark = strings.TrimSpace(firstNonEmpty(input.LogoMark, site.LogoMark, "T"))
	site.OverviewLabel = strings.TrimSpace(firstNonEmpty(input.OverviewLabel, site.OverviewLabel, "监控总览"))
	site.RecommendLabel = strings.TrimSpace(firstNonEmpty(input.RecommendLabel, site.RecommendLabel, "精选推荐"))
	site.Modules = normalizeChannelSiteModules(input.Modules, site.Modules)
	site.Copy = normalizeChannelSiteCopy(input.Copy, site.Copy, site.Name)
	site.SEO = normalizeChannelSiteSEO(input.SEO, site.SEO, site)
	site.NavItems = normalizeChannelSiteNavItems(input.NavItems)
	site.QPSLimit = input.QPSLimit
	if site.QPSLimit <= 0 {
		site.QPSLimit = 60
	}
	if site.QPSLimit > 500 {
		site.QPSLimit = 500
	}
	site.Status = strings.TrimSpace(firstNonEmpty(input.Status, site.Status, "active"))
	if !validChannelSiteStatus(site.Status) {
		return site, ChannelSiteValidationError{Message: "渠道站点状态不合法"}
	}
	if site.Name == "" || len([]rune(site.Name)) > 60 {
		return site, ChannelSiteValidationError{Message: "渠道站点名称必须为 1-60 个字符"}
	}
	if len([]rune(site.Title)) > 90 {
		return site, ChannelSiteValidationError{Message: "站点标题不能超过 90 个字符"}
	}
	if len([]rune(site.Description)) > 180 {
		return site, ChannelSiteValidationError{Message: "站点描述不能超过 180 个字符"}
	}
	if site.LogoMark == "" || len([]rune(site.LogoMark)) > 2 {
		return site, ChannelSiteValidationError{Message: "Logo 标记必须是 1-2 个字符"}
	}
	for _, item := range site.NavItems {
		if item.Label == "" || len([]rune(item.Label)) > 24 {
			return site, ChannelSiteValidationError{Message: "自定义菜单名称必须为 1-24 个字符"}
		}
		if !validChannelSiteHref(item.Href) {
			return site, ChannelSiteValidationError{Message: "自定义菜单链接必须是站内路径、锚点或 http(s) 链接"}
		}
	}
	return site, nil
}

func normalizeChannelSiteModules(input ChannelSiteModules, fallback ChannelSiteModules) ChannelSiteModules {
	if input == (ChannelSiteModules{}) && fallback != (ChannelSiteModules{}) {
		return fallback
	}
	if input == (ChannelSiteModules{}) {
		return defaultChannelSiteModules()
	}
	return input
}

func normalizeChannelSiteCopy(input ChannelSiteCopy, fallback ChannelSiteCopy, name string) ChannelSiteCopy {
	if input.HomeIntro == "" && input.RecommendIntro == "" && input.FooterText == "" {
		if fallback != (ChannelSiteCopy{}) {
			return fallback
		}
		return defaultChannelSiteCopy(name)
	}
	input.HomeIntro = strings.TrimSpace(firstNonEmpty(input.HomeIntro, fallback.HomeIntro, "实时追踪中转站健康、延迟、可用率和真实生成表现"))
	input.RecommendIntro = strings.TrimSpace(firstNonEmpty(input.RecommendIntro, fallback.RecommendIntro, "精选可用供应商与接入入口，适合快速选择稳定通道"))
	input.FooterText = strings.TrimSpace(firstNonEmpty(input.FooterText, fallback.FooterText, name+" · Powered by TokHub"))
	return input
}

func normalizeChannelSiteSEO(input ChannelSiteSEO, fallback ChannelSiteSEO, site ChannelSite) ChannelSiteSEO {
	if input.Title == "" && input.Description == "" && input.CanonicalURL == "" {
		if fallback != (ChannelSiteSEO{}) {
			return fallback
		}
		return defaultChannelSiteSEO(site)
	}
	input.Title = strings.TrimSpace(firstNonEmpty(input.Title, fallback.Title, site.Title))
	input.Description = strings.TrimSpace(firstNonEmpty(input.Description, fallback.Description, site.Description))
	input.CanonicalURL = normalizeChannelSitePublicURL(firstNonEmpty(input.CanonicalURL, fallback.CanonicalURL, site.PublicURL))
	return input
}

func normalizeChannelSiteNavItems(items []ChannelSiteNavItem) []ChannelSiteNavItem {
	out := []ChannelSiteNavItem{}
	for i, item := range items {
		if i >= 3 {
			break
		}
		item.Label = strings.TrimSpace(item.Label)
		item.Href = strings.TrimSpace(item.Href)
		if item.Label == "" && item.Href == "" {
			continue
		}
		if item.Position <= 0 {
			item.Position = len(out) + 1
		}
		out = append(out, item)
	}
	return out
}

func defaultChannelSiteModules() ChannelSiteModules {
	return ChannelSiteModules{Overview: true, ChannelBoard: true, Recommend: true, ProviderRank: true, Strategy: true}
}

func defaultChannelSiteCopy(name string) ChannelSiteCopy {
	return ChannelSiteCopy{
		HomeIntro:      "实时追踪中转站健康、延迟、可用率和真实生成表现",
		RecommendIntro: "精选可用供应商与接入入口，适合快速选择稳定通道",
		FooterText:     name + " · Powered by TokHub",
	}
}

func defaultChannelSiteSEO(site ChannelSite) ChannelSiteSEO {
	return ChannelSiteSEO{Title: firstNonEmpty(site.Title, site.Name+" API 中转站监控"), Description: firstNonEmpty(site.Description, "实时 API 中转站监控与精选推荐"), CanonicalURL: site.PublicURL}
}

func validChannelSiteStatus(status string) bool {
	return status == "active" || status == "paused" || status == "revoked"
}

func validChannelSiteHref(href string) bool {
	href = strings.TrimSpace(href)
	if strings.HasPrefix(href, "#") && len(href) > 1 {
		return true
	}
	if strings.HasPrefix(href, "/") && !strings.HasPrefix(href, "//") {
		return true
	}
	lower := strings.ToLower(href)
	return strings.HasPrefix(lower, "https://") || strings.HasPrefix(lower, "http://")
}

func normalizeChannelSiteDomain(raw string) string {
	raw = strings.TrimSpace(raw)
	raw = strings.TrimSuffix(raw, "/")
	if raw == "" {
		return ""
	}
	if parsed, err := url.Parse(raw); err == nil && parsed.Host != "" {
		return strings.ToLower(parsed.Host)
	}
	return strings.ToLower(strings.TrimPrefix(strings.TrimPrefix(raw, "https://"), "http://"))
}

func normalizeChannelSitePublicURL(raw string) string {
	raw = strings.TrimSpace(raw)
	raw = strings.TrimSuffix(raw, "/")
	if raw == "" {
		return ""
	}
	if parsed, err := url.Parse(raw); err == nil && parsed.Scheme != "" && parsed.Host != "" {
		return raw
	}
	if strings.Contains(raw, "localhost") || strings.HasPrefix(raw, "127.") || strings.HasPrefix(raw, "0.0.0.0") {
		return "http://" + raw
	}
	return "https://" + raw
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func NewChannelSiteRuntimeKey() (string, error) {
	raw := make([]byte, 24)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return "csite-th-" + base64.RawURLEncoding.EncodeToString(raw), nil
}

func ChannelSiteRuntimeKeyPrefix(key string) string {
	if len(key) <= 14 {
		return key
	}
	return key[:14]
}
