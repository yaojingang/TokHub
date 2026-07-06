alter table users
  add column if not exists suspended_at timestamptz;

alter table orgs
  add column if not exists suspended_at timestamptz,
  add column if not exists updated_at timestamptz not null default now();

alter table notification_channels
  add column if not exists last_tested_at timestamptz,
  add column if not exists last_error text not null default '';

alter table alert_deliveries
  add column if not exists delivered_by text not null default '';

create index if not exists idx_users_status_created on users(status, created_at desc);
create index if not exists idx_orgs_status_created on orgs(status, created_at desc);
create index if not exists idx_open_api_sites_status_updated on open_api_sites(status, updated_at desc);
