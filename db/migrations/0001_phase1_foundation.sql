create extension if not exists timescaledb cascade;

create table if not exists users (
  id text primary key,
  email text not null unique,
  password_hash text not null,
  name text not null,
  avatar text not null default '',
  status text not null default 'active',
  role text not null default 'user',
  email_verified_at timestamptz,
  last_active_at timestamptz,
  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now()
);

create table if not exists auth_sessions (
  id text primary key,
  user_id text not null references users(id) on delete cascade,
  session_hash text not null unique,
  ip text,
  user_agent text,
  expires_at timestamptz not null,
  revoked_at timestamptz,
  created_at timestamptz not null default now()
);

create index if not exists idx_auth_sessions_user on auth_sessions(user_id);
create index if not exists idx_auth_sessions_valid on auth_sessions(session_hash, expires_at) where revoked_at is null;

create table if not exists email_tokens (
  id text primary key,
  user_id text not null references users(id) on delete cascade,
  type text not null,
  token_hash text not null unique,
  expires_at timestamptz not null,
  used_at timestamptz,
  created_at timestamptz not null default now()
);

create table if not exists orgs (
  id text primary key,
  name text not null,
  slug text not null unique,
  plan text not null default 'starter',
  status text not null default 'active',
  timezone text not null default 'Asia/Shanghai',
  created_at timestamptz not null default now()
);

create table if not exists org_members (
  org_id text not null references orgs(id) on delete cascade,
  user_id text not null references users(id) on delete cascade,
  role text not null,
  group_name text,
  status text not null default 'active',
  created_at timestamptz not null default now(),
  primary key(org_id, user_id)
);

create table if not exists channels (
  id text primary key,
  owner_type text not null check(owner_type in ('platform','user')),
  owner_id text,
  name text not null,
  provider text not null,
  type text not null,
  model text not null,
  upstream_model text not null,
  endpoint text not null,
  status text not null default 'unknown',
  score integer not null default 0,
  probe_daily integer not null default 0,
  probes_used_today integer not null default 0,
  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now()
);

create index if not exists idx_channels_owner on channels(owner_type, owner_id);
create index if not exists idx_channels_status on channels(status);

create table if not exists model_catalog (
  id text primary key,
  provider text not null,
  model_key text not null unique,
  display_name text not null,
  context_window integer not null default 0,
  capabilities_json jsonb not null default '{}'::jsonb,
  status text not null default 'active',
  created_at timestamptz not null default now()
);

create table if not exists model_prices (
  id text primary key,
  model_id text not null references model_catalog(id) on delete cascade,
  channel_id text references channels(id) on delete cascade,
  input_per_mtok numeric(12,4) not null,
  output_per_mtok numeric(12,4) not null,
  currency text not null default 'USD',
  effective_at timestamptz not null default now()
);

create table if not exists site_configs (
  key text primary key,
  value_json jsonb not null,
  updated_by text references users(id) on delete set null,
  updated_at timestamptz not null default now()
);

create table if not exists audit_events (
  id text primary key,
  actor_type text not null,
  actor_id text,
  action text not null,
  object_type text not null,
  object_id text,
  ip text,
  result text not null,
  metadata jsonb not null default '{}'::jsonb,
  created_at timestamptz not null default now()
);

create index if not exists idx_audit_events_created on audit_events(created_at desc);
create index if not exists idx_audit_events_actor on audit_events(actor_type, actor_id, created_at desc);
