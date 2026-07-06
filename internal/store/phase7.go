package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/mail"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

var (
	ErrInvalidIncidentInput = errors.New("invalid incident input")
	ErrOpenIncidentExists   = errors.New("channel already has an open incident")
)

type UsageRollupItem struct {
	Day          string  `json:"day"`
	OrgID        string  `json:"orgId"`
	Source       string  `json:"source"`
	GatewayID    string  `json:"gatewayId"`
	ChannelID    string  `json:"channelId"`
	Model        string  `json:"model"`
	MemberUserID string  `json:"memberUserId"`
	Requests     int     `json:"requests"`
	Tokens       int     `json:"tokens"`
	CostUSD      float64 `json:"costUsd"`
	Errors       int     `json:"errors"`
	ProbeRuns    int     `json:"probeRuns"`
}

type AlertRule struct {
	ID            string         `json:"id"`
	OrgID         string         `json:"orgId"`
	Scope         string         `json:"scope"`
	Name          string         `json:"name"`
	Kind          string         `json:"kind"`
	Severity      string         `json:"severity"`
	Threshold     float64        `json:"threshold"`
	WindowMinutes int            `json:"windowMinutes"`
	DedupeMinutes int            `json:"dedupeMinutes"`
	Enabled       bool           `json:"enabled"`
	Config        map[string]any `json:"config"`
	CreatedAt     time.Time      `json:"createdAt"`
	UpdatedAt     time.Time      `json:"updatedAt"`
}

type NotificationChannel struct {
	ID                string    `json:"id"`
	OrgID             string    `json:"orgId"`
	Scope             string    `json:"scope"`
	Name              string    `json:"name"`
	Type              string    `json:"type"`
	Target            string    `json:"-"`
	TargetCiphertext  string    `json:"-"`
	TargetNonce       string    `json:"-"`
	TargetFingerprint string    `json:"-"`
	TargetMask        string    `json:"targetMask"`
	Enabled           bool      `json:"enabled"`
	CreatedAt         time.Time `json:"createdAt"`
	UpdatedAt         time.Time `json:"updatedAt"`
}

type AlertDelivery struct {
	ID                    string         `json:"id"`
	OrgID                 string         `json:"orgId"`
	Scope                 string         `json:"scope"`
	RuleID                string         `json:"ruleId"`
	RuleName              string         `json:"ruleName"`
	NotificationChannelID string         `json:"notificationChannelId"`
	ChannelName           string         `json:"channelName"`
	IncidentID            string         `json:"incidentId"`
	DedupeKey             string         `json:"dedupeKey"`
	Severity              string         `json:"severity"`
	Status                string         `json:"status"`
	Title                 string         `json:"title"`
	Message               string         `json:"message"`
	Error                 string         `json:"error"`
	Metadata              map[string]any `json:"metadata"`
	CreatedAt             time.Time      `json:"createdAt"`
	SentAt                *time.Time     `json:"sentAt,omitempty"`
}

type AlertCenter struct {
	Summary    AlertSummary          `json:"summary"`
	Rules      []AlertRule           `json:"rules"`
	Channels   []NotificationChannel `json:"channels"`
	Deliveries []AlertDelivery       `json:"deliveries"`
	Incidents  []IncidentItem        `json:"incidents"`
}

type AlertSummary struct {
	EnabledRules    int `json:"enabledRules"`
	OpenIncidents   int `json:"openIncidents"`
	SentToday       int `json:"sentToday"`
	SuppressedToday int `json:"suppressedToday"`
	FailedToday     int `json:"failedToday"`
	RecoveredToday  int `json:"recoveredToday"`
}

type AlertRuleInput struct {
	Name          string  `json:"name"`
	Kind          string  `json:"kind"`
	Severity      string  `json:"severity"`
	Threshold     float64 `json:"threshold"`
	WindowMinutes int     `json:"windowMinutes"`
	DedupeMinutes int     `json:"dedupeMinutes"`
	Enabled       bool    `json:"enabled"`
}

type NotificationChannelInput struct {
	Name              string `json:"name"`
	Type              string `json:"type"`
	Target            string `json:"target"`
	TargetCiphertext  string `json:"-"`
	TargetNonce       string `json:"-"`
	TargetMask        string `json:"-"`
	TargetFingerprint string `json:"-"`
	Enabled           bool   `json:"enabled"`
}

type NotificationChannelLegacyTarget struct {
	ID     string
	Type   string
	Target string
}

type AuditLogItem struct {
	ID         string         `json:"id"`
	ActorType  string         `json:"actorType"`
	ActorID    string         `json:"actorId"`
	ActorEmail string         `json:"actorEmail"`
	Action     string         `json:"action"`
	ObjectType string         `json:"objectType"`
	ObjectID   string         `json:"objectId"`
	IP         string         `json:"ip"`
	Result     string         `json:"result"`
	Metadata   map[string]any `json:"metadata"`
	CreatedAt  time.Time      `json:"createdAt"`
}

type AuditQuery struct {
	OrgID      string
	ID         string
	EventClass string
	Actor      string
	Action     string
	ObjectType string
	ObjectID   string
	Result     string
	Query      string
	From       time.Time
	To         time.Time
	Limit      int
}

type AuditLogResult struct {
	Items []AuditLogItem `json:"items"`
	Total int            `json:"total"`
}

type IncidentItem struct {
	ID         string          `json:"id"`
	OrgID      string          `json:"orgId"`
	ChannelID  string          `json:"channelId"`
	Channel    string          `json:"channel"`
	Status     string          `json:"status"`
	Title      string          `json:"title"`
	Open       bool            `json:"open"`
	OpenedAt   time.Time       `json:"openedAt"`
	ResolvedAt *time.Time      `json:"resolvedAt,omitempty"`
	Metadata   map[string]any  `json:"metadata"`
	Events     []IncidentEvent `json:"events,omitempty"`
}

type IncidentEvent struct {
	ID        string         `json:"id"`
	Type      string         `json:"type"`
	Message   string         `json:"message"`
	Metadata  map[string]any `json:"metadata"`
	CreatedAt time.Time      `json:"createdAt"`
}

type IncidentQuery struct {
	OrgID  string
	Status string
	Query  string
	Limit  int
}

type IncidentInput struct {
	ChannelID string         `json:"channelId"`
	Status    string         `json:"status"`
	Title     string         `json:"title"`
	Message   string         `json:"message"`
	Metadata  map[string]any `json:"metadata"`
}

type GovernanceSummary struct {
	OpenIncidents int             `json:"openIncidents"`
	AlertsToday   int             `json:"alertsToday"`
	AuditToday    int             `json:"auditToday"`
	CostTodayUSD  float64         `json:"costTodayUsd"`
	RecentAlerts  []AlertDelivery `json:"recentAlerts"`
	RecentAudit   []AuditLogItem  `json:"recentAudit"`
	Incidents     []IncidentItem  `json:"incidents"`
}

type MetricsSnapshot struct {
	GatewayRequests int
	GatewayErrors   int
	ProbeRuns       int
	OpenIncidents   int
	AlertDeliveries int
	AuditEvents     int
	UsageRollups    int
}

func (r *Repository) RecomputeUsageDailyRollups(ctx context.Context, orgID string) error {
	orgID = strings.TrimSpace(orgID)
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, `
		delete from usage_daily_rollups
		where day >= current_date - interval '90 days'
			and ($1::text='' or org_id=$1)
	`, orgID); err != nil {
		return err
	}

	if _, err := tx.Exec(ctx, `
		insert into usage_daily_rollups(day,org_id,source,gateway_id,channel_id,model,member_user_id,requests,tokens,cost_usd,errors,probe_runs)
		select
			e.created_at::date,
			g.org_id,
			'gateway',
			g.id,
			coalesce(e.upstream_channel_id,''),
			coalesce(nullif(e.model,''),'-'),
			coalesce(k.created_by,''),
			count(*)::int,
			coalesce(sum(e.request_tokens + e.response_tokens),0)::int,
			coalesce(sum(e.cost_usd),0),
			coalesce(sum(case when e.status_code >= 400 then 1 else 0 end),0)::int,
			0
		from gateway_request_events e
		join gateways g on g.id=e.gateway_id
		left join gateway_keys k on k.id=e.gateway_key_id
		where e.created_at >= current_date - interval '90 days'
			and ($1::text='' or g.org_id=$1)
		group by e.created_at::date,g.org_id,g.id,coalesce(e.upstream_channel_id,''),coalesce(nullif(e.model,''),'-'),coalesce(k.created_by,'')
		on conflict(day,org_id,source,gateway_id,channel_id,model,member_user_id) do update set
			requests=excluded.requests,
			tokens=excluded.tokens,
			cost_usd=excluded.cost_usd,
			errors=excluded.errors,
			probe_runs=excluded.probe_runs,
			updated_at=now()
	`, orgID); err != nil {
		return err
	}

	if _, err := tx.Exec(ctx, `
		insert into usage_daily_rollups(day,org_id,source,gateway_id,channel_id,model,member_user_id,requests,tokens,cost_usd,errors,probe_runs)
		select
			pr.started_at::date,
				case when c.owner_type='user' then coalesce(c.org_id,'org_' || coalesce(c.owner_id,'')) else $2::text end,
			'probe',
			'',
			c.id,
			c.model,
			coalesce(c.owner_id,''),
			0,
			0,
			0,
			coalesce(sum(case when pr.status='failed' then 1 else 0 end),0)::int,
			count(*)::int
		from probe_runs pr
		join channels c on c.id=pr.channel_id
		where pr.started_at >= current_date - interval '90 days'
			and (
				$1::text=''
					or (c.owner_type='user' and coalesce(c.org_id,'org_' || coalesce(c.owner_id,'')) = $1)
				or (c.owner_type='platform' and $1=$2)
			)
			group by pr.started_at::date,c.owner_type,c.owner_id,c.org_id,c.id,c.model
		on conflict(day,org_id,source,gateway_id,channel_id,model,member_user_id) do update set
			errors=excluded.errors,
			probe_runs=excluded.probe_runs,
			updated_at=now()
	`, orgID, DefaultOrgID); err != nil {
		return err
	}

	return tx.Commit(ctx)
}

func (r *Repository) UsageRollups(ctx context.Context, days int, orgID string) ([]UsageRollupItem, error) {
	return r.UsageRollupsWithFilter(ctx, GatewayUsageFilter{Days: days, OrgID: orgID})
}

func (r *Repository) UsageRollupsWithFilter(ctx context.Context, filter GatewayUsageFilter) ([]UsageRollupItem, error) {
	filter = normalizeGatewayUsageFilter(filter)
	rows, err := r.db.Query(ctx, `
		select day,org_id,source,gateway_id,channel_id,model,member_user_id,requests,tokens,cost_usd,errors,probe_runs
		from usage_daily_rollups
		where day >= current_date - (($1::int - 1) * interval '1 day')
			and ($2::text='' or org_id=$2)
			and ($3::text='' or gateway_id=$3)
			and ($4::text='' or channel_id=$4)
			and ($5::text='' or model=$5)
			and ($6::text='' or member_user_id=$6)
			and ($7::text='' or source=$7)
		order by day desc, requests desc, probe_runs desc
		limit 200
	`, filter.Days, filter.OrgID, filter.GatewayID, filter.ChannelID, filter.Model, filter.MemberUserID, filter.Source)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []UsageRollupItem{}
	for rows.Next() {
		var item UsageRollupItem
		var day time.Time
		var cost float64
		if err := rows.Scan(&day, &item.OrgID, &item.Source, &item.GatewayID, &item.ChannelID, &item.Model, &item.MemberUserID, &item.Requests, &item.Tokens, &cost, &item.Errors, &item.ProbeRuns); err != nil {
			return nil, err
		}
		item.Day = day.Format("2006-01-02")
		item.CostUSD = round3(cost)
		out = append(out, item)
	}
	return out, rows.Err()
}

func (r *Repository) EnsureDefaultAlertConfig(ctx context.Context, scope string, orgID string, actorID string, email string) error {
	scope = normalizeAlertScope(scope)
	orgID = normalizeAlertOrg(scope, orgID)
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	tag, err := tx.Exec(ctx, `
		insert into alert_config_states(scope,org_id,defaults_initialized_at,updated_by)
		values($1,$2,now(),$3)
		on conflict(scope,org_id) do nothing
	`, scope, orgID, nullableText(actorID))
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return tx.Commit(ctx)
	}

	prefix := stableAlertPrefix(scope, orgID)
	defaults := []AlertRuleInput{
		{Name: "L3 连续失败超过 2 次", Kind: "l3_consecutive_failures", Severity: "critical", Threshold: 2, WindowMinutes: 60, DedupeMinutes: 30, Enabled: true},
		{Name: "成本超过预算阈值", Kind: "cost_threshold", Severity: "warning", Threshold: 1, WindowMinutes: 1440, DedupeMinutes: 120, Enabled: true},
		{Name: "网关错误率超过 20%", Kind: "gateway_error_rate", Severity: "critical", Threshold: 20, WindowMinutes: 60, DedupeMinutes: 30, Enabled: true},
		{Name: "Key 配额使用超过 90%", Kind: "quota_anomaly", Severity: "warning", Threshold: 90, WindowMinutes: 1440, DedupeMinutes: 120, Enabled: true},
	}
	for _, item := range defaults {
		if _, err := tx.Exec(ctx, `
			insert into alert_rules(id,org_id,scope,name,kind,severity,threshold,window_minutes,dedupe_minutes,enabled,config,created_by)
			values($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,'{}'::jsonb,$11)
			on conflict(id) do nothing
		`, prefix+"_"+item.Kind, orgID, scope, item.Name, item.Kind, item.Severity, item.Threshold, item.WindowMinutes, item.DedupeMinutes, item.Enabled, nullableText(actorID)); err != nil {
			return err
		}
	}
	target := strings.TrimSpace(email)
	if target == "" {
		target = "admin@tokhub.local"
	}
	if _, err := tx.Exec(ctx, `
		insert into notification_channels(id,org_id,scope,name,type,target,enabled,created_by)
		values($1,$2,$3,$4,'email',$5,true,$6)
		on conflict(id) do nothing
	`, prefix+"_email", orgID, scope, "默认邮件通知", target, nullableText(actorID)); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (r *Repository) AlertCenter(ctx context.Context, scope string, orgID string, actor PublicUser) (AlertCenter, error) {
	scope = normalizeAlertScope(scope)
	orgID = normalizeAlertOrg(scope, orgID)
	if err := r.EnsureDefaultAlertConfig(ctx, scope, orgID, actor.ID, actor.Email); err != nil {
		return AlertCenter{}, err
	}
	rules, err := r.AlertRules(ctx, scope, orgID)
	if err != nil {
		return AlertCenter{}, err
	}
	channels, err := r.NotificationChannels(ctx, scope, orgID)
	if err != nil {
		return AlertCenter{}, err
	}
	deliveries, err := r.AlertDeliveries(ctx, scope, orgID, 50)
	if err != nil {
		return AlertCenter{}, err
	}
	incidents, err := r.Incidents(ctx, orgID, 20)
	if err != nil {
		return AlertCenter{}, err
	}
	summary, err := r.AlertSummary(ctx, scope, orgID)
	if err != nil {
		return AlertCenter{}, err
	}
	return AlertCenter{Summary: summary, Rules: rules, Channels: channels, Deliveries: deliveries, Incidents: incidents}, nil
}

func (r *Repository) AlertRules(ctx context.Context, scope string, orgID string) ([]AlertRule, error) {
	rows, err := r.db.Query(ctx, `
		select id,org_id,scope,name,kind,severity,threshold,window_minutes,dedupe_minutes,enabled,config,created_at,updated_at
		from alert_rules
		where scope=$1 and org_id=$2
		order by enabled desc, created_at asc
	`, normalizeAlertScope(scope), normalizeAlertOrg(scope, orgID))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []AlertRule{}
	for rows.Next() {
		var item AlertRule
		var raw []byte
		if err := rows.Scan(&item.ID, &item.OrgID, &item.Scope, &item.Name, &item.Kind, &item.Severity, &item.Threshold, &item.WindowMinutes, &item.DedupeMinutes, &item.Enabled, &raw, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		item.Config = decodeMap(raw)
		out = append(out, item)
	}
	return out, rows.Err()
}

func (r *Repository) UpsertAlertRule(ctx context.Context, scope string, orgID string, actorID string, input AlertRuleInput) (AlertRule, error) {
	scope = normalizeAlertScope(scope)
	orgID = normalizeAlertOrg(scope, orgID)
	kind := normalizeAlertKind(input.Kind)
	severity := normalizeAlertSeverity(input.Severity)
	if input.WindowMinutes <= 0 {
		input.WindowMinutes = 60
	}
	if input.DedupeMinutes <= 0 {
		input.DedupeMinutes = 30
	}
	name := strings.TrimSpace(input.Name)
	if name == "" {
		name = alertKindLabel(kind)
	}
	id := "alr_" + uuid.NewString()
	var item AlertRule
	var raw []byte
	err := r.db.QueryRow(ctx, `
		insert into alert_rules(id,org_id,scope,name,kind,severity,threshold,window_minutes,dedupe_minutes,enabled,config,created_by)
		values($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,'{}'::jsonb,$11)
		returning id,org_id,scope,name,kind,severity,threshold,window_minutes,dedupe_minutes,enabled,config,created_at,updated_at
	`, id, orgID, scope, name, kind, severity, input.Threshold, input.WindowMinutes, input.DedupeMinutes, input.Enabled, nullableText(actorID)).
		Scan(&item.ID, &item.OrgID, &item.Scope, &item.Name, &item.Kind, &item.Severity, &item.Threshold, &item.WindowMinutes, &item.DedupeMinutes, &item.Enabled, &raw, &item.CreatedAt, &item.UpdatedAt)
	if err != nil {
		return AlertRule{}, err
	}
	item.Config = decodeMap(raw)
	_ = r.WriteAudit(ctx, AuditEvent{ActorType: "user", ActorID: actorID, Action: "alert.rule.created", ObjectType: "alert_rule", ObjectID: item.ID, Result: "success", Metadata: map[string]any{"scope": scope, "org_id": orgID, "kind": kind}})
	return item, nil
}

func (r *Repository) PatchAlertRule(ctx context.Context, scope string, orgID string, ruleID string, actorID string, input AlertRuleInput) (AlertRule, error) {
	scope = normalizeAlertScope(scope)
	orgID = normalizeAlertOrg(scope, orgID)
	kind := normalizeAlertKind(input.Kind)
	severity := normalizeAlertSeverity(input.Severity)
	if input.WindowMinutes <= 0 {
		input.WindowMinutes = 60
	}
	if input.DedupeMinutes <= 0 {
		input.DedupeMinutes = 30
	}
	name := strings.TrimSpace(input.Name)
	if name == "" {
		name = alertKindLabel(kind)
	}
	var item AlertRule
	var raw []byte
	err := r.db.QueryRow(ctx, `
		update alert_rules
		set name=$4,kind=$5,severity=$6,threshold=$7,window_minutes=$8,dedupe_minutes=$9,enabled=$10,updated_at=now()
		where id=$1 and scope=$2 and org_id=$3
		returning id,org_id,scope,name,kind,severity,threshold,window_minutes,dedupe_minutes,enabled,config,created_at,updated_at
	`, ruleID, scope, orgID, name, kind, severity, input.Threshold, input.WindowMinutes, input.DedupeMinutes, input.Enabled).
		Scan(&item.ID, &item.OrgID, &item.Scope, &item.Name, &item.Kind, &item.Severity, &item.Threshold, &item.WindowMinutes, &item.DedupeMinutes, &item.Enabled, &raw, &item.CreatedAt, &item.UpdatedAt)
	if err != nil {
		return AlertRule{}, err
	}
	item.Config = decodeMap(raw)
	_ = r.WriteAudit(ctx, AuditEvent{ActorType: "user", ActorID: actorID, Action: "alert.rule.updated", ObjectType: "alert_rule", ObjectID: item.ID, Result: "success", Metadata: map[string]any{"scope": scope, "org_id": orgID, "kind": kind, "enabled": item.Enabled}})
	return item, nil
}

func (r *Repository) DeleteAlertRule(ctx context.Context, scope string, orgID string, ruleID string, actorID string) error {
	scope = normalizeAlertScope(scope)
	orgID = normalizeAlertOrg(scope, orgID)
	tag, err := r.db.Exec(ctx, `delete from alert_rules where id=$1 and scope=$2 and org_id=$3`, ruleID, scope, orgID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	_ = r.WriteAudit(ctx, AuditEvent{ActorType: "user", ActorID: actorID, Action: "alert.rule.deleted", ObjectType: "alert_rule", ObjectID: ruleID, Result: "success", Metadata: map[string]any{"scope": scope, "org_id": orgID}})
	return nil
}

func (r *Repository) BulkUpdateAlertRules(ctx context.Context, scope string, orgID string, ids []string, actorID string, enabled bool) ([]AlertRule, error) {
	scope = normalizeAlertScope(scope)
	orgID = normalizeAlertOrg(scope, orgID)
	ids = uniqueIDs(ids)
	if len(ids) == 0 {
		return nil, fmt.Errorf("select at least one alert rule")
	}
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	tag, err := tx.Exec(ctx, `
		update alert_rules
		set enabled=$4, updated_at=now()
		where id=any($1) and scope=$2 and org_id=$3
	`, ids, scope, orgID, enabled)
	if err != nil {
		return nil, err
	}
	if int(tag.RowsAffected()) != len(ids) {
		return nil, pgx.ErrNoRows
	}
	for _, id := range ids {
		if err := writeAuditTx(ctx, tx, AuditEvent{
			ActorType: "user", ActorID: actorID, Action: "alert.rule.bulk_enabled",
			ObjectType: "alert_rule", ObjectID: id, Result: "success",
			Metadata: map[string]any{"scope": scope, "org_id": orgID, "enabled": enabled},
		}); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return r.AlertRules(ctx, scope, orgID)
}

func (r *Repository) BulkDeleteAlertRules(ctx context.Context, scope string, orgID string, ids []string, actorID string) ([]AlertRule, error) {
	scope = normalizeAlertScope(scope)
	orgID = normalizeAlertOrg(scope, orgID)
	ids = uniqueIDs(ids)
	if len(ids) == 0 {
		return nil, fmt.Errorf("select at least one alert rule")
	}
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	tag, err := tx.Exec(ctx, `delete from alert_rules where id=any($1) and scope=$2 and org_id=$3`, ids, scope, orgID)
	if err != nil {
		return nil, err
	}
	if int(tag.RowsAffected()) != len(ids) {
		return nil, pgx.ErrNoRows
	}
	for _, id := range ids {
		if err := writeAuditTx(ctx, tx, AuditEvent{
			ActorType: "user", ActorID: actorID, Action: "alert.rule.bulk_deleted",
			ObjectType: "alert_rule", ObjectID: id, Result: "success",
			Metadata: map[string]any{"scope": scope, "org_id": orgID},
		}); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return r.AlertRules(ctx, scope, orgID)
}

func (r *Repository) NotificationChannels(ctx context.Context, scope string, orgID string) ([]NotificationChannel, error) {
	rows, err := r.db.Query(ctx, `
		select id,org_id,scope,name,type,target,
			coalesce(target_ciphertext,''),coalesce(target_nonce,''),coalesce(target_fingerprint,''),coalesce(target_mask,''),
			enabled,created_at,updated_at
		from notification_channels
		where scope=$1 and org_id=$2
		order by enabled desc, created_at asc
	`, normalizeAlertScope(scope), normalizeAlertOrg(scope, orgID))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []NotificationChannel{}
	for rows.Next() {
		var item NotificationChannel
		if err := rows.Scan(&item.ID, &item.OrgID, &item.Scope, &item.Name, &item.Type, &item.Target, &item.TargetCiphertext, &item.TargetNonce, &item.TargetFingerprint, &item.TargetMask, &item.Enabled, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		item.TargetMask = notificationChannelMask(item)
		out = append(out, item)
	}
	return out, rows.Err()
}

func (r *Repository) LegacyNotificationChannelTargets(ctx context.Context) ([]NotificationChannelLegacyTarget, error) {
	rows, err := r.db.Query(ctx, `
		select id,type,target
		from notification_channels
		where trim(coalesce(target,'')) <> ''
			and trim(coalesce(target_ciphertext,'')) = ''
		order by created_at asc
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []NotificationChannelLegacyTarget{}
	for rows.Next() {
		var item NotificationChannelLegacyTarget
		if err := rows.Scan(&item.ID, &item.Type, &item.Target); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (r *Repository) StoreNotificationChannelEncryptedTarget(ctx context.Context, channelID string, secret EncryptedCredential) error {
	tag, err := r.db.Exec(ctx, `
		update notification_channels
		set target='',
			target_ciphertext=$2,
			target_nonce=$3,
			target_mask=$4,
			target_fingerprint=$5,
			updated_at=now()
		where id=$1
			and trim(coalesce(target,'')) <> ''
			and trim(coalesce(target_ciphertext,'')) = ''
	`, channelID, secret.Ciphertext, secret.Nonce, secret.Mask, secret.Fingerprint)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	return nil
}

func (r *Repository) UpsertNotificationChannel(ctx context.Context, scope string, orgID string, actorID string, input NotificationChannelInput) (NotificationChannel, error) {
	scope = normalizeAlertScope(scope)
	orgID = normalizeAlertOrg(scope, orgID)
	name := strings.TrimSpace(input.Name)
	if name == "" {
		name = "通知渠道"
	}
	channelType := strings.TrimSpace(input.Type)
	if channelType != "webhook" && channelType != "feishu" {
		channelType = "email"
	}
	target := strings.TrimSpace(input.Target)
	if target == "" {
		target = "admin@tokhub.local"
	}
	if err := validateNotificationTarget(channelType, target); err != nil {
		return NotificationChannel{}, err
	}
	targetCiphertext := strings.TrimSpace(input.TargetCiphertext)
	targetNonce := strings.TrimSpace(input.TargetNonce)
	targetMask := strings.TrimSpace(input.TargetMask)
	if targetMask == "" {
		targetMask = MaskNotificationTarget(channelType, target)
	}
	targetFingerprint := strings.TrimSpace(input.TargetFingerprint)
	if targetFingerprint == "" {
		targetFingerprint = HashOpaqueToken(target)[:16]
	}
	targetForStorage := target
	if targetCiphertext != "" && targetNonce != "" {
		targetForStorage = ""
	}
	var item NotificationChannel
	err := r.db.QueryRow(ctx, `
		insert into notification_channels(id,org_id,scope,name,type,target,target_ciphertext,target_nonce,target_mask,target_fingerprint,enabled,created_by)
		values($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)
		returning id,org_id,scope,name,type,target,
			coalesce(target_ciphertext,''),coalesce(target_nonce,''),coalesce(target_fingerprint,''),coalesce(target_mask,''),
			enabled,created_at,updated_at
	`, "ntc_"+uuid.NewString(), orgID, scope, name, channelType, targetForStorage, targetCiphertext, targetNonce, targetMask, targetFingerprint, input.Enabled, nullableText(actorID)).
		Scan(&item.ID, &item.OrgID, &item.Scope, &item.Name, &item.Type, &item.Target, &item.TargetCiphertext, &item.TargetNonce, &item.TargetFingerprint, &item.TargetMask, &item.Enabled, &item.CreatedAt, &item.UpdatedAt)
	if err == nil {
		item.TargetMask = notificationChannelMask(item)
		_ = r.WriteAudit(ctx, AuditEvent{ActorType: "user", ActorID: actorID, Action: "notification.channel.created", ObjectType: "notification_channel", ObjectID: item.ID, Result: "success", Metadata: map[string]any{"scope": scope, "org_id": orgID, "type": channelType}})
	}
	return item, err
}

func (r *Repository) PatchNotificationChannel(ctx context.Context, scope string, orgID string, channelID string, actorID string, input NotificationChannelInput) (NotificationChannel, error) {
	scope = normalizeAlertScope(scope)
	orgID = normalizeAlertOrg(scope, orgID)
	name := strings.TrimSpace(input.Name)
	if name == "" {
		name = "通知渠道"
	}
	channelType := strings.TrimSpace(input.Type)
	if channelType != "webhook" && channelType != "feishu" {
		channelType = "email"
	}
	target := strings.TrimSpace(input.Target)
	var currentType, currentTarget, currentCiphertext, currentNonce, currentMask, currentFingerprint string
	if err := r.db.QueryRow(ctx, `
		select type,target,coalesce(target_ciphertext,''),coalesce(target_nonce,''),coalesce(target_mask,''),coalesce(target_fingerprint,'')
		from notification_channels
		where id=$1 and scope=$2 and org_id=$3
	`, channelID, scope, orgID).Scan(&currentType, &currentTarget, &currentCiphertext, &currentNonce, &currentMask, &currentFingerprint); err != nil {
		return NotificationChannel{}, err
	}
	updateTarget := target != ""
	targetCiphertext := currentCiphertext
	targetNonce := currentNonce
	targetMask := currentMask
	targetFingerprint := currentFingerprint
	targetForStorage := currentTarget
	if target == "" {
		if channelType != currentType {
			return NotificationChannel{}, fmt.Errorf("notification target is required when changing channel type")
		}
	} else {
		if err := validateNotificationTarget(channelType, target); err != nil {
			return NotificationChannel{}, err
		}
		targetCiphertext = strings.TrimSpace(input.TargetCiphertext)
		targetNonce = strings.TrimSpace(input.TargetNonce)
		targetMask = strings.TrimSpace(input.TargetMask)
		if targetMask == "" {
			targetMask = MaskNotificationTarget(channelType, target)
		}
		targetFingerprint = strings.TrimSpace(input.TargetFingerprint)
		if targetFingerprint == "" {
			targetFingerprint = HashOpaqueToken(target)[:16]
		}
		targetForStorage = target
		if targetCiphertext != "" && targetNonce != "" {
			targetForStorage = ""
		}
	}
	var item NotificationChannel
	err := r.db.QueryRow(ctx, `
		update notification_channels
		set name=$4,type=$5,target=$6,target_ciphertext=$7,target_nonce=$8,target_mask=$9,target_fingerprint=$10,enabled=$11,updated_at=now()
		where id=$1 and scope=$2 and org_id=$3
		returning id,org_id,scope,name,type,target,
			coalesce(target_ciphertext,''),coalesce(target_nonce,''),coalesce(target_fingerprint,''),coalesce(target_mask,''),
			enabled,created_at,updated_at
	`, channelID, scope, orgID, name, channelType, targetForStorage, targetCiphertext, targetNonce, targetMask, targetFingerprint, input.Enabled).
		Scan(&item.ID, &item.OrgID, &item.Scope, &item.Name, &item.Type, &item.Target, &item.TargetCiphertext, &item.TargetNonce, &item.TargetFingerprint, &item.TargetMask, &item.Enabled, &item.CreatedAt, &item.UpdatedAt)
	if err != nil {
		return NotificationChannel{}, err
	}
	item.TargetMask = notificationChannelMask(item)
	_ = r.WriteAudit(ctx, AuditEvent{ActorType: "user", ActorID: actorID, Action: "notification.channel.updated", ObjectType: "notification_channel", ObjectID: item.ID, Result: "success", Metadata: map[string]any{"scope": scope, "org_id": orgID, "type": channelType, "enabled": item.Enabled, "target_rotated": updateTarget}})
	return item, nil
}

func (r *Repository) DeleteNotificationChannel(ctx context.Context, scope string, orgID string, channelID string, actorID string) error {
	scope = normalizeAlertScope(scope)
	orgID = normalizeAlertOrg(scope, orgID)
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `update orgs set default_notification_channel_id=null, updated_at=now() where default_notification_channel_id=$1`, channelID); err != nil {
		return err
	}
	tag, err := tx.Exec(ctx, `delete from notification_channels where id=$1 and scope=$2 and org_id=$3`, channelID, scope, orgID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	if err := writeAuditTx(ctx, tx, AuditEvent{ActorType: "user", ActorID: actorID, Action: "notification.channel.deleted", ObjectType: "notification_channel", ObjectID: channelID, Result: "success", Metadata: map[string]any{"scope": scope, "org_id": orgID}}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (r *Repository) BulkUpdateNotificationChannels(ctx context.Context, scope string, orgID string, ids []string, actorID string, enabled bool) ([]NotificationChannel, error) {
	scope = normalizeAlertScope(scope)
	orgID = normalizeAlertOrg(scope, orgID)
	ids = uniqueIDs(ids)
	if len(ids) == 0 {
		return nil, fmt.Errorf("select at least one notification channel")
	}
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	tag, err := tx.Exec(ctx, `
		update notification_channels
		set enabled=$4, updated_at=now()
		where id=any($1) and scope=$2 and org_id=$3
	`, ids, scope, orgID, enabled)
	if err != nil {
		return nil, err
	}
	if int(tag.RowsAffected()) != len(ids) {
		return nil, pgx.ErrNoRows
	}
	for _, id := range ids {
		if err := writeAuditTx(ctx, tx, AuditEvent{
			ActorType: "user", ActorID: actorID, Action: "notification.channel.bulk_enabled",
			ObjectType: "notification_channel", ObjectID: id, Result: "success",
			Metadata: map[string]any{"scope": scope, "org_id": orgID, "enabled": enabled},
		}); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return r.NotificationChannels(ctx, scope, orgID)
}

func (r *Repository) BulkDeleteNotificationChannels(ctx context.Context, scope string, orgID string, ids []string, actorID string) ([]NotificationChannel, error) {
	scope = normalizeAlertScope(scope)
	orgID = normalizeAlertOrg(scope, orgID)
	ids = uniqueIDs(ids)
	if len(ids) == 0 {
		return nil, fmt.Errorf("select at least one notification channel")
	}
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `update orgs set default_notification_channel_id=null, updated_at=now() where default_notification_channel_id=any($1)`, ids); err != nil {
		return nil, err
	}
	tag, err := tx.Exec(ctx, `delete from notification_channels where id=any($1) and scope=$2 and org_id=$3`, ids, scope, orgID)
	if err != nil {
		return nil, err
	}
	if int(tag.RowsAffected()) != len(ids) {
		return nil, pgx.ErrNoRows
	}
	for _, id := range ids {
		if err := writeAuditTx(ctx, tx, AuditEvent{
			ActorType: "user", ActorID: actorID, Action: "notification.channel.bulk_deleted",
			ObjectType: "notification_channel", ObjectID: id, Result: "success",
			Metadata: map[string]any{"scope": scope, "org_id": orgID},
		}); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return r.NotificationChannels(ctx, scope, orgID)
}

func (r *Repository) TestNotificationChannel(ctx context.Context, scope string, orgID string, channelID string, actorID string) (AlertDelivery, error) {
	scope = normalizeAlertScope(scope)
	orgID = normalizeAlertOrg(scope, orgID)
	var channel NotificationChannel
	err := r.db.QueryRow(ctx, `
		select id,org_id,scope,name,type,target,
			coalesce(target_ciphertext,''),coalesce(target_nonce,''),coalesce(target_fingerprint,''),coalesce(target_mask,''),
			enabled,created_at,updated_at
		from notification_channels
		where id=$1 and scope=$2 and org_id=$3
	`, channelID, scope, orgID).Scan(&channel.ID, &channel.OrgID, &channel.Scope, &channel.Name, &channel.Type, &channel.Target, &channel.TargetCiphertext, &channel.TargetNonce, &channel.TargetFingerprint, &channel.TargetMask, &channel.Enabled, &channel.CreatedAt, &channel.UpdatedAt)
	if err != nil {
		return AlertDelivery{}, err
	}
	channel.TargetMask = notificationChannelMask(channel)
	status := "test"
	errorText := ""
	if !channel.Enabled {
		status = "failed"
		errorText = "notification channel is disabled"
	}
	return r.insertAlertDelivery(ctx, insertAlertDeliveryInput{
		Scope: scope, OrgID: orgID, ChannelID: channel.ID, DedupeKey: "test:" + channel.ID,
		Severity: "info", Status: status, Title: "测试通知", Message: "TokHub 测试通知已发送到 " + channel.Name,
		Error: errorText, Metadata: map[string]any{"actor_id": actorID, "channel_type": channel.Type},
	})
}

func (r *Repository) EvaluateAlerts(ctx context.Context, scope string, orgID string, actor PublicUser) ([]AlertDelivery, error) {
	scope = normalizeAlertScope(scope)
	orgID = normalizeAlertOrg(scope, orgID)
	if err := r.EnsureDefaultAlertConfig(ctx, scope, orgID, actor.ID, actor.Email); err != nil {
		return nil, err
	}
	rules, err := r.AlertRules(ctx, scope, orgID)
	if err != nil {
		return nil, err
	}
	out := []AlertDelivery{}
	for _, rule := range rules {
		if !rule.Enabled {
			continue
		}
		triggered, value, message, affectedChannels, err := r.evaluateAlertRule(ctx, rule)
		if err != nil {
			return nil, err
		}
		dedupeKey := fmt.Sprintf("%s:%s:%s", rule.Scope, rule.OrgID, rule.ID)
		if triggered {
			if rule.Kind == "l3_consecutive_failures" && len(affectedChannels) > 0 {
				if err := r.disableGatewayUpstreamsForChannels(ctx, rule.OrgID, affectedChannels); err != nil {
					return nil, err
				}
			}
			delivery, err := r.recordTriggeredAlert(ctx, rule, dedupeKey, value, message, affectedChannels)
			if err != nil {
				return nil, err
			}
			out = append(out, delivery)
			continue
		}
		delivery, ok, err := r.recordRecoveredAlert(ctx, rule, dedupeKey, value)
		if err != nil {
			return nil, err
		}
		if ok {
			out = append(out, delivery)
		}
	}
	return out, nil
}

func (r *Repository) AlertDeliveries(ctx context.Context, scope string, orgID string, limit int) ([]AlertDelivery, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := r.db.Query(ctx, `
		select d.id,d.org_id,d.scope,coalesce(d.rule_id,''),coalesce(r.name,''),coalesce(d.notification_channel_id,''),coalesce(n.name,''),coalesce(d.incident_id,''),
			d.dedupe_key,d.severity,d.status,d.title,d.message,d.error,d.metadata,d.created_at,d.sent_at
		from alert_deliveries d
		left join alert_rules r on r.id=d.rule_id
		left join notification_channels n on n.id=d.notification_channel_id
		where d.scope=$1 and d.org_id=$2
		order by d.created_at desc
		limit $3
	`, normalizeAlertScope(scope), normalizeAlertOrg(scope, orgID), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []AlertDelivery{}
	for rows.Next() {
		item, err := scanAlertDelivery(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (r *Repository) AlertSummary(ctx context.Context, scope string, orgID string) (AlertSummary, error) {
	scope = normalizeAlertScope(scope)
	orgID = normalizeAlertOrg(scope, orgID)
	var s AlertSummary
	err := r.db.QueryRow(ctx, `
		select
			(select count(*) from alert_rules where scope=$1 and org_id=$2 and enabled=true),
				(select count(*) from incidents i join channels c on c.id=i.channel_id where i.deleted_at is null and i.resolved_at is null and ($2::text='' or (case when c.owner_type='user' then coalesce(c.org_id,'org_' || coalesce(c.owner_id,'')) else $3::text end)=$2)),
			(select count(*) from alert_deliveries where scope=$1 and org_id=$2 and status='sent' and created_at::date=current_date),
			(select count(*) from alert_deliveries where scope=$1 and org_id=$2 and status='suppressed' and created_at::date=current_date),
			(select count(*) from alert_deliveries where scope=$1 and org_id=$2 and status='failed' and created_at::date=current_date),
			(select count(*) from alert_deliveries where scope=$1 and org_id=$2 and status='recovered' and created_at::date=current_date)
	`, scope, orgID, DefaultOrgID).Scan(&s.EnabledRules, &s.OpenIncidents, &s.SentToday, &s.SuppressedToday, &s.FailedToday, &s.RecoveredToday)
	return s, err
}

func (r *Repository) AuditLogs(ctx context.Context, query AuditQuery) (AuditLogResult, error) {
	limit := query.Limit
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	where := []string{"true"}
	args := []any{}
	addArg := func(value any) string {
		args = append(args, value)
		return fmt.Sprintf("$%d", len(args))
	}
	if strings.TrimSpace(query.OrgID) != "" {
		p := addArg(strings.TrimSpace(query.OrgID))
		where = append(where, fmt.Sprintf(`(
			a.actor_id in (select user_id from org_members where org_id=%[1]s)
			or exists(select 1 from gateways g where g.id=a.object_id and g.org_id=%[1]s)
			or exists(select 1 from gateway_keys k where k.id=a.object_id and k.org_id=%[1]s)
			or exists(select 1 from gateways g where g.id=a.metadata->>'gateway_id' and g.org_id=%[1]s)
				or exists(select 1 from channels c where c.id=a.object_id and c.owner_type='user' and coalesce(c.org_id,'org_' || coalesce(c.owner_id,''))=%[1]s)
		)`, p))
	}
	if strings.TrimSpace(query.ID) != "" {
		where = append(where, "a.id="+addArg(strings.TrimSpace(query.ID)))
	}
	switch strings.TrimSpace(query.EventClass) {
	case "governance":
		where = append(where, "a.action <> 'probe.status.changed'")
	case "system":
		where = append(where, "a.action = 'probe.status.changed'")
	}
	if strings.TrimSpace(query.Actor) != "" {
		p := addArg(strings.TrimSpace(query.Actor))
		where = append(where, fmt.Sprintf("(a.actor_id=%[1]s or coalesce(u.email,'') ilike '%%' || %[1]s || '%%')", p))
	}
	if strings.TrimSpace(query.Action) != "" {
		where = append(where, "a.action="+addArg(strings.TrimSpace(query.Action)))
	}
	if strings.TrimSpace(query.ObjectType) != "" {
		where = append(where, "a.object_type="+addArg(strings.TrimSpace(query.ObjectType)))
	}
	if strings.TrimSpace(query.ObjectID) != "" {
		where = append(where, "a.object_id ilike '%' || "+addArg(strings.TrimSpace(query.ObjectID))+" || '%'")
	}
	if strings.TrimSpace(query.Result) != "" {
		where = append(where, "a.result="+addArg(strings.TrimSpace(query.Result)))
	}
	if !query.From.IsZero() {
		where = append(where, "a.created_at >= "+addArg(query.From))
	}
	if !query.To.IsZero() {
		where = append(where, "a.created_at <= "+addArg(query.To))
	}
	if strings.TrimSpace(query.Query) != "" {
		p := addArg(strings.TrimSpace(query.Query))
		where = append(where, fmt.Sprintf("(a.action ilike '%%' || %[1]s || '%%' or a.object_type ilike '%%' || %[1]s || '%%' or a.object_id ilike '%%' || %[1]s || '%%' or coalesce(u.email,'') ilike '%%' || %[1]s || '%%')", p))
	}
	whereSQL := strings.Join(where, " and ")
	var total int
	countArgs := append([]any{}, args...)
	if err := r.db.QueryRow(ctx, "select count(*) from audit_events a left join users u on u.id=a.actor_id where "+whereSQL, countArgs...).Scan(&total); err != nil {
		return AuditLogResult{}, err
	}
	args = append(args, limit)
	rows, err := r.db.Query(ctx, `
		select a.id,a.actor_type,coalesce(a.actor_id,''),coalesce(u.email,''),a.action,a.object_type,coalesce(a.object_id,''),coalesce(a.ip,''),a.result,a.metadata,a.created_at
		from audit_events a
		left join users u on u.id=a.actor_id
		where `+whereSQL+`
		order by a.created_at desc
		limit $`+fmt.Sprint(len(args)), args...)
	if err != nil {
		return AuditLogResult{}, err
	}
	defer rows.Close()
	out := []AuditLogItem{}
	for rows.Next() {
		item, err := scanAuditLog(rows)
		if err != nil {
			return AuditLogResult{}, err
		}
		out = append(out, item)
	}
	return AuditLogResult{Items: out, Total: total}, rows.Err()
}

func (r *Repository) AuditLogDetail(ctx context.Context, id string, orgID string) (AuditLogItem, error) {
	result, err := r.AuditLogs(ctx, AuditQuery{OrgID: orgID, ID: id, Limit: 1})
	if err != nil {
		return AuditLogItem{}, err
	}
	for _, item := range result.Items {
		if item.ID == id {
			return item, nil
		}
	}
	return AuditLogItem{}, sql.ErrNoRows
}

func (r *Repository) Incidents(ctx context.Context, orgID string, limit int) ([]IncidentItem, error) {
	return r.IncidentsWithFilter(ctx, IncidentQuery{OrgID: orgID, Limit: limit})
}

func (r *Repository) IncidentsWithFilter(ctx context.Context, query IncidentQuery) ([]IncidentItem, error) {
	orgID := strings.TrimSpace(query.OrgID)
	limit := query.Limit
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	args := []any{orgID, DefaultOrgID}
	where := []string{
		"i.deleted_at is null",
		"($1::text='' or (case when c.owner_type='user' then coalesce(c.org_id,'org_' || coalesce(c.owner_id,'')) else $2::text end)=$1)",
	}
	status := strings.ToLower(strings.TrimSpace(query.Status))
	switch status {
	case "", "all":
	case "open":
		where = append(where, "i.resolved_at is null")
	case "resolved", "closed":
		where = append(where, "i.resolved_at is not null")
	default:
		args = append(args, status)
		where = append(where, fmt.Sprintf("i.status=$%d", len(args)))
	}
	if q := strings.TrimSpace(query.Query); q != "" {
		args = append(args, q)
		p := fmt.Sprintf("$%d", len(args))
		where = append(where, fmt.Sprintf("(i.title ilike '%%' || %[1]s || '%%' or i.status ilike '%%' || %[1]s || '%%' or i.channel_id ilike '%%' || %[1]s || '%%' or c.name ilike '%%' || %[1]s || '%%')", p))
	}
	args = append(args, limit)
	rows, err := r.db.Query(ctx, `
			select i.id,(case when c.owner_type='user' then coalesce(c.org_id,'org_' || coalesce(c.owner_id,'')) else $2::text end),i.channel_id,c.name,i.status,i.title,i.opened_at,i.resolved_at,i.metadata
		from incidents i
		join channels c on c.id=i.channel_id
		where `+strings.Join(where, " and ")+`
		order by case when i.resolved_at is null then 0 else 1 end, i.opened_at desc
		limit $`+fmt.Sprint(len(args))+`
	`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return r.scanIncidentRows(ctx, rows)
}

func (r *Repository) scanIncidentRows(ctx context.Context, rows pgx.Rows) ([]IncidentItem, error) {
	out := []IncidentItem{}
	for rows.Next() {
		var item IncidentItem
		var resolved sql.NullTime
		var raw []byte
		if err := rows.Scan(&item.ID, &item.OrgID, &item.ChannelID, &item.Channel, &item.Status, &item.Title, &item.OpenedAt, &resolved, &raw); err != nil {
			return nil, err
		}
		item.Open = !resolved.Valid
		if resolved.Valid {
			item.ResolvedAt = &resolved.Time
		}
		item.Metadata = decodeMap(raw)
		events, err := r.incidentEvents(ctx, item.ID)
		if err != nil {
			return nil, err
		}
		item.Events = events
		out = append(out, item)
	}
	return out, rows.Err()
}

func (r *Repository) IncidentByID(ctx context.Context, orgID string, incidentID string) (IncidentItem, error) {
	rows, err := r.db.Query(ctx, `
			select i.id,(case when c.owner_type='user' then coalesce(c.org_id,'org_' || coalesce(c.owner_id,'')) else $2::text end),i.channel_id,c.name,i.status,i.title,i.opened_at,i.resolved_at,i.metadata
		from incidents i
		join channels c on c.id=i.channel_id
		where i.id=$3
			and i.deleted_at is null
				and ($1::text='' or (case when c.owner_type='user' then coalesce(c.org_id,'org_' || coalesce(c.owner_id,'')) else $2::text end)=$1)
		limit 1
	`, strings.TrimSpace(orgID), DefaultOrgID, strings.TrimSpace(incidentID))
	if err != nil {
		return IncidentItem{}, err
	}
	defer rows.Close()
	items, err := r.scanIncidentRows(ctx, rows)
	if err != nil {
		return IncidentItem{}, err
	}
	if len(items) == 0 {
		return IncidentItem{}, pgx.ErrNoRows
	}
	return items[0], nil
}

func (r *Repository) CreateIncident(ctx context.Context, orgID string, actor PublicUser, input IncidentInput) (IncidentItem, error) {
	input = normalizeIncidentInput(input)
	if input.ChannelID == "" || input.Title == "" {
		return IncidentItem{}, ErrInvalidIncidentInput
	}
	orgID = strings.TrimSpace(orgID)
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return IncidentItem{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	channelOrgID, channelName, err := incidentChannelForOrg(ctx, tx, input.ChannelID, orgID)
	if err != nil {
		return IncidentItem{}, err
	}
	var openExists bool
	if err := tx.QueryRow(ctx, `select exists(select 1 from incidents where channel_id=$1 and resolved_at is null and deleted_at is null)`, input.ChannelID).Scan(&openExists); err != nil {
		return IncidentItem{}, err
	}
	if openExists {
		return IncidentItem{}, ErrOpenIncidentExists
	}
	meta := input.Metadata
	if meta == nil {
		meta = map[string]any{}
	}
	meta["source"] = "manual"
	meta["createdBy"] = actor.ID
	raw, err := json.Marshal(meta)
	if err != nil {
		return IncidentItem{}, err
	}
	id := "inc_" + uuid.NewString()
	if _, err := tx.Exec(ctx, `
		insert into incidents(id,channel_id,status,title,opened_at,metadata)
		values($1,$2,$3,$4,now(),$5)
	`, id, input.ChannelID, input.Status, input.Title, raw); err != nil {
		return IncidentItem{}, err
	}
	if err := insertIncidentEventTx(ctx, tx, id, channelOrgID, "manual_created", defaultString(input.Message, "后台手动登记 Incident"), map[string]any{"channel": channelName, "actorId": actor.ID}); err != nil {
		return IncidentItem{}, err
	}
	if err := writeAuditTx(ctx, tx, AuditEvent{
		ActorType: "user", ActorID: actor.ID, Action: "incident.created", ObjectType: "incident", ObjectID: id, Result: "success",
		Metadata: map[string]any{"orgId": channelOrgID, "channelId": input.ChannelID, "status": input.Status},
	}); err != nil {
		return IncidentItem{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return IncidentItem{}, err
	}
	return r.IncidentByID(ctx, orgID, id)
}

func (r *Repository) UpdateIncident(ctx context.Context, orgID string, actor PublicUser, incidentID string, input IncidentInput) (IncidentItem, error) {
	incidentID = strings.TrimSpace(incidentID)
	input = normalizeIncidentInput(input)
	if incidentID == "" || input.Title == "" || input.Status == "" {
		return IncidentItem{}, ErrInvalidIncidentInput
	}
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return IncidentItem{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	channelOrgID, channelID, err := incidentForOrg(ctx, tx, incidentID, strings.TrimSpace(orgID))
	if err != nil {
		return IncidentItem{}, err
	}
	tag, err := tx.Exec(ctx, `
		update incidents
		set status=$2,title=$3,metadata=jsonb_set(metadata, '{updatedBy}', to_jsonb($4::text), true)
		where id=$1 and deleted_at is null
	`, incidentID, input.Status, input.Title, actor.ID)
	if err != nil {
		return IncidentItem{}, err
	}
	if tag.RowsAffected() == 0 {
		return IncidentItem{}, pgx.ErrNoRows
	}
	if err := insertIncidentEventTx(ctx, tx, incidentID, channelOrgID, "manual_updated", defaultString(input.Message, "后台编辑 Incident"), map[string]any{"actorId": actor.ID, "status": input.Status, "title": input.Title}); err != nil {
		return IncidentItem{}, err
	}
	if err := writeAuditTx(ctx, tx, AuditEvent{
		ActorType: "user", ActorID: actor.ID, Action: "incident.updated", ObjectType: "incident", ObjectID: incidentID, Result: "success",
		Metadata: map[string]any{"orgId": channelOrgID, "channelId": channelID, "status": input.Status},
	}); err != nil {
		return IncidentItem{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return IncidentItem{}, err
	}
	return r.IncidentByID(ctx, orgID, incidentID)
}

func (r *Repository) ResolveIncident(ctx context.Context, orgID string, actor PublicUser, incidentID string, message string) (IncidentItem, error) {
	return r.updateIncidentState(ctx, orgID, actor, incidentID, "manual_resolved", defaultString(message, "后台手动关闭 Incident"), "update incidents set resolved_at=coalesce(resolved_at, now()) where id=$1 and deleted_at is null")
}

func (r *Repository) ReopenIncident(ctx context.Context, orgID string, actor PublicUser, incidentID string, message string) (IncidentItem, error) {
	return r.updateIncidentState(ctx, orgID, actor, incidentID, "manual_reopened", defaultString(message, "后台手动重开 Incident"), "update incidents set resolved_at=null where id=$1 and deleted_at is null")
}

func (r *Repository) DeleteIncident(ctx context.Context, orgID string, actor PublicUser, incidentID string, message string) error {
	_, err := r.updateIncidentState(ctx, orgID, actor, incidentID, "manual_deleted", defaultString(message, "后台软删除 Incident"), "update incidents set deleted_at=coalesce(deleted_at, now()), resolved_at=coalesce(resolved_at, now()) where id=$1 and deleted_at is null")
	return err
}

func (r *Repository) BulkUpdateIncidents(ctx context.Context, orgID string, actor PublicUser, action string, incidentIDs []string, message string) ([]IncidentItem, error) {
	ids := uniqueStrings(incidentIDs)
	if len(ids) == 0 {
		return nil, ErrInvalidIncidentInput
	}
	action = strings.ToLower(strings.TrimSpace(action))
	if action != "resolve" && action != "close" && action != "reopen" && action != "delete" {
		return nil, ErrInvalidIncidentInput
	}
	orgID = strings.TrimSpace(orgID)
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	states, err := r.incidentStatesForBulk(ctx, tx, orgID, ids)
	if err != nil {
		return nil, err
	}
	if len(states) != len(ids) {
		return nil, pgx.ErrNoRows
	}
	if action == "reopen" {
		channelCounts := map[string]int{}
		channelIDs := []string{}
		for _, id := range ids {
			state := states[id]
			channelCounts[state.channelID]++
			if channelCounts[state.channelID] == 1 {
				channelIDs = append(channelIDs, state.channelID)
			}
			if channelCounts[state.channelID] > 1 {
				return nil, ErrOpenIncidentExists
			}
		}
		var openExists bool
		if err := tx.QueryRow(ctx, `
			select exists(
				select 1 from incidents
				where deleted_at is null and resolved_at is null and id <> all($1) and channel_id=any($2)
			)
		`, ids, channelIDs).Scan(&openExists); err != nil {
			return nil, err
		}
		if openExists {
			return nil, ErrOpenIncidentExists
		}
	}
	var updateSQL string
	eventType := ""
	auditAction := ""
	defaultMessage := ""
	switch action {
	case "resolve", "close":
		updateSQL = "update incidents set resolved_at=coalesce(resolved_at, now()) where id=any($1) and deleted_at is null"
		eventType = "manual_resolved"
		auditAction = "incident.resolved"
		defaultMessage = "后台批量关闭 Incident"
	case "reopen":
		updateSQL = "update incidents set resolved_at=null where id=any($1) and deleted_at is null"
		eventType = "manual_reopened"
		auditAction = "incident.reopened"
		defaultMessage = "后台批量重开 Incident"
	case "delete":
		updateSQL = "update incidents set deleted_at=coalesce(deleted_at, now()), resolved_at=coalesce(resolved_at, now()) where id=any($1) and deleted_at is null"
		eventType = "manual_deleted"
		auditAction = "incident.deleted"
		defaultMessage = "后台批量软删除 Incident"
	}
	tag, err := tx.Exec(ctx, updateSQL, ids)
	if err != nil {
		return nil, err
	}
	if int(tag.RowsAffected()) != len(ids) {
		return nil, pgx.ErrNoRows
	}
	for _, id := range ids {
		state := states[id]
		if err := insertIncidentEventTx(ctx, tx, id, state.orgID, eventType, defaultString(message, defaultMessage), map[string]any{"actorId": actor.ID}); err != nil {
			return nil, err
		}
		if err := writeAuditTx(ctx, tx, AuditEvent{
			ActorType: "user", ActorID: actor.ID, Action: auditAction, ObjectType: "incident", ObjectID: id, Result: "success",
			Metadata: map[string]any{"orgId": state.orgID, "channelId": state.channelID},
		}); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return r.IncidentsWithFilter(ctx, IncidentQuery{OrgID: orgID, Limit: 100})
}

type incidentBulkState struct {
	id        string
	orgID     string
	channelID string
}

func (r *Repository) incidentStatesForBulk(ctx context.Context, q DBTX, orgID string, incidentIDs []string) (map[string]incidentBulkState, error) {
	rows, err := q.Query(ctx, `
			select i.id,(case when c.owner_type='user' then coalesce(c.org_id,'org_' || coalesce(c.owner_id,'')) else $2::text end),i.channel_id
		from incidents i
		join channels c on c.id=i.channel_id
		where i.deleted_at is null
			and i.id=any($3)
				and ($1::text='' or (case when c.owner_type='user' then coalesce(c.org_id,'org_' || coalesce(c.owner_id,'')) else $2::text end)=$1)
		for update of i
	`, strings.TrimSpace(orgID), DefaultOrgID, incidentIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	states := map[string]incidentBulkState{}
	for rows.Next() {
		var state incidentBulkState
		if err := rows.Scan(&state.id, &state.orgID, &state.channelID); err != nil {
			return nil, err
		}
		states[state.id] = state
	}
	return states, rows.Err()
}

func (r *Repository) updateIncidentState(ctx context.Context, orgID string, actor PublicUser, incidentID string, eventType string, message string, updateSQL string) (IncidentItem, error) {
	incidentID = strings.TrimSpace(incidentID)
	if incidentID == "" {
		return IncidentItem{}, ErrInvalidIncidentInput
	}
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return IncidentItem{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	channelOrgID, channelID, err := incidentForOrg(ctx, tx, incidentID, strings.TrimSpace(orgID))
	if err != nil {
		return IncidentItem{}, err
	}
	if eventType == "manual_reopened" {
		var openExists bool
		if err := tx.QueryRow(ctx, `select exists(select 1 from incidents where channel_id=$1 and id<>$2 and resolved_at is null and deleted_at is null)`, channelID, incidentID).Scan(&openExists); err != nil {
			return IncidentItem{}, err
		}
		if openExists {
			return IncidentItem{}, ErrOpenIncidentExists
		}
	}
	tag, err := tx.Exec(ctx, updateSQL, incidentID)
	if err != nil {
		return IncidentItem{}, err
	}
	if tag.RowsAffected() == 0 {
		return IncidentItem{}, pgx.ErrNoRows
	}
	if err := insertIncidentEventTx(ctx, tx, incidentID, channelOrgID, eventType, message, map[string]any{"actorId": actor.ID}); err != nil {
		return IncidentItem{}, err
	}
	auditAction := "incident.updated"
	switch eventType {
	case "manual_resolved":
		auditAction = "incident.resolved"
	case "manual_reopened":
		auditAction = "incident.reopened"
	case "manual_deleted":
		auditAction = "incident.deleted"
	}
	if err := writeAuditTx(ctx, tx, AuditEvent{
		ActorType: "user", ActorID: actor.ID, Action: auditAction, ObjectType: "incident", ObjectID: incidentID, Result: "success",
		Metadata: map[string]any{"orgId": channelOrgID},
	}); err != nil {
		return IncidentItem{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return IncidentItem{}, err
	}
	if eventType == "manual_deleted" {
		return IncidentItem{ID: incidentID}, nil
	}
	return r.IncidentByID(ctx, orgID, incidentID)
}

func normalizeIncidentInput(input IncidentInput) IncidentInput {
	input.ChannelID = strings.TrimSpace(input.ChannelID)
	input.Title = strings.TrimSpace(input.Title)
	input.Message = strings.TrimSpace(input.Message)
	status := strings.ToLower(strings.TrimSpace(input.Status))
	if status == "" {
		status = "manual"
	}
	status = strings.ReplaceAll(status, " ", "_")
	status = strings.ReplaceAll(status, "-", "_")
	if len(status) > 64 {
		status = status[:64]
	}
	input.Status = status
	if len(input.Title) > 160 {
		input.Title = input.Title[:160]
	}
	if len(input.Message) > 500 {
		input.Message = input.Message[:500]
	}
	return input
}

func incidentChannelForOrg(ctx context.Context, tx pgx.Tx, channelID string, orgID string) (string, string, error) {
	var channelOrgID, channelName string
	err := tx.QueryRow(ctx, `
		select
				(case when owner_type='user' then coalesce(org_id,'org_' || coalesce(owner_id,'')) else $3::text end),
			name
		from channels
		where id=$1
			and status <> 'deleted'
			and deleted_at is null
				and ($2::text='' or (case when owner_type='user' then coalesce(org_id,'org_' || coalesce(owner_id,'')) else $3::text end)=$2)
	`, channelID, strings.TrimSpace(orgID), DefaultOrgID).Scan(&channelOrgID, &channelName)
	return channelOrgID, channelName, err
}

func incidentForOrg(ctx context.Context, tx pgx.Tx, incidentID string, orgID string) (string, string, error) {
	var channelOrgID, channelID string
	err := tx.QueryRow(ctx, `
		select
				(case when c.owner_type='user' then coalesce(c.org_id,'org_' || coalesce(c.owner_id,'')) else $3::text end),
			i.channel_id
		from incidents i
		join channels c on c.id=i.channel_id
		where i.id=$1
			and i.deleted_at is null
				and ($2::text='' or (case when c.owner_type='user' then coalesce(c.org_id,'org_' || coalesce(c.owner_id,'')) else $3::text end)=$2)
	`, incidentID, strings.TrimSpace(orgID), DefaultOrgID).Scan(&channelOrgID, &channelID)
	return channelOrgID, channelID, err
}

func (r *Repository) GovernanceSummary(ctx context.Context, scope string, orgID string) (GovernanceSummary, error) {
	scope = normalizeAlertScope(scope)
	orgID = normalizeAlertOrg(scope, orgID)
	var s GovernanceSummary
	var cost float64
	err := r.db.QueryRow(ctx, `
		select
				(select count(*) from incidents i join channels c on c.id=i.channel_id where i.deleted_at is null and i.resolved_at is null and ($1::text='' or (case when c.owner_type='user' then coalesce(c.org_id,'org_' || coalesce(c.owner_id,'')) else $3::text end)=$1)),
			(select count(*) from alert_deliveries where scope=$2 and org_id=$1 and created_at::date=current_date),
			(select count(*) from audit_events a where a.created_at::date=current_date and a.action <> 'probe.status.changed' and ($1::text='' or a.actor_id in (select user_id from org_members where org_id=$1))),
			(select coalesce(sum(cost_usd),0) from usage_daily_rollups where day=current_date and ($1::text='' or org_id=$1))
	`, orgID, scope, DefaultOrgID).Scan(&s.OpenIncidents, &s.AlertsToday, &s.AuditToday, &cost)
	if err != nil {
		return s, err
	}
	s.CostTodayUSD = round3(cost)
	alerts, err := r.AlertDeliveries(ctx, scope, orgID, 5)
	if err != nil {
		return s, err
	}
	s.RecentAlerts = alerts
	audit, err := r.AuditLogs(ctx, AuditQuery{OrgID: orgID, Limit: 5})
	if err != nil {
		return s, err
	}
	s.RecentAudit = audit.Items
	incidents, err := r.Incidents(ctx, orgID, 5)
	if err != nil {
		return s, err
	}
	s.Incidents = incidents
	return s, nil
}

func (r *Repository) MetricsSnapshot(ctx context.Context) (MetricsSnapshot, error) {
	var m MetricsSnapshot
	err := r.db.QueryRow(ctx, `
		select
			(select count(*) from gateway_request_events),
			(select count(*) from gateway_request_events where status_code >= 400),
			(select count(*) from probe_runs),
			(select count(*) from incidents where deleted_at is null and resolved_at is null),
			(select count(*) from alert_deliveries),
			(select count(*) from audit_events),
			(select count(*) from usage_daily_rollups)
	`).Scan(&m.GatewayRequests, &m.GatewayErrors, &m.ProbeRuns, &m.OpenIncidents, &m.AlertDeliveries, &m.AuditEvents, &m.UsageRollups)
	return m, err
}

func (r *Repository) evaluateAlertRule(ctx context.Context, rule AlertRule) (bool, float64, string, []string, error) {
	switch rule.Kind {
	case "cost_threshold":
		var value float64
		err := r.db.QueryRow(ctx, `
			select coalesce(sum(cost_usd),0)
			from gateway_request_events e join gateways g on g.id=e.gateway_id
			where e.created_at::date=current_date and ($1::text='' or g.org_id=$1)
		`, rule.OrgID).Scan(&value)
		return value >= rule.Threshold && rule.Threshold >= 0, value, fmt.Sprintf("今日成本 $%.4f，阈值 $%.4f", value, rule.Threshold), nil, err
	case "gateway_error_rate":
		var total, errors int
		err := r.db.QueryRow(ctx, `
			select count(*), coalesce(sum(case when e.status_code >= 400 then 1 else 0 end),0)
			from gateway_request_events e join gateways g on g.id=e.gateway_id
			where e.created_at >= now() - ($1::int * interval '1 minute') and ($2::text='' or g.org_id=$2)
		`, rule.WindowMinutes, rule.OrgID).Scan(&total, &errors)
		value := 0.0
		if total > 0 {
			value = float64(errors) / float64(total) * 100
		}
		return total > 0 && value >= rule.Threshold, value, fmt.Sprintf("近 %d 分钟错误率 %.1f%%，阈值 %.1f%%", rule.WindowMinutes, value, rule.Threshold), nil, err
	case "quota_anomaly":
		var value float64
		err := r.db.QueryRow(ctx, `
			select coalesce(max(case when k.quota_month > 0 then k.requests_used::numeric / k.quota_month::numeric * 100 else 0 end),0)
			from gateway_keys k join gateways g on g.id=k.gateway_id
			where k.status='active' and ($1::text='' or g.org_id=$1)
		`, rule.OrgID).Scan(&value)
		return value >= rule.Threshold, value, fmt.Sprintf("最高 Key 配额使用 %.1f%%，阈值 %.1f%%", value, rule.Threshold), nil, err
	default:
		rows, err := r.db.Query(ctx, `
			select pr.channel_id,count(*)::int
			from probe_runs pr
			join channels c on c.id=pr.channel_id
			where pr.layer='l3' and pr.status='failed'
				and pr.started_at >= now() - ($1::int * interval '1 minute')
				and ($2::text='' or (case when c.owner_type='user' then coalesce(c.org_id,'org_' || coalesce(c.owner_id,'')) else $4::text end)=$2)
			group by pr.channel_id
			having count(*) >= $3
		`, rule.WindowMinutes, rule.OrgID, int(rule.Threshold), DefaultOrgID)
		if err != nil {
			return false, 0, "", nil, err
		}
		defer rows.Close()
		channels := []string{}
		maxFailures := 0
		for rows.Next() {
			var id string
			var count int
			if err := rows.Scan(&id, &count); err != nil {
				return false, 0, "", nil, err
			}
			channels = append(channels, id)
			if count > maxFailures {
				maxFailures = count
			}
		}
		if err := rows.Err(); err != nil {
			return false, 0, "", nil, err
		}
		return len(channels) > 0, float64(maxFailures), fmt.Sprintf("近 %d 分钟 L3 连续失败通道 %d 个", rule.WindowMinutes, len(channels)), channels, nil
	}
}

func (r *Repository) recordTriggeredAlert(ctx context.Context, rule AlertRule, dedupeKey string, value float64, message string, channels []string) (AlertDelivery, error) {
	var recent bool
	if err := r.db.QueryRow(ctx, `
		select exists(
			select 1 from alert_deliveries
			where dedupe_key=$1 and status='sent' and created_at >= now() - ($2::int * interval '1 minute')
		)
	`, dedupeKey, rule.DedupeMinutes).Scan(&recent); err != nil {
		return AlertDelivery{}, err
	}
	status := "sent"
	if recent {
		status = "suppressed"
	}
	channelID, err := r.firstNotificationChannel(ctx, rule.Scope, rule.OrgID)
	if err != nil {
		return AlertDelivery{}, err
	}
	errorText := ""
	if channelID == "" {
		status = "failed"
		errorText = "no enabled notification channel"
	}
	return r.insertAlertDelivery(ctx, insertAlertDeliveryInput{
		Scope: rule.Scope, OrgID: rule.OrgID, RuleID: rule.ID, ChannelID: channelID, DedupeKey: dedupeKey,
		Severity: rule.Severity, Status: status, Title: rule.Name, Message: message, Error: errorText,
		Metadata: map[string]any{"value": value, "threshold": rule.Threshold, "kind": rule.Kind, "channels": channels},
	})
}

func (r *Repository) recordRecoveredAlert(ctx context.Context, rule AlertRule, dedupeKey string, value float64) (AlertDelivery, bool, error) {
	var lastSent, lastRecovered sql.NullTime
	if err := r.db.QueryRow(ctx, `
		select
			(select max(created_at) from alert_deliveries where dedupe_key=$1 and status='sent'),
			(select max(created_at) from alert_deliveries where dedupe_key=$1 and status='recovered')
	`, dedupeKey).Scan(&lastSent, &lastRecovered); err != nil {
		return AlertDelivery{}, false, err
	}
	if !lastSent.Valid || (lastRecovered.Valid && lastRecovered.Time.After(lastSent.Time)) {
		return AlertDelivery{}, false, nil
	}
	channelID, err := r.firstNotificationChannel(ctx, rule.Scope, rule.OrgID)
	if err != nil {
		return AlertDelivery{}, false, err
	}
	delivery, err := r.insertAlertDelivery(ctx, insertAlertDeliveryInput{
		Scope: rule.Scope, OrgID: rule.OrgID, RuleID: rule.ID, ChannelID: channelID, DedupeKey: dedupeKey,
		Severity: "info", Status: "recovered", Title: rule.Name + " 已恢复", Message: fmt.Sprintf("当前值 %.2f 已低于阈值 %.2f", value, rule.Threshold),
		Metadata: map[string]any{"value": value, "threshold": rule.Threshold, "kind": rule.Kind},
	})
	return delivery, err == nil, err
}

type insertAlertDeliveryInput struct {
	Scope     string
	OrgID     string
	RuleID    string
	ChannelID string
	DedupeKey string
	Severity  string
	Status    string
	Title     string
	Message   string
	Error     string
	Metadata  map[string]any
}

func (r *Repository) insertAlertDelivery(ctx context.Context, input insertAlertDeliveryInput) (AlertDelivery, error) {
	meta, err := json.Marshal(input.Metadata)
	if err != nil {
		return AlertDelivery{}, err
	}
	var item AlertDelivery
	var raw []byte
	var sent sql.NullTime
	err = r.db.QueryRow(ctx, `
		insert into alert_deliveries(id,org_id,scope,rule_id,notification_channel_id,dedupe_key,severity,status,title,message,error,metadata,sent_at)
		values($1,$2,$3,nullif($4,''),nullif($5,''),$6,$7,$8,$9,$10,$11,$12,case when $8 in ('sent','test','recovered') then now() else null end)
		returning id,org_id,scope,coalesce(rule_id,''),coalesce(notification_channel_id,''),dedupe_key,severity,status,title,message,error,metadata,created_at,sent_at
	`, "ald_"+uuid.NewString(), input.OrgID, input.Scope, input.RuleID, input.ChannelID, input.DedupeKey, input.Severity, input.Status, input.Title, input.Message, input.Error, meta).
		Scan(&item.ID, &item.OrgID, &item.Scope, &item.RuleID, &item.NotificationChannelID, &item.DedupeKey, &item.Severity, &item.Status, &item.Title, &item.Message, &item.Error, &raw, &item.CreatedAt, &sent)
	if err != nil {
		return AlertDelivery{}, err
	}
	item.Metadata = decodeMap(raw)
	if sent.Valid {
		item.SentAt = &sent.Time
	}
	return item, nil
}

func (r *Repository) firstNotificationChannel(ctx context.Context, scope string, orgID string) (string, error) {
	var id string
	err := r.db.QueryRow(ctx, `
		select id from notification_channels
		where scope=$1 and org_id=$2 and enabled=true
		order by created_at asc
		limit 1
	`, scope, orgID).Scan(&id)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return id, err
}

func (r *Repository) disableGatewayUpstreamsForChannels(ctx context.Context, orgID string, channelIDs []string) error {
	if len(channelIDs) == 0 {
		return nil
	}
	_, err := r.db.Exec(ctx, `
		update gateway_upstreams gu
		set enabled=false
		from gateways g
		where g.id=gu.gateway_id
			and gu.channel_id=any($1)
			and ($2::text='' or g.org_id=$2)
	`, channelIDs, orgID)
	return err
}

func (r *Repository) incidentEvents(ctx context.Context, incidentID string) ([]IncidentEvent, error) {
	rows, err := r.db.Query(ctx, `
		select id,event_type,message,metadata,created_at
		from incident_events
		where incident_id=$1
		order by created_at desc
		limit 20
	`, incidentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []IncidentEvent{}
	for rows.Next() {
		var item IncidentEvent
		var raw []byte
		if err := rows.Scan(&item.ID, &item.Type, &item.Message, &raw, &item.CreatedAt); err != nil {
			return nil, err
		}
		item.Metadata = decodeMap(raw)
		out = append(out, item)
	}
	return out, rows.Err()
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanAuditLog(row rowScanner) (AuditLogItem, error) {
	var item AuditLogItem
	var raw []byte
	if err := row.Scan(&item.ID, &item.ActorType, &item.ActorID, &item.ActorEmail, &item.Action, &item.ObjectType, &item.ObjectID, &item.IP, &item.Result, &raw, &item.CreatedAt); err != nil {
		return item, err
	}
	item.Metadata = decodeMap(raw)
	return item, nil
}

func scanAlertDelivery(row rowScanner) (AlertDelivery, error) {
	var item AlertDelivery
	var raw []byte
	var sent sql.NullTime
	if err := row.Scan(&item.ID, &item.OrgID, &item.Scope, &item.RuleID, &item.RuleName, &item.NotificationChannelID, &item.ChannelName, &item.IncidentID, &item.DedupeKey, &item.Severity, &item.Status, &item.Title, &item.Message, &item.Error, &raw, &item.CreatedAt, &sent); err != nil {
		return item, err
	}
	item.Metadata = decodeMap(raw)
	if sent.Valid {
		item.SentAt = &sent.Time
	}
	return item, nil
}

func decodeMap(raw []byte) map[string]any {
	out := map[string]any{}
	_ = json.Unmarshal(raw, &out)
	return out
}

func normalizeAlertScope(scope string) string {
	if strings.TrimSpace(scope) == "console" {
		return "console"
	}
	return "admin"
}

func normalizeAlertOrg(scope string, orgID string) string {
	orgID = strings.TrimSpace(orgID)
	if normalizeAlertScope(scope) == "admin" {
		return ""
	}
	return orgID
}

func normalizeAlertKind(kind string) string {
	switch strings.TrimSpace(kind) {
	case "cost_threshold", "gateway_error_rate", "quota_anomaly":
		return strings.TrimSpace(kind)
	default:
		return "l3_consecutive_failures"
	}
}

func normalizeAlertSeverity(severity string) string {
	switch strings.TrimSpace(severity) {
	case "info", "critical":
		return strings.TrimSpace(severity)
	default:
		return "warning"
	}
}

func validateNotificationTarget(channelType string, target string) error {
	target = strings.TrimSpace(target)
	if target == "" {
		return fmt.Errorf("notification target is required")
	}
	if strings.ContainsAny(target, "\r\n") {
		return fmt.Errorf("notification target contains invalid characters")
	}
	switch channelType {
	case "webhook", "feishu":
		parsed, err := url.Parse(target)
		if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
			return fmt.Errorf("notification webhook target must be an http or https URL")
		}
	default:
		parsed, err := mail.ParseAddress(target)
		if err != nil || parsed.Address != target {
			return fmt.Errorf("notification email target must be a plain email address")
		}
	}
	return nil
}

func MaskNotificationTarget(channelType string, target string) string {
	target = strings.TrimSpace(target)
	if target == "" {
		return ""
	}
	if channelType == "webhook" || channelType == "feishu" {
		parsed, err := url.Parse(target)
		if err != nil || parsed.Host == "" {
			return "***"
		}
		return parsed.Scheme + "://" + parsed.Host + "/***"
	}
	at := strings.LastIndex(target, "@")
	if at <= 0 || at == len(target)-1 {
		return "***"
	}
	name := target[:at]
	domain := target[at+1:]
	if len(name) <= 1 {
		return name + "***@" + domain
	}
	return name[:1] + "***@" + domain
}

func notificationChannelMask(channel NotificationChannel) string {
	if strings.TrimSpace(channel.TargetMask) != "" {
		return strings.TrimSpace(channel.TargetMask)
	}
	return MaskNotificationTarget(channel.Type, channel.Target)
}

func alertKindLabel(kind string) string {
	switch kind {
	case "cost_threshold":
		return "成本超过预算阈值"
	case "gateway_error_rate":
		return "网关错误率超限"
	case "quota_anomaly":
		return "Key 配额异常"
	default:
		return "L3 连续失败"
	}
}

func stableAlertPrefix(scope string, orgID string) string {
	value := scope + "_" + orgID
	value = strings.Trim(value, "_")
	if value == "" {
		value = "admin"
	}
	replacer := strings.NewReplacer(":", "_", "/", "_", ".", "_", "@", "_", "-", "_")
	return "alr_" + replacer.Replace(value)
}
