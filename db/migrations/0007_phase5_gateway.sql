create table if not exists gateways (
  id text primary key,
  org_id text not null references orgs(id) on delete cascade,
  name text not null,
  slug text not null unique,
  base_url text not null,
  policy text not null check(policy in ('latency','success','cost')),
  status text not null default 'active' check(status in ('active','paused','deleted')),
  qps_limit integer not null default 60,
  quota_month integer not null default 100000,
  created_by text references users(id) on delete set null,
  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now()
);

create index if not exists idx_gateways_org_status on gateways(org_id, status, created_at desc);

create table if not exists gateway_upstreams (
  id text primary key,
  gateway_id text not null references gateways(id) on delete cascade,
  channel_id text not null references channels(id) on delete restrict,
  weight integer not null default 100,
  priority integer not null default 0,
  enabled boolean not null default true,
  created_at timestamptz not null default now(),
  unique(gateway_id, channel_id)
);

create index if not exists idx_gateway_upstreams_gateway on gateway_upstreams(gateway_id, enabled, priority);
create index if not exists idx_gateway_upstreams_channel on gateway_upstreams(channel_id);

create table if not exists gateway_keys (
  id text primary key,
  org_id text not null references orgs(id) on delete cascade,
  gateway_id text not null references gateways(id) on delete cascade,
  name text not null,
  key_hash text not null unique,
  key_prefix text not null,
  key_mask text not null,
  quota_month integer not null default 100000,
  qps_limit integer not null default 60,
  requests_used integer not null default 0,
  status text not null default 'active' check(status in ('active','revoked','expired')),
  expires_at timestamptz,
  last_used_at timestamptz,
  revoked_at timestamptz,
  created_by text references users(id) on delete set null,
  created_at timestamptz not null default now()
);

create index if not exists idx_gateway_keys_gateway on gateway_keys(gateway_id, status, created_at desc);
create index if not exists idx_gateway_keys_org on gateway_keys(org_id, status, created_at desc);

create table if not exists gateway_request_events (
  id text primary key,
  gateway_id text not null references gateways(id) on delete cascade,
  gateway_key_id text references gateway_keys(id) on delete set null,
  upstream_channel_id text references channels(id) on delete set null,
  request_path text not null,
  model text not null default '',
  status_code integer not null,
  request_tokens integer not null default 0,
  response_tokens integer not null default 0,
  cost_usd numeric(12,6) not null default 0,
  latency_ms integer not null default 0,
  error_type text,
  stream boolean not null default false,
  created_at timestamptz not null default now()
);

create index if not exists idx_gateway_events_gateway_time on gateway_request_events(gateway_id, created_at desc);
create index if not exists idx_gateway_events_key_time on gateway_request_events(gateway_key_id, created_at desc);
create index if not exists idx_gateway_events_upstream_time on gateway_request_events(upstream_channel_id, created_at desc);
