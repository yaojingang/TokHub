-- Personal local browser connectors. Browser credentials remain on the user's device.
alter table ai_connections
  drop constraint if exists ai_connections_auth_method_check;

alter table ai_connections
  add constraint ai_connections_auth_method_check
  check(auth_method in ('api_key','api_key_guided','oauth','codex_oauth','deepseek_web_token','opencli_browser'));

alter table ai_connection_secrets
  drop constraint if exists ai_connection_secrets_secret_type_check,
  drop constraint if exists ai_connection_secrets_payload_format_check;

alter table ai_connection_secrets
  add constraint ai_connection_secrets_secret_type_check
    check(secret_type in ('api_key','oauth_bundle','browser_connector')),
  add constraint ai_connection_secrets_payload_format_check
    check(payload_format in ('opaque','oauth_bundle_v1','browser_connector_v1'));

create table if not exists ai_browser_connectors (
  id text primary key,
  owner_user_id text not null references users(id) on delete cascade,
  org_id text not null references orgs(id) on delete cascade,
  display_name text not null,
  status text not null default 'pending' check(status in ('pending','active','revoked')),
  pairing_hash text not null default '',
  pairing_expires_at timestamptz,
  token_hash text not null default '',
  token_prefix text not null default '',
  opencli_version text not null default '',
  extension_version text not null default '',
  capabilities_json jsonb not null default '[]'::jsonb,
  last_seen_at timestamptz,
  paired_at timestamptz,
  revoked_at timestamptz,
  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now()
);

create index if not exists idx_ai_browser_connectors_owner
  on ai_browser_connectors(owner_user_id,org_id,status,created_at desc);

create unique index if not exists idx_ai_browser_connectors_pairing_hash
  on ai_browser_connectors(pairing_hash)
  where pairing_hash <> '';

create unique index if not exists idx_ai_browser_connectors_token_hash
  on ai_browser_connectors(token_hash)
  where token_hash <> '';

create table if not exists ai_browser_tasks (
  id text primary key,
  connector_id text not null references ai_browser_connectors(id) on delete cascade,
  owner_user_id text not null references users(id) on delete cascade,
  org_id text not null references orgs(id) on delete cascade,
  connection_id text references ai_connections(id) on delete set null,
  provider text not null check(provider in ('openai','gemini','deepseek')),
  action text not null check(action in ('status','ask')),
  request_json jsonb not null default '{}'::jsonb,
  response_json jsonb not null default '{}'::jsonb,
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

create index if not exists idx_ai_browser_tasks_claim
  on ai_browser_tasks(connector_id,status,created_at asc)
  where status in ('queued','claimed');

create index if not exists idx_ai_browser_tasks_expiry
  on ai_browser_tasks(expires_at)
  where status in ('queued','claimed');

create unique index if not exists idx_ai_browser_tasks_single_active
  on ai_browser_tasks(connector_id)
  where status in ('queued','claimed');
