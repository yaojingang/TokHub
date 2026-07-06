alter table incidents
  add column if not exists deleted_at timestamptz;

create index if not exists idx_incidents_active_opened
  on incidents(opened_at desc)
  where deleted_at is null;

create index if not exists idx_incidents_active_status_time
  on incidents(status, opened_at desc)
  where deleted_at is null;
