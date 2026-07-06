alter table channels
  add column if not exists public_slug text not null default '';

do $$
declare
  rec record;
  candidate text;
begin
  for rec in
    select id
    from channels
    where coalesce(public_slug, '') = ''
  loop
    loop
      candidate := lower(substr(md5(rec.id || random()::text || clock_timestamp()::text), 1, 8));
      exit when not exists (
        select 1
        from channels
        where public_slug = candidate
      );
    end loop;

    update channels
    set public_slug = candidate,
        updated_at = now()
    where id = rec.id;
  end loop;
end $$;

alter table channels
  alter column public_slug set default lower(substr(md5(random()::text || clock_timestamp()::text), 1, 8));

create unique index if not exists idx_channels_public_slug_unique
  on channels(public_slug)
  where public_slug <> '';
