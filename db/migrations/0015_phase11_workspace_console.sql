alter table orgs
  add column if not exists default_gateway_policy text not null default 'latency',
  add column if not exists default_notification_channel_id text,
  add column if not exists updated_at timestamptz not null default now();

do $$
begin
  if not exists (
    select 1 from pg_constraint where conname = 'orgs_default_gateway_policy_check'
  ) then
    alter table orgs add constraint orgs_default_gateway_policy_check
      check(default_gateway_policy in ('latency','success','cost'));
  end if;
end $$;

create index if not exists idx_org_members_user_status on org_members(user_id, status);
