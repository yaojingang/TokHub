alter table open_api_sites
  add column if not exists deleted_at timestamptz;

create index if not exists idx_open_api_sites_visible
  on open_api_sites(status, created_at desc)
  where deleted_at is null;
