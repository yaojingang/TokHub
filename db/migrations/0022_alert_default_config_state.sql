create table if not exists alert_config_states (
    scope text not null check (scope in ('admin', 'console')),
    org_id text not null default '',
    defaults_initialized_at timestamptz not null default now(),
    updated_by text references users(id) on delete set null,
    updated_at timestamptz not null default now(),
    primary key (scope, org_id)
);
