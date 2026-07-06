package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type ChannelFilter struct {
	Provider string
	Status   string
	Query    string
	Page     int
	PageSize int
}

type PublicOverview struct {
	Total             int       `json:"total"`
	Healthy           int       `json:"healthy"`
	FunctionalDown    int       `json:"functionalDown"`
	ConnectivityDown  int       `json:"connectivityDown"`
	Degraded          int       `json:"degraded"`
	Unknown           int       `json:"unknown"`
	HealthyRate       float64   `json:"healthyRate"`
	P95LatencySeconds float64   `json:"p95LatencySeconds"`
	AverageLatencyMs  int       `json:"averageLatencyMs"`
	SlowRate          float64   `json:"slowRate"`
	ProbeCostToday    float64   `json:"probeCostToday"`
	ProbeTokensToday  int       `json:"probeTokensToday"`
	ProbeRunsToday    int       `json:"probeRunsToday"`
	UpdatedAt         time.Time `json:"updatedAt"`
}

type PublicChannel struct {
	ID              string     `json:"id"`
	PublicSlug      string     `json:"publicSlug"`
	Name            string     `json:"name"`
	Provider        string     `json:"provider"`
	Type            string     `json:"type"`
	Model           string     `json:"model"`
	UpstreamModel   string     `json:"upstreamModel"`
	Endpoint        string     `json:"endpoint"`
	OfficialSiteURL string     `json:"officialSiteUrl"`
	IntroTitle      string     `json:"introTitle"`
	IntroSummary    string     `json:"introSummary"`
	IntroBody       string     `json:"introBody"`
	IntroHighlights []string   `json:"introHighlights"`
	LogoURL         string     `json:"logoUrl"`
	IntroSourceURL  string     `json:"introSourceUrl"`
	IntroUpdatedAt  *time.Time `json:"introUpdatedAt,omitempty"`
	Status          string     `json:"status"`
	StatusLabel     string     `json:"statusLabel"`
	Diagnosis       Diagnosis  `json:"diagnosis"`
	Score           int        `json:"score"`
	Uptime24h       float64    `json:"uptime24h"`
	SuccessRate     float64    `json:"successRate"`
	LatencyP95Ms    int        `json:"latencyP95Ms"`
	L1Status        string     `json:"l1Status"`
	L2Status        string     `json:"l2Status"`
	L3Status        string     `json:"l3Status"`
	L1LatencyMs     int        `json:"l1LatencyMs"`
	L2LatencyMs     int        `json:"l2LatencyMs"`
	L3LatencyMs     int        `json:"l3LatencyMs"`
	TokensUsed      int        `json:"tokensUsed"`
	CostUSD         float64    `json:"costUsd"`
	InputPerMTok    float64    `json:"inputPerMtok"`
	OutputPerMTok   float64    `json:"outputPerMtok"`
	ErrorType       string     `json:"errorType,omitempty"`
	LastProbeAt     time.Time  `json:"lastProbeAt"`
	Trend           []int      `json:"trend"`
	Mark            string     `json:"mark"`
}

type Diagnosis struct {
	Code     string `json:"code"`
	Label    string `json:"label"`
	Severity string `json:"severity"`
	Hint     string `json:"hint,omitempty"`
}

type PublicChannelList struct {
	Items    []PublicChannel `json:"items"`
	Total    int             `json:"total"`
	Page     int             `json:"page"`
	PageSize int             `json:"pageSize"`
}

type SeriesPoint struct {
	Date           string  `json:"date"`
	HealthIndex    float64 `json:"healthIndex"`
	SuccessRate    float64 `json:"successRate"`
	LatencyOpenMs  int     `json:"latencyOpenMs"`
	LatencyCloseMs int     `json:"latencyCloseMs"`
	LatencyHighMs  int     `json:"latencyHighMs"`
	LatencyLowMs   int     `json:"latencyLowMs"`
	ProbeCount     int     `json:"probeCount"`
	CostUSD        float64 `json:"costUsd"`
}

type ProbeLayer struct {
	Name      string `json:"name"`
	Status    string `json:"status"`
	LatencyMs int    `json:"latencyMs"`
}

type L3Stats struct {
	SuccessRate      float64 `json:"successRate"`
	ContentValidRate float64 `json:"contentValidRate"`
	FirstTokenMs     int     `json:"firstTokenMs"`
	TokensPerSecond  int     `json:"tokensPerSecond"`
	AverageTokens    int     `json:"averageTokens"`
	QuotaClass       string  `json:"quotaClass"`
}

type ProbeRecord struct {
	Time      time.Time `json:"time"`
	Layer     string    `json:"layer"`
	Type      string    `json:"type"`
	HTTPCode  int       `json:"httpCode"`
	LatencyMs int       `json:"latencyMs"`
	Result    string    `json:"result"`
	ErrorType string    `json:"errorType,omitempty"`
}

type ErrorBucket struct {
	Type  string `json:"type"`
	Label string `json:"label"`
	Count int    `json:"count"`
}

type CostPoint struct {
	Date    string  `json:"date"`
	CostUSD float64 `json:"costUsd"`
	Tokens  int     `json:"tokens"`
}

type PublicChannelDetail struct {
	Channel       PublicChannel `json:"channel"`
	Layers        []ProbeLayer  `json:"layers"`
	L3            L3Stats       `json:"l3"`
	RecentRecords []ProbeRecord `json:"recentRecords"`
	Errors        []ErrorBucket `json:"errors"`
	Costs         []CostPoint   `json:"costs"`
}

type ProviderRank struct {
	Provider     string  `json:"provider"`
	Channels     int     `json:"channels"`
	Healthy      int     `json:"healthy"`
	SuccessRate  float64 `json:"successRate"`
	LatencyP95Ms int     `json:"latencyP95Ms"`
	Score        int     `json:"score"`
}

type ErrorSummary struct {
	Type  string `json:"type"`
	Label string `json:"label"`
	Count int    `json:"count"`
}

func (r *Repository) PublicOverview(ctx context.Context) (PublicOverview, error) {
	list, err := r.allPublicChannels(ctx)
	if err != nil {
		return PublicOverview{}, err
	}
	var o PublicOverview
	o.Total = len(list)
	if o.Total == 0 {
		o.UpdatedAt = time.Now()
		return o, nil
	}
	var latencyTotal int
	var latencyCount int
	latencies := []int{}
	for _, ch := range list {
		switch ch.Status {
		case "healthy":
			o.Healthy++
		case "functional_down", "auth_error":
			o.FunctionalDown++
		case "connectivity_down":
			o.ConnectivityDown++
		case "degraded":
			o.Degraded++
		default:
			o.Unknown++
		}
		if ch.LatencyP95Ms > 0 {
			latencyTotal += ch.LatencyP95Ms
			latencyCount++
			latencies = append(latencies, ch.LatencyP95Ms)
		}
		if ch.LatencyP95Ms > 2500 {
			o.SlowRate += 1
		}
		if ch.LastProbeAt.After(o.UpdatedAt) {
			o.UpdatedAt = ch.LastProbeAt
		}
	}
	tokens, cost, err := r.publicProbeUsage(ctx, "", time.Time{})
	if err != nil {
		return PublicOverview{}, err
	}
	o.ProbeTokensToday = tokens
	o.ProbeCostToday = round3(cost)
	if err := r.db.QueryRow(ctx, `
		select count(*)
		from probe_runs pr
		join channels c on c.id=pr.channel_id
		where `+publicChannelWhereClause("c")+`
			and pr.started_at >= current_date
	`).Scan(&o.ProbeRunsToday); err != nil {
		return PublicOverview{}, err
	}
	o.HealthyRate = round1(float64(o.Healthy) / float64(o.Total) * 100)
	if latencyCount > 0 {
		o.AverageLatencyMs = latencyTotal / latencyCount
		sort.Ints(latencies)
		idx := int(math.Ceil(float64(len(latencies))*0.95)) - 1
		if idx < 0 {
			idx = 0
		}
		o.P95LatencySeconds = float64(latencies[idx]) / 1000
	}
	o.SlowRate = round1(o.SlowRate / float64(o.Total) * 100)
	return o, nil
}

func (r *Repository) PublicChannels(ctx context.Context, filter ChannelFilter) (PublicChannelList, error) {
	filter = normalizeChannelFilter(filter)
	where, args := channelWhere(filter)

	var total int
	countSQL := `select count(*) from channels c where ` + strings.Join(where, " and ")
	if err := r.db.QueryRow(ctx, countSQL, args...).Scan(&total); err != nil {
		return PublicChannelList{}, err
	}

	args = append(args, filter.PageSize, (filter.Page-1)*filter.PageSize)
	query := publicChannelSQL(`where `+strings.Join(where, " and ")+" order by c.score desc, c.name asc limit $%d offset $%d", len(args)-1, len(args))
	rows, err := r.db.Query(ctx, query, args...)
	if err != nil {
		return PublicChannelList{}, err
	}
	defer rows.Close()

	items, err := scanPublicChannels(rows)
	if err != nil {
		return PublicChannelList{}, err
	}
	return PublicChannelList{Items: items, Total: total, Page: filter.Page, PageSize: filter.PageSize}, nil
}

func (r *Repository) PublicChannel(ctx context.Context, channelID string) (PublicChannelDetail, error) {
	resolvedID, err := r.resolvePublicChannelID(ctx, channelID)
	if err != nil {
		return PublicChannelDetail{}, err
	}
	rows, err := r.db.Query(ctx, publicChannelSQL("where "+publicChannelWhereClause("c")+" and c.id=$1", 0, 0), resolvedID)
	if err != nil {
		return PublicChannelDetail{}, err
	}
	defer rows.Close()
	channels, err := scanPublicChannels(rows)
	if err != nil {
		return PublicChannelDetail{}, err
	}
	if len(channels) == 0 {
		return PublicChannelDetail{}, pgx.ErrNoRows
	}
	ch := channels[0]
	layers, err := r.publicProbeLayers(ctx, resolvedID)
	if err != nil {
		return PublicChannelDetail{}, err
	}
	l3, err := r.publicL3Stats(ctx, resolvedID)
	if err != nil {
		return PublicChannelDetail{}, err
	}
	records, err := r.publicProbeRecords(ctx, resolvedID)
	if err != nil {
		return PublicChannelDetail{}, err
	}
	errors, err := r.publicErrors(ctx, resolvedID)
	if err != nil {
		return PublicChannelDetail{}, err
	}
	costs, err := r.publicCosts(ctx, resolvedID, 7)
	if err != nil {
		return PublicChannelDetail{}, err
	}
	return PublicChannelDetail{
		Channel:       ch,
		Layers:        layers,
		L3:            l3,
		RecentRecords: records,
		Errors:        errors,
		Costs:         costs,
	}, nil
}

func (r *Repository) PublicChannelSeries(ctx context.Context, channelID string, days int) ([]SeriesPoint, error) {
	if days != 7 && days != 30 && days != 90 {
		days = 30
	}
	resolvedID, err := r.resolvePublicChannelID(ctx, channelID)
	if err != nil {
		return nil, err
	}
	type dayAgg struct {
		health  float64
		success float64
		open    int
		close   int
		high    int
		low     int
		probes  int
		cost    float64
	}
	byDay := map[string]*dayAgg{}
	rows, err := r.db.Query(ctx, `
		with recent as (
			select sampled_at::date as day,sampled_at,score,success_rate,latency_p95_ms
			from channel_status_snapshots
			where channel_id=$1 and sampled_at >= current_date - ($2::int - 1) * interval '1 day'
		),
		snap as (
			select day,
				avg(score)::float8 as health,
				avg(success_rate)::float8 as success,
				(array_agg(latency_p95_ms order by sampled_at asc))[1] as open_latency,
				(array_agg(latency_p95_ms order by sampled_at desc))[1] as close_latency,
				max(latency_p95_ms) as high_latency,
				min(latency_p95_ms) as low_latency
			from recent
			group by day
		),
		runs as (
			select started_at::date as day,count(*)::int as probes
			from probe_runs
			where channel_id=$1 and started_at >= current_date - ($2::int - 1) * interval '1 day'
			group by started_at::date
		),
		costs as (
			select day,sum(cost)::float8 as cost
			from (
				select created_at::date as day,
					round((sum(case when metadata->>'tokens_used' ~ '^[0-9]+$' then (metadata->>'tokens_used')::numeric else 0 end) / 1000000 * 5.2)::numeric, 6)::float8 as cost
				from probe_results
				where channel_id=$1 and layer='l3' and created_at >= current_date - ($2::int - 1) * interval '1 day'
				group by created_at::date
				union all
				select created_at::date as day,sum(cost_usd)::float8 as cost
				from gateway_request_events
				where upstream_channel_id=$1 and created_at >= current_date - ($2::int - 1) * interval '1 day'
				group by created_at::date
			) u
			group by day
		)
		select coalesce(s.day,r.day,c.day),coalesce(s.health,0),coalesce(s.success,0),
			coalesce(s.open_latency,0),coalesce(s.close_latency,0),coalesce(s.high_latency,0),coalesce(s.low_latency,0),
			coalesce(r.probes,0),coalesce(c.cost,0)
		from snap s
		full join runs r on r.day=s.day
		full join costs c on c.day=coalesce(s.day,r.day)
		order by 1 asc
	`, resolvedID, days)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var day time.Time
		item := dayAgg{}
		if err := rows.Scan(&day, &item.health, &item.success, &item.open, &item.close, &item.high, &item.low, &item.probes, &item.cost); err != nil {
			return nil, err
		}
		byDay[day.Format("2006-01-02")] = &item
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	points := make([]SeriesPoint, 0, days)
	now := time.Now()
	for i := days - 1; i >= 0; i-- {
		key := now.AddDate(0, 0, -i).Format("2006-01-02")
		item := byDay[key]
		if item == nil {
			points = append(points, SeriesPoint{Date: key})
			continue
		}
		points = append(points, SeriesPoint{
			Date:           key,
			HealthIndex:    round1(item.health),
			SuccessRate:    round1(item.success),
			LatencyOpenMs:  item.open,
			LatencyCloseMs: item.close,
			LatencyHighMs:  item.high,
			LatencyLowMs:   item.low,
			ProbeCount:     item.probes,
			CostUSD:        round3(item.cost),
		})
	}
	return points, nil
}

func (r *Repository) PublicProviderRank(ctx context.Context) ([]ProviderRank, error) {
	return r.PublicProviderRankForRange(ctx, 0)
}

func (r *Repository) PublicProviderRankForRange(ctx context.Context, days int) ([]ProviderRank, error) {
	if days <= 0 {
		return r.publicProviderRankFromLatestSnapshots(ctx)
	}
	rows, err := r.db.Query(ctx, `
		select c.provider,
			count(distinct c.id)::int,
			count(distinct c.id) filter (where c.status='healthy')::int,
				coalesce(
					case when count(pr.id) > 0
						then avg(case when pr.id is not null then case when pr.status='ok' then 100.0 else 0.0 end end)
						else avg(coalesce(s.success_rate,0))
					end,
					0
				)::float8,
			coalesce(
				case when count(pr.id) > 0
					then avg(nullif(pr.latency_ms,0))
					else avg(coalesce(s.latency_p95_ms,0))
				end,
				0
			)::int,
			coalesce(avg(c.score),0)::int
		from channels c
		left join lateral (
			select * from channel_status_snapshots ss where ss.channel_id=c.id order by sampled_at desc limit 1
		) s on true
		left join probe_results pr on pr.channel_id=c.id
			and pr.created_at >= now() - ($1::int * interval '1 day')
			and pr.layer='l3'
		where `+publicChannelWhereClause("c")+`
		group by c.provider
		order by 6 desc,1 asc
	`, days)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ProviderRank{}
	for rows.Next() {
		var item ProviderRank
		if err := rows.Scan(&item.Provider, &item.Channels, &item.Healthy, &item.SuccessRate, &item.LatencyP95Ms, &item.Score); err != nil {
			return nil, err
		}
		item.SuccessRate = round1(item.SuccessRate)
		out = append(out, item)
	}
	return out, rows.Err()
}

func (r *Repository) publicProviderRankFromLatestSnapshots(ctx context.Context) ([]ProviderRank, error) {
	list, err := r.allPublicChannels(ctx)
	if err != nil {
		return nil, err
	}
	type agg struct {
		count, healthy, latency, score int
		success                        float64
	}
	groups := map[string]*agg{}
	for _, ch := range list {
		a := groups[ch.Provider]
		if a == nil {
			a = &agg{}
			groups[ch.Provider] = a
		}
		a.count++
		if ch.Status == "healthy" {
			a.healthy++
		}
		a.latency += ch.LatencyP95Ms
		a.score += ch.Score
		a.success += ch.SuccessRate
	}
	out := make([]ProviderRank, 0, len(groups))
	for provider, a := range groups {
		out = append(out, ProviderRank{
			Provider:     provider,
			Channels:     a.count,
			Healthy:      a.healthy,
			SuccessRate:  round1(a.success / float64(a.count)),
			LatencyP95Ms: a.latency / a.count,
			Score:        a.score / a.count,
		})
	}
	for i := 0; i < len(out); i++ {
		for j := i + 1; j < len(out); j++ {
			if out[j].Score > out[i].Score {
				out[i], out[j] = out[j], out[i]
			}
		}
	}
	return out, nil
}

func (r *Repository) PublicErrorsSummary(ctx context.Context) ([]ErrorSummary, error) {
	return r.PublicErrorsSummaryForRange(ctx, 7)
}

func (r *Repository) PublicErrorsSummaryForRange(ctx context.Context, days int) ([]ErrorSummary, error) {
	rows, err := r.db.Query(ctx, `
		with real_failures as (
			select lower(trim(coalesce(pr.error_type,''))) as error_type
			from probe_results pr
			join channels c on c.id=pr.channel_id
			where `+publicChannelWhereClause("c")+`
				and ($1::int <= 0 or pr.created_at >= now() - ($1::int * interval '1 day'))
				and pr.status <> 'ok'
				and coalesce(pr.error_type,'') <> ''
		)
		select error_type,count(*)::int
		from real_failures
		group by error_type
		order by count(*) desc,error_type asc
		limit 12
	`, days)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ErrorSummary{}
	for rows.Next() {
		var typ string
		var count int
		if err := rows.Scan(&typ, &count); err != nil {
			return nil, err
		}
		out = append(out, ErrorSummary{Type: typ, Label: errorLabel(typ), Count: count})
	}
	return out, rows.Err()
}

func (r *Repository) allPublicChannels(ctx context.Context) ([]PublicChannel, error) {
	rows, err := r.db.Query(ctx, publicChannelSQL("where "+publicChannelWhereClause("c")+" order by c.score desc, c.name asc", 0, 0))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanPublicChannels(rows)
}

func seedSnapshots(ctx context.Context, db *pgxpool.Pool) error {
	rows, err := db.Query(ctx, `select id,status,score from channels where owner_type='platform' and status <> 'deleted' and deleted_at is null`)
	if err != nil {
		return err
	}
	defer rows.Close()

	type row struct {
		id     string
		status string
		score  int
	}
	var channels []row
	for rows.Next() {
		var ch row
		if err := rows.Scan(&ch.id, &ch.status, &ch.score); err != nil {
			return err
		}
		channels = append(channels, ch)
	}
	if err := rows.Err(); err != nil {
		return err
	}

	for _, ch := range channels {
		metrics := baselineMetrics(ch.id, ch.status, ch.score)
		_, err := db.Exec(ctx, `
			insert into channel_status_snapshots(
				id,channel_id,sampled_at,status,score,uptime_24h,success_rate,latency_p95_ms,
				l1_status,l2_status,l3_status,l1_latency_ms,l2_latency_ms,l3_latency_ms,
				tokens_used,cost_usd,error_type,metadata
			)
			values($1,$2,now(),$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,'{"source":"phase2_seed"}'::jsonb)
			on conflict(id) do update set
				sampled_at=excluded.sampled_at,
				status=excluded.status,
				score=excluded.score,
				uptime_24h=excluded.uptime_24h,
				success_rate=excluded.success_rate,
				latency_p95_ms=excluded.latency_p95_ms,
				l1_status=excluded.l1_status,
				l2_status=excluded.l2_status,
				l3_status=excluded.l3_status,
				l1_latency_ms=excluded.l1_latency_ms,
				l2_latency_ms=excluded.l2_latency_ms,
				l3_latency_ms=excluded.l3_latency_ms,
				tokens_used=excluded.tokens_used,
				cost_usd=excluded.cost_usd,
				error_type=excluded.error_type
		`, "snap_seed_"+ch.id, ch.id, ch.status, ch.score, metrics.uptime, metrics.success, metrics.latency,
			metrics.l1Status, metrics.l2Status, metrics.l3Status, metrics.l1Latency, metrics.l2Latency, metrics.l3Latency,
			metrics.tokens, metrics.cost, nullIfEmpty(metrics.err))
		if err != nil {
			return err
		}
	}
	return nil
}

type baseline struct {
	uptime                          float64
	success                         float64
	latency                         int
	l1Status, l2Status, l3Status    string
	l1Latency, l2Latency, l3Latency int
	tokens                          int
	cost                            float64
	err                             string
}

func baselineMetrics(id string, status string, score int) baseline {
	seed := stableSeed(id)
	latency := 700 + (100-score)*35 + seed%280
	out := baseline{
		uptime:    clampFloat(float64(score)+5.2, 0, 100),
		success:   clampFloat(float64(score)+4.5, 0, 100),
		latency:   latency,
		l1Status:  "ok",
		l2Status:  "ok",
		l3Status:  "ok",
		l1Latency: 60 + seed%70,
		l2Latency: 220 + seed%260,
		l3Latency: latency,
		tokens:    16000 + seed%9000,
		cost:      round3(float64(16000+seed%9000) / 1000000 * 5.2),
	}
	switch status {
	case "healthy":
	case "degraded":
		out.uptime = 96.2
		out.success = 97.1
		out.latency += 900
		out.l3Latency = out.latency
		out.l3Status = "warn"
		out.err = "slow_response"
	case "functional_down":
		out.uptime = 99.1
		out.success = 76.4
		out.l3Status = "down"
		out.err = "model_not_found"
	case "auth_error":
		out.uptime = 98.8
		out.success = 88.0
		out.l2Status = "down"
		out.l3Status = "down"
		out.err = "auth_error"
	case "connectivity_down":
		out.uptime = 61.5
		out.success = 58.0
		out.latency = 0
		out.l1Status = "down"
		out.l2Status = "na"
		out.l3Status = "na"
		out.l1Latency = 0
		out.l2Latency = 0
		out.l3Latency = 0
		out.err = "connectivity_down"
	default:
		out.uptime = 0
		out.success = 0
		out.l1Status = "na"
		out.l2Status = "na"
		out.l3Status = "na"
		out.err = "unknown"
	}
	out.uptime = round1(out.uptime)
	out.success = round1(out.success)
	return out
}

func publicChannelSQL(tail string, limitArg int, offsetArg int) string {
	base := `
		select c.id,coalesce(c.public_slug,''),c.name,c.provider,c.type,c.model,c.upstream_model,c.endpoint,coalesce(c.official_site_url,''),c.status,c.score,
			coalesce(c.intro_title,''),coalesce(c.intro_summary,''),coalesce(c.intro_body,''),coalesce(c.intro_highlights,'[]'::jsonb),
			coalesce(c.logo_url,''),coalesce(c.intro_source_url,''),c.intro_updated_at,
			coalesce(s.uptime_24h,0),coalesce(s.success_rate,0),coalesce(s.latency_p95_ms,0),
			coalesce(s.l1_status,'na'),coalesce(s.l2_status,'na'),coalesce(s.l3_status,'na'),
			coalesce(s.l1_latency_ms,0),coalesce(s.l2_latency_ms,0),coalesce(s.l3_latency_ms,0),
			coalesce(s.tokens_used,0),coalesce(s.cost_usd,0),coalesce(s.error_type,''),coalesce(s.sampled_at,c.updated_at),c.updated_at,
			coalesce(mp.input_per_mtok,0),coalesce(mp.output_per_mtok,0),
			coalesce(t.trend,'[]'::jsonb)
		from channels c
		left join lateral (
			select * from channel_status_snapshots ss where ss.channel_id=c.id order by sampled_at desc limit 1
		) s on true
		left join lateral (
			select input_per_mtok,output_per_mtok from model_prices where channel_id=c.id order by effective_at desc limit 1
		) mp on true
		left join lateral (
			select jsonb_agg(score order by sampled_at asc) as trend
			from (
				select sampled_at,score
				from channel_status_snapshots
				where channel_id=c.id
				order by sampled_at desc
				limit 18
			) tr
		) t on true
		`
	if limitArg > 0 && offsetArg > 0 {
		return base + fmt.Sprintf(tail, limitArg, offsetArg)
	}
	return base + tail
}

func scanPublicChannels(rows pgx.Rows) ([]PublicChannel, error) {
	out := []PublicChannel{}
	for rows.Next() {
		var ch PublicChannel
		var cost float64
		var updatedAt time.Time
		var introUpdatedAt sql.NullTime
		var introHighlightsRaw []byte
		var trendRaw []byte
		if err := rows.Scan(&ch.ID, &ch.PublicSlug, &ch.Name, &ch.Provider, &ch.Type, &ch.Model, &ch.UpstreamModel, &ch.Endpoint, &ch.OfficialSiteURL, &ch.Status, &ch.Score,
			&ch.IntroTitle, &ch.IntroSummary, &ch.IntroBody, &introHighlightsRaw, &ch.LogoURL, &ch.IntroSourceURL, &introUpdatedAt,
			&ch.Uptime24h, &ch.SuccessRate, &ch.LatencyP95Ms, &ch.L1Status, &ch.L2Status, &ch.L3Status, &ch.L1LatencyMs, &ch.L2LatencyMs, &ch.L3LatencyMs,
			&ch.TokensUsed, &cost, &ch.ErrorType, &ch.LastProbeAt, &updatedAt, &ch.InputPerMTok, &ch.OutputPerMTok, &trendRaw); err != nil {
			return nil, err
		}
		ch.IntroHighlights = parseStringSliceJSON(introHighlightsRaw)
		if introUpdatedAt.Valid {
			ch.IntroUpdatedAt = &introUpdatedAt.Time
		}
		ch.CostUSD = round3(cost)
		ch.StatusLabel = statusLabel(ch.Status)
		ch.Diagnosis = channelDiagnosis(ch)
		ch.Mark = providerMark(ch.Provider)
		ch.Trend = parseTrend(trendRaw)
		out = append(out, ch)
	}
	return out, rows.Err()
}

func parseTrend(raw []byte) []int {
	if len(raw) == 0 {
		return []int{}
	}
	var values []int
	if err := json.Unmarshal(raw, &values); err != nil {
		return []int{}
	}
	return values
}

func parseStringSliceJSON(raw []byte) []string {
	if len(raw) == 0 {
		return []string{}
	}
	var values []string
	if err := json.Unmarshal(raw, &values); err != nil {
		return []string{}
	}
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			out = append(out, value)
		}
	}
	return out
}

func singlePointTrend(score int) []int {
	if score <= 0 {
		return []int{}
	}
	return []int{score}
}

func publicChannelPredicates(alias string) []string {
	prefix := ""
	if strings.TrimSpace(alias) != "" {
		prefix = alias + "."
	}
	return []string{
		prefix + "owner_type='platform'",
		prefix + "public_visible is true",
		prefix + "status not in ('disabled','deleted')",
		prefix + "deleted_at is null",
		"coalesce(" + prefix + "data_origin,'') not in ('demo','test')",
		"coalesce(" + prefix + "name,'') !~* '(phase|load|test|mock|e2e|crud|pilot|admin delete|detail action|rc real provider|ui one time|console favorite)'",
	}
}

func publicChannelWhereClause(alias string) string {
	return strings.Join(publicChannelPredicates(alias), " and ")
}

func (r *Repository) resolvePublicChannelID(ctx context.Context, channelID string) (string, error) {
	channelID = strings.TrimSpace(channelID)
	if channelID == "" {
		return "", pgx.ErrNoRows
	}
	var id string
	if err := r.db.QueryRow(ctx, `
		select c.id
		from channels c
		where (c.id=$1 or c.public_slug=$1)
			and `+publicChannelWhereClause("c")+`
		limit 1
	`, channelID).Scan(&id); err != nil {
		return "", err
	}
	return id, nil
}

func (r *Repository) ensurePublicChannelVisible(ctx context.Context, channelID string) error {
	_, err := r.resolvePublicChannelID(ctx, channelID)
	return err
}

func (r *Repository) publicProbeLayers(ctx context.Context, channelID string) ([]ProbeLayer, error) {
	rows, err := r.db.Query(ctx, `
		with latest_runs as (
			select distinct on (p.layer) p.id,p.layer,p.started_at
			from probe_runs p
			join channels c on c.id=p.channel_id
			where p.channel_id=$1
				and p.layer in ('l1','l2')
				and `+publicChannelWhereClause("c")+`
			order by p.layer,p.started_at desc
		)
		select pr.step,pr.status,pr.latency_ms
		from probe_results pr
		join latest_runs lr on lr.id=pr.probe_run_id and lr.layer=pr.layer
		order by case pr.layer when 'l1' then 1 else 2 end,pr.created_at asc
	`, channelID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	layers := []ProbeLayer{}
	for rows.Next() {
		var stepName string
		var status string
		var latency int
		if err := rows.Scan(&stepName, &status, &latency); err != nil {
			return nil, err
		}
		layers = append(layers, ProbeLayer{Name: probeStepLabel(stepName), Status: status, LatencyMs: latency})
	}
	return layers, rows.Err()
}

func (r *Repository) publicL3Stats(ctx context.Context, channelID string) (L3Stats, error) {
	var total int
	var success, content, firstToken, tps, avgTokens float64
	err := r.db.QueryRow(ctx, `
		select count(*)::int,
			coalesce(avg(case when pr.status='ok' then 1.0 else 0.0 end) * 100,0)::float8,
			coalesce(avg(case when pr.metadata->>'content_valid'='true' then 1.0 else 0.0 end) * 100,0)::float8,
			coalesce(avg(case when pr.metadata->>'first_token_ms' ~ '^[0-9]+$' then (pr.metadata->>'first_token_ms')::numeric else 0 end),0)::float8,
			coalesce(avg(case when pr.metadata->>'tokens_per_second' ~ '^[0-9]+$' then (pr.metadata->>'tokens_per_second')::numeric else 0 end),0)::float8,
			coalesce(avg(case when pr.metadata->>'tokens_used' ~ '^[0-9]+$' then (pr.metadata->>'tokens_used')::numeric else 0 end),0)::float8
		from probe_results pr
		join channels c on c.id=pr.channel_id
		where pr.channel_id=$1
			and pr.layer='l3'
			and pr.created_at >= now() - interval '30 days'
			and `+publicChannelWhereClause("c")+`
	`, channelID).Scan(&total, &success, &content, &firstToken, &tps, &avgTokens)
	if err != nil {
		return L3Stats{}, err
	}
	quota := "normal"
	if total == 0 {
		quota = "not_run"
	}
	return L3Stats{
		SuccessRate:      round1(success),
		ContentValidRate: round1(content),
		FirstTokenMs:     int(math.Round(firstToken)),
		TokensPerSecond:  int(math.Round(tps)),
		AverageTokens:    int(math.Round(avgTokens)),
		QuotaClass:       quota,
	}, nil
}

func (r *Repository) publicProbeRecords(ctx context.Context, channelID string) ([]ProbeRecord, error) {
	rows, err := r.db.Query(ctx, `
		select pr.created_at,pr.layer,pr.step,coalesce(pr.http_status,0),pr.latency_ms,pr.status,coalesce(pr.error_type,'')
		from probe_results pr
		join channels c on c.id=pr.channel_id
		where pr.channel_id=$1
			and `+publicChannelWhereClause("c")+`
		order by pr.created_at desc
		limit 20
	`, channelID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	records := []ProbeRecord{}
	for rows.Next() {
		var rec ProbeRecord
		var layer string
		if err := rows.Scan(&rec.Time, &layer, &rec.Type, &rec.HTTPCode, &rec.LatencyMs, &rec.Result, &rec.ErrorType); err != nil {
			return nil, err
		}
		rec.Layer = strings.ToUpper(layer)
		records = append(records, rec)
	}
	return records, rows.Err()
}

func (r *Repository) publicErrors(ctx context.Context, channelID string) ([]ErrorBucket, error) {
	rows, err := r.db.Query(ctx, `
		select coalesce(pr.error_type,'upstream_error') as error_type,count(*)::int
		from probe_results pr
		join channels c on c.id=pr.channel_id
		where pr.channel_id=$1
			and pr.created_at >= now() - interval '30 days'
			and pr.status <> 'ok'
			and coalesce(pr.error_type,'') <> ''
			and `+publicChannelWhereClause("c")+`
		group by coalesce(pr.error_type,'upstream_error')
		order by count(*) desc,error_type asc
		limit 12
	`, channelID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ErrorBucket{}
	for rows.Next() {
		var typ string
		var count int
		if err := rows.Scan(&typ, &count); err != nil {
			return nil, err
		}
		out = append(out, ErrorBucket{Type: typ, Label: errorLabel(typ), Count: count})
	}
	return out, rows.Err()
}

func (r *Repository) publicCosts(ctx context.Context, channelID string, days int) ([]CostPoint, error) {
	rows, err := r.db.Query(ctx, `
		with usage as (
			select pr.created_at::date as day,
				sum(case when pr.metadata->>'tokens_used' ~ '^[0-9]+$' then (pr.metadata->>'tokens_used')::int else 0 end)::int as tokens,
				round((sum(case when pr.metadata->>'tokens_used' ~ '^[0-9]+$' then (pr.metadata->>'tokens_used')::numeric else 0 end) / 1000000 * 5.2)::numeric, 6)::float8 as cost
			from probe_results pr
			join channels c on c.id=pr.channel_id
			where pr.channel_id=$1
				and pr.layer='l3'
				and pr.created_at >= current_date - ($2::int - 1) * interval '1 day'
				and `+publicChannelWhereClause("c")+`
			group by pr.created_at::date
			union all
			select e.created_at::date as day,
				sum(e.request_tokens + e.response_tokens)::int as tokens,
				sum(e.cost_usd)::float8 as cost
			from gateway_request_events e
			join channels c on c.id=e.upstream_channel_id
			where e.upstream_channel_id=$1
				and e.created_at >= current_date - ($2::int - 1) * interval '1 day'
				and `+publicChannelWhereClause("c")+`
			group by e.created_at::date
		)
		select day,sum(tokens)::int,sum(cost)::float8
		from usage
		group by day
		order by day asc
	`, channelID, days)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	byDay := map[string]CostPoint{}
	for rows.Next() {
		var day time.Time
		var point CostPoint
		var cost float64
		if err := rows.Scan(&day, &point.Tokens, &cost); err != nil {
			return nil, err
		}
		point.Date = day.Format("2006-01-02")
		point.CostUSD = round3(cost)
		byDay[point.Date] = point
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := make([]CostPoint, 0, days)
	now := time.Now()
	for i := days - 1; i >= 0; i-- {
		date := now.AddDate(0, 0, -i).Format("2006-01-02")
		if point, ok := byDay[date]; ok {
			out = append(out, point)
		} else {
			out = append(out, CostPoint{Date: date})
		}
	}
	return out, nil
}

func (r *Repository) publicProbeUsage(ctx context.Context, channelID string, since time.Time) (int, float64, error) {
	args := []any{}
	sinceClause := "and pr.created_at >= current_date"
	if !since.IsZero() {
		args = append(args, since)
		sinceClause = fmt.Sprintf("and pr.created_at >= $%d", len(args))
	}
	channelClause := ""
	if strings.TrimSpace(channelID) != "" {
		args = append(args, channelID)
		channelClause = fmt.Sprintf(" and pr.channel_id=$%d", len(args))
	}
	var tokens int
	var cost float64
	err := r.db.QueryRow(ctx, `
		select
			coalesce(sum(case when pr.metadata->>'tokens_used' ~ '^[0-9]+$' then (pr.metadata->>'tokens_used')::int else 0 end),0)::int,
			coalesce(round((sum(case when pr.metadata->>'tokens_used' ~ '^[0-9]+$' then (pr.metadata->>'tokens_used')::numeric else 0 end) / 1000000 * 5.2)::numeric, 6),0)::float8
		from probe_results pr
		join channels c on c.id=pr.channel_id
		where `+publicChannelWhereClause("c")+`
				and pr.layer='l3'
				`+sinceClause+`
				`+channelClause, args...).Scan(&tokens, &cost)
	if err != nil {
		return 0, 0, err
	}
	return tokens, cost, nil
}

func normalizeChannelFilter(filter ChannelFilter) ChannelFilter {
	if filter.Page < 1 {
		filter.Page = 1
	}
	if filter.PageSize < 1 {
		filter.PageSize = 20
	}
	if filter.PageSize > 100 {
		filter.PageSize = 100
	}
	filter.Provider = strings.TrimSpace(filter.Provider)
	filter.Status = strings.TrimSpace(filter.Status)
	filter.Query = strings.TrimSpace(filter.Query)
	return filter
}

func channelWhere(filter ChannelFilter) ([]string, []any) {
	where := publicChannelPredicates("c")
	args := []any{}
	add := func(pattern string, value any) {
		args = append(args, value)
		where = append(where, fmt.Sprintf(pattern, len(args)))
	}
	if filter.Provider != "" && filter.Provider != "all" {
		add("lower(c.provider)=lower($%d)", filter.Provider)
	}
	if filter.Status != "" && filter.Status != "all" {
		add("c.status=$%d", filter.Status)
	}
	if filter.Query != "" {
		q := "%" + filter.Query + "%"
		args = append(args, q, q, q)
		n := len(args)
		where = append(where, fmt.Sprintf("(lower(c.name) like lower($%d) or lower(c.provider) like lower($%d) or lower(c.model) like lower($%d))", n-2, n-1, n))
	}
	return where, args
}

func statusLabel(status string) string {
	switch status {
	case "healthy":
		return "Healthy"
	case "degraded":
		return "Degraded"
	case "functional_down":
		return "Functional Down"
	case "auth_error":
		return "Auth Error"
	case "connectivity_down":
		return "Connectivity Down"
	case "disabled":
		return "Disabled"
	default:
		return "Unknown"
	}
}

func channelDiagnosis(ch PublicChannel) Diagnosis {
	if ch.Status == "disabled" {
		return Diagnosis{Code: "disabled", Label: "通道已停用", Severity: "info"}
	}
	if ch.L3Status == "ok" && (ch.L2Status == "auth_error" || ch.ErrorType == "models_auth_error" || ch.ErrorType == "models_unavailable") {
		return Diagnosis{
			Code:     "generation_ok_models_restricted",
			Label:    "生成可用，模型列表受限",
			Severity: "warn",
			Hint:     "上游生成接口正常，但模型列表接口不可访问或权限受限；优先确认供应商是否支持 /models，必要时配置 modelsProbeMode=skip。",
		}
	}
	if ch.L3Status == "ok" && ch.L2Status == "na" {
		return Diagnosis{
			Code:     "generation_ok_models_skipped",
			Label:    "生成可用，模型列表未参与判定",
			Severity: "info",
			Hint:     "该通道以真实生成探测为主，模型列表探测被跳过或暂不可用。",
		}
	}
	if ch.L3Status == "down" && ch.ErrorType == "empty_content" {
		return Diagnosis{
			Code:     "generation_empty_content",
			Label:    "生成返回内容为空",
			Severity: "error",
			Hint:     "上游返回 HTTP 200，但没有可解析的 assistant content；检查模型名、协议适配或供应商当前生成质量。",
		}
	}
	if ch.L3Status == "down" && ch.ErrorType == "content_mismatch" {
		return Diagnosis{
			Code:     "generation_content_mismatch",
			Label:    "生成内容未通过校验",
			Severity: "error",
			Hint:     "上游有返回内容，但没有按探测提示返回期望内容；可能是模型、系统提示或协议适配问题。",
		}
	}
	if ch.L3Status == "auth_error" || ch.Status == "auth_error" {
		return Diagnosis{
			Code:     "auth_error",
			Label:    "认证异常",
			Severity: "error",
			Hint:     "API Key、签名方式或供应商权限异常；先检查凭据和鉴权头配置。",
		}
	}
	if ch.Status == "connectivity_down" || ch.L1Status == "down" {
		return Diagnosis{
			Code:     "connectivity_down",
			Label:    "基础链路不可达",
			Severity: "error",
			Hint:     "DNS、TCP、TLS 或 HTTP 基础探测失败；优先检查 endpoint 域名和网络连通性。",
		}
	}
	if ch.Status == "degraded" && ch.ErrorType == "slow_response" {
		return Diagnosis{
			Code:     "slow_response",
			Label:    "响应偏慢",
			Severity: "warn",
			Hint:     "真实生成成功但超过当前 L3 延迟阈值；可检查供应商拥塞或调整 l3WarnMs。",
		}
	}
	if ch.Status == "functional_down" {
		return Diagnosis{
			Code:     firstNonEmptyDiagnosisText(ch.ErrorType, "functional_down"),
			Label:    errorLabel(firstNonEmptyDiagnosisText(ch.ErrorType, "functional_down")),
			Severity: "error",
			Hint:     "基础链路可达，但真实生成探测未通过。",
		}
	}
	if ch.Status == "healthy" {
		return Diagnosis{Code: "all_checks_ok", Label: "链路与生成均正常", Severity: "ok"}
	}
	if ch.Status == "unknown" {
		return Diagnosis{Code: "unknown", Label: "等待足够探测数据", Severity: "info"}
	}
	return Diagnosis{Code: firstNonEmptyDiagnosisText(ch.ErrorType, ch.Status, "unknown"), Label: statusLabel(ch.Status), Severity: diagnosisSeverity(ch.Status)}
}

func diagnosisSeverity(status string) string {
	switch status {
	case "healthy":
		return "ok"
	case "degraded":
		return "warn"
	case "functional_down", "connectivity_down", "auth_error":
		return "error"
	default:
		return "info"
	}
}

func firstNonEmptyDiagnosisText(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func errorLabel(typ string) string {
	switch strings.ToLower(strings.TrimSpace(typ)) {
	case "slow_response":
		return "慢响应"
	case "rate_limited":
		return "限流"
	case "auth_error":
		return "认证异常"
	case "models_auth_error":
		return "模型列表认证受限"
	case "models_timeout":
		return "模型列表超时"
	case "models_rate_limited":
		return "模型列表限流"
	case "models_probe_skipped", "l2_probe_skipped":
		return "模型列表探测已跳过"
	case "connectivity_down":
		return "连通异常"
	case "dns_failed":
		return "DNS 失败"
	case "tcp_failed":
		return "TCP 连接失败"
	case "tls_failed":
		return "TLS 失败"
	case "http_failed":
		return "HTTP 调用失败"
	case "timeout":
		return "请求超时"
	case "model_not_found":
		return "模型不可用"
	case "models_unavailable":
		return "模型列表不可用"
	case "upstream_error":
		return "上游错误"
	case "upstream_rejected":
		return "上游拒绝"
	case "upstream_unavailable":
		return "上游不可用"
	case "quota_exceeded":
		return "配额不足"
	case "empty_content":
		return "生成内容为空"
	case "content_mismatch":
		return "生成内容不匹配"
	case "functional_down":
		return "真实生成异常"
	default:
		return "未知错误"
	}
}

func probeStepLabel(step string) string {
	switch strings.ToLower(strings.TrimSpace(step)) {
	case "dns":
		return "DNS"
	case "tcp":
		return "TCP"
	case "tls":
		return "TLS"
	case "http":
		return "HTTP"
	case "models":
		return "Models"
	case "generate":
		return "Generate"
	case "parse_url":
		return "URL"
	default:
		if strings.TrimSpace(step) == "" {
			return "Unknown"
		}
		return strings.TrimSpace(step)
	}
}

func providerMark(provider string) string {
	switch strings.ToLower(provider) {
	case "anthropic":
		return "#d97706"
	case "google":
		return "#2563eb"
	case "alibaba":
		return "#7c3aed"
	case "deepseek":
		return "#0f766e"
	case "mistral":
		return "#f59e0b"
	case "volcengine":
		return "#db2777"
	case "groq":
		return "#ea580c"
	case "baidu":
		return "#1d4ed8"
	default:
		return "#10b981"
	}
}

func stableSeed(s string) int {
	total := 0
	for _, r := range s {
		total = total*31 + int(r)
	}
	if total < 0 {
		return -total
	}
	return total
}

func clampFloat(v, lo, hi float64) float64 {
	return math.Max(lo, math.Min(hi, v))
}

func round1(v float64) float64 {
	return math.Round(v*10) / 10
}

func round3(v float64) float64 {
	return math.Round(v*1000) / 1000
}

func round6(v float64) float64 {
	return math.Round(v*1000000) / 1000000
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func nullIfEmpty(value string) any {
	if value == "" {
		return nil
	}
	return value
}
