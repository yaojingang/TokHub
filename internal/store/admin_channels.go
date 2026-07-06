package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type AdminPlatformChannel struct {
	PublicChannel
	ProbeDaily          int            `json:"probeDaily"`
	ProbesUsedToday     int            `json:"probesUsedToday"`
	PublicVisible       bool           `json:"publicVisible"`
	GatewayEnabled      bool           `json:"gatewayEnabled"`
	Recommended         bool           `json:"recommended"`
	DisabledAt          *time.Time     `json:"disabledAt,omitempty"`
	DeletedAt           *time.Time     `json:"deletedAt,omitempty"`
	DataOrigin          string         `json:"dataOrigin"`
	KeyMask             string         `json:"keyMask"`
	KeyFingerprint      string         `json:"keyFingerprint"`
	CredentialUpdatedAt *time.Time     `json:"credentialUpdatedAt,omitempty"`
	InputPerMTok        float64        `json:"inputPerMtok"`
	OutputPerMTok       float64        `json:"outputPerMtok"`
	ProviderConfig      map[string]any `json:"providerConfig"`
}

type PlatformChannelInput struct {
	Name            string
	Provider        string
	Type            string
	Model           string
	UpstreamModel   string
	Endpoint        string
	OfficialSiteURL string
	IntroTitle      string
	IntroSummary    string
	IntroBody       string
	IntroHighlights []string
	LogoURL         string
	IntroSourceURL  string
	ProbeDaily      int
	PublicVisible   bool
	GatewayEnabled  bool
	Enabled         bool
	InputPerMTok    float64
	OutputPerMTok   float64
	ProviderConfig  map[string]any
	Credential      EncryptedCredential
}

type PlatformCredential struct {
	ChannelID   string
	Ciphertext  string
	Nonce       string
	Mask        string
	Fingerprint string
}

type PlatformChannelImportRow struct {
	RowNumber int
	ID        string
	Input     PlatformChannelInput
	Snapshot  *PlatformChannelImportSnapshot
}

type PlatformChannelImportResult struct {
	RowNumber      int    `json:"rowNumber"`
	ID             string `json:"id"`
	Name           string `json:"name"`
	Action         string `json:"action"`
	KeyMask        string `json:"keyMask"`
	KeyFingerprint string `json:"keyFingerprint"`
}

type PlatformChannelImportSnapshot struct {
	Status       string
	Score        int
	Uptime24h    float64
	SuccessRate  float64
	LatencyP95Ms int
	L1Status     string
	L2Status     string
	L3Status     string
	L1LatencyMs  int
	L2LatencyMs  int
	L3LatencyMs  int
	TokensUsed   int
	CostUSD      float64
	ErrorType    string
	SampledAt    *time.Time
}

func (r *Repository) AdminPlatformChannels(ctx context.Context) ([]AdminPlatformChannel, error) {
	rows, err := r.db.Query(ctx, adminPlatformChannelSQL(`
		where c.owner_type='platform' and c.status <> 'deleted' and c.deleted_at is null
		order by c.created_at desc, c.score desc, c.name asc
	`, 0, 0))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanAdminPlatformChannels(rows)
}

func (r *Repository) AdminPlatformChannel(ctx context.Context, channelID string) (AdminPlatformChannel, error) {
	rows, err := r.db.Query(ctx, adminPlatformChannelSQL(`
		where c.owner_type='platform' and c.id=$1 and c.status <> 'deleted' and c.deleted_at is null
	`, 0, 0), channelID)
	if err != nil {
		return AdminPlatformChannel{}, err
	}
	defer rows.Close()
	items, err := scanAdminPlatformChannels(rows)
	if err != nil {
		return AdminPlatformChannel{}, err
	}
	if len(items) == 0 {
		return AdminPlatformChannel{}, pgx.ErrNoRows
	}
	return items[0], nil
}

func (r *Repository) CreatePlatformChannel(ctx context.Context, actorID string, input PlatformChannelInput) (AdminPlatformChannel, error) {
	items, err := r.CreatePlatformChannels(ctx, actorID, []PlatformChannelInput{input})
	if err != nil {
		return AdminPlatformChannel{}, err
	}
	if len(items) == 0 {
		return AdminPlatformChannel{}, fmt.Errorf("platform channel was not created")
	}
	return items[0], nil
}

func (r *Repository) CreatePlatformChannels(ctx context.Context, actorID string, inputs []PlatformChannelInput) ([]AdminPlatformChannel, error) {
	if len(inputs) == 0 {
		return nil, ErrEmptyBulkSelection
	}
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	channelIDs := make([]string, 0, len(inputs))
	for _, input := range inputs {
		input = normalizePlatformChannelInput(input)
		if input.Credential.Ciphertext == "" || input.Credential.Nonce == "" {
			return nil, fmt.Errorf("apiKey is required")
		}
		channelID := "ch_" + uuid.NewString()
		if err := createPlatformChannelTx(ctx, tx, actorID, channelID, input); err != nil {
			return nil, err
		}
		channelIDs = append(channelIDs, channelID)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	items := make([]AdminPlatformChannel, 0, len(channelIDs))
	for _, channelID := range channelIDs {
		item, err := r.AdminPlatformChannel(ctx, channelID)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, nil
}

func createPlatformChannelTx(ctx context.Context, tx pgx.Tx, actorID string, channelID string, input PlatformChannelInput) error {
	status := "unknown"
	if !input.Enabled {
		status = "disabled"
	}
	var disabledAt any
	if !input.Enabled {
		disabledAt = time.Now()
	}
	if _, err := tx.Exec(ctx, `
		insert into channels(
			id,owner_type,owner_id,name,provider,type,model,upstream_model,endpoint,official_site_url,
			intro_title,intro_summary,intro_body,intro_highlights,logo_url,intro_source_url,intro_updated_at,status,score,
			probe_daily,probes_used_today,probe_reset_date,public_visible,gateway_enabled,disabled_at,provider_config,data_origin,created_at,updated_at
		)
		values($1,'platform',null,$2,$3,$4,$5,$6,$7,$8,
			$9,$10,$11,$12::jsonb,$13,$14,
			case when $9<>'' or $10<>'' or $11<>'' or jsonb_array_length($12::jsonb)>0 or $13<>'' or $14<>'' then now() else null end,
			$15,0,$16,0,current_date,$17,$18,$19,$20::jsonb,'runtime',now(),now())
	`, channelID, input.Name, input.Provider, input.Type, input.Model, input.UpstreamModel, input.Endpoint, input.OfficialSiteURL,
		input.IntroTitle, input.IntroSummary, input.IntroBody, introHighlightsJSON(input.IntroHighlights), input.LogoURL, input.IntroSourceURL,
		status, input.ProbeDaily, input.PublicVisible, input.GatewayEnabled, disabledAt, providerConfigJSON(input.ProviderConfig)); err != nil {
		return err
	}
	if err := upsertPlatformCredentialTx(ctx, tx, channelID, actorID, input.Credential); err != nil {
		return err
	}
	if err := upsertModelPriceTx(ctx, tx, channelID, input.Provider, input.UpstreamModel, input.InputPerMTok, input.OutputPerMTok); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		insert into channel_status_snapshots(
			id,channel_id,sampled_at,status,score,uptime_24h,success_rate,latency_p95_ms,
			l1_status,l2_status,l3_status,l1_latency_ms,l2_latency_ms,l3_latency_ms,
			tokens_used,cost_usd,error_type,metadata
		)
		values($1,$2,now(),$3,0,0,0,0,'na','na','na',0,0,0,0,0,null,'{"source":"platform_channel_created"}'::jsonb)
	`, "snap_platform_"+channelID, channelID, status); err != nil {
		return err
	}
	if err := writeAuditTx(ctx, tx, AuditEvent{
		ActorType:  "user",
		ActorID:    actorID,
		Action:     "platform_channel.created",
		ObjectType: "channel",
		ObjectID:   channelID,
		Result:     "success",
		Metadata: map[string]any{
			"provider": input.Provider, "model": input.Model, "upstream_model": input.UpstreamModel, "official_site_url": input.OfficialSiteURL,
			"public_visible": input.PublicVisible, "gateway_enabled": input.GatewayEnabled,
			"key_fingerprint": input.Credential.Fingerprint,
		},
	}); err != nil {
		return err
	}
	return nil
}

func (r *Repository) UpdatePlatformChannel(ctx context.Context, actorID string, channelID string, input PlatformChannelInput) (AdminPlatformChannel, error) {
	input = normalizePlatformChannelInput(input)
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return AdminPlatformChannel{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	statusExpr := "case when $18::boolean then case when status in ('disabled','deleted') then 'unknown' else status end else 'disabled' end"
	tag, err := tx.Exec(ctx, `
		update channels
		set name=$2,
			provider=$3,
			type=$4,
			model=$5,
			upstream_model=$6,
			endpoint=$7,
			official_site_url=$8,
			intro_title=$9,
			intro_summary=$10,
			intro_body=$11,
			intro_highlights=$12::jsonb,
			logo_url=$13,
			intro_source_url=$14,
			intro_updated_at=case when $9<>'' or $10<>'' or $11<>'' or jsonb_array_length($12::jsonb)>0 or $13<>'' or $14<>'' then now() else null end,
			probe_daily=$15,
			public_visible=$16,
			gateway_enabled=$17,
			disabled_at=case when $18::boolean then null else coalesce(disabled_at, now()) end,
			provider_config=$19::jsonb,
			deleted_at=null,
			status=`+statusExpr+`,
			updated_at=now()
		where id=$1 and owner_type='platform' and status <> 'deleted' and deleted_at is null
	`, channelID, input.Name, input.Provider, input.Type, input.Model, input.UpstreamModel, input.Endpoint, input.OfficialSiteURL,
		input.IntroTitle, input.IntroSummary, input.IntroBody, introHighlightsJSON(input.IntroHighlights), input.LogoURL, input.IntroSourceURL,
		input.ProbeDaily, input.PublicVisible, input.GatewayEnabled, input.Enabled, providerConfigJSON(input.ProviderConfig))
	if err != nil {
		return AdminPlatformChannel{}, err
	}
	if tag.RowsAffected() == 0 {
		return AdminPlatformChannel{}, pgx.ErrNoRows
	}
	if err := upsertModelPriceTx(ctx, tx, channelID, input.Provider, input.UpstreamModel, input.InputPerMTok, input.OutputPerMTok); err != nil {
		return AdminPlatformChannel{}, err
	}
	if !input.Enabled || !input.GatewayEnabled {
		if _, err := tx.Exec(ctx, `update gateway_upstreams set enabled=false where channel_id=$1`, channelID); err != nil {
			return AdminPlatformChannel{}, err
		}
	}
	if err := writeAuditTx(ctx, tx, AuditEvent{
		ActorType:  "user",
		ActorID:    actorID,
		Action:     "platform_channel.updated",
		ObjectType: "channel",
		ObjectID:   channelID,
		Result:     "success",
		Metadata: map[string]any{
			"provider": input.Provider, "model": input.Model, "upstream_model": input.UpstreamModel, "official_site_url": input.OfficialSiteURL,
			"public_visible": input.PublicVisible, "gateway_enabled": input.GatewayEnabled, "enabled": input.Enabled,
		},
	}); err != nil {
		return AdminPlatformChannel{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return AdminPlatformChannel{}, err
	}
	return r.AdminPlatformChannel(ctx, channelID)
}

func (r *Repository) RotatePlatformCredential(ctx context.Context, actorID string, channelID string, credential EncryptedCredential) (AdminPlatformChannel, error) {
	if credential.Ciphertext == "" || credential.Nonce == "" {
		return AdminPlatformChannel{}, fmt.Errorf("apiKey is required")
	}
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return AdminPlatformChannel{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var exists bool
	if err := tx.QueryRow(ctx, `select exists(select 1 from channels where id=$1 and owner_type='platform' and status <> 'deleted' and deleted_at is null)`, channelID).Scan(&exists); err != nil {
		return AdminPlatformChannel{}, err
	}
	if !exists {
		return AdminPlatformChannel{}, pgx.ErrNoRows
	}
	if err := upsertPlatformCredentialTx(ctx, tx, channelID, actorID, credential); err != nil {
		return AdminPlatformChannel{}, err
	}
	if err := writeAuditTx(ctx, tx, AuditEvent{
		ActorType:  "user",
		ActorID:    actorID,
		Action:     "platform_channel.credential_rotated",
		ObjectType: "channel",
		ObjectID:   channelID,
		Result:     "success",
		Metadata:   map[string]any{"key_fingerprint": credential.Fingerprint},
	}); err != nil {
		return AdminPlatformChannel{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return AdminPlatformChannel{}, err
	}
	return r.AdminPlatformChannel(ctx, channelID)
}

func (r *Repository) ImportPlatformChannels(ctx context.Context, actorID string, rows []PlatformChannelImportRow) ([]PlatformChannelImportResult, error) {
	if len(rows) == 0 {
		return nil, ErrEmptyBulkSelection
	}
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	results := make([]PlatformChannelImportResult, 0, len(rows))
	for _, row := range rows {
		input := normalizePlatformChannelInput(row.Input)
		if input.Credential.Ciphertext == "" || input.Credential.Nonce == "" {
			return nil, fmt.Errorf("row %d: apiKey is required", row.RowNumber)
		}
		snapshot := normalizePlatformChannelImportSnapshot(row.Snapshot)
		channelID := strings.TrimSpace(row.ID)
		action := "created"
		if channelID == "" {
			channelID = "ch_" + uuid.NewString()
		} else {
			var ownerType string
			err := tx.QueryRow(ctx, `select owner_type from channels where id=$1`, channelID).Scan(&ownerType)
			if err == nil {
				if ownerType != "platform" {
					return nil, fmt.Errorf("row %d: id %s belongs to a non-platform channel", row.RowNumber, channelID)
				}
				action = "updated"
			} else if !errors.Is(err, pgx.ErrNoRows) {
				return nil, err
			}
		}

		status := "unknown"
		score := 0
		var disabledAt any
		if !input.Enabled {
			status = "disabled"
			disabledAt = time.Now()
			snapshot = nil
		} else if snapshot != nil {
			status = snapshot.Status
			score = snapshot.Score
		}
		if action == "created" {
			if _, err := tx.Exec(ctx, `
				insert into channels(
					id,owner_type,owner_id,name,provider,type,model,upstream_model,endpoint,official_site_url,
					intro_title,intro_summary,intro_body,intro_highlights,logo_url,intro_source_url,intro_updated_at,status,score,
					probe_daily,probes_used_today,probe_reset_date,public_visible,gateway_enabled,disabled_at,provider_config,data_origin,created_at,updated_at
				)
				values($1,'platform',null,$2,$3,$4,$5,$6,$7,$8,
					$9,$10,$11,$12::jsonb,$13,$14,
					case when $9<>'' or $10<>'' or $11<>'' or jsonb_array_length($12::jsonb)>0 or $13<>'' or $14<>'' then now() else null end,
					$15,$16,$17,0,current_date,$18,$19,$20,$21::jsonb,'runtime',now(),now())
			`, channelID, input.Name, input.Provider, input.Type, input.Model, input.UpstreamModel, input.Endpoint, input.OfficialSiteURL,
				input.IntroTitle, input.IntroSummary, input.IntroBody, introHighlightsJSON(input.IntroHighlights), input.LogoURL, input.IntroSourceURL,
				status, score, input.ProbeDaily, input.PublicVisible, input.GatewayEnabled, disabledAt, providerConfigJSON(input.ProviderConfig)); err != nil {
				return nil, fmt.Errorf("row %d: %w", row.RowNumber, err)
			}
			snapshotSource := "platform_channel_imported"
			if snapshot != nil {
				snapshotSource = "channel_api_sync"
			}
			if err := insertPlatformChannelImportSnapshotTx(ctx, tx, channelID, status, score, snapshot, snapshotSource); err != nil {
				return nil, fmt.Errorf("row %d: %w", row.RowNumber, err)
			}
		} else {
			tag, err := tx.Exec(ctx, `
				update channels
				set name=$2,
					provider=$3,
					type=$4,
					model=$5,
					upstream_model=$6,
					endpoint=$7,
					official_site_url=$8,
					intro_title=$9,
					intro_summary=$10,
					intro_body=$11,
					intro_highlights=$12::jsonb,
					logo_url=$13,
					intro_source_url=$14,
					intro_updated_at=case when $9<>'' or $10<>'' or $11<>'' or jsonb_array_length($12::jsonb)>0 or $13<>'' or $14<>'' then now() else null end,
					status=$15,
					probe_daily=$17,
					public_visible=$18,
					gateway_enabled=$19,
					disabled_at=case when $20::boolean then null else coalesce(disabled_at, now()) end,
					deleted_at=null,
					provider_config=$21::jsonb,
					score=case when $22::boolean then $16 else score end,
					data_origin='runtime',
					updated_at=now()
				where id=$1 and owner_type='platform'
			`, channelID, input.Name, input.Provider, input.Type, input.Model, input.UpstreamModel, input.Endpoint, input.OfficialSiteURL,
				input.IntroTitle, input.IntroSummary, input.IntroBody, introHighlightsJSON(input.IntroHighlights), input.LogoURL, input.IntroSourceURL,
				status, score, input.ProbeDaily, input.PublicVisible, input.GatewayEnabled, input.Enabled, providerConfigJSON(input.ProviderConfig), snapshot != nil)
			if err != nil {
				return nil, fmt.Errorf("row %d: %w", row.RowNumber, err)
			}
			if tag.RowsAffected() == 0 {
				return nil, fmt.Errorf("row %d: platform channel not found", row.RowNumber)
			}
			if snapshot != nil {
				if err := insertPlatformChannelImportSnapshotTx(ctx, tx, channelID, status, score, snapshot, "channel_api_sync"); err != nil {
					return nil, fmt.Errorf("row %d: %w", row.RowNumber, err)
				}
			}
		}

		if err := upsertPlatformCredentialTx(ctx, tx, channelID, actorID, input.Credential); err != nil {
			return nil, fmt.Errorf("row %d: %w", row.RowNumber, err)
		}
		if err := upsertModelPriceTx(ctx, tx, channelID, input.Provider, input.UpstreamModel, input.InputPerMTok, input.OutputPerMTok); err != nil {
			return nil, fmt.Errorf("row %d: %w", row.RowNumber, err)
		}
		if !input.Enabled || !input.GatewayEnabled {
			if _, err := tx.Exec(ctx, `update gateway_upstreams set enabled=false where channel_id=$1`, channelID); err != nil {
				return nil, fmt.Errorf("row %d: %w", row.RowNumber, err)
			}
		}
		if err := writeAuditTx(ctx, tx, AuditEvent{
			ActorType:  "user",
			ActorID:    actorID,
			Action:     "platform_channel.import_" + action,
			ObjectType: "channel",
			ObjectID:   channelID,
			Result:     "success",
			Metadata: map[string]any{
				"row":             row.RowNumber,
				"provider":        input.Provider,
				"model":           input.Model,
				"upstream_model":  input.UpstreamModel,
				"key_fingerprint": input.Credential.Fingerprint,
			},
		}); err != nil {
			return nil, fmt.Errorf("row %d: %w", row.RowNumber, err)
		}
		results = append(results, PlatformChannelImportResult{
			RowNumber:      row.RowNumber,
			ID:             channelID,
			Name:           input.Name,
			Action:         action,
			KeyMask:        input.Credential.Mask,
			KeyFingerprint: input.Credential.Fingerprint,
		})
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return results, nil
}

func insertPlatformChannelImportSnapshotTx(ctx context.Context, tx pgx.Tx, channelID string, status string, score int, snapshot *PlatformChannelImportSnapshot, source string) error {
	metadata := map[string]any{"source": source}
	if snapshot == nil {
		_, err := tx.Exec(ctx, `
			insert into channel_status_snapshots(
				id,channel_id,sampled_at,status,score,uptime_24h,success_rate,latency_p95_ms,
				l1_status,l2_status,l3_status,l1_latency_ms,l2_latency_ms,l3_latency_ms,
				tokens_used,cost_usd,error_type,metadata
			)
			values($1,$2,now(),$3,$4,0,0,0,'na','na','na',0,0,0,0,0,null,$5::jsonb)
		`, "snap_import_"+uuid.NewString(), channelID, status, score, importJSON(metadata))
		return err
	}
	if snapshot.SampledAt != nil {
		metadata["source_sampled_at"] = snapshot.SampledAt.UTC().Format(time.RFC3339)
	}
	var errValue any
	if snapshot.ErrorType != "" {
		errValue = snapshot.ErrorType
	}
	_, err := tx.Exec(ctx, `
		insert into channel_status_snapshots(
			id,channel_id,sampled_at,status,score,uptime_24h,success_rate,latency_p95_ms,
			l1_status,l2_status,l3_status,l1_latency_ms,l2_latency_ms,l3_latency_ms,
			tokens_used,cost_usd,error_type,metadata
		)
		values($1,$2,now(),$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17::jsonb)
	`, "snap_sync_"+uuid.NewString(), channelID, snapshot.Status, snapshot.Score, snapshot.Uptime24h, snapshot.SuccessRate, snapshot.LatencyP95Ms,
		snapshot.L1Status, snapshot.L2Status, snapshot.L3Status, snapshot.L1LatencyMs, snapshot.L2LatencyMs, snapshot.L3LatencyMs,
		snapshot.TokensUsed, snapshot.CostUSD, errValue, importJSON(metadata))
	return err
}

func normalizePlatformChannelImportSnapshot(snapshot *PlatformChannelImportSnapshot) *PlatformChannelImportSnapshot {
	if snapshot == nil {
		return nil
	}
	out := *snapshot
	out.Status = normalizeImportedChannelStatus(out.Status)
	out.Score = importClampInt(out.Score, 0, 100)
	out.Uptime24h = importClampFloat(out.Uptime24h, 0, 100)
	out.SuccessRate = importClampFloat(out.SuccessRate, 0, 100)
	out.LatencyP95Ms = importMaxInt(out.LatencyP95Ms, 0)
	out.L1Status = normalizeImportedLayerStatus(out.L1Status)
	out.L2Status = normalizeImportedLayerStatus(out.L2Status)
	out.L3Status = normalizeImportedLayerStatus(out.L3Status)
	out.L1LatencyMs = importMaxInt(out.L1LatencyMs, 0)
	out.L2LatencyMs = importMaxInt(out.L2LatencyMs, 0)
	out.L3LatencyMs = importMaxInt(out.L3LatencyMs, 0)
	out.TokensUsed = importMaxInt(out.TokensUsed, 0)
	if out.CostUSD < 0 {
		out.CostUSD = 0
	}
	out.ErrorType = strings.TrimSpace(out.ErrorType)
	if len(out.ErrorType) > 120 {
		out.ErrorType = out.ErrorType[:120]
	}
	return &out
}

func normalizeImportedChannelStatus(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "healthy", "degraded", "auth_error", "connectivity_down", "functional_down", "unknown":
		return strings.ToLower(strings.TrimSpace(status))
	default:
		return "unknown"
	}
}

func normalizeImportedLayerStatus(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "ok", "down", "auth_error", "warn", "na":
		return strings.ToLower(strings.TrimSpace(status))
	default:
		return "na"
	}
}

func importClampInt(value int, minValue int, maxValue int) int {
	if value < minValue {
		return minValue
	}
	if value > maxValue {
		return maxValue
	}
	return value
}

func importMaxInt(value int, minValue int) int {
	if value < minValue {
		return minValue
	}
	return value
}

func importClampFloat(value float64, minValue float64, maxValue float64) float64 {
	if value < minValue {
		return minValue
	}
	if value > maxValue {
		return maxValue
	}
	return value
}

func importJSON(value any) string {
	raw, err := json.Marshal(value)
	if err != nil {
		return "{}"
	}
	return string(raw)
}

func (r *Repository) DisablePlatformChannel(ctx context.Context, actorID string, channelID string) (AdminPlatformChannel, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return AdminPlatformChannel{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	tag, err := tx.Exec(ctx, `
		update channels
		set status='disabled',
			public_visible=false,
			gateway_enabled=false,
			disabled_at=coalesce(disabled_at, now()),
			updated_at=now()
		where id=$1 and owner_type='platform' and status <> 'deleted' and deleted_at is null
	`, channelID)
	if err != nil {
		return AdminPlatformChannel{}, err
	}
	if tag.RowsAffected() == 0 {
		return AdminPlatformChannel{}, pgx.ErrNoRows
	}
	if _, err := tx.Exec(ctx, `update gateway_upstreams set enabled=false where channel_id=$1`, channelID); err != nil {
		return AdminPlatformChannel{}, err
	}
	if err := writeAuditTx(ctx, tx, AuditEvent{
		ActorType:  "user",
		ActorID:    actorID,
		Action:     "platform_channel.disabled",
		ObjectType: "channel",
		ObjectID:   channelID,
		Result:     "success",
		Metadata:   map[string]any{},
	}); err != nil {
		return AdminPlatformChannel{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return AdminPlatformChannel{}, err
	}
	return r.AdminPlatformChannel(ctx, channelID)
}

func (r *Repository) DeletePlatformChannel(ctx context.Context, actorID string, channelID string) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	tag, err := tx.Exec(ctx, `
		update channels
		set status='deleted',
			public_visible=false,
			gateway_enabled=false,
			disabled_at=coalesce(disabled_at, now()),
			deleted_at=coalesce(deleted_at, now()),
			updated_at=now()
		where id=$1 and owner_type='platform' and status <> 'deleted' and deleted_at is null
	`, channelID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	if _, err := tx.Exec(ctx, `update gateway_upstreams set enabled=false where channel_id=$1`, channelID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		update channel_credentials
		set key_ciphertext='',
			key_nonce='',
			key_mask='deleted',
			key_fingerprint='deleted:' || channel_id,
			updated_at=now()
		where channel_id=$1
	`, channelID); err != nil {
		return err
	}
	if err := writeAuditTx(ctx, tx, AuditEvent{
		ActorType:  "user",
		ActorID:    actorID,
		Action:     "platform_channel.deleted",
		ObjectType: "channel",
		ObjectID:   channelID,
		Result:     "success",
		Metadata:   map[string]any{"credential_scrubbed": true},
	}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (r *Repository) BulkDisablePlatformChannels(ctx context.Context, actorID string, ids []string) ([]AdminPlatformChannel, error) {
	ids = uniqueIDs(ids)
	if len(ids) == 0 {
		return nil, ErrEmptyBulkSelection
	}
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	rows, err := tx.Query(ctx, `
		update channels
		set status='disabled',
			public_visible=false,
			gateway_enabled=false,
			disabled_at=coalesce(disabled_at, now()),
			updated_at=now()
		where id=any($1) and owner_type='platform' and status <> 'deleted' and deleted_at is null
		returning id
	`, ids)
	if err != nil {
		return nil, err
	}
	updatedIDs, err := collectStringRows(rows)
	if err != nil {
		return nil, err
	}
	if len(updatedIDs) != len(ids) {
		return nil, pgx.ErrNoRows
	}
	if _, err := tx.Exec(ctx, `update gateway_upstreams set enabled=false where channel_id=any($1)`, ids); err != nil {
		return nil, err
	}
	for _, id := range updatedIDs {
		if err := writeAuditTx(ctx, tx, AuditEvent{
			ActorType:  "user",
			ActorID:    actorID,
			Action:     "platform_channel.bulk_disabled",
			ObjectType: "channel",
			ObjectID:   id,
			Result:     "success",
			Metadata:   map[string]any{"bulk": true},
		}); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return r.AdminPlatformChannels(ctx)
}

func (r *Repository) BulkEnablePlatformChannels(ctx context.Context, actorID string, ids []string) ([]AdminPlatformChannel, error) {
	ids = uniqueIDs(ids)
	if len(ids) == 0 {
		return nil, ErrEmptyBulkSelection
	}
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	rows, err := tx.Query(ctx, `
		update channels
		set status=case when status='disabled' then 'unknown' else status end,
			public_visible=true,
			gateway_enabled=true,
			disabled_at=null,
			updated_at=now()
		where id=any($1) and owner_type='platform' and status <> 'deleted' and deleted_at is null
		returning id
	`, ids)
	if err != nil {
		return nil, err
	}
	updatedIDs, err := collectStringRows(rows)
	if err != nil {
		return nil, err
	}
	if len(updatedIDs) != len(ids) {
		return nil, pgx.ErrNoRows
	}
	for _, id := range updatedIDs {
		if err := writeAuditTx(ctx, tx, AuditEvent{
			ActorType:  "user",
			ActorID:    actorID,
			Action:     "platform_channel.bulk_enabled",
			ObjectType: "channel",
			ObjectID:   id,
			Result:     "success",
			Metadata:   map[string]any{"bulk": true},
		}); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return r.AdminPlatformChannels(ctx)
}

func (r *Repository) BulkDeletePlatformChannels(ctx context.Context, actorID string, ids []string) ([]AdminPlatformChannel, error) {
	ids = uniqueIDs(ids)
	if len(ids) == 0 {
		return nil, ErrEmptyBulkSelection
	}
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	rows, err := tx.Query(ctx, `
		update channels
		set status='deleted',
			public_visible=false,
			gateway_enabled=false,
			disabled_at=coalesce(disabled_at, now()),
			deleted_at=coalesce(deleted_at, now()),
			updated_at=now()
		where id=any($1) and owner_type='platform' and status <> 'deleted' and deleted_at is null
		returning id
	`, ids)
	if err != nil {
		return nil, err
	}
	deletedIDs, err := collectStringRows(rows)
	if err != nil {
		return nil, err
	}
	if len(deletedIDs) != len(ids) {
		return nil, pgx.ErrNoRows
	}
	if _, err := tx.Exec(ctx, `update gateway_upstreams set enabled=false where channel_id=any($1)`, ids); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `
		update channel_credentials
		set key_ciphertext='',
			key_nonce='',
			key_mask='deleted',
			key_fingerprint='deleted:' || channel_id,
			updated_at=now()
		where channel_id=any($1)
	`, ids); err != nil {
		return nil, err
	}
	for _, id := range deletedIDs {
		if err := writeAuditTx(ctx, tx, AuditEvent{
			ActorType:  "user",
			ActorID:    actorID,
			Action:     "platform_channel.bulk_deleted",
			ObjectType: "channel",
			ObjectID:   id,
			Result:     "success",
			Metadata:   map[string]any{"bulk": true, "credential_scrubbed": true},
		}); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return r.AdminPlatformChannels(ctx)
}

func (r *Repository) PlatformCredential(ctx context.Context, channelID string) (PlatformCredential, error) {
	var cred PlatformCredential
	err := r.db.QueryRow(ctx, `
		select cc.channel_id,cc.key_ciphertext,cc.key_nonce,cc.key_mask,cc.key_fingerprint
		from channel_credentials cc
		join channels c on c.id=cc.channel_id
		where cc.channel_id=$1 and c.owner_type='platform' and c.status <> 'deleted' and c.deleted_at is null
	`, channelID).Scan(&cred.ChannelID, &cred.Ciphertext, &cred.Nonce, &cred.Mask, &cred.Fingerprint)
	return cred, err
}

func normalizePlatformChannelInput(input PlatformChannelInput) PlatformChannelInput {
	input.Name = strings.TrimSpace(input.Name)
	input.Provider = strings.TrimSpace(input.Provider)
	input.Type = strings.TrimSpace(input.Type)
	input.Model = strings.TrimSpace(input.Model)
	input.UpstreamModel = strings.TrimSpace(input.UpstreamModel)
	input.Endpoint = strings.TrimRight(strings.TrimSpace(input.Endpoint), "/")
	input.OfficialSiteURL = strings.TrimSpace(input.OfficialSiteURL)
	input.IntroTitle = strings.TrimSpace(input.IntroTitle)
	input.IntroSummary = strings.TrimSpace(input.IntroSummary)
	input.IntroBody = normalizeMultilineText(input.IntroBody)
	input.IntroHighlights = normalizeStringList(input.IntroHighlights, 8, 120)
	input.LogoURL = strings.TrimSpace(input.LogoURL)
	input.IntroSourceURL = strings.TrimSpace(input.IntroSourceURL)
	if input.Provider == "" {
		input.Provider = "OpenAI"
	}
	if input.Type == "" {
		input.Type = "openai-compatible"
	}
	if input.UpstreamModel == "" {
		input.UpstreamModel = input.Model
	}
	if input.ProbeDaily <= 0 {
		input.ProbeDaily = 2880
	}
	if input.ProbeDaily > 10000 {
		input.ProbeDaily = 10000
	}
	if input.InputPerMTok < 0 {
		input.InputPerMTok = 0
	}
	if input.OutputPerMTok < 0 {
		input.OutputPerMTok = 0
	}
	if !input.Enabled {
		input.PublicVisible = false
		input.GatewayEnabled = false
	}
	if input.ProviderConfig == nil {
		input.ProviderConfig = map[string]any{}
	}
	return input
}

func providerConfigJSON(config map[string]any) string {
	if config == nil {
		return "{}"
	}
	raw, err := json.Marshal(config)
	if err != nil {
		return "{}"
	}
	return string(raw)
}

func introHighlightsJSON(values []string) string {
	raw, err := json.Marshal(normalizeStringList(values, 8, 120))
	if err != nil {
		return "[]"
	}
	return string(raw)
}

func normalizeStringList(values []string, maxItems int, maxLen int) []string {
	if maxItems <= 0 {
		return []string{}
	}
	out := make([]string, 0, len(values))
	seen := map[string]bool{}
	for _, value := range values {
		value = strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
		if value == "" {
			continue
		}
		if len(value) > maxLen {
			value = value[:maxLen]
		}
		key := strings.ToLower(value)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, value)
		if len(out) >= maxItems {
			break
		}
	}
	return out
}

func normalizeMultilineText(value string) string {
	value = strings.ReplaceAll(value, "\r\n", "\n")
	value = strings.ReplaceAll(value, "\r", "\n")
	lines := strings.Split(value, "\n")
	out := make([]string, 0, len(lines))
	blank := false
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			if !blank && len(out) > 0 {
				out = append(out, "")
				blank = true
			}
			continue
		}
		out = append(out, line)
		blank = false
	}
	for len(out) > 0 && out[len(out)-1] == "" {
		out = out[:len(out)-1]
	}
	return strings.Join(out, "\n")
}

func upsertPlatformCredentialTx(ctx context.Context, tx pgx.Tx, channelID string, actorID string, credential EncryptedCredential) error {
	_, err := tx.Exec(ctx, `
		insert into channel_credentials(id,channel_id,owner_id,key_ciphertext,key_nonce,key_fingerprint,key_mask,created_at,updated_at)
		values($1,$2,$3,$4,$5,$6,$7,now(),now())
		on conflict(channel_id) do update set
			owner_id=excluded.owner_id,
			key_ciphertext=excluded.key_ciphertext,
			key_nonce=excluded.key_nonce,
			key_fingerprint=excluded.key_fingerprint,
			key_mask=excluded.key_mask,
			updated_at=now()
	`, "cred_"+uuid.NewString(), channelID, actorID, credential.Ciphertext, credential.Nonce, credential.Fingerprint, credential.Mask)
	return err
}

func collectStringRows(rows pgx.Rows) ([]string, error) {
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var value string
		if err := rows.Scan(&value); err != nil {
			return nil, err
		}
		out = append(out, value)
	}
	return out, rows.Err()
}

func upsertModelPriceTx(ctx context.Context, tx pgx.Tx, channelID string, provider string, modelKey string, inputPerMTok float64, outputPerMTok float64) error {
	modelKey = strings.TrimSpace(modelKey)
	if modelKey == "" {
		return nil
	}
	if inputPerMTok == 0 && outputPerMTok == 0 {
		_, err := tx.Exec(ctx, `delete from model_prices where channel_id=$1`, channelID)
		return err
	}
	modelID, err := ensureModelCatalogTx(ctx, tx, provider, modelKey)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `
		insert into model_prices(id,model_id,channel_id,input_per_mtok,output_per_mtok,currency,effective_at)
		values($1,$2,$3,$4,$5,'USD',now())
	`, "mpr_"+uuid.NewString(), modelID, channelID, inputPerMTok, outputPerMTok)
	return err
}

func ensureModelCatalogTx(ctx context.Context, tx pgx.Tx, provider string, modelKey string) (string, error) {
	var modelID string
	err := tx.QueryRow(ctx, `select id from model_catalog where model_key=$1`, modelKey).Scan(&modelID)
	if err == nil {
		return modelID, nil
	}
	if err != pgx.ErrNoRows {
		return "", err
	}
	modelID = "mdl_" + uuid.NewString()
	display := modelKey
	if provider != "" {
		display = strings.TrimSpace(provider) + " " + modelKey
	}
	err = tx.QueryRow(ctx, `
		insert into model_catalog(id,provider,model_key,display_name,context_window,capabilities_json,status,created_at)
		values($1,$2,$3,$4,0,'{}'::jsonb,'active',now())
		on conflict(model_key) do update set provider=excluded.provider
		returning id
	`, modelID, provider, modelKey, display).Scan(&modelID)
	return modelID, err
}

func adminPlatformChannelSQL(tail string, limitArg int, offsetArg int) string {
	base := `
		select c.id,coalesce(c.public_slug,''),c.name,c.provider,c.type,c.model,c.upstream_model,c.endpoint,coalesce(c.official_site_url,''),c.status,c.score,
			coalesce(c.intro_title,''),coalesce(c.intro_summary,''),coalesce(c.intro_body,''),coalesce(c.intro_highlights,'[]'::jsonb),
			coalesce(c.logo_url,''),coalesce(c.intro_source_url,''),c.intro_updated_at,
			coalesce(s.uptime_24h,0),coalesce(s.success_rate,0),coalesce(s.latency_p95_ms,0),
			coalesce(s.l1_status,'na'),coalesce(s.l2_status,'na'),coalesce(s.l3_status,'na'),
			coalesce(s.l1_latency_ms,0),coalesce(s.l2_latency_ms,0),coalesce(s.l3_latency_ms,0),
			coalesce(s.tokens_used,0),coalesce(s.cost_usd,0),coalesce(s.error_type,''),coalesce(s.sampled_at,c.updated_at),c.updated_at,
			c.probe_daily,c.probes_used_today,c.public_visible,c.gateway_enabled,c.disabled_at,c.deleted_at,c.data_origin,
			coalesce(cc.key_mask,''),coalesce(cc.key_fingerprint,''),cc.updated_at,
			coalesce(mp.input_per_mtok,0),coalesce(mp.output_per_mtok,0),coalesce(c.provider_config,'{}'::jsonb),
			exists(select 1 from recommend_picks rp where rp.channel_id=c.id and rp.enabled=true)
		from channels c
		left join channel_credentials cc on cc.channel_id=c.id
		left join lateral (
			select * from channel_status_snapshots ss where ss.channel_id=c.id order by sampled_at desc limit 1
		) s on true
		left join lateral (
			select input_per_mtok,output_per_mtok from model_prices where channel_id=c.id order by effective_at desc limit 1
		) mp on true
		`
	if limitArg > 0 && offsetArg > 0 {
		return base + fmt.Sprintf(tail, limitArg, offsetArg)
	}
	return base + tail
}

func scanAdminPlatformChannels(rows pgx.Rows) ([]AdminPlatformChannel, error) {
	out := []AdminPlatformChannel{}
	for rows.Next() {
		var item AdminPlatformChannel
		var cost float64
		var updatedAt time.Time
		var disabledAt sql.NullTime
		var deletedAt sql.NullTime
		var credUpdatedAt sql.NullTime
		var introUpdatedAt sql.NullTime
		var introHighlightsRaw []byte
		var providerConfigRaw []byte
		if err := rows.Scan(&item.ID, &item.PublicSlug, &item.Name, &item.Provider, &item.Type, &item.Model, &item.UpstreamModel, &item.Endpoint, &item.OfficialSiteURL, &item.Status, &item.Score,
			&item.IntroTitle, &item.IntroSummary, &item.IntroBody, &introHighlightsRaw, &item.LogoURL, &item.IntroSourceURL, &introUpdatedAt,
			&item.Uptime24h, &item.SuccessRate, &item.LatencyP95Ms, &item.L1Status, &item.L2Status, &item.L3Status, &item.L1LatencyMs, &item.L2LatencyMs, &item.L3LatencyMs,
			&item.TokensUsed, &cost, &item.ErrorType, &item.LastProbeAt, &updatedAt,
			&item.ProbeDaily, &item.ProbesUsedToday, &item.PublicVisible, &item.GatewayEnabled, &disabledAt, &deletedAt, &item.DataOrigin,
			&item.KeyMask, &item.KeyFingerprint, &credUpdatedAt, &item.InputPerMTok, &item.OutputPerMTok, &providerConfigRaw, &item.Recommended); err != nil {
			return nil, err
		}
		item.IntroHighlights = parseStringSliceJSON(introHighlightsRaw)
		if introUpdatedAt.Valid {
			item.IntroUpdatedAt = &introUpdatedAt.Time
		}
		if len(providerConfigRaw) > 0 {
			_ = json.Unmarshal(providerConfigRaw, &item.ProviderConfig)
		}
		if item.ProviderConfig == nil {
			item.ProviderConfig = map[string]any{}
		}
		item.CostUSD = round3(cost)
		item.StatusLabel = statusLabel(item.Status)
		item.Diagnosis = channelDiagnosis(item.PublicChannel)
		item.Mark = providerMark(item.Provider)
		item.Trend = singlePointTrend(item.Score)
		if disabledAt.Valid {
			item.DisabledAt = &disabledAt.Time
		}
		if deletedAt.Valid {
			item.DeletedAt = &deletedAt.Time
		}
		if credUpdatedAt.Valid {
			item.CredentialUpdatedAt = &credUpdatedAt.Time
		}
		out = append(out, item)
	}
	return out, rows.Err()
}
