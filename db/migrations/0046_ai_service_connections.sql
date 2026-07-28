-- User-managed AI service connections and quick relay provisioning.
create table if not exists ai_connections (
  id text primary key,
  owner_user_id text not null references users(id) on delete cascade,
  org_id text not null references orgs(id) on delete cascade,
  provider text not null check(provider in ('openai','gemini','kimi','deepseek','doubao','claude','qwen')),
  product_line text not null,
  region text not null,
  workspace_id text not null default '',
  auth_method text not null default 'api_key' check(auth_method='api_key'),
  protocol text not null check(protocol in ('openai','openai_compatible','gemini','anthropic')),
  adapter_type text not null check(adapter_type in ('openai','openai-compatible','gemini','anthropic')),
  endpoint text not null,
  provider_config jsonb not null default '{}'::jsonb,
  display_name text not null,
  status text not null default 'pending' check(status in ('pending','active','attention','disabled','deleted')),
  validation_stage text not null default 'pending',
  validation_latency_ms integer not null default 0,
  model_count integer not null default 0,
  last_error_code text not null default '',
  last_error_message text not null default '',
  last_validated_at timestamptz,
  policy_version text not null default 'official-developer-credentials-v1',
  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now(),
  deleted_at timestamptz
);

create index if not exists idx_ai_connections_owner_org
  on ai_connections(owner_user_id, org_id, status, created_at desc);

create table if not exists ai_connection_secrets (
  connection_id text primary key references ai_connections(id) on delete cascade,
  ciphertext text not null,
  nonce text not null,
  mask text not null,
  fingerprint text not null,
  encryption_key_id text not null,
  fingerprint_key_id text not null,
  algorithm text not null default 'aes-256-gcm',
  version integer not null default 1,
  rotated_at timestamptz not null default now(),
  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now()
);

create index if not exists idx_ai_connection_secrets_fingerprint
  on ai_connection_secrets(fingerprint_key_id, fingerprint);

create table if not exists ai_connection_models (
  id text primary key,
  connection_id text not null references ai_connections(id) on delete cascade,
  provider_model_id text not null,
  display_name text not null,
  enabled boolean not null default true,
  verification_status text not null default 'unverified' check(verification_status in ('verified','unverified','disabled')),
  validation_latency_ms integer not null default 0,
  last_error_code text not null default '',
  last_error_message text not null default '',
  last_validated_at timestamptz,
  capabilities_json jsonb not null default '{}'::jsonb,
  route_channel_id text references channels(id) on delete set null,
  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now(),
  unique(connection_id, provider_model_id)
);

create index if not exists idx_ai_connection_models_connection
  on ai_connection_models(connection_id, enabled, created_at asc);

alter table channels
  add column if not exists ai_connection_id text references ai_connections(id) on delete set null,
  add column if not exists ai_connection_model_id text references ai_connection_models(id) on delete set null,
  add column if not exists managed_source text not null default '';

create index if not exists idx_channels_ai_connection
  on channels(ai_connection_id, ai_connection_model_id)
  where ai_connection_id is not null;

create table if not exists ai_quick_relay_requests (
  id text primary key,
  owner_user_id text not null references users(id) on delete cascade,
  org_id text not null references orgs(id) on delete cascade,
  idempotency_key text not null,
  request_hash text not null,
  connection_id text not null references ai_connections(id) on delete cascade,
  gateway_id text references gateways(id) on delete set null,
  gateway_key_id text references gateway_keys(id) on delete set null,
  reveal_ciphertext text not null,
  reveal_nonce text not null,
  reveal_encryption_key_id text not null,
  reveal_fingerprint text not null,
  reveal_fingerprint_key_id text not null,
  reveal_mask text not null,
  status text not null default 'completed' check(status in ('completed','expired')),
  expires_at timestamptz not null,
  created_at timestamptz not null default now(),
  unique(owner_user_id, org_id, idempotency_key)
);

create index if not exists idx_ai_quick_relay_requests_expiry
  on ai_quick_relay_requests(expires_at)
  where status='completed';
