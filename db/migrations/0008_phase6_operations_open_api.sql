create table if not exists recommend_picks (
  id text primary key,
  channel_id text not null references channels(id) on delete cascade,
  position integer not null check(position between 1 and 12),
  title text not null,
  ribbon text not null default '',
  summary text not null default '',
  points_json jsonb not null default '[]'::jsonb,
  cta_label text not null default '去官方体验',
  cta_url text not null default '',
  enabled boolean not null default true,
  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now(),
  unique(position)
);

create index if not exists idx_recommend_picks_enabled on recommend_picks(enabled, position);
create index if not exists idx_recommend_picks_channel on recommend_picks(channel_id);

create table if not exists recommend_rewards (
  id text primary key,
  channel_id text references channels(id) on delete set null,
  provider_name text not null,
  reward_type text not null,
  reward_value text not null,
  code text not null default '',
  expires_at_text text not null default '',
  enabled boolean not null default true,
  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now()
);

create index if not exists idx_recommend_rewards_enabled on recommend_rewards(enabled, provider_name);

create table if not exists recommend_scenarios (
  id text primary key,
  title text not null,
  icon text not null default '',
  channel_id text references channels(id) on delete set null,
  summary text not null default '',
  position integer not null default 0,
  enabled boolean not null default true,
  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now()
);

create index if not exists idx_recommend_scenarios_enabled on recommend_scenarios(enabled, position);

create table if not exists recommend_click_events (
  id text primary key,
  item_type text not null check(item_type in ('pick','rank','reward','scenario','cta')),
  item_id text not null,
  channel_id text references channels(id) on delete set null,
  user_id text references users(id) on delete set null,
  ip text,
  user_agent text,
  created_at timestamptz not null default now()
);

create index if not exists idx_recommend_click_events_item on recommend_click_events(item_type, item_id, created_at desc);
create index if not exists idx_recommend_click_events_channel on recommend_click_events(channel_id, created_at desc);

create table if not exists open_api_sites (
  id text primary key,
  name text not null,
  site_key_hash text not null unique,
  site_key_prefix text not null,
  site_key_mask text not null,
  scopes text[] not null default array[]::text[],
  qps_limit integer not null default 60,
  status text not null default 'active' check(status in ('active','paused','revoked')),
  created_by text references users(id) on delete set null,
  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now(),
  last_used_at timestamptz
);

create index if not exists idx_open_api_sites_status on open_api_sites(status, created_at desc);

create table if not exists open_api_call_logs (
  id text primary key,
  site_id text references open_api_sites(id) on delete set null,
  endpoint text not null,
  status_code integer not null,
  latency_ms integer not null default 0,
  ip text,
  user_agent text,
  created_at timestamptz not null default now()
);

create index if not exists idx_open_api_call_logs_site_time on open_api_call_logs(site_id, created_at desc);
create index if not exists idx_open_api_call_logs_endpoint_time on open_api_call_logs(endpoint, created_at desc);
