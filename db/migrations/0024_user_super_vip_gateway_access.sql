alter table users
  add column if not exists plan text;

update users
set plan='free'
where plan is null or btrim(plan) = '' or plan not in ('free','super_vip');

update users
set plan='super_vip'
where role in ('owner','admin')
  and data_origin='system'
  and status <> 'deleted';

alter table users
  alter column plan set default 'free';

alter table users
  alter column plan set not null;

do $$
begin
  if not exists (
    select 1 from pg_constraint
    where conname = 'users_plan_check'
      and conrelid = 'users'::regclass
  ) then
    alter table users
      add constraint users_plan_check check (plan in ('free','super_vip'));
  end if;
end $$;

create index if not exists idx_users_plan on users(plan);

delete from gateway_upstreams gu
using gateways g, channels c
where gu.gateway_id=g.id
  and gu.channel_id=c.id
  and g.org_id like 'org_usr_%'
  and c.owner_type='platform'
  and not exists (
    select 1
    from users u
    where g.org_id='org_' || u.id
      and u.plan='super_vip'
      and u.status='active'
      and u.deleted_at is null
  );

update gateways g
set status='paused', updated_at=now()
where g.org_id like 'org_usr_%'
  and g.status='active'
  and not exists (
    select 1
    from gateway_upstreams gu
    where gu.gateway_id=g.id
  );
