package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

var (
	ErrIdempotencyConflict                       = errors.New("idempotency key was already used for a different request")
	ErrAIConnectionLimit                         = errors.New("AI connection limit reached")
	ErrAIConnectionDuplicate                     = errors.New("AI connection credential is already connected")
	ErrAIConnectionCredentialRotationUnsupported = errors.New("AI connection credential rotation is unsupported")
	ErrExperimentalRelayExists                   = errors.New("experimental AI connection already has a personal relay")
)

type AIConnection struct {
	ID                     string              `json:"id"`
	OwnerUserID            string              `json:"-"`
	OrgID                  string              `json:"orgId"`
	Provider               string              `json:"provider"`
	ProductLine            string              `json:"productLine"`
	Region                 string              `json:"region"`
	WorkspaceID            string              `json:"workspaceId,omitempty"`
	AuthMethod             string              `json:"authMethod"`
	Protocol               string              `json:"protocol"`
	AdapterType            string              `json:"adapterType"`
	Endpoint               string              `json:"endpoint"`
	ProviderConfig         map[string]any      `json:"providerConfig"`
	DisplayName            string              `json:"displayName"`
	Status                 string              `json:"status"`
	AuthStatus             string              `json:"authStatus"`
	SharingScope           string              `json:"sharingScope"`
	RiskLevel              string              `json:"riskLevel"`
	ProviderAdapterVersion string              `json:"providerAdapterVersion"`
	TermsAckVersion        string              `json:"termsAckVersion,omitempty"`
	AccountMask            string              `json:"accountMask,omitempty"`
	ValidationStage        string              `json:"validationStage"`
	ValidationLatencyMs    int                 `json:"validationLatencyMs"`
	ModelCount             int                 `json:"modelCount"`
	LastErrorCode          string              `json:"lastErrorCode,omitempty"`
	LastErrorMessage       string              `json:"lastErrorMessage,omitempty"`
	LastValidatedAt        *time.Time          `json:"lastValidatedAt,omitempty"`
	PolicyVersion          string              `json:"policyVersion"`
	SecretMask             string              `json:"secretMask"`
	Models                 []AIConnectionModel `json:"models"`
	CreatedAt              time.Time           `json:"createdAt"`
	UpdatedAt              time.Time           `json:"updatedAt"`
}

type AIConnectionModel struct {
	ID                  string         `json:"id"`
	ConnectionID        string         `json:"connectionId"`
	ProviderModelID     string         `json:"providerModelId"`
	DisplayName         string         `json:"displayName"`
	Enabled             bool           `json:"enabled"`
	VerificationStatus  string         `json:"verificationStatus"`
	ValidationLatencyMs int            `json:"validationLatencyMs"`
	LastErrorCode       string         `json:"lastErrorCode,omitempty"`
	LastErrorMessage    string         `json:"lastErrorMessage,omitempty"`
	LastValidatedAt     *time.Time     `json:"lastValidatedAt,omitempty"`
	Capabilities        map[string]any `json:"capabilities"`
	RouteChannelID      string         `json:"routeChannelId,omitempty"`
	CreatedAt           time.Time      `json:"createdAt"`
	UpdatedAt           time.Time      `json:"updatedAt"`
}

type AIConnectionSecret struct {
	ConnectionID         string
	OwnerUserID          string
	Provider             string
	Ciphertext           string
	Nonce                string
	Mask                 string
	Fingerprint          string
	EncryptionKeyID      string
	FingerprintKeyID     string
	Algorithm            string
	Version              int
	SecretType           string
	PayloadFormat        string
	SubjectFingerprint   string
	ExpiresAt            *time.Time
	NextRefreshAt        *time.Time
	LastRefreshedAt      *time.Time
	RefreshFailures      int
	LastRefreshErrorCode string
}

type AIConnectionCreateInput struct {
	OwnerUserID            string
	OrgID                  string
	Provider               string
	ProductLine            string
	Region                 string
	WorkspaceID            string
	Protocol               string
	AdapterType            string
	Endpoint               string
	ProviderConfig         map[string]any
	DisplayName            string
	Models                 []string
	Credential             AIConnectionSecret
	Validation             AIConnectionValidation
	AuthMethod             string
	AuthStatus             string
	SharingScope           string
	RiskLevel              string
	ProviderAdapterVersion string
	TermsAckVersion        string
	AccountMask            string
	AuthorizationID        string
}

type AIConnectionValidation struct {
	OK                bool
	Stage             string
	LatencyMs         int
	ModelCount        int
	ErrorCode         string
	ErrorMessage      string
	BillableConfirmed bool
	Models            []AIConnectionModelValidation
}

type AIConnectionModelValidation struct {
	ProviderModelID string
	OK              bool
	LatencyMs       int
	ErrorCode       string
	ErrorMessage    string
}

type QuickRelayReveal struct {
	Ciphertext       string
	Nonce            string
	EncryptionKeyID  string
	Fingerprint      string
	FingerprintKeyID string
	Mask             string
}

type QuickRelayInput struct {
	OwnerUserID    string
	OrgID          string
	ConnectionID   string
	ModelIDs       []string
	Name           string
	Policy         string
	QPSLimit       int
	QuotaMonth     int
	BaseURL        string
	IdempotencyKey string
	RequestHash    string
	PlainKey       string
	Reveal         QuickRelayReveal
}

type QuickRelayResult struct {
	Gateway Gateway          `json:"gateway"`
	Key     GatewayKey       `json:"key"`
	Reveal  QuickRelayReveal `json:"-"`
	Replay  bool             `json:"replay"`
}

func (r *Repository) CreateAIConnection(ctx context.Context, input AIConnectionCreateInput) (AIConnection, error) {
	models := uniqueStrings(input.Models)
	if len(models) == 0 {
		return AIConnection{}, fmt.Errorf("at least one model is required")
	}
	displayName := strings.TrimSpace(input.DisplayName)
	if displayName == "" {
		displayName = strings.TrimSpace(input.ProductLine)
	}
	providerConfig, err := json.Marshal(input.ProviderConfig)
	if err != nil {
		return AIConnection{}, err
	}
	connectionID := "aic_" + uuid.NewString()
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return AIConnection{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := ensureActiveOrg(ctx, tx, input.OrgID); err != nil {
		return AIConnection{}, err
	}
	connectionLimitLock := input.OwnerUserID + "\x1f" + input.OrgID + "\x1fai-connections"
	if _, err := tx.Exec(ctx, `select pg_advisory_xact_lock(hashtextextended($1,0))`, connectionLimitLock); err != nil {
		return AIConnection{}, err
	}
	var connectionCount int
	if err := tx.QueryRow(ctx, `
		select count(*) from ai_connections
		where owner_user_id=$1 and org_id=$2 and status <> 'deleted' and deleted_at is null
	`, input.OwnerUserID, input.OrgID).Scan(&connectionCount); err != nil {
		return AIConnection{}, err
	}
	if connectionCount >= 32 {
		return AIConnection{}, ErrAIConnectionLimit
	}
	authMethod := strings.TrimSpace(input.AuthMethod)
	if authMethod == "" {
		authMethod = "api_key"
	}
	if err := rejectDuplicateAIConnectionCredentialTx(ctx, tx, input.OwnerUserID, input.OrgID, input.Provider, input.Endpoint, input.Credential, ""); err != nil {
		return AIConnection{}, err
	}
	if authMethod == "opencli_browser" {
		if err := rejectDuplicateOpenCLIBrowserProviderTx(
			ctx, tx, input.OwnerUserID, input.OrgID, input.Provider, "",
		); err != nil {
			return AIConnection{}, err
		}
	}
	status := "active"
	if !input.Validation.OK {
		status = "attention"
	}
	authStatus := strings.TrimSpace(input.AuthStatus)
	if authStatus == "" {
		authStatus = "active"
	}
	sharingScope := strings.TrimSpace(input.SharingScope)
	if sharingScope == "" {
		sharingScope = "personal"
	}
	riskLevel := strings.TrimSpace(input.RiskLevel)
	if riskLevel == "" {
		riskLevel = "standard"
	}
	adapterVersion := strings.TrimSpace(input.ProviderAdapterVersion)
	if adapterVersion == "" {
		adapterVersion = "api-key-v1"
	}
	if _, err := tx.Exec(ctx, `
		insert into ai_connections(
			id,owner_user_id,org_id,provider,product_line,region,workspace_id,auth_method,
			protocol,adapter_type,endpoint,provider_config,display_name,status,validation_stage,
			validation_latency_ms,model_count,last_error_code,last_error_message,last_validated_at,
			auth_status,sharing_scope,risk_level,provider_adapter_version,terms_ack_version,account_mask
		)
		values($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,now(),
			$20,$21,$22,$23,$24,$25)
	`, connectionID, input.OwnerUserID, input.OrgID, input.Provider, input.ProductLine, input.Region,
		input.WorkspaceID, authMethod, input.Protocol, input.AdapterType, input.Endpoint, providerConfig, displayName,
		status, input.Validation.Stage, input.Validation.LatencyMs, input.Validation.ModelCount,
		input.Validation.ErrorCode, input.Validation.ErrorMessage, authStatus, sharingScope, riskLevel,
		adapterVersion, strings.TrimSpace(input.TermsAckVersion), strings.TrimSpace(input.AccountMask)); err != nil {
		return AIConnection{}, err
	}
	secret := input.Credential
	secretType := strings.TrimSpace(secret.SecretType)
	if secretType == "" {
		secretType = "api_key"
	}
	payloadFormat := strings.TrimSpace(secret.PayloadFormat)
	if payloadFormat == "" {
		payloadFormat = "opaque"
	}
	if _, err := tx.Exec(ctx, `
		insert into ai_connection_secrets(
			connection_id,ciphertext,nonce,mask,fingerprint,encryption_key_id,
			fingerprint_key_id,algorithm,version,secret_type,payload_format,subject_fingerprint,
			expires_at,next_refresh_at,last_refreshed_at,refresh_failures,last_refresh_error_code,
			rotated_at,created_at,updated_at
		)
		values($1,$2,$3,$4,$5,$6,$7,$8,1,$9,$10,$11,$12,$13,$14,$15,$16,now(),now(),now())
	`, connectionID, secret.Ciphertext, secret.Nonce, secret.Mask, secret.Fingerprint,
		secret.EncryptionKeyID, secret.FingerprintKeyID, secret.Algorithm, secretType, payloadFormat,
		secret.SubjectFingerprint, secret.ExpiresAt, secret.NextRefreshAt, secret.LastRefreshedAt,
		secret.RefreshFailures, secret.LastRefreshErrorCode); err != nil {
		return AIConnection{}, err
	}
	for _, model := range models {
		modelValidation := aiConnectionModelValidation(input.Validation, model)
		verificationStatus := "unverified"
		if modelValidation.OK {
			verificationStatus = "verified"
		}
		if _, err := tx.Exec(ctx, `
			insert into ai_connection_models(
				id,connection_id,provider_model_id,display_name,enabled,verification_status,
				validation_latency_ms,last_error_code,last_error_message,last_validated_at,created_at,updated_at
			)
			values($1,$2,$3,$3,true,$4,$5,$6,$7,now(),now(),now())
		`, "aicm_"+uuid.NewString(), connectionID, model, verificationStatus,
			modelValidation.LatencyMs, modelValidation.ErrorCode, modelValidation.ErrorMessage); err != nil {
			return AIConnection{}, err
		}
	}
	if err := completeAIAuthorizationAttemptTx(ctx, tx, input.AuthorizationID, input.OwnerUserID, connectionID); err != nil {
		return AIConnection{}, err
	}
	if err := writeAuditTx(ctx, tx, AuditEvent{
		ActorType: "user", ActorID: input.OwnerUserID, Action: "ai_connection.created",
		ObjectType: "ai_connection", ObjectID: connectionID, Result: map[bool]string{true: "success", false: "failed"}[input.Validation.OK],
		Metadata: map[string]any{
			"provider": input.Provider, "region": input.Region, "models": len(models),
			"policy_version":                "ai-authorization-v2",
			"auth_method":                   authMethod,
			"sharing_scope":                 sharingScope,
			"billable_validation_confirmed": input.Validation.BillableConfirmed,
		},
	}); err != nil {
		return AIConnection{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return AIConnection{}, err
	}
	return r.AIConnectionForOwnerOrg(ctx, input.OwnerUserID, input.OrgID, connectionID)
}

func (r *Repository) AIConnectionsForOwnerOrg(ctx context.Context, ownerUserID string, orgID string) ([]AIConnection, error) {
	rows, err := r.db.Query(ctx, aiConnectionSelect+`
		where c.owner_user_id=$1 and c.org_id=$2 and c.status <> 'deleted' and c.deleted_at is null
		order by c.created_at desc
	`, ownerUserID, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []AIConnection{}
	for rows.Next() {
		item, err := scanAIConnection(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for idx := range items {
		items[idx].Models, err = r.aiConnectionModels(ctx, items[idx].ID)
		if err != nil {
			return nil, err
		}
	}
	return items, nil
}

func (r *Repository) AIConnectionForOwnerOrg(ctx context.Context, ownerUserID string, orgID string, connectionID string) (AIConnection, error) {
	item, err := scanAIConnection(r.db.QueryRow(ctx, aiConnectionSelect+`
		where c.id=$1 and c.owner_user_id=$2 and c.org_id=$3 and c.status <> 'deleted' and c.deleted_at is null
	`, connectionID, ownerUserID, orgID))
	if err != nil {
		return AIConnection{}, err
	}
	item.Models, err = r.aiConnectionModels(ctx, item.ID)
	return item, err
}

func (r *Repository) AIConnectionSecretForOwnerOrg(ctx context.Context, ownerUserID string, orgID string, connectionID string) (AIConnectionSecret, error) {
	var item AIConnectionSecret
	err := r.db.QueryRow(ctx, `
		select s.connection_id,c.owner_user_id,c.provider,s.ciphertext,s.nonce,s.mask,s.fingerprint,
			s.encryption_key_id,s.fingerprint_key_id,s.algorithm,s.version,s.secret_type,s.payload_format,
			s.subject_fingerprint,s.expires_at,s.next_refresh_at,s.last_refreshed_at,
			s.refresh_failures,s.last_refresh_error_code
		from ai_connection_secrets s
		join ai_connections c on c.id=s.connection_id
		where c.id=$1 and c.owner_user_id=$2 and c.org_id=$3
			and c.status <> 'deleted' and c.deleted_at is null
	`, connectionID, ownerUserID, orgID).Scan(
		&item.ConnectionID, &item.OwnerUserID, &item.Provider, &item.Ciphertext, &item.Nonce,
		&item.Mask, &item.Fingerprint, &item.EncryptionKeyID, &item.FingerprintKeyID,
		&item.Algorithm, &item.Version, &item.SecretType, &item.PayloadFormat,
		&item.SubjectFingerprint, nullableTimePtr(&item.ExpiresAt), nullableTimePtr(&item.NextRefreshAt),
		nullableTimePtr(&item.LastRefreshedAt), &item.RefreshFailures, &item.LastRefreshErrorCode,
	)
	return item, err
}

func (r *Repository) AIConnectionCredentialExists(ctx context.Context, ownerUserID string, orgID string, provider string, endpoint string, credential AIConnectionSecret, excludeConnectionID string) (bool, error) {
	var duplicate bool
	err := r.db.QueryRow(ctx, `
		select exists(
			select 1
			from ai_connections c
			join ai_connection_secrets s on s.connection_id=c.id
			where c.owner_user_id=$1 and c.org_id=$2 and c.provider=$3 and c.endpoint=$4
				and c.id <> $5 and c.status <> 'deleted' and c.deleted_at is null
				and s.fingerprint_key_id=$6 and s.fingerprint=$7
		)
	`, ownerUserID, orgID, provider, endpoint, excludeConnectionID,
		credential.FingerprintKeyID, credential.Fingerprint).Scan(&duplicate)
	return duplicate, err
}

func (r *Repository) UpdateAIConnectionValidation(ctx context.Context, ownerUserID string, orgID string, connectionID string, expectedCredentialVersion int, validation AIConnectionValidation) (AIConnection, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return AIConnection{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var locked bool
	err = tx.QueryRow(ctx, `
		select true
		from ai_connections c
		join ai_connection_secrets s on s.connection_id=c.id
		where c.id=$1 and c.owner_user_id=$2 and c.org_id=$3
			and c.status <> 'deleted' and c.deleted_at is null
			and s.version=$4
		for update of c,s
	`, connectionID, ownerUserID, orgID, expectedCredentialVersion).Scan(&locked)
	if err != nil {
		return AIConnection{}, err
	}
	status := "active"
	if !validation.OK {
		status = "attention"
	}
	_, err = tx.Exec(ctx, `
		update ai_connections
		set status=$4,validation_stage=$5,validation_latency_ms=$6,model_count=$7,
			last_error_code=$8,last_error_message=$9,last_validated_at=now(),updated_at=now()
		where id=$1 and owner_user_id=$2 and org_id=$3 and status <> 'deleted' and deleted_at is null
	`, connectionID, ownerUserID, orgID, status, validation.Stage, validation.LatencyMs,
		validation.ModelCount, validation.ErrorCode, validation.ErrorMessage)
	if err != nil {
		return AIConnection{}, err
	}
	if err := applyAIConnectionModelValidationTx(ctx, tx, connectionID, validation); err != nil {
		return AIConnection{}, err
	}
	if err := writeAuditTx(ctx, tx, AuditEvent{
		ActorType: "user", ActorID: ownerUserID, Action: "ai_connection.validated",
		ObjectType: "ai_connection", ObjectID: connectionID,
		Result: map[bool]string{true: "success", false: "failed"}[validation.OK],
		Metadata: map[string]any{
			"stage": validation.Stage, "latency_ms": validation.LatencyMs,
			"model_count": validation.ModelCount, "error_code": validation.ErrorCode,
			"billable_validation_confirmed": validation.BillableConfirmed,
		},
	}); err != nil {
		return AIConnection{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return AIConnection{}, err
	}
	return r.AIConnectionForOwnerOrg(ctx, ownerUserID, orgID, connectionID)
}

func (r *Repository) RotateAIConnectionSecret(ctx context.Context, ownerUserID string, orgID string, connectionID string, credential AIConnectionSecret, validation AIConnectionValidation) (AIConnection, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return AIConnection{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var locked bool
	if err := tx.QueryRow(ctx, `
		select true
		from ai_connections
		where id=$1 and owner_user_id=$2 and org_id=$3
			and status <> 'deleted' and deleted_at is null
		for update
	`, connectionID, ownerUserID, orgID).Scan(&locked); err != nil {
		return AIConnection{}, err
	}
	var provider, endpoint, authMethod string
	if err := tx.QueryRow(ctx, `select provider,endpoint,auth_method from ai_connections where id=$1`, connectionID).Scan(&provider, &endpoint, &authMethod); err != nil {
		return AIConnection{}, err
	}
	if !supportsAIConnectionSecretRotation(authMethod) {
		return AIConnection{}, ErrAIConnectionCredentialRotationUnsupported
	}
	if err := rejectDuplicateAIConnectionCredentialTx(ctx, tx, ownerUserID, orgID, provider, endpoint, credential, connectionID); err != nil {
		return AIConnection{}, err
	}
	if _, err := tx.Exec(ctx, `
		update ai_connection_secrets
		set ciphertext=$2,nonce=$3,mask=$4,fingerprint=$5,encryption_key_id=$6,
			fingerprint_key_id=$7,algorithm=$8,version=version+1,rotated_at=now(),updated_at=now()
		where connection_id=$1
	`, connectionID, credential.Ciphertext, credential.Nonce, credential.Mask, credential.Fingerprint,
		credential.EncryptionKeyID, credential.FingerprintKeyID, credential.Algorithm); err != nil {
		return AIConnection{}, err
	}
	status := "active"
	if !validation.OK {
		status = "attention"
	}
	if _, err := tx.Exec(ctx, `
		update ai_connections
		set status=$2,validation_stage=$3,validation_latency_ms=$4,model_count=$5,
			last_error_code=$6,last_error_message=$7,last_validated_at=now(),updated_at=now()
		where id=$1
	`, connectionID, status, validation.Stage, validation.LatencyMs, validation.ModelCount,
		validation.ErrorCode, validation.ErrorMessage); err != nil {
		return AIConnection{}, err
	}
	if err := applyAIConnectionModelValidationTx(ctx, tx, connectionID, validation); err != nil {
		return AIConnection{}, err
	}
	if err := writeAuditTx(ctx, tx, AuditEvent{
		ActorType: "user", ActorID: ownerUserID, Action: "ai_connection.credential_rotated",
		ObjectType: "ai_connection", ObjectID: connectionID, Result: map[bool]string{true: "success", false: "failed"}[validation.OK],
		Metadata: map[string]any{
			"fingerprint_key_id":            credential.FingerprintKeyID,
			"encryption_key_id":             credential.EncryptionKeyID,
			"billable_validation_confirmed": validation.BillableConfirmed,
		},
	}); err != nil {
		return AIConnection{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return AIConnection{}, err
	}
	return r.AIConnectionForOwnerOrg(ctx, ownerUserID, orgID, connectionID)
}

func supportsAIConnectionSecretRotation(authMethod string) bool {
	switch strings.ToLower(strings.TrimSpace(authMethod)) {
	case "api_key", "api_key_guided":
		return true
	default:
		return false
	}
}

func (r *Repository) DeleteAIConnection(ctx context.Context, ownerUserID string, orgID string, connectionID string) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	tag, err := tx.Exec(ctx, `
		update ai_connections
		set status='deleted',deleted_at=now(),updated_at=now()
		where id=$1 and owner_user_id=$2 and org_id=$3 and status <> 'deleted' and deleted_at is null
	`, connectionID, ownerUserID, orgID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	if _, err := tx.Exec(ctx, `
		update gateway_upstreams set enabled=false
		where channel_id in (select id from channels where ai_connection_id=$1)
	`, connectionID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		update gateways g
		set status='paused',updated_at=now()
		where g.id in (
				select distinct gu.gateway_id
				from gateway_upstreams gu
				join channels c on c.id=gu.channel_id
				where c.ai_connection_id=$1
			)
			and g.status='active'
			and not exists (
				select 1 from gateway_upstreams remaining
				where remaining.gateway_id=g.id and remaining.enabled=true
			)
	`, connectionID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		update gateway_keys k
		set status='revoked',revoked_at=coalesce(revoked_at,now())
		where k.gateway_id in (
				select distinct gu.gateway_id
				from gateway_upstreams gu
				join channels c on c.id=gu.channel_id
				where c.ai_connection_id=$1
			)
			and k.status='active'
			and exists (
				select 1 from gateways g where g.id=k.gateway_id and g.status='paused'
			)
	`, connectionID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		update channels
		set status='deleted',gateway_enabled=false,disabled_at=coalesce(disabled_at,now()),
			deleted_at=coalesce(deleted_at,now()),updated_at=now()
		where ai_connection_id=$1 and status <> 'deleted'
	`, connectionID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		update ai_connection_secrets
		set ciphertext='',nonce='',mask='deleted',fingerprint='deleted:' || connection_id,
			version=version+1,updated_at=now()
		where connection_id=$1
	`, connectionID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		update ai_quick_relay_requests
		set status='expired',reveal_ciphertext='',reveal_nonce='',
			reveal_encryption_key_id='',reveal_fingerprint='',
			reveal_fingerprint_key_id='',reveal_mask='expired'
		where connection_id=$1 and status='completed'
	`, connectionID); err != nil {
		return err
	}
	if err := writeAuditTx(ctx, tx, AuditEvent{
		ActorType: "user", ActorID: ownerUserID, Action: "ai_connection.deleted",
		ObjectType: "ai_connection", ObjectID: connectionID, Result: "success",
		Metadata: map[string]any{
			"credential_scrubbed": true, "managed_channels_disabled": true,
			"empty_gateways_paused": true, "gateway_keys_revoked": true,
		},
	}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (r *Repository) CreateQuickRelay(ctx context.Context, input QuickRelayInput) (QuickRelayResult, error) {
	modelIDs := uniqueStrings(input.ModelIDs)
	if len(modelIDs) == 0 {
		return QuickRelayResult{}, fmt.Errorf("at least one connection model is required")
	}
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return QuickRelayResult{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	lockKey := input.OwnerUserID + "\x1f" + input.OrgID + "\x1f" + input.IdempotencyKey
	if _, err := tx.Exec(ctx, `select pg_advisory_xact_lock(hashtextextended($1,0))`, lockKey); err != nil {
		return QuickRelayResult{}, err
	}
	if existing, ok, err := quickRelayReplayTx(ctx, tx, input); err != nil {
		return QuickRelayResult{}, err
	} else if ok {
		if err := tx.Commit(ctx); err != nil {
			return QuickRelayResult{}, err
		}
		existing.Gateway, err = r.GatewayByIDForOrg(ctx, existing.Gateway.ID, input.OrgID)
		if err != nil {
			return QuickRelayResult{}, err
		}
		return existing, nil
	}
	var connection AIConnection
	var providerConfigRaw []byte
	err = tx.QueryRow(ctx, `
		select id,owner_user_id,org_id,provider,product_line,region,workspace_id,protocol,adapter_type,
			endpoint,provider_config,display_name,status,auth_method,auth_status,risk_level
		from ai_connections
		where id=$1 and owner_user_id=$2 and org_id=$3 and status in ('active','attention')
			and auth_status in ('active','refreshing') and deleted_at is null
		for update
	`, input.ConnectionID, input.OwnerUserID, input.OrgID).Scan(
		&connection.ID, &connection.OwnerUserID, &connection.OrgID, &connection.Provider,
		&connection.ProductLine, &connection.Region, &connection.WorkspaceID, &connection.Protocol,
		&connection.AdapterType, &connection.Endpoint, &providerConfigRaw, &connection.DisplayName,
		&connection.Status, &connection.AuthMethod, &connection.AuthStatus, &connection.RiskLevel,
	)
	if err != nil {
		return QuickRelayResult{}, err
	}
	connection.ProviderConfig = decodeMap(providerConfigRaw)
	if connection.AuthMethod == "codex_oauth" || connection.AuthMethod == "deepseek_web_token" || connection.AuthMethod == "opencli_browser" {
		var relayExists bool
		if err := tx.QueryRow(ctx, `
			select exists(
				select 1
				from gateways g
				join gateway_upstreams gu on gu.gateway_id=g.id
				join channels c on c.id=gu.channel_id
				where c.ai_connection_id=$1 and g.status in ('active','paused')
			)
		`, connection.ID).Scan(&relayExists); err != nil {
			return QuickRelayResult{}, err
		}
		if relayExists {
			return QuickRelayResult{}, ErrExperimentalRelayExists
		}
		input.QPSLimit = 1
	}
	rows, err := tx.Query(ctx, `
		select id,provider_model_id,coalesce(route_channel_id,'')
		from ai_connection_models
		where connection_id=$1 and id=any($2) and enabled=true and verification_status='verified'
		order by created_at asc
		for update
	`, connection.ID, modelIDs)
	if err != nil {
		return QuickRelayResult{}, err
	}
	type selectedModel struct{ id, model, channelID string }
	selected := []selectedModel{}
	for rows.Next() {
		var item selectedModel
		if err := rows.Scan(&item.id, &item.model, &item.channelID); err != nil {
			rows.Close()
			return QuickRelayResult{}, err
		}
		selected = append(selected, item)
	}
	rows.Close()
	if len(selected) != len(modelIDs) {
		return QuickRelayResult{}, fmt.Errorf("one or more connection models are unavailable")
	}
	channelIDs := make([]string, 0, len(selected))
	providerConfig, err := json.Marshal(connection.ProviderConfig)
	if err != nil {
		return QuickRelayResult{}, err
	}
	for idx := range selected {
		if selected[idx].channelID == "" {
			selected[idx].channelID = "aich_" + uuid.NewString()
			if _, err := tx.Exec(ctx, `
				insert into channels(
					id,owner_type,owner_id,org_id,name,provider,type,model,upstream_model,endpoint,
					status,score,probe_daily,probes_used_today,probe_reset_date,public_visible,
					gateway_enabled,provider_config,data_origin,ai_connection_id,ai_connection_model_id,
					managed_source,created_at,updated_at
				)
				values(
					$1,'user',$2,$3,$4,$5,$6,$7,$7,$8,
					'healthy',100,0,0,current_date,false,true,$9,'runtime',$10,$11,
					'ai_connection',now(),now()
				)
			`, selected[idx].channelID, input.OwnerUserID, input.OrgID,
				connection.DisplayName+" · "+selected[idx].model, connection.Provider,
				connection.AdapterType, selected[idx].model, connection.Endpoint, providerConfig,
				connection.ID, selected[idx].id); err != nil {
				return QuickRelayResult{}, err
			}
			if _, err := tx.Exec(ctx, `
				insert into channel_status_snapshots(
					id,channel_id,sampled_at,status,score,uptime_24h,success_rate,latency_p95_ms,
					l1_status,l2_status,l3_status,l1_latency_ms,l2_latency_ms,l3_latency_ms,
					tokens_used,cost_usd,error_type,metadata
				)
				values($1,$2,now(),'healthy',100,100,100,0,'ok','ok','ok',0,0,0,0,0,null,
					jsonb_build_object('source','ai_connection_validation','connection_id',$3::text))
			`, "snap_"+uuid.NewString(), selected[idx].channelID, connection.ID); err != nil {
				return QuickRelayResult{}, err
			}
			if _, err := tx.Exec(ctx, `
				update ai_connection_models set route_channel_id=$2,updated_at=now() where id=$1
			`, selected[idx].id, selected[idx].channelID); err != nil {
				return QuickRelayResult{}, err
			}
		}
		channelIDs = append(channelIDs, selected[idx].channelID)
	}
	name := strings.TrimSpace(input.Name)
	if name == "" {
		name = connection.DisplayName + " 个人中转"
	}
	qps := input.QPSLimit
	if qps <= 0 {
		qps = 20
	}
	quota := input.QuotaMonth
	if quota <= 0 {
		quota = 100000
	}
	gatewayID := "gw_" + uuid.NewString()
	if _, err := tx.Exec(ctx, `
		insert into gateways(id,org_id,name,slug,base_url,policy,status,qps_limit,quota_month,created_by,created_at,updated_at)
		values($1,$2,$3,$4,$5,$6,'active',$7,$8,$9,now(),now())
	`, gatewayID, input.OrgID, name, UniqueSlug(name), input.BaseURL, normalizeGatewayPolicy(input.Policy), qps, quota, input.OwnerUserID); err != nil {
		return QuickRelayResult{}, err
	}
	for idx, channelID := range channelIDs {
		if _, err := tx.Exec(ctx, `
			insert into gateway_upstreams(id,gateway_id,channel_id,weight,priority,enabled,created_at)
			values($1,$2,$3,100,$4,true,now())
		`, "gwu_"+uuid.NewString(), gatewayID, channelID, idx); err != nil {
			return QuickRelayResult{}, err
		}
	}
	var key GatewayKey
	err = tx.QueryRow(ctx, `
		insert into gateway_keys(
			id,org_id,gateway_id,name,key_hash,key_prefix,key_mask,key_ciphertext,key_nonce,
			quota_month,qps_limit,status,created_by,created_at
		)
		values($1,$2,$3,'默认调用密钥',$4,$5,$6,'','',$7,$8,'active',$9,now())
		returning id,org_id,gateway_id,name,key_prefix,key_mask,quota_month,qps_limit,
			requests_used,status,created_at,last_used_at,expires_at
	`, "gwk_"+uuid.NewString(), input.OrgID, gatewayID, HashGatewayKey(input.PlainKey),
		GatewayKeyPrefix(input.PlainKey), MaskGatewayKey(input.PlainKey), quota, qps, input.OwnerUserID).
		Scan(&key.ID, &key.OrgID, &key.GatewayID, &key.Name, &key.KeyPrefix, &key.KeyMask,
			&key.QuotaMonth, &key.QPSLimit, &key.RequestsUsed, &key.Status, &key.CreatedAt,
			nullableTimePtr(&key.LastUsedAt), nullableTimePtr(&key.ExpiresAt))
	if err != nil {
		return QuickRelayResult{}, err
	}
	key.GatewayName = name
	key.PlainKey = input.PlainKey
	if _, err := tx.Exec(ctx, `
		insert into ai_quick_relay_requests(
			id,owner_user_id,org_id,idempotency_key,request_hash,connection_id,gateway_id,gateway_key_id,
			reveal_ciphertext,reveal_nonce,reveal_encryption_key_id,reveal_fingerprint,
			reveal_fingerprint_key_id,reveal_mask,status,expires_at,created_at
		)
		values($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,'completed',now()+interval '10 minutes',now())
	`, "aiqr_"+uuid.NewString(), input.OwnerUserID, input.OrgID, input.IdempotencyKey,
		input.RequestHash, input.ConnectionID, gatewayID, key.ID, input.Reveal.Ciphertext,
		input.Reveal.Nonce, input.Reveal.EncryptionKeyID, input.Reveal.Fingerprint,
		input.Reveal.FingerprintKeyID, input.Reveal.Mask); err != nil {
		return QuickRelayResult{}, err
	}
	if err := writeAuditTx(ctx, tx, AuditEvent{
		ActorType: "user", ActorID: input.OwnerUserID, Action: "ai_connection.quick_relay_created",
		ObjectType: "gateway", ObjectID: gatewayID, Result: "success",
		Metadata: map[string]any{"connection_id": input.ConnectionID, "models": len(modelIDs), "idempotency_key": input.IdempotencyKey},
	}); err != nil {
		return QuickRelayResult{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return QuickRelayResult{}, err
	}
	gateway, err := r.GatewayByIDForOrg(ctx, gatewayID, input.OrgID)
	if err != nil {
		return QuickRelayResult{}, err
	}
	return QuickRelayResult{Gateway: gateway, Key: key, Reveal: input.Reveal}, nil
}

func (r *Repository) ExpireAIQuickRelayReveals(ctx context.Context) (int64, error) {
	tag, err := r.db.Exec(ctx, `
		update ai_quick_relay_requests
		set status='expired',reveal_ciphertext='',reveal_nonce='',
			reveal_encryption_key_id='',reveal_fingerprint='',
			reveal_fingerprint_key_id='',reveal_mask='expired'
		where status='completed' and expires_at <= now()
	`)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

func quickRelayReplayTx(ctx context.Context, tx pgx.Tx, input QuickRelayInput) (QuickRelayResult, bool, error) {
	var requestHash, gatewayID, requestStatus string
	var key GatewayKey
	var reveal QuickRelayReveal
	var expiresAt time.Time
	err := tx.QueryRow(ctx, `
		select q.request_hash,coalesce(q.gateway_id,''),coalesce(q.gateway_key_id,''),
			q.reveal_ciphertext,q.reveal_nonce,q.reveal_encryption_key_id,q.reveal_fingerprint,
			q.reveal_fingerprint_key_id,q.reveal_mask,q.status,q.expires_at,
			coalesce(k.name,''),coalesce(k.key_prefix,''),coalesce(k.key_mask,''),
			coalesce(k.quota_month,0),coalesce(k.qps_limit,0),coalesce(k.requests_used,0),
			coalesce(k.status,''),coalesce(k.created_at,q.created_at),k.last_used_at,k.expires_at
		from ai_quick_relay_requests q
		left join gateway_keys k on k.id=q.gateway_key_id
		where q.owner_user_id=$1 and q.org_id=$2 and q.idempotency_key=$3
		for update of q
	`, input.OwnerUserID, input.OrgID, input.IdempotencyKey).Scan(
		&requestHash, &gatewayID, &key.ID, &reveal.Ciphertext, &reveal.Nonce,
		&reveal.EncryptionKeyID, &reveal.Fingerprint, &reveal.FingerprintKeyID,
		&reveal.Mask, &requestStatus, &expiresAt, &key.Name, &key.KeyPrefix, &key.KeyMask,
		&key.QuotaMonth, &key.QPSLimit, &key.RequestsUsed, &key.Status, &key.CreatedAt,
		nullableTimePtr(&key.LastUsedAt), nullableTimePtr(&key.ExpiresAt),
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return QuickRelayResult{}, false, nil
	}
	if err != nil {
		return QuickRelayResult{}, false, err
	}
	if requestHash != input.RequestHash {
		return QuickRelayResult{}, false, ErrIdempotencyConflict
	}
	if requestStatus != "completed" || reveal.Ciphertext == "" || time.Now().After(expiresAt) {
		_, _ = tx.Exec(ctx, `
			update ai_quick_relay_requests
			set status='expired',reveal_ciphertext='',reveal_nonce='',
				reveal_encryption_key_id='',reveal_fingerprint='',
				reveal_fingerprint_key_id='',reveal_mask='expired'
			where owner_user_id=$1 and org_id=$2 and idempotency_key=$3
		`, input.OwnerUserID, input.OrgID, input.IdempotencyKey)
		return QuickRelayResult{}, false, fmt.Errorf("idempotent reveal window expired")
	}
	key.OrgID = input.OrgID
	key.GatewayID = gatewayID
	gateway, err := gatewayByIDForOrgTx(ctx, tx, gatewayID, input.OrgID)
	if err != nil {
		return QuickRelayResult{}, false, err
	}
	key.GatewayName = gateway.Name
	return QuickRelayResult{Gateway: gateway, Key: key, Reveal: reveal, Replay: true}, true, nil
}

func gatewayByIDForOrgTx(ctx context.Context, tx pgx.Tx, gatewayID string, orgID string) (Gateway, error) {
	var item Gateway
	err := tx.QueryRow(ctx, `
		select id,org_id,name,slug,base_url,policy,status,qps_limit,quota_month,created_at
		from gateways where id=$1 and org_id=$2 and status <> 'deleted'
	`, gatewayID, orgID).Scan(&item.ID, &item.OrgID, &item.Name, &item.Slug, &item.BaseURL,
		&item.Policy, &item.Status, &item.QPSLimit, &item.QuotaMonth, &item.CreatedAt)
	item.PolicyLabel = gatewayPolicyLabel(item.Policy)
	return item, err
}

const aiConnectionSelect = `
	select c.id,c.owner_user_id,c.org_id,c.provider,c.product_line,c.region,c.workspace_id,
		c.auth_method,c.protocol,c.adapter_type,c.endpoint,c.provider_config,c.display_name,c.status,
		c.auth_status,c.sharing_scope,c.risk_level,c.provider_adapter_version,c.terms_ack_version,c.account_mask,
		c.validation_stage,c.validation_latency_ms,c.model_count,c.last_error_code,c.last_error_message,
		c.last_validated_at,c.policy_version,c.created_at,c.updated_at,s.mask
	from ai_connections c
	join ai_connection_secrets s on s.connection_id=c.id
`

type aiConnectionRowScanner interface {
	Scan(dest ...any) error
}

func scanAIConnection(row aiConnectionRowScanner) (AIConnection, error) {
	var item AIConnection
	var providerConfigRaw []byte
	err := row.Scan(
		&item.ID, &item.OwnerUserID, &item.OrgID, &item.Provider, &item.ProductLine,
		&item.Region, &item.WorkspaceID, &item.AuthMethod, &item.Protocol, &item.AdapterType,
		&item.Endpoint, &providerConfigRaw, &item.DisplayName, &item.Status,
		&item.AuthStatus, &item.SharingScope, &item.RiskLevel, &item.ProviderAdapterVersion,
		&item.TermsAckVersion, &item.AccountMask,
		&item.ValidationStage, &item.ValidationLatencyMs, &item.ModelCount,
		&item.LastErrorCode, &item.LastErrorMessage, nullableTimePtr(&item.LastValidatedAt),
		&item.PolicyVersion, &item.CreatedAt, &item.UpdatedAt, &item.SecretMask,
	)
	item.ProviderConfig = decodeMap(providerConfigRaw)
	return item, err
}

func (r *Repository) aiConnectionModels(ctx context.Context, connectionID string) ([]AIConnectionModel, error) {
	rows, err := r.db.Query(ctx, `
		select id,connection_id,provider_model_id,display_name,enabled,verification_status,
			validation_latency_ms,last_error_code,last_error_message,last_validated_at,
			capabilities_json,coalesce(route_channel_id,''),created_at,updated_at
		from ai_connection_models where connection_id=$1 order by created_at asc
	`, connectionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []AIConnectionModel{}
	for rows.Next() {
		var item AIConnectionModel
		var capabilitiesRaw []byte
		if err := rows.Scan(&item.ID, &item.ConnectionID, &item.ProviderModelID, &item.DisplayName,
			&item.Enabled, &item.VerificationStatus, &item.ValidationLatencyMs,
			&item.LastErrorCode, &item.LastErrorMessage, nullableTimePtr(&item.LastValidatedAt),
			&capabilitiesRaw, &item.RouteChannelID,
			&item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		item.Capabilities = decodeMap(capabilitiesRaw)
		items = append(items, item)
	}
	return items, rows.Err()
}

func aiConnectionModelValidation(validation AIConnectionValidation, model string) AIConnectionModelValidation {
	for _, result := range validation.Models {
		if result.ProviderModelID == model {
			return result
		}
	}
	return AIConnectionModelValidation{
		ProviderModelID: model, OK: validation.OK, LatencyMs: validation.LatencyMs,
		ErrorCode: validation.ErrorCode, ErrorMessage: validation.ErrorMessage,
	}
}

func applyAIConnectionModelValidationTx(ctx context.Context, tx pgx.Tx, connectionID string, validation AIConnectionValidation) error {
	if len(validation.Models) == 0 {
		status := "unverified"
		if validation.OK {
			status = "verified"
		}
		_, err := tx.Exec(ctx, `
			update ai_connection_models
			set verification_status=$2,validation_latency_ms=$3,last_error_code=$4,
				last_error_message=$5,last_validated_at=now(),updated_at=now()
			where connection_id=$1 and enabled=true
		`, connectionID, status, validation.LatencyMs, validation.ErrorCode, validation.ErrorMessage)
		return err
	}
	if _, err := tx.Exec(ctx, `
		update ai_connection_models
		set verification_status='unverified',validation_latency_ms=0,
			last_error_code='validation_result_missing',
			last_error_message='No validation result was returned for this model',
			last_validated_at=now(),updated_at=now()
		where connection_id=$1 and enabled=true
	`, connectionID); err != nil {
		return err
	}
	for _, result := range validation.Models {
		status := "unverified"
		if result.OK {
			status = "verified"
		}
		tag, err := tx.Exec(ctx, `
			update ai_connection_models
			set verification_status=$3,validation_latency_ms=$4,last_error_code=$5,
				last_error_message=$6,last_validated_at=now(),updated_at=now()
			where connection_id=$1 and provider_model_id=$2 and enabled=true
		`, connectionID, result.ProviderModelID, status, result.LatencyMs,
			result.ErrorCode, result.ErrorMessage)
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 1 {
			return fmt.Errorf("validation result references an unavailable model")
		}
	}
	return nil
}

func rejectDuplicateAIConnectionCredentialTx(ctx context.Context, tx pgx.Tx, ownerUserID string, orgID string, provider string, endpoint string, credential AIConnectionSecret, excludeConnectionID string) error {
	lockKey := strings.Join([]string{
		ownerUserID, orgID, provider, endpoint, credential.FingerprintKeyID, credential.Fingerprint,
	}, "\x1f")
	if _, err := tx.Exec(ctx, `select pg_advisory_xact_lock(hashtextextended($1,0))`, lockKey); err != nil {
		return err
	}
	var duplicate bool
	if err := tx.QueryRow(ctx, `
		select exists(
			select 1
			from ai_connections c
			join ai_connection_secrets s on s.connection_id=c.id
			where c.owner_user_id=$1 and c.org_id=$2 and c.provider=$3 and c.endpoint=$4
				and c.id <> $5 and c.status <> 'deleted' and c.deleted_at is null
				and s.fingerprint_key_id=$6 and s.fingerprint=$7
		)
	`, ownerUserID, orgID, provider, endpoint, excludeConnectionID,
		credential.FingerprintKeyID, credential.Fingerprint).Scan(&duplicate); err != nil {
		return err
	}
	if duplicate {
		return ErrAIConnectionDuplicate
	}
	return nil
}

func rejectDuplicateOpenCLIBrowserProviderTx(
	ctx context.Context,
	tx pgx.Tx,
	ownerUserID string,
	orgID string,
	provider string,
	excludeConnectionID string,
) error {
	provider = strings.ToLower(strings.TrimSpace(provider))
	lockKey := strings.Join([]string{
		"opencli-browser-provider", ownerUserID, orgID, provider,
	}, "\x1f")
	if _, err := tx.Exec(ctx, `select pg_advisory_xact_lock(hashtextextended($1,0))`, lockKey); err != nil {
		return err
	}
	var duplicate bool
	if err := tx.QueryRow(ctx, `
		select exists(
			select 1
			from ai_connections
			where owner_user_id=$1 and org_id=$2 and provider=$3
				and auth_method='opencli_browser' and id <> $4
				and status <> 'deleted' and deleted_at is null
		)
	`, ownerUserID, orgID, provider, excludeConnectionID).Scan(&duplicate); err != nil {
		return err
	}
	if duplicate {
		return ErrAIConnectionDuplicate
	}
	return nil
}
