create table if not exists channel_status_snapshots (
  id text primary key,
  channel_id text not null references channels(id) on delete cascade,
  sampled_at timestamptz not null,
  status text not null,
  score integer not null,
  uptime_24h numeric(6,3) not null,
  success_rate numeric(6,3) not null,
  latency_p95_ms integer not null,
  l1_status text not null,
  l2_status text not null,
  l3_status text not null,
  l1_latency_ms integer not null,
  l2_latency_ms integer not null,
  l3_latency_ms integer not null,
  tokens_used integer not null default 0,
  cost_usd numeric(12,6) not null default 0,
  error_type text,
  metadata jsonb not null default '{}'::jsonb
);

create index if not exists idx_channel_snapshots_channel_time on channel_status_snapshots(channel_id, sampled_at desc);
create index if not exists idx_channel_snapshots_time on channel_status_snapshots(sampled_at desc);
create index if not exists idx_channel_snapshots_status on channel_status_snapshots(status);
