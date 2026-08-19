package store

import (
	"context"
	"crypto/ed25519"
	"crypto/hmac"
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
	ErrAIClientConnectorPairingInvalid = errors.New("official client connector pairing code is invalid or expired")
	ErrAIClientConnectorUnauthorized   = errors.New("official client connector token is invalid")
	ErrAIClientConnectorBusy           = errors.New("official client connector already has an active task")
	ErrAIClientTaskLeaseInvalid        = errors.New("official client task lease is invalid or expired")
	ErrAIClientSessionExpired          = errors.New("official client session expired")
	ErrAIClientRiskTransitionDenied    = errors.New("official client risk state cannot be changed by this action")
)

const (
	AIClientMinimumInterval = 15 * time.Second
	AIClientHourlyLimit     = 20
	AIClientDailyLimit      = 80
)

type AIClientConnector struct {
	ID               string         `json:"id"`
	OwnerUserID      string         `json:"-"`
	OrgID            string         `json:"orgId"`
	DisplayName      string         `json:"displayName"`
	Status           string         `json:"status"`
	Online           bool           `json:"online"`
	TokenPrefix      string         `json:"tokenPrefix,omitempty"`
	PublicKey        string         `json:"-"`
	RuntimeKind      string         `json:"runtimeKind"`
	ConnectorVersion string         `json:"connectorVersion,omitempty"`
	CodexVersion     string         `json:"codexVersion,omitempty"`
	GrokVersion      string         `json:"grokVersion,omitempty"`
	Capabilities     []string       `json:"capabilities"`
	Identity         map[string]any `json:"identity,omitempty"`
	PairingExpiresAt *time.Time     `json:"pairingExpiresAt,omitempty"`
	LastSeenAt       *time.Time     `json:"lastSeenAt,omitempty"`
	PairedAt         *time.Time     `json:"pairedAt,omitempty"`
	CreatedAt        time.Time      `json:"createdAt"`
	UpdatedAt        time.Time      `json:"updatedAt"`
}

type AIClientConnectorCreateResult struct {
	Connector   AIClientConnector `json:"connector"`
	PairingCode string            `json:"pairingCode"`
}

type AIClientConnectorPairResult struct {
	Connector   AIClientConnector `json:"connector"`
	DeviceToken string            `json:"deviceToken"`
}

type AIClientTaskInput struct {
	ConnectorID  string
	OwnerUserID  string
	OrgID        string
	ConnectionID string
	Provider     string
	Action       string
	PayloadKey   string
	ExpiresAt    time.Time
}

type AIClientTask struct {
	ID           string     `json:"id"`
	ConnectorID  string     `json:"connectorId"`
	ConnectionID string     `json:"connectionId,omitempty"`
	Provider     string     `json:"provider"`
	Action       string     `json:"action"`
	PayloadKey   string     `json:"-"`
	ResultKey    string     `json:"-"`
	Status       string     `json:"status"`
	LeaseToken   string     `json:"leaseToken,omitempty"`
	ErrorCode    string     `json:"errorCode,omitempty"`
	ErrorMessage string     `json:"errorMessage,omitempty"`
	ExpiresAt    time.Time  `json:"expiresAt"`
	CreatedAt    time.Time  `json:"createdAt"`
	CompletedAt  *time.Time `json:"completedAt,omitempty"`
}

type AIClientResponseInput struct {
	OwnerUserID        string
	OrgID              string
	GatewayKeyID       string
	ConnectionID       string
	ConnectorID        string
	Provider           string
	Model              string
	SessionCiphertext  string
	SessionNonce       string
	PreviousResponseID string
	IdleExpiresAt      time.Time
	AbsoluteExpiresAt  time.Time
}

type AIClientResponse struct {
	ID                 string    `json:"id"`
	OwnerUserID        string    `json:"-"`
	OrgID              string    `json:"-"`
	GatewayKeyID       string    `json:"-"`
	ConnectionID       string    `json:"connectionId"`
	ConnectorID        string    `json:"connectorId"`
	Provider           string    `json:"provider"`
	Model              string    `json:"model"`
	SessionCiphertext  string    `json:"-"`
	SessionNonce       string    `json:"-"`
	PreviousResponseID string    `json:"previousResponseId,omitempty"`
	Status             string    `json:"status"`
	IdleExpiresAt      time.Time `json:"idleExpiresAt"`
	AbsoluteExpiresAt  time.Time `json:"absoluteExpiresAt"`
	LastUsedAt         time.Time `json:"lastUsedAt"`
	CreatedAt          time.Time `json:"createdAt"`
}

type AIClientRiskState struct {
	ConnectionID             string     `json:"connectionId"`
	Provider                 string     `json:"provider"`
	IdentityAssurance        string     `json:"identityAssurance"`
	State                    string     `json:"state"`
	CooldownUntil            *time.Time `json:"cooldownUntil,omitempty"`
	RequestsHour             int        `json:"requestsHour"`
	RequestsDay              int        `json:"requestsDay"`
	RateLimitEvents          int        `json:"rateLimitEvents"`
	IdentityChanges          int        `json:"identityChanges"`
	ConsecutiveFailures      int        `json:"consecutiveFailures"`
	LastRequestAt            *time.Time `json:"lastRequestAt,omitempty"`
	LastSuccessAt            *time.Time `json:"lastSuccessAt,omitempty"`
	LastErrorAt              *time.Time `json:"lastErrorAt,omitempty"`
	LastErrorCode            string     `json:"lastErrorCode,omitempty"`
	UpdatedAt                time.Time  `json:"updatedAt"`
	MinimumIntervalSeconds   int        `json:"minimumIntervalSeconds"`
	HourlyLimit              int        `json:"hourlyLimit"`
	DailyLimit               int        `json:"dailyLimit"`
	hourWindowStartedAt      time.Time
	dayWindowStartedAt       time.Time
	rateLimitWindowStartedAt time.Time
	accountSubject           string
}

type AIClientRiskDecision struct {
	Allowed bool              `json:"allowed"`
	Reason  string            `json:"reason,omitempty"`
	RetryAt *time.Time        `json:"retryAt,omitempty"`
	Risk    AIClientRiskState `json:"risk"`
}

type AIClientConnectorVersionMetric struct {
	ConnectorVersion string
	CodexVersion     string
	GrokVersion      string
	Count            int
}

type AIClientTaskMetric struct {
	Provider  string
	Status    string
	ErrorCode string
	Count     int
}

type AIClientSessionMetric struct {
	Provider string
	Status   string
	Resumed  bool
	Count    int
}

type AIClientRiskMetric struct {
	Provider          string
	State             string
	IdentityAssurance string
	Count             int
	IdentityChanges   int
}

type AIClientMetrics struct {
	OnlineVersions []AIClientConnectorVersionMetric
	Tasks          []AIClientTaskMetric
	Sessions       []AIClientSessionMetric
	Risks          []AIClientRiskMetric
}

func (r *Repository) CreateAIClientConnector(ctx context.Context, ownerUserID, orgID, displayName string) (AIClientConnectorCreateResult, error) {
	displayName = strings.TrimSpace(displayName)
	if displayName == "" {
		displayName = "我的官方客户端"
	}
	if len([]rune(displayName)) > 80 {
		return AIClientConnectorCreateResult{}, errors.New("official client connector name is too long")
	}
	pairingCode, err := secureClientToken(24)
	if err != nil {
		return AIClientConnectorCreateResult{}, err
	}
	id := "aicc_" + uuid.NewString()
	expiresAt := time.Now().Add(10 * time.Minute)
	_, err = r.db.Exec(ctx, `
		insert into ai_client_connectors(
			id,owner_user_id,org_id,display_name,status,pairing_hash,pairing_expires_at,created_at,updated_at
		)
		select $1,$2,$3,$4,'pending',$5,$6,now(),now()
		where exists(select 1 from users where id=$2 and status='active' and deleted_at is null)
		  and exists(select 1 from orgs where id=$3 and status='active')
		  and exists(select 1 from org_members where org_id=$3 and user_id=$2 and status='active')
	`, id, ownerUserID, orgID, displayName, hashClientToken(pairingCode), expiresAt)
	if err != nil {
		return AIClientConnectorCreateResult{}, err
	}
	connector, err := r.AIClientConnectorForOwner(ctx, ownerUserID, orgID, id)
	return AIClientConnectorCreateResult{Connector: connector, PairingCode: pairingCode}, err
}

func (r *Repository) AIClientConnectorsForOwner(ctx context.Context, ownerUserID, orgID string) ([]AIClientConnector, error) {
	rows, err := r.db.Query(ctx, clientConnectorSelect+`
		where owner_user_id=$1 and org_id=$2 and status <> 'revoked'
		order by created_at desc
	`, ownerUserID, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []AIClientConnector{}
	for rows.Next() {
		item, err := scanAIClientConnector(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *Repository) AIClientConnectorForOwner(ctx context.Context, ownerUserID, orgID, connectorID string) (AIClientConnector, error) {
	return scanAIClientConnector(r.db.QueryRow(ctx, clientConnectorSelect+`
		where id=$1 and owner_user_id=$2 and org_id=$3 and status <> 'revoked'
	`, connectorID, ownerUserID, orgID))
}

func (r *Repository) PairAIClientConnector(ctx context.Context, pairingCode, publicKey string) (AIClientConnectorPairResult, error) {
	pairingCode = strings.TrimSpace(pairingCode)
	publicKey = strings.TrimSpace(publicKey)
	decodedKey, err := base64.RawURLEncoding.DecodeString(publicKey)
	if err != nil || len(decodedKey) != ed25519.PublicKeySize || len(pairingCode) < 32 {
		return AIClientConnectorPairResult{}, ErrAIClientConnectorPairingInvalid
	}
	deviceToken, err := secureClientToken(32)
	if err != nil {
		return AIClientConnectorPairResult{}, err
	}
	var id string
	err = r.db.QueryRow(ctx, `
		update ai_client_connectors as connector
		set status='active',pairing_hash='',pairing_expires_at=null,token_hash=$2,
			token_prefix=$3,public_key=$4,paired_at=now(),last_seen_at=now(),updated_at=now()
		where pairing_hash=$1 and pairing_expires_at>now() and status='pending'
		  and exists(select 1 from users where id=connector.owner_user_id and status='active' and deleted_at is null)
		  and exists(select 1 from orgs where id=connector.org_id and status='active')
		  and exists(select 1 from org_members where org_id=connector.org_id and user_id=connector.owner_user_id and status='active')
		returning id
	`, hashClientToken(pairingCode), hashClientToken(deviceToken), clientTokenPrefix(deviceToken), publicKey).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return AIClientConnectorPairResult{}, ErrAIClientConnectorPairingInvalid
	}
	if err != nil {
		return AIClientConnectorPairResult{}, err
	}
	connector, err := r.aiClientConnectorByID(ctx, id)
	return AIClientConnectorPairResult{Connector: connector, DeviceToken: deviceToken}, err
}

func (r *Repository) AuthenticateAIClientConnector(ctx context.Context, deviceToken string) (AIClientConnector, error) {
	deviceToken = strings.TrimSpace(deviceToken)
	if len(deviceToken) < 40 {
		return AIClientConnector{}, ErrAIClientConnectorUnauthorized
	}
	connector, err := scanAIClientConnector(r.db.QueryRow(ctx, clientConnectorSelect+`
		where token_hash=$1 and status='active'
		  and exists(select 1 from users where id=ai_client_connectors.owner_user_id and status='active' and deleted_at is null)
		  and exists(select 1 from orgs where id=ai_client_connectors.org_id and status='active')
		  and exists(select 1 from org_members where org_id=ai_client_connectors.org_id and user_id=ai_client_connectors.owner_user_id and status='active')
	`, hashClientToken(deviceToken)))
	if errors.Is(err, pgx.ErrNoRows) {
		return AIClientConnector{}, ErrAIClientConnectorUnauthorized
	}
	return connector, err
}

func (r *Repository) HeartbeatAIClientConnector(ctx context.Context, connectorID, connectorVersion, codexVersion, grokVersion string, capabilities []string, identity map[string]any) (AIClientConnector, error) {
	capabilities = normalizedClientCapabilities(capabilities)
	capabilitiesRaw, err := json.Marshal(capabilities)
	if err != nil {
		return AIClientConnector{}, err
	}
	identityRaw, err := json.Marshal(identity)
	if err != nil || len(identityRaw) > 16*1024 {
		return AIClientConnector{}, errors.New("official client identity payload is invalid")
	}
	tag, err := r.db.Exec(ctx, `
		update ai_client_connectors
		set connector_version=$2,codex_version=$3,grok_version=$4,capabilities_json=$5,
			identity_json=$6,last_seen_at=now(),updated_at=now()
		where id=$1 and status='active'
	`, connectorID, truncateStoreText(connectorVersion, 64), truncateStoreText(codexVersion, 64),
		truncateStoreText(grokVersion, 64), capabilitiesRaw, identityRaw)
	if err != nil {
		return AIClientConnector{}, err
	}
	if tag.RowsAffected() != 1 {
		return AIClientConnector{}, ErrAIClientConnectorUnauthorized
	}
	return r.aiClientConnectorByID(ctx, connectorID)
}

func (r *Repository) RevokeAIClientConnector(ctx context.Context, ownerUserID, orgID, connectorID string) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	tag, err := tx.Exec(ctx, `
		update ai_client_connectors
		set status='revoked',token_hash='',pairing_hash='',public_key='',revoked_at=now(),updated_at=now()
		where id=$1 and owner_user_id=$2 and org_id=$3 and status <> 'revoked'
	`, connectorID, ownerUserID, orgID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return pgx.ErrNoRows
	}
	if _, err := tx.Exec(ctx, `
		update ai_client_tasks set status='cancelled',lease_hash='',lease_expires_at=null,
			error_code='connector_revoked',error_message='Official client connector revoked',completed_at=now(),updated_at=now()
		where connector_id=$1 and status in ('queued','claimed')
	`, connectorID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		update ai_connections set status='disabled',auth_status='disabled',risk_level='paused',
			last_error_code='connector_revoked',last_error_message='Official client connector revoked',updated_at=now()
		where owner_user_id=$1 and org_id=$2 and auth_method='official_client'
		  and provider_config->>'connectorId'=$3 and deleted_at is null
	`, ownerUserID, orgID, connectorID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		update ai_client_responses set status='deleted',session_ciphertext='',session_nonce='',updated_at=now()
		where connector_id=$1 and owner_user_id=$2 and org_id=$3 and status <> 'deleted'
	`, connectorID, ownerUserID, orgID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (r *Repository) CreateAIClientTask(ctx context.Context, input AIClientTaskInput) (AIClientTask, error) {
	if input.ExpiresAt.IsZero() {
		input.ExpiresAt = time.Now().Add(3 * time.Minute)
	}
	if strings.TrimSpace(input.PayloadKey) == "" {
		return AIClientTask{}, errors.New("official client payload key is required")
	}
	if _, err := r.db.Exec(ctx, `
		update ai_client_tasks set status='expired',lease_hash='',lease_expires_at=null,
			error_code='task_expired',error_message='Official client task expired',completed_at=now(),updated_at=now()
		where connector_id=$1 and status in ('queued','claimed') and expires_at<=now()
	`, input.ConnectorID); err != nil {
		return AIClientTask{}, err
	}
	id := "aict_" + uuid.NewString()
	_, err := r.db.Exec(ctx, `
		insert into ai_client_tasks(
			id,connector_id,owner_user_id,org_id,connection_id,provider,action,payload_key,status,expires_at,created_at,updated_at
		)
		select $1,$2,$3,$4,nullif($5,''),$6,$7,$8,'queued',$9,now(),now()
		from ai_client_connectors
		where id=$2 and owner_user_id=$3 and org_id=$4 and status='active'
	`, id, input.ConnectorID, input.OwnerUserID, input.OrgID, input.ConnectionID,
		strings.ToLower(strings.TrimSpace(input.Provider)), strings.ToLower(strings.TrimSpace(input.Action)),
		input.PayloadKey, input.ExpiresAt)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.ConstraintName == "idx_ai_client_tasks_single_active" {
			return AIClientTask{}, ErrAIClientConnectorBusy
		}
		return AIClientTask{}, err
	}
	return r.AIClientTaskForOwner(ctx, input.OwnerUserID, input.OrgID, id)
}

func (r *Repository) ClaimAIClientTask(ctx context.Context, connectorID string, leaseDuration time.Duration) (*AIClientTask, error) {
	if leaseDuration < 10*time.Second || leaseDuration > 5*time.Minute {
		leaseDuration = 2 * time.Minute
	}
	leaseToken, err := secureClientToken(24)
	if err != nil {
		return nil, err
	}
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `
		update ai_client_tasks set status='expired',lease_hash='',lease_expires_at=null,
			error_code='task_expired',error_message='Official client task expired',completed_at=now(),updated_at=now()
		where connector_id=$1 and status in ('queued','claimed') and expires_at<=now()
	`, connectorID); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `
		update ai_client_tasks set status='queued',lease_hash='',lease_expires_at=null,updated_at=now()
		where connector_id=$1 and status='claimed' and lease_expires_at<=now() and expires_at>now()
	`, connectorID); err != nil {
		return nil, err
	}
	var id string
	err = tx.QueryRow(ctx, `
		select id from ai_client_tasks where connector_id=$1 and status='queued' and expires_at>now()
		order by created_at asc for update skip locked limit 1
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
		update ai_client_tasks set status='claimed',lease_hash=$2,lease_expires_at=$3,claimed_at=now(),updated_at=now()
		where id=$1
	`, id, hashClientToken(leaseToken), leaseExpiresAt); err != nil {
		return nil, err
	}
	task, err := scanAIClientTask(tx.QueryRow(ctx, clientTaskSelect+` where id=$1`, id))
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	task.LeaseToken = leaseToken
	return &task, nil
}

func (r *Repository) CompleteAIClientTask(ctx context.Context, connectorID, taskID, leaseToken string, ok bool, resultKey, errorCode, errorMessage string) error {
	status := "failed"
	if ok {
		status = "completed"
		errorCode, errorMessage = "", ""
	}
	tag, err := r.db.Exec(ctx, `
		update ai_client_tasks set status=$4,result_key=$5,lease_hash='',lease_expires_at=null,
			error_code=$6,error_message=$7,completed_at=now(),updated_at=now()
		where id=$1 and connector_id=$2 and status='claimed' and lease_hash=$3
		  and lease_expires_at>now() and expires_at>now()
	`, taskID, connectorID, hashClientToken(strings.TrimSpace(leaseToken)), status,
		strings.TrimSpace(resultKey), strings.TrimSpace(errorCode), truncateStoreText(errorMessage, 512))
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return ErrAIClientTaskLeaseInvalid
	}
	return nil
}

func (r *Repository) AIClientTaskLeaseActive(ctx context.Context, connectorID, taskID, leaseToken string) (bool, error) {
	var active bool
	err := r.db.QueryRow(ctx, `
		select exists(
			select 1 from ai_client_tasks
			where id=$1 and connector_id=$2 and status='claimed' and lease_hash=$3
			  and lease_expires_at>now() and expires_at>now()
		)
	`, taskID, connectorID, hashClientToken(strings.TrimSpace(leaseToken))).Scan(&active)
	return active, err
}

func (r *Repository) AIClientTaskForOwner(ctx context.Context, ownerUserID, orgID, taskID string) (AIClientTask, error) {
	return scanAIClientTask(r.db.QueryRow(ctx, clientTaskSelect+`
		where id=$1 and owner_user_id=$2 and org_id=$3
	`, taskID, ownerUserID, orgID))
}

func (r *Repository) CancelAIClientTask(ctx context.Context, ownerUserID, orgID, taskID, reason string) error {
	_, err := r.db.Exec(ctx, `
		update ai_client_tasks set status=case when status in ('queued','claimed') then 'cancelled' else status end,
			lease_hash='',lease_expires_at=null,error_code=case when status in ('queued','claimed') then 'cancelled' else error_code end,
			error_message=case when status in ('queued','claimed') then $4 else error_message end,
			completed_at=coalesce(completed_at,now()),updated_at=now()
		where id=$1 and owner_user_id=$2 and org_id=$3
	`, taskID, ownerUserID, orgID, truncateStoreText(reason, 512))
	return err
}

func (r *Repository) CreateAIClientResponse(ctx context.Context, input AIClientResponseInput) (AIClientResponse, error) {
	id := "resp_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if input.IdleExpiresAt.IsZero() {
		input.IdleExpiresAt = time.Now().Add(24 * time.Hour)
	}
	if input.AbsoluteExpiresAt.IsZero() {
		input.AbsoluteExpiresAt = time.Now().Add(7 * 24 * time.Hour)
	}
	_, err := r.db.Exec(ctx, `
		insert into ai_client_responses(
			id,owner_user_id,org_id,gateway_key_id,connection_id,connector_id,provider,model,
			session_ciphertext,session_nonce,previous_response_id,status,idle_expires_at,absolute_expires_at,last_used_at,created_at,updated_at
		) values($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,nullif($11,''),'active',$12,$13,now(),now(),now())
	`, id, input.OwnerUserID, input.OrgID, input.GatewayKeyID, input.ConnectionID, input.ConnectorID,
		input.Provider, input.Model, input.SessionCiphertext, input.SessionNonce, input.PreviousResponseID,
		input.IdleExpiresAt, input.AbsoluteExpiresAt)
	if err != nil {
		return AIClientResponse{}, err
	}
	return r.aiClientResponseByID(ctx, id)
}

func (r *Repository) AIClientResponseForUse(ctx context.Context, responseID, ownerUserID, orgID, gatewayKeyID, connectionID, model string) (AIClientResponse, error) {
	response, err := scanAIClientResponse(r.db.QueryRow(ctx, clientResponseSelect+`
		where id=$1 and owner_user_id=$2 and org_id=$3 and gateway_key_id=$4
		  and connection_id=$5 and model=$6 and status <> 'deleted'
	`, responseID, ownerUserID, orgID, gatewayKeyID, connectionID, model))
	if err != nil {
		return AIClientResponse{}, err
	}
	now := time.Now()
	if response.Status != "active" || !response.IdleExpiresAt.After(now) || !response.AbsoluteExpiresAt.After(now) {
		_, _ = r.db.Exec(ctx, `update ai_client_responses set status='expired',updated_at=now() where id=$1 and status='active'`, response.ID)
		return AIClientResponse{}, ErrAIClientSessionExpired
	}
	newIdle := now.Add(24 * time.Hour)
	if newIdle.After(response.AbsoluteExpiresAt) {
		newIdle = response.AbsoluteExpiresAt
	}
	_, err = r.db.Exec(ctx, `
		update ai_client_responses set idle_expires_at=$2,last_used_at=now(),updated_at=now()
		where id=$1 and status='active'
	`, response.ID, newIdle)
	if err != nil {
		return AIClientResponse{}, err
	}
	response.IdleExpiresAt = newIdle
	response.LastUsedAt = now
	return response, nil
}

func (r *Repository) DeleteAIClientResponseForOwner(ctx context.Context, ownerUserID, orgID, responseID string) (AIClientResponse, error) {
	response, err := scanAIClientResponse(r.db.QueryRow(ctx, clientResponseSelect+`
		where id=$1 and owner_user_id=$2 and org_id=$3 and status <> 'deleted'
	`, responseID, ownerUserID, orgID))
	if err != nil {
		return AIClientResponse{}, err
	}
	_, err = r.db.Exec(ctx, `update ai_client_responses set status='deleted',session_ciphertext='',session_nonce='',updated_at=now() where id=$1`, response.ID)
	return response, err
}

func (r *Repository) MaintainAIClientState(ctx context.Context) (int64, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	connectors, err := tx.Exec(ctx, `
		update ai_client_connectors set status='revoked',pairing_hash='',pairing_expires_at=null,
			revoked_at=now(),updated_at=now()
		where status='pending' and pairing_expires_at<=now()
	`)
	if err != nil {
		return 0, err
	}
	tasks, err := tx.Exec(ctx, `
		update ai_client_tasks set status='expired',lease_hash='',lease_expires_at=null,
			error_code='task_expired',error_message='Official client task expired',completed_at=now(),updated_at=now()
		where status in ('queued','claimed') and expires_at<=now()
	`)
	if err != nil {
		return 0, err
	}
	responses, err := tx.Exec(ctx, `
		update ai_client_responses set status='expired',session_ciphertext='',session_nonce='',updated_at=now()
		where status='active' and (idle_expires_at<=now() or absolute_expires_at<=now())
	`)
	if err != nil {
		return 0, err
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	return connectors.RowsAffected() + tasks.RowsAffected() + responses.RowsAffected(), nil
}

// AIClientMetrics reports only bounded operational labels. Task payloads,
// generated content, account subjects, connector tokens, and network addresses
// never enter this snapshot.
func (r *Repository) AIClientMetrics(ctx context.Context) (AIClientMetrics, error) {
	var snapshot AIClientMetrics
	rows, err := r.db.Query(ctx, `
		select connector_version,codex_version,grok_version,count(*)::int
		from ai_client_connectors
		where status='active' and last_seen_at>now()-interval '45 seconds'
		group by connector_version,codex_version,grok_version
		order by count(*) desc,connector_version,codex_version,grok_version
		limit 50
	`)
	if err != nil {
		return snapshot, err
	}
	for rows.Next() {
		var item AIClientConnectorVersionMetric
		if err := rows.Scan(&item.ConnectorVersion, &item.CodexVersion, &item.GrokVersion, &item.Count); err != nil {
			rows.Close()
			return snapshot, err
		}
		snapshot.OnlineVersions = append(snapshot.OnlineVersions, item)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return snapshot, err
	}
	rows.Close()

	rows, err = r.db.Query(ctx, `
		select provider,status,
			case
				when status='completed' then 'success'
				when error_code in ('unauthorized','login_required','401') then '401'
				when error_code in ('forbidden','security_challenge','403','identity_changed') then '403'
				when error_code in ('rate_limited','429') then '429'
				when error_code in ('timeout','cancelled','task_expired') then 'timeout'
				when error_code in ('tool_event','tool_call','policy_violation') then 'tool_blocked'
				else 'other'
			end as outcome,
			count(*)::int
		from ai_client_tasks
		group by provider,status,outcome
		order by provider,status,outcome
	`)
	if err != nil {
		return snapshot, err
	}
	for rows.Next() {
		var item AIClientTaskMetric
		if err := rows.Scan(&item.Provider, &item.Status, &item.ErrorCode, &item.Count); err != nil {
			rows.Close()
			return snapshot, err
		}
		snapshot.Tasks = append(snapshot.Tasks, item)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return snapshot, err
	}
	rows.Close()

	rows, err = r.db.Query(ctx, `
		select provider,status,(previous_response_id is not null) as resumed,count(*)::int
		from ai_client_responses
		group by provider,status,resumed
		order by provider,status,resumed
	`)
	if err != nil {
		return snapshot, err
	}
	for rows.Next() {
		var item AIClientSessionMetric
		if err := rows.Scan(&item.Provider, &item.Status, &item.Resumed, &item.Count); err != nil {
			rows.Close()
			return snapshot, err
		}
		snapshot.Sessions = append(snapshot.Sessions, item)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return snapshot, err
	}
	rows.Close()

	rows, err = r.db.Query(ctx, `
		select provider,state,identity_assurance,count(*)::int,coalesce(sum(identity_changes),0)::int
		from ai_client_account_risk
		group by provider,state,identity_assurance
		order by provider,state,identity_assurance
	`)
	if err != nil {
		return snapshot, err
	}
	defer rows.Close()
	for rows.Next() {
		var item AIClientRiskMetric
		if err := rows.Scan(&item.Provider, &item.State, &item.IdentityAssurance, &item.Count, &item.IdentityChanges); err != nil {
			return snapshot, err
		}
		snapshot.Risks = append(snapshot.Risks, item)
	}
	return snapshot, rows.Err()
}

func (r *Repository) InitializeAIClientRisk(ctx context.Context, connectionID, ownerUserID, orgID, connectorID, provider, accountSubject, assurance string) error {
	if assurance != "account" {
		assurance = "device"
	}
	_, err := r.db.Exec(ctx, `
		insert into ai_client_account_risk(
			connection_id,owner_user_id,org_id,connector_id,provider,account_subject,identity_assurance
		) values($1,$2,$3,$4,$5,$6,$7)
		on conflict(connection_id) do update set
			connector_id=excluded.connector_id,account_subject=excluded.account_subject,
			identity_assurance=excluded.identity_assurance,updated_at=now()
	`, connectionID, ownerUserID, orgID, connectorID, provider, accountSubject, assurance)
	return err
}

func (r *Repository) UpdateAIClientConnectionRuntimeDetails(ctx context.Context, ownerUserID, orgID, connectionID string, actualModels []string, clientVersion string) error {
	tag, err := r.db.Exec(ctx, `
		update ai_connections
		set provider_config=provider_config || jsonb_build_object(
			'actualModels',to_jsonb($4::text[]),'clientVersion',$5::text
		),updated_at=now()
		where id=$1 and owner_user_id=$2 and org_id=$3 and auth_method='official_client'
		  and status <> 'deleted' and deleted_at is null
	`, connectionID, ownerUserID, orgID, actualModels, strings.TrimSpace(clientVersion))
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return pgx.ErrNoRows
	}
	return nil
}

func (r *Repository) AIClientRiskForConnection(ctx context.Context, ownerUserID, orgID, connectionID string) (AIClientRiskState, error) {
	state, err := scanAIClientRisk(r.db.QueryRow(ctx, clientRiskSelect+`
		where connection_id=$1 and owner_user_id=$2 and org_id=$3
	`, connectionID, ownerUserID, orgID))
	if err != nil {
		return AIClientRiskState{}, err
	}
	state = normalizeAIClientRisk(state, time.Now())
	applyAIClientRiskLimits(&state)
	return state, nil
}

func (r *Repository) ReserveAIClientRequest(ctx context.Context, ownerUserID, orgID, connectionID string, now time.Time) (AIClientRiskDecision, error) {
	if now.IsZero() {
		now = time.Now()
	}
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return AIClientRiskDecision{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	state, err := scanAIClientRisk(tx.QueryRow(ctx, clientRiskSelect+`
		where connection_id=$1 and owner_user_id=$2 and org_id=$3 for update
	`, connectionID, ownerUserID, orgID))
	if err != nil {
		return AIClientRiskDecision{}, err
	}
	state = normalizeAIClientRisk(state, now)
	decision := AIClientRiskDecision{Risk: state}
	if state.State != "normal" {
		decision.Reason, decision.RetryAt = state.State, state.CooldownUntil
	} else if state.LastRequestAt != nil && state.LastRequestAt.Add(AIClientMinimumInterval).After(now) {
		retryAt := state.LastRequestAt.Add(AIClientMinimumInterval)
		decision.Reason, decision.RetryAt = "minimum_interval", &retryAt
	} else if state.RequestsHour >= AIClientHourlyLimit {
		retryAt := state.hourWindowStartedAt.Add(time.Hour)
		decision.Reason, decision.RetryAt = "hourly_limit", &retryAt
	} else if state.RequestsDay >= AIClientDailyLimit {
		retryAt := state.dayWindowStartedAt.Add(24 * time.Hour)
		decision.Reason, decision.RetryAt = "daily_limit", &retryAt
	} else {
		decision.Allowed = true
		state.RequestsHour++
		state.RequestsDay++
		state.LastRequestAt = &now
	}
	state.UpdatedAt = now
	if err := updateAIClientRisk(ctx, tx, state); err != nil {
		return AIClientRiskDecision{}, err
	}
	applyAIClientRiskLimits(&state)
	decision.Risk = state
	if err := tx.Commit(ctx); err != nil {
		return AIClientRiskDecision{}, err
	}
	return decision, nil
}

func (r *Repository) RecordAIClientResult(ctx context.Context, ownerUserID, orgID, connectionID string, ok bool, errorCode string, retryAfter time.Duration, now time.Time) (AIClientRiskState, error) {
	if now.IsZero() {
		now = time.Now()
	}
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return AIClientRiskState{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	state, err := scanAIClientRisk(tx.QueryRow(ctx, clientRiskSelect+`
		where connection_id=$1 and owner_user_id=$2 and org_id=$3 for update
	`, connectionID, ownerUserID, orgID))
	if err != nil {
		return AIClientRiskState{}, err
	}
	state = normalizeAIClientRisk(state, now)
	errorCode = strings.ToLower(strings.TrimSpace(errorCode))
	if ok {
		state.ConsecutiveFailures = 0
		state.LastSuccessAt = &now
		if state.State == "cooldown" {
			state.State, state.CooldownUntil = "normal", nil
		}
	} else {
		state.ConsecutiveFailures++
		state.LastErrorAt, state.LastErrorCode = &now, errorCode
		switch errorCode {
		case "unauthorized", "login_required", "401":
			state.State, state.CooldownUntil = "reauth_required", nil
		case "forbidden", "security_challenge", "403", "identity_changed":
			state.State, state.CooldownUntil = "security_locked", nil
			if errorCode == "identity_changed" {
				state.IdentityChanges++
			}
		case "tool_event", "tool_call", "policy_violation":
			state.State, state.CooldownUntil = "policy_locked", nil
		case "rate_limited", "429":
			if now.Sub(state.rateLimitWindowStartedAt) >= 24*time.Hour {
				state.rateLimitWindowStartedAt, state.RateLimitEvents = now, 0
			}
			state.RateLimitEvents++
			if state.RateLimitEvents >= 2 {
				state.State, state.CooldownUntil = "manual_recovery", nil
			} else {
				if retryAfter < time.Hour {
					retryAfter = time.Hour
				}
				until := now.Add(retryAfter)
				state.State, state.CooldownUntil = "cooldown", &until
			}
		default:
			until := now.Add(5 * time.Minute)
			state.State, state.CooldownUntil = "cooldown", &until
		}
	}
	state.UpdatedAt = now
	if err := updateAIClientRisk(ctx, tx, state); err != nil {
		return AIClientRiskState{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return AIClientRiskState{}, err
	}
	applyAIClientRiskLimits(&state)
	return state, nil
}

func (r *Repository) SetAIClientConnectionPaused(ctx context.Context, ownerUserID, orgID, connectionID string, paused bool) (AIClientRiskState, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return AIClientRiskState{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	state, err := scanAIClientRisk(tx.QueryRow(ctx, clientRiskSelect+`
		where connection_id=$1 and owner_user_id=$2 and org_id=$3 for update
	`, connectionID, ownerUserID, orgID))
	if err != nil {
		return AIClientRiskState{}, err
	}
	if paused && state.State != "normal" && state.State != "paused" {
		return AIClientRiskState{}, ErrAIClientRiskTransitionDenied
	}
	if !paused && state.State != "normal" && state.State != "paused" && state.State != "manual_recovery" {
		return AIClientRiskState{}, ErrAIClientRiskTransitionDenied
	}
	if paused && state.State == "normal" {
		state.State = "paused"
	} else if !paused && (state.State == "paused" || state.State == "manual_recovery") {
		state.State = "normal"
		state.ConsecutiveFailures, state.LastErrorCode, state.RateLimitEvents = 0, "", 0
	}
	state.UpdatedAt = time.Now()
	if err := updateAIClientRisk(ctx, tx, state); err != nil {
		return AIClientRiskState{}, err
	}
	connectionStatus, authStatus, riskLevel := "attention", "active", "paused"
	lastErrorCode, lastErrorMessage := "official_client_paused", "Official client connection is paused"
	if state.State == "normal" {
		connectionStatus, authStatus, riskLevel = "active", "active", "elevated"
		lastErrorCode, lastErrorMessage = "", ""
	}
	tag, err := tx.Exec(ctx, `
		update ai_connections set status=$4,auth_status=$5,risk_level=$6,
			last_error_code=$7,last_error_message=$8,updated_at=now()
		where id=$1 and owner_user_id=$2 and org_id=$3 and auth_method='official_client'
		  and status <> 'deleted' and deleted_at is null
	`, connectionID, ownerUserID, orgID, connectionStatus, authStatus, riskLevel, lastErrorCode, lastErrorMessage)
	if err != nil {
		return AIClientRiskState{}, err
	}
	if tag.RowsAffected() != 1 {
		return AIClientRiskState{}, pgx.ErrNoRows
	}
	if err := tx.Commit(ctx); err != nil {
		return AIClientRiskState{}, err
	}
	applyAIClientRiskLimits(&state)
	return state, nil
}

// ConfirmAIClientIdentity applies the manual identity checkpoint used after a
// reconnect or reauthentication. A changed subject is permanently locked for
// owner-level operations. A renewed login with the original subject moves to
// paused so the owner must explicitly resume the connection.
func (r *Repository) ConfirmAIClientIdentity(ctx context.Context, ownerUserID, orgID, connectionID, accountSubject, assurance string, now time.Time) (AIClientRiskState, bool, error) {
	if now.IsZero() {
		now = time.Now()
	}
	if assurance != "account" {
		assurance = "device"
	}
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return AIClientRiskState{}, false, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	state, err := scanAIClientRisk(tx.QueryRow(ctx, clientRiskSelect+`
		where connection_id=$1 and owner_user_id=$2 and org_id=$3 for update
	`, connectionID, ownerUserID, orgID))
	if err != nil {
		return AIClientRiskState{}, false, err
	}
	state = normalizeAIClientRisk(state, now)
	matched := strings.TrimSpace(accountSubject) != "" && hmac.Equal([]byte(state.accountSubject), []byte(strings.TrimSpace(accountSubject)))
	if !matched {
		state.State = "security_locked"
		state.CooldownUntil = nil
		state.IdentityChanges++
		state.LastErrorAt = &now
		state.LastErrorCode = "identity_changed"
	} else {
		state.IdentityAssurance = assurance
		state.ConsecutiveFailures = 0
		state.LastErrorCode = ""
		if state.State == "reauth_required" {
			state.State = "paused"
		}
	}
	state.UpdatedAt = now
	if err := updateAIClientRisk(ctx, tx, state); err != nil {
		return AIClientRiskState{}, false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return AIClientRiskState{}, false, err
	}
	applyAIClientRiskLimits(&state)
	return state, matched, nil
}

func (r *Repository) SetAIClientConnectionRiskState(ctx context.Context, ownerUserID, orgID, connectionID, riskState, errorCode, errorMessage string) error {
	status, authStatus, riskLevel := "attention", "attention", "paused"
	switch riskState {
	case "normal":
		status, authStatus, riskLevel = "active", "active", "elevated"
		errorCode, errorMessage = "", ""
	case "reauth_required":
		authStatus = "reauth_required"
	case "paused":
		authStatus = "active"
	}
	tag, err := r.db.Exec(ctx, `
		update ai_connections set status=$4,auth_status=$5,risk_level=$6,
			last_error_code=$7,last_error_message=$8,updated_at=now()
		where id=$1 and owner_user_id=$2 and org_id=$3 and auth_method='official_client'
		  and status <> 'deleted' and deleted_at is null
	`, connectionID, ownerUserID, orgID, status, authStatus, riskLevel,
		strings.TrimSpace(errorCode), truncateStoreText(errorMessage, 512))
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return pgx.ErrNoRows
	}
	return nil
}

const clientConnectorSelect = `
	select id,owner_user_id,org_id,display_name,status,token_prefix,public_key,runtime_kind,
		connector_version,codex_version,grok_version,capabilities_json,identity_json,
		pairing_expires_at,last_seen_at,paired_at,created_at,updated_at
	from ai_client_connectors
`

func scanAIClientConnector(row pgx.Row) (AIClientConnector, error) {
	var item AIClientConnector
	var capabilitiesRaw, identityRaw []byte
	err := row.Scan(&item.ID, &item.OwnerUserID, &item.OrgID, &item.DisplayName, &item.Status,
		&item.TokenPrefix, &item.PublicKey, &item.RuntimeKind, &item.ConnectorVersion, &item.CodexVersion,
		&item.GrokVersion, &capabilitiesRaw, &identityRaw, &item.PairingExpiresAt, &item.LastSeenAt,
		&item.PairedAt, &item.CreatedAt, &item.UpdatedAt)
	if err != nil {
		return AIClientConnector{}, err
	}
	_ = json.Unmarshal(capabilitiesRaw, &item.Capabilities)
	_ = json.Unmarshal(identityRaw, &item.Identity)
	if item.Capabilities == nil {
		item.Capabilities = []string{}
	}
	if item.Identity == nil {
		item.Identity = map[string]any{}
	}
	item.Online = item.Status == "active" && item.LastSeenAt != nil && item.LastSeenAt.After(time.Now().Add(-45*time.Second))
	return item, nil
}

func (r *Repository) aiClientConnectorByID(ctx context.Context, connectorID string) (AIClientConnector, error) {
	return scanAIClientConnector(r.db.QueryRow(ctx, clientConnectorSelect+` where id=$1`, connectorID))
}

const clientTaskSelect = `
	select id,connector_id,coalesce(connection_id,''),provider,action,payload_key,result_key,
		status,error_code,error_message,expires_at,created_at,completed_at
	from ai_client_tasks
`

func scanAIClientTask(row pgx.Row) (AIClientTask, error) {
	var item AIClientTask
	err := row.Scan(&item.ID, &item.ConnectorID, &item.ConnectionID, &item.Provider, &item.Action,
		&item.PayloadKey, &item.ResultKey, &item.Status, &item.ErrorCode, &item.ErrorMessage,
		&item.ExpiresAt, &item.CreatedAt, &item.CompletedAt)
	return item, err
}

const clientResponseSelect = `
	select id,owner_user_id,org_id,gateway_key_id,connection_id,connector_id,provider,model,
		session_ciphertext,session_nonce,coalesce(previous_response_id,''),status,idle_expires_at,
		absolute_expires_at,last_used_at,created_at
	from ai_client_responses
`

func scanAIClientResponse(row pgx.Row) (AIClientResponse, error) {
	var item AIClientResponse
	err := row.Scan(&item.ID, &item.OwnerUserID, &item.OrgID, &item.GatewayKeyID, &item.ConnectionID,
		&item.ConnectorID, &item.Provider, &item.Model, &item.SessionCiphertext, &item.SessionNonce,
		&item.PreviousResponseID, &item.Status, &item.IdleExpiresAt, &item.AbsoluteExpiresAt,
		&item.LastUsedAt, &item.CreatedAt)
	return item, err
}

func (r *Repository) aiClientResponseByID(ctx context.Context, responseID string) (AIClientResponse, error) {
	return scanAIClientResponse(r.db.QueryRow(ctx, clientResponseSelect+` where id=$1`, responseID))
}

const clientRiskSelect = `
	select connection_id,provider,identity_assurance,state,cooldown_until,requests_hour,requests_day,
		rate_limit_events,identity_changes,consecutive_failures,last_request_at,last_success_at,last_error_at,
		last_error_code,updated_at,hour_window_started_at,day_window_started_at,rate_limit_window_started_at,
		account_subject
	from ai_client_account_risk
`

func scanAIClientRisk(row pgx.Row) (AIClientRiskState, error) {
	var state AIClientRiskState
	err := row.Scan(&state.ConnectionID, &state.Provider, &state.IdentityAssurance, &state.State,
		&state.CooldownUntil, &state.RequestsHour, &state.RequestsDay, &state.RateLimitEvents,
		&state.IdentityChanges, &state.ConsecutiveFailures, &state.LastRequestAt, &state.LastSuccessAt,
		&state.LastErrorAt, &state.LastErrorCode, &state.UpdatedAt, &state.hourWindowStartedAt,
		&state.dayWindowStartedAt, &state.rateLimitWindowStartedAt, &state.accountSubject)
	return state, err
}

func normalizeAIClientRisk(state AIClientRiskState, now time.Time) AIClientRiskState {
	if now.Sub(state.hourWindowStartedAt) >= time.Hour || now.Before(state.hourWindowStartedAt) {
		state.hourWindowStartedAt, state.RequestsHour = now, 0
	}
	if now.Sub(state.dayWindowStartedAt) >= 24*time.Hour || now.Before(state.dayWindowStartedAt) {
		state.dayWindowStartedAt, state.RequestsDay = now, 0
	}
	if now.Sub(state.rateLimitWindowStartedAt) >= 24*time.Hour || now.Before(state.rateLimitWindowStartedAt) {
		state.rateLimitWindowStartedAt, state.RateLimitEvents = now, 0
	}
	if state.State == "cooldown" && state.CooldownUntil != nil && !state.CooldownUntil.After(now) {
		state.State, state.CooldownUntil = "normal", nil
	}
	return state
}

func applyAIClientRiskLimits(state *AIClientRiskState) {
	state.MinimumIntervalSeconds = int(AIClientMinimumInterval / time.Second)
	state.HourlyLimit = AIClientHourlyLimit
	state.DailyLimit = AIClientDailyLimit
}

func updateAIClientRisk(ctx context.Context, tx pgx.Tx, state AIClientRiskState) error {
	_, err := tx.Exec(ctx, `
		update ai_client_account_risk set state=$2,cooldown_until=$3,hour_window_started_at=$4,
			day_window_started_at=$5,rate_limit_window_started_at=$6,requests_hour=$7,requests_day=$8,
			rate_limit_events=$9,identity_changes=$10,consecutive_failures=$11,last_request_at=$12,
			last_success_at=$13,last_error_at=$14,last_error_code=$15,updated_at=$16,
			identity_assurance=$17
		where connection_id=$1
	`, state.ConnectionID, state.State, state.CooldownUntil, state.hourWindowStartedAt,
		state.dayWindowStartedAt, state.rateLimitWindowStartedAt, state.RequestsHour, state.RequestsDay,
		state.RateLimitEvents, state.IdentityChanges, state.ConsecutiveFailures, state.LastRequestAt,
		state.LastSuccessAt, state.LastErrorAt, state.LastErrorCode, state.UpdatedAt,
		state.IdentityAssurance)
	return err
}

func secureClientToken(size int) (string, error) {
	buffer := make([]byte, size)
	if _, err := rand.Read(buffer); err != nil {
		return "", fmt.Errorf("generate official client connector token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buffer), nil
}

func hashClientToken(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func clientTokenPrefix(value string) string {
	if len(value) <= 8 {
		return value
	}
	return value[:8]
}

func normalizedClientCapabilities(values []string) []string {
	seen := map[string]struct{}{}
	out := []string{}
	for _, value := range values {
		value = strings.ToLower(strings.TrimSpace(value))
		if value != "chatgpt" && value != "grok" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}
