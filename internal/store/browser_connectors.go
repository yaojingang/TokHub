package store

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

var (
	ErrAIBrowserConnectorPairingInvalid = errors.New("browser connector pairing code is invalid or expired")
	ErrAIBrowserConnectorUnauthorized   = errors.New("browser connector device token is invalid")
	ErrAIBrowserConnectorBusy           = errors.New("browser connector already has an active task")
	ErrAIBrowserTaskLeaseInvalid        = errors.New("browser task lease is invalid or expired")
)

type AIBrowserConnector struct {
	ID               string     `json:"id"`
	OrgID            string     `json:"orgId"`
	DisplayName      string     `json:"displayName"`
	Status           string     `json:"status"`
	Online           bool       `json:"online"`
	TokenPrefix      string     `json:"tokenPrefix,omitempty"`
	OpenCLIVersion   string     `json:"opencliVersion,omitempty"`
	ExtensionVersion string     `json:"extensionVersion,omitempty"`
	Capabilities     []string   `json:"capabilities"`
	PairingExpiresAt *time.Time `json:"pairingExpiresAt,omitempty"`
	LastSeenAt       *time.Time `json:"lastSeenAt,omitempty"`
	PairedAt         *time.Time `json:"pairedAt,omitempty"`
	CreatedAt        time.Time  `json:"createdAt"`
	UpdatedAt        time.Time  `json:"updatedAt"`
}

type AIBrowserConnectorCreateResult struct {
	Connector   AIBrowserConnector `json:"connector"`
	PairingCode string             `json:"pairingCode"`
}

type AIBrowserConnectorPairResult struct {
	Connector   AIBrowserConnector `json:"connector"`
	DeviceToken string             `json:"deviceToken"`
}

type AIBrowserTaskInput struct {
	ConnectorID  string
	OwnerUserID  string
	OrgID        string
	ConnectionID string
	Provider     string
	Action       string
	Request      map[string]any
	ExpiresAt    time.Time
}

type AIBrowserTask struct {
	ID           string         `json:"id"`
	ConnectorID  string         `json:"connectorId"`
	ConnectionID string         `json:"connectionId,omitempty"`
	Provider     string         `json:"provider"`
	Action       string         `json:"action"`
	Request      map[string]any `json:"request,omitempty"`
	Response     map[string]any `json:"response,omitempty"`
	Status       string         `json:"status"`
	LeaseToken   string         `json:"leaseToken,omitempty"`
	ErrorCode    string         `json:"errorCode,omitempty"`
	ErrorMessage string         `json:"errorMessage,omitempty"`
	ExpiresAt    time.Time      `json:"expiresAt"`
	CreatedAt    time.Time      `json:"createdAt"`
	CompletedAt  *time.Time     `json:"completedAt,omitempty"`
}

type AIBrowserRiskPolicy struct {
	MinimumInterval time.Duration
	HourlyLimit     int
	DailyLimit      int
}

type AIBrowserRiskState struct {
	Provider                 string     `json:"provider"`
	State                    string     `json:"state"`
	RequestsHour             int        `json:"requestsHour"`
	RequestsDay              int        `json:"requestsDay"`
	RateLimitEvents          int        `json:"rateLimitEvents"`
	ConsecutiveFailures      int        `json:"consecutiveFailures"`
	HourWindowStartedAt      time.Time  `json:"hourWindowStartedAt"`
	DayWindowStartedAt       time.Time  `json:"dayWindowStartedAt"`
	RateLimitWindowStartedAt time.Time  `json:"rateLimitWindowStartedAt"`
	CooldownUntil            *time.Time `json:"cooldownUntil,omitempty"`
	LastRequestAt            *time.Time `json:"lastRequestAt,omitempty"`
	LastSuccessAt            *time.Time `json:"lastSuccessAt,omitempty"`
	LastErrorAt              *time.Time `json:"lastErrorAt,omitempty"`
	LastErrorCode            string     `json:"lastErrorCode,omitempty"`
	LastChallengeAt          *time.Time `json:"lastChallengeAt,omitempty"`
	UpdatedAt                time.Time  `json:"updatedAt"`
	HourlyLimit              int        `json:"hourlyLimit"`
	DailyLimit               int        `json:"dailyLimit"`
	MinimumIntervalSecond    int        `json:"minimumIntervalSeconds"`
}

type AIBrowserRiskDecision struct {
	Allowed bool               `json:"allowed"`
	Reason  string             `json:"reason,omitempty"`
	RetryAt *time.Time         `json:"retryAt,omitempty"`
	Risk    AIBrowserRiskState `json:"risk"`
}

type browserRiskIdentity struct {
	Provider   string
	AccountKey string
}

func (r *Repository) CreateAIBrowserConnector(ctx context.Context, ownerUserID string, orgID string, displayName string) (AIBrowserConnectorCreateResult, error) {
	displayName = strings.TrimSpace(displayName)
	if displayName == "" {
		displayName = "我的 Chrome"
	}
	if len([]rune(displayName)) > 80 {
		return AIBrowserConnectorCreateResult{}, errors.New("browser connector name is too long")
	}
	pairingCode, err := secureBrowserToken(24)
	if err != nil {
		return AIBrowserConnectorCreateResult{}, err
	}
	id := "aibc_" + uuid.NewString()
	expiresAt := time.Now().Add(10 * time.Minute)
	if _, err := r.db.Exec(ctx, `
		insert into ai_browser_connectors(
			id,owner_user_id,org_id,display_name,status,pairing_hash,pairing_expires_at,created_at,updated_at
		)
		select $1,$2,$3,$4,'pending',$5,$6,now(),now()
		where exists(select 1 from users where id=$2 and status='active' and deleted_at is null)
			and exists(select 1 from orgs where id=$3 and status='active')
			and exists(
				select 1 from org_members
				where org_id=$3 and user_id=$2 and status='active'
			)
	`, id, ownerUserID, orgID, displayName, hashBrowserToken(pairingCode), expiresAt); err != nil {
		return AIBrowserConnectorCreateResult{}, err
	}
	connector, err := r.AIBrowserConnectorForOwner(ctx, ownerUserID, orgID, id)
	if err != nil {
		return AIBrowserConnectorCreateResult{}, err
	}
	return AIBrowserConnectorCreateResult{Connector: connector, PairingCode: pairingCode}, nil
}

func (r *Repository) AIBrowserConnectorsForOwner(ctx context.Context, ownerUserID string, orgID string) ([]AIBrowserConnector, error) {
	rows, err := r.db.Query(ctx, browserConnectorSelect+`
		where owner_user_id=$1 and org_id=$2 and status <> 'revoked'
		order by created_at desc
	`, ownerUserID, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []AIBrowserConnector{}
	for rows.Next() {
		item, err := scanAIBrowserConnector(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *Repository) AIBrowserConnectorForOwner(ctx context.Context, ownerUserID string, orgID string, connectorID string) (AIBrowserConnector, error) {
	return scanAIBrowserConnector(r.db.QueryRow(ctx, browserConnectorSelect+`
		where id=$1 and owner_user_id=$2 and org_id=$3 and status <> 'revoked'
	`, connectorID, ownerUserID, orgID))
}

func (r *Repository) PairAIBrowserConnector(ctx context.Context, pairingCode string) (AIBrowserConnectorPairResult, error) {
	pairingCode = strings.TrimSpace(pairingCode)
	if len(pairingCode) < 32 {
		return AIBrowserConnectorPairResult{}, ErrAIBrowserConnectorPairingInvalid
	}
	deviceToken, err := secureBrowserToken(32)
	if err != nil {
		return AIBrowserConnectorPairResult{}, err
	}
	var id string
	err = r.db.QueryRow(ctx, `
		update ai_browser_connectors as connector
		set status='active',pairing_hash='',pairing_expires_at=null,
			token_hash=$2,token_prefix=$3,paired_at=now(),last_seen_at=now(),updated_at=now()
		where pairing_hash=$1 and pairing_expires_at>now() and status='pending'
			and exists(
				select 1 from users
				where id=connector.owner_user_id and status='active' and deleted_at is null
			)
			and exists(
				select 1 from orgs
				where id=connector.org_id and status='active'
			)
			and exists(
				select 1 from org_members
				where org_id=connector.org_id and user_id=connector.owner_user_id and status='active'
			)
		returning id
	`, hashBrowserToken(pairingCode), hashBrowserToken(deviceToken), tokenPrefix(deviceToken)).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return AIBrowserConnectorPairResult{}, ErrAIBrowserConnectorPairingInvalid
	}
	if err != nil {
		return AIBrowserConnectorPairResult{}, err
	}
	connector, err := r.aiBrowserConnectorByID(ctx, id)
	if err != nil {
		return AIBrowserConnectorPairResult{}, err
	}
	return AIBrowserConnectorPairResult{Connector: connector, DeviceToken: deviceToken}, nil
}

func (r *Repository) AuthenticateAIBrowserConnector(ctx context.Context, deviceToken string) (AIBrowserConnector, error) {
	deviceToken = strings.TrimSpace(deviceToken)
	if len(deviceToken) < 40 {
		return AIBrowserConnector{}, ErrAIBrowserConnectorUnauthorized
	}
	connector, err := scanAIBrowserConnector(r.db.QueryRow(ctx, browserConnectorSelect+`
		where token_hash=$1 and status='active'
			and exists(
				select 1 from users
				where id=ai_browser_connectors.owner_user_id and status='active' and deleted_at is null
			)
			and exists(
				select 1 from orgs
				where id=ai_browser_connectors.org_id and status='active'
			)
			and exists(
				select 1 from org_members
				where org_id=ai_browser_connectors.org_id
					and user_id=ai_browser_connectors.owner_user_id
					and status='active'
			)
	`, hashBrowserToken(deviceToken)))
	if errors.Is(err, pgx.ErrNoRows) {
		return AIBrowserConnector{}, ErrAIBrowserConnectorUnauthorized
	}
	return connector, err
}

func (r *Repository) HeartbeatAIBrowserConnector(ctx context.Context, connectorID string, opencliVersion string, extensionVersion string, capabilities []string) (AIBrowserConnector, error) {
	capabilities = normalizedBrowserCapabilities(capabilities)
	raw, err := json.Marshal(capabilities)
	if err != nil {
		return AIBrowserConnector{}, err
	}
	tag, err := r.db.Exec(ctx, `
		update ai_browser_connectors
		set opencli_version=$2,extension_version=$3,capabilities_json=$4,last_seen_at=now(),updated_at=now()
		where id=$1 and status='active'
	`, connectorID, truncateStoreText(opencliVersion, 64), truncateStoreText(extensionVersion, 64), raw)
	if err != nil {
		return AIBrowserConnector{}, err
	}
	if tag.RowsAffected() != 1 {
		return AIBrowserConnector{}, ErrAIBrowserConnectorUnauthorized
	}
	return r.aiBrowserConnectorByID(ctx, connectorID)
}

func (r *Repository) RevokeAIBrowserConnector(ctx context.Context, ownerUserID string, orgID string, connectorID string) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	tag, err := tx.Exec(ctx, `
		update ai_browser_connectors
		set status='revoked',token_hash='',pairing_hash='',revoked_at=now(),updated_at=now()
		where id=$1 and owner_user_id=$2 and org_id=$3 and status <> 'revoked'
	`, connectorID, ownerUserID, orgID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return pgx.ErrNoRows
	}
	if _, err := tx.Exec(ctx, `
		update ai_browser_tasks
		set status='cancelled',request_json='{}'::jsonb,response_json='{}'::jsonb,
			lease_hash='',lease_expires_at=null,error_code='connector_revoked',
			error_message='浏览器连接器已撤销',completed_at=now(),updated_at=now()
		where connector_id=$1 and status in ('queued','claimed')
	`, connectorID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		update ai_connections
		set status='disabled',auth_status='disabled',last_error_code='connector_revoked',
			last_error_message='本地浏览器连接器已撤销',updated_at=now()
		where owner_user_id=$1 and org_id=$2 and auth_method='opencli_browser'
			and provider_config->>'connectorId'=$3 and deleted_at is null
	`, ownerUserID, orgID, connectorID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (r *Repository) CreateAIBrowserTask(ctx context.Context, input AIBrowserTaskInput) (AIBrowserTask, error) {
	requestRaw, err := json.Marshal(input.Request)
	if err != nil {
		return AIBrowserTask{}, err
	}
	if input.ExpiresAt.IsZero() {
		input.ExpiresAt = time.Now().Add(2 * time.Minute)
	}
	if _, err := r.db.Exec(ctx, `
		update ai_browser_tasks
		set status='expired',request_json='{}'::jsonb,lease_hash='',lease_expires_at=null,
			error_code='task_expired',error_message='浏览器任务已过期',completed_at=now(),updated_at=now()
		where connector_id=$1 and status in ('queued','claimed') and expires_at<=now()
	`, input.ConnectorID); err != nil {
		return AIBrowserTask{}, err
	}
	id := "aibt_" + uuid.NewString()
	_, err = r.db.Exec(ctx, `
		insert into ai_browser_tasks(
			id,connector_id,owner_user_id,org_id,connection_id,provider,action,request_json,status,expires_at,created_at,updated_at
		)
		select $1,$2,$3,$4,nullif($5,''),$6,$7,$8,'queued',$9,now(),now()
		from ai_browser_connectors
		where id=$2 and owner_user_id=$3 and org_id=$4 and status='active'
	`, id, input.ConnectorID, input.OwnerUserID, input.OrgID, input.ConnectionID,
		strings.ToLower(strings.TrimSpace(input.Provider)), strings.ToLower(strings.TrimSpace(input.Action)),
		requestRaw, input.ExpiresAt)
	if err != nil {
		var postgresError *pgconn.PgError
		if errors.As(err, &postgresError) && postgresError.ConstraintName == "idx_ai_browser_tasks_single_active" {
			return AIBrowserTask{}, ErrAIBrowserConnectorBusy
		}
		return AIBrowserTask{}, err
	}
	return r.AIBrowserTaskForOwner(ctx, input.OwnerUserID, input.OrgID, id)
}

func (r *Repository) ClaimAIBrowserTask(ctx context.Context, connectorID string, leaseDuration time.Duration) (*AIBrowserTask, error) {
	if leaseDuration < 5*time.Second || leaseDuration > 2*time.Minute {
		leaseDuration = 30 * time.Second
	}
	leaseToken, err := secureBrowserToken(24)
	if err != nil {
		return nil, err
	}
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `
		update ai_browser_tasks
		set status='expired',request_json='{}'::jsonb,lease_hash='',lease_expires_at=null,
			error_code='task_expired',error_message='浏览器任务已过期',completed_at=now(),updated_at=now()
		where connector_id=$1 and status in ('queued','claimed') and expires_at<=now()
	`, connectorID); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `
		update ai_browser_tasks
		set status='queued',lease_hash='',lease_expires_at=null,updated_at=now()
		where connector_id=$1 and status='claimed' and lease_expires_at<=now() and expires_at>now()
	`, connectorID); err != nil {
		return nil, err
	}
	var id string
	err = tx.QueryRow(ctx, `
		select id from ai_browser_tasks
		where connector_id=$1 and status='queued' and expires_at>now()
		order by created_at asc
		for update skip locked
		limit 1
	`, connectorID).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		if err := tx.Commit(ctx); err != nil {
			return nil, err
		}
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	leaseExpiresAt := time.Now().Add(leaseDuration)
	if _, err := tx.Exec(ctx, `
		update ai_browser_tasks
		set status='claimed',lease_hash=$2,lease_expires_at=$3,claimed_at=now(),updated_at=now()
		where id=$1
	`, id, hashBrowserToken(leaseToken), leaseExpiresAt); err != nil {
		return nil, err
	}
	task, err := scanAIBrowserTask(tx.QueryRow(ctx, browserTaskSelect+` where id=$1`, id))
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	task.LeaseToken = leaseToken
	return &task, nil
}

func (r *Repository) CompleteAIBrowserTask(ctx context.Context, connectorID string, taskID string, leaseToken string, ok bool, response map[string]any, errorCode string, errorMessage string) error {
	responseRaw, err := json.Marshal(response)
	if err != nil {
		return err
	}
	status := "failed"
	if ok {
		status = "completed"
		errorCode = ""
		errorMessage = ""
	}
	tag, err := r.db.Exec(ctx, `
		update ai_browser_tasks
		set status=$4,response_json=$5,request_json='{}'::jsonb,lease_hash='',lease_expires_at=null,
			error_code=$6,error_message=$7,completed_at=now(),updated_at=now()
		where id=$1 and connector_id=$2 and status='claimed' and lease_hash=$3
			and lease_expires_at>now() and expires_at>now()
	`, taskID, connectorID, hashBrowserToken(strings.TrimSpace(leaseToken)), status,
		responseRaw, strings.TrimSpace(errorCode), truncateStoreText(errorMessage, 512))
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return ErrAIBrowserTaskLeaseInvalid
	}
	return nil
}

func (r *Repository) AIBrowserTaskForOwner(ctx context.Context, ownerUserID string, orgID string, taskID string) (AIBrowserTask, error) {
	return scanAIBrowserTask(r.db.QueryRow(ctx, browserTaskSelect+`
		where id=$1 and owner_user_id=$2 and org_id=$3
	`, taskID, ownerUserID, orgID))
}

func (r *Repository) ExpireAIBrowserTaskForOwner(ctx context.Context, ownerUserID string, orgID string, taskID string) error {
	_, err := r.db.Exec(ctx, `
		update ai_browser_tasks
		set status=case when status in ('queued','claimed') then 'expired' else status end,
			request_json='{}'::jsonb,response_json='{}'::jsonb,lease_hash='',lease_expires_at=null,
			error_code=case when status in ('queued','claimed') then 'task_expired' else error_code end,
			error_message=case when status in ('queued','claimed') then '浏览器任务已过期' else error_message end,
			completed_at=coalesce(completed_at,now()),updated_at=now()
		where id=$1 and owner_user_id=$2 and org_id=$3
	`, taskID, ownerUserID, orgID)
	return err
}

func (r *Repository) ScrubAIBrowserTaskPayloadForOwner(ctx context.Context, ownerUserID string, orgID string, taskID string) error {
	_, err := r.db.Exec(ctx, `
		update ai_browser_tasks
		set request_json='{}'::jsonb,response_json='{}'::jsonb,updated_at=now()
		where id=$1 and owner_user_id=$2 and org_id=$3
			and status in ('completed','failed','expired','cancelled')
	`, taskID, ownerUserID, orgID)
	return err
}

func (r *Repository) MaintainAIBrowserTasks(ctx context.Context, payloadRetention time.Duration) (int64, error) {
	if payloadRetention < time.Minute {
		payloadRetention = 10 * time.Minute
	}
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	expired, err := tx.Exec(ctx, `
		update ai_browser_tasks
		set status='expired',request_json='{}'::jsonb,response_json='{}'::jsonb,
			lease_hash='',lease_expires_at=null,error_code='task_expired',
			error_message='浏览器任务已过期',completed_at=coalesce(completed_at,now()),updated_at=now()
		where status in ('queued','claimed') and expires_at<=now()
	`)
	if err != nil {
		return 0, err
	}
	scrubbed, err := tx.Exec(ctx, `
		update ai_browser_tasks
		set request_json='{}'::jsonb,response_json='{}'::jsonb,updated_at=now()
		where status in ('completed','failed','expired','cancelled')
			and completed_at<=$1
			and (request_json<>'{}'::jsonb or response_json<>'{}'::jsonb)
	`, time.Now().Add(-payloadRetention))
	if err != nil {
		return 0, err
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	return expired.RowsAffected() + scrubbed.RowsAffected(), nil
}

func (r *Repository) SetAIBrowserConnectionAuthStatus(ctx context.Context, ownerUserID string, orgID string, connectionID string, active bool, errorCode string, errorMessage string) error {
	status := "attention"
	authStatus := "attention"
	if active {
		status = "active"
		authStatus = "active"
		errorCode = ""
		errorMessage = ""
	} else if errorCode == "login_required" || errorCode == "security_challenge" || errorCode == "identity_mismatch" {
		authStatus = "reauth_required"
	}
	tag, err := r.db.Exec(ctx, `
		update ai_connections
		set status=$4,auth_status=$5,last_error_code=$6,last_error_message=$7,updated_at=now()
		where id=$1 and owner_user_id=$2 and org_id=$3 and auth_method='opencli_browser'
			and status <> 'deleted' and deleted_at is null
	`, connectionID, ownerUserID, orgID, status, authStatus,
		strings.TrimSpace(errorCode), truncateStoreText(errorMessage, 512))
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return pgx.ErrNoRows
	}
	return nil
}

func (r *Repository) AIBrowserRiskForConnection(
	ctx context.Context,
	ownerUserID string,
	orgID string,
	connectionID string,
	policy AIBrowserRiskPolicy,
) (AIBrowserRiskState, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return AIBrowserRiskState{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	identity, err := browserRiskIdentityForConnection(ctx, tx, ownerUserID, orgID, connectionID)
	if err != nil {
		return AIBrowserRiskState{}, err
	}
	if err := ensureBrowserRiskState(ctx, tx, ownerUserID, orgID, identity); err != nil {
		return AIBrowserRiskState{}, err
	}
	state, err := scanBrowserRiskState(tx.QueryRow(ctx, browserRiskSelect+`
		where owner_user_id=$1 and org_id=$2 and provider=$3 and account_key=$4
		for update
	`, ownerUserID, orgID, identity.Provider, identity.AccountKey))
	if err != nil {
		return AIBrowserRiskState{}, err
	}
	now := time.Now()
	changed := normalizeBrowserRiskWindows(&state, now)
	if state.State == "cooldown" && state.CooldownUntil != nil && !state.CooldownUntil.After(now) {
		state.State = "normal"
		state.CooldownUntil = nil
		changed = true
	}
	if changed {
		state.UpdatedAt = now
		if err := updateBrowserRiskState(ctx, tx, ownerUserID, orgID, identity.AccountKey, state); err != nil {
			return AIBrowserRiskState{}, err
		}
	}
	applyBrowserRiskPolicy(&state, policy)
	if err := tx.Commit(ctx); err != nil {
		return AIBrowserRiskState{}, err
	}
	return state, nil
}

func (r *Repository) ReserveAIBrowserConnectionRequest(
	ctx context.Context,
	ownerUserID string,
	orgID string,
	connectionID string,
	policy AIBrowserRiskPolicy,
	now time.Time,
) (AIBrowserRiskDecision, error) {
	if now.IsZero() {
		now = time.Now()
	}
	policy = normalizeBrowserRiskPolicy(policy)
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return AIBrowserRiskDecision{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	identity, err := browserRiskIdentityForConnection(ctx, tx, ownerUserID, orgID, connectionID)
	if err != nil {
		return AIBrowserRiskDecision{}, err
	}
	if err := ensureBrowserRiskState(ctx, tx, ownerUserID, orgID, identity); err != nil {
		return AIBrowserRiskDecision{}, err
	}
	state, err := scanBrowserRiskState(tx.QueryRow(ctx, browserRiskSelect+`
		where owner_user_id=$1 and org_id=$2 and provider=$3 and account_key=$4
		for update
	`, ownerUserID, orgID, identity.Provider, identity.AccountKey))
	if err != nil {
		return AIBrowserRiskDecision{}, err
	}
	stateChanged := normalizeBrowserRiskWindows(&state, now)
	if state.State == "cooldown" && state.CooldownUntil != nil && !state.CooldownUntil.After(now) {
		state.State = "normal"
		state.CooldownUntil = nil
		stateChanged = true
	}
	applyBrowserRiskPolicy(&state, policy)
	decision := AIBrowserRiskDecision{Risk: state}
	if state.State != "normal" {
		decision.Reason = state.State
		decision.RetryAt = state.CooldownUntil
		if stateChanged {
			if err := updateBrowserRiskState(ctx, tx, ownerUserID, orgID, identity.AccountKey, state); err != nil {
				return AIBrowserRiskDecision{}, err
			}
		}
		if err := tx.Commit(ctx); err != nil {
			return AIBrowserRiskDecision{}, err
		}
		return decision, nil
	}
	if state.LastRequestAt != nil && state.LastRequestAt.Add(policy.MinimumInterval).After(now) {
		retryAt := state.LastRequestAt.Add(policy.MinimumInterval)
		decision.Reason = "minimum_interval"
		decision.RetryAt = &retryAt
		if stateChanged {
			if err := updateBrowserRiskState(ctx, tx, ownerUserID, orgID, identity.AccountKey, state); err != nil {
				return AIBrowserRiskDecision{}, err
			}
		}
		if err := tx.Commit(ctx); err != nil {
			return AIBrowserRiskDecision{}, err
		}
		return decision, nil
	}
	if state.RequestsHour >= policy.HourlyLimit {
		retryAt := state.HourWindowStartedAt.Add(time.Hour)
		decision.Reason = "hourly_limit"
		decision.RetryAt = &retryAt
		if stateChanged {
			if err := updateBrowserRiskState(ctx, tx, ownerUserID, orgID, identity.AccountKey, state); err != nil {
				return AIBrowserRiskDecision{}, err
			}
		}
		if err := tx.Commit(ctx); err != nil {
			return AIBrowserRiskDecision{}, err
		}
		return decision, nil
	}
	if state.RequestsDay >= policy.DailyLimit {
		retryAt := state.DayWindowStartedAt.Add(24 * time.Hour)
		decision.Reason = "daily_limit"
		decision.RetryAt = &retryAt
		if stateChanged {
			if err := updateBrowserRiskState(ctx, tx, ownerUserID, orgID, identity.AccountKey, state); err != nil {
				return AIBrowserRiskDecision{}, err
			}
		}
		if err := tx.Commit(ctx); err != nil {
			return AIBrowserRiskDecision{}, err
		}
		return decision, nil
	}
	state.RequestsHour++
	state.RequestsDay++
	state.LastRequestAt = &now
	state.UpdatedAt = now
	decision.Allowed = true
	decision.Risk = state
	if err := updateBrowserRiskState(ctx, tx, ownerUserID, orgID, identity.AccountKey, state); err != nil {
		return AIBrowserRiskDecision{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return AIBrowserRiskDecision{}, err
	}
	return decision, nil
}

func (r *Repository) RecordAIBrowserConnectionResult(
	ctx context.Context,
	ownerUserID string,
	orgID string,
	connectionID string,
	ok bool,
	errorCode string,
	now time.Time,
) (AIBrowserRiskState, error) {
	if now.IsZero() {
		now = time.Now()
	}
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return AIBrowserRiskState{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	identity, err := browserRiskIdentityForConnection(ctx, tx, ownerUserID, orgID, connectionID)
	if err != nil {
		return AIBrowserRiskState{}, err
	}
	if err := ensureBrowserRiskState(ctx, tx, ownerUserID, orgID, identity); err != nil {
		return AIBrowserRiskState{}, err
	}
	state, err := scanBrowserRiskState(tx.QueryRow(ctx, browserRiskSelect+`
		where owner_user_id=$1 and org_id=$2 and provider=$3 and account_key=$4
		for update
	`, ownerUserID, orgID, identity.Provider, identity.AccountKey))
	if err != nil {
		return AIBrowserRiskState{}, err
	}
	normalizeBrowserRiskWindows(&state, now)
	errorCode = strings.ToLower(strings.TrimSpace(errorCode))
	if ok {
		accessDeniedCooldownActive := state.State == "security_locked" &&
			state.LastErrorCode == "access_denied" &&
			state.CooldownUntil != nil &&
			state.CooldownUntil.After(now)
		if state.State != "paused" && !accessDeniedCooldownActive {
			state.State = "normal"
			state.CooldownUntil = nil
			state.LastErrorCode = ""
		}
		state.ConsecutiveFailures = 0
		state.LastSuccessAt = &now
	} else {
		state.ConsecutiveFailures++
		state.LastErrorAt = &now
		state.LastErrorCode = errorCode
		switch errorCode {
		case "login_required", "identity_mismatch":
			state.State = "reauth_required"
			state.CooldownUntil = nil
		case "security_challenge":
			state.State = "security_locked"
			state.CooldownUntil = nil
			state.LastChallengeAt = &now
		case "access_denied":
			state.State = "security_locked"
			until := now.Add(24 * time.Hour)
			state.CooldownUntil = &until
			state.LastChallengeAt = &now
		case "adapter_incompatible":
			state.State = "adapter_blocked"
			state.CooldownUntil = nil
		case "rate_limited":
			if now.Sub(state.RateLimitWindowStartedAt) >= 24*time.Hour {
				state.RateLimitWindowStartedAt = now
				state.RateLimitEvents = 0
			}
			state.RateLimitEvents++
			state.State = "cooldown"
			duration := 30 * time.Minute
			if state.RateLimitEvents >= 3 {
				duration = 24 * time.Hour
			}
			until := now.Add(duration)
			state.CooldownUntil = &until
		default:
			state.State = "cooldown"
			durations := []time.Duration{time.Minute, 5 * time.Minute, 15 * time.Minute, time.Hour}
			index := state.ConsecutiveFailures - 1
			if index >= len(durations) {
				index = len(durations) - 1
			}
			until := now.Add(durations[index])
			state.CooldownUntil = &until
		}
	}
	state.UpdatedAt = now
	if err := updateBrowserRiskState(ctx, tx, ownerUserID, orgID, identity.AccountKey, state); err != nil {
		return AIBrowserRiskState{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return AIBrowserRiskState{}, err
	}
	return state, nil
}

func (r *Repository) SetAIBrowserConnectionPaused(
	ctx context.Context,
	ownerUserID string,
	orgID string,
	connectionID string,
	paused bool,
) (AIBrowserRiskState, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return AIBrowserRiskState{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	identity, err := browserRiskIdentityForConnection(ctx, tx, ownerUserID, orgID, connectionID)
	if err != nil {
		return AIBrowserRiskState{}, err
	}
	if err := ensureBrowserRiskState(ctx, tx, ownerUserID, orgID, identity); err != nil {
		return AIBrowserRiskState{}, err
	}
	state, err := scanBrowserRiskState(tx.QueryRow(ctx, browserRiskSelect+`
		where owner_user_id=$1 and org_id=$2 and provider=$3 and account_key=$4
		for update
	`, ownerUserID, orgID, identity.Provider, identity.AccountKey))
	if err != nil {
		return AIBrowserRiskState{}, err
	}
	if paused {
		if state.State == "normal" {
			state.State = "paused"
			state.CooldownUntil = nil
		}
	} else if state.State == "paused" {
		state.State = "normal"
		state.ConsecutiveFailures = 0
		state.LastErrorCode = ""
	}
	state.UpdatedAt = time.Now()
	if err := updateBrowserRiskState(ctx, tx, ownerUserID, orgID, identity.AccountKey, state); err != nil {
		return AIBrowserRiskState{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return AIBrowserRiskState{}, err
	}
	return state, nil
}

func browserRiskIdentityForConnection(
	ctx context.Context,
	tx pgx.Tx,
	ownerUserID string,
	orgID string,
	connectionID string,
) (browserRiskIdentity, error) {
	var provider, fingerprint string
	err := tx.QueryRow(ctx, `
		select c.provider,s.subject_fingerprint
		from ai_connections c
		join ai_connection_secrets s on s.connection_id=c.id
		where c.id=$1 and c.owner_user_id=$2 and c.org_id=$3
			and c.auth_method='opencli_browser' and c.status <> 'deleted' and c.deleted_at is null
	`, connectionID, ownerUserID, orgID).Scan(&provider, &fingerprint)
	if err != nil {
		return browserRiskIdentity{}, err
	}
	accountKey := browserRiskAccountKey(ownerUserID, provider, fingerprint)
	if accountKey == "" {
		return browserRiskIdentity{}, errors.New("browser connection account identity is invalid")
	}
	return browserRiskIdentity{Provider: strings.ToLower(strings.TrimSpace(provider)), AccountKey: accountKey}, nil
}

func browserRiskAccountKey(ownerUserID string, provider string, fingerprint string) string {
	ownerUserID = strings.TrimSpace(ownerUserID)
	provider = strings.ToLower(strings.TrimSpace(provider))
	fingerprint = strings.ToLower(strings.TrimSpace(fingerprint))
	if ownerUserID == "" || provider == "" || len(fingerprint) != 64 {
		return ""
	}
	for _, char := range fingerprint {
		if (char < '0' || char > '9') && (char < 'a' || char > 'f') {
			return ""
		}
	}
	sum := sha256.Sum256([]byte("tokhub-opencli-risk-v2\x00" + ownerUserID + "\x00" + provider))
	return hex.EncodeToString(sum[:])
}

func ensureBrowserRiskState(
	ctx context.Context,
	tx pgx.Tx,
	ownerUserID string,
	orgID string,
	identity browserRiskIdentity,
) error {
	_, err := tx.Exec(ctx, `
		insert into ai_browser_account_risk(owner_user_id,org_id,provider,account_key)
		values($1,$2,$3,$4)
		on conflict(owner_user_id,org_id,provider,account_key) do nothing
	`, ownerUserID, orgID, identity.Provider, identity.AccountKey)
	return err
}

const browserRiskSelect = `
	select provider,state,requests_hour,requests_day,rate_limit_events,consecutive_failures,
		hour_window_started_at,day_window_started_at,rate_limit_window_started_at,
		cooldown_until,last_request_at,last_success_at,
		last_error_at,last_error_code,last_challenge_at,updated_at
	from ai_browser_account_risk
`

func scanBrowserRiskState(row pgx.Row) (AIBrowserRiskState, error) {
	var state AIBrowserRiskState
	err := row.Scan(
		&state.Provider, &state.State, &state.RequestsHour, &state.RequestsDay,
		&state.RateLimitEvents, &state.ConsecutiveFailures, &state.HourWindowStartedAt,
		&state.DayWindowStartedAt, &state.RateLimitWindowStartedAt,
		&state.CooldownUntil, &state.LastRequestAt,
		&state.LastSuccessAt, &state.LastErrorAt, &state.LastErrorCode,
		&state.LastChallengeAt, &state.UpdatedAt,
	)
	return state, err
}

func updateBrowserRiskState(
	ctx context.Context,
	tx pgx.Tx,
	ownerUserID string,
	orgID string,
	accountKey string,
	state AIBrowserRiskState,
) error {
	_, err := tx.Exec(ctx, `
		update ai_browser_account_risk
		set state=$5,cooldown_until=$6,hour_window_started_at=$7,day_window_started_at=$8,
			rate_limit_window_started_at=$9,requests_hour=$10,requests_day=$11,
			rate_limit_events=$12,consecutive_failures=$13,last_request_at=$14,
			last_success_at=$15,last_error_at=$16,last_error_code=$17,
			last_challenge_at=$18,updated_at=$19
		where owner_user_id=$1 and org_id=$2 and provider=$3 and account_key=$4
	`, ownerUserID, orgID, state.Provider, accountKey, state.State, state.CooldownUntil,
		state.HourWindowStartedAt, state.DayWindowStartedAt, state.RateLimitWindowStartedAt,
		state.RequestsHour, state.RequestsDay, state.RateLimitEvents, state.ConsecutiveFailures, state.LastRequestAt,
		state.LastSuccessAt, state.LastErrorAt, state.LastErrorCode, state.LastChallengeAt, state.UpdatedAt)
	return err
}

func normalizeBrowserRiskPolicy(policy AIBrowserRiskPolicy) AIBrowserRiskPolicy {
	if policy.MinimumInterval < time.Second {
		policy.MinimumInterval = 10 * time.Second
	}
	if policy.HourlyLimit < 1 {
		policy.HourlyLimit = 20
	}
	if policy.DailyLimit < policy.HourlyLimit {
		policy.DailyLimit = policy.HourlyLimit
	}
	return policy
}

func normalizeBrowserRiskWindows(state *AIBrowserRiskState, now time.Time) bool {
	changed := false
	if now.Sub(state.HourWindowStartedAt) >= time.Hour || now.Before(state.HourWindowStartedAt) {
		state.HourWindowStartedAt = now
		state.RequestsHour = 0
		changed = true
	}
	if now.Sub(state.DayWindowStartedAt) >= 24*time.Hour || now.Before(state.DayWindowStartedAt) {
		state.DayWindowStartedAt = now
		state.RequestsDay = 0
		changed = true
	}
	if now.Sub(state.RateLimitWindowStartedAt) >= 24*time.Hour || now.Before(state.RateLimitWindowStartedAt) {
		state.RateLimitWindowStartedAt = now
		state.RateLimitEvents = 0
		changed = true
	}
	return changed
}

func applyBrowserRiskPolicy(state *AIBrowserRiskState, policy AIBrowserRiskPolicy) {
	policy = normalizeBrowserRiskPolicy(policy)
	state.HourlyLimit = policy.HourlyLimit
	state.DailyLimit = policy.DailyLimit
	state.MinimumIntervalSecond = int(policy.MinimumInterval / time.Second)
}

func (r *Repository) aiBrowserConnectorByID(ctx context.Context, connectorID string) (AIBrowserConnector, error) {
	return scanAIBrowserConnector(r.db.QueryRow(ctx, browserConnectorSelect+` where id=$1`, connectorID))
}

const browserConnectorSelect = `
	select id,org_id,display_name,status,token_prefix,opencli_version,extension_version,
		capabilities_json,pairing_expires_at,last_seen_at,paired_at,created_at,updated_at
	from ai_browser_connectors
`

func scanAIBrowserConnector(row pgx.Row) (AIBrowserConnector, error) {
	var item AIBrowserConnector
	var capabilitiesRaw []byte
	if err := row.Scan(
		&item.ID, &item.OrgID, &item.DisplayName, &item.Status, &item.TokenPrefix,
		&item.OpenCLIVersion, &item.ExtensionVersion, &capabilitiesRaw, &item.PairingExpiresAt,
		&item.LastSeenAt, &item.PairedAt, &item.CreatedAt, &item.UpdatedAt,
	); err != nil {
		return AIBrowserConnector{}, err
	}
	_ = json.Unmarshal(capabilitiesRaw, &item.Capabilities)
	if item.Capabilities == nil {
		item.Capabilities = []string{}
	}
	item.Online = item.Status == "active" && item.LastSeenAt != nil && item.LastSeenAt.After(time.Now().Add(-45*time.Second))
	return item, nil
}

const browserTaskSelect = `
	select id,connector_id,coalesce(connection_id,''),provider,action,request_json,response_json,
		status,error_code,error_message,expires_at,created_at,completed_at
	from ai_browser_tasks
`

func scanAIBrowserTask(row pgx.Row) (AIBrowserTask, error) {
	var item AIBrowserTask
	var requestRaw, responseRaw []byte
	if err := row.Scan(
		&item.ID, &item.ConnectorID, &item.ConnectionID, &item.Provider, &item.Action,
		&requestRaw, &responseRaw, &item.Status, &item.ErrorCode, &item.ErrorMessage,
		&item.ExpiresAt, &item.CreatedAt, &item.CompletedAt,
	); err != nil {
		return AIBrowserTask{}, err
	}
	_ = json.Unmarshal(requestRaw, &item.Request)
	_ = json.Unmarshal(responseRaw, &item.Response)
	if item.Request == nil {
		item.Request = map[string]any{}
	}
	if item.Response == nil {
		item.Response = map[string]any{}
	}
	return item, nil
}

func secureBrowserToken(bytes int) (string, error) {
	buffer := make([]byte, bytes)
	if _, err := rand.Read(buffer); err != nil {
		return "", fmt.Errorf("generate browser connector token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buffer), nil
}

func hashBrowserToken(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func tokenPrefix(value string) string {
	if len(value) <= 8 {
		return value
	}
	return value[:8]
}

func normalizedBrowserCapabilities(values []string) []string {
	allowed := map[string]bool{"openai": true, "gemini": true, "deepseek": true}
	seen := map[string]bool{}
	result := []string{}
	for _, value := range values {
		value = strings.ToLower(strings.TrimSpace(value))
		if allowed[value] && !seen[value] {
			seen[value] = true
			result = append(result, value)
		}
	}
	return result
}

func truncateStoreText(value string, max int) string {
	value = strings.TrimSpace(value)
	runes := []rune(value)
	if len(runes) <= max {
		return value
	}
	return string(runes[:max])
}
