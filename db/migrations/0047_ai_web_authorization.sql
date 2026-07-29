-- Official OAuth and guided authorization lifecycle for personal AI connections.
alter table ai_connections
  drop constraint if exists ai_connections_auth_method_check;

alter table ai_connections
  add constraint ai_connections_auth_method_check
  check(auth_method in ('api_key','api_key_guided','oauth','codex_oauth'));

alter table ai_connections
  add column if not exists auth_status text not null default 'active',
  add column if not exists sharing_scope text not null default 'personal',
  add column if not exists risk_level text not null default 'standard',
  add column if not exists provider_adapter_version text not null default 'api-key-v1',
  add column if not exists terms_ack_version text not null default '',
  add column if not exists account_mask text not null default '';

alter table ai_connections
  drop constraint if exists ai_connections_auth_status_check,
  drop constraint if exists ai_connections_sharing_scope_check,
  drop constraint if exists ai_connections_risk_level_check;

alter table ai_connections
  add constraint ai_connections_auth_status_check
    check(auth_status in ('active','refreshing','attention','reauth_required','disabled','deleted')),
  add constraint ai_connections_sharing_scope_check
    check(sharing_scope='personal'),
  add constraint ai_connections_risk_level_check
    check(risk_level in ('standard','elevated','experimental','paused'));

alter table ai_connection_secrets
  add column if not exists secret_type text not null default 'api_key',
  add column if not exists payload_format text not null default 'opaque',
  add column if not exists subject_fingerprint text not null default '',
  add column if not exists expires_at timestamptz,
  add column if not exists next_refresh_at timestamptz,
  add column if not exists last_refreshed_at timestamptz,
  add column if not exists refresh_failures integer not null default 0,
  add column if not exists last_refresh_error_code text not null default '';

alter table ai_connection_secrets
  drop constraint if exists ai_connection_secrets_secret_type_check,
  drop constraint if exists ai_connection_secrets_payload_format_check,
  drop constraint if exists ai_connection_secrets_refresh_failures_check;

alter table ai_connection_secrets
  add constraint ai_connection_secrets_secret_type_check
    check(secret_type in ('api_key','oauth_bundle')),
  add constraint ai_connection_secrets_payload_format_check
    check(payload_format in ('opaque','oauth_bundle_v1')),
  add constraint ai_connection_secrets_refresh_failures_check
    check(refresh_failures >= 0);

create index if not exists idx_ai_connection_secrets_refresh_due
  on ai_connection_secrets(next_refresh_at, connection_id)
  where secret_type='oauth_bundle' and next_refresh_at is not null;

create index if not exists idx_ai_connection_subject_fingerprint
  on ai_connection_secrets(subject_fingerprint)
  where subject_fingerprint <> '';

create table if not exists ai_authorization_attempts (
  id text primary key,
  owner_user_id text not null references users(id) on delete cascade,
  org_id text not null references orgs(id) on delete cascade,
  provider text not null check(provider in ('openai','gemini','deepseek')),
  auth_method text not null check(auth_method in ('api_key_guided','oauth','codex_oauth')),
  status text not null default 'authorization_pending'
    check(status in ('authorization_pending','validating','completed','failed','cancelled','expired')),
  completion_mode text not null
    check(completion_mode in ('redirect_callback','paste_callback','guided_api_key')),
  connection_id text references ai_connections(id) on delete set null,
  error_code text not null default '',
  error_message text not null default '',
  started_at timestamptz not null default now(),
  completed_at timestamptz,
  expires_at timestamptz not null,
  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now()
);

create index if not exists idx_ai_authorization_attempts_owner
  on ai_authorization_attempts(owner_user_id, created_at desc);

create index if not exists idx_ai_authorization_attempts_expiry
  on ai_authorization_attempts(expires_at)
  where status='authorization_pending';
