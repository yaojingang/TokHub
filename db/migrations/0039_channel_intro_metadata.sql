alter table channels
  add column if not exists intro_title text not null default '',
  add column if not exists intro_summary text not null default '',
  add column if not exists intro_body text not null default '',
  add column if not exists intro_highlights jsonb not null default '[]'::jsonb,
  add column if not exists logo_url text not null default '',
  add column if not exists intro_source_url text not null default '',
  add column if not exists intro_updated_at timestamptz;

update channels
set intro_source_url = official_site_url
where coalesce(intro_source_url, '') = ''
  and coalesce(official_site_url, '') <> '';
