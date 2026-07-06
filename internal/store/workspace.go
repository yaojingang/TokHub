package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

var (
	ErrWorkspaceOwnerProtected  = errors.New("workspace owner cannot be changed through member management")
	ErrWorkspaceSelfRoleChange  = errors.New("cannot change your own workspace role")
	ErrWorkspaceEmailUnverified = errors.New("workspace member email must be verified")
)

type WorkspaceSettings struct {
	OrgID                        string                `json:"orgId"`
	Name                         string                `json:"name"`
	Plan                         string                `json:"plan"`
	Status                       string                `json:"status"`
	Role                         string                `json:"role"`
	Timezone                     string                `json:"timezone"`
	DefaultGatewayPolicy         string                `json:"defaultGatewayPolicy"`
	DefaultGatewayPolicyLabel    string                `json:"defaultGatewayPolicyLabel"`
	DefaultNotificationChannelID string                `json:"defaultNotificationChannelId"`
	DefaultNotificationName      string                `json:"defaultNotificationName"`
	Members                      int                   `json:"members"`
	PrivateChannels              int                   `json:"privateChannels"`
	Gateways                     int                   `json:"gateways"`
	ActiveKeys                   int                   `json:"activeKeys"`
	NotificationChannels         []NotificationChannel `json:"notificationChannels"`
	UpdatedAt                    time.Time             `json:"updatedAt"`
}

type WorkspaceOption struct {
	OrgID           string `json:"orgId"`
	Name            string `json:"name"`
	Plan            string `json:"plan"`
	Status          string `json:"status"`
	Role            string `json:"role"`
	Members         int    `json:"members"`
	PrivateChannels int    `json:"privateChannels"`
	Gateways        int    `json:"gateways"`
	ActiveKeys      int    `json:"activeKeys"`
}

type WorkspaceSettingsInput struct {
	Name                         string
	Timezone                     string
	DefaultGatewayPolicy         string
	DefaultNotificationChannelID string
}

type WorkspaceMemberInput struct {
	Email     string
	Role      string
	GroupName string
}

func defaultPersonalWorkspaceName(user PublicUser) string {
	name := strings.TrimSpace(user.Name)
	if name == "" {
		name = strings.Split(user.Email, "@")[0]
	}
	if name == "" {
		name = "个人工作区"
	}
	return name + " 的工作区"
}

func (r *Repository) WorkspaceSettings(ctx context.Context, user PublicUser) (WorkspaceSettings, error) {
	orgID, err := r.EnsurePersonalWorkspace(ctx, user)
	if err != nil {
		return WorkspaceSettings{}, err
	}
	settings, err := r.WorkspaceSettingsForOrg(ctx, orgID)
	if err != nil {
		return WorkspaceSettings{}, err
	}
	settings.Role = "owner"
	return settings, nil
}

func (r *Repository) EnsurePersonalWorkspaceForUserID(ctx context.Context, userID string) (string, error) {
	var user User
	err := r.db.QueryRow(ctx, `
		select id,email,coalesce(username,''),password_hash,name,avatar,status,role,plan,email_verified_at is not null
		from users
		where id=$1 and status='active' and deleted_at is null
	`, userID).Scan(&user.ID, &user.Email, &user.Username, &user.PasswordHash, &user.Name, &user.Avatar, &user.Status, &user.Role, &user.Plan, &user.EmailVerified)
	if err != nil {
		return "", err
	}
	return r.EnsurePersonalWorkspace(ctx, user.Public())
}

func (r *Repository) WorkspaceSettingsForOrg(ctx context.Context, orgID string) (WorkspaceSettings, error) {
	var item WorkspaceSettings
	err := r.db.QueryRow(ctx, `
		select o.id,o.name,o.plan,o.status,o.timezone,o.default_gateway_policy,
			coalesce(o.default_notification_channel_id,''),coalesce(n.name,''),o.updated_at,
			(select count(*) from org_members where org_id=o.id and status='active'),
			(select count(*) from channels where owner_type='user' and coalesce(org_id,'org_' || coalesce(owner_id,''))=o.id and status <> 'deleted' and deleted_at is null),
			(select count(*) from gateways where org_id=o.id and status <> 'deleted'),
			(select count(*) from gateway_keys where org_id=o.id and status='active' and deleted_at is null)
		from orgs o
		left join notification_channels n on n.id=o.default_notification_channel_id and n.org_id=o.id and n.scope='console'
		where o.id=$1
	`, orgID).Scan(&item.OrgID, &item.Name, &item.Plan, &item.Status, &item.Timezone, &item.DefaultGatewayPolicy, &item.DefaultNotificationChannelID, &item.DefaultNotificationName, &item.UpdatedAt, &item.Members, &item.PrivateChannels, &item.Gateways, &item.ActiveKeys)
	if err != nil {
		return WorkspaceSettings{}, err
	}
	item.DefaultGatewayPolicyLabel = gatewayPolicyLabel(item.DefaultGatewayPolicy)
	channels, err := r.NotificationChannels(ctx, "console", orgID)
	if err != nil {
		return WorkspaceSettings{}, err
	}
	item.NotificationChannels = channels
	return item, nil
}

func (r *Repository) UserWorkspaces(ctx context.Context, user PublicUser) ([]WorkspaceOption, error) {
	personalOrgID, err := r.EnsurePersonalWorkspace(ctx, user)
	if err != nil {
		return nil, err
	}
	rows, err := r.db.Query(ctx, `
		select o.id,o.name,o.plan,o.status,om.role,
			(select count(*) from org_members where org_id=o.id and status='active'),
			(select count(*) from channels where owner_type='user' and coalesce(org_id,'org_' || coalesce(owner_id,''))=o.id and status <> 'deleted' and deleted_at is null),
			(select count(*) from gateways where org_id=o.id and status <> 'deleted'),
			(select count(*) from gateway_keys where org_id=o.id and status='active' and deleted_at is null)
		from org_members om
		join orgs o on o.id=om.org_id
		where om.user_id=$1
			and om.status='active'
			and o.status='active'
			and o.deleted_at is null
		order by case when o.id=$2 then 0 else 1 end,
			case om.role when 'owner' then 0 when 'admin' then 1 when 'operator' then 2 else 3 end,
			o.created_at asc
	`, user.ID, personalOrgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []WorkspaceOption{}
	for rows.Next() {
		var item WorkspaceOption
		if err := rows.Scan(&item.OrgID, &item.Name, &item.Plan, &item.Status, &item.Role, &item.Members, &item.PrivateChannels, &item.Gateways, &item.ActiveKeys); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (r *Repository) UpdateWorkspaceSettings(ctx context.Context, orgID string, actorID string, input WorkspaceSettingsInput) (WorkspaceSettings, error) {
	name := strings.TrimSpace(input.Name)
	if name == "" {
		return WorkspaceSettings{}, fmt.Errorf("workspace name is required")
	}
	timezone := strings.TrimSpace(input.Timezone)
	if timezone == "" {
		timezone = "Asia/Shanghai"
	}
	policy := normalizeGatewayPolicy(input.DefaultGatewayPolicy)
	notificationID := strings.TrimSpace(input.DefaultNotificationChannelID)
	if notificationID != "" {
		var exists bool
		if err := r.db.QueryRow(ctx, `
			select exists(select 1 from notification_channels where id=$1 and scope='console' and org_id=$2 and enabled=true)
		`, notificationID, orgID).Scan(&exists); err != nil {
			return WorkspaceSettings{}, err
		}
		if !exists {
			return WorkspaceSettings{}, pgx.ErrNoRows
		}
	}
	tag, err := r.db.Exec(ctx, `
		update orgs
		set name=$2, timezone=$3, default_gateway_policy=$4, default_notification_channel_id=$5, updated_at=now()
		where id=$1 and status='active'
	`, orgID, name, timezone, policy, nullableText(notificationID))
	if err != nil {
		return WorkspaceSettings{}, err
	}
	if tag.RowsAffected() == 0 {
		return WorkspaceSettings{}, pgx.ErrNoRows
	}
	_ = r.WriteAudit(ctx, AuditEvent{
		ActorType:  "user",
		ActorID:    actorID,
		Action:     "workspace.settings.updated",
		ObjectType: "org",
		ObjectID:   orgID,
		Result:     "success",
		Metadata:   map[string]any{"name": name, "default_gateway_policy": policy, "default_notification_channel_id": notificationID},
	})
	return r.WorkspaceSettingsForOrg(ctx, orgID)
}

func (r *Repository) ResetWorkspaceSettings(ctx context.Context, orgID string, actor PublicUser) (WorkspaceSettings, error) {
	name := defaultPersonalWorkspaceName(actor)
	tag, err := r.db.Exec(ctx, `
		update orgs
		set name=$2, timezone='Asia/Shanghai', default_gateway_policy='latency', default_notification_channel_id=null, updated_at=now()
		where id=$1 and status='active'
	`, orgID, name)
	if err != nil {
		return WorkspaceSettings{}, err
	}
	if tag.RowsAffected() == 0 {
		return WorkspaceSettings{}, pgx.ErrNoRows
	}
	_ = r.WriteAudit(ctx, AuditEvent{
		ActorType:  "user",
		ActorID:    actor.ID,
		Action:     "workspace.settings.reset",
		ObjectType: "org",
		ObjectID:   orgID,
		Result:     "success",
		Metadata:   map[string]any{"name": name, "default_gateway_policy": "latency"},
	})
	return r.WorkspaceSettingsForOrg(ctx, orgID)
}

func (r *Repository) WorkspaceRole(ctx context.Context, orgID string, userID string) (string, error) {
	var role string
	err := r.db.QueryRow(ctx, `
		select role from org_members where org_id=$1 and user_id=$2 and status='active'
	`, orgID, userID).Scan(&role)
	return role, err
}

func (r *Repository) UpsertWorkspaceMember(ctx context.Context, orgID string, actorID string, input WorkspaceMemberInput) (GatewayMember, error) {
	email := strings.ToLower(strings.TrimSpace(input.Email))
	if !strings.Contains(email, "@") {
		return GatewayMember{}, fmt.Errorf("valid email is required")
	}
	role := normalizeWorkspaceWritableRole(input.Role)
	group := strings.TrimSpace(input.GroupName)
	if group == "" {
		group = "工作区成员"
	}
	var userID string
	var emailVerified bool
	err := r.db.QueryRow(ctx, `select id,email_verified_at is not null from users where email=$1 and status='active'`, email).Scan(&userID, &emailVerified)
	if err != nil {
		return GatewayMember{}, err
	}
	if !emailVerified {
		return GatewayMember{}, ErrWorkspaceEmailUnverified
	}
	if userID == actorID {
		return GatewayMember{}, ErrWorkspaceSelfRoleChange
	}
	existingRole, err := r.WorkspaceRole(ctx, orgID, userID)
	if err == nil && existingRole == "owner" {
		return GatewayMember{}, ErrWorkspaceOwnerProtected
	}
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return GatewayMember{}, err
	}
	if _, err := r.db.Exec(ctx, `
		insert into org_members(org_id,user_id,role,group_name,status)
		values($1,$2,$3,$4,'active')
		on conflict(org_id,user_id) do update set role=excluded.role, group_name=excluded.group_name, status='active'
	`, orgID, userID, role, group); err != nil {
		return GatewayMember{}, err
	}
	_ = r.WriteAudit(ctx, AuditEvent{
		ActorType:  "user",
		ActorID:    actorID,
		Action:     "workspace.member.invited",
		ObjectType: "user",
		ObjectID:   userID,
		Result:     "success",
		Metadata:   map[string]any{"org_id": orgID, "email": email, "role": role},
	})
	return r.workspaceMember(ctx, orgID, userID)
}

func (r *Repository) UpdateWorkspaceMember(ctx context.Context, orgID string, actorID string, memberID string, input WorkspaceMemberInput) (GatewayMember, error) {
	if memberID == actorID {
		return GatewayMember{}, ErrWorkspaceSelfRoleChange
	}
	existingRole, err := r.WorkspaceRole(ctx, orgID, memberID)
	if err != nil {
		return GatewayMember{}, err
	}
	if existingRole == "owner" {
		return GatewayMember{}, ErrWorkspaceOwnerProtected
	}
	role := normalizeWorkspaceWritableRole(input.Role)
	group := strings.TrimSpace(input.GroupName)
	if group == "" {
		group = "工作区成员"
	}
	tag, err := r.db.Exec(ctx, `
		update org_members set role=$3, group_name=$4, status='active'
		where org_id=$1 and user_id=$2 and status='active'
	`, orgID, memberID, role, group)
	if err != nil {
		return GatewayMember{}, err
	}
	if tag.RowsAffected() == 0 {
		return GatewayMember{}, pgx.ErrNoRows
	}
	_ = r.WriteAudit(ctx, AuditEvent{
		ActorType:  "user",
		ActorID:    actorID,
		Action:     "workspace.member.updated",
		ObjectType: "user",
		ObjectID:   memberID,
		Result:     "success",
		Metadata:   map[string]any{"org_id": orgID, "role": role},
	})
	return r.workspaceMember(ctx, orgID, memberID)
}

func (r *Repository) RemoveWorkspaceMember(ctx context.Context, orgID string, actorID string, memberID string) error {
	if memberID == actorID {
		return ErrWorkspaceSelfRoleChange
	}
	existingRole, err := r.WorkspaceRole(ctx, orgID, memberID)
	if err != nil {
		return err
	}
	if existingRole == "owner" {
		return ErrWorkspaceOwnerProtected
	}
	tag, err := r.db.Exec(ctx, `
		update org_members set status='removed'
		where org_id=$1 and user_id=$2 and status='active'
	`, orgID, memberID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	return r.WriteAudit(ctx, AuditEvent{
		ActorType:  "user",
		ActorID:    actorID,
		Action:     "workspace.member.removed",
		ObjectType: "user",
		ObjectID:   memberID,
		Result:     "success",
		Metadata:   map[string]any{"org_id": orgID},
	})
}

func (r *Repository) BulkUpdateWorkspaceMembersRole(ctx context.Context, orgID string, actorID string, memberIDs []string, role string) ([]GatewayMember, error) {
	if orgID == "" {
		orgID = DefaultOrgID
	}
	memberIDs = uniqueStrings(memberIDs)
	if len(memberIDs) == 0 {
		return nil, fmt.Errorf("select at least one workspace member")
	}
	nextRole, err := validateWorkspaceWritableRole(role)
	if err != nil {
		return nil, err
	}
	rows, err := r.db.Query(ctx, `
		select user_id,role,coalesce(group_name,'工作区成员')
		from org_members
		where org_id=$1 and user_id=any($2) and status='active'
	`, orgID, memberIDs)
	if err != nil {
		return nil, err
	}
	type memberState struct {
		id    string
		role  string
		group string
	}
	states := map[string]memberState{}
	for rows.Next() {
		var state memberState
		if err := rows.Scan(&state.id, &state.role, &state.group); err != nil {
			rows.Close()
			return nil, err
		}
		states[state.id] = state
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()
	if len(states) != len(memberIDs) {
		return nil, pgx.ErrNoRows
	}
	for _, id := range memberIDs {
		if id == actorID {
			return nil, ErrWorkspaceSelfRoleChange
		}
		if states[id].role == "owner" {
			return nil, ErrWorkspaceOwnerProtected
		}
	}
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	tag, err := tx.Exec(ctx, `
		update org_members set role=$3, status='active'
		where org_id=$1 and user_id=any($2) and status='active'
	`, orgID, memberIDs, nextRole)
	if err != nil {
		return nil, err
	}
	if int(tag.RowsAffected()) != len(memberIDs) {
		return nil, pgx.ErrNoRows
	}
	for _, id := range memberIDs {
		if err := writeAuditTx(ctx, tx, AuditEvent{
			ActorType:  "user",
			ActorID:    actorID,
			Action:     "workspace.member.bulk_role_updated",
			ObjectType: "user",
			ObjectID:   id,
			Result:     "success",
			Metadata:   map[string]any{"org_id": orgID, "role": nextRole},
		}); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return r.gatewayMembers(ctx, orgID, true)
}

func (r *Repository) BulkRemoveWorkspaceMembers(ctx context.Context, orgID string, actorID string, memberIDs []string) ([]GatewayMember, error) {
	if orgID == "" {
		orgID = DefaultOrgID
	}
	memberIDs = uniqueStrings(memberIDs)
	if len(memberIDs) == 0 {
		return nil, fmt.Errorf("select at least one workspace member")
	}
	rows, err := r.db.Query(ctx, `
		select user_id,role
		from org_members
		where org_id=$1 and user_id=any($2) and status='active'
	`, orgID, memberIDs)
	if err != nil {
		return nil, err
	}
	states := map[string]string{}
	for rows.Next() {
		var id, role string
		if err := rows.Scan(&id, &role); err != nil {
			rows.Close()
			return nil, err
		}
		states[id] = role
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()
	if len(states) != len(memberIDs) {
		return nil, pgx.ErrNoRows
	}
	for _, id := range memberIDs {
		if id == actorID {
			return nil, ErrWorkspaceSelfRoleChange
		}
		if states[id] == "owner" {
			return nil, ErrWorkspaceOwnerProtected
		}
	}
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	tag, err := tx.Exec(ctx, `
		update org_members set status='removed'
		where org_id=$1 and user_id=any($2) and status='active'
	`, orgID, memberIDs)
	if err != nil {
		return nil, err
	}
	if int(tag.RowsAffected()) != len(memberIDs) {
		return nil, pgx.ErrNoRows
	}
	for _, id := range memberIDs {
		if err := writeAuditTx(ctx, tx, AuditEvent{
			ActorType:  "user",
			ActorID:    actorID,
			Action:     "workspace.member.bulk_removed",
			ObjectType: "user",
			ObjectID:   id,
			Result:     "success",
			Metadata:   map[string]any{"org_id": orgID},
		}); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return r.gatewayMembers(ctx, orgID, true)
}

func (r *Repository) workspaceMember(ctx context.Context, orgID string, userID string) (GatewayMember, error) {
	members, err := r.gatewayMembers(ctx, orgID, true)
	if err != nil {
		return GatewayMember{}, err
	}
	for _, member := range members {
		if member.UserID == userID {
			return member, nil
		}
	}
	return GatewayMember{}, pgx.ErrNoRows
}

func normalizeWorkspaceRole(role string) string {
	switch strings.TrimSpace(role) {
	case "owner", "admin", "operator", "viewer":
		return strings.TrimSpace(role)
	default:
		return "viewer"
	}
}

func normalizeWorkspaceWritableRole(role string) string {
	switch strings.TrimSpace(role) {
	case "admin", "operator", "viewer":
		return strings.TrimSpace(role)
	default:
		return "viewer"
	}
}

func validateWorkspaceWritableRole(role string) (string, error) {
	switch strings.TrimSpace(role) {
	case "admin", "operator", "viewer":
		return strings.TrimSpace(role), nil
	default:
		return "", fmt.Errorf("invalid workspace role")
	}
}

func CanManageWorkspace(role string) bool {
	role = strings.TrimSpace(role)
	return role == "owner" || role == "admin"
}

func CanOperateWorkspace(role string) bool {
	role = strings.TrimSpace(role)
	return role == "owner" || role == "admin" || role == "operator"
}
