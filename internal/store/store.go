package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"
)

type Repository struct {
	db *pgxpool.Pool
}

type User struct {
	ID            string
	Email         string
	Username      string
	PasswordHash  string
	Name          string
	Avatar        string
	Status        string
	Role          string
	Plan          string
	EmailVerified bool
}

type PublicUser struct {
	ID            string `json:"id"`
	Email         string `json:"email"`
	Username      string `json:"username,omitempty"`
	Name          string `json:"name"`
	Avatar        string `json:"avatar"`
	Role          string `json:"role"`
	Status        string `json:"status"`
	Plan          string `json:"plan"`
	EmailVerified bool   `json:"emailVerified"`
}

type AuditEvent struct {
	ActorType  string
	ActorID    string
	Action     string
	ObjectType string
	ObjectID   string
	IP         string
	Result     string
	Metadata   map[string]any
}

type AgentAuditContext struct {
	TokenID            string
	TokenName          string
	DelegatedUserID    string
	DelegatedUserEmail string
	Reason             string
	IdempotencyKey     string
}

type agentAuditContextKey struct{}

func WithAgentAudit(ctx context.Context, info AgentAuditContext) context.Context {
	return context.WithValue(ctx, agentAuditContextKey{}, info)
}

func AgentAuditFromContext(ctx context.Context) (AgentAuditContext, bool) {
	info, ok := ctx.Value(agentAuditContextKey{}).(AgentAuditContext)
	return info, ok && info.TokenID != ""
}

type SiteConfig struct {
	RegistrationOpen          bool                 `json:"registrationOpen"`
	ShowRegisterCTA           bool                 `json:"showRegisterCta"`
	EmailVerificationRequired bool                 `json:"emailVerificationRequired"`
	BrandName                 string               `json:"brandName"`
	LogoMark                  string               `json:"logoMark"`
	Subtitle                  string               `json:"subtitle"`
	PublicURL                 string               `json:"publicUrl"`
	FooterText                string               `json:"footerText"`
	DefaultGatewayPolicy      string               `json:"defaultGatewayPolicy"`
	Timezone                  string               `json:"timezone"`
	NavItems                  []NavItem            `json:"navItems"`
	FooterLinks               []NavItem            `json:"footerLinks"`
	MonitorModels             []MonitorModelConfig `json:"monitorModels"`
}

type NavItem struct {
	Label string `json:"label"`
	Href  string `json:"href"`
}

type MonitorModelConfig struct {
	Key             string   `json:"key"`
	Label           string   `json:"label"`
	Model           string   `json:"model"`
	UpstreamModel   string   `json:"upstreamModel"`
	Type            string   `json:"type"`
	Enabled         bool     `json:"enabled"`
	DefaultSelected bool     `json:"defaultSelected"`
	InputPerMTok    float64  `json:"inputPerMtok"`
	OutputPerMTok   float64  `json:"outputPerMtok"`
	Aliases         []string `json:"aliases"`
}

func DefaultSiteConfig(publicURL string, registrationOpen bool) SiteConfig {
	return normalizeSiteConfig(SiteConfig{
		RegistrationOpen:          registrationOpen,
		ShowRegisterCTA:           registrationOpen,
		EmailVerificationRequired: false,
		BrandName:                 "TokHub",
		LogoMark:                  "T",
		Subtitle:                  "API 中转站监控",
		PublicURL:                 publicURL,
		FooterText:                "TokHub API 中转站监控与企业网关",
		DefaultGatewayPolicy:      "latency",
		Timezone:                  "Asia/Shanghai",
		NavItems:                  defaultNavItems(),
		FooterLinks:               defaultFooterLinks(),
		MonitorModels:             DefaultMonitorModels(),
	})
}

type SeedConfig struct {
	PublicURL        string
	AdminEmail       string
	AdminUsername    string
	AdminPassword    string
	RegistrationOpen bool
	SeedMode         string
}

func (u User) Public() PublicUser {
	return PublicUser{
		ID:            u.ID,
		Email:         u.Email,
		Username:      u.Username,
		Name:          u.Name,
		Avatar:        u.Avatar,
		Role:          u.Role,
		Status:        u.Status,
		Plan:          normalizeUserPlan(u.Plan),
		EmailVerified: u.EmailVerified,
	}
}

func (u PublicUser) CanUsePlatformGatewayUpstreams() bool {
	return normalizeUserPlan(u.Plan) == "super_vip"
}

func normalizeUserPlan(plan string) string {
	switch strings.ToLower(strings.TrimSpace(plan)) {
	case "super_vip", "super-vip", "supervip", "vip":
		return "super_vip"
	default:
		return "free"
	}
}

func normalizeUsernameOrEmpty(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var out strings.Builder
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' || r == '-' || r == '.' {
			out.WriteRune(r)
		}
	}
	return out.String()
}

func Open(ctx context.Context, databaseURL string) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, err
	}
	cfg.MaxConns = 10
	cfg.MinConns = 1
	cfg.MaxConnLifetime = 30 * time.Minute
	db, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, err
	}
	if err := db.Ping(ctx); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

func NewRepository(db *pgxpool.Pool) *Repository {
	return &Repository{db: db}
}

func (r *Repository) Ping(ctx context.Context) error {
	return r.db.Ping(ctx)
}

func RunMigrations(ctx context.Context, db *pgxpool.Pool, migrationsDir string, logger *slog.Logger) error {
	if _, err := db.Exec(ctx, `create table if not exists schema_migrations (version text primary key, applied_at timestamptz not null default now())`); err != nil {
		return err
	}
	files, err := filepath.Glob(filepath.Join(migrationsDir, "*.sql"))
	if err != nil {
		return err
	}
	sort.Strings(files)
	for _, file := range files {
		version := filepath.Base(file)
		var exists bool
		if err := db.QueryRow(ctx, `select exists(select 1 from schema_migrations where version=$1)`, version).Scan(&exists); err != nil {
			return err
		}
		if exists {
			continue
		}
		sqlBytes, err := os.ReadFile(file)
		if err != nil {
			return err
		}
		tx, err := db.Begin(ctx)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, string(sqlBytes)); err != nil {
			_ = tx.Rollback(ctx)
			return fmt.Errorf("%s: %w", version, err)
		}
		if _, err := tx.Exec(ctx, `insert into schema_migrations(version) values($1)`, version); err != nil {
			_ = tx.Rollback(ctx)
			return err
		}
		if err := tx.Commit(ctx); err != nil {
			return err
		}
		logger.Info("migration applied", "version", version)
	}
	return nil
}

func Seed(ctx context.Context, db *pgxpool.Pool, cfg SeedConfig, logger *slog.Logger) error {
	seedMode := normalizeSeedMode(cfg.SeedMode)
	repo := NewRepository(db)
	passwordHashBytes, err := bcrypt.GenerateFromPassword([]byte(cfg.AdminPassword), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	passwordHash := string(passwordHashBytes)
	adminID := "usr_" + uuid.NewString()
	err = db.QueryRow(ctx, `
		insert into users(id,email,username,password_hash,name,avatar,status,role,plan,email_verified_at,data_origin)
		values($1,$2,$3,$4,$5,$6,'active','owner','super_vip',now(),'system')
		on conflict(email) do update set
			username=excluded.username,
			name=excluded.name,
			avatar=excluded.avatar,
			status='active',
			role='owner',
			plan='super_vip',
			data_origin='system'
		returning id
	`, adminID, strings.ToLower(cfg.AdminEmail), normalizeUsernameOrEmpty(cfg.AdminUsername), passwordHash, "TokHub Admin", "TA").Scan(&adminID)
	if err != nil {
		return err
	}
	if err := seedDefaultOrg(ctx, db, adminID); err != nil {
		return err
	}
	if err := repo.SetSiteConfig(ctx, DefaultSiteConfig(cfg.PublicURL, cfg.RegistrationOpen)); err != nil {
		return err
	}
	if err := seedModels(ctx, db); err != nil {
		return err
	}
	if seedMode != "prod" {
		if err := seedChannels(ctx, db, seedMode); err != nil {
			return err
		}
		if err := seedSnapshots(ctx, db); err != nil {
			return err
		}
		if err := SeedPhase6(ctx, db, seedMode); err != nil {
			return err
		}
		if err := seedNotificationChannels(ctx, db, seedMode); err != nil {
			return err
		}
	}
	logger.Info("seeded admin and baseline data", "admin_email", cfg.AdminEmail, "seed_mode", seedMode)
	return nil
}

func normalizeSeedMode(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "prod", "demo", "test":
		return strings.ToLower(strings.TrimSpace(mode))
	default:
		return "prod"
	}
}

func seedDefaultOrg(ctx context.Context, db *pgxpool.Pool, adminID string) error {
	if _, err := db.Exec(ctx, `
		insert into orgs(id,name,slug,plan,status,timezone,data_origin)
		values('org_default','TokHub','tokhub','enterprise','active','Asia/Shanghai','system')
		on conflict(id) do update set name=excluded.name, slug=excluded.slug, plan=excluded.plan, status=excluded.status, data_origin='system'
	`); err != nil {
		return err
	}
	_, err := db.Exec(ctx, `
		insert into org_members(org_id,user_id,role,group_name,status)
		values('org_default',$1,'owner','平台管理','active')
		on conflict(org_id,user_id) do update set role=excluded.role, group_name=excluded.group_name, status=excluded.status
	`, adminID)
	return err
}

func seedChannels(ctx context.Context, db *pgxpool.Pool, seedMode string) error {
	dataOrigin := "demo"
	if seedMode == "test" {
		dataOrigin = "test"
	}
	channels := []struct {
		ID       string
		Name     string
		Provider string
		Model    string
		Endpoint string
		Score    int
		Status   string
	}{
		{"ch_cc_claude", "CC · Claude", "Anthropic", "claude-3-5-sonnet", "https://api.cc.example/v1", 94, "healthy"},
		{"ch_cx_mix", "CX · 混合", "OpenAI", "gpt-4o", "https://api.cx.example/v1", 91, "healthy"},
		{"ch_gm_gemini", "GM · Gemini", "Google", "gemini-1.5-pro", "https://api.gm.example/v1", 89, "healthy"},
		{"ch_or_openrouter", "OpenRouter", "OpenAI", "openrouter/auto", "https://openrouter.example/api/v1", 87, "degraded"},
		{"ch_sf_fast", "SwiftFlow", "OpenAI", "gpt-4o-mini", "https://swiftflow.example/v1", 84, "healthy"},
		{"ch_nb_backup", "Nimbus Backup", "OpenAI", "gpt-4.1", "https://nimbus.example/v1", 78, "functional_down"},
		{"ch_qn_qwen", "Qianwen Relay", "Alibaba", "qwen-max", "https://qwen.example/v1", 82, "healthy"},
		{"ch_ds_deepseek", "DeepSeek Relay", "DeepSeek", "deepseek-chat", "https://deepseek.example/v1", 81, "healthy"},
		{"ch_mn_mistral", "Mistral Link", "Mistral", "mistral-large", "https://mistral.example/v1", 76, "degraded"},
		{"ch_vl_volc", "Volc Relay", "Volcengine", "doubao-pro", "https://volc.example/v1", 88, "healthy"},
		{"ch_az_azure", "Azure Bridge", "OpenAI", "gpt-4o", "https://azure.example/openai", 73, "auth_error"},
		{"ch_gp_groq", "Groq Proxy", "Groq", "llama-3.1-70b", "https://groq.example/v1", 86, "healthy"},
		{"ch_bd_baidu", "Baidu Bridge", "Baidu", "ernie-4.0", "https://baidu.example/v1", 72, "connectivity_down"},
		{"ch_lt_local", "Local Trial", "OpenAI", "gpt-4o-mini", "https://local-trial.example/v1", 80, "healthy"},
	}
	for _, ch := range channels {
		if _, err := db.Exec(ctx, `
			insert into channels(id,owner_type,owner_id,name,provider,type,model,upstream_model,endpoint,status,score,probe_daily,probes_used_today,created_at,data_origin)
			values($1,'platform',null,$2,$3,'openai-compatible',$4,$4,$5,$6,$7,1000,0,now(),$8)
			on conflict(id) do update set name=excluded.name, provider=excluded.provider, model=excluded.model, endpoint=excluded.endpoint, status=excluded.status, score=excluded.score, data_origin=excluded.data_origin
		`, ch.ID, ch.Name, ch.Provider, ch.Model, ch.Endpoint, ch.Status, ch.Score, dataOrigin); err != nil {
			return err
		}
	}
	return nil
}

func seedModels(ctx context.Context, db *pgxpool.Pool) error {
	models := []struct {
		ID       string
		Provider string
		Key      string
		Name     string
		Context  int
	}{
		{"mdl_gpt4o", "OpenAI", "gpt-4o", "GPT-4o", 128000},
		{"mdl_gpt4omini", "OpenAI", "gpt-4o-mini", "GPT-4o mini", 128000},
		{"mdl_claude35", "Anthropic", "claude-3-5-sonnet", "Claude 3.5 Sonnet", 200000},
		{"mdl_gemini15", "Google", "gemini-1.5-pro", "Gemini 1.5 Pro", 1000000},
	}
	for _, m := range models {
		if _, err := db.Exec(ctx, `
			insert into model_catalog(id,provider,model_key,display_name,context_window,capabilities_json,status)
			values($1,$2,$3,$4,$5,'{"chat":true,"stream":true}'::jsonb,'active')
			on conflict(id) do update set provider=excluded.provider, model_key=excluded.model_key, display_name=excluded.display_name, context_window=excluded.context_window
		`, m.ID, m.Provider, m.Key, m.Name, m.Context); err != nil {
			return err
		}
		if _, err := db.Exec(ctx, `
			insert into model_prices(id,model_id,channel_id,input_per_mtok,output_per_mtok,currency,effective_at)
			values($1,$2,null,2.50,10.00,'USD',now())
			on conflict(id) do nothing
		`, "price_"+m.ID, m.ID); err != nil {
			return err
		}
	}
	return nil
}

func seedNotificationChannels(ctx context.Context, db *pgxpool.Pool, seedMode string) error {
	dataOrigin := "demo"
	if seedMode == "test" {
		dataOrigin = "test"
	}
	channels := []struct {
		ID     string
		Name   string
		Type   string
		Target string
	}{
		{"ntc_admin_email", "平台邮件通知", "email", "admin@tokhub.local"},
		{"ntc_admin_webhook", "平台 Webhook Mock", "webhook", "https://webhook.example/tokhub"},
	}
	for _, ch := range channels {
		if _, err := db.Exec(ctx, `
			insert into notification_channels(id,org_id,scope,name,type,target,target_ciphertext,target_nonce,target_mask,target_fingerprint,enabled,data_origin)
			values($1,'','admin',$2,$3,$4,'','','','',true,$5)
			on conflict(id) do update set
				name=excluded.name,
				type=excluded.type,
				target=excluded.target,
				target_ciphertext='',
				target_nonce='',
				target_mask='',
				target_fingerprint='',
				enabled=excluded.enabled,
				data_origin=excluded.data_origin
		`, ch.ID, ch.Name, ch.Type, ch.Target, dataOrigin); err != nil {
			return err
		}
	}
	return nil
}

func (r *Repository) UserByEmail(ctx context.Context, email string) (User, error) {
	var user User
	err := r.db.QueryRow(ctx, `
		select id,email,coalesce(username,''),password_hash,name,avatar,status,role,plan,email_verified_at is not null
		from users
		where email=$1 and status='active'
	`, email).Scan(&user.ID, &user.Email, &user.Username, &user.PasswordHash, &user.Name, &user.Avatar, &user.Status, &user.Role, &user.Plan, &user.EmailVerified)
	return user, err
}

func (r *Repository) UserByID(ctx context.Context, userID string) (User, error) {
	var user User
	err := r.db.QueryRow(ctx, `
		select id,email,coalesce(username,''),password_hash,name,avatar,status,role,plan,email_verified_at is not null
		from users
		where id=$1 and status='active'
	`, strings.TrimSpace(userID)).Scan(&user.ID, &user.Email, &user.Username, &user.PasswordHash, &user.Name, &user.Avatar, &user.Status, &user.Role, &user.Plan, &user.EmailVerified)
	return user, err
}

func (r *Repository) UserByLoginIdentifier(ctx context.Context, identifier string) (User, error) {
	identifier = strings.ToLower(strings.TrimSpace(identifier))
	if strings.Contains(identifier, "@") {
		return r.UserByEmail(ctx, identifier)
	}
	username := normalizeUsernameOrEmpty(identifier)
	if username == "" {
		return User{}, pgx.ErrNoRows
	}
	var user User
	err := r.db.QueryRow(ctx, `
		select id,email,coalesce(username,''),password_hash,name,avatar,status,role,plan,email_verified_at is not null
		from users
		where lower(username)=$1 and status='active'
	`, username).Scan(&user.ID, &user.Email, &user.Username, &user.PasswordHash, &user.Name, &user.Avatar, &user.Status, &user.Role, &user.Plan, &user.EmailVerified)
	return user, err
}

func (r *Repository) UserBySession(ctx context.Context, sessionHash string) (User, error) {
	var user User
	err := r.db.QueryRow(ctx, `
		select u.id,u.email,coalesce(u.username,''),u.password_hash,u.name,u.avatar,u.status,u.role,u.plan,u.email_verified_at is not null
		from auth_sessions s
		join users u on u.id=s.user_id
		where s.session_hash=$1
			and s.revoked_at is null
			and s.expires_at > now()
			and u.status='active'
	`, sessionHash).Scan(&user.ID, &user.Email, &user.Username, &user.PasswordHash, &user.Name, &user.Avatar, &user.Status, &user.Role, &user.Plan, &user.EmailVerified)
	return user, err
}

func (r *Repository) CreateSession(ctx context.Context, userID string, sessionHash string, ip string, userAgent string, expiresAt time.Time) error {
	_, err := r.db.Exec(ctx, `
		insert into auth_sessions(id,user_id,session_hash,ip,user_agent,expires_at)
		values($1,$2,$3,$4,$5,$6)
	`, "ses_"+uuid.NewString(), userID, sessionHash, ip, userAgent, expiresAt)
	return err
}

func (r *Repository) RevokeSession(ctx context.Context, sessionHash string) error {
	_, err := r.db.Exec(ctx, `update auth_sessions set revoked_at=now() where session_hash=$1 and revoked_at is null`, sessionHash)
	return err
}

func (r *Repository) WriteAudit(ctx context.Context, event AuditEvent) error {
	event = enrichAgentAuditEvent(ctx, event)
	meta, err := json.Marshal(event.Metadata)
	if err != nil {
		return err
	}
	_, err = r.db.Exec(ctx, `
		insert into audit_events(id,actor_type,actor_id,action,object_type,object_id,ip,result,metadata,created_at)
		values($1,$2,$3,$4,$5,$6,$7,$8,$9,now())
	`, "aud_"+uuid.NewString(), event.ActorType, event.ActorID, event.Action, event.ObjectType, event.ObjectID, event.IP, event.Result, meta)
	return err
}

func enrichAgentAuditEvent(ctx context.Context, event AuditEvent) AuditEvent {
	info, ok := AgentAuditFromContext(ctx)
	if !ok {
		return event
	}
	metadata := map[string]any{}
	for key, value := range event.Metadata {
		metadata[key] = value
	}
	if event.ActorType != "" || event.ActorID != "" {
		metadata["delegated_actor_type"] = event.ActorType
		metadata["delegated_actor_id"] = event.ActorID
	}
	metadata["agent_token_id"] = info.TokenID
	metadata["agent_token_name"] = info.TokenName
	metadata["delegated_user_id"] = info.DelegatedUserID
	metadata["delegated_user_email"] = info.DelegatedUserEmail
	if info.Reason != "" {
		metadata["agent_reason"] = info.Reason
	}
	if info.IdempotencyKey != "" {
		metadata["idempotency_key"] = info.IdempotencyKey
	}
	event.ActorType = "agent"
	event.ActorID = info.TokenID
	event.Metadata = metadata
	return event
}

func (r *Repository) SiteConfig(ctx context.Context) (SiteConfig, error) {
	var raw []byte
	err := r.db.QueryRow(ctx, `select value_json from site_configs where key='site'`).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return DefaultSiteConfig("", true), nil
	}
	if err != nil {
		return SiteConfig{}, err
	}
	var cfg SiteConfig
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return SiteConfig{}, err
	}
	return normalizeSiteConfig(cfg), nil
}

func (r *Repository) SetSiteConfig(ctx context.Context, cfg SiteConfig) error {
	return r.SetSiteConfigBy(ctx, cfg, "")
}

func (r *Repository) SetSiteConfigBy(ctx context.Context, cfg SiteConfig, actorID string) error {
	cfg = normalizeSiteConfig(cfg)
	raw, err := json.Marshal(cfg)
	if err != nil {
		return err
	}
	_, err = r.db.Exec(ctx, `
		insert into site_configs(key,value_json,updated_by,updated_at)
		values('site',$1,$2,now())
		on conflict(key) do update set value_json=excluded.value_json, updated_by=excluded.updated_by, updated_at=now()
	`, raw, nullableText(actorID))
	return err
}

func normalizeSiteConfig(cfg SiteConfig) SiteConfig {
	if cfg.BrandName == "" {
		cfg.BrandName = "TokHub"
	}
	if cfg.LogoMark == "" {
		cfg.LogoMark = "T"
	}
	cfg.DefaultGatewayPolicy = normalizeGatewayPolicy(cfg.DefaultGatewayPolicy)
	cfg.Timezone = strings.TrimSpace(cfg.Timezone)
	if cfg.Timezone == "" {
		cfg.Timezone = "Asia/Shanghai"
	}
	if len(cfg.NavItems) == 0 {
		cfg.NavItems = defaultNavItems()
	} else {
		cfg.NavItems = normalizePublicLinks(cfg.NavItems)
	}
	if len(cfg.FooterLinks) == 0 {
		cfg.FooterLinks = defaultFooterLinks()
	} else {
		cfg.FooterLinks = normalizePublicLinks(cfg.FooterLinks)
	}
	cfg.MonitorModels = normalizeMonitorModels(cfg.MonitorModels)
	if !cfg.RegistrationOpen {
		cfg.ShowRegisterCTA = false
	}
	return cfg
}

func DefaultMonitorModels() []MonitorModelConfig {
	return []MonitorModelConfig{
		{
			Key:             "claude-sonnet-4-6",
			Label:           "Claude Sonnet 4.6",
			Model:           "claude-sonnet-4-6",
			UpstreamModel:   "claude-sonnet-4-6",
			Type:            "anthropic",
			Enabled:         true,
			DefaultSelected: true,
			InputPerMTok:    3,
			OutputPerMTok:   15,
			Aliases:         []string{"claude-sonnet-4-6", "claude-sonnet-4.6", "claude-4-6-sonnet"},
		},
		{
			Key:             "gpt-5.5",
			Label:           "GPT-5.5",
			Model:           "gpt-5.5",
			UpstreamModel:   "gpt-5.5",
			Type:            "openai-compatible",
			Enabled:         true,
			DefaultSelected: true,
			InputPerMTok:    5,
			OutputPerMTok:   30,
			Aliases:         []string{"gpt-5.5", "gpt-5-5", "gpt55"},
		},
	}
}

func normalizeMonitorModels(models []MonitorModelConfig) []MonitorModelConfig {
	if len(models) == 0 {
		return DefaultMonitorModels()
	}
	out := make([]MonitorModelConfig, 0, len(models))
	seen := map[string]bool{}
	for _, item := range models {
		item.Key = strings.TrimSpace(item.Key)
		item.Label = strings.TrimSpace(item.Label)
		item.Model = strings.TrimSpace(item.Model)
		item.UpstreamModel = strings.TrimSpace(item.UpstreamModel)
		item.Type = normalizeMonitorModelType(item.Type)
		if item.Model == "" {
			continue
		}
		if item.Key == "" {
			item.Key = item.Model
		}
		if item.Label == "" {
			item.Label = item.Model
		}
		if item.UpstreamModel == "" {
			item.UpstreamModel = item.Model
		}
		if item.InputPerMTok < 0 {
			item.InputPerMTok = 0
		}
		if item.OutputPerMTok < 0 {
			item.OutputPerMTok = 0
		}
		item.Aliases = normalizeMonitorModelAliases(item.Aliases, item.Model)
		key := strings.ToLower(item.Key)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, item)
	}
	if len(out) == 0 {
		return DefaultMonitorModels()
	}
	return out
}

func normalizeMonitorModelType(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "anthropic", "gemini", "google", "openai", "openai-compatible":
		return strings.ToLower(strings.TrimSpace(value))
	default:
		return "openai-compatible"
	}
}

func normalizeMonitorModelAliases(items []string, model string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, item := range append([]string{model}, items...) {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		key := strings.ToLower(item)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, item)
	}
	return out
}

func defaultNavItems() []NavItem {
	return []NavItem{
		{Label: "首页", Href: "/"},
		{Label: "监控总览", Href: "/dashboard"},
		{Label: "精选推荐", Href: "/recommend"},
	}
}

func defaultFooterLinks() []NavItem {
	return []NavItem{
		{Label: "首页", Href: "/"},
		{Label: "监控总览", Href: "/dashboard"},
		{Label: "精选推荐", Href: "/recommend"},
		{Label: "控制台", Href: "/console"},
		{Label: "平台管理", Href: "/admin"},
	}
}

func normalizePublicLinks(items []NavItem) []NavItem {
	out := make([]NavItem, len(items))
	copy(out, items)
	homeIndex := -1
	legacyMonitorHome := false
	hasDashboard := false
	for i := range out {
		out[i].Label = strings.TrimSpace(out[i].Label)
		out[i].Href = strings.TrimSpace(out[i].Href)
		switch out[i].Href {
		case "/":
			if homeIndex < 0 {
				homeIndex = i
			}
			if isLegacyMonitorHomeLabel(out[i].Label) {
				legacyMonitorHome = true
				out[i].Label = "首页"
			}
		case "/dashboard":
			hasDashboard = true
			if out[i].Label == "" || strings.Contains(out[i].Label, "首页") {
				out[i].Label = "监控总览"
			}
		}
	}
	if !hasDashboard && homeIndex >= 0 && legacyMonitorHome {
		dashboard := NavItem{Label: "监控总览", Href: "/dashboard"}
		out = append(out, NavItem{})
		copy(out[homeIndex+2:], out[homeIndex+1:])
		out[homeIndex+1] = dashboard
	}
	return out
}

func isLegacyMonitorHomeLabel(label string) bool {
	lower := strings.ToLower(label)
	return strings.Contains(label, "监控") ||
		strings.Contains(label, "总览") ||
		strings.Contains(lower, "monitor") ||
		strings.Contains(lower, "dashboard")
}
