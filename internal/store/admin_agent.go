package store

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

var (
	ErrInvalidAdminAgentTokenName    = errors.New("admin agent token name is required")
	ErrInvalidAdminAgentTokenScope   = errors.New("invalid admin agent token scope")
	ErrAdminAgentIdempotencyConflict = errors.New("admin agent idempotency key was already used")
)

var adminAgentScopeSet = map[string]bool{
	"admin:read":      true,
	"admin:write":     true,
	"admin:dangerous": true,
	"admin:secrets":   true,
	"admin:export":    true,
}

type AdminAgentToken struct {
	ID             string     `json:"id"`
	Name           string     `json:"name"`
	TokenPrefix    string     `json:"tokenPrefix"`
	TokenMask      string     `json:"tokenMask"`
	PlainToken     string     `json:"plainToken,omitempty"`
	Scopes         []string   `json:"scopes"`
	CreatedBy      string     `json:"createdBy"`
	CreatedByEmail string     `json:"createdByEmail"`
	ExpiresAt      *time.Time `json:"expiresAt,omitempty"`
	RevokedAt      *time.Time `json:"revokedAt,omitempty"`
	LastUsedAt     *time.Time `json:"lastUsedAt,omitempty"`
	CreatedAt      time.Time  `json:"createdAt"`
}

type AdminAgentTokenCreateInput struct {
	Name      string
	Scopes    []string
	ExpiresAt *time.Time
	ActorID   string
}

type AuthenticatedAdminAgent struct {
	Token AdminAgentToken
	User  PublicUser
}

func (r *Repository) CreateAdminAgentToken(ctx context.Context, input AdminAgentTokenCreateInput) (AdminAgentToken, error) {
	name := strings.TrimSpace(input.Name)
	if name == "" {
		return AdminAgentToken{}, ErrInvalidAdminAgentTokenName
	}
	scopes, err := NormalizeAdminAgentScopes(input.Scopes)
	if err != nil {
		return AdminAgentToken{}, err
	}
	plain, err := NewAdminAgentPlainToken()
	if err != nil {
		return AdminAgentToken{}, err
	}
	item := AdminAgentToken{
		ID:          "aat_" + uuid.NewString(),
		Name:        name,
		TokenPrefix: AdminAgentTokenPrefix(plain),
		TokenMask:   MaskAdminAgentToken(plain),
		PlainToken:  plain,
		Scopes:      scopes,
		CreatedBy:   input.ActorID,
		ExpiresAt:   input.ExpiresAt,
	}
	scopesJSON, _ := json.Marshal(scopes)
	err = r.db.QueryRow(ctx, `
		insert into admin_agent_tokens(id,name,token_hash,token_prefix,token_mask,scopes,created_by,expires_at,created_at)
		values($1,$2,$3,$4,$5,$6,$7,$8,now())
		returning created_at
	`, item.ID, item.Name, HashOpaqueToken(plain), item.TokenPrefix, item.TokenMask, scopesJSON, input.ActorID, input.ExpiresAt).Scan(&item.CreatedAt)
	if err != nil {
		return AdminAgentToken{}, err
	}
	if err := r.db.QueryRow(ctx, `select email from users where id=$1`, input.ActorID).Scan(&item.CreatedByEmail); err != nil {
		item.CreatedByEmail = ""
	}
	_ = r.WriteAudit(ctx, AuditEvent{
		ActorType:  "user",
		ActorID:    input.ActorID,
		Action:     "admin.agent_token.created",
		ObjectType: "admin_agent_token",
		ObjectID:   item.ID,
		Result:     "success",
		Metadata:   map[string]any{"name": item.Name, "scopes": item.Scopes, "token_prefix": item.TokenPrefix},
	})
	return item, nil
}

func (r *Repository) ListAdminAgentTokens(ctx context.Context) ([]AdminAgentToken, error) {
	rows, err := r.db.Query(ctx, `
		select t.id,t.name,t.token_prefix,t.token_mask,t.scopes,t.created_by,coalesce(u.email,''),t.expires_at,t.revoked_at,t.last_used_at,t.created_at
		from admin_agent_tokens t
		left join users u on u.id=t.created_by
		order by t.created_at desc
		limit 200
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []AdminAgentToken{}
	for rows.Next() {
		var item AdminAgentToken
		var rawScopes []byte
		if err := rows.Scan(&item.ID, &item.Name, &item.TokenPrefix, &item.TokenMask, &rawScopes, &item.CreatedBy, &item.CreatedByEmail, nullableTimePtr(&item.ExpiresAt), nullableTimePtr(&item.RevokedAt), nullableTimePtr(&item.LastUsedAt), &item.CreatedAt); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(rawScopes, &item.Scopes)
		out = append(out, item)
	}
	return out, rows.Err()
}

func (r *Repository) RevokeAdminAgentToken(ctx context.Context, tokenID string, actorID string) (AdminAgentToken, error) {
	var item AdminAgentToken
	var rawScopes []byte
	err := r.db.QueryRow(ctx, `
		update admin_agent_tokens
		set revoked_at=coalesce(revoked_at, now())
		where id=$1
		returning id,name,token_prefix,token_mask,scopes,created_by,expires_at,revoked_at,last_used_at,created_at
	`, strings.TrimSpace(tokenID)).Scan(&item.ID, &item.Name, &item.TokenPrefix, &item.TokenMask, &rawScopes, &item.CreatedBy, nullableTimePtr(&item.ExpiresAt), nullableTimePtr(&item.RevokedAt), nullableTimePtr(&item.LastUsedAt), &item.CreatedAt)
	if err != nil {
		return AdminAgentToken{}, err
	}
	_ = json.Unmarshal(rawScopes, &item.Scopes)
	_ = r.db.QueryRow(ctx, `select email from users where id=$1`, item.CreatedBy).Scan(&item.CreatedByEmail)
	_ = r.WriteAudit(ctx, AuditEvent{
		ActorType:  "user",
		ActorID:    actorID,
		Action:     "admin.agent_token.revoked",
		ObjectType: "admin_agent_token",
		ObjectID:   item.ID,
		Result:     "success",
		Metadata:   map[string]any{"name": item.Name, "token_prefix": item.TokenPrefix},
	})
	return item, nil
}

func (r *Repository) AuthenticateAdminAgentToken(ctx context.Context, plain string) (AuthenticatedAdminAgent, error) {
	plain = strings.TrimSpace(plain)
	if plain == "" {
		return AuthenticatedAdminAgent{}, pgx.ErrNoRows
	}
	var item AdminAgentToken
	var user User
	var rawScopes []byte
	err := r.db.QueryRow(ctx, `
		select t.id,t.name,t.token_prefix,t.token_mask,t.scopes,t.created_by,coalesce(u.email,''),t.expires_at,t.revoked_at,t.last_used_at,t.created_at,
			u.id,u.email,coalesce(u.username,''),u.password_hash,u.name,u.avatar,u.status,u.role,u.plan,u.email_verified_at is not null
		from admin_agent_tokens t
		join users u on u.id=t.created_by
		where t.token_hash=$1
			and t.revoked_at is null
			and (t.expires_at is null or t.expires_at > now())
			and u.status='active'
			and u.role in ('owner','admin')
	`, HashOpaqueToken(plain)).Scan(&item.ID, &item.Name, &item.TokenPrefix, &item.TokenMask, &rawScopes, &item.CreatedBy, &item.CreatedByEmail, nullableTimePtr(&item.ExpiresAt), nullableTimePtr(&item.RevokedAt), nullableTimePtr(&item.LastUsedAt), &item.CreatedAt, &user.ID, &user.Email, &user.Username, &user.PasswordHash, &user.Name, &user.Avatar, &user.Status, &user.Role, &user.Plan, &user.EmailVerified)
	if err != nil {
		return AuthenticatedAdminAgent{}, err
	}
	_ = json.Unmarshal(rawScopes, &item.Scopes)
	_, _ = r.db.Exec(ctx, `update admin_agent_tokens set last_used_at=now() where id=$1`, item.ID)
	return AuthenticatedAdminAgent{Token: item, User: user.Public()}, nil
}

func (r *Repository) RecordAdminAgentIdempotencyKey(ctx context.Context, tokenID string, key string, method string, path string) error {
	key = strings.TrimSpace(key)
	if key == "" {
		return nil
	}
	_, err := r.db.Exec(ctx, `
		insert into admin_agent_idempotency_keys(id,token_id,idempotency_key,method,path,created_at)
		values($1,$2,$3,$4,$5,now())
	`, "aik_"+uuid.NewString(), tokenID, key, strings.ToUpper(strings.TrimSpace(method)), strings.TrimSpace(path))
	if isUniqueViolation(err) {
		return ErrAdminAgentIdempotencyConflict
	}
	return err
}

func NormalizeAdminAgentScopes(scopes []string) ([]string, error) {
	if len(scopes) == 0 {
		return nil, ErrInvalidAdminAgentTokenScope
	}
	seen := map[string]bool{}
	for _, raw := range scopes {
		scope := strings.ToLower(strings.TrimSpace(raw))
		if scope == "admin:*" {
			for candidate := range adminAgentScopeSet {
				seen[candidate] = true
			}
			continue
		}
		if !adminAgentScopeSet[scope] {
			return nil, ErrInvalidAdminAgentTokenScope
		}
		seen[scope] = true
	}
	ordered := []string{"admin:read", "admin:write", "admin:dangerous", "admin:secrets", "admin:export"}
	out := make([]string, 0, len(seen))
	for _, scope := range ordered {
		if seen[scope] {
			out = append(out, scope)
		}
	}
	if len(out) == 0 {
		return nil, ErrInvalidAdminAgentTokenScope
	}
	return out, nil
}

func NewAdminAgentPlainToken() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return "aat_" + base64.RawURLEncoding.EncodeToString(raw), nil
}

func AdminAgentTokenPrefix(token string) string {
	token = strings.TrimSpace(token)
	if len(token) <= 16 {
		return token
	}
	return token[:16]
}

func MaskAdminAgentToken(token string) string {
	prefix := AdminAgentTokenPrefix(token)
	if prefix == "" {
		return ""
	}
	return prefix + "...hidden"
}

func AdminAgentScopesContain(scopes []string, required string) bool {
	for _, scope := range scopes {
		if scope == required {
			return true
		}
	}
	return false
}
