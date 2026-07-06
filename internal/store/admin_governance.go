package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

var (
	ErrProtectedOwner            = errors.New("at least one active owner must remain")
	ErrProtectedOrg              = errors.New("default platform org cannot be suspended")
	ErrInvalidAdminStatus        = errors.New("invalid admin status")
	ErrInvalidAdminRole          = errors.New("invalid admin role")
	ErrInvalidAdminPlan          = errors.New("invalid admin plan")
	ErrInvalidUserPlan           = errors.New("invalid user plan")
	ErrInvalidOpenAPIStatus      = errors.New("invalid Open API site status")
	ErrInvalidOpenAPIScope       = errors.New("invalid Open API scope")
	ErrEmptyBulkSelection        = errors.New("select at least one item")
	ErrAdminUserIdentifierExists = errors.New("email or username already exists")
)

type AdminUserItem struct {
	ID              string     `json:"id"`
	Email           string     `json:"email"`
	Username        string     `json:"username"`
	Name            string     `json:"name"`
	Avatar          string     `json:"avatar"`
	Role            string     `json:"role"`
	Plan            string     `json:"plan"`
	Status          string     `json:"status"`
	EmailVerified   bool       `json:"emailVerified"`
	DataOrigin      string     `json:"dataOrigin"`
	Orgs            int        `json:"orgs"`
	Gateways        int        `json:"gateways"`
	PrivateChannels int        `json:"privateChannels"`
	AuditEvents     int        `json:"auditEvents"`
	LastActiveAt    *time.Time `json:"lastActiveAt,omitempty"`
	CreatedAt       time.Time  `json:"createdAt"`
	UpdatedAt       time.Time  `json:"updatedAt"`
	DeletedAt       *time.Time `json:"deletedAt,omitempty"`
}

type AdminUsersResult struct {
	Items []AdminUserItem `json:"items"`
	Stats map[string]int  `json:"stats"`
}

type AdminOrgItem struct {
	ID              string     `json:"id"`
	Name            string     `json:"name"`
	Slug            string     `json:"slug"`
	Plan            string     `json:"plan"`
	Status          string     `json:"status"`
	Timezone        string     `json:"timezone"`
	DataOrigin      string     `json:"dataOrigin"`
	Members         int        `json:"members"`
	Gateways        int        `json:"gateways"`
	PrivateChannels int        `json:"privateChannels"`
	ActiveKeys      int        `json:"activeKeys"`
	AuditEvents     int        `json:"auditEvents"`
	CreatedAt       time.Time  `json:"createdAt"`
	UpdatedAt       time.Time  `json:"updatedAt"`
	SuspendedAt     *time.Time `json:"suspendedAt,omitempty"`
	DeletedAt       *time.Time `json:"deletedAt,omitempty"`
}

type AdminOrgsResult struct {
	Items []AdminOrgItem `json:"items"`
	Stats map[string]int `json:"stats"`
}

type AdminUserFilter struct {
	Query      string
	Status     string
	Role       string
	Plan       string
	DataOrigin string
}

type AdminOrgFilter struct {
	Query      string
	Status     string
	Plan       string
	DataOrigin string
}

type AdminUserInput struct {
	Email            string
	Username         string
	PasswordHash     string
	Name             string
	Role             string
	Plan             string
	Status           string
	EmailVerified    bool
	EmailVerifiedSet bool
	DataOrigin       string
}

type AdminOrgInput struct {
	Name       string
	Slug       string
	Plan       string
	Status     string
	Timezone   string
	DataOrigin string
}

type OpenAPISiteUpdateInput struct {
	Name     string
	Scopes   []string
	QPSLimit int
	Status   string
	ActorID  string
}

type ProductionHealthCheck struct {
	ID       string `json:"id"`
	Label    string `json:"label"`
	Status   string `json:"status"`
	Severity string `json:"severity"`
	Value    string `json:"value"`
	Message  string `json:"message"`
	Action   string `json:"action"`
}

type ProductionHealth struct {
	GeneratedAt time.Time               `json:"generatedAt"`
	Stats       map[string]int          `json:"stats"`
	Summary     map[string]int          `json:"summary"`
	Checks      []ProductionHealthCheck `json:"checks"`
}

type AdminSettingsSummary struct {
	PlatformChannels            int          `json:"platformChannels"`
	Users                       int          `json:"users"`
	Orgs                        int          `json:"orgs"`
	RecommendPicks              int          `json:"recommendPicks"`
	PrivateChannels             int          `json:"privateChannels"`
	Gateways                    int          `json:"gateways"`
	ActiveGateways              int          `json:"activeGateways"`
	ActiveGatewayKeys           int          `json:"activeGatewayKeys"`
	UsageRequestsMonth          int          `json:"usageRequestsMonth"`
	UsageCostMonth              float64      `json:"usageCostMonth"`
	EnabledAlertRules           int          `json:"enabledAlertRules"`
	EnabledAdminAlertRules      int          `json:"enabledAdminAlertRules"`
	EnabledNotificationChannels int          `json:"enabledNotificationChannels"`
	OpenAPISites                int          `json:"openApiSites"`
	ActiveSessions              int          `json:"activeSessions"`
	AuditToday                  int          `json:"auditToday"`
	PlatformOrg                 AdminOrgItem `json:"platformOrg"`
}

func (r *Repository) AdminUsers(ctx context.Context, filter AdminUserFilter) (AdminUsersResult, error) {
	where := []string{"true"}
	args := []any{}
	add := func(clause string, value any) {
		args = append(args, value)
		where = append(where, fmt.Sprintf(clause, len(args)))
	}
	if value := strings.TrimSpace(filter.Status); value != "" && value != "all" {
		add("u.status=$%d", value)
	}
	if value := strings.TrimSpace(filter.Role); value != "" && value != "all" {
		add("u.role=$%d", value)
	}
	if value := strings.TrimSpace(filter.Plan); value != "" && value != "all" {
		add("u.plan=$%d", normalizeUserPlan(value))
	}
	if value := strings.TrimSpace(filter.DataOrigin); value != "" && value != "all" {
		add("u.data_origin=$%d", value)
	}
	if value := strings.TrimSpace(filter.Query); value != "" {
		add("(u.email ilike '%%' || $%[1]d || '%%' or coalesce(u.username,'') ilike '%%' || $%[1]d || '%%' or u.name ilike '%%' || $%[1]d || '%%' or u.role ilike '%%' || $%[1]d || '%%' or u.plan ilike '%%' || $%[1]d || '%%' or u.data_origin ilike '%%' || $%[1]d || '%%')", value)
	}
	whereSQL := strings.Join(where, " and ")
	stats, err := r.adminUserStats(ctx, whereSQL, args)
	if err != nil {
		return AdminUsersResult{}, err
	}
	rows, err := r.db.Query(ctx, `
		select u.id,u.email,coalesce(u.username,''),u.name,u.avatar,u.role,u.plan,u.status,u.email_verified_at is not null,u.data_origin,u.last_active_at,u.created_at,u.updated_at,
			u.deleted_at,
			(select count(*) from org_members om where om.user_id=u.id and om.status='active'),
			(select count(*) from gateways g where g.created_by=u.id and g.status <> 'deleted'),
			(select count(*) from channels c where c.owner_type='user' and c.owner_id=u.id and c.status <> 'deleted' and c.deleted_at is null),
			(select count(*) from audit_events a where a.actor_id=u.id)
		from users u
		where `+whereSQL+`
		order by case when u.role in ('owner','admin') then 0 else 1 end, u.created_at desc
		limit 300
	`, args...)
	if err != nil {
		return AdminUsersResult{}, err
	}
	defer rows.Close()
	out := []AdminUserItem{}
	for rows.Next() {
		var item AdminUserItem
		if err := rows.Scan(&item.ID, &item.Email, &item.Username, &item.Name, &item.Avatar, &item.Role, &item.Plan, &item.Status, &item.EmailVerified, &item.DataOrigin, nullableTimePtr(&item.LastActiveAt), &item.CreatedAt, &item.UpdatedAt, nullableTimePtr(&item.DeletedAt), &item.Orgs, &item.Gateways, &item.PrivateChannels, &item.AuditEvents); err != nil {
			return AdminUsersResult{}, err
		}
		item.Plan = normalizeUserPlan(item.Plan)
		out = append(out, item)
	}
	return AdminUsersResult{Items: out, Stats: stats}, rows.Err()
}

type queryRower interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

func defaultAdminUserStats() map[string]int {
	return map[string]int{"total": 0, "active": 0, "suspended": 0, "disabled": 0, "deleted": 0, "verified": 0, "owners": 0, "admins": 0, "free": 0, "superVip": 0, "runtime": 0, "system": 0, "test": 0, "demo": 0}
}

func (r *Repository) adminUserStats(ctx context.Context, whereSQL string, args []any) (map[string]int, error) {
	return adminUserStats(ctx, r.db, whereSQL, args)
}

func adminUserStats(ctx context.Context, q queryRower, whereSQL string, args []any) (map[string]int, error) {
	stats := defaultAdminUserStats()
	var total, active, suspended, disabled, deleted, verified, owners, admins, free, superVip, runtime, system, test, demo int
	err := q.QueryRow(ctx, `
		select
			count(*),
			count(*) filter (where u.status='active'),
			count(*) filter (where u.status='suspended'),
			count(*) filter (where u.status='disabled'),
			count(*) filter (where u.status='deleted'),
			count(*) filter (where u.email_verified_at is not null),
			count(*) filter (where u.role='owner'),
			count(*) filter (where u.role='admin'),
			count(*) filter (where coalesce(u.plan,'') <> 'super_vip'),
			count(*) filter (where u.plan='super_vip'),
			count(*) filter (where u.data_origin='runtime'),
			count(*) filter (where u.data_origin='system'),
			count(*) filter (where u.data_origin='test'),
			count(*) filter (where u.data_origin='demo')
		from users u
		where `+whereSQL,
		args...,
	).Scan(
		&total,
		&active,
		&suspended,
		&disabled,
		&deleted,
		&verified,
		&owners,
		&admins,
		&free,
		&superVip,
		&runtime,
		&system,
		&test,
		&demo,
	)
	if err == nil {
		stats["total"] = total
		stats["active"] = active
		stats["suspended"] = suspended
		stats["disabled"] = disabled
		stats["deleted"] = deleted
		stats["verified"] = verified
		stats["owners"] = owners
		stats["admins"] = admins
		stats["free"] = free
		stats["superVip"] = superVip
		stats["runtime"] = runtime
		stats["system"] = system
		stats["test"] = test
		stats["demo"] = demo
	}
	return stats, err
}

func (r *Repository) CreateAdminUser(ctx context.Context, input AdminUserInput, actorID string) (AdminUserItem, error) {
	normalized, err := normalizeAdminUserInput(input, true)
	if err != nil {
		return AdminUserItem{}, err
	}
	id := "usr_" + uuid.NewString()
	avatar := avatarForName(normalized.Name)
	err = r.db.QueryRow(ctx, `
		insert into users(id,email,username,password_hash,name,avatar,status,role,plan,email_verified_at,data_origin,last_active_at,created_at,updated_at)
		values($1,$2,$3,$4,$5,$6,$7,$8,$9,case when $10 then now() else null end,$11,now(),now(),now())
		returning id
	`, id, normalized.Email, normalized.Username, normalized.PasswordHash, normalized.Name, avatar, normalized.Status, normalized.Role, normalized.Plan, normalized.EmailVerified, normalized.DataOrigin).Scan(&id)
	if err != nil {
		if isUniqueViolation(err) {
			return AdminUserItem{}, ErrAdminUserIdentifierExists
		}
		return AdminUserItem{}, err
	}
	orgID := "org_" + id
	_, _ = r.db.Exec(ctx, `
		insert into orgs(id,name,slug,plan,status,timezone,data_origin,created_at,updated_at)
		values($1,$2,$3,'starter','active','Asia/Shanghai','runtime',now(),now())
		on conflict(id) do nothing
	`, orgID, normalized.Name+" 的工作区", uniqueWorkspaceSlug(id))
	_, _ = r.db.Exec(ctx, `
		insert into org_members(org_id,user_id,role,group_name,status)
		values($1,$2,'owner','默认工作区','active')
		on conflict(org_id,user_id) do update set role='owner', status='active'
	`, orgID, id)
	_ = r.WriteAudit(ctx, AuditEvent{
		ActorType: "user", ActorID: actorID, Action: "admin.user.created", ObjectType: "user", ObjectID: id, Result: "success",
		Metadata: map[string]any{"email": normalized.Email, "username": normalized.Username, "role": normalized.Role, "plan": normalized.Plan, "status": normalized.Status},
	})
	return r.adminUser(ctx, id)
}

func (r *Repository) UpdateAdminUser(ctx context.Context, userID string, actorID string, input AdminUserInput) (AdminUserItem, error) {
	var current AdminUserItem
	if err := r.db.QueryRow(ctx, `select id,email,coalesce(username,''),name,role,plan,status,email_verified_at is not null,data_origin from users where id=$1`, userID).Scan(&current.ID, &current.Email, &current.Username, &current.Name, &current.Role, &current.Plan, &current.Status, &current.EmailVerified, &current.DataOrigin); err != nil {
		return AdminUserItem{}, err
	}
	if current.Status == "deleted" {
		return AdminUserItem{}, ErrInvalidAdminStatus
	}
	if strings.TrimSpace(input.Email) == "" {
		input.Email = current.Email
	}
	if strings.TrimSpace(input.Username) == "" {
		input.Username = current.Username
	}
	if strings.TrimSpace(input.Name) == "" {
		input.Name = current.Name
	}
	if strings.TrimSpace(input.Role) == "" {
		input.Role = current.Role
	}
	if strings.TrimSpace(input.Plan) == "" {
		input.Plan = current.Plan
	}
	if strings.TrimSpace(input.Status) == "" {
		input.Status = current.Status
	}
	if strings.TrimSpace(input.DataOrigin) == "" {
		input.DataOrigin = current.DataOrigin
	}
	if !input.EmailVerifiedSet {
		input.EmailVerified = current.EmailVerified
	}
	normalized, err := normalizeAdminUserInput(input, false)
	if err != nil {
		return AdminUserItem{}, err
	}
	if userID == actorID && normalized.Status != "active" {
		return AdminUserItem{}, ErrWorkspaceSelfRoleChange
	}
	if current.Role == "owner" && normalized.Role != "owner" {
		if err := r.ensureAnotherActiveOwner(ctx, userID); err != nil {
			return AdminUserItem{}, err
		}
	}
	if current.Role == "owner" && normalized.Status != "active" {
		if err := r.ensureAnotherActiveOwner(ctx, userID); err != nil {
			return AdminUserItem{}, err
		}
	}
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return AdminUserItem{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if strings.TrimSpace(normalized.PasswordHash) != "" {
		tag, err := tx.Exec(ctx, `
			update users
			set email=$2,username=$3,name=$4,avatar=$5,role=$6,plan=$7,status=$8,email_verified_at=case when $9 then coalesce(email_verified_at, now()) else null end,
				data_origin=$10,password_hash=$11,suspended_at=case when $8='active' then null else coalesce(suspended_at, now()) end,
				deleted_at=case when $8='deleted' then coalesce(deleted_at, now()) else null end,updated_at=now()
			where id=$1 and status <> 'deleted' and deleted_at is null
		`, userID, normalized.Email, normalized.Username, normalized.Name, avatarForName(normalized.Name), normalized.Role, normalized.Plan, normalized.Status, normalized.EmailVerified, normalized.DataOrigin, normalized.PasswordHash)
		if err != nil {
			if isUniqueViolation(err) {
				return AdminUserItem{}, ErrAdminUserIdentifierExists
			}
			return AdminUserItem{}, err
		}
		if tag.RowsAffected() == 0 {
			return AdminUserItem{}, pgx.ErrNoRows
		}
	} else {
		tag, err := tx.Exec(ctx, `
			update users
			set email=$2,username=$3,name=$4,avatar=$5,role=$6,plan=$7,status=$8,email_verified_at=case when $9 then coalesce(email_verified_at, now()) else null end,
				data_origin=$10,suspended_at=case when $8='active' then null else coalesce(suspended_at, now()) end,
				deleted_at=case when $8='deleted' then coalesce(deleted_at, now()) else null end,updated_at=now()
			where id=$1 and status <> 'deleted' and deleted_at is null
		`, userID, normalized.Email, normalized.Username, normalized.Name, avatarForName(normalized.Name), normalized.Role, normalized.Plan, normalized.Status, normalized.EmailVerified, normalized.DataOrigin)
		if err != nil {
			if isUniqueViolation(err) {
				return AdminUserItem{}, ErrAdminUserIdentifierExists
			}
			return AdminUserItem{}, err
		}
		if tag.RowsAffected() == 0 {
			return AdminUserItem{}, pgx.ErrNoRows
		}
	}
	if normalized.Status != "active" {
		if _, err := tx.Exec(ctx, `update auth_sessions set revoked_at=now() where user_id=$1 and revoked_at is null`, userID); err != nil {
			return AdminUserItem{}, err
		}
		if err := deactivateInactiveUsersGatewayAccessTx(ctx, tx, []string{userID}, actorID); err != nil {
			return AdminUserItem{}, err
		}
	}
	if normalized.Plan != "super_vip" {
		reason := "plan_not_super_vip"
		if normalizeUserPlan(current.Plan) == "super_vip" {
			reason = "super_vip_removed"
		}
		if err := revokePersonalPlatformGatewayUpstreamsTx(ctx, tx, userID, actorID, reason); err != nil {
			return AdminUserItem{}, err
		}
	}
	if err := writeAuditTx(ctx, tx, AuditEvent{
		ActorType: "user", ActorID: actorID, Action: "admin.user.updated", ObjectType: "user", ObjectID: userID, Result: "success",
		Metadata: map[string]any{"email": normalized.Email, "username": normalized.Username, "role": normalized.Role, "plan": normalized.Plan, "status": normalized.Status},
	}); err != nil {
		return AdminUserItem{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return AdminUserItem{}, err
	}
	return r.adminUser(ctx, userID)
}

func (r *Repository) UpdateAdminUserStatus(ctx context.Context, userID string, actorID string, status string) (AdminUserItem, error) {
	status, err := validateAdminStatus(status)
	if err != nil {
		return AdminUserItem{}, err
	}
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return AdminUserItem{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var role, currentStatus string
	if err := tx.QueryRow(ctx, `select role,status from users where id=$1 for update`, userID).Scan(&role, &currentStatus); err != nil {
		return AdminUserItem{}, err
	}
	if currentStatus == "deleted" {
		return AdminUserItem{}, ErrInvalidAdminStatus
	}
	if userID == actorID && status != "active" {
		return AdminUserItem{}, ErrWorkspaceSelfRoleChange
	}
	if role == "owner" && status != "active" {
		if err := ensureActiveOwnerOutside(ctx, tx, []string{userID}); err != nil {
			return AdminUserItem{}, err
		}
	}
	tag, err := tx.Exec(ctx, `
		update users
		set status=$2,
			suspended_at=case when $2='active' then null else coalesce(suspended_at, now()) end,
			deleted_at=case when $2='deleted' then coalesce(deleted_at, now()) else null end,
			updated_at=now()
		where id=$1 and status <> 'deleted' and deleted_at is null
	`, userID, status)
	if err != nil {
		return AdminUserItem{}, err
	}
	if tag.RowsAffected() == 0 {
		return AdminUserItem{}, pgx.ErrNoRows
	}
	if status != "active" {
		if _, err := tx.Exec(ctx, `update auth_sessions set revoked_at=now() where user_id=$1 and revoked_at is null`, userID); err != nil {
			return AdminUserItem{}, err
		}
		if err := deactivateInactiveUsersGatewayAccessTx(ctx, tx, []string{userID}, actorID); err != nil {
			return AdminUserItem{}, err
		}
	}
	if err := writeAuditTx(ctx, tx, AuditEvent{
		ActorType: "user", ActorID: actorID, Action: "admin.user.status.updated", ObjectType: "user", ObjectID: userID, Result: "success",
		Metadata: map[string]any{"status": status},
	}); err != nil {
		return AdminUserItem{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return AdminUserItem{}, err
	}
	return r.adminUser(ctx, userID)
}

func (r *Repository) UpdateAdminUserRole(ctx context.Context, userID string, actorID string, role string) (AdminUserItem, error) {
	role, err := validateAdminRole(role)
	if err != nil {
		return AdminUserItem{}, err
	}
	var currentRole, currentStatus string
	if err := r.db.QueryRow(ctx, `select role,status from users where id=$1`, userID).Scan(&currentRole, &currentStatus); err != nil {
		return AdminUserItem{}, err
	}
	if currentStatus == "deleted" {
		return AdminUserItem{}, ErrInvalidAdminStatus
	}
	if currentRole == "owner" && role != "owner" {
		if err := r.ensureAnotherActiveOwner(ctx, userID); err != nil {
			return AdminUserItem{}, err
		}
	}
	tag, err := r.db.Exec(ctx, `update users set role=$2, updated_at=now() where id=$1 and status <> 'deleted' and deleted_at is null`, userID, role)
	if err != nil {
		return AdminUserItem{}, err
	}
	if tag.RowsAffected() == 0 {
		return AdminUserItem{}, pgx.ErrNoRows
	}
	_ = r.WriteAudit(ctx, AuditEvent{
		ActorType: "user", ActorID: actorID, Action: "admin.user.role.updated", ObjectType: "user", ObjectID: userID, Result: "success",
		Metadata: map[string]any{"role": role},
	})
	return r.adminUser(ctx, userID)
}

func (r *Repository) DeleteAdminUser(ctx context.Context, userID string, actorID string) error {
	if userID == actorID {
		return ErrWorkspaceSelfRoleChange
	}
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var role string
	if err := tx.QueryRow(ctx, `select role from users where id=$1 and status <> 'deleted' and deleted_at is null for update`, userID).Scan(&role); err != nil {
		return err
	}
	if role == "owner" {
		if err := ensureActiveOwnerOutside(ctx, tx, []string{userID}); err != nil {
			return err
		}
	}
	tag, err := tx.Exec(ctx, `update users set status='deleted', deleted_at=now(), suspended_at=now(), updated_at=now() where id=$1 and status <> 'deleted' and deleted_at is null`, userID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	if _, err := tx.Exec(ctx, `update auth_sessions set revoked_at=now() where user_id=$1 and revoked_at is null`, userID); err != nil {
		return err
	}
	if err := deactivateDeletedUsersRuntimeResources(ctx, tx, []string{userID}, actorID); err != nil {
		return err
	}
	if err := writeAuditTx(ctx, tx, AuditEvent{ActorType: "user", ActorID: actorID, Action: "admin.user.deleted", ObjectType: "user", ObjectID: userID, Result: "success"}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (r *Repository) BulkUpdateAdminUsers(ctx context.Context, ids []string, actorID string, status string) (AdminUsersResult, error) {
	ids = uniqueIDs(ids)
	if len(ids) == 0 {
		return AdminUsersResult{}, ErrEmptyBulkSelection
	}
	status, err := validateAdminStatus(status)
	if err != nil {
		return AdminUsersResult{}, err
	}
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return AdminUsersResult{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	states, err := adminUserBulkStates(ctx, tx, ids)
	if err != nil {
		return AdminUsersResult{}, err
	}
	if status != "active" {
		if _, ok := states[actorID]; ok {
			return AdminUsersResult{}, ErrWorkspaceSelfRoleChange
		}
		if selectedAdminOwner(states) {
			if err := ensureActiveOwnerOutside(ctx, tx, ids); err != nil {
				return AdminUsersResult{}, err
			}
		}
	}
	tag, err := tx.Exec(ctx, `
		update users
		set status=$2,
			suspended_at=case when $2='active' then null else coalesce(suspended_at, now()) end,
			deleted_at=case when $2='deleted' then coalesce(deleted_at, now()) else null end,
			updated_at=now()
		where id=any($1) and status <> 'deleted' and deleted_at is null
	`, ids, status)
	if err != nil {
		return AdminUsersResult{}, err
	}
	if int(tag.RowsAffected()) != len(ids) {
		return AdminUsersResult{}, pgx.ErrNoRows
	}
	if status != "active" {
		if _, err := tx.Exec(ctx, `update auth_sessions set revoked_at=now() where user_id=any($1) and revoked_at is null`, ids); err != nil {
			return AdminUsersResult{}, err
		}
		if err := deactivateInactiveUsersGatewayAccessTx(ctx, tx, ids, actorID); err != nil {
			return AdminUsersResult{}, err
		}
	}
	for _, id := range ids {
		state := states[id]
		if err := writeAuditTx(ctx, tx, AuditEvent{
			ActorType: "user", ActorID: actorID, Action: "admin.user.bulk_status",
			ObjectType: "user", ObjectID: id, Result: "success",
			Metadata: map[string]any{"status": status, "previous_status": state.status, "role": state.role},
		}); err != nil {
			return AdminUsersResult{}, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return AdminUsersResult{}, err
	}
	return r.AdminUsers(ctx, AdminUserFilter{})
}

func (r *Repository) deactivateInactiveUsersGatewayAccess(ctx context.Context, userIDs []string, actorID string) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := deactivateInactiveUsersGatewayAccessTx(ctx, tx, userIDs, actorID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func deactivateInactiveUsersGatewayAccessTx(ctx context.Context, tx pgx.Tx, userIDs []string, actorID string) error {
	if len(userIDs) == 0 {
		return nil
	}
	orgIDs := personalOrgIDs(userIDs)
	if _, err := tx.Exec(ctx, `
		update gateways
		set status='paused', updated_at=now()
		where org_id=any($1) and status='active'
	`, orgIDs); err != nil {
		return err
	}
	rows, err := tx.Query(ctx, `
		update gateway_keys
		set status='revoked', revoked_at=coalesce(revoked_at, now())
		where org_id=any($1) and status='active' and deleted_at is null
		returning id,org_id
	`, orgIDs)
	if err != nil {
		return err
	}
	keyOrgs := map[string]string{}
	for rows.Next() {
		var keyID, orgID string
		if err := rows.Scan(&keyID, &orgID); err != nil {
			rows.Close()
			return err
		}
		keyOrgs[keyID] = orgID
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	for keyID, orgID := range keyOrgs {
		if err := writeAuditTx(ctx, tx, AuditEvent{
			ActorType:  "user",
			ActorID:    actorID,
			Action:     "admin.user.gateway_key_revoked",
			ObjectType: "gateway_key",
			ObjectID:   keyID,
			Result:     "success",
			Metadata:   map[string]any{"org_id": orgID, "reason": "user_inactive"},
		}); err != nil {
			return err
		}
	}
	return nil
}

func revokePersonalPlatformGatewayUpstreamsTx(ctx context.Context, tx pgx.Tx, userID string, actorID string, reason string) error {
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return nil
	}
	orgID := PersonalOrgID(userID)
	rows, err := tx.Query(ctx, `
		delete from gateway_upstreams gu
		using gateways g, channels c
		where gu.gateway_id=g.id
			and gu.channel_id=c.id
			and g.org_id=$1
			and c.owner_type='platform'
		returning gu.id,gu.gateway_id,gu.channel_id
	`, orgID)
	if err != nil {
		return err
	}
	removedGatewayIDs := []string{}
	removedChannelIDs := []string{}
	removedCount := 0
	for rows.Next() {
		var upstreamID, gatewayID, channelID string
		if err := rows.Scan(&upstreamID, &gatewayID, &channelID); err != nil {
			rows.Close()
			return err
		}
		_ = upstreamID
		removedGatewayIDs = append(removedGatewayIDs, gatewayID)
		removedChannelIDs = append(removedChannelIDs, channelID)
		removedCount++
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()

	pausedRows, err := tx.Query(ctx, `
		update gateways g
		set status='paused', updated_at=now()
		where g.org_id=$1
			and g.status='active'
			and not exists(select 1 from gateway_upstreams gu where gu.gateway_id=g.id)
		returning g.id
	`, orgID)
	if err != nil {
		return err
	}
	pausedGatewayIDs := []string{}
	for pausedRows.Next() {
		var gatewayID string
		if err := pausedRows.Scan(&gatewayID); err != nil {
			pausedRows.Close()
			return err
		}
		pausedGatewayIDs = append(pausedGatewayIDs, gatewayID)
	}
	if err := pausedRows.Err(); err != nil {
		pausedRows.Close()
		return err
	}
	pausedRows.Close()

	if removedCount == 0 && len(pausedGatewayIDs) == 0 {
		return nil
	}
	return writeAuditTx(ctx, tx, AuditEvent{
		ActorType:  "user",
		ActorID:    actorID,
		Action:     "admin.user.platform_gateway_access.revoked",
		ObjectType: "user",
		ObjectID:   userID,
		Result:     "success",
		Metadata: map[string]any{
			"org_id":            orgID,
			"reason":            reason,
			"removed_upstreams": removedCount,
			"gateway_ids":       uniqueStrings(removedGatewayIDs),
			"channel_ids":       uniqueStrings(removedChannelIDs),
			"paused_gateways":   pausedGatewayIDs,
		},
	})
}

func (r *Repository) BulkUpdateAdminUserRoles(ctx context.Context, ids []string, actorID string, role string) (AdminUsersResult, error) {
	ids = uniqueIDs(ids)
	if len(ids) == 0 {
		return AdminUsersResult{}, ErrEmptyBulkSelection
	}
	role, err := validateAdminRole(role)
	if err != nil {
		return AdminUsersResult{}, err
	}
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return AdminUsersResult{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	states, err := adminUserBulkStates(ctx, tx, ids)
	if err != nil {
		return AdminUsersResult{}, err
	}
	if role != "owner" && selectedAdminOwner(states) {
		if err := ensureActiveOwnerOutside(ctx, tx, ids); err != nil {
			return AdminUsersResult{}, err
		}
	}
	tag, err := tx.Exec(ctx, `
		update users
		set role=$2, updated_at=now()
		where id=any($1) and status <> 'deleted' and deleted_at is null
	`, ids, role)
	if err != nil {
		return AdminUsersResult{}, err
	}
	if int(tag.RowsAffected()) != len(ids) {
		return AdminUsersResult{}, pgx.ErrNoRows
	}
	for _, id := range ids {
		state := states[id]
		if err := writeAuditTx(ctx, tx, AuditEvent{
			ActorType: "user", ActorID: actorID, Action: "admin.user.bulk_role",
			ObjectType: "user", ObjectID: id, Result: "success",
			Metadata: map[string]any{"role": role, "previous_role": state.role, "status": state.status},
		}); err != nil {
			return AdminUsersResult{}, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return AdminUsersResult{}, err
	}
	return r.AdminUsers(ctx, AdminUserFilter{})
}

func (r *Repository) BulkDeleteAdminUsers(ctx context.Context, ids []string, actorID string) (AdminUsersResult, error) {
	ids = uniqueIDs(ids)
	if len(ids) == 0 {
		return AdminUsersResult{}, ErrEmptyBulkSelection
	}
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return AdminUsersResult{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	states, err := adminUserBulkStates(ctx, tx, ids)
	if err != nil {
		return AdminUsersResult{}, err
	}
	if _, ok := states[actorID]; ok {
		return AdminUsersResult{}, ErrWorkspaceSelfRoleChange
	}
	if selectedAdminOwner(states) {
		if err := ensureActiveOwnerOutside(ctx, tx, ids); err != nil {
			return AdminUsersResult{}, err
		}
	}
	tag, err := tx.Exec(ctx, `
		update users
		set status='deleted', deleted_at=now(), suspended_at=now(), updated_at=now()
		where id=any($1) and status <> 'deleted' and deleted_at is null
	`, ids)
	if err != nil {
		return AdminUsersResult{}, err
	}
	if int(tag.RowsAffected()) != len(ids) {
		return AdminUsersResult{}, pgx.ErrNoRows
	}
	if _, err := tx.Exec(ctx, `update auth_sessions set revoked_at=now() where user_id=any($1) and revoked_at is null`, ids); err != nil {
		return AdminUsersResult{}, err
	}
	if err := deactivateDeletedUsersRuntimeResources(ctx, tx, ids, actorID); err != nil {
		return AdminUsersResult{}, err
	}
	for _, id := range ids {
		state := states[id]
		if err := writeAuditTx(ctx, tx, AuditEvent{
			ActorType: "user", ActorID: actorID, Action: "admin.user.bulk_deleted",
			ObjectType: "user", ObjectID: id, Result: "success",
			Metadata: map[string]any{"previous_status": state.status, "role": state.role},
		}); err != nil {
			return AdminUsersResult{}, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return AdminUsersResult{}, err
	}
	return r.AdminUsers(ctx, AdminUserFilter{})
}

func deactivateDeletedUsersRuntimeResources(ctx context.Context, tx pgx.Tx, userIDs []string, actorID string) error {
	if len(userIDs) == 0 {
		return nil
	}
	orgIDs := personalOrgIDs(userIDs)
	if _, err := tx.Exec(ctx, `
		update gateways
		set status='deleted', updated_at=now()
		where org_id=any($1) and status <> 'deleted'
	`, orgIDs); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		update gateway_keys
		set status='revoked', revoked_at=coalesce(revoked_at, now())
		where org_id=any($1) and status='active' and deleted_at is null
	`, orgIDs); err != nil {
		return err
	}
	rows, err := tx.Query(ctx, `
		update channels
		set status='deleted',
			gateway_enabled=false,
			disabled_at=coalesce(disabled_at, now()),
			deleted_at=coalesce(deleted_at, now()),
			updated_at=now()
		where owner_type='user' and owner_id=any($1) and status <> 'deleted' and deleted_at is null
		returning id,owner_id
	`, userIDs)
	if err != nil {
		return err
	}
	channelIDs := []string{}
	channelOwners := map[string]string{}
	for rows.Next() {
		var channelID, ownerID string
		if err := rows.Scan(&channelID, &ownerID); err != nil {
			rows.Close()
			return err
		}
		channelIDs = append(channelIDs, channelID)
		channelOwners[channelID] = ownerID
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	if _, err := tx.Exec(ctx, `update org_members set status='removed' where user_id=any($1) and status <> 'removed'`, userIDs); err != nil {
		return err
	}
	if len(channelIDs) == 0 {
		return nil
	}
	if _, err := tx.Exec(ctx, `update gateway_upstreams set enabled=false where channel_id=any($1)`, channelIDs); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		update channel_credentials
		set key_ciphertext='',
			key_nonce='',
			key_mask='deleted',
			key_fingerprint='deleted:' || channel_id,
			updated_at=now()
		where channel_id=any($1)
	`, channelIDs); err != nil {
		return err
	}
	for _, channelID := range channelIDs {
		if err := writeAuditTx(ctx, tx, AuditEvent{
			ActorType:  "user",
			ActorID:    actorID,
			Action:     "admin.user.private_channel_deleted",
			ObjectType: "channel",
			ObjectID:   channelID,
			Result:     "success",
			Metadata:   map[string]any{"owner_id": channelOwners[channelID], "credential_scrubbed": true},
		}); err != nil {
			return err
		}
	}
	return nil
}

func personalOrgIDs(userIDs []string) []string {
	orgIDs := make([]string, 0, len(userIDs))
	for _, id := range userIDs {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		orgIDs = append(orgIDs, PersonalOrgID(id))
	}
	return orgIDs
}

type adminUserBulkState struct {
	role   string
	status string
}

func adminUserBulkStates(ctx context.Context, q DBTX, ids []string) (map[string]adminUserBulkState, error) {
	rows, err := q.Query(ctx, `
		select id,role,status
		from users
		where id=any($1) and status <> 'deleted' and deleted_at is null
	`, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	states := map[string]adminUserBulkState{}
	for rows.Next() {
		var id string
		var state adminUserBulkState
		if err := rows.Scan(&id, &state.role, &state.status); err != nil {
			return nil, err
		}
		states[id] = state
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(states) != len(ids) {
		return nil, pgx.ErrNoRows
	}
	return states, nil
}

func selectedAdminOwner(states map[string]adminUserBulkState) bool {
	for _, state := range states {
		if state.role == "owner" && state.status == "active" {
			return true
		}
	}
	return false
}

func (r *Repository) AdminOrgs(ctx context.Context, filter AdminOrgFilter) (AdminOrgsResult, error) {
	where := []string{"true"}
	args := []any{DefaultOrgID}
	add := func(clause string, value any) {
		args = append(args, value)
		where = append(where, fmt.Sprintf(clause, len(args)))
	}
	if value := strings.TrimSpace(filter.Status); value != "" && value != "all" {
		add("o.status=$%d", value)
	}
	if value := strings.TrimSpace(filter.Plan); value != "" && value != "all" {
		add("o.plan=$%d", value)
	}
	if value := strings.TrimSpace(filter.DataOrigin); value != "" && value != "all" {
		add("o.data_origin=$%d", value)
	}
	if value := strings.TrimSpace(filter.Query); value != "" {
		add("(o.name ilike '%%' || $%[1]d || '%%' or o.slug ilike '%%' || $%[1]d || '%%' or o.plan ilike '%%' || $%[1]d || '%%' or o.data_origin ilike '%%' || $%[1]d || '%%')", value)
	}
	whereSQL := strings.Join(where, " and ")
	stats, err := r.adminOrgStats(ctx, whereSQL, args)
	if err != nil {
		return AdminOrgsResult{}, err
	}
	rows, err := r.db.Query(ctx, `
		select o.id,o.name,o.slug,o.plan,o.status,o.timezone,o.data_origin,o.created_at,o.updated_at,o.suspended_at,
			o.deleted_at,
			(select count(*) from org_members om where om.org_id=o.id and om.status='active'),
			(select count(*) from gateways g where g.org_id=o.id and g.status <> 'deleted'),
			(select count(*) from channels c where c.owner_type='user' and coalesce(c.org_id,'org_' || coalesce(c.owner_id,''))=o.id and c.status <> 'deleted' and c.deleted_at is null),
			(select count(*) from gateway_keys k where k.org_id=o.id and k.status='active' and k.deleted_at is null),
			(select count(*) from audit_events a where a.actor_id in (select user_id from org_members where org_id=o.id))
		from orgs o
		where `+whereSQL+`
		order by case when o.id=$1 then 0 else 1 end, o.created_at desc
		limit 300
	`, args...)
	if err != nil {
		return AdminOrgsResult{}, err
	}
	defer rows.Close()
	out := []AdminOrgItem{}
	for rows.Next() {
		var item AdminOrgItem
		if err := rows.Scan(&item.ID, &item.Name, &item.Slug, &item.Plan, &item.Status, &item.Timezone, &item.DataOrigin, &item.CreatedAt, &item.UpdatedAt, nullableTimePtr(&item.SuspendedAt), nullableTimePtr(&item.DeletedAt), &item.Members, &item.Gateways, &item.PrivateChannels, &item.ActiveKeys, &item.AuditEvents); err != nil {
			return AdminOrgsResult{}, err
		}
		out = append(out, item)
	}
	return AdminOrgsResult{Items: out, Stats: stats}, rows.Err()
}

func defaultAdminOrgStats() map[string]int {
	return map[string]int{"total": 0, "active": 0, "suspended": 0, "disabled": 0, "deleted": 0, "system": 0, "runtime": 0, "test": 0, "demo": 0}
}

func (r *Repository) adminOrgStats(ctx context.Context, whereSQL string, args []any) (map[string]int, error) {
	return adminOrgStats(ctx, r.db, whereSQL, args)
}

func adminOrgStats(ctx context.Context, q queryRower, whereSQL string, args []any) (map[string]int, error) {
	stats := defaultAdminOrgStats()
	var total, active, suspended, disabled, deleted, system, runtime, test, demo int
	err := q.QueryRow(ctx, `
		select
			count(*),
			count(*) filter (where o.status='active'),
			count(*) filter (where o.status='suspended'),
			count(*) filter (where o.status='disabled'),
			count(*) filter (where o.status='deleted'),
			count(*) filter (where o.data_origin='system'),
			count(*) filter (where o.data_origin='runtime'),
			count(*) filter (where o.data_origin='test'),
			count(*) filter (where o.data_origin='demo')
		from orgs o
		where ($1::text is not null or $1::text is null) and `+whereSQL,
		args...,
	).Scan(
		&total,
		&active,
		&suspended,
		&disabled,
		&deleted,
		&system,
		&runtime,
		&test,
		&demo,
	)
	if err == nil {
		stats["total"] = total
		stats["active"] = active
		stats["suspended"] = suspended
		stats["disabled"] = disabled
		stats["deleted"] = deleted
		stats["system"] = system
		stats["runtime"] = runtime
		stats["test"] = test
		stats["demo"] = demo
	}
	return stats, err
}

func (r *Repository) CreateAdminOrg(ctx context.Context, input AdminOrgInput, actorID string) (AdminOrgItem, error) {
	normalized, err := normalizeAdminOrgInput(input, true)
	if err != nil {
		return AdminOrgItem{}, err
	}
	id := "org_" + uuid.NewString()
	if normalized.Slug == "" {
		normalized.Slug = uniqueOrgSlug(normalized.Name, id)
	}
	err = r.db.QueryRow(ctx, `
		insert into orgs(id,name,slug,plan,status,timezone,data_origin,created_at,updated_at)
		values($1,$2,$3,$4,$5,$6,$7,now(),now())
		returning id
	`, id, normalized.Name, normalized.Slug, normalized.Plan, normalized.Status, normalized.Timezone, normalized.DataOrigin).Scan(&id)
	if err != nil {
		return AdminOrgItem{}, err
	}
	_ = r.WriteAudit(ctx, AuditEvent{
		ActorType: "user", ActorID: actorID, Action: "admin.org.created", ObjectType: "org", ObjectID: id, Result: "success",
		Metadata: map[string]any{"name": normalized.Name, "slug": normalized.Slug, "status": normalized.Status},
	})
	return r.adminOrg(ctx, id)
}

func (r *Repository) UpdateAdminOrg(ctx context.Context, orgID string, actorID string, input AdminOrgInput) (AdminOrgItem, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return AdminOrgItem{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var current AdminOrgItem
	if err := tx.QueryRow(ctx, `select id,name,slug,plan,status,timezone,data_origin from orgs where id=$1 for update`, orgID).Scan(&current.ID, &current.Name, &current.Slug, &current.Plan, &current.Status, &current.Timezone, &current.DataOrigin); err != nil {
		return AdminOrgItem{}, err
	}
	if current.Status == "deleted" {
		return AdminOrgItem{}, ErrInvalidAdminStatus
	}
	if orgID == DefaultOrgID && strings.TrimSpace(input.Status) != "" && strings.TrimSpace(input.Status) != "active" {
		return AdminOrgItem{}, ErrProtectedOrg
	}
	if strings.TrimSpace(input.Name) == "" {
		input.Name = current.Name
	}
	if strings.TrimSpace(input.Slug) == "" {
		input.Slug = current.Slug
	}
	if strings.TrimSpace(input.Plan) == "" {
		input.Plan = current.Plan
	}
	if strings.TrimSpace(input.Status) == "" {
		input.Status = current.Status
	}
	if strings.TrimSpace(input.Timezone) == "" {
		input.Timezone = current.Timezone
	}
	if strings.TrimSpace(input.DataOrigin) == "" {
		input.DataOrigin = current.DataOrigin
	}
	normalized, err := normalizeAdminOrgInput(input, false)
	if err != nil {
		return AdminOrgItem{}, err
	}
	tag, err := tx.Exec(ctx, `
		update orgs
		set name=$2,slug=$3,plan=$4,status=$5,timezone=$6,data_origin=$7,
			suspended_at=case when $5='active' then null else coalesce(suspended_at, now()) end,
			deleted_at=case when $5='deleted' then coalesce(deleted_at, now()) else null end,updated_at=now()
		where id=$1 and status <> 'deleted' and deleted_at is null
	`, orgID, normalized.Name, normalized.Slug, normalized.Plan, normalized.Status, normalized.Timezone, normalized.DataOrigin)
	if err != nil {
		return AdminOrgItem{}, err
	}
	if tag.RowsAffected() == 0 {
		return AdminOrgItem{}, pgx.ErrNoRows
	}
	if normalized.Status != "active" {
		if err := deactivateInactiveOrgsGatewayAccessTx(ctx, tx, []string{orgID}, actorID); err != nil {
			return AdminOrgItem{}, err
		}
	}
	if err := writeAuditTx(ctx, tx, AuditEvent{
		ActorType: "user", ActorID: actorID, Action: "admin.org.updated", ObjectType: "org", ObjectID: orgID, Result: "success",
		Metadata: map[string]any{"name": normalized.Name, "slug": normalized.Slug, "status": normalized.Status},
	}); err != nil {
		return AdminOrgItem{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return AdminOrgItem{}, err
	}
	return r.adminOrg(ctx, orgID)
}

func (r *Repository) UpdateAdminOrgStatus(ctx context.Context, orgID string, actorID string, status string) (AdminOrgItem, error) {
	status, err := validateAdminStatus(status)
	if err != nil {
		return AdminOrgItem{}, err
	}
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return AdminOrgItem{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var currentStatus string
	if err := tx.QueryRow(ctx, `select status from orgs where id=$1 for update`, orgID).Scan(&currentStatus); err != nil {
		return AdminOrgItem{}, err
	}
	if currentStatus == "deleted" {
		return AdminOrgItem{}, ErrInvalidAdminStatus
	}
	if orgID == DefaultOrgID && status != "active" {
		return AdminOrgItem{}, ErrProtectedOrg
	}
	tag, err := tx.Exec(ctx, `
		update orgs
		set status=$2,
			suspended_at=case when $2='active' then null else coalesce(suspended_at, now()) end,
			deleted_at=case when $2='deleted' then coalesce(deleted_at, now()) else null end,
			updated_at=now()
		where id=$1 and status <> 'deleted' and deleted_at is null
	`, orgID, status)
	if err != nil {
		return AdminOrgItem{}, err
	}
	if tag.RowsAffected() == 0 {
		return AdminOrgItem{}, pgx.ErrNoRows
	}
	if status != "active" {
		if err := deactivateInactiveOrgsGatewayAccessTx(ctx, tx, []string{orgID}, actorID); err != nil {
			return AdminOrgItem{}, err
		}
	}
	if err := writeAuditTx(ctx, tx, AuditEvent{
		ActorType: "user", ActorID: actorID, Action: "admin.org.status.updated", ObjectType: "org", ObjectID: orgID, Result: "success",
		Metadata: map[string]any{"status": status},
	}); err != nil {
		return AdminOrgItem{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return AdminOrgItem{}, err
	}
	return r.adminOrg(ctx, orgID)
}

func (r *Repository) UpdateAdminOrgPlan(ctx context.Context, orgID string, actorID string, plan string) (AdminOrgItem, error) {
	plan, err := validateAdminPlan(plan)
	if err != nil {
		return AdminOrgItem{}, err
	}
	var currentStatus string
	if err := r.db.QueryRow(ctx, `select status from orgs where id=$1`, orgID).Scan(&currentStatus); err != nil {
		return AdminOrgItem{}, err
	}
	if currentStatus == "deleted" {
		return AdminOrgItem{}, ErrInvalidAdminStatus
	}
	tag, err := r.db.Exec(ctx, `
		update orgs
		set plan=$2, updated_at=now()
		where id=$1 and status <> 'deleted' and deleted_at is null
	`, orgID, plan)
	if err != nil {
		return AdminOrgItem{}, err
	}
	if tag.RowsAffected() == 0 {
		return AdminOrgItem{}, pgx.ErrNoRows
	}
	_ = r.WriteAudit(ctx, AuditEvent{
		ActorType: "user", ActorID: actorID, Action: "admin.org.plan.updated", ObjectType: "org", ObjectID: orgID, Result: "success",
		Metadata: map[string]any{"plan": plan},
	})
	return r.adminOrg(ctx, orgID)
}

func (r *Repository) DeleteAdminOrg(ctx context.Context, orgID string, actorID string) error {
	if orgID == DefaultOrgID {
		return ErrProtectedOrg
	}
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	tag, err := tx.Exec(ctx, `update orgs set status='deleted', deleted_at=now(), suspended_at=now(), updated_at=now() where id=$1 and status <> 'deleted' and deleted_at is null`, orgID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	if err := deactivateDeletedOrgsRuntimeResourcesTx(ctx, tx, []string{orgID}, actorID); err != nil {
		return err
	}
	if err := writeAuditTx(ctx, tx, AuditEvent{ActorType: "user", ActorID: actorID, Action: "admin.org.deleted", ObjectType: "org", ObjectID: orgID, Result: "success"}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (r *Repository) BulkUpdateAdminOrgs(ctx context.Context, ids []string, actorID string, status string) (AdminOrgsResult, error) {
	ids = uniqueIDs(ids)
	if len(ids) == 0 {
		return AdminOrgsResult{}, ErrEmptyBulkSelection
	}
	status, err := validateAdminStatus(status)
	if err != nil {
		return AdminOrgsResult{}, err
	}
	if err := r.validateAdminOrgBulkSelection(ctx, ids); err != nil {
		return AdminOrgsResult{}, err
	}
	if containsString(ids, DefaultOrgID) && status != "active" {
		return AdminOrgsResult{}, ErrProtectedOrg
	}
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return AdminOrgsResult{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	states, err := adminOrgBulkStates(ctx, tx, ids)
	if err != nil {
		return AdminOrgsResult{}, err
	}
	tag, err := tx.Exec(ctx, `
		update orgs
		set status=$2,
			suspended_at=case when $2='active' then null else coalesce(suspended_at, now()) end,
			deleted_at=case when $2='deleted' then coalesce(deleted_at, now()) else null end,
			updated_at=now()
		where id=any($1) and status <> 'deleted' and deleted_at is null
	`, ids, status)
	if err != nil {
		return AdminOrgsResult{}, err
	}
	if int(tag.RowsAffected()) != len(ids) {
		return AdminOrgsResult{}, pgx.ErrNoRows
	}
	if status != "active" {
		if err := deactivateInactiveOrgsGatewayAccessTx(ctx, tx, ids, actorID); err != nil {
			return AdminOrgsResult{}, err
		}
	}
	for _, id := range ids {
		state := states[id]
		if err := writeAuditTx(ctx, tx, AuditEvent{
			ActorType: "user", ActorID: actorID, Action: "admin.org.bulk_status",
			ObjectType: "org", ObjectID: id, Result: "success",
			Metadata: map[string]any{"status": status, "previous_status": state.status, "plan": state.plan},
		}); err != nil {
			return AdminOrgsResult{}, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return AdminOrgsResult{}, err
	}
	return r.AdminOrgs(ctx, AdminOrgFilter{})
}

func (r *Repository) BulkUpdateAdminOrgPlans(ctx context.Context, ids []string, actorID string, plan string) (AdminOrgsResult, error) {
	ids = uniqueIDs(ids)
	if len(ids) == 0 {
		return AdminOrgsResult{}, ErrEmptyBulkSelection
	}
	plan, err := validateAdminPlan(plan)
	if err != nil {
		return AdminOrgsResult{}, err
	}
	if err := r.validateAdminOrgBulkSelection(ctx, ids); err != nil {
		return AdminOrgsResult{}, err
	}
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return AdminOrgsResult{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	states, err := adminOrgBulkStates(ctx, tx, ids)
	if err != nil {
		return AdminOrgsResult{}, err
	}
	tag, err := tx.Exec(ctx, `
		update orgs
		set plan=$2, updated_at=now()
		where id=any($1) and status <> 'deleted' and deleted_at is null
	`, ids, plan)
	if err != nil {
		return AdminOrgsResult{}, err
	}
	if int(tag.RowsAffected()) != len(ids) {
		return AdminOrgsResult{}, pgx.ErrNoRows
	}
	if err := deactivateDeletedOrgsRuntimeResourcesTx(ctx, tx, ids, actorID); err != nil {
		return AdminOrgsResult{}, err
	}
	for _, id := range ids {
		state := states[id]
		if err := writeAuditTx(ctx, tx, AuditEvent{
			ActorType: "user", ActorID: actorID, Action: "admin.org.bulk_plan",
			ObjectType: "org", ObjectID: id, Result: "success",
			Metadata: map[string]any{"plan": plan, "previous_plan": state.plan, "status": state.status},
		}); err != nil {
			return AdminOrgsResult{}, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return AdminOrgsResult{}, err
	}
	return r.AdminOrgs(ctx, AdminOrgFilter{})
}

func (r *Repository) BulkDeleteAdminOrgs(ctx context.Context, ids []string, actorID string) (AdminOrgsResult, error) {
	ids = uniqueIDs(ids)
	if len(ids) == 0 {
		return AdminOrgsResult{}, ErrEmptyBulkSelection
	}
	if containsString(ids, DefaultOrgID) {
		return AdminOrgsResult{}, ErrProtectedOrg
	}
	if err := r.validateAdminOrgBulkSelection(ctx, ids); err != nil {
		return AdminOrgsResult{}, err
	}
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return AdminOrgsResult{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	states, err := adminOrgBulkStates(ctx, tx, ids)
	if err != nil {
		return AdminOrgsResult{}, err
	}
	tag, err := tx.Exec(ctx, `
		update orgs
		set status='deleted', deleted_at=now(), suspended_at=now(), updated_at=now()
		where id=any($1) and status <> 'deleted' and deleted_at is null
	`, ids)
	if err != nil {
		return AdminOrgsResult{}, err
	}
	if int(tag.RowsAffected()) != len(ids) {
		return AdminOrgsResult{}, pgx.ErrNoRows
	}
	for _, id := range ids {
		state := states[id]
		if err := writeAuditTx(ctx, tx, AuditEvent{
			ActorType: "user", ActorID: actorID, Action: "admin.org.bulk_deleted",
			ObjectType: "org", ObjectID: id, Result: "success",
			Metadata: map[string]any{"previous_status": state.status, "plan": state.plan},
		}); err != nil {
			return AdminOrgsResult{}, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return AdminOrgsResult{}, err
	}
	return r.AdminOrgs(ctx, AdminOrgFilter{})
}

func deactivateInactiveOrgsGatewayAccessTx(ctx context.Context, tx pgx.Tx, orgIDs []string, actorID string) error {
	orgIDs = uniqueIDs(orgIDs)
	if len(orgIDs) == 0 {
		return nil
	}
	if _, err := tx.Exec(ctx, `
		update gateways
		set status='paused', updated_at=now()
		where org_id=any($1) and status='active'
	`, orgIDs); err != nil {
		return err
	}
	return revokeActiveGatewayKeysForOrgsTx(ctx, tx, orgIDs, actorID, "admin.org.gateway_key_revoked", "org_inactive")
}

func deactivateDeletedOrgsRuntimeResourcesTx(ctx context.Context, tx pgx.Tx, orgIDs []string, actorID string) error {
	orgIDs = uniqueIDs(orgIDs)
	if len(orgIDs) == 0 {
		return nil
	}
	if _, err := tx.Exec(ctx, `
		update gateway_upstreams gu
		set enabled=false
		from gateways g
		where gu.gateway_id=g.id and g.org_id=any($1) and gu.enabled is true
	`, orgIDs); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		update gateways
		set status='deleted', updated_at=now()
		where org_id=any($1) and status <> 'deleted'
	`, orgIDs); err != nil {
		return err
	}
	if err := revokeActiveGatewayKeysForOrgsTx(ctx, tx, orgIDs, actorID, "admin.org.gateway_key_revoked", "org_deleted"); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		update org_members
		set status='removed'
		where org_id=any($1) and status <> 'removed'
	`, orgIDs); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		update alert_rules
		set enabled=false, updated_at=now()
		where org_id=any($1) and enabled is true
	`, orgIDs); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		update notification_channels
		set enabled=false, updated_at=now()
		where org_id=any($1) and enabled is true
	`, orgIDs); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		update orgs
		set default_notification_channel_id=null
		where id=any($1)
	`, orgIDs); err != nil {
		return err
	}
	return nil
}

func revokeActiveGatewayKeysForOrgsTx(ctx context.Context, tx pgx.Tx, orgIDs []string, actorID string, action string, reason string) error {
	rows, err := tx.Query(ctx, `
		update gateway_keys
		set status='revoked', revoked_at=coalesce(revoked_at, now())
		where org_id=any($1) and status='active' and deleted_at is null
		returning id,org_id,gateway_id
	`, orgIDs)
	if err != nil {
		return err
	}
	type revokedKey struct {
		id        string
		orgID     string
		gatewayID string
	}
	keys := []revokedKey{}
	for rows.Next() {
		var key revokedKey
		if err := rows.Scan(&key.id, &key.orgID, &key.gatewayID); err != nil {
			rows.Close()
			return err
		}
		keys = append(keys, key)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	for _, key := range keys {
		if err := writeAuditTx(ctx, tx, AuditEvent{
			ActorType:  "user",
			ActorID:    actorID,
			Action:     action,
			ObjectType: "gateway_key",
			ObjectID:   key.id,
			Result:     "success",
			Metadata:   map[string]any{"org_id": key.orgID, "gateway_id": key.gatewayID, "reason": reason},
		}); err != nil {
			return err
		}
	}
	return nil
}

func (r *Repository) validateAdminOrgBulkSelection(ctx context.Context, ids []string) error {
	_, err := adminOrgBulkStates(ctx, r.db, ids)
	return err
}

type adminOrgBulkState struct {
	status string
	plan   string
}

type adminOrgBulkQuerier interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
}

func adminOrgBulkStates(ctx context.Context, q adminOrgBulkQuerier, ids []string) (map[string]adminOrgBulkState, error) {
	rows, err := q.Query(ctx, `
		select id,status,plan
		from orgs
		where id=any($1) and status <> 'deleted' and deleted_at is null
	`, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	seen := map[string]adminOrgBulkState{}
	for rows.Next() {
		var id, status, plan string
		if err := rows.Scan(&id, &status, &plan); err != nil {
			return nil, err
		}
		seen[id] = adminOrgBulkState{status: status, plan: plan}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(seen) != len(ids) {
		return nil, pgx.ErrNoRows
	}
	return seen, nil
}

func (r *Repository) UpdateOpenAPISite(ctx context.Context, siteID string, input OpenAPISiteUpdateInput) (OpenAPISite, error) {
	var currentName, currentStatus string
	var currentScopes []string
	var currentQPS int
	if err := r.db.QueryRow(ctx, `select name,scopes,qps_limit,status from open_api_sites where id=$1 and deleted_at is null`, siteID).Scan(&currentName, &currentScopes, &currentQPS, &currentStatus); err != nil {
		return OpenAPISite{}, err
	}
	if currentStatus == "revoked" {
		return OpenAPISite{}, ErrInvalidOpenAPIStatus
	}
	name := strings.TrimSpace(input.Name)
	if name == "" {
		name = currentName
	}
	scopes := currentScopes
	if input.Scopes != nil {
		normalized, err := normalizeOpenAPIScopes(input.Scopes, false)
		if err != nil {
			return OpenAPISite{}, err
		}
		scopes = normalized
	}
	qps := input.QPSLimit
	if qps <= 0 {
		qps = currentQPS
	}
	status := currentStatus
	if strings.TrimSpace(input.Status) != "" {
		normalized, err := validateOpenAPIEditableStatus(input.Status)
		if err != nil {
			return OpenAPISite{}, err
		}
		status = normalized
	}
	var site OpenAPISite
	err := r.db.QueryRow(ctx, `
		update open_api_sites
		set name=$2, scopes=$3, qps_limit=$4, status=$5, updated_at=now()
		where id=$1 and deleted_at is null
		returning id,name,site_key_prefix,site_key_mask,scopes,qps_limit,status,created_at,last_used_at
	`, siteID, name, scopes, qps, status).Scan(&site.ID, &site.Name, &site.SiteKeyPrefix, &site.SiteKeyMask, &site.Scopes, &site.QPSLimit, &site.Status, &site.CreatedAt, nullableTimePtr(&site.LastUsedAt))
	if err != nil {
		return OpenAPISite{}, err
	}
	_ = r.WriteAudit(ctx, AuditEvent{
		ActorType: "user", ActorID: input.ActorID, Action: "open_api.site.updated", ObjectType: "open_api_site", ObjectID: siteID, Result: "success",
		Metadata: map[string]any{"name": name, "scopes": scopes, "qps_limit": qps, "status": status, "site_key_prefix": site.SiteKeyPrefix},
	})
	return site, nil
}

func (r *Repository) RevokeOpenAPISite(ctx context.Context, siteID string, actorID string) (OpenAPISite, error) {
	var site OpenAPISite
	err := r.db.QueryRow(ctx, `
		update open_api_sites
		set status='revoked', updated_at=now()
		where id=$1 and deleted_at is null
		returning id,name,site_key_prefix,site_key_mask,scopes,qps_limit,status,created_at,last_used_at
	`, siteID).Scan(&site.ID, &site.Name, &site.SiteKeyPrefix, &site.SiteKeyMask, &site.Scopes, &site.QPSLimit, &site.Status, &site.CreatedAt, nullableTimePtr(&site.LastUsedAt))
	if err != nil {
		return OpenAPISite{}, err
	}
	_ = r.WriteAudit(ctx, AuditEvent{
		ActorType: "user", ActorID: actorID, Action: "open_api.site.revoked", ObjectType: "open_api_site", ObjectID: siteID, Result: "success",
		Metadata: map[string]any{"site_key_prefix": site.SiteKeyPrefix},
	})
	return site, nil
}

func (r *Repository) DeleteOpenAPISite(ctx context.Context, siteID string, actorID string) (OpenAPISite, error) {
	var site OpenAPISite
	err := r.db.QueryRow(ctx, `
		update open_api_sites
		set status='revoked', deleted_at=now(), updated_at=now()
		where id=$1 and deleted_at is null
		returning id,name,site_key_prefix,site_key_mask,scopes,qps_limit,status,created_at,last_used_at
	`, siteID).Scan(&site.ID, &site.Name, &site.SiteKeyPrefix, &site.SiteKeyMask, &site.Scopes, &site.QPSLimit, &site.Status, &site.CreatedAt, nullableTimePtr(&site.LastUsedAt))
	if err != nil {
		return OpenAPISite{}, err
	}
	_ = r.WriteAudit(ctx, AuditEvent{
		ActorType: "user", ActorID: actorID, Action: "open_api.site.deleted", ObjectType: "open_api_site", ObjectID: siteID, Result: "success",
		Metadata: map[string]any{"site_key_prefix": site.SiteKeyPrefix},
	})
	return site, nil
}

func (r *Repository) BulkUpdateOpenAPISites(ctx context.Context, ids []string, actorID string, status string) ([]OpenAPISite, error) {
	ids = uniqueIDs(ids)
	if len(ids) == 0 {
		return nil, ErrEmptyBulkSelection
	}
	status, err := validateOpenAPIEditableStatus(status)
	if err != nil {
		return nil, err
	}
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	states, err := openAPISiteBulkStates(ctx, tx, ids, true)
	if err != nil {
		return nil, err
	}
	tag, err := tx.Exec(ctx, `
		update open_api_sites
		set status=$2, updated_at=now()
		where id=any($1) and deleted_at is null
	`, ids, status)
	if err != nil {
		return nil, err
	}
	if int(tag.RowsAffected()) != len(ids) {
		return nil, pgx.ErrNoRows
	}
	for _, id := range ids {
		state := states[id]
		if err := writeAuditTx(ctx, tx, AuditEvent{
			ActorType: "user", ActorID: actorID, Action: "open_api.site.bulk_status",
			ObjectType: "open_api_site", ObjectID: id, Result: "success",
			Metadata: map[string]any{"status": status, "site_key_prefix": state.siteKeyPrefix},
		}); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return r.ListOpenAPISites(ctx)
}

func (r *Repository) BulkRevokeOpenAPISites(ctx context.Context, ids []string, actorID string) ([]OpenAPISite, error) {
	ids = uniqueIDs(ids)
	if len(ids) == 0 {
		return nil, ErrEmptyBulkSelection
	}
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	states, err := openAPISiteBulkStates(ctx, tx, ids, false)
	if err != nil {
		return nil, err
	}
	tag, err := tx.Exec(ctx, `
		update open_api_sites
		set status='revoked', updated_at=now()
		where id=any($1) and deleted_at is null
	`, ids)
	if err != nil {
		return nil, err
	}
	if int(tag.RowsAffected()) != len(ids) {
		return nil, pgx.ErrNoRows
	}
	for _, id := range ids {
		state := states[id]
		if err := writeAuditTx(ctx, tx, AuditEvent{
			ActorType: "user", ActorID: actorID, Action: "open_api.site.bulk_revoked",
			ObjectType: "open_api_site", ObjectID: id, Result: "success",
			Metadata: map[string]any{"site_key_prefix": state.siteKeyPrefix},
		}); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return r.ListOpenAPISites(ctx)
}

func (r *Repository) BulkDeleteOpenAPISites(ctx context.Context, ids []string, actorID string) ([]OpenAPISite, error) {
	ids = uniqueIDs(ids)
	if len(ids) == 0 {
		return nil, ErrEmptyBulkSelection
	}
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	states, err := openAPISiteBulkStates(ctx, tx, ids, false)
	if err != nil {
		return nil, err
	}
	tag, err := tx.Exec(ctx, `
		update open_api_sites
		set status='revoked', deleted_at=now(), updated_at=now()
		where id=any($1) and deleted_at is null
	`, ids)
	if err != nil {
		return nil, err
	}
	if int(tag.RowsAffected()) != len(ids) {
		return nil, pgx.ErrNoRows
	}
	for _, id := range ids {
		state := states[id]
		if err := writeAuditTx(ctx, tx, AuditEvent{
			ActorType: "user", ActorID: actorID, Action: "open_api.site.bulk_deleted",
			ObjectType: "open_api_site", ObjectID: id, Result: "success",
			Metadata: map[string]any{"site_key_prefix": state.siteKeyPrefix},
		}); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return r.ListOpenAPISites(ctx)
}

func (r *Repository) validateOpenAPISiteBulkSelection(ctx context.Context, ids []string, requireEditable bool) error {
	_, err := openAPISiteBulkStates(ctx, r.db, ids, requireEditable)
	return err
}

type openAPISiteBulkState struct {
	status        string
	siteKeyPrefix string
}

type openAPISiteBulkQuerier interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
}

func openAPISiteBulkStates(ctx context.Context, q openAPISiteBulkQuerier, ids []string, requireEditable bool) (map[string]openAPISiteBulkState, error) {
	rows, err := q.Query(ctx, `
		select id,status,site_key_prefix
		from open_api_sites
		where id=any($1) and deleted_at is null
	`, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	seen := map[string]openAPISiteBulkState{}
	for rows.Next() {
		var id string
		var state openAPISiteBulkState
		if err := rows.Scan(&id, &state.status, &state.siteKeyPrefix); err != nil {
			return nil, err
		}
		seen[id] = state
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(seen) != len(ids) {
		return nil, pgx.ErrNoRows
	}
	if requireEditable {
		for _, state := range seen {
			if state.status == "revoked" {
				return nil, ErrInvalidOpenAPIStatus
			}
		}
	}
	return seen, nil
}

func (r *Repository) NotificationChannelByID(ctx context.Context, scope string, orgID string, channelID string) (NotificationChannel, error) {
	scope = normalizeAlertScope(scope)
	orgID = normalizeAlertOrg(scope, orgID)
	var channel NotificationChannel
	err := r.db.QueryRow(ctx, `
		select id,org_id,scope,name,type,target,
			coalesce(target_ciphertext,''),coalesce(target_nonce,''),coalesce(target_fingerprint,''),coalesce(target_mask,''),
			enabled,created_at,updated_at
		from notification_channels
		where id=$1 and scope=$2 and org_id=$3
	`, channelID, scope, orgID).Scan(&channel.ID, &channel.OrgID, &channel.Scope, &channel.Name, &channel.Type, &channel.Target, &channel.TargetCiphertext, &channel.TargetNonce, &channel.TargetFingerprint, &channel.TargetMask, &channel.Enabled, &channel.CreatedAt, &channel.UpdatedAt)
	channel.TargetMask = notificationChannelMask(channel)
	return channel, err
}

func (r *Repository) UpdateAlertDeliverySendResult(ctx context.Context, deliveryID string, status string, errorText string, deliveredBy string, metadata map[string]any) (AlertDelivery, error) {
	status = normalizeDeliveryStatus(status)
	raw, err := json.Marshal(metadata)
	if err != nil {
		return AlertDelivery{}, err
	}
	tag, err := r.db.Exec(ctx, `
		update alert_deliveries
		set status=$2,
			error=$3,
			delivered_by=$4,
			metadata=metadata || $5::jsonb,
			sent_at=case when $2 in ('sent','test','recovered') then coalesce(sent_at, now()) else null end
		where id=$1
	`, deliveryID, status, errorText, deliveredBy, raw)
	if err != nil {
		return AlertDelivery{}, err
	}
	if tag.RowsAffected() == 0 {
		return AlertDelivery{}, pgx.ErrNoRows
	}
	return r.alertDeliveryByID(ctx, deliveryID)
}

func (r *Repository) MarkNotificationChannelTested(ctx context.Context, channelID string, errorText string) error {
	_, err := r.db.Exec(ctx, `
		update notification_channels
		set last_tested_at=now(), last_error=$2, updated_at=now()
		where id=$1
	`, channelID, errorText)
	return err
}

func (r *Repository) ProductionHealth(ctx context.Context) (ProductionHealth, error) {
	stats, err := r.productionStats(ctx)
	if err != nil {
		return ProductionHealth{}, err
	}
	checks := []ProductionHealthCheck{}
	addCountCheck := func(id, label string, count int, action string) {
		status := "pass"
		severity := "info"
		message := "未发现残留"
		if count > 0 {
			status = "fail"
			severity = "critical"
			message = fmt.Sprintf("发现 %d 条需要清理的数据", count)
		}
		checks = append(checks, ProductionHealthCheck{ID: id, Label: label, Status: status, Severity: severity, Value: fmt.Sprint(count), Message: message, Action: action})
	}
	addCountCheck("demo_channels", "示例/测试通道", stats["demoChannels"], "运行 purge-demo-data 后重新检查")
	addCountCheck("test_users", "测试用户", stats["testUsers"], "清理 phase/load/test/e2e 用户")
	addCountCheck("test_orgs", "测试组织", stats["testOrgs"], "清理 phase/load/test/e2e 组织")
	addCountCheck("test_gateways", "测试网关和 Key", stats["testGateways"]+stats["testGatewayKeys"], "清理测试网关与 Key")
	addCountCheck("test_open_api", "测试 Open API 站点", stats["testOpenAPISites"], "吊销或清理测试 Site Key")
	addCountCheck("test_notifications", "示例/测试通知渠道", stats["testNotifications"], "清理 example/mock/phase 通知渠道")
	addCountCheck("test_alerts", "测试告警规则和记录", stats["testAlertRules"]+stats["testAlertDeliveries"], "清理测试告警规则和发送记录")
	addCountCheck("demo_recommend", "示例推荐配置", stats["demoRecommend"], "用真实运营配置替换推荐位")
	addCountCheck("seed_snapshots", "Seed 快照", stats["seedSnapshots"], "清理 phase2 seed snapshot")

	notificationStatus := "pass"
	notificationSeverity := "info"
	notificationMessage := "已有可用通知渠道"
	if stats["enabledNotifications"] == 0 {
		notificationStatus = "warn"
		notificationSeverity = "warning"
		notificationMessage = "没有启用的通知渠道"
	}
	checks = append(checks, ProductionHealthCheck{ID: "notifications", Label: "真实通知渠道", Status: notificationStatus, Severity: notificationSeverity, Value: fmt.Sprint(stats["enabledNotifications"]), Message: notificationMessage, Action: "在告警页面添加 email/webhook/feishu 渠道并测试发送"})

	summary := map[string]int{"pass": 0, "warn": 0, "fail": 0}
	for _, check := range checks {
		summary[check.Status]++
	}
	return ProductionHealth{GeneratedAt: time.Now(), Stats: stats, Summary: summary, Checks: checks}, nil
}

func (r *Repository) AdminSettingsSummary(ctx context.Context) (AdminSettingsSummary, error) {
	var s AdminSettingsSummary
	err := r.db.QueryRow(ctx, `
		select
				(select count(*) from channels where owner_type='platform' and status <> 'deleted' and deleted_at is null),
				(select count(*) from users where status <> 'deleted' and deleted_at is null),
				(select count(*) from orgs where status <> 'deleted' and deleted_at is null),
			(select count(*) from recommend_picks where enabled=true),
			(select count(*) from channels c join users u on u.id=c.owner_id where c.owner_type='user' and c.status <> 'deleted' and c.deleted_at is null and u.status <> 'deleted' and u.deleted_at is null),
			(select count(*) from gateways where status <> 'deleted'),
			(select count(*) from gateways where status='active'),
				(select count(*) from gateway_keys where status='active' and revoked_at is null and deleted_at is null),
				(select coalesce(sum(requests),0) from usage_daily_rollups where day >= date_trunc('month', current_date)::date),
				(select coalesce(sum(cost_usd),0) from usage_daily_rollups where day >= date_trunc('month', current_date)::date),
				(select count(*) from alert_rules r where r.enabled=true and (r.scope='admin' or exists(select 1 from orgs o where o.id=r.org_id and o.status <> 'deleted' and o.deleted_at is null))),
				(select count(*) from alert_rules r where r.enabled=true and r.scope='admin' and r.org_id=''),
				(select count(*) from notification_channels n where n.enabled=true and (n.scope='admin' or exists(select 1 from orgs o where o.id=n.org_id and o.status <> 'deleted' and o.deleted_at is null))),
				(select count(*) from open_api_sites where status='active' and deleted_at is null),
				(select count(*) from auth_sessions where revoked_at is null and expires_at > now()),
			(select count(*) from audit_events where created_at::date=current_date and action <> 'probe.status.changed')
	`).Scan(
		&s.PlatformChannels,
		&s.Users,
		&s.Orgs,
		&s.RecommendPicks,
		&s.PrivateChannels,
		&s.Gateways,
		&s.ActiveGateways,
		&s.ActiveGatewayKeys,
		&s.UsageRequestsMonth,
		&s.UsageCostMonth,
		&s.EnabledAlertRules,
		&s.EnabledAdminAlertRules,
		&s.EnabledNotificationChannels,
		&s.OpenAPISites,
		&s.ActiveSessions,
		&s.AuditToday,
	)
	if err != nil {
		return s, err
	}
	platformOrg, err := r.adminOrg(ctx, DefaultOrgID)
	if err != nil {
		return s, err
	}
	s.PlatformOrg = platformOrg
	return s, nil
}

func (r *Repository) adminUser(ctx context.Context, userID string) (AdminUserItem, error) {
	result, err := r.AdminUsers(ctx, AdminUserFilter{})
	if err != nil {
		return AdminUserItem{}, err
	}
	for _, item := range result.Items {
		if item.ID == userID {
			return item, nil
		}
	}
	return AdminUserItem{}, pgx.ErrNoRows
}

func (r *Repository) adminOrg(ctx context.Context, orgID string) (AdminOrgItem, error) {
	result, err := r.AdminOrgs(ctx, AdminOrgFilter{})
	if err != nil {
		return AdminOrgItem{}, err
	}
	for _, item := range result.Items {
		if item.ID == orgID {
			return item, nil
		}
	}
	return AdminOrgItem{}, pgx.ErrNoRows
}

func (r *Repository) alertDeliveryByID(ctx context.Context, deliveryID string) (AlertDelivery, error) {
	row := r.db.QueryRow(ctx, `
		select d.id,d.org_id,d.scope,coalesce(d.rule_id,''),coalesce(r.name,''),coalesce(d.notification_channel_id,''),coalesce(n.name,''),coalesce(d.incident_id,''),
			d.dedupe_key,d.severity,d.status,d.title,d.message,d.error,d.metadata,d.created_at,d.sent_at
		from alert_deliveries d
		left join alert_rules r on r.id=d.rule_id
		left join notification_channels n on n.id=d.notification_channel_id
		where d.id=$1
	`, deliveryID)
	return scanAlertDelivery(row)
}

func (r *Repository) productionStats(ctx context.Context) (map[string]int, error) {
	rows, err := r.db.Query(ctx, `
		with
		test_users as (
			select id from users
			where role <> 'owner' and (data_origin in ('demo','test') or email ~* '(^|[+._-])(phase[0-9]*|load|closed|test|e2e)([+._@-]|$)' or name ~* '(^|[[:space:]])(phase|load|test|e2e)([[:space:]]|$)')
		),
		test_orgs as (
			select id from orgs
			where id <> 'org_default' and (data_origin in ('demo','test') or name ~* '(phase|load|test|mock|e2e)' or slug ~* '(phase|load|test|mock|e2e)')
		),
		demo_channels as (
			select id from channels
			where data_origin in ('demo','test')
			   or endpoint ~* '^https?://[^/]*\.example([/:]|$)'
			   or endpoint ~* 'example/'
			   or name ~* '(phase|load|test|mock|e2e)'
			   or owner_id in (select id from test_users)
		),
		test_gateways as (
			select id from gateways
			where data_origin in ('demo','test')
			   or name ~* '(phase|load|test|mock|e2e)'
			   or org_id in (select id from test_orgs)
			   or created_by in (select id from test_users)
		),
		test_open_api_sites as (
			select id from open_api_sites
			where data_origin in ('demo','test')
			   or name ~* '(phase|load|test|mock|e2e)'
			   or created_by in (select id from test_users)
		),
		test_notification_channels as (
			select id from notification_channels
			where data_origin in ('demo','test')
			   or target ~* '(^|[/:.@-])example([./:@-]|$)'
			   or name ~* '(phase|load|test|mock|e2e)'
		),
		test_alert_rules as (
			select id from alert_rules
			where data_origin in ('demo','test')
			   or name ~* '(phase|load|test|mock|e2e)'
			   or org_id in (select id from test_orgs)
			   or created_by in (select id from test_users)
		),
		test_recommend_rank_rules as (
			select id from recommend_rank_rules
			where data_origin in ('demo','test')
			   or label ~* '(^|[[:space:]-])(phase|load|test|mock|e2e|crud|pilot|smoke)([[:space:]-]|$)'
			   or label ~* '^(CRUD|UI) Rank Rule'
		)
		select 'demoChannels', count(*) from demo_channels
		union all select 'testUsers', count(*) from test_users
		union all select 'testOrgs', count(*) from test_orgs
		union all select 'testGateways', count(*) from test_gateways
		union all select 'testGatewayKeys', count(*) from gateway_keys where data_origin in ('demo','test') or gateway_id in (select id from test_gateways) or created_by in (select id from test_users)
		union all select 'testOpenAPISites', count(*) from test_open_api_sites
		union all select 'testNotifications', count(*) from test_notification_channels
		union all select 'testAlertRules', count(*) from test_alert_rules
		union all select 'testAlertDeliveries', count(*) from alert_deliveries where rule_id in (select id from test_alert_rules) or notification_channel_id in (select id from test_notification_channels)
		union all
		select 'enabledNotifications', count(*) from notification_channels where enabled=true and id not in (select id from test_notification_channels)
		union all
		select 'demoRecommend', (
			(select count(*) from recommend_picks where data_origin in ('demo','test') or channel_id in (select id from demo_channels)) +
			(select count(*) from recommend_rewards where data_origin in ('demo','test') or channel_id in (select id from demo_channels)) +
			(select count(*) from recommend_scenarios where data_origin in ('demo','test') or channel_id in (select id from demo_channels)) +
			(select count(*) from test_recommend_rank_rules)
		)
		union all
		select 'seedSnapshots', count(*) from channel_status_snapshots where channel_id in (select id from demo_channels) or metadata->>'source'='phase2_seed'
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	stats := map[string]int{}
	for rows.Next() {
		var name string
		var count int
		if err := rows.Scan(&name, &count); err != nil {
			return nil, err
		}
		stats[name] = count
	}
	return stats, rows.Err()
}

func validateAdminStatus(status string) (string, error) {
	status = strings.ToLower(strings.TrimSpace(status))
	switch status {
	case "active", "suspended", "disabled":
		return status, nil
	default:
		return "", ErrInvalidAdminStatus
	}
}

func validateAdminRole(role string) (string, error) {
	role = strings.ToLower(strings.TrimSpace(role))
	switch role {
	case "owner", "admin", "user":
		return role, nil
	default:
		return "", ErrInvalidAdminRole
	}
}

func validateAdminPlan(plan string) (string, error) {
	plan = strings.ToLower(strings.TrimSpace(plan))
	switch plan {
	case "starter", "team", "business", "enterprise":
		return plan, nil
	default:
		return "", ErrInvalidAdminPlan
	}
}

func normalizeAdminUserInput(input AdminUserInput, requirePassword bool) (AdminUserInput, error) {
	input.Email = strings.ToLower(strings.TrimSpace(input.Email))
	input.Username = normalizeUsernameOrEmpty(input.Username)
	input.Name = strings.TrimSpace(input.Name)
	input.DataOrigin = normalizeAdminDataOrigin(input.DataOrigin)
	if strings.TrimSpace(input.Role) == "" {
		input.Role = "user"
	}
	if strings.TrimSpace(input.Plan) == "" {
		input.Plan = "free"
	}
	if strings.TrimSpace(input.Status) == "" {
		input.Status = "active"
	}
	if input.Email == "" || !strings.Contains(input.Email, "@") || strings.ContainsAny(input.Email, " \t\r\n") {
		return AdminUserInput{}, fmt.Errorf("invalid email")
	}
	if input.Name == "" {
		input.Name = strings.Split(input.Email, "@")[0]
	}
	role, err := validateAdminRole(input.Role)
	if err != nil {
		return AdminUserInput{}, err
	}
	input.Role = role
	plan, err := validateUserPlan(input.Plan)
	if err != nil {
		return AdminUserInput{}, err
	}
	input.Plan = plan
	status, err := validateAdminStatus(input.Status)
	if err != nil {
		return AdminUserInput{}, err
	}
	input.Status = status
	if requirePassword && strings.TrimSpace(input.PasswordHash) == "" {
		return AdminUserInput{}, fmt.Errorf("password is required")
	}
	return input, nil
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

func validateUserPlan(plan string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(plan)) {
	case "", "free":
		return "free", nil
	case "super_vip", "super-vip", "supervip", "vip":
		return "super_vip", nil
	default:
		return "", ErrInvalidUserPlan
	}
}

func normalizeAdminOrgInput(input AdminOrgInput, requireName bool) (AdminOrgInput, error) {
	input.Name = strings.TrimSpace(input.Name)
	input.Slug = normalizeSlug(input.Slug)
	input.Plan = strings.TrimSpace(input.Plan)
	input.Timezone = strings.TrimSpace(input.Timezone)
	input.DataOrigin = normalizeAdminDataOrigin(input.DataOrigin)
	if requireName && input.Name == "" {
		return AdminOrgInput{}, fmt.Errorf("name is required")
	}
	if input.Plan == "" {
		input.Plan = "starter"
	}
	plan, err := validateAdminPlan(input.Plan)
	if err != nil {
		return AdminOrgInput{}, err
	}
	input.Plan = plan
	if strings.TrimSpace(input.Status) == "" {
		input.Status = "active"
	}
	if input.Timezone == "" {
		input.Timezone = "Asia/Shanghai"
	}
	status, err := validateAdminStatus(input.Status)
	if err != nil {
		return AdminOrgInput{}, err
	}
	input.Status = status
	return input, nil
}

func normalizeAdminDataOrigin(origin string) string {
	switch strings.TrimSpace(origin) {
	case "system", "runtime", "demo", "test":
		return strings.TrimSpace(origin)
	default:
		return "runtime"
	}
}

func avatarForName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return "U"
	}
	return strings.ToUpper(string([]rune(name)[0]))
}

func normalizeSlug(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	out := []rune{}
	lastDash := false
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			out = append(out, r)
			lastDash = false
			continue
		}
		if unicode.IsSpace(r) || r == '-' || r == '_' {
			if !lastDash && len(out) > 0 {
				out = append(out, '-')
				lastDash = true
			}
		}
	}
	return strings.Trim(string(out), "-")
}

func uniqueOrgSlug(name string, id string) string {
	base := normalizeSlug(name)
	if base == "" {
		base = "org"
	}
	suffix := strings.TrimPrefix(id, "org_")
	if len(suffix) > 8 {
		suffix = suffix[:8]
	}
	return base + "-" + suffix
}

func uniqueWorkspaceSlug(userID string) string {
	suffix := strings.TrimPrefix(userID, "usr_")
	if len(suffix) > 8 {
		suffix = suffix[:8]
	}
	return "workspace-" + suffix
}

func uniqueIDs(ids []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func (r *Repository) ensureAnotherActiveOwner(ctx context.Context, userID string) error {
	var owners int
	if err := r.db.QueryRow(ctx, `select count(*) from users where role='owner' and status='active' and id<>$1`, userID).Scan(&owners); err != nil {
		return err
	}
	if owners == 0 {
		return ErrProtectedOwner
	}
	return nil
}

func (r *Repository) ensureActiveOwnerOutside(ctx context.Context, ids []string) error {
	return ensureActiveOwnerOutside(ctx, r.db, ids)
}

func ensureActiveOwnerOutside(ctx context.Context, q DBTX, ids []string) error {
	var owners int
	if err := q.QueryRow(ctx, `select count(*) from users where role='owner' and status='active' and id<>all($1)`, ids).Scan(&owners); err != nil {
		return err
	}
	if owners == 0 {
		return ErrProtectedOwner
	}
	return nil
}

func validateOpenAPIStatus(status string) (string, error) {
	status = strings.ToLower(strings.TrimSpace(status))
	switch status {
	case "active", "paused", "revoked":
		return status, nil
	default:
		return "", ErrInvalidOpenAPIStatus
	}
}

func validateOpenAPIEditableStatus(status string) (string, error) {
	status = strings.ToLower(strings.TrimSpace(status))
	switch status {
	case "active", "paused":
		return status, nil
	default:
		return "", ErrInvalidOpenAPIStatus
	}
}

func normalizeDeliveryStatus(status string) string {
	switch strings.TrimSpace(status) {
	case "sent", "failed", "suppressed", "recovered", "test":
		return strings.TrimSpace(status)
	default:
		return "failed"
	}
}
