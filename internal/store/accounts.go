package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type EmailToken struct {
	Token string
	Hash  string
}

type EncryptedCredential struct {
	Ciphertext  string
	Nonce       string
	Fingerprint string
	Mask        string
}

type PrivateChannel struct {
	PublicChannel
	ProbeDaily      int    `json:"probeDaily"`
	ProbesUsedToday int    `json:"probesUsedToday"`
	KeyMask         string `json:"keyMask"`
	KeyFingerprint  string `json:"keyFingerprint"`
	QuotaExhausted  bool   `json:"quotaExhausted"`
}

type PrivateChannelSummary struct {
	ID              string    `json:"id"`
	Name            string    `json:"name"`
	OwnerEmail      string    `json:"ownerEmail"`
	Provider        string    `json:"provider"`
	Model           string    `json:"model"`
	Endpoint        string    `json:"endpoint"`
	Status          string    `json:"status"`
	ProbeDaily      int       `json:"probeDaily"`
	ProbesUsedToday int       `json:"probesUsedToday"`
	LastProbeAt     time.Time `json:"lastProbeAt"`
}

type PrivateChannelInput struct {
	Name       string
	Provider   string
	Type       string
	Model      string
	Endpoint   string
	ProbeDaily int
	Credential EncryptedCredential
}

type PrivateCredential struct {
	ChannelID  string
	Ciphertext string
	Nonce      string
}

func NewEmailToken() EmailToken {
	token := "tok_" + uuid.NewString() + uuid.NewString()
	return EmailToken{Token: token, Hash: HashOpaqueToken(token)}
}

func HashOpaqueToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func (r *Repository) CreateUser(ctx context.Context, email string, passwordHash string, name string, emailVerified bool) (User, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	name = strings.TrimSpace(name)
	if name == "" {
		name = strings.Split(email, "@")[0]
	}
	avatar := userAvatarFromName(name)
	var user User
	err := r.db.QueryRow(ctx, `
		insert into users(id,email,password_hash,name,avatar,status,role,email_verified_at,created_at,updated_at)
		values($1,$2,$3,$4,$5,'active','user',case when $6 then now() else null end,now(),now())
		returning id,email,password_hash,name,avatar,status,role,plan,email_verified_at is not null
	`, "usr_"+uuid.NewString(), email, passwordHash, name, avatar, emailVerified).Scan(&user.ID, &user.Email, &user.PasswordHash, &user.Name, &user.Avatar, &user.Status, &user.Role, &user.Plan, &user.EmailVerified)
	return user, err
}

func (r *Repository) UpdateUserProfile(ctx context.Context, userID string, name string) (User, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return User{}, pgx.ErrNoRows
	}
	avatar := userAvatarFromName(name)
	var user User
	err := r.db.QueryRow(ctx, `
		update users
		set name=$2, avatar=$3, updated_at=now()
		where id=$1 and status='active'
		returning id,email,coalesce(username,''),password_hash,name,avatar,status,role,plan,email_verified_at is not null
	`, userID, name, avatar).Scan(&user.ID, &user.Email, &user.Username, &user.PasswordHash, &user.Name, &user.Avatar, &user.Status, &user.Role, &user.Plan, &user.EmailVerified)
	return user, err
}

func userAvatarFromName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return "U"
	}
	return strings.ToUpper(string([]rune(name)[0]))
}

func (r *Repository) CreateEmailToken(ctx context.Context, userID string, typ string, tokenHash string, ttl time.Duration) error {
	_, err := r.db.Exec(ctx, `
		insert into email_tokens(id,user_id,type,token_hash,expires_at)
		values($1,$2,$3,$4,$5)
	`, "emt_"+uuid.NewString(), userID, typ, tokenHash, time.Now().Add(ttl))
	return err
}

func (r *Repository) VerifyEmail(ctx context.Context, tokenHash string) (string, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var userID string
	if err := tx.QueryRow(ctx, `
		update email_tokens
		set used_at=now()
		where token_hash=$1 and type='verify_email' and used_at is null and expires_at > now()
		returning user_id
	`, tokenHash).Scan(&userID); err != nil {
		return "", err
	}
	if _, err := tx.Exec(ctx, `update users set email_verified_at=now(), updated_at=now() where id=$1`, userID); err != nil {
		return "", err
	}
	return userID, tx.Commit(ctx)
}

func (r *Repository) ResetPassword(ctx context.Context, tokenHash string, passwordHash string) (string, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var userID string
	if err := tx.QueryRow(ctx, `
		update email_tokens
		set used_at=now()
		where token_hash=$1 and type='reset_password' and used_at is null and expires_at > now()
		returning user_id
	`, tokenHash).Scan(&userID); err != nil {
		return "", err
	}
	if _, err := tx.Exec(ctx, `update users set password_hash=$2, updated_at=now() where id=$1`, userID, passwordHash); err != nil {
		return "", err
	}
	if _, err := tx.Exec(ctx, `update auth_sessions set revoked_at=now() where user_id=$1 and revoked_at is null`, userID); err != nil {
		return "", err
	}
	return userID, tx.Commit(ctx)
}

func (r *Repository) RevokeUserSessions(ctx context.Context, userID string, keepSessionHash string) error {
	_, err := r.db.Exec(ctx, `
		update auth_sessions
		set revoked_at=now()
		where user_id=$1 and revoked_at is null and session_hash<>$2
	`, userID, keepSessionHash)
	return err
}

func (r *Repository) FavoriteChannels(ctx context.Context, userID string) ([]PublicChannel, error) {
	rows, err := r.db.Query(ctx, publicChannelSQL(`
		join favorites f on f.channel_id=c.id and f.user_id=$1
		where c.owner_type='platform'
		order by f.created_at desc
	`, 0, 0), userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanPublicChannels(rows)
}

func (r *Repository) FavoriteIDs(ctx context.Context, userID string) ([]string, error) {
	rows, err := r.db.Query(ctx, `select channel_id from favorites where user_id=$1 order by created_at desc`, userID)
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

func (r *Repository) AddFavorite(ctx context.Context, userID string, channelID string) error {
	var targetExists bool
	if err := r.db.QueryRow(ctx, `
		with target as (
			select id from channels
			where id=$2
				and owner_type='platform'
				and public_visible is true
				and status not in ('disabled','deleted')
				and deleted_at is null
		), inserted as (
			insert into favorites(user_id, channel_id)
			select $1, id from target
			on conflict(user_id, channel_id) do nothing
			returning channel_id
		)
		select exists(select 1 from target)
	`, userID, channelID).Scan(&targetExists); err != nil {
		return err
	}
	if !targetExists {
		return pgx.ErrNoRows
	}
	return nil
}

func (r *Repository) RemoveFavorite(ctx context.Context, userID string, channelID string) error {
	tag, err := r.db.Exec(ctx, `delete from favorites where user_id=$1 and channel_id=$2`, userID, channelID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	return err
}

func (r *Repository) BulkRemoveFavorites(ctx context.Context, userID string, ids []string) ([]PublicChannel, []string, error) {
	ids = uniqueIDs(ids)
	if len(ids) == 0 {
		return nil, nil, ErrEmptyBulkSelection
	}
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return nil, nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	rows, err := tx.Query(ctx, `
		delete from favorites
		where user_id=$1 and channel_id=any($2)
		returning channel_id
	`, userID, ids)
	if err != nil {
		return nil, nil, err
	}
	removedIDs, err := collectStringRows(rows)
	if err != nil {
		return nil, nil, err
	}
	if len(removedIDs) != len(ids) {
		return nil, nil, pgx.ErrNoRows
	}
	for _, id := range removedIDs {
		if err := writeAuditTx(ctx, tx, AuditEvent{
			ActorType:  "user",
			ActorID:    userID,
			Action:     "favorite.bulk_removed",
			ObjectType: "channel",
			ObjectID:   id,
			Result:     "success",
			Metadata:   map[string]any{"bulk": true},
		}); err != nil {
			return nil, nil, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, nil, err
	}
	items, err := r.FavoriteChannels(ctx, userID)
	if err != nil {
		return nil, nil, err
	}
	favoriteIDs, err := r.FavoriteIDs(ctx, userID)
	return items, favoriteIDs, err
}

func (r *Repository) PrivateChannels(ctx context.Context, userID string) ([]PrivateChannel, error) {
	return r.PrivateChannelsForOrg(ctx, PersonalOrgID(userID))
}

func (r *Repository) PrivateChannelsForOrg(ctx context.Context, orgID string) ([]PrivateChannel, error) {
	rows, err := r.db.Query(ctx, privateChannelSQL("where c.owner_type='user' and coalesce(c.org_id,'org_' || coalesce(c.owner_id,''))=$1 and c.status <> 'deleted' and c.deleted_at is null order by c.created_at desc"), orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanPrivateChannels(rows)
}

func (r *Repository) PrivateChannel(ctx context.Context, userID string, channelID string) (PrivateChannel, error) {
	return r.PrivateChannelForOrg(ctx, PersonalOrgID(userID), channelID)
}

func (r *Repository) PrivateChannelForOrg(ctx context.Context, orgID string, channelID string) (PrivateChannel, error) {
	rows, err := r.db.Query(ctx, privateChannelSQL("where c.owner_type='user' and coalesce(c.org_id,'org_' || coalesce(c.owner_id,''))=$1 and c.id=$2 and c.status <> 'deleted' and c.deleted_at is null"), orgID, channelID)
	if err != nil {
		return PrivateChannel{}, err
	}
	defer rows.Close()
	items, err := scanPrivateChannels(rows)
	if err != nil {
		return PrivateChannel{}, err
	}
	if len(items) == 0 {
		return PrivateChannel{}, pgx.ErrNoRows
	}
	return items[0], nil
}

func (r *Repository) CreatePrivateChannel(ctx context.Context, userID string, input PrivateChannelInput) (PrivateChannel, error) {
	orgID, err := r.EnsurePersonalWorkspaceForUserID(ctx, userID)
	if err != nil {
		return PrivateChannel{}, err
	}
	return r.CreatePrivateChannelForOrg(ctx, orgID, userID, input)
}

func (r *Repository) CreatePrivateChannelForOrg(ctx context.Context, orgID string, actorID string, input PrivateChannelInput) (PrivateChannel, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return PrivateChannel{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	channelID := "pch_" + uuid.NewString()
	probeDaily := input.ProbeDaily
	if probeDaily <= 0 {
		probeDaily = 50
	}
	if orgID == "" {
		orgID = PersonalOrgID(actorID)
	}
	if err := ensureActiveOrg(ctx, tx, orgID); err != nil {
		return PrivateChannel{}, err
	}
	if _, err := tx.Exec(ctx, `
		insert into channels(id,owner_type,owner_id,org_id,name,provider,type,model,upstream_model,endpoint,status,score,probe_daily,probes_used_today,probe_reset_date,created_at,updated_at)
		values($1,'user',$2,$3,$4,$5,$6,$7,$7,$8,'unknown',0,$9,0,current_date,now(),now())
	`, channelID, actorID, orgID, input.Name, input.Provider, input.Type, input.Model, input.Endpoint, probeDaily); err != nil {
		return PrivateChannel{}, err
	}
	if _, err := tx.Exec(ctx, `
		insert into channel_credentials(id,channel_id,owner_id,key_ciphertext,key_nonce,key_fingerprint,key_mask,created_at,updated_at)
		values($1,$2,$3,$4,$5,$6,$7,now(),now())
	`, "cred_"+uuid.NewString(), channelID, actorID, input.Credential.Ciphertext, input.Credential.Nonce, input.Credential.Fingerprint, input.Credential.Mask); err != nil {
		return PrivateChannel{}, err
	}
	if _, err := tx.Exec(ctx, `
		insert into channel_status_snapshots(
			id,channel_id,sampled_at,status,score,uptime_24h,success_rate,latency_p95_ms,
			l1_status,l2_status,l3_status,l1_latency_ms,l2_latency_ms,l3_latency_ms,
			tokens_used,cost_usd,error_type,metadata
		)
		values($1,$2,now(),'unknown',0,0,0,0,'na','na','na',0,0,0,0,0,null,'{"source":"private_channel_created"}'::jsonb)
	`, "snap_private_"+channelID, channelID); err != nil {
		return PrivateChannel{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return PrivateChannel{}, err
	}
	return r.PrivateChannelForOrg(ctx, orgID, channelID)
}

func (r *Repository) UpdatePrivateChannel(ctx context.Context, userID string, channelID string, input PrivateChannelInput, updateCredential bool) (PrivateChannel, error) {
	return r.UpdatePrivateChannelForOrg(ctx, PersonalOrgID(userID), userID, channelID, input, updateCredential)
}

func (r *Repository) UpdatePrivateChannelForOrg(ctx context.Context, orgID string, actorID string, channelID string, input PrivateChannelInput, updateCredential bool) (PrivateChannel, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return PrivateChannel{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var currentProvider, currentType, currentModel, currentEndpoint string
	if err := tx.QueryRow(ctx, `
		select provider,type,model,endpoint
		from channels
		where id=$1 and coalesce(org_id,'org_' || coalesce(owner_id,''))=$2 and owner_type='user' and status <> 'deleted' and deleted_at is null
		for update
	`, channelID, orgID).Scan(&currentProvider, &currentType, &currentModel, &currentEndpoint); err != nil {
		return PrivateChannel{}, err
	}
	upstreamChanged := updateCredential ||
		currentProvider != input.Provider ||
		currentType != input.Type ||
		currentModel != input.Model ||
		currentEndpoint != input.Endpoint

	tag, err := tx.Exec(ctx, `
		update channels
		set name=$3,
			provider=$4,
			type=$5,
			model=$6,
			upstream_model=$6,
			endpoint=$7,
			probe_daily=$8,
			status=case when $9::boolean and status <> 'disabled' then 'unknown' else status end,
			score=case when $9::boolean and status <> 'disabled' then 0 else score end,
			updated_at=now()
		where id=$1 and coalesce(org_id,'org_' || coalesce(owner_id,''))=$2 and owner_type='user' and status <> 'deleted' and deleted_at is null
	`, channelID, orgID, input.Name, input.Provider, input.Type, input.Model, input.Endpoint, input.ProbeDaily, upstreamChanged)
	if err != nil {
		return PrivateChannel{}, err
	}
	if tag.RowsAffected() == 0 {
		return PrivateChannel{}, pgx.ErrNoRows
	}
	if updateCredential {
		if _, err := tx.Exec(ctx, `
			update channel_credentials
			set key_ciphertext=$3, key_nonce=$4, key_fingerprint=$5, key_mask=$6, updated_at=now()
			where channel_id=$1
				and exists(
					select 1 from channels c
					where c.id=$1 and coalesce(c.org_id,'org_' || coalesce(c.owner_id,''))=$2
				)
		`, channelID, orgID, input.Credential.Ciphertext, input.Credential.Nonce, input.Credential.Fingerprint, input.Credential.Mask); err != nil {
			return PrivateChannel{}, err
		}
	}
	if upstreamChanged {
		if _, err := tx.Exec(ctx, `update gateway_upstreams set enabled=false where channel_id=$1 and enabled is true`, channelID); err != nil {
			return PrivateChannel{}, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return PrivateChannel{}, err
	}
	_ = actorID
	return r.PrivateChannelForOrg(ctx, orgID, channelID)
}

func (r *Repository) DeletePrivateChannel(ctx context.Context, userID string, channelID string) error {
	return r.DeletePrivateChannelForOrg(ctx, PersonalOrgID(userID), userID, channelID)
}

func (r *Repository) DeletePrivateChannelForOrg(ctx context.Context, orgID string, actorID string, channelID string) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	tag, err := tx.Exec(ctx, `
		update channels
		set status='deleted',
			gateway_enabled=false,
			disabled_at=coalesce(disabled_at, now()),
			deleted_at=coalesce(deleted_at, now()),
			updated_at=now()
		where id=$1 and coalesce(org_id,'org_' || coalesce(owner_id,''))=$2 and owner_type='user' and status <> 'deleted' and deleted_at is null
	`, channelID, orgID)
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
			and exists(
				select 1 from channels c
				where c.id=$1 and coalesce(c.org_id,'org_' || coalesce(c.owner_id,''))=$2
			)
	`, channelID, orgID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (r *Repository) BulkUpdatePrivateChannelsStatus(ctx context.Context, userID string, ids []string, status string) ([]PrivateChannel, error) {
	return r.BulkUpdatePrivateChannelsStatusForOrg(ctx, PersonalOrgID(userID), userID, ids, status)
}

func (r *Repository) BulkUpdatePrivateChannelsStatusForOrg(ctx context.Context, orgID string, actorID string, ids []string, status string) ([]PrivateChannel, error) {
	ids = uniqueIDs(ids)
	if len(ids) == 0 {
		return nil, ErrEmptyBulkSelection
	}
	status = strings.ToLower(strings.TrimSpace(status))
	if status == "enabled" || status == "active" {
		status = "unknown"
	}
	if status != "disabled" && status != "unknown" {
		return nil, ErrInvalidAdminStatus
	}
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	rows, err := tx.Query(ctx, `
		update channels
		set status=$3,
			gateway_enabled=($3 <> 'disabled'),
			disabled_at=case when $3='disabled' then coalesce(disabled_at, now()) else null end,
			updated_at=now()
		where id=any($1) and coalesce(org_id,'org_' || coalesce(owner_id,''))=$2 and owner_type='user' and status <> 'deleted' and deleted_at is null
		returning id
	`, ids, orgID, status)
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
	if status == "disabled" {
		if _, err := tx.Exec(ctx, `update gateway_upstreams set enabled=false where channel_id=any($1)`, ids); err != nil {
			return nil, err
		}
	}
	for _, id := range updatedIDs {
		if err := writeAuditTx(ctx, tx, AuditEvent{
			ActorType:  "user",
			ActorID:    actorID,
			Action:     "private_channel.bulk_status",
			ObjectType: "channel",
			ObjectID:   id,
			Result:     "success",
			Metadata:   map[string]any{"bulk": true, "status": status},
		}); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return r.PrivateChannelsForOrg(ctx, orgID)
}

func (r *Repository) BulkDeletePrivateChannels(ctx context.Context, userID string, ids []string) ([]PrivateChannel, error) {
	return r.BulkDeletePrivateChannelsForOrg(ctx, PersonalOrgID(userID), userID, ids)
}

func (r *Repository) BulkDeletePrivateChannelsForOrg(ctx context.Context, orgID string, actorID string, ids []string) ([]PrivateChannel, error) {
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
			gateway_enabled=false,
			disabled_at=coalesce(disabled_at, now()),
			deleted_at=coalesce(deleted_at, now()),
			updated_at=now()
		where id=any($1) and coalesce(org_id,'org_' || coalesce(owner_id,''))=$2 and owner_type='user' and status <> 'deleted' and deleted_at is null
		returning id
	`, ids, orgID)
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
			and exists(
				select 1 from channels c
				where c.id=channel_credentials.channel_id and coalesce(c.org_id,'org_' || coalesce(c.owner_id,''))=$2
			)
	`, ids, orgID); err != nil {
		return nil, err
	}
	for _, id := range deletedIDs {
		if err := writeAuditTx(ctx, tx, AuditEvent{
			ActorType:  "user",
			ActorID:    actorID,
			Action:     "private_channel.bulk_deleted",
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
	return r.PrivateChannelsForOrg(ctx, orgID)
}

func (r *Repository) PrivateCredential(ctx context.Context, userID string, channelID string) (PrivateCredential, error) {
	return r.PrivateCredentialForOrg(ctx, PersonalOrgID(userID), channelID)
}

func (r *Repository) PrivateCredentialForOrg(ctx context.Context, orgID string, channelID string) (PrivateCredential, error) {
	var cred PrivateCredential
	err := r.db.QueryRow(ctx, `
		select cc.channel_id, cc.key_ciphertext, cc.key_nonce
		from channel_credentials cc
		join channels c on c.id=cc.channel_id
		where cc.channel_id=$1 and coalesce(c.org_id,'org_' || coalesce(c.owner_id,''))=$2 and c.owner_type='user' and c.status <> 'deleted' and c.deleted_at is null
	`, channelID, orgID).Scan(&cred.ChannelID, &cred.Ciphertext, &cred.Nonce)
	return cred, err
}

func (r *Repository) ReservePrivateL3Probe(ctx context.Context, userID string, channelID string) (bool, error) {
	return r.ReservePrivateL3ProbeForOrg(ctx, PersonalOrgID(userID), channelID)
}

func (r *Repository) ReservePrivateL3ProbeForOrg(ctx context.Context, orgID string, channelID string) (bool, error) {
	tag, err := r.db.Exec(ctx, `
		update channels
		set
			probes_used_today = case when probe_reset_date < current_date then 1 else probes_used_today + 1 end,
			probe_reset_date = current_date,
			updated_at = now()
		where id=$1 and coalesce(org_id,'org_' || coalesce(owner_id,''))=$2 and owner_type='user'
			and status <> 'deleted' and deleted_at is null
			and (
				case when probe_reset_date < current_date then 0 else probes_used_today end
			) < probe_daily
	`, channelID, orgID)
	return tag.RowsAffected() == 1, err
}

func (r *Repository) AdminPrivateChannelSummaries(ctx context.Context) ([]PrivateChannelSummary, error) {
	rows, err := r.db.Query(ctx, `
		select c.id,c.name,u.email,c.provider,c.model,c.endpoint,c.status,c.probe_daily,c.probes_used_today,
			coalesce(s.sampled_at,c.updated_at)
		from channels c
		join users u on u.id=c.owner_id
		left join lateral (
			select sampled_at from channel_status_snapshots ss where ss.channel_id=c.id order by sampled_at desc limit 1
		) s on true
			where c.owner_type='user' and c.status <> 'deleted' and c.deleted_at is null
				and u.status <> 'deleted' and u.deleted_at is null
			order by c.created_at desc
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []PrivateChannelSummary{}
	for rows.Next() {
		var item PrivateChannelSummary
		if err := rows.Scan(&item.ID, &item.Name, &item.OwnerEmail, &item.Provider, &item.Model, &item.Endpoint, &item.Status, &item.ProbeDaily, &item.ProbesUsedToday, &item.LastProbeAt); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func privateChannelSQL(tail string) string {
	return `
		select c.id,coalesce(c.public_slug,''),c.name,c.provider,c.type,c.model,c.upstream_model,c.endpoint,coalesce(c.official_site_url,''),c.status,c.score,
			coalesce(s.uptime_24h,0),coalesce(s.success_rate,0),coalesce(s.latency_p95_ms,0),
			coalesce(s.l1_status,'na'),coalesce(s.l2_status,'na'),coalesce(s.l3_status,'na'),
			coalesce(s.l1_latency_ms,0),coalesce(s.l2_latency_ms,0),coalesce(s.l3_latency_ms,0),
			coalesce(s.tokens_used,0),coalesce(s.cost_usd,0),coalesce(s.error_type,''),coalesce(s.sampled_at,c.updated_at),c.updated_at,
			c.probe_daily,c.probes_used_today,cc.key_mask,cc.key_fingerprint
		from channels c
		join channel_credentials cc on cc.channel_id=c.id
		left join lateral (
			select * from channel_status_snapshots ss where ss.channel_id=c.id order by sampled_at desc limit 1
		) s on true
		` + tail
}

func scanPrivateChannels(rows pgx.Rows) ([]PrivateChannel, error) {
	out := []PrivateChannel{}
	for rows.Next() {
		var item PrivateChannel
		var cost float64
		var updatedAt time.Time
		if err := rows.Scan(&item.ID, &item.PublicSlug, &item.Name, &item.Provider, &item.Type, &item.Model, &item.UpstreamModel, &item.Endpoint, &item.OfficialSiteURL, &item.Status, &item.Score,
			&item.Uptime24h, &item.SuccessRate, &item.LatencyP95Ms, &item.L1Status, &item.L2Status, &item.L3Status, &item.L1LatencyMs, &item.L2LatencyMs, &item.L3LatencyMs,
			&item.TokensUsed, &cost, &item.ErrorType, &item.LastProbeAt, &updatedAt, &item.ProbeDaily, &item.ProbesUsedToday, &item.KeyMask, &item.KeyFingerprint); err != nil {
			return nil, err
		}
		item.CostUSD = round3(cost)
		item.StatusLabel = statusLabel(item.Status)
		item.Diagnosis = channelDiagnosis(item.PublicChannel)
		item.Mark = providerMark(item.Provider)
		item.Trend = singlePointTrend(item.Score)
		item.QuotaExhausted = item.ProbeDaily > 0 && item.ProbesUsedToday >= item.ProbeDaily
		out = append(out, item)
	}
	return out, rows.Err()
}
