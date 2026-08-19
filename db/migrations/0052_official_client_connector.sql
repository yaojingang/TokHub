-- Signed local connectors for official Codex app-server and Grok Build ACP clients.

alter table ai_connections
  drop constraint if exists ai_connections_auth_method_check;

alter table ai_connections
  add constraint ai_connections_auth_method_check
  check(auth_method in (
    'api_key','api_key_guided','oauth','codex_oauth','deepseek_web_token',
    'opencli_browser','official_client'
  ));

alter table ai_connection_secrets
  drop constraint if exists ai_connection_secrets_secret_type_check,
  drop constraint if exists ai_connection_secrets_payload_format_check;

alter table ai_connection_secrets
  add constraint ai_connection_secrets_secret_type_check
    check(secret_type in ('api_key','oauth_bundle','browser_connector','client_connector')),
  add constraint ai_connection_secrets_payload_format_check
    check(payload_format in ('opaque','oauth_bundle_v1','browser_connector_v1','client_connector_v1'));

create table if not exists ai_client_connectors (
  id text primary key,
  owner_user_id text not null references users(id) on delete cascade,
  org_id text not null references orgs(id) on delete cascade,
  display_name text not null,
  status text not null default 'pending' check(status in ('pending','active','revoked')),
  pairing_hash text not null default '',
  pairing_expires_at timestamptz,
  token_hash text not null default '',
  token_prefix text not null default '',
  public_key text not null default '',
  runtime_kind text not null default 'container' check(runtime_kind='container'),
  connector_version text not null default '',
  codex_version text not null default '',
  grok_version text not null default '',
  capabilities_json jsonb not null default '[]'::jsonb,
  identity_json jsonb not null default '{}'::jsonb,
  last_seen_at timestamptz,
  paired_at timestamptz,
  revoked_at timestamptz,
  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now()
);

create index if not exists idx_ai_client_connectors_owner
  on ai_client_connectors(owner_user_id,org_id,status,created_at desc);

create unique index if not exists idx_ai_client_connectors_pairing_hash
  on ai_client_connectors(pairing_hash) where pairing_hash <> '';

create unique index if not exists idx_ai_client_connectors_token_hash
  on ai_client_connectors(token_hash) where token_hash <> '';

create unique index if not exists idx_ai_client_one_connector_per_owner
  on ai_client_connectors(owner_user_id,org_id) where status in ('pending','active');

create table if not exists ai_client_tasks (
  id text primary key,
  connector_id text not null references ai_client_connectors(id) on delete cascade,
  owner_user_id text not null references users(id) on delete cascade,
  org_id text not null references orgs(id) on delete cascade,
  connection_id text references ai_connections(id) on delete set null,
  provider text not null check(provider in ('openai','grok')),
  action text not null check(action in ('identify','generate','interrupt','delete_session')),
  payload_key text not null,
  result_key text not null default '',
  status text not null default 'queued'
    check(status in ('queued','claimed','completed','failed','expired','cancelled')),
  lease_hash text not null default '',
  lease_expires_at timestamptz,
  error_code text not null default '',
  error_message text not null default '',
  expires_at timestamptz not null,
  claimed_at timestamptz,
  completed_at timestamptz,
  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now()
);

create index if not exists idx_ai_client_tasks_claim
  on ai_client_tasks(connector_id,status,created_at asc)
  where status in ('queued','claimed');

create index if not exists idx_ai_client_tasks_expiry
  on ai_client_tasks(expires_at) where status in ('queued','claimed');

create unique index if not exists idx_ai_client_tasks_single_active
  on ai_client_tasks(connector_id) where status in ('queued','claimed');

create table if not exists ai_client_responses (
  id text primary key,
  owner_user_id text not null references users(id) on delete cascade,
  org_id text not null references orgs(id) on delete cascade,
  gateway_key_id text not null references gateway_keys(id) on delete cascade,
  connection_id text not null references ai_connections(id) on delete cascade,
  connector_id text not null references ai_client_connectors(id) on delete cascade,
  provider text not null check(provider in ('openai','grok')),
  model text not null,
  session_ciphertext text not null,
  session_nonce text not null,
  previous_response_id text references ai_client_responses(id) on delete set null,
  status text not null default 'active' check(status in ('active','expired','deleted')),
  idle_expires_at timestamptz not null,
  absolute_expires_at timestamptz not null,
  last_used_at timestamptz not null default now(),
  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now()
);

create index if not exists idx_ai_client_responses_owner
  on ai_client_responses(owner_user_id,org_id,created_at desc);

create index if not exists idx_ai_client_responses_expiry
  on ai_client_responses(idle_expires_at,absolute_expires_at)
  where status='active';

create table if not exists ai_client_account_risk (
  connection_id text primary key references ai_connections(id) on delete cascade,
  owner_user_id text not null references users(id) on delete cascade,
  org_id text not null references orgs(id) on delete cascade,
  connector_id text not null references ai_client_connectors(id) on delete cascade,
  provider text not null check(provider in ('openai','grok')),
  account_subject text not null default '',
  identity_assurance text not null default 'device'
    check(identity_assurance in ('account','device')),
  state text not null default 'normal'
    check(state in ('normal','cooldown','reauth_required','security_locked','policy_locked','manual_recovery','paused')),
  cooldown_until timestamptz,
  hour_window_started_at timestamptz not null default now(),
  day_window_started_at timestamptz not null default now(),
  rate_limit_window_started_at timestamptz not null default now(),
  requests_hour integer not null default 0 check(requests_hour >= 0),
  requests_day integer not null default 0 check(requests_day >= 0),
  rate_limit_events integer not null default 0 check(rate_limit_events >= 0),
  identity_changes integer not null default 0 check(identity_changes >= 0),
  consecutive_failures integer not null default 0 check(consecutive_failures >= 0),
  last_request_at timestamptz,
  last_success_at timestamptz,
  last_error_at timestamptz,
  last_error_code text not null default '',
  last_identity_change_at timestamptz,
  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now()
);

create index if not exists idx_ai_client_account_risk_state
  on ai_client_account_risk(state,cooldown_until,updated_at desc);

create unique index if not exists idx_ai_client_one_connection_per_provider
  on ai_connections(owner_user_id,org_id,provider)
  where auth_method='official_client' and status not in ('deleted','disabled');
