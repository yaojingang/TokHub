create table if not exists probe_runs (
  id text primary key,
  channel_id text not null references channels(id) on delete cascade,
  layer text not null check(layer in ('l1','l2','l3')),
  source text not null default 'scheduler',
  status text not null default 'running',
  started_at timestamptz not null default now(),
  finished_at timestamptz,
  error_type text,
  metadata jsonb not null default '{}'::jsonb
);

create index if not exists idx_probe_runs_channel_time on probe_runs(channel_id, started_at desc);
create index if not exists idx_probe_runs_layer_status on probe_runs(layer, status, started_at desc);

create table if not exists probe_results (
  id text primary key,
  probe_run_id text not null references probe_runs(id) on delete cascade,
  channel_id text not null references channels(id) on delete cascade,
  layer text not null check(layer in ('l1','l2','l3')),
  step text not null,
  status text not null,
  latency_ms integer not null default 0,
  http_status integer,
  error_type text,
  metadata jsonb not null default '{}'::jsonb,
  created_at timestamptz not null default now(),
  unique(probe_run_id, layer, step)
);

create index if not exists idx_probe_results_channel_time on probe_results(channel_id, created_at desc);
create index if not exists idx_probe_results_layer on probe_results(layer, created_at desc);

create table if not exists incidents (
  id text primary key,
  channel_id text not null references channels(id) on delete cascade,
  status text not null,
  title text not null,
  opened_at timestamptz not null default now(),
  resolved_at timestamptz,
  metadata jsonb not null default '{}'::jsonb
);

create index if not exists idx_incidents_channel_open on incidents(channel_id, opened_at desc);
create index if not exists idx_incidents_open on incidents(opened_at desc) where resolved_at is null;
