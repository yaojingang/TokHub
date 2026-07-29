package store

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

type AIAuthorizationAttempt struct {
	ID             string     `json:"id"`
	OwnerUserID    string     `json:"-"`
	OrgID          string     `json:"orgId"`
	Provider       string     `json:"provider"`
	AuthMethod     string     `json:"authMethod"`
	Status         string     `json:"status"`
	CompletionMode string     `json:"completionMode"`
	ConnectionID   string     `json:"connectionId,omitempty"`
	ErrorCode      string     `json:"errorCode,omitempty"`
	ErrorMessage   string     `json:"errorMessage,omitempty"`
	StartedAt      time.Time  `json:"startedAt"`
	CompletedAt    *time.Time `json:"completedAt,omitempty"`
	ExpiresAt      time.Time  `json:"expiresAt"`
}

type AIAuthorizationAttemptInput struct {
	ID             string
	OwnerUserID    string
	OrgID          string
	Provider       string
	AuthMethod     string
	CompletionMode string
	ExpiresAt      time.Time
}

type OAuthRefreshCandidate struct {
	ConnectionID string
	OwnerUserID  string
	OrgID        string
	Provider     string
	AuthMethod   string
	Secret       AIConnectionSecret
}

type AIAuthorizationMetric struct {
	Provider string
	Method   string
	Status   string
	Count    int
}

type AIOAuthConnectionMetric struct {
	Provider   string
	Method     string
	AuthStatus string
	Count      int
}

type AIOAuthRefreshFailureMetric struct {
	Provider string
	Count    int
}

type AIAuthorizationMetrics struct {
	Attempts        []AIAuthorizationMetric
	Connections     []AIOAuthConnectionMetric
	RefreshFailures []AIOAuthRefreshFailureMetric
}

func (r *Repository) AIAuthorizationMetrics(ctx context.Context) (AIAuthorizationMetrics, error) {
	var snapshot AIAuthorizationMetrics
	rows, err := r.db.Query(ctx, `
		select provider,auth_method,status,count(*)
		from ai_authorization_attempts
		group by provider,auth_method,status
		order by provider,auth_method,status
	`)
	if err != nil {
		return snapshot, err
	}
	for rows.Next() {
		var item AIAuthorizationMetric
		if err := rows.Scan(&item.Provider, &item.Method, &item.Status, &item.Count); err != nil {
			rows.Close()
			return snapshot, err
		}
		snapshot.Attempts = append(snapshot.Attempts, item)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return snapshot, err
	}
	rows.Close()

	rows, err = r.db.Query(ctx, `
		select provider,auth_method,auth_status,count(*)
		from ai_connections
		where auth_method in ('oauth','codex_oauth','deepseek_web_token') and status <> 'deleted' and deleted_at is null
		group by provider,auth_method,auth_status
		order by provider,auth_method,auth_status
	`)
	if err != nil {
		return snapshot, err
	}
	for rows.Next() {
		var item AIOAuthConnectionMetric
		if err := rows.Scan(&item.Provider, &item.Method, &item.AuthStatus, &item.Count); err != nil {
			rows.Close()
			return snapshot, err
		}
		snapshot.Connections = append(snapshot.Connections, item)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return snapshot, err
	}
	rows.Close()

	rows, err = r.db.Query(ctx, `
		select c.provider,coalesce(sum(s.refresh_failures),0)
		from ai_connection_secrets s
		join ai_connections c on c.id=s.connection_id
		where s.secret_type='oauth_bundle' and c.status <> 'deleted' and c.deleted_at is null
		group by c.provider
		order by c.provider
	`)
	if err != nil {
		return snapshot, err
	}
	defer rows.Close()
	for rows.Next() {
		var item AIOAuthRefreshFailureMetric
		if err := rows.Scan(&item.Provider, &item.Count); err != nil {
			return snapshot, err
		}
		snapshot.RefreshFailures = append(snapshot.RefreshFailures, item)
	}
	return snapshot, rows.Err()
}

func (r *Repository) CreateAIAuthorizationAttempt(ctx context.Context, input AIAuthorizationAttemptInput) (AIAuthorizationAttempt, error) {
	_, err := r.db.Exec(ctx, `
		insert into ai_authorization_attempts(
			id,owner_user_id,org_id,provider,auth_method,status,completion_mode,expires_at
		)
		values($1,$2,$3,$4,$5,'authorization_pending',$6,$7)
	`, input.ID, input.OwnerUserID, input.OrgID, input.Provider, input.AuthMethod,
		input.CompletionMode, input.ExpiresAt)
	if err != nil {
		return AIAuthorizationAttempt{}, err
	}
	_ = r.WriteAudit(ctx, AuditEvent{
		ActorType: "user", ActorID: input.OwnerUserID, Action: "ai_authorization.started",
		ObjectType: "ai_authorization", ObjectID: input.ID, Result: "success",
		Metadata: map[string]any{
			"provider": input.Provider, "auth_method": input.AuthMethod,
			"completion_mode": input.CompletionMode,
		},
	})
	return r.AIAuthorizationAttemptForOwner(ctx, input.OwnerUserID, input.ID)
}

func (r *Repository) AIAuthorizationAttemptForOwner(ctx context.Context, ownerUserID string, id string) (AIAuthorizationAttempt, error) {
	var attempt AIAuthorizationAttempt
	err := r.db.QueryRow(ctx, `
		select id,owner_user_id,org_id,provider,auth_method,status,completion_mode,
			coalesce(connection_id,''),error_code,error_message,started_at,completed_at,expires_at
		from ai_authorization_attempts
		where id=$1 and owner_user_id=$2
	`, id, ownerUserID).Scan(
		&attempt.ID, &attempt.OwnerUserID, &attempt.OrgID, &attempt.Provider,
		&attempt.AuthMethod, &attempt.Status, &attempt.CompletionMode, &attempt.ConnectionID,
		&attempt.ErrorCode, &attempt.ErrorMessage, &attempt.StartedAt,
		nullableTimePtr(&attempt.CompletedAt), &attempt.ExpiresAt,
	)
	if err != nil {
		return AIAuthorizationAttempt{}, err
	}
	if attempt.Status == "authorization_pending" && !attempt.ExpiresAt.After(time.Now()) {
		_ = r.FailAIAuthorizationAttempt(ctx, attempt.OwnerUserID, attempt.ID, "expired", "Authorization window expired")
		attempt.Status = "expired"
		attempt.ErrorCode = "expired"
		attempt.ErrorMessage = "Authorization window expired"
	}
	return attempt, nil
}

func (r *Repository) SetAIAuthorizationValidating(ctx context.Context, ownerUserID string, id string) error {
	tag, err := r.db.Exec(ctx, `
		update ai_authorization_attempts
		set status='validating',updated_at=now()
		where id=$1 and owner_user_id=$2 and status='authorization_pending' and expires_at>now()
	`, id, ownerUserID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return pgx.ErrNoRows
	}
	return nil
}

func (r *Repository) CompleteAIAuthorizationAttempt(ctx context.Context, ownerUserID string, id string, connectionID string) error {
	tag, err := r.db.Exec(ctx, `
		update ai_authorization_attempts
		set status='completed',connection_id=$3,error_code='',error_message='',
			completed_at=now(),updated_at=now()
		where id=$1 and owner_user_id=$2 and status in ('authorization_pending','validating')
	`, id, ownerUserID, connectionID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return pgx.ErrNoRows
	}
	_ = r.WriteAudit(ctx, AuditEvent{
		ActorType: "user", ActorID: ownerUserID, Action: "ai_authorization.completed",
		ObjectType: "ai_authorization", ObjectID: id, Result: "success",
		Metadata: map[string]any{"connection_id": connectionID},
	})
	return nil
}

func completeAIAuthorizationAttemptTx(ctx context.Context, tx pgx.Tx, id string, ownerUserID string, connectionID string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil
	}
	tag, err := tx.Exec(ctx, `
		update ai_authorization_attempts
		set status='completed',connection_id=$3,error_code='',error_message='',
			completed_at=now(),updated_at=now()
		where id=$1 and owner_user_id=$2
			and status in ('authorization_pending','validating') and expires_at>now()
	`, id, ownerUserID, connectionID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return pgx.ErrNoRows
	}
	return writeAuditTx(ctx, tx, AuditEvent{
		ActorType: "user", ActorID: ownerUserID, Action: "ai_authorization.completed",
		ObjectType: "ai_authorization", ObjectID: id, Result: "success",
		Metadata: map[string]any{"connection_id": connectionID},
	})
}

func (r *Repository) FailAIAuthorizationAttempt(ctx context.Context, ownerUserID string, id string, errorCode string, errorMessage string) error {
	errorCode = strings.TrimSpace(errorCode)
	if errorCode == "" {
		errorCode = "authorization_failed"
	}
	if len(errorMessage) > 240 {
		errorMessage = errorMessage[:240]
	}
	status := "failed"
	if errorCode == "expired" {
		status = "expired"
	}
	tag, err := r.db.Exec(ctx, `
		update ai_authorization_attempts
		set status=$3,error_code=$4,error_message=$5,completed_at=now(),updated_at=now()
		where id=$1 and owner_user_id=$2 and status in ('authorization_pending','validating')
	`, id, ownerUserID, status, errorCode, errorMessage)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	_ = r.WriteAudit(ctx, AuditEvent{
		ActorType: "user", ActorID: ownerUserID, Action: "ai_authorization.failed",
		ObjectType: "ai_authorization", ObjectID: id, Result: "failed",
		Metadata: map[string]any{"error_code": errorCode},
	})
	return nil
}

func (r *Repository) CancelAIAuthorizationAttempt(ctx context.Context, ownerUserID string, id string) error {
	tag, err := r.db.Exec(ctx, `
		update ai_authorization_attempts
		set status='cancelled',completed_at=now(),updated_at=now()
		where id=$1 and owner_user_id=$2 and status='authorization_pending'
	`, id, ownerUserID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return pgx.ErrNoRows
	}
	_ = r.WriteAudit(ctx, AuditEvent{
		ActorType: "user", ActorID: ownerUserID, Action: "ai_authorization.cancelled",
		ObjectType: "ai_authorization", ObjectID: id, Result: "success",
		Metadata: map[string]any{},
	})
	return nil
}

func (r *Repository) OAuthRefreshCandidates(ctx context.Context, limit int) ([]OAuthRefreshCandidate, error) {
	if limit <= 0 || limit > 100 {
		limit = 32
	}
	rows, err := r.db.Query(ctx, `
		select c.id,c.owner_user_id,c.org_id,c.provider,c.auth_method,
			s.connection_id,c.owner_user_id,c.provider,s.ciphertext,s.nonce,s.mask,s.fingerprint,
			s.encryption_key_id,s.fingerprint_key_id,s.algorithm,s.version,s.secret_type,s.payload_format,
			s.subject_fingerprint,s.expires_at,s.next_refresh_at,s.last_refreshed_at,
			s.refresh_failures,s.last_refresh_error_code
		from ai_connections c
		join ai_connection_secrets s on s.connection_id=c.id
		where c.deleted_at is null and c.status in ('active','attention')
			and c.auth_status in ('active','attention')
			and s.secret_type='oauth_bundle'
			and s.next_refresh_at is not null and s.next_refresh_at<=now()
		order by s.next_refresh_at asc
		limit $1
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []OAuthRefreshCandidate{}
	for rows.Next() {
		var item OAuthRefreshCandidate
		if err := rows.Scan(
			&item.ConnectionID, &item.OwnerUserID, &item.OrgID, &item.Provider, &item.AuthMethod,
			&item.Secret.ConnectionID, &item.Secret.OwnerUserID, &item.Secret.Provider,
			&item.Secret.Ciphertext, &item.Secret.Nonce, &item.Secret.Mask, &item.Secret.Fingerprint,
			&item.Secret.EncryptionKeyID, &item.Secret.FingerprintKeyID, &item.Secret.Algorithm,
			&item.Secret.Version, &item.Secret.SecretType, &item.Secret.PayloadFormat,
			&item.Secret.SubjectFingerprint, nullableTimePtr(&item.Secret.ExpiresAt),
			nullableTimePtr(&item.Secret.NextRefreshAt), nullableTimePtr(&item.Secret.LastRefreshedAt),
			&item.Secret.RefreshFailures, &item.Secret.LastRefreshErrorCode,
		); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *Repository) UpdateOAuthConnectionSecret(ctx context.Context, connectionID string, expectedVersion int, secret AIConnectionSecret) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	tag, err := tx.Exec(ctx, `
		update ai_connection_secrets
		set ciphertext=$3,nonce=$4,mask=$5,fingerprint=$6,encryption_key_id=$7,
			fingerprint_key_id=$8,algorithm=$9,subject_fingerprint=$10,
			expires_at=$11,next_refresh_at=$12,last_refreshed_at=now(),
			refresh_failures=0,last_refresh_error_code='',version=version+1,
			rotated_at=now(),updated_at=now()
		where connection_id=$1 and version=$2 and secret_type='oauth_bundle'
	`, connectionID, expectedVersion, secret.Ciphertext, secret.Nonce, secret.Mask,
		secret.Fingerprint, secret.EncryptionKeyID, secret.FingerprintKeyID, secret.Algorithm,
		secret.SubjectFingerprint, secret.ExpiresAt, secret.NextRefreshAt)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return pgx.ErrNoRows
	}
	if _, err := tx.Exec(ctx, `
		update ai_connections
		set auth_status='active',last_error_code='',last_error_message='',updated_at=now()
		where id=$1 and deleted_at is null
	`, connectionID); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	return nil
}

func (r *Repository) ReplaceOAuthAIConnectionAuthorization(ctx context.Context, connectionID string, input AIConnectionCreateInput) (AIConnection, error) {
	providerConfig, err := json.Marshal(input.ProviderConfig)
	if err != nil {
		return AIConnection{}, err
	}
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return AIConnection{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var locked bool
	if err := tx.QueryRow(ctx, `
		select true
		from ai_connections
		where id=$1 and owner_user_id=$2 and org_id=$3 and provider=$4 and auth_method=$5
			and status <> 'deleted' and deleted_at is null
		for update
	`, connectionID, input.OwnerUserID, input.OrgID, input.Provider, input.AuthMethod).Scan(&locked); err != nil {
		return AIConnection{}, err
	}
	status := "active"
	if !input.Validation.OK {
		status = "attention"
	}
	if _, err := tx.Exec(ctx, `
		update ai_connections
		set endpoint=$2,provider_config=$3,display_name=$4,status=$5,auth_status=$6,
			sharing_scope='personal',risk_level=$7,provider_adapter_version=$8,
			terms_ack_version=$9,account_mask=$10,validation_stage=$11,
			validation_latency_ms=$12,model_count=$13,last_error_code=$14,
			last_error_message=$15,last_validated_at=now(),updated_at=now()
		where id=$1
	`, connectionID, input.Endpoint, providerConfig, input.DisplayName, status, "active",
		input.RiskLevel, input.ProviderAdapterVersion, input.TermsAckVersion, input.AccountMask,
		input.Validation.Stage, input.Validation.LatencyMs, input.Validation.ModelCount,
		input.Validation.ErrorCode, input.Validation.ErrorMessage); err != nil {
		return AIConnection{}, err
	}
	secret := input.Credential
	if _, err := tx.Exec(ctx, `
		update ai_connection_secrets
		set ciphertext=$2,nonce=$3,mask=$4,fingerprint=$5,encryption_key_id=$6,
			fingerprint_key_id=$7,algorithm=$8,secret_type='oauth_bundle',
			payload_format='oauth_bundle_v1',subject_fingerprint=$9,expires_at=$10,
			next_refresh_at=$11,last_refreshed_at=now(),refresh_failures=0,
			last_refresh_error_code='',version=version+1,rotated_at=now(),updated_at=now()
		where connection_id=$1
	`, connectionID, secret.Ciphertext, secret.Nonce, secret.Mask, secret.Fingerprint,
		secret.EncryptionKeyID, secret.FingerprintKeyID, secret.Algorithm,
		secret.SubjectFingerprint, secret.ExpiresAt, secret.NextRefreshAt); err != nil {
		return AIConnection{}, err
	}
	if err := applyAIConnectionModelValidationTx(ctx, tx, connectionID, input.Validation); err != nil {
		return AIConnection{}, err
	}
	if err := completeAIAuthorizationAttemptTx(ctx, tx, input.AuthorizationID, input.OwnerUserID, connectionID); err != nil {
		return AIConnection{}, err
	}
	if err := writeAuditTx(ctx, tx, AuditEvent{
		ActorType: "user", ActorID: input.OwnerUserID, Action: "ai_connection.reauthorized",
		ObjectType: "ai_connection", ObjectID: connectionID, Result: map[bool]string{true: "success", false: "failed"}[input.Validation.OK],
		Metadata: map[string]any{
			"provider": input.Provider, "auth_method": input.AuthMethod,
			"adapter_version": input.ProviderAdapterVersion,
		},
	}); err != nil {
		return AIConnection{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return AIConnection{}, err
	}
	return r.AIConnectionForOwnerOrg(ctx, input.OwnerUserID, input.OrgID, connectionID)
}

func (r *Repository) MarkOAuthRefreshFailure(ctx context.Context, connectionID string, expectedVersion int, reauthRequired bool, errorCode string, nextRefresh time.Time) error {
	authStatus := "attention"
	if reauthRequired {
		authStatus = "reauth_required"
	}
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var locked bool
	if err := tx.QueryRow(ctx, `
		select true
		from ai_connections
		where id=$1 and deleted_at is null
		for update
	`, connectionID).Scan(&locked); err != nil {
		return err
	}
	tag, err := tx.Exec(ctx, `
		update ai_connection_secrets
		set refresh_failures=refresh_failures+1,last_refresh_error_code=$2,
			next_refresh_at=case when $3 then null else $4::timestamptz end,updated_at=now()
		where connection_id=$1 and version=$5 and secret_type='oauth_bundle'
	`, connectionID, errorCode, reauthRequired, nextRefresh, expectedVersion)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return pgx.ErrNoRows
	}
	if _, err := tx.Exec(ctx, `
		update ai_connections
		set auth_status=$2,last_error_code=$3,
			last_error_message=case when $2='reauth_required' then '登录授权已失效，请重新登录' else '授权续期暂时失败，系统将自动重试' end,
			updated_at=now()
		where id=$1 and deleted_at is null
	`, connectionID, authStatus, errorCode); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func IsOptimisticCredentialConflict(err error) bool {
	return errors.Is(err, pgx.ErrNoRows)
}
