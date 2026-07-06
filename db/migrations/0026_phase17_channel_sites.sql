create table if not exists channel_sites (
	id text primary key,
	name text not null,
	slug text not null unique,
	domain text not null default '',
	public_url text not null default '',
	runtime_key_hash text not null unique,
	runtime_key_prefix text not null,
	runtime_key_mask text not null,
	title text not null,
	description text not null default '',
	logo_mark text not null default 'T',
	overview_label text not null default '监控总览',
	recommend_label text not null default '精选推荐',
	modules jsonb not null default '{"overview":true,"channelBoard":true,"recommend":true,"providerRank":true,"strategy":true}'::jsonb,
	copy_json jsonb not null default '{}'::jsonb,
	seo_json jsonb not null default '{}'::jsonb,
	qps_limit integer not null default 60,
	status text not null default 'active' check (status in ('active','paused','revoked')),
	created_by text references users(id) on delete set null,
	created_at timestamptz not null default now(),
	updated_at timestamptz not null default now(),
	last_used_at timestamptz,
	deleted_at timestamptz
);

create table if not exists channel_site_nav_items (
	id text primary key,
	site_id text not null references channel_sites(id) on delete cascade,
	label text not null,
	href text not null,
	position integer not null check (position between 1 and 3),
	created_at timestamptz not null default now(),
	updated_at timestamptz not null default now(),
	unique(site_id, position)
);

create table if not exists channel_site_package_exports (
	id text primary key,
	site_id text not null references channel_sites(id) on delete cascade,
	version text not null,
	file_name text not null,
	file_size integer not null default 0,
	created_by text references users(id) on delete set null,
	created_at timestamptz not null default now()
);

create table if not exists channel_site_runtime_logs (
	id text primary key,
	site_id text references channel_sites(id) on delete set null,
	endpoint text not null,
	status_code integer not null,
	latency_ms integer not null default 0,
	origin text,
	ip text,
	user_agent text,
	created_at timestamptz not null default now()
);

create index if not exists idx_channel_sites_status on channel_sites(status) where deleted_at is null;
create index if not exists idx_channel_site_nav_items_site on channel_site_nav_items(site_id, position);
create index if not exists idx_channel_site_exports_site on channel_site_package_exports(site_id, created_at desc);
create index if not exists idx_channel_site_runtime_logs_site on channel_site_runtime_logs(site_id, created_at desc);
create index if not exists idx_channel_site_runtime_logs_endpoint on channel_site_runtime_logs(endpoint, created_at desc);
