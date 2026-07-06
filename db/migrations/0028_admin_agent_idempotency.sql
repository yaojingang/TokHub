create table if not exists admin_agent_idempotency_keys (
  id text primary key,
  token_id text not null references admin_agent_tokens(id) on delete cascade,
  idempotency_key text not null,
  method text not null,
  path text not null,
  created_at timestamptz not null default now(),
  unique(token_id, idempotency_key)
);

create index if not exists idx_admin_agent_idempotency_created on admin_agent_idempotency_keys(created_at desc);
