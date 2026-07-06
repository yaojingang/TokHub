create table if not exists usage_daily_rollups (
  day date not null,
  org_id text not null default '',
  source text not null check(source in ('gateway','probe')),
  gateway_id text not null default '',
  channel_id text not null default '',
  model text not null default '',
  member_user_id text not null default '',
  requests integer not null default 0,
  tokens integer not null default 0,
  cost_usd numeric(12,6) not null default 0,
  errors integer not null default 0,
  probe_runs integer not null default 0,
  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now(),
  primary key(day, org_id, source, gateway_id, channel_id, model, member_user_id)
);

create index if not exists idx_usage_rollups_org_day on usage_daily_rollups(org_id, day desc);
create index if not exists idx_usage_rollups_source_day on usage_daily_rollups(source, day desc);

create table if not exists alert_rules (
  id text primary key,
  org_id text not null default '',
  scope text not null check(scope in ('admin','console')),
  name text not null,
  kind text not null check(kind in ('l3_consecutive_failures','cost_threshold','gateway_error_rate','quota_anomaly')),
  severity text not null default 'warning' check(severity in ('info','warning','critical')),
  threshold numeric(12,4) not null default 0,
  window_minutes integer not null default 60,
  dedupe_minutes integer not null default 30,
  enabled boolean not null default true,
  config jsonb not null default '{}'::jsonb,
  created_by text references users(id) on delete set null,
  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now()
);

create index if not exists idx_alert_rules_scope_org on alert_rules(scope, org_id, enabled, created_at desc);

create table if not exists notification_channels (
  id text primary key,
  org_id text not null default '',
  scope text not null check(scope in ('admin','console')),
  name text not null,
  type text not null check(type in ('email','webhook','feishu')),
  target text not null,
  enabled boolean not null default true,
  created_by text references users(id) on delete set null,
  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now()
);

create index if not exists idx_notification_channels_scope_org on notification_channels(scope, org_id, enabled, created_at desc);

create table if not exists alert_deliveries (
  id text primary key,
  org_id text not null default '',
  scope text not null check(scope in ('admin','console')),
  rule_id text references alert_rules(id) on delete set null,
  notification_channel_id text references notification_channels(id) on delete set null,
  incident_id text references incidents(id) on delete set null,
  dedupe_key text not null,
  severity text not null default 'warning',
  status text not null check(status in ('sent','failed','suppressed','recovered','test')),
  title text not null,
  message text not null,
  error text not null default '',
  metadata jsonb not null default '{}'::jsonb,
  created_at timestamptz not null default now(),
  sent_at timestamptz
);

create index if not exists idx_alert_deliveries_scope_org_time on alert_deliveries(scope, org_id, created_at desc);
create index if not exists idx_alert_deliveries_rule_time on alert_deliveries(rule_id, created_at desc);
create index if not exists idx_alert_deliveries_dedupe on alert_deliveries(dedupe_key, created_at desc);

create table if not exists incident_events (
  id text primary key,
  incident_id text references incidents(id) on delete cascade,
  org_id text not null default '',
  event_type text not null,
  message text not null,
  metadata jsonb not null default '{}'::jsonb,
  created_at timestamptz not null default now()
);

create index if not exists idx_incident_events_incident_time on incident_events(incident_id, created_at desc);
create index if not exists idx_incident_events_org_time on incident_events(org_id, created_at desc);

insert into alert_rules(id,org_id,scope,name,kind,severity,threshold,window_minutes,dedupe_minutes,enabled,config)
values
  ('alr_admin_gateway_error_rate','', 'admin', '平台网关错误率超过 20%', 'gateway_error_rate', 'critical', 20, 60, 30, true, '{"unit":"percent"}'::jsonb),
  ('alr_admin_cost_threshold','', 'admin', '平台今日成本超过 $1', 'cost_threshold', 'warning', 1, 1440, 120, true, '{"unit":"usd"}'::jsonb),
  ('alr_admin_l3_failures','', 'admin', '平台 L3 连续失败超过 2 次', 'l3_consecutive_failures', 'critical', 2, 60, 30, true, '{"layer":"l3"}'::jsonb),
  ('alr_admin_quota_anomaly','', 'admin', '平台 Key 配额使用超过 90%', 'quota_anomaly', 'warning', 90, 1440, 120, true, '{"unit":"percent"}'::jsonb)
on conflict(id) do nothing;
