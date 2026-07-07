package store

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

const DefaultOrgID = "org_default"

func PersonalOrgID(userID string) string {
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return DefaultOrgID
	}
	return "org_" + userID
}

type Gateway struct {
	ID          string            `json:"id"`
	OrgID       string            `json:"orgId"`
	Name        string            `json:"name"`
	Slug        string            `json:"slug"`
	BaseURL     string            `json:"baseUrl"`
	Policy      string            `json:"policy"`
	PolicyLabel string            `json:"policyLabel"`
	Status      string            `json:"status"`
	QPSLimit    int               `json:"qpsLimit"`
	QuotaMonth  int               `json:"quotaMonth"`
	CreatedAt   time.Time         `json:"createdAt"`
	Upstreams   []GatewayUpstream `json:"upstreams"`
	Stats       GatewayStats      `json:"stats"`
}

type GatewayUpstream struct {
	ID             string         `json:"id"`
	GatewayID      string         `json:"gatewayId"`
	ChannelID      string         `json:"channelId"`
	Name           string         `json:"name"`
	Provider       string         `json:"provider"`
	OwnerType      string         `json:"ownerType"`
	Type           string         `json:"type"`
	Model          string         `json:"model"`
	Endpoint       string         `json:"endpoint"`
	Status         string         `json:"status"`
	Score          int            `json:"score"`
	SuccessRate    float64        `json:"successRate"`
	LatencyP95Ms   int            `json:"latencyP95Ms"`
	CostUSD        float64        `json:"costUsd"`
	Weight         int            `json:"weight"`
	Priority       int            `json:"priority"`
	Enabled        bool           `json:"enabled"`
	Mark           string         `json:"mark"`
	ProviderConfig map[string]any `json:"providerConfig"`
}

type GatewayStats struct {
	RequestsToday int     `json:"requestsToday"`
	TokensToday   int     `json:"tokensToday"`
	CostToday     float64 `json:"costToday"`
	ErrorRate     float64 `json:"errorRate"`
	SuccessRate   float64 `json:"successRate"`
	P95LatencyMs  int     `json:"p95LatencyMs"`
	KeysActive    int     `json:"keysActive"`
}

type GatewayKey struct {
	ID           string     `json:"id"`
	OrgID        string     `json:"orgId"`
	GatewayID    string     `json:"gatewayId"`
	GatewayName  string     `json:"gatewayName"`
	Name         string     `json:"name"`
	KeyPrefix    string     `json:"keyPrefix"`
	KeyMask      string     `json:"keyMask"`
	PlainKey     string     `json:"plainKey,omitempty"`
	QuotaMonth   int        `json:"quotaMonth"`
	QPSLimit     int        `json:"qpsLimit"`
	RequestsUsed int        `json:"requestsUsed"`
	Status       string     `json:"status"`
	CreatedAt    time.Time  `json:"createdAt"`
	LastUsedAt   *time.Time `json:"lastUsedAt,omitempty"`
	ExpiresAt    *time.Time `json:"expiresAt,omitempty"`
}

type GatewayMember struct {
	UserID        string     `json:"userId"`
	Email         string     `json:"email"`
	Name          string     `json:"name"`
	Avatar        string     `json:"avatar"`
	Role          string     `json:"role"`
	GroupName     string     `json:"groupName"`
	Status        string     `json:"status"`
	RequestsToday int        `json:"requestsToday"`
	LastActiveAt  *time.Time `json:"lastActiveAt,omitempty"`
}

type GatewayCreateInput struct {
	Name          string
	Policy        string
	UpstreamIDs   []string
	QPSLimit      int
	QuotaMonth    int
	BaseURL       string
	ActorID       string
	OrgID         string
	PrivateOrgID  string
	PrivateUserID string
	AllowPlatform bool
}

type GatewayUpdateInput struct {
	Name          string
	Policy        string
	Status        string
	UpstreamIDs   []string
	UpstreamsSet  bool
	QPSLimit      int
	QuotaMonth    int
	ActorID       string
	OrgID         string
	PrivateOrgID  string
	PrivateUserID string
	AllowPlatform bool
}

type GatewayKeyCreateInput struct {
	GatewayID  string
	Name       string
	PlainKey   string
	QuotaMonth int
	QPSLimit   int
	ExpiresAt  *time.Time
	ActorID    string
	OrgID      string
}

type GatewayKeyUpdateInput struct {
	Name       string
	QuotaMonth int
	QPSLimit   int
	Status     string
	ActorID    string
	OrgID      string
}

type AuthenticatedGatewayKey struct {
	Key     GatewayKey
	Gateway Gateway
}

type GatewayRequestEvent struct {
	GatewayID         string
	GatewayKeyID      string
	UpstreamChannelID string
	RequestPath       string
	Model             string
	StatusCode        int
	RequestTokens     int
	ResponseTokens    int
	CostUSD           float64
	LatencyMs         int
	ErrorType         string
	Stream            bool
	UsageEstimated    bool
}

type GatewayChannelCredential struct {
	ChannelID   string
	Ciphertext  string
	Nonce       string
	Mask        string
	Fingerprint string
}

type GatewayUsageSummary struct {
	Totals     GatewayUsageTotals      `json:"totals"`
	QuotaMonth int                     `json:"quotaMonth"`
	Trend      []GatewayUsagePoint     `json:"trend"`
	Gateways   []GatewayUsageBreakdown `json:"gateways"`
	Models     []GatewayUsageBreakdown `json:"models"`
	Channels   []GatewayUsageBreakdown `json:"channels"`
	Rollups    []UsageRollupItem       `json:"rollups"`
	Recent     []GatewayUsageEvent     `json:"recent"`
}

type GatewayUsageFilter struct {
	Days                int
	OrgID               string
	Source              string
	GatewayID           string
	ChannelID           string
	Model               string
	MemberUserID        string
	SkipRollupRecompute bool
}

type GatewayUsageTotals struct {
	Requests  int     `json:"requests"`
	Tokens    int     `json:"tokens"`
	CostUSD   float64 `json:"costUsd"`
	ErrorRate float64 `json:"errorRate"`
}

type GatewayUsagePoint struct {
	Date     string  `json:"date"`
	Requests int     `json:"requests"`
	Tokens   int     `json:"tokens"`
	CostUSD  float64 `json:"costUsd"`
}

type GatewayUsageBreakdown struct {
	ID        string  `json:"id"`
	Name      string  `json:"name"`
	Requests  int     `json:"requests"`
	Tokens    int     `json:"tokens"`
	CostUSD   float64 `json:"costUsd"`
	ErrorRate float64 `json:"errorRate"`
}

type GatewayUsageEvent struct {
	ID         string    `json:"id"`
	Gateway    string    `json:"gateway"`
	KeyName    string    `json:"keyName"`
	Channel    string    `json:"channel"`
	Model      string    `json:"model"`
	StatusCode int       `json:"statusCode"`
	Tokens     int       `json:"tokens"`
	CostUSD    float64   `json:"costUsd"`
	LatencyMs  int       `json:"latencyMs"`
	Stream     bool      `json:"stream"`
	CreatedAt  time.Time `json:"createdAt"`
	ErrorType  string    `json:"errorType,omitempty"`
	Estimated  bool      `json:"estimated"`
}

func (r *Repository) EnsurePersonalWorkspace(ctx context.Context, user PublicUser) (string, error) {
	orgID := PersonalOrgID(user.ID)
	slug := UniqueSlug(orgID)
	if _, err := r.db.Exec(ctx, `
		insert into orgs(id,name,slug,plan,status,timezone)
		values($1,$2,$3,'starter','active','Asia/Shanghai')
		on conflict(id) do nothing
	`, orgID, defaultPersonalWorkspaceName(user), slug); err != nil {
		return "", err
	}
	if _, err := r.db.Exec(ctx, `
		insert into org_members(org_id,user_id,role,group_name,status)
		values($1,$2,'owner','个人工作区','active')
		on conflict(org_id,user_id) do update
		set role=case when exists(select 1 from orgs where id=$1 and status='active' and deleted_at is null) then excluded.role else org_members.role end,
			group_name=case when exists(select 1 from orgs where id=$1 and status='active' and deleted_at is null) then excluded.group_name else org_members.group_name end,
			status=case when exists(select 1 from orgs where id=$1 and status='active' and deleted_at is null) then 'active' else org_members.status end
	`, orgID, user.ID); err != nil {
		return "", err
	}
	return orgID, nil
}

func ensureActiveOrg(ctx context.Context, q DBTX, orgID string) error {
	var active bool
	if err := q.QueryRow(ctx, `select exists(select 1 from orgs where id=$1 and status='active' and deleted_at is null)`, orgID).Scan(&active); err != nil {
		return err
	}
	if !active {
		return pgx.ErrNoRows
	}
	return nil
}

func (r *Repository) GatewayAdminData(ctx context.Context) ([]Gateway, []GatewayUpstream, error) {
	gateways, err := r.ListGateways(ctx)
	if err != nil {
		return nil, nil, err
	}
	upstreams, err := r.AvailableGatewayUpstreams(ctx)
	return gateways, upstreams, err
}

func (r *Repository) GatewayConsoleData(ctx context.Context, user PublicUser) ([]Gateway, []GatewayUpstream, error) {
	orgID, err := r.EnsurePersonalWorkspace(ctx, user)
	if err != nil {
		return nil, nil, err
	}
	return r.GatewayConsoleDataForOrg(ctx, user, orgID)
}

func (r *Repository) GatewayConsoleDataForOrg(ctx context.Context, user PublicUser, orgID string) ([]Gateway, []GatewayUpstream, error) {
	gateways, err := r.ListGatewaysForOrg(ctx, orgID)
	if err != nil {
		return nil, nil, err
	}
	upstreams, err := r.AvailableGatewayUpstreamsForOrg(ctx, orgID, user.CanUsePlatformGatewayUpstreams())
	return gateways, upstreams, err
}

func (r *Repository) ListGateways(ctx context.Context) ([]Gateway, error) {
	return r.ListGatewaysForOrg(ctx, DefaultOrgID)
}

func (r *Repository) ListGatewaysForOrg(ctx context.Context, orgID string) ([]Gateway, error) {
	if orgID == "" {
		orgID = DefaultOrgID
	}
	rows, err := r.db.Query(ctx, `
		select id,org_id,name,slug,base_url,policy,status,qps_limit,quota_month,created_at
		from gateways
		where org_id=$1 and status <> 'deleted'
		order by created_at desc
	`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := []Gateway{}
	for rows.Next() {
		var g Gateway
		if err := rows.Scan(&g.ID, &g.OrgID, &g.Name, &g.Slug, &g.BaseURL, &g.Policy, &g.Status, &g.QPSLimit, &g.QuotaMonth, &g.CreatedAt); err != nil {
			return nil, err
		}
		g.PolicyLabel = gatewayPolicyLabel(g.Policy)
		items = append(items, g)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range items {
		upstreams, err := r.GatewayUpstreams(ctx, items[i].ID)
		if err != nil {
			return nil, err
		}
		stats, err := r.GatewayStats(ctx, items[i].ID)
		if err != nil {
			return nil, err
		}
		items[i].Upstreams = upstreams
		items[i].Stats = stats
	}
	return items, nil
}

func (r *Repository) GatewayByID(ctx context.Context, gatewayID string) (Gateway, error) {
	return r.GatewayByIDForOrg(ctx, gatewayID, DefaultOrgID)
}

func (r *Repository) GatewayByIDForOrg(ctx context.Context, gatewayID string, orgID string) (Gateway, error) {
	if orgID == "" {
		orgID = DefaultOrgID
	}
	var g Gateway
	err := r.db.QueryRow(ctx, `
		select id,org_id,name,slug,base_url,policy,status,qps_limit,quota_month,created_at
		from gateways
		where id=$1 and org_id=$2 and status <> 'deleted'
	`, gatewayID, orgID).Scan(&g.ID, &g.OrgID, &g.Name, &g.Slug, &g.BaseURL, &g.Policy, &g.Status, &g.QPSLimit, &g.QuotaMonth, &g.CreatedAt)
	if err != nil {
		return Gateway{}, err
	}
	g.PolicyLabel = gatewayPolicyLabel(g.Policy)
	g.Upstreams, err = r.GatewayUpstreams(ctx, g.ID)
	if err != nil {
		return Gateway{}, err
	}
	g.Stats, err = r.GatewayStats(ctx, g.ID)
	return g, err
}

func (r *Repository) AvailableGatewayUpstreams(ctx context.Context) ([]GatewayUpstream, error) {
	return r.availableGatewayUpstreams(ctx, "", true)
}

func (r *Repository) AvailableGatewayUpstreamsForUser(ctx context.Context, userID string, allowPlatform bool) ([]GatewayUpstream, error) {
	return r.AvailableGatewayUpstreamsForOrg(ctx, PersonalOrgID(userID), allowPlatform)
}

func (r *Repository) AvailableGatewayUpstreamsForOrg(ctx context.Context, orgID string, allowPlatform bool) ([]GatewayUpstream, error) {
	return r.availableGatewayUpstreams(ctx, orgID, allowPlatform)
}

func gatewaySQLAlias(alias string) string {
	alias = strings.TrimSpace(alias)
	if alias == "" {
		return ""
	}
	if strings.HasSuffix(alias, ".") {
		return alias
	}
	return alias + "."
}

func gatewayEligiblePlatformPredicate(alias string) string {
	prefix := gatewaySQLAlias(alias)
	return prefix + "owner_type='platform' and " +
		prefix + "gateway_enabled is true and " +
		prefix + "status in ('healthy','degraded') and " +
		prefix + "deleted_at is null"
}

func gatewayEligiblePrivatePredicate(alias string, orgParam string) string {
	prefix := gatewaySQLAlias(alias)
	return prefix + "owner_type='user' and " +
		prefix + "gateway_enabled is true and " +
		"coalesce(" + prefix + "org_id,'org_' || coalesce(" + prefix + "owner_id,''))=" + orgParam + " and " +
		prefix + "status in ('healthy','degraded') and " +
		prefix + "deleted_at is null"
}

func gatewayRuntimePlatformCredentialPredicate(alias string, orgParam string, defaultOrgParam string) string {
	prefix := gatewaySQLAlias(alias)
	return prefix + "owner_type='platform' and " +
		prefix + "gateway_enabled is true and (" +
		orgParam + "=" + defaultOrgParam + " or exists (" +
		"select 1 from users u where " + orgParam + "='org_' || u.id " +
		"and u.plan='super_vip' and u.status='active' and u.deleted_at is null" +
		"))"
}

func (r *Repository) availableGatewayUpstreams(ctx context.Context, orgID string, allowPlatform bool) ([]GatewayUpstream, error) {
	where := gatewayEligiblePlatformPredicate("c")
	args := []any{}
	if strings.TrimSpace(orgID) != "" {
		privateWhere := gatewayEligiblePrivatePredicate("c", "$1")
		where = privateWhere
		if allowPlatform {
			where = "((" + gatewayEligiblePlatformPredicate("c") + ") or (" + privateWhere + "))"
		}
		args = append(args, orgID)
	}
	rows, err := r.db.Query(ctx, `
		select c.id,c.name,c.provider,c.owner_type,c.type,c.model,c.endpoint,c.status,c.score,
			coalesce(s.success_rate,0),coalesce(s.latency_p95_ms,0),coalesce(s.cost_usd,0),coalesce(c.provider_config,'{}'::jsonb)
		from channels c
		left join lateral (
			select * from channel_status_snapshots ss where ss.channel_id=c.id order by sampled_at desc limit 1
		) s on true
		where `+where+`
		order by c.score desc, c.name asc
	`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []GatewayUpstream{}
	for rows.Next() {
		var item GatewayUpstream
		var providerConfigRaw []byte
		if err := rows.Scan(&item.ChannelID, &item.Name, &item.Provider, &item.OwnerType, &item.Type, &item.Model, &item.Endpoint, &item.Status, &item.Score, &item.SuccessRate, &item.LatencyP95Ms, &item.CostUSD, &providerConfigRaw); err != nil {
			return nil, err
		}
		item.ProviderConfig = decodeMap(providerConfigRaw)
		item.Enabled = true
		item.Mark = providerMark(item.Provider)
		out = append(out, item)
	}
	return out, rows.Err()
}

func (r *Repository) GatewayUpstreams(ctx context.Context, gatewayID string) ([]GatewayUpstream, error) {
	rows, err := r.db.Query(ctx, `
		select gu.id,gu.gateway_id,gu.channel_id,c.name,c.provider,c.owner_type,c.type,c.model,c.endpoint,c.status,c.score,
			coalesce(s.success_rate,0),coalesce(s.latency_p95_ms,0),coalesce(s.cost_usd,0),
			gu.weight,gu.priority,gu.enabled,coalesce(c.provider_config,'{}'::jsonb)
		from gateway_upstreams gu
		join channels c on c.id=gu.channel_id
		left join lateral (
			select * from channel_status_snapshots ss where ss.channel_id=c.id order by sampled_at desc limit 1
		) s on true
		where gu.gateway_id=$1
		order by gu.priority asc, c.score desc, c.name asc
	`, gatewayID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []GatewayUpstream{}
	for rows.Next() {
		var item GatewayUpstream
		var providerConfigRaw []byte
		if err := rows.Scan(&item.ID, &item.GatewayID, &item.ChannelID, &item.Name, &item.Provider, &item.OwnerType, &item.Type, &item.Model, &item.Endpoint, &item.Status, &item.Score, &item.SuccessRate, &item.LatencyP95Ms, &item.CostUSD, &item.Weight, &item.Priority, &item.Enabled, &providerConfigRaw); err != nil {
			return nil, err
		}
		item.ProviderConfig = decodeMap(providerConfigRaw)
		item.Mark = providerMark(item.Provider)
		out = append(out, item)
	}
	return out, rows.Err()
}

func (r *Repository) CreateGateway(ctx context.Context, input GatewayCreateInput) (Gateway, error) {
	name := strings.TrimSpace(input.Name)
	if name == "" {
		name = "专属中转站"
	}
	policy := normalizeGatewayPolicy(input.Policy)
	orgID := strings.TrimSpace(input.OrgID)
	if orgID == "" {
		orgID = DefaultOrgID
	}
	upstreamIDs := uniqueStrings(input.UpstreamIDs)
	if len(upstreamIDs) == 0 {
		return Gateway{}, fmt.Errorf("gateway requires at least one upstream")
	}
	privateOrgID := strings.TrimSpace(input.PrivateOrgID)
	if privateOrgID == "" && strings.TrimSpace(input.PrivateUserID) != "" {
		privateOrgID = PersonalOrgID(input.PrivateUserID)
	}
	valid, err := r.gatewayAllowedChannelIDs(ctx, upstreamIDs, privateOrgID, input.AllowPlatform)
	if err != nil {
		return Gateway{}, err
	}
	if len(valid) != len(upstreamIDs) {
		return Gateway{}, fmt.Errorf("%s", gatewayUpstreamAccessMessage(privateOrgID, input.AllowPlatform))
	}
	qps := input.QPSLimit
	if qps <= 0 {
		qps = 60
	}
	quota := input.QuotaMonth
	if quota <= 0 {
		quota = 100000
	}
	slug := UniqueSlug(name)
	gatewayID := "gw_" + uuid.NewString()

	tx, err := r.db.Begin(ctx)
	if err != nil {
		return Gateway{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := ensureActiveOrg(ctx, tx, orgID); err != nil {
		return Gateway{}, err
	}
	if _, err := tx.Exec(ctx, `
		insert into gateways(id,org_id,name,slug,base_url,policy,status,qps_limit,quota_month,created_by,created_at,updated_at)
		values($1,$2,$3,$4,$5,$6,'active',$7,$8,$9,now(),now())
	`, gatewayID, orgID, name, slug, input.BaseURL, policy, qps, quota, input.ActorID); err != nil {
		return Gateway{}, err
	}
	for idx, channelID := range upstreamIDs {
		if _, err := tx.Exec(ctx, `
			insert into gateway_upstreams(id,gateway_id,channel_id,weight,priority,enabled,created_at)
			values($1,$2,$3,100,$4,true,now())
		`, "gwu_"+uuid.NewString(), gatewayID, channelID, idx); err != nil {
			return Gateway{}, err
		}
	}
	if _, err := tx.Exec(ctx, `
		insert into audit_events(id,actor_type,actor_id,action,object_type,object_id,result,metadata)
		values($1,'user',$2,'gateway.created','gateway',$3,'success',jsonb_build_object('name',$4::text,'upstreams',$5::int,'policy',$6::text))
	`, "aud_"+uuid.NewString(), input.ActorID, gatewayID, name, len(upstreamIDs), policy); err != nil {
		return Gateway{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Gateway{}, err
	}
	return r.GatewayByIDForOrg(ctx, gatewayID, orgID)
}

func (r *Repository) UpdateGateway(ctx context.Context, gatewayID string, input GatewayUpdateInput) (Gateway, error) {
	return r.UpdateGatewayForOrg(ctx, gatewayID, DefaultOrgID, input)
}

func (r *Repository) UpdateGatewayForOrg(ctx context.Context, gatewayID string, orgID string, input GatewayUpdateInput) (Gateway, error) {
	if orgID == "" {
		orgID = DefaultOrgID
	}
	current, err := r.GatewayByIDForOrg(ctx, gatewayID, orgID)
	if err != nil {
		return Gateway{}, err
	}
	name := strings.TrimSpace(input.Name)
	if name == "" {
		name = current.Name
	}
	policy := current.Policy
	if strings.TrimSpace(input.Policy) != "" {
		policy = normalizeGatewayPolicy(input.Policy)
	}
	status := current.Status
	if strings.TrimSpace(input.Status) != "" {
		status, err = validateGatewayStatus(input.Status)
		if err != nil {
			return Gateway{}, err
		}
	}
	qps := input.QPSLimit
	if qps <= 0 {
		qps = current.QPSLimit
	}
	quota := input.QuotaMonth
	if quota <= 0 {
		quota = current.QuotaMonth
	}
	upstreamIDs := uniqueStrings(input.UpstreamIDs)
	if input.UpstreamsSet {
		if len(upstreamIDs) == 0 {
			return Gateway{}, fmt.Errorf("gateway requires at least one upstream")
		}
		privateOrgID := strings.TrimSpace(input.PrivateOrgID)
		if privateOrgID == "" && strings.TrimSpace(input.PrivateUserID) != "" {
			privateOrgID = PersonalOrgID(input.PrivateUserID)
		}
		valid, err := r.gatewayAllowedChannelIDs(ctx, upstreamIDs, privateOrgID, input.AllowPlatform)
		if err != nil {
			return Gateway{}, err
		}
		if len(valid) != len(upstreamIDs) {
			return Gateway{}, fmt.Errorf("%s", gatewayUpstreamAccessMessage(privateOrgID, input.AllowPlatform))
		}
	}

	tx, err := r.db.Begin(ctx)
	if err != nil {
		return Gateway{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	tag, err := tx.Exec(ctx, `
		update gateways
		set name=$3, policy=$4, status=$5, qps_limit=$6, quota_month=$7, updated_at=now()
		where id=$1 and org_id=$2 and status <> 'deleted'
	`, gatewayID, orgID, name, policy, status, qps, quota)
	if err != nil {
		return Gateway{}, err
	}
	if tag.RowsAffected() == 0 {
		return Gateway{}, pgx.ErrNoRows
	}
	if input.UpstreamsSet {
		if _, err := tx.Exec(ctx, `delete from gateway_upstreams where gateway_id=$1`, gatewayID); err != nil {
			return Gateway{}, err
		}
		for idx, channelID := range upstreamIDs {
			if _, err := tx.Exec(ctx, `
				insert into gateway_upstreams(id,gateway_id,channel_id,weight,priority,enabled,created_at)
				values($1,$2,$3,100,$4,true,now())
			`, "gwu_"+uuid.NewString(), gatewayID, channelID, idx); err != nil {
				return Gateway{}, err
			}
		}
	}
	if status == "deleted" {
		if _, err := tx.Exec(ctx, `update gateway_keys set status='revoked', revoked_at=coalesce(revoked_at, now()) where gateway_id=$1 and status='active'`, gatewayID); err != nil {
			return Gateway{}, err
		}
	}
	if _, err := tx.Exec(ctx, `
		insert into audit_events(id,actor_type,actor_id,action,object_type,object_id,result,metadata)
		values($1,'user',$2,'gateway.updated','gateway',$3,'success',jsonb_build_object('name',$4::text,'status',$5::text,'policy',$6::text))
	`, "aud_"+uuid.NewString(), input.ActorID, gatewayID, name, status, policy); err != nil {
		return Gateway{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Gateway{}, err
	}
	return r.GatewayByIDForOrg(ctx, gatewayID, orgID)
}

func (r *Repository) DeleteGateway(ctx context.Context, gatewayID string, actorID string) error {
	return r.DeleteGatewayForOrg(ctx, gatewayID, actorID, DefaultOrgID)
}

func (r *Repository) DeleteGatewayForOrg(ctx context.Context, gatewayID string, actorID string, orgID string) error {
	if orgID == "" {
		orgID = DefaultOrgID
	}
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	tag, err := tx.Exec(ctx, `update gateways set status='deleted', updated_at=now() where id=$1 and org_id=$2 and status <> 'deleted'`, gatewayID, orgID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	if _, err := tx.Exec(ctx, `update gateway_keys set status='revoked', revoked_at=coalesce(revoked_at, now()) where gateway_id=$1 and status='active'`, gatewayID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		insert into audit_events(id,actor_type,actor_id,action,object_type,object_id,result,metadata)
		values($1,'user',$2,'gateway.deleted','gateway',$3,'success','{}'::jsonb)
	`, "aud_"+uuid.NewString(), actorID, gatewayID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (r *Repository) BulkUpdateGateways(ctx context.Context, ids []string, actorID string, status string) ([]Gateway, error) {
	return r.BulkUpdateGatewaysForOrg(ctx, ids, actorID, DefaultOrgID, status)
}

func (r *Repository) BulkUpdateGatewaysForOrg(ctx context.Context, ids []string, actorID string, orgID string, status string) ([]Gateway, error) {
	if orgID == "" {
		orgID = DefaultOrgID
	}
	ids = uniqueStrings(ids)
	if len(ids) == 0 {
		return nil, fmt.Errorf("select at least one gateway")
	}
	nextStatus, err := validateGatewayStatus(status)
	if err != nil {
		return nil, err
	}
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	rows, err := tx.Query(ctx, `
		update gateways
		set status=$3, updated_at=now()
		where id=any($1) and org_id=$2 and status <> 'deleted'
		returning id
	`, ids, orgID, nextStatus)
	if err != nil {
		return nil, err
	}
	updated := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		updated = append(updated, id)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()
	if len(updated) != len(ids) {
		return nil, pgx.ErrNoRows
	}
	if nextStatus == "deleted" {
		if _, err := tx.Exec(ctx, `update gateway_keys set status='revoked', revoked_at=coalesce(revoked_at, now()) where gateway_id=any($1) and status='active'`, updated); err != nil {
			return nil, err
		}
	}
	for _, id := range updated {
		if _, err := tx.Exec(ctx, `
			insert into audit_events(id,actor_type,actor_id,action,object_type,object_id,result,metadata)
			values($1,'user',$2,'gateway.bulk_status','gateway',$3,'success',jsonb_build_object('status',$4::text))
		`, "aud_"+uuid.NewString(), actorID, id, nextStatus); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return r.ListGatewaysForOrg(ctx, orgID)
}

func (r *Repository) BulkDeleteGateways(ctx context.Context, ids []string, actorID string) ([]Gateway, error) {
	return r.BulkDeleteGatewaysForOrg(ctx, ids, actorID, DefaultOrgID)
}

func (r *Repository) BulkDeleteGatewaysForOrg(ctx context.Context, ids []string, actorID string, orgID string) ([]Gateway, error) {
	return r.BulkUpdateGatewaysForOrg(ctx, ids, actorID, orgID, "deleted")
}

func (r *Repository) platformChannelIDs(ctx context.Context, ids []string) (map[string]bool, error) {
	rows, err := r.db.Query(ctx, `
		select id from channels
		where id=any($1)
			and (`+gatewayEligiblePlatformPredicate("")+`)
	`, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out[id] = true
	}
	return out, rows.Err()
}

func (r *Repository) gatewayAllowedChannelIDs(ctx context.Context, ids []string, privateOrgID string, allowPlatform bool) (map[string]bool, error) {
	if strings.TrimSpace(privateOrgID) == "" {
		return r.platformChannelIDs(ctx, ids)
	}
	privateWhere := gatewayEligiblePrivatePredicate("", "$2")
	where := privateWhere
	if allowPlatform {
		where = "(" + gatewayEligiblePlatformPredicate("") + ") or (" + privateWhere + ")"
	}
	rows, err := r.db.Query(ctx, `
		select id from channels
		where id=any($1)
			and (`+where+`)
	`, ids, privateOrgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out[id] = true
	}
	return out, rows.Err()
}

func gatewayUpstreamAccessMessage(privateOrgID string, allowPlatform bool) string {
	if strings.TrimSpace(privateOrgID) == "" {
		return "platform gateways can only use enabled, healthy or degraded platform channels"
	}
	if allowPlatform {
		return "Super VIP gateways can only use enabled, healthy or degraded platform channels, or tested private channels in this workspace"
	}
	return "普通用户的专属中转站只能使用当前工作区已通过检测的私有通道；如需一键使用平台监控通道，请先由管理员开通 Super VIP"
}

func (r *Repository) CreateGatewayKey(ctx context.Context, input GatewayKeyCreateInput) (GatewayKey, error) {
	orgID := strings.TrimSpace(input.OrgID)
	if orgID == "" {
		orgID = DefaultOrgID
	}
	gateway, err := r.GatewayByIDForOrg(ctx, input.GatewayID, orgID)
	if err != nil {
		return GatewayKey{}, err
	}
	if err := ensureActiveOrg(ctx, r.db, orgID); err != nil {
		return GatewayKey{}, err
	}
	name := strings.TrimSpace(input.Name)
	if name == "" {
		name = "默认调用密钥"
	}
	plain := strings.TrimSpace(input.PlainKey)
	if plain == "" {
		var err error
		plain, err = NewGatewayPlainKey()
		if err != nil {
			return GatewayKey{}, err
		}
	}
	hash := HashGatewayKey(plain)
	prefix := GatewayKeyPrefix(plain)
	mask := MaskGatewayKey(plain)
	quota := input.QuotaMonth
	if quota <= 0 {
		quota = gateway.QuotaMonth
	}
	qps := input.QPSLimit
	if qps <= 0 {
		qps = gateway.QPSLimit
	}
	var key GatewayKey
	var expires sql.NullTime
	if input.ExpiresAt != nil {
		expires = sql.NullTime{Time: *input.ExpiresAt, Valid: true}
	}
	err = r.db.QueryRow(ctx, `
			insert into gateway_keys(id,org_id,gateway_id,name,key_hash,key_prefix,key_mask,key_ciphertext,key_nonce,quota_month,qps_limit,status,expires_at,created_by,created_at)
			values($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,'active',$12,$13,now())
			returning id,org_id,gateway_id,name,key_prefix,key_mask,quota_month,qps_limit,requests_used,status,created_at,last_used_at,expires_at
		`, "gwk_"+uuid.NewString(), gateway.OrgID, gateway.ID, name, hash, prefix, mask, "", "", quota, qps, nullableTime(expires), input.ActorID).
		Scan(&key.ID, &key.OrgID, &key.GatewayID, &key.Name, &key.KeyPrefix, &key.KeyMask, &key.QuotaMonth, &key.QPSLimit, &key.RequestsUsed, &key.Status, &key.CreatedAt, nullableTimePtr(&key.LastUsedAt), nullableTimePtr(&key.ExpiresAt))
	if err != nil {
		return GatewayKey{}, err
	}
	key.GatewayName = gateway.Name
	key.PlainKey = plain
	_ = r.WriteAudit(ctx, AuditEvent{
		ActorType:  "user",
		ActorID:    input.ActorID,
		Action:     "gateway.key.created",
		ObjectType: "gateway_key",
		ObjectID:   key.ID,
		Result:     "success",
		Metadata:   map[string]any{"gateway_id": gateway.ID, "name": key.Name, "key_prefix": key.KeyPrefix},
	})
	return key, nil
}

func (r *Repository) ListGatewayKeys(ctx context.Context) ([]GatewayKey, error) {
	return r.ListGatewayKeysForOrg(ctx, DefaultOrgID)
}

func (r *Repository) ListGatewayKeysForOrg(ctx context.Context, orgID string) ([]GatewayKey, error) {
	if orgID == "" {
		orgID = DefaultOrgID
	}
	rows, err := r.db.Query(ctx, `
		select k.id,k.org_id,k.gateway_id,g.name,k.name,k.key_prefix,k.key_mask,k.quota_month,k.qps_limit,k.requests_used,k.status,k.created_at,k.last_used_at,k.expires_at
		from gateway_keys k
		join gateways g on g.id=k.gateway_id
		where k.org_id=$1
			and k.deleted_at is null
			and g.status <> 'deleted'
		order by k.created_at desc
	`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []GatewayKey{}
	for rows.Next() {
		var key GatewayKey
		if err := rows.Scan(&key.ID, &key.OrgID, &key.GatewayID, &key.GatewayName, &key.Name, &key.KeyPrefix, &key.KeyMask, &key.QuotaMonth, &key.QPSLimit, &key.RequestsUsed, &key.Status, &key.CreatedAt, nullableTimePtr(&key.LastUsedAt), nullableTimePtr(&key.ExpiresAt)); err != nil {
			return nil, err
		}
		out = append(out, key)
	}
	return out, rows.Err()
}

func (r *Repository) RevokeGatewayKey(ctx context.Context, keyID string, actorID string) error {
	return r.RevokeGatewayKeyForOrg(ctx, keyID, actorID, DefaultOrgID)
}

func (r *Repository) UpdateGatewayKey(ctx context.Context, keyID string, input GatewayKeyUpdateInput) (GatewayKey, error) {
	return r.UpdateGatewayKeyForOrg(ctx, keyID, DefaultOrgID, input)
}

func (r *Repository) UpdateGatewayKeyForOrg(ctx context.Context, keyID string, orgID string, input GatewayKeyUpdateInput) (GatewayKey, error) {
	if orgID == "" {
		orgID = DefaultOrgID
	}
	var current GatewayKey
	err := r.db.QueryRow(ctx, `
		select k.id,k.org_id,k.gateway_id,g.name,k.name,k.key_prefix,k.key_mask,k.quota_month,k.qps_limit,k.requests_used,k.status,k.created_at,k.last_used_at,k.expires_at
		from gateway_keys k
		join gateways g on g.id=k.gateway_id
		where k.id=$1 and k.org_id=$2 and k.deleted_at is null and g.status <> 'deleted'
	`, keyID, orgID).Scan(&current.ID, &current.OrgID, &current.GatewayID, &current.GatewayName, &current.Name, &current.KeyPrefix, &current.KeyMask, &current.QuotaMonth, &current.QPSLimit, &current.RequestsUsed, &current.Status, &current.CreatedAt, nullableTimePtr(&current.LastUsedAt), nullableTimePtr(&current.ExpiresAt))
	if err != nil {
		return GatewayKey{}, err
	}
	if current.Status == "revoked" {
		return GatewayKey{}, fmt.Errorf("revoked gateway keys cannot be edited")
	}
	name := strings.TrimSpace(input.Name)
	if name == "" {
		name = current.Name
	}
	quota := input.QuotaMonth
	if quota <= 0 {
		quota = current.QuotaMonth
	}
	qps := input.QPSLimit
	if qps <= 0 {
		qps = current.QPSLimit
	}
	status := current.Status
	if strings.TrimSpace(input.Status) != "" {
		status, err = validateGatewayKeyEditableStatus(input.Status)
		if err != nil {
			return GatewayKey{}, err
		}
	}
	var key GatewayKey
	err = r.db.QueryRow(ctx, `
		update gateway_keys
		set name=$3, quota_month=$4, qps_limit=$5, status=$6,
			revoked_at=case when $6='active' then null else revoked_at end
		where id=$1 and org_id=$2 and deleted_at is null
			and exists(select 1 from gateways g where g.id=gateway_keys.gateway_id and g.status <> 'deleted')
		returning id,org_id,gateway_id,name,key_prefix,key_mask,quota_month,qps_limit,requests_used,status,created_at,last_used_at,expires_at
	`, keyID, orgID, name, quota, qps, status).Scan(&key.ID, &key.OrgID, &key.GatewayID, &key.Name, &key.KeyPrefix, &key.KeyMask, &key.QuotaMonth, &key.QPSLimit, &key.RequestsUsed, &key.Status, &key.CreatedAt, nullableTimePtr(&key.LastUsedAt), nullableTimePtr(&key.ExpiresAt))
	if err != nil {
		return GatewayKey{}, err
	}
	key.GatewayName = current.GatewayName
	_ = r.WriteAudit(ctx, AuditEvent{
		ActorType:  "user",
		ActorID:    input.ActorID,
		Action:     "gateway.key.updated",
		ObjectType: "gateway_key",
		ObjectID:   keyID,
		Result:     "success",
		Metadata:   map[string]any{"name": name, "status": status, "key_prefix": key.KeyPrefix},
	})
	return key, nil
}

func (r *Repository) RevokeGatewayKeyForOrg(ctx context.Context, keyID string, actorID string, orgID string) error {
	if orgID == "" {
		orgID = DefaultOrgID
	}
	tag, err := r.db.Exec(ctx, `
		update gateway_keys k
		set status='revoked', revoked_at=now()
		from gateways g
		where k.id=$1
			and k.org_id=$2
			and k.status<>'revoked'
			and k.deleted_at is null
			and g.id=k.gateway_id
			and g.status <> 'deleted'
	`, keyID, orgID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	return r.WriteAudit(ctx, AuditEvent{
		ActorType:  "user",
		ActorID:    actorID,
		Action:     "gateway.key.revoked",
		ObjectType: "gateway_key",
		ObjectID:   keyID,
		Result:     "success",
		Metadata:   map[string]any{},
	})
}

func (r *Repository) DeleteGatewayKey(ctx context.Context, keyID string, actorID string) error {
	return r.DeleteGatewayKeyForOrg(ctx, keyID, actorID, DefaultOrgID)
}

func (r *Repository) DeleteGatewayKeyForOrg(ctx context.Context, keyID string, actorID string, orgID string) error {
	if orgID == "" {
		orgID = DefaultOrgID
	}
	var prefix string
	tag, err := r.db.Exec(ctx, `
		update gateway_keys
		set status='revoked', revoked_at=coalesce(revoked_at, now()), deleted_at=now()
		where id=$1 and org_id=$2 and deleted_at is null
	`, keyID, orgID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	_ = r.db.QueryRow(ctx, `select key_prefix from gateway_keys where id=$1 and org_id=$2`, keyID, orgID).Scan(&prefix)
	return r.WriteAudit(ctx, AuditEvent{
		ActorType:  "user",
		ActorID:    actorID,
		Action:     "gateway.key.deleted",
		ObjectType: "gateway_key",
		ObjectID:   keyID,
		Result:     "success",
		Metadata:   map[string]any{"key_prefix": prefix},
	})
}

func (r *Repository) BulkUpdateGatewayKeys(ctx context.Context, ids []string, actorID string, status string) ([]GatewayKey, error) {
	return r.BulkUpdateGatewayKeysForOrg(ctx, ids, actorID, DefaultOrgID, status)
}

func (r *Repository) BulkUpdateGatewayKeysForOrg(ctx context.Context, ids []string, actorID string, orgID string, status string) ([]GatewayKey, error) {
	if orgID == "" {
		orgID = DefaultOrgID
	}
	ids = uniqueStrings(ids)
	if len(ids) == 0 {
		return nil, fmt.Errorf("select at least one gateway key")
	}
	nextStatus, err := validateGatewayKeyEditableStatus(status)
	if err != nil {
		return nil, err
	}
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	states, err := gatewayKeyBulkStatesForOrg(ctx, tx, ids, orgID, true)
	if err != nil {
		return nil, err
	}
	for _, state := range states {
		if state.status == "revoked" {
			return nil, fmt.Errorf("revoked gateway keys cannot be edited")
		}
	}
	tag, err := tx.Exec(ctx, `
		update gateway_keys k
		set status=$3,
			revoked_at=case when $3='active' then null else revoked_at end
		from gateways g
		where k.id=any($1)
			and k.org_id=$2
			and k.deleted_at is null
			and g.id=k.gateway_id
			and g.status <> 'deleted'
	`, ids, orgID, nextStatus)
	if err != nil {
		return nil, err
	}
	if int(tag.RowsAffected()) != len(ids) {
		return nil, pgx.ErrNoRows
	}
	for _, id := range ids {
		state := states[id]
		if _, err := tx.Exec(ctx, `
			insert into audit_events(id,actor_type,actor_id,action,object_type,object_id,result,metadata)
			values($1,'user',$2,'gateway.key.bulk_status','gateway_key',$3,'success',jsonb_build_object('status',$4::text,'previous_status',$5::text,'key_prefix',$6::text))
		`, "aud_"+uuid.NewString(), actorID, id, nextStatus, state.status, state.keyPrefix); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return r.ListGatewayKeysForOrg(ctx, orgID)
}

func (r *Repository) BulkRevokeGatewayKeys(ctx context.Context, ids []string, actorID string) ([]GatewayKey, error) {
	return r.BulkRevokeGatewayKeysForOrg(ctx, ids, actorID, DefaultOrgID)
}

func (r *Repository) BulkRevokeGatewayKeysForOrg(ctx context.Context, ids []string, actorID string, orgID string) ([]GatewayKey, error) {
	if orgID == "" {
		orgID = DefaultOrgID
	}
	ids = uniqueStrings(ids)
	if len(ids) == 0 {
		return nil, fmt.Errorf("select at least one gateway key")
	}
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	states, err := gatewayKeyBulkStatesForOrg(ctx, tx, ids, orgID, true)
	if err != nil {
		return nil, err
	}
	tag, err := tx.Exec(ctx, `
		update gateway_keys k
		set status='revoked', revoked_at=coalesce(revoked_at, now())
		from gateways g
		where k.id=any($1)
			and k.org_id=$2
			and k.deleted_at is null
			and g.id=k.gateway_id
			and g.status <> 'deleted'
	`, ids, orgID)
	if err != nil {
		return nil, err
	}
	if int(tag.RowsAffected()) != len(ids) {
		return nil, pgx.ErrNoRows
	}
	for _, id := range ids {
		state := states[id]
		if _, err := tx.Exec(ctx, `
			insert into audit_events(id,actor_type,actor_id,action,object_type,object_id,result,metadata)
			values($1,'user',$2,'gateway.key.bulk_revoked','gateway_key',$3,'success',jsonb_build_object('previous_status',$4::text,'key_prefix',$5::text))
		`, "aud_"+uuid.NewString(), actorID, id, state.status, state.keyPrefix); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return r.ListGatewayKeysForOrg(ctx, orgID)
}

func (r *Repository) BulkDeleteGatewayKeys(ctx context.Context, ids []string, actorID string) ([]GatewayKey, error) {
	return r.BulkDeleteGatewayKeysForOrg(ctx, ids, actorID, DefaultOrgID)
}

func (r *Repository) BulkDeleteGatewayKeysForOrg(ctx context.Context, ids []string, actorID string, orgID string) ([]GatewayKey, error) {
	if orgID == "" {
		orgID = DefaultOrgID
	}
	ids = uniqueStrings(ids)
	if len(ids) == 0 {
		return nil, fmt.Errorf("select at least one gateway key")
	}
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	states, err := gatewayKeyBulkStatesForOrg(ctx, tx, ids, orgID, false)
	if err != nil {
		return nil, err
	}
	tag, err := tx.Exec(ctx, `
		update gateway_keys
		set status='revoked', revoked_at=coalesce(revoked_at, now()), deleted_at=now()
		where id=any($1) and org_id=$2 and deleted_at is null
	`, ids, orgID)
	if err != nil {
		return nil, err
	}
	if int(tag.RowsAffected()) != len(ids) {
		return nil, pgx.ErrNoRows
	}
	for _, id := range ids {
		state := states[id]
		if _, err := tx.Exec(ctx, `
			insert into audit_events(id,actor_type,actor_id,action,object_type,object_id,result,metadata)
			values($1,'user',$2,'gateway.key.deleted','gateway_key',$3,'success',jsonb_build_object('key_prefix',$4::text,'previous_status',$5::text))
		`, "aud_"+uuid.NewString(), actorID, id, state.keyPrefix, state.status); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return r.ListGatewayKeysForOrg(ctx, orgID)
}

type gatewayKeyBulkState struct {
	status    string
	keyPrefix string
}

func gatewayKeyBulkStatesForOrg(ctx context.Context, q DBTX, ids []string, orgID string, requireLiveGateway bool) (map[string]gatewayKeyBulkState, error) {
	query := `
		select k.id,k.status,k.key_prefix
		from gateway_keys k
		where k.id=any($1) and k.org_id=$2 and k.deleted_at is null
		for update of k
	`
	if requireLiveGateway {
		query = `
			select k.id,k.status,k.key_prefix
			from gateway_keys k
			join gateways g on g.id=k.gateway_id
			where k.id=any($1)
				and k.org_id=$2
				and k.deleted_at is null
				and g.status <> 'deleted'
			for update of k
		`
	}
	rows, err := q.Query(ctx, query, ids, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	states := map[string]gatewayKeyBulkState{}
	for rows.Next() {
		var id string
		var state gatewayKeyBulkState
		if err := rows.Scan(&id, &state.status, &state.keyPrefix); err != nil {
			return nil, err
		}
		states[id] = state
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(states) != len(ids) {
		return nil, pgx.ErrNoRows
	}
	return states, nil
}

func (r *Repository) AuthenticateGatewayKey(ctx context.Context, plainKey string) (AuthenticatedGatewayKey, error) {
	hash := HashGatewayKey(plainKey)
	var key GatewayKey
	var gatewayID string
	err := r.db.QueryRow(ctx, `
		select k.id,k.org_id,k.gateway_id,g.name,k.name,k.key_prefix,k.key_mask,k.quota_month,k.qps_limit,k.requests_used,k.status,k.created_at,k.last_used_at,k.expires_at
		from gateway_keys k
		join gateways g on g.id=k.gateway_id
		join orgs o on o.id=k.org_id
		where k.key_hash=$1
			and k.status='active'
			and k.deleted_at is null
			and g.status='active'
			and o.status='active'
			and o.deleted_at is null
			and (k.expires_at is null or k.expires_at > now())
	`, hash).Scan(&key.ID, &key.OrgID, &gatewayID, &key.GatewayName, &key.Name, &key.KeyPrefix, &key.KeyMask, &key.QuotaMonth, &key.QPSLimit, &key.RequestsUsed, &key.Status, &key.CreatedAt, nullableTimePtr(&key.LastUsedAt), nullableTimePtr(&key.ExpiresAt))
	if err != nil {
		return AuthenticatedGatewayKey{}, err
	}
	key.GatewayID = gatewayID
	gateway, err := r.GatewayByIDForOrg(ctx, gatewayID, key.OrgID)
	if err != nil {
		return AuthenticatedGatewayKey{}, err
	}
	return AuthenticatedGatewayKey{Key: key, Gateway: gateway}, nil
}

func (r *Repository) ReserveGatewayKeyRequest(ctx context.Context, keyID string) (bool, error) {
	tag, err := r.db.Exec(ctx, `
		update gateway_keys
		set last_used_at=now(), requests_used=requests_used+1
		where id=$1 and status='active' and deleted_at is null and (quota_month <= 0 or requests_used < quota_month)
	`, keyID)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() == 1, nil
}

func (r *Repository) GatewayMembers(ctx context.Context) ([]GatewayMember, error) {
	return r.gatewayMembers(ctx, DefaultOrgID, false)
}

func (r *Repository) GatewayMembersForOrg(ctx context.Context, orgID string) ([]GatewayMember, error) {
	return r.gatewayMembers(ctx, orgID, true)
}

func (r *Repository) gatewayMembers(ctx context.Context, orgID string, membersOnly bool) ([]GatewayMember, error) {
	if orgID == "" {
		orgID = DefaultOrgID
	}
	where := ""
	if membersOnly {
		where = "where om.org_id=$1 and om.status='active'"
	}
	rows, err := r.db.Query(ctx, `
		select u.id,u.email,u.name,u.avatar,coalesce(om.role,u.role),coalesce(om.group_name,'默认组'),coalesce(om.status,u.status),u.last_active_at,
			coalesce((
				select count(*) from gateway_request_events e
				join gateway_keys k on k.id=e.gateway_key_id
				where k.created_by=u.id and k.org_id=$1 and e.created_at::date=current_date
			),0)
		from users u
		left join org_members om on om.user_id=u.id and om.org_id=$1 and om.status='active'
		`+where+`
		order by case when u.role in ('owner','admin') then 0 else 1 end, u.created_at desc
		limit 100
	`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []GatewayMember{}
	for rows.Next() {
		var item GatewayMember
		var last sql.NullTime
		if err := rows.Scan(&item.UserID, &item.Email, &item.Name, &item.Avatar, &item.Role, &item.GroupName, &item.Status, &last, &item.RequestsToday); err != nil {
			return nil, err
		}
		if last.Valid {
			item.LastActiveAt = &last.Time
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (r *Repository) PlanGatewayRoute(_ context.Context, gateway Gateway) []GatewayUpstream {
	candidates := make([]GatewayUpstream, 0, len(gateway.Upstreams))
	for _, upstream := range gateway.Upstreams {
		if !upstream.Enabled {
			continue
		}
		if !gatewayRuntimeEligibleStatus(upstream.Status) {
			continue
		}
		candidates = append(candidates, upstream)
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		a, b := candidates[i], candidates[j]
		switch gateway.Policy {
		case "success":
			if a.SuccessRate == b.SuccessRate {
				return a.Score > b.Score
			}
			return a.SuccessRate > b.SuccessRate
		case "cost":
			if a.CostUSD == b.CostUSD {
				return a.Score > b.Score
			}
			return a.CostUSD < b.CostUSD
		default:
			if a.LatencyP95Ms == b.LatencyP95Ms {
				return a.Score > b.Score
			}
			return a.LatencyP95Ms < b.LatencyP95Ms
		}
	})
	return candidates
}

func gatewayRuntimeEligibleStatus(status string) bool {
	switch strings.TrimSpace(status) {
	case "healthy", "degraded":
		return true
	default:
		return false
	}
}

func (r *Repository) RecordGatewayEvent(ctx context.Context, event GatewayRequestEvent) error {
	_, err := r.db.Exec(ctx, `
		insert into gateway_request_events(
			id,gateway_id,gateway_key_id,upstream_channel_id,request_path,model,status_code,
			request_tokens,response_tokens,cost_usd,latency_ms,error_type,stream,usage_estimated,created_at
		)
		values($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,nullif($12,''),$13,$14,now())
	`, "gwe_"+uuid.NewString(), event.GatewayID, nullableText(event.GatewayKeyID), nullableText(event.UpstreamChannelID), event.RequestPath, event.Model, event.StatusCode, event.RequestTokens, event.ResponseTokens, event.CostUSD, event.LatencyMs, event.ErrorType, event.Stream, event.UsageEstimated)
	return err
}

func (r *Repository) GatewayCostEstimate(ctx context.Context, channelID string, model string, promptTokens int, completionTokens int) (float64, bool, error) {
	channelID = strings.TrimSpace(channelID)
	model = strings.TrimSpace(model)
	if promptTokens <= 0 && completionTokens <= 0 {
		return 0, true, nil
	}
	var inputPerMTok float64
	var outputPerMTok float64
	err := r.db.QueryRow(ctx, `
		select p.input_per_mtok,p.output_per_mtok
		from model_prices p
		join model_catalog m on m.id=p.model_id
		where ($1 <> '' and p.channel_id=$1)
		   or (p.channel_id is null and m.model_key=$2)
		order by case when p.channel_id=$1 then 0 else 1 end, p.effective_at desc
		limit 1
	`, channelID, model).Scan(&inputPerMTok, &outputPerMTok)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	return CalculateModelCostUSD(promptTokens, completionTokens, inputPerMTok, outputPerMTok), true, nil
}

func CalculateModelCostUSD(promptTokens int, completionTokens int, inputPerMTok float64, outputPerMTok float64) float64 {
	if promptTokens < 0 {
		promptTokens = 0
	}
	if completionTokens < 0 {
		completionTokens = 0
	}
	return (float64(promptTokens)/1_000_000)*inputPerMTok + (float64(completionTokens)/1_000_000)*outputPerMTok
}

func (r *Repository) GatewayChannelCredential(ctx context.Context, orgID string, channelID string) (GatewayChannelCredential, error) {
	var cred GatewayChannelCredential
	err := r.db.QueryRow(ctx, `
		select cc.channel_id,cc.key_ciphertext,cc.key_nonce,cc.key_mask,cc.key_fingerprint
		from channel_credentials cc
		join channels c on c.id=cc.channel_id
		where cc.channel_id=$1
			and c.status not in ('disabled','deleted')
			and c.deleted_at is null
			and (
				(`+gatewayRuntimePlatformCredentialPredicate("c", "$2", "$3")+`)
				or (
					c.owner_type='user'
					and c.gateway_enabled is true
					and coalesce(c.org_id,'org_' || coalesce(c.owner_id,'')) = $2
				)
			)
	`, channelID, orgID, DefaultOrgID).Scan(&cred.ChannelID, &cred.Ciphertext, &cred.Nonce, &cred.Mask, &cred.Fingerprint)
	return cred, err
}

func (r *Repository) GatewayStats(ctx context.Context, gatewayID string) (GatewayStats, error) {
	var stats GatewayStats
	var cost float64
	var errors int
	err := r.db.QueryRow(ctx, `
		select count(*), coalesce(sum(request_tokens + response_tokens),0), coalesce(sum(cost_usd),0),
			coalesce(sum(case when status_code >= 400 then 1 else 0 end),0),
			coalesce(percentile_disc(0.95) within group (order by latency_ms),0)
		from gateway_request_events
		where gateway_id=$1 and created_at::date=current_date
	`, gatewayID).Scan(&stats.RequestsToday, &stats.TokensToday, &cost, &errors, &stats.P95LatencyMs)
	if err != nil {
		return GatewayStats{}, err
	}
	stats.CostToday = round3(cost)
	if stats.RequestsToday > 0 {
		stats.ErrorRate = round1(float64(errors) / float64(stats.RequestsToday) * 100)
		stats.SuccessRate = round1(100 - stats.ErrorRate)
	}
	if err := r.db.QueryRow(ctx, `select count(*) from gateway_keys where gateway_id=$1 and status='active' and deleted_at is null`, gatewayID).Scan(&stats.KeysActive); err != nil {
		return GatewayStats{}, err
	}
	return stats, nil
}

func (r *Repository) GatewayUsageSummary(ctx context.Context, days int) (GatewayUsageSummary, error) {
	return r.GatewayUsageSummaryForOrg(ctx, days, "")
}

func (r *Repository) GatewayUsageSummaryForOrg(ctx context.Context, days int, orgID string) (GatewayUsageSummary, error) {
	return r.GatewayUsageSummaryWithFilter(ctx, GatewayUsageFilter{Days: days, OrgID: orgID})
}

func (r *Repository) GatewayUsageSummaryWithFilter(ctx context.Context, filter GatewayUsageFilter) (GatewayUsageSummary, error) {
	filter = normalizeGatewayUsageFilter(filter)
	if !filter.SkipRollupRecompute {
		if err := r.RecomputeUsageDailyRollups(ctx, filter.OrgID); err != nil {
			return GatewayUsageSummary{}, err
		}
	}
	var out GatewayUsageSummary
	var cost float64
	var errors int
	err := r.db.QueryRow(ctx, `
		select count(*), coalesce(sum(request_tokens + response_tokens),0), coalesce(sum(cost_usd),0),
			coalesce(sum(case when status_code >= 400 then 1 else 0 end),0)
		from gateway_request_events e
		join gateways g on g.id=e.gateway_id
		left join gateway_keys k on k.id=e.gateway_key_id
		where `+gatewayUsageWindowPredicate("e")+`
			and ($2::text='' or g.org_id=$2)
			and ($3::text='' or e.gateway_id=$3)
			and ($4::text='' or coalesce(e.upstream_channel_id,'')=$4)
			and ($5::text='' or e.model=$5)
			and ($6::text='' or coalesce(k.created_by,'')=$6)
			and ($7::text='' or $7='gateway')
	`, filter.Days, filter.OrgID, filter.GatewayID, filter.ChannelID, filter.Model, filter.MemberUserID, filter.Source).Scan(&out.Totals.Requests, &out.Totals.Tokens, &cost, &errors)
	if err != nil {
		return out, err
	}
	out.Totals.CostUSD = round3(cost)
	if out.Totals.Requests > 0 {
		out.Totals.ErrorRate = round1(float64(errors) / float64(out.Totals.Requests) * 100)
	}
	quotaMonth, err := r.gatewayUsageQuotaMonth(ctx, filter)
	if err != nil {
		return out, err
	}
	out.QuotaMonth = quotaMonth
	var err2 error
	out.Trend, err2 = r.gatewayUsageTrend(ctx, filter)
	if err2 != nil {
		return out, err2
	}
	out.Gateways, err2 = r.gatewayUsageBreakdown(ctx, "gateway", filter)
	if err2 != nil {
		return out, err2
	}
	out.Models, err2 = r.gatewayUsageBreakdown(ctx, "model", filter)
	if err2 != nil {
		return out, err2
	}
	out.Channels, err2 = r.gatewayUsageBreakdown(ctx, "channel", filter)
	if err2 != nil {
		return out, err2
	}
	out.Rollups, err2 = r.UsageRollupsWithFilter(ctx, filter)
	if err2 != nil {
		return out, err2
	}
	out.Recent, err2 = r.gatewayUsageRecent(ctx, filter)
	return out, err2
}

func (r *Repository) gatewayUsageQuotaMonth(ctx context.Context, filter GatewayUsageFilter) (int, error) {
	filter = normalizeGatewayUsageFilter(filter)
	if filter.Source == "probe" || filter.ChannelID != "" || filter.Model != "" {
		return 0, nil
	}
	var quotaMonth int64
	err := r.db.QueryRow(ctx, `
		select coalesce(sum(k.quota_month),0)
		from gateway_keys k
		join gateways g on g.id=k.gateway_id
		where k.deleted_at is null
			and k.status='active'
			and (k.expires_at is null or k.expires_at > now())
			and g.status <> 'deleted'
			and ($1::text='' or g.org_id=$1)
			and ($2::text='' or k.gateway_id=$2)
			and ($3::text='' or coalesce(k.created_by,'')=$3)
	`, filter.OrgID, filter.GatewayID, filter.MemberUserID).Scan(&quotaMonth)
	if err != nil {
		return 0, err
	}
	return int(quotaMonth), nil
}

func normalizeGatewayUsageFilter(filter GatewayUsageFilter) GatewayUsageFilter {
	if filter.Days <= 0 || filter.Days > 90 {
		filter.Days = 30
	}
	filter.OrgID = strings.TrimSpace(filter.OrgID)
	filter.Source = strings.ToLower(strings.TrimSpace(filter.Source))
	if filter.Source != "gateway" && filter.Source != "probe" {
		filter.Source = ""
	}
	filter.GatewayID = strings.TrimSpace(filter.GatewayID)
	filter.ChannelID = strings.TrimSpace(filter.ChannelID)
	filter.Model = strings.TrimSpace(filter.Model)
	filter.MemberUserID = strings.TrimSpace(filter.MemberUserID)
	return filter
}

func gatewayUsageWindowPredicate(eventAlias string) string {
	if strings.TrimSpace(eventAlias) == "" {
		eventAlias = "e"
	}
	return eventAlias + ".created_at::date >= current_date - (($1::int - 1) * interval '1 day')"
}

func (r *Repository) gatewayUsageTrend(ctx context.Context, filter GatewayUsageFilter) ([]GatewayUsagePoint, error) {
	filter = normalizeGatewayUsageFilter(filter)
	rows, err := r.db.Query(ctx, `
		select d::date,
			coalesce(count(e.id),0),
			coalesce(sum(e.request_tokens + e.response_tokens),0),
			coalesce(sum(e.cost_usd),0)
		from generate_series(current_date - (($1::int - 1) * interval '1 day'), current_date, interval '1 day') d
		left join (
			select e.*
			from gateway_request_events e
			join gateways g on g.id=e.gateway_id
			left join gateway_keys k on k.id=e.gateway_key_id
			where `+gatewayUsageWindowPredicate("e")+`
				and ($2::text='' or g.org_id=$2)
				and ($3::text='' or e.gateway_id=$3)
				and ($4::text='' or coalesce(e.upstream_channel_id,'')=$4)
				and ($5::text='' or e.model=$5)
				and ($6::text='' or coalesce(k.created_by,'')=$6)
				and ($7::text='' or $7='gateway')
		) e on e.created_at::date=d::date
		group by d::date
		order by d::date asc
	`, filter.Days, filter.OrgID, filter.GatewayID, filter.ChannelID, filter.Model, filter.MemberUserID, filter.Source)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []GatewayUsagePoint{}
	for rows.Next() {
		var day time.Time
		var item GatewayUsagePoint
		var cost float64
		if err := rows.Scan(&day, &item.Requests, &item.Tokens, &cost); err != nil {
			return nil, err
		}
		item.Date = day.Format("2006-01-02")
		item.CostUSD = round3(cost)
		out = append(out, item)
	}
	return out, rows.Err()
}

func (r *Repository) gatewayUsageBreakdown(ctx context.Context, scope string, filter GatewayUsageFilter) ([]GatewayUsageBreakdown, error) {
	filter = normalizeGatewayUsageFilter(filter)
	var query string
	switch scope {
	case "model":
		query = `
			select model, model, count(*), coalesce(sum(request_tokens + response_tokens),0), coalesce(sum(cost_usd),0),
				coalesce(sum(case when status_code >= 400 then 1 else 0 end),0)
			from gateway_request_events e
			join gateways g on g.id=e.gateway_id
			left join gateway_keys k on k.id=e.gateway_key_id
			where ` + gatewayUsageWindowPredicate("e") + `
				and ($2::text='' or g.org_id=$2)
				and ($3::text='' or e.gateway_id=$3)
				and ($4::text='' or coalesce(e.upstream_channel_id,'')=$4)
				and ($5::text='' or e.model=$5)
				and ($6::text='' or coalesce(k.created_by,'')=$6)
				and ($7::text='' or $7='gateway')
			group by model
			order by count(*) desc
			limit 8`
	case "channel":
		query = `
			select coalesce(c.id,''), coalesce(c.name,'未路由'), count(e.id), coalesce(sum(e.request_tokens + e.response_tokens),0), coalesce(sum(e.cost_usd),0),
				coalesce(sum(case when e.status_code >= 400 then 1 else 0 end),0)
			from gateway_request_events e
			join gateways g on g.id=e.gateway_id
			left join gateway_keys k on k.id=e.gateway_key_id
			left join channels c on c.id=e.upstream_channel_id
			where ` + gatewayUsageWindowPredicate("e") + `
				and ($2::text='' or g.org_id=$2)
				and ($3::text='' or e.gateway_id=$3)
				and ($4::text='' or coalesce(e.upstream_channel_id,'')=$4)
				and ($5::text='' or e.model=$5)
				and ($6::text='' or coalesce(k.created_by,'')=$6)
				and ($7::text='' or $7='gateway')
			group by c.id,c.name
			order by count(e.id) desc
			limit 8`
	default:
		query = `
			select g.id,g.name,count(e.id),coalesce(sum(e.request_tokens + e.response_tokens),0),coalesce(sum(e.cost_usd),0),
				coalesce(sum(case when e.status_code >= 400 then 1 else 0 end),0)
			from gateway_request_events e
			join gateways g on g.id=e.gateway_id
			left join gateway_keys k on k.id=e.gateway_key_id
			where ` + gatewayUsageWindowPredicate("e") + `
				and ($2::text='' or g.org_id=$2)
				and ($3::text='' or e.gateway_id=$3)
				and ($4::text='' or coalesce(e.upstream_channel_id,'')=$4)
				and ($5::text='' or e.model=$5)
				and ($6::text='' or coalesce(k.created_by,'')=$6)
				and ($7::text='' or $7='gateway')
			group by g.id,g.name
			order by count(e.id) desc
			limit 8`
	}
	rows, err := r.db.Query(ctx, query, filter.Days, filter.OrgID, filter.GatewayID, filter.ChannelID, filter.Model, filter.MemberUserID, filter.Source)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []GatewayUsageBreakdown{}
	for rows.Next() {
		var item GatewayUsageBreakdown
		var cost float64
		var errors int
		if err := rows.Scan(&item.ID, &item.Name, &item.Requests, &item.Tokens, &cost, &errors); err != nil {
			return nil, err
		}
		item.CostUSD = round3(cost)
		if item.Requests > 0 {
			item.ErrorRate = round1(float64(errors) / float64(item.Requests) * 100)
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (r *Repository) gatewayUsageRecent(ctx context.Context, filter GatewayUsageFilter) ([]GatewayUsageEvent, error) {
	filter = normalizeGatewayUsageFilter(filter)
	rows, err := r.db.Query(ctx, `
		select e.id,g.name,coalesce(k.name,'-'),coalesce(c.name,'-'),e.model,e.status_code,
			e.request_tokens + e.response_tokens,e.cost_usd,e.latency_ms,e.stream,e.created_at,coalesce(e.error_type,''),e.usage_estimated
		from gateway_request_events e
		join gateways g on g.id=e.gateway_id
		left join gateway_keys k on k.id=e.gateway_key_id
		left join channels c on c.id=e.upstream_channel_id
		where `+gatewayUsageWindowPredicate("e")+`
			and ($2::text='' or g.org_id=$2)
			and ($3::text='' or e.gateway_id=$3)
			and ($4::text='' or coalesce(e.upstream_channel_id,'')=$4)
			and ($5::text='' or e.model=$5)
			and ($6::text='' or coalesce(k.created_by,'')=$6)
			and ($7::text='' or $7='gateway')
		order by e.created_at desc
		limit 50
	`, filter.Days, filter.OrgID, filter.GatewayID, filter.ChannelID, filter.Model, filter.MemberUserID, filter.Source)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []GatewayUsageEvent{}
	for rows.Next() {
		var item GatewayUsageEvent
		var cost float64
		if err := rows.Scan(&item.ID, &item.Gateway, &item.KeyName, &item.Channel, &item.Model, &item.StatusCode, &item.Tokens, &cost, &item.LatencyMs, &item.Stream, &item.CreatedAt, &item.ErrorType, &item.Estimated); err != nil {
			return nil, err
		}
		item.CostUSD = round6(cost)
		out = append(out, item)
	}
	return out, rows.Err()
}

func NewGatewayPlainKey() (string, error) {
	raw := make([]byte, 24)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return "sk-th-" + base64.RawURLEncoding.EncodeToString(raw), nil
}

func HashGatewayKey(key string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(key)))
	return hex.EncodeToString(sum[:])
}

func GatewayKeyPrefix(key string) string {
	if len(key) <= 12 {
		return key
	}
	return key[:12]
}

func MaskGatewayKey(key string) string {
	if len(key) <= 16 {
		return key[:4] + "****"
	}
	return key[:12] + "••••" + key[len(key)-4:]
}

func UniqueSlug(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	re := regexp.MustCompile(`[^a-z0-9]+`)
	value = strings.Trim(re.ReplaceAllString(value, "-"), "-")
	if value == "" {
		value = "gateway"
	}
	return fmt.Sprintf("%s-%s", value, uuid.NewString()[:8])
}

func normalizeGatewayPolicy(policy string) string {
	switch strings.TrimSpace(policy) {
	case "success", "cost":
		return policy
	default:
		return "latency"
	}
}

func validateGatewayStatus(status string) (string, error) {
	status = strings.ToLower(strings.TrimSpace(status))
	switch status {
	case "active", "paused", "deleted":
		return status, nil
	default:
		return "", fmt.Errorf("invalid gateway status")
	}
}

func validateGatewayKeyStatus(status string) (string, error) {
	status = strings.ToLower(strings.TrimSpace(status))
	switch status {
	case "active", "revoked", "expired":
		return status, nil
	default:
		return "", fmt.Errorf("invalid gateway key status")
	}
}

func validateGatewayKeyEditableStatus(status string) (string, error) {
	status = strings.ToLower(strings.TrimSpace(status))
	switch status {
	case "active", "expired":
		return status, nil
	default:
		return "", fmt.Errorf("invalid gateway key status")
	}
}

func gatewayPolicyLabel(policy string) string {
	switch policy {
	case "success":
		return "最高成功率"
	case "cost":
		return "成本优先"
	default:
		return "最低延迟优先"
	}
}

func uniqueStrings(values []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}

func nullableText(value string) any {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return value
}

func nullableTime(value sql.NullTime) any {
	if !value.Valid {
		return nil
	}
	return value.Time
}

func nullableTimePtr(target **time.Time) any {
	return sql.Scanner(sqlNullTimeScanner{target: target})
}

type sqlNullTimeScanner struct {
	target **time.Time
}

func (s sqlNullTimeScanner) Scan(src any) error {
	var nt sql.NullTime
	if err := nt.Scan(src); err != nil {
		return err
	}
	if nt.Valid {
		value := nt.Time
		*s.target = &value
	}
	return nil
}
