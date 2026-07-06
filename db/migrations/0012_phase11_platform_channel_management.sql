alter table channels add column if not exists public_visible boolean not null default true;
alter table channels add column if not exists gateway_enabled boolean not null default true;
alter table channels add column if not exists disabled_at timestamptz;
alter table channels add column if not exists deleted_at timestamptz;

create index if not exists idx_channels_public_visible on channels(owner_type, public_visible, status, score desc);
create index if not exists idx_channels_gateway_enabled on channels(owner_type, gateway_enabled, status, score desc);

update channels
set public_visible=true,
    gateway_enabled=true
where owner_type='platform'
  and deleted_at is null
  and status <> 'deleted';

update channels
set public_visible=false,
    gateway_enabled=false,
    disabled_at=coalesce(disabled_at, now())
where status in ('disabled','deleted');
