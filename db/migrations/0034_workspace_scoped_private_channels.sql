alter table channels
  add column if not exists org_id text references orgs(id) on delete set null;

update channels c
set org_id = 'org_' || c.owner_id
where c.owner_type = 'user'
  and c.owner_id is not null
  and c.org_id is null
  and exists(select 1 from orgs o where o.id = 'org_' || c.owner_id);

create index if not exists idx_channels_org on channels(org_id, owner_type, status);
