alter table users
  add column if not exists username text;

update users
set username = 'admin'
where id = (
  select id
  from users
  where (username is null or username = '')
    and role = 'owner'
    and data_origin = 'system'
    and email = 'admin@tokhub.local'
  order by created_at asc
  limit 1
);

create unique index if not exists idx_users_username_lower
  on users ((lower(username)))
  where username is not null and username <> '';
