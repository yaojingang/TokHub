alter table channels
  add column if not exists official_site_url text not null default '';
