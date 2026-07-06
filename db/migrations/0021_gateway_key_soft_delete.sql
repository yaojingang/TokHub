alter table gateway_keys
  add column if not exists deleted_at timestamptz;

create index if not exists idx_gateway_keys_visible_org
  on gateway_keys(org_id, status, created_at desc)
  where deleted_at is null;
