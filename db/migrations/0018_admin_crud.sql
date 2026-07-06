alter table users
  add column if not exists deleted_at timestamptz;

alter table orgs
  add column if not exists deleted_at timestamptz;

create index if not exists idx_users_status_created on users(status, created_at desc);
create index if not exists idx_orgs_status_created on orgs(status, created_at desc);
