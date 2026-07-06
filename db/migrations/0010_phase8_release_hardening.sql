create table if not exists ops_release_checks (
  id text primary key,
  check_name text not null,
  scope text not null check(scope in ('db','backup','restore','security','load','visual','deploy')),
  status text not null default 'pending' check(status in ('pending','passed','failed','waived')),
  detail jsonb not null default '{}'::jsonb,
  checked_at timestamptz not null default now()
);

create index if not exists idx_ops_release_checks_scope_status on ops_release_checks(scope, status, checked_at desc);

create index if not exists idx_audit_events_action_time on audit_events(action, created_at desc);
create index if not exists idx_audit_events_object_time on audit_events(object_type, object_id, created_at desc);
create index if not exists idx_gateway_events_model_time on gateway_request_events(model, created_at desc);
create index if not exists idx_gateway_events_status_time on gateway_request_events(status_code, created_at desc);
create index if not exists idx_probe_runs_started on probe_runs(started_at desc);
create index if not exists idx_probe_results_status_time on probe_results(status, created_at desc);
create index if not exists idx_usage_rollups_member_day on usage_daily_rollups(member_user_id, day desc);
create index if not exists idx_alert_deliveries_status_time on alert_deliveries(status, created_at desc);
create index if not exists idx_incidents_status_time on incidents(status, opened_at desc);

insert into ops_release_checks(id, check_name, scope, status, detail)
values
  ('phase8_retention_policy', 'Timescale retention/compression policies configured when supported', 'db', 'pending', '{"retention_days":90,"compression_after_days":14}'::jsonb),
  ('phase8_single_compose', 'Single-container Docker Compose starts and passes health checks', 'deploy', 'pending', '{}'::jsonb),
  ('phase8_split_compose', 'Split-role Docker Compose starts api/gateway/prober/worker', 'deploy', 'pending', '{}'::jsonb),
  ('phase8_security_scan', 'Secret and sensitive export scan passes', 'security', 'pending', '{}'::jsonb),
  ('phase8_visual_regression', 'All public, console, and admin pages captured in 1440/1280/390 viewports', 'visual', 'pending', '{}'::jsonb)
on conflict(id) do nothing;

do $$
begin
  if exists(select 1 from pg_extension where extname = 'timescaledb') then
    begin
      execute 'alter table probe_results set (timescaledb.compress, timescaledb.compress_segmentby = ''channel_id,layer'', timescaledb.compress_orderby = ''created_at desc'')';
      perform add_compression_policy('probe_results', interval '14 days', if_not_exists => true);
    exception when others then
      raise notice 'probe_results compression policy skipped: %', sqlerrm;
    end;

    begin
      perform add_retention_policy('probe_results', interval '90 days', if_not_exists => true);
    exception when others then
      raise notice 'probe_results retention policy skipped: %', sqlerrm;
    end;

    begin
      execute 'alter table gateway_request_events set (timescaledb.compress, timescaledb.compress_segmentby = ''gateway_id'', timescaledb.compress_orderby = ''created_at desc'')';
      perform add_compression_policy('gateway_request_events', interval '30 days', if_not_exists => true);
    exception when others then
      raise notice 'gateway_request_events compression policy skipped: %', sqlerrm;
    end;

    begin
      perform add_retention_policy('gateway_request_events', interval '180 days', if_not_exists => true);
    exception when others then
      raise notice 'gateway_request_events retention policy skipped: %', sqlerrm;
    end;
  end if;
end $$;
