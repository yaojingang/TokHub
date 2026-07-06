create table if not exists admin_agent_tokens (
  id text primary key,
  name text not null,
  token_hash text not null unique,
  token_prefix text not null,
  token_mask text not null,
  scopes jsonb not null default '[]'::jsonb,
  created_by text not null references users(id) on delete cascade,
  expires_at timestamptz,
  revoked_at timestamptz,
  last_used_at timestamptz,
  created_at timestamptz not null default now()
);

create index if not exists idx_admin_agent_tokens_valid on admin_agent_tokens(token_hash, expires_at) where revoked_at is null;
create index if not exists idx_admin_agent_tokens_created on admin_agent_tokens(created_at desc);
