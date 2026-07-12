package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

type ProbeLogQuery struct {
	ID           string
	From         time.Time
	To           time.Time
	Provider     string
	ChannelID    string
	Layer        string
	Status       string
	HTTPStatus   int
	ErrorType    string
	Source       string
	OnlyAbnormal bool
	Page         int
	PageSize     int
}

type ProbeLogItem struct {
	ID          string     `json:"id"`
	ChannelID   string     `json:"channelId"`
	ChannelName string     `json:"channelName"`
	Provider    string     `json:"provider"`
	Type        string     `json:"type"`
	Model       string     `json:"model"`
	Endpoint    string     `json:"endpoint"`
	Layer       string     `json:"layer"`
	Source      string     `json:"source"`
	Status      string     `json:"status"`
	RunStatus   string     `json:"runStatus"`
	Step        string     `json:"step"`
	HTTPStatus  *int       `json:"httpStatus,omitempty"`
	ErrorType   string     `json:"errorType"`
	LatencyMs   int        `json:"latencyMs"`
	StepCount   int        `json:"stepCount"`
	StartedAt   time.Time  `json:"startedAt"`
	FinishedAt  *time.Time `json:"finishedAt,omitempty"`
}

type ProbeLogStep struct {
	Step       string         `json:"step"`
	Status     string         `json:"status"`
	LatencyMs  int            `json:"latencyMs"`
	HTTPStatus *int           `json:"httpStatus,omitempty"`
	ErrorType  string         `json:"errorType"`
	Metadata   map[string]any `json:"metadata"`
	CreatedAt  time.Time      `json:"createdAt"`
}

type ProbeLogSummary struct {
	Total         int        `json:"total"`
	Abnormal      int        `json:"abnormal"`
	AuthErrors    int        `json:"authErrors"`
	SlowResponses int        `json:"slowResponses"`
	LatestAt      *time.Time `json:"latestAt,omitempty"`
}

type ProbeLogResult struct {
	Items    []ProbeLogItem  `json:"items"`
	Total    int             `json:"total"`
	Page     int             `json:"page"`
	PageSize int             `json:"pageSize"`
	Summary  ProbeLogSummary `json:"summary"`
}

type ProbeLogDetail struct {
	ProbeLogItem
	Steps []ProbeLogStep `json:"steps"`
}

type ProbeLogExportItem struct {
	ProbeLogItem
	UpstreamErrorSummary string
}

const probeLogBaseSQL = `
	with base as (
		select
			pr.id,pr.channel_id,
			case when pr.metadata ? 'channel_name' then pr.metadata->>'channel_name' else c.name end as channel_name,
			case when pr.metadata ? 'provider' then pr.metadata->>'provider' else c.provider end as provider,
			case when pr.metadata ? 'type' then pr.metadata->>'type' else c.type end as type,
			case when pr.metadata ? 'model' then pr.metadata->>'model' else c.model end as model,
			case when pr.metadata ? 'endpoint' then pr.metadata->>'endpoint' else c.endpoint end as endpoint,
			pr.layer,pr.source,pr.status as run_status,
			case
				when pr.status='running' and pr.started_at < now() - interval '10 minutes' then 'failed'
				when pr.status='running' then 'running'
				when primary_result.status is not null then primary_result.status
				when pr.status='success' then 'ok'
				else pr.status
			end as status,
			coalesce(primary_result.step,'') as step,primary_result.http_status,
			coalesce(primary_result.error_type,pr.error_type,
				case when pr.status='running' and pr.started_at < now() - interval '10 minutes' then 'probe_interrupted' end,'') as error_type,
			coalesce(primary_result.metadata->>'upstream_error_summary','') as upstream_error_summary,
			coalesce(result_totals.latency_ms,0)::int as latency_ms,
			coalesce(result_totals.step_count,0)::int as step_count,
			pr.started_at,pr.finished_at
		from probe_runs pr
		join channels c on c.id=pr.channel_id
		left join lateral (
			select r.step,r.status,r.http_status,r.error_type,r.metadata
			from probe_results r
			where r.probe_run_id=pr.id
			order by case r.status when 'auth_error' then 5 when 'down' then 4 when 'warn' then 3 when 'ok' then 2 else 1 end desc,r.created_at desc
			limit 1
		) primary_result on true
		left join lateral (
			select sum(r.latency_ms) as latency_ms,count(*) as step_count
			from probe_results r
			where r.probe_run_id=pr.id
		) result_totals on true
		where c.owner_type='platform'
	)
`

func (r *Repository) ProbeLogs(ctx context.Context, query ProbeLogQuery) (ProbeLogResult, error) {
	page := query.Page
	if page < 1 {
		page = 1
	}
	pageSize := query.PageSize
	if pageSize != 25 && pageSize != 50 && pageSize != 100 {
		pageSize = 50
	}
	whereSQL, args := probeLogWhere(query)

	var summary ProbeLogSummary
	var latest sql.NullTime
	err := r.db.QueryRow(ctx, probeLogBaseSQL+`
		select count(*)::int,
			count(*) filter(where b.status in ('warn','auth_error','down','failed'))::int,
			count(*) filter(where b.status='auth_error' or b.error_type in ('auth_error','models_auth_error'))::int,
			count(*) filter(where b.error_type='slow_response')::int,
			max(b.started_at)
		from base b where `+whereSQL, args...).Scan(&summary.Total, &summary.Abnormal, &summary.AuthErrors, &summary.SlowResponses, &latest)
	if err != nil {
		return ProbeLogResult{}, err
	}
	if latest.Valid {
		summary.LatestAt = &latest.Time
	}

	listArgs := append([]any{}, args...)
	listArgs = append(listArgs, pageSize, (page-1)*pageSize)
	limitArg := fmt.Sprintf("$%d", len(listArgs)-1)
	offsetArg := fmt.Sprintf("$%d", len(listArgs))
	rows, err := r.db.Query(ctx, probeLogBaseSQL+`
		select b.id,b.channel_id,b.channel_name,b.provider,b.type,b.model,b.endpoint,b.layer,b.source,b.status,b.run_status,b.step,b.http_status,b.error_type,b.latency_ms,b.step_count,b.started_at,b.finished_at
		from base b where `+whereSQL+`
		order by b.started_at desc
		limit `+limitArg+` offset `+offsetArg, listArgs...)
	if err != nil {
		return ProbeLogResult{}, err
	}
	defer rows.Close()
	items := make([]ProbeLogItem, 0, pageSize)
	for rows.Next() {
		item, err := scanProbeLogItem(rows)
		if err != nil {
			return ProbeLogResult{}, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return ProbeLogResult{}, err
	}
	return ProbeLogResult{Items: items, Total: summary.Total, Page: page, PageSize: pageSize, Summary: summary}, nil
}

func (r *Repository) ProbeLogDetail(ctx context.Context, runID string) (ProbeLogDetail, error) {
	runID = strings.TrimSpace(runID)
	result, err := r.ProbeLogs(ctx, ProbeLogQuery{ID: runID, Page: 1, PageSize: 25})
	if err != nil {
		return ProbeLogDetail{}, err
	}
	if len(result.Items) == 0 {
		return ProbeLogDetail{}, sql.ErrNoRows
	}
	detail := ProbeLogDetail{ProbeLogItem: result.Items[0], Steps: []ProbeLogStep{}}
	rows, err := r.db.Query(ctx, `
		select step,status,latency_ms,http_status,coalesce(error_type,''),metadata,created_at
		from probe_results
		where probe_run_id=$1
		order by case step when 'parse_url' then 1 when 'dns' then 2 when 'tcp' then 3 when 'tls' then 4 when 'http' then 5 when 'models' then 6 when 'generate' then 7 else 8 end,created_at asc
	`, runID)
	if err != nil {
		return ProbeLogDetail{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var step ProbeLogStep
		var httpStatus sql.NullInt32
		var raw []byte
		if err := rows.Scan(&step.Step, &step.Status, &step.LatencyMs, &httpStatus, &step.ErrorType, &raw, &step.CreatedAt); err != nil {
			return ProbeLogDetail{}, err
		}
		if httpStatus.Valid {
			value := int(httpStatus.Int32)
			step.HTTPStatus = &value
		}
		metadata := map[string]any{}
		_ = json.Unmarshal(raw, &metadata)
		step.Metadata = safeProbeLogMetadata(metadata)
		detail.Steps = append(detail.Steps, step)
	}
	return detail, rows.Err()
}

func (r *Repository) ExportProbeLogs(ctx context.Context, query ProbeLogQuery, visit func(ProbeLogExportItem) error) error {
	whereSQL, args := probeLogWhere(query)
	rows, err := r.db.Query(ctx, probeLogBaseSQL+`
		select b.id,b.channel_id,b.channel_name,b.provider,b.type,b.model,b.endpoint,b.layer,b.source,b.status,b.run_status,b.step,b.http_status,b.error_type,b.latency_ms,b.step_count,b.started_at,b.finished_at,b.upstream_error_summary
		from base b where `+whereSQL+`
		order by b.started_at desc
	`, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		item, err := scanProbeLogExportItem(rows)
		if err != nil {
			return err
		}
		if err := visit(item); err != nil {
			return err
		}
	}
	return rows.Err()
}

func probeLogWhere(query ProbeLogQuery) (string, []any) {
	where := []string{"true"}
	args := []any{}
	addArg := func(value any) string {
		args = append(args, value)
		return fmt.Sprintf("$%d", len(args))
	}
	if value := strings.TrimSpace(query.ID); value != "" {
		where = append(where, "b.id="+addArg(value))
	}
	if !query.From.IsZero() {
		where = append(where, "b.started_at >= "+addArg(query.From))
	}
	if !query.To.IsZero() {
		where = append(where, "b.started_at <= "+addArg(query.To))
	}
	if value := strings.TrimSpace(query.Provider); value != "" {
		where = append(where, "b.provider ilike '%' || "+addArg(value)+" || '%'")
	}
	if value := strings.TrimSpace(query.ChannelID); value != "" {
		where = append(where, "b.channel_id="+addArg(value))
	}
	if value := strings.TrimSpace(query.Layer); value != "" {
		where = append(where, "b.layer="+addArg(value))
	}
	if value := strings.TrimSpace(query.Status); value != "" {
		where = append(where, "b.status="+addArg(value))
	}
	if query.HTTPStatus > 0 {
		where = append(where, "b.http_status="+addArg(query.HTTPStatus))
	}
	if value := strings.TrimSpace(query.ErrorType); value != "" {
		where = append(where, "b.error_type="+addArg(value))
	}
	if value := strings.TrimSpace(query.Source); value != "" {
		where = append(where, "b.source="+addArg(value))
	}
	if query.OnlyAbnormal {
		where = append(where, "b.status in ('warn','auth_error','down','failed')")
	}
	return strings.Join(where, " and "), args
}

func scanProbeLogItem(row rowScanner) (ProbeLogItem, error) {
	var item ProbeLogItem
	var httpStatus sql.NullInt32
	var finishedAt sql.NullTime
	err := row.Scan(&item.ID, &item.ChannelID, &item.ChannelName, &item.Provider, &item.Type, &item.Model, &item.Endpoint, &item.Layer, &item.Source, &item.Status, &item.RunStatus, &item.Step, &httpStatus, &item.ErrorType, &item.LatencyMs, &item.StepCount, &item.StartedAt, &finishedAt)
	if httpStatus.Valid {
		value := int(httpStatus.Int32)
		item.HTTPStatus = &value
	}
	if finishedAt.Valid {
		item.FinishedAt = &finishedAt.Time
	}
	return item, err
}

func scanProbeLogExportItem(row rowScanner) (ProbeLogExportItem, error) {
	var item ProbeLogExportItem
	var httpStatus sql.NullInt32
	var finishedAt sql.NullTime
	err := row.Scan(&item.ID, &item.ChannelID, &item.ChannelName, &item.Provider, &item.Type, &item.Model, &item.Endpoint, &item.Layer, &item.Source, &item.Status, &item.RunStatus, &item.Step, &httpStatus, &item.ErrorType, &item.LatencyMs, &item.StepCount, &item.StartedAt, &finishedAt, &item.UpstreamErrorSummary)
	if httpStatus.Valid {
		value := int(httpStatus.Int32)
		item.HTTPStatus = &value
	}
	if finishedAt.Valid {
		item.FinishedAt = &finishedAt.Time
	}
	return item, err
}

func safeProbeLogMetadata(metadata map[string]any) map[string]any {
	allowed := map[string]struct{}{
		"cert_expires_at": {}, "models_count": {}, "model_found": {}, "probe_mode": {}, "reason": {},
		"content_valid": {}, "content_error": {}, "first_token_ms": {}, "tokens_used": {},
		"tokens_per_second": {}, "usage_estimated": {}, "warn_threshold_ms": {}, "upstream_error_summary": {},
	}
	out := map[string]any{}
	for key, value := range metadata {
		if _, ok := allowed[key]; ok {
			out[key] = value
		}
	}
	return out
}
