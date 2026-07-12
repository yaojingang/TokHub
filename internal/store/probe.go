package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

var ErrProbeTargetNotFound = errors.New("probe target not found")

type ProbeTarget struct {
	ID             string
	OwnerType      string
	OwnerID        string
	OrgID          string
	Name           string
	Provider       string
	Type           string
	Endpoint       string
	Status         string
	Model          string
	ProviderConfig map[string]any
}

type ProbeRunSnapshot struct {
	ChannelName string
	Provider    string
	Type        string
	Endpoint    string
	Model       string
}

type ProbeStepResult struct {
	Step       string
	Status     string
	LatencyMs  int
	HTTPStatus int
	ErrorType  string
	Metadata   map[string]any
}

type ProbeLayerSummary struct {
	Status    string
	LatencyMs int
	ErrorType string
}

type ProbeScheduleTarget struct {
	ProbeTarget
	CreatedAt time.Time
	LastL1At  time.Time
	LastL2At  time.Time
	LastL3At  time.Time
}

func (t ProbeScheduleTarget) LastProbeAt(layer string) time.Time {
	switch layer {
	case "l1":
		return t.LastL1At
	case "l2":
		return t.LastL2At
	case "l3":
		return t.LastL3At
	default:
		return time.Time{}
	}
}

func (r *Repository) ProbeTargets(ctx context.Context) ([]ProbeTarget, error) {
	rows, err := r.db.Query(ctx, `
		select id,owner_type,coalesce(owner_id,''),
			case when owner_type='user' then coalesce(org_id,'org_' || coalesce(owner_id,'')) else '' end,
			name,provider,type,endpoint,status,model,coalesce(provider_config,'{}'::jsonb)
		from channels
		where owner_type='platform'
			and status not in ('disabled','deleted')
			and deleted_at is null
		order by name asc
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var targets []ProbeTarget
	for rows.Next() {
		var target ProbeTarget
		var providerConfigRaw []byte
		if err := rows.Scan(&target.ID, &target.OwnerType, &target.OwnerID, &target.OrgID, &target.Name, &target.Provider, &target.Type, &target.Endpoint, &target.Status, &target.Model, &providerConfigRaw); err != nil {
			return nil, err
		}
		target.ProviderConfig = decodeMap(providerConfigRaw)
		targets = append(targets, target)
	}
	return targets, rows.Err()
}

func (r *Repository) ProbeScheduleTargets(ctx context.Context) ([]ProbeScheduleTarget, error) {
	rows, err := r.db.Query(ctx, `
			select
				c.id,c.owner_type,coalesce(c.owner_id,''),
				case when c.owner_type='user' then coalesce(c.org_id,'org_' || coalesce(c.owner_id,'')) else '' end,
				c.name,c.provider,c.type,c.endpoint,c.status,c.model,coalesce(c.provider_config,'{}'::jsonb),
				coalesce(c.created_at,now()) as created_at,
				coalesce(max(pr.started_at) filter (where pr.layer='l1'), 'epoch'::timestamptz) as last_l1_at,
				coalesce(max(pr.started_at) filter (where pr.layer='l2'), 'epoch'::timestamptz) as last_l2_at,
			coalesce(max(pr.started_at) filter (where pr.layer='l3'), 'epoch'::timestamptz) as last_l3_at
		from channels c
		left join probe_runs pr on pr.channel_id=c.id and pr.layer in ('l1','l2','l3')
		where c.owner_type='platform'
			and c.status not in ('disabled','deleted')
			and c.deleted_at is null
			group by c.id,c.owner_type,c.owner_id,c.org_id,c.name,c.provider,c.type,c.endpoint,c.status,c.model,c.provider_config,c.created_at
			order by c.name asc
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var targets []ProbeScheduleTarget
	for rows.Next() {
		var target ProbeScheduleTarget
		var providerConfigRaw []byte
		if err := rows.Scan(
			&target.ID,
			&target.OwnerType,
			&target.OwnerID,
			&target.OrgID,
			&target.Name,
			&target.Provider,
			&target.Type,
			&target.Endpoint,
			&target.Status,
			&target.Model,
			&providerConfigRaw,
			&target.CreatedAt,
			&target.LastL1At,
			&target.LastL2At,
			&target.LastL3At,
		); err != nil {
			return nil, err
		}
		target.ProviderConfig = decodeMap(providerConfigRaw)
		targets = append(targets, target)
	}
	return targets, rows.Err()
}

func (r *Repository) ProbeTarget(ctx context.Context, channelID string) (ProbeTarget, error) {
	var target ProbeTarget
	var providerConfigRaw []byte
	err := r.db.QueryRow(ctx, `
		select id,owner_type,coalesce(owner_id,''),
			case when owner_type='user' then coalesce(org_id,'org_' || coalesce(owner_id,'')) else '' end,
			name,provider,type,endpoint,status,model,coalesce(provider_config,'{}'::jsonb)
		from channels
		where id=$1 and status <> 'deleted' and deleted_at is null
	`, channelID).Scan(&target.ID, &target.OwnerType, &target.OwnerID, &target.OrgID, &target.Name, &target.Provider, &target.Type, &target.Endpoint, &target.Status, &target.Model, &providerConfigRaw)
	if errors.Is(err, pgx.ErrNoRows) {
		return ProbeTarget{}, fmt.Errorf("%w: %s", ErrProbeTargetNotFound, channelID)
	}
	target.ProviderConfig = decodeMap(providerConfigRaw)
	return target, err
}

func (r *Repository) UpsertProbeRun(ctx context.Context, runID string, channelID string, layer string, source string, snapshot ProbeRunSnapshot) error {
	metadata := mapJSON(map[string]any{
		"channel_name": snapshot.ChannelName,
		"provider":     snapshot.Provider,
		"type":         snapshot.Type,
		"endpoint":     snapshot.Endpoint,
		"model":        snapshot.Model,
	})
	_, err := r.db.Exec(ctx, `
		insert into probe_runs(id,channel_id,layer,source,status,metadata,started_at)
		values($1,$2,$3,$4,'running',$5,now())
		on conflict(id) do update set source=excluded.source,metadata=excluded.metadata
	`, runID, channelID, layer, source, metadata)
	return err
}

func (r *Repository) UpsertProbeResults(ctx context.Context, runID string, channelID string, layer string, results []ProbeStepResult) error {
	for _, result := range results {
		meta, err := json.Marshal(result.Metadata)
		if err != nil {
			return err
		}
		var httpStatus any
		if result.HTTPStatus > 0 {
			httpStatus = result.HTTPStatus
		}
		var errorType any
		if result.ErrorType != "" {
			errorType = result.ErrorType
		}
		_, err = r.db.Exec(ctx, `
			insert into probe_results(id,probe_run_id,channel_id,layer,step,status,latency_ms,http_status,error_type,metadata,created_at)
			values($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,now())
			on conflict(probe_run_id,layer,step) do update set
				status=excluded.status,
				latency_ms=excluded.latency_ms,
				http_status=excluded.http_status,
				error_type=excluded.error_type,
				metadata=excluded.metadata
		`, "prs_"+uuid.NewString(), runID, channelID, layer, result.Step, result.Status, result.LatencyMs, httpStatus, errorType, meta)
		if err != nil {
			return err
		}
	}
	return nil
}

func (r *Repository) CompleteProbeRun(ctx context.Context, runID string, status string, errorType string) error {
	var errValue any
	if errorType != "" {
		errValue = errorType
	}
	_, err := r.db.Exec(ctx, `
		update probe_runs
		set status=$2, error_type=$3, finished_at=now()
		where id=$1
	`, runID, status, errValue)
	return err
}

func (r *Repository) LatestLayerSummary(ctx context.Context, channelID string, layer string) (ProbeLayerSummary, error) {
	rows, err := r.db.Query(ctx, `
		select status,latency_ms,coalesce(error_type,'')
		from probe_results
		where channel_id=$1 and layer=$2
			and probe_run_id=(
				select pr.id from probe_runs pr
				where pr.channel_id=$1 and pr.layer=$2
					and pr.status <> 'running'
					and pr.finished_at is not null
					and exists (
						select 1 from probe_results latest
						where latest.probe_run_id=pr.id and latest.layer=pr.layer
					)
				order by pr.started_at desc
				limit 1
			)
		order by created_at asc
	`, channelID, layer)
	if err != nil {
		return ProbeLayerSummary{}, err
	}
	defer rows.Close()

	summary := ProbeLayerSummary{Status: "na"}
	var total int
	var firstErrType string
	for rows.Next() {
		var status, errType string
		var latency int
		if err := rows.Scan(&status, &latency, &errType); err != nil {
			return ProbeLayerSummary{}, err
		}
		total += latency
		if firstErrType == "" && errType != "" {
			firstErrType = errType
		}
		if status == "auth_error" {
			return ProbeLayerSummary{Status: "auth_error", LatencyMs: total, ErrorType: "auth_error"}, nil
		}
		if status == "down" {
			return ProbeLayerSummary{Status: "down", LatencyMs: total, ErrorType: errType}, nil
		}
		if status == "warn" {
			return ProbeLayerSummary{Status: "warn", LatencyMs: total, ErrorType: errType}, nil
		}
		if status == "ok" && summary.Status != "down" {
			summary.Status = "ok"
		}
	}
	if err := rows.Err(); err != nil {
		return ProbeLayerSummary{}, err
	}
	summary.LatencyMs = total
	summary.ErrorType = firstErrType
	return summary, nil
}

func (r *Repository) ApplyProbeStatus(ctx context.Context, channelID string, decisionStatus string, errorType string, l1 ProbeLayerSummary, l2 ProbeLayerSummary) error {
	return r.ApplyProbeStatusWithL3(ctx, channelID, decisionStatus, errorType, l1, l2, ProbeLayerSummary{Status: "na"})
}

func (r *Repository) ApplyProbeStatusWithL3(ctx context.Context, channelID string, decisionStatus string, errorType string, l1 ProbeLayerSummary, l2 ProbeLayerSummary, l3 ProbeLayerSummary) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var oldStatus string
	var deletedAt sql.NullTime
	if err := tx.QueryRow(ctx, `select status,deleted_at from channels where id=$1 for update`, channelID).Scan(&oldStatus, &deletedAt); err != nil {
		return err
	}
	if oldStatus == "disabled" || oldStatus == "deleted" || deletedAt.Valid {
		return tx.Commit(ctx)
	}

	latest := struct {
		successRate float64
		l3Status    string
		l3Latency   int
		tokens      int
		cost        float64
	}{
		successRate: 0,
		l3Status:    "na",
	}
	_ = tx.QueryRow(ctx, `
		select success_rate,l3_status,l3_latency_ms,tokens_used,cost_usd
		from channel_status_snapshots
		where channel_id=$1
		order by sampled_at desc
		limit 1
	`, channelID).Scan(&latest.successRate, &latest.l3Status, &latest.l3Latency, &latest.tokens, &latest.cost)

	if l3.Status != "" && l3.Status != "na" {
		latest.l3Status = l3.Status
		latest.l3Latency = l3.LatencyMs
		tokens, cost := latestL3MetricsTx(ctx, tx, channelID)
		latest.tokens = tokens
		latest.cost = cost
		latest.successRate = successRateForProbeDecision(decisionStatus, latest.successRate)
	} else if l3.Status == "na" && l3.ErrorType == "l3_probe_skipped" {
		latest.l3Status = l3.Status
		latest.l3Latency = 0
		latest.tokens = 0
		latest.cost = 0
		latest.successRate = successRateForProbeDecision(decisionStatus, latest.successRate)
	}

	score := scoreForStatus(decisionStatus, latest.successRate)
	uptime := uptimeForStatus(decisionStatus)
	latency := l2.LatencyMs
	if latency == 0 {
		latency = l1.LatencyMs
	}
	if _, err := tx.Exec(ctx, `
		update channels
		set status=$2, score=$3, updated_at=now()
		where id=$1
	`, channelID, decisionStatus, score); err != nil {
		return err
	}

	var errValue any
	if errorType != "" {
		errValue = errorType
	}
	if _, err := tx.Exec(ctx, `
		insert into channel_status_snapshots(
			id,channel_id,sampled_at,status,score,uptime_24h,success_rate,latency_p95_ms,
			l1_status,l2_status,l3_status,l1_latency_ms,l2_latency_ms,l3_latency_ms,
			tokens_used,cost_usd,error_type,metadata
		)
		values($1,$2,now(),$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,'{"source":"phase3_probe"}'::jsonb)
	`, fmt.Sprintf("snap_probe_%s_%d", channelID, time.Now().UnixNano()), channelID, decisionStatus, score, uptime, latest.successRate, latency,
		l1.Status, l2.Status, latest.l3Status, l1.LatencyMs, l2.LatencyMs, latest.l3Latency, latest.tokens, latest.cost, errValue); err != nil {
		return err
	}

	if oldStatus != decisionStatus {
		orgID, err := channelOrgIDTx(ctx, tx, channelID)
		if err != nil {
			return err
		}
		if decisionStatus == "healthy" {
			resolvedIDs, err := resolveIncidentIDs(ctx, tx, `update incidents set resolved_at=now() where channel_id=$1 and resolved_at is null and deleted_at is null returning id`, channelID)
			if err != nil {
				return err
			}
			for _, incidentID := range resolvedIDs {
				if err := insertIncidentEventTx(ctx, tx, incidentID, orgID, "resolved", "Channel recovered to healthy", map[string]any{"old_status": oldStatus, "new_status": decisionStatus}); err != nil {
					return err
				}
			}
		} else {
			manualIncidentID := ""
			err := tx.QueryRow(ctx, `
				select id from incidents
				where channel_id=$1 and status='manual' and resolved_at is null and deleted_at is null
				order by opened_at desc
				limit 1
			`, channelID).Scan(&manualIncidentID)
			if err != nil && !errors.Is(err, pgx.ErrNoRows) {
				return err
			}
			if manualIncidentID != "" {
				if err := insertIncidentEventTx(ctx, tx, manualIncidentID, orgID, "status_changed", "Channel status changed to "+decisionStatus, map[string]any{"old_status": oldStatus, "new_status": decisionStatus, "error_type": errorType}); err != nil {
					return err
				}
			} else {
				resolvedIDs, err := resolveIncidentIDs(ctx, tx, `update incidents set resolved_at=now() where channel_id=$1 and status<>$2 and resolved_at is null and deleted_at is null returning id`, channelID, decisionStatus)
				if err != nil {
					return err
				}
				for _, incidentID := range resolvedIDs {
					if err := insertIncidentEventTx(ctx, tx, incidentID, orgID, "resolved", "Incident closed by status change to "+decisionStatus, map[string]any{"old_status": oldStatus, "new_status": decisionStatus, "error_type": errorType}); err != nil {
						return err
					}
				}

				incidentID := "inc_" + uuid.NewString()
				err = tx.QueryRow(ctx, `
				insert into incidents(id,channel_id,status,title,opened_at,metadata)
				select $1,$2,$3,$4,now(),$5
				where not exists (
					select 1 from incidents
					where channel_id=$2 and status=$3 and resolved_at is null and deleted_at is null
				)
				returning id
			`, incidentID, channelID, decisionStatus, "Channel status changed to "+decisionStatus, mapJSON(map[string]any{"old_status": oldStatus, "error_type": errorType})).Scan(&incidentID)
				if errors.Is(err, pgx.ErrNoRows) {
					err = tx.QueryRow(ctx, `select id from incidents where channel_id=$1 and status=$2 and resolved_at is null and deleted_at is null order by opened_at desc limit 1`, channelID, decisionStatus).Scan(&incidentID)
				}
				if err != nil {
					return err
				}
				if err := insertIncidentEventTx(ctx, tx, incidentID, orgID, "opened", "Incident opened: "+decisionStatus, map[string]any{"old_status": oldStatus, "new_status": decisionStatus, "error_type": errorType}); err != nil {
					return err
				}
			}
		}
		if _, err := tx.Exec(ctx, `
			insert into audit_events(id,actor_type,actor_id,action,object_type,object_id,result,metadata,created_at)
			values($1,'system',null,'probe.status.changed','channel',$2,'success',$3,now())
		`, "aud_"+uuid.NewString(), channelID, mapJSON(map[string]any{"old_status": oldStatus, "new_status": decisionStatus, "error_type": errorType})); err != nil {
			return err
		}
	}

	return tx.Commit(ctx)
}

func channelOrgIDTx(ctx context.Context, tx pgx.Tx, channelID string) (string, error) {
	var orgID string
	err := tx.QueryRow(ctx, `
		select case when owner_type='user' then coalesce(org_id,'org_' || coalesce(owner_id,'')) else $2::text end
		from channels
		where id=$1
	`, channelID, DefaultOrgID).Scan(&orgID)
	return orgID, err
}

func resolveIncidentIDs(ctx context.Context, tx pgx.Tx, query string, args ...any) ([]string, error) {
	rows, err := tx.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func insertIncidentEventTx(ctx context.Context, tx pgx.Tx, incidentID string, orgID string, eventType string, message string, metadata map[string]any) error {
	_, err := tx.Exec(ctx, `
		insert into incident_events(id,incident_id,org_id,event_type,message,metadata,created_at)
		values($1,$2,$3,$4,$5,$6,now())
	`, "ine_"+uuid.NewString(), incidentID, orgID, eventType, message, mapJSON(metadata))
	return err
}

type queryer interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

func latestL3MetricsTx(ctx context.Context, q queryer, channelID string) (int, float64) {
	var tokens int
	var cost float64
	_ = q.QueryRow(ctx, `
		select
			coalesce(nullif(metadata->>'tokens_used','')::int,0) as tokens,
			round((coalesce(nullif(metadata->>'tokens_used','')::numeric,0) / 1000000 * 5.2)::numeric, 6)::float8 as cost
		from probe_results
		where channel_id=$1 and layer='l3'
		order by created_at desc
		limit 1
	`, channelID).Scan(&tokens, &cost)
	return tokens, cost
}

func (r *Repository) ProbeResultCount(ctx context.Context, runID string) (int, error) {
	var count int
	err := r.db.QueryRow(ctx, `select count(*) from probe_results where probe_run_id=$1`, runID).Scan(&count)
	return count, err
}

func scoreForStatus(status string, fallbackSuccess float64) int {
	switch status {
	case "healthy":
		if fallbackSuccess > 0 {
			return minInt(99, maxInt(80, int(fallbackSuccess)))
		}
		return 92
	case "auth_error":
		return 70
	case "degraded":
		return 72
	case "functional_down":
		return 42
	case "connectivity_down":
		return 35
	default:
		return 50
	}
}

func successRateForProbeDecision(status string, fallback float64) float64 {
	switch status {
	case "healthy":
		return 100
	case "degraded", "auth_error":
		return 75
	case "functional_down", "connectivity_down":
		return 0
	default:
		return fallback
	}
}

func uptimeForStatus(status string) float64 {
	switch status {
	case "healthy":
		return 99.5
	case "auth_error":
		return 98.0
	case "degraded":
		return 96.0
	case "functional_down":
		return 88.0
	case "connectivity_down":
		return 55.0
	default:
		return 0
	}
}

func mapJSON(value map[string]any) string {
	raw, _ := json.Marshal(value)
	return string(raw)
}

func IsNoRows(err error) bool {
	return err == pgx.ErrNoRows
}
