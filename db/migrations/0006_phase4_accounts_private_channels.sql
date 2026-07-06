create table if not exists favorites (
  user_id text not null references users(id) on delete cascade,
  channel_id text not null references channels(id) on delete cascade,
  created_at timestamptz not null default now(),
  primary key(user_id, channel_id)
);

create index if not exists idx_favorites_user_time on favorites(user_id, created_at desc);

alter table channels
  add column if not exists probe_reset_date date not null default current_date;

create table if not exists channel_credentials (
  id text primary key,
  channel_id text not null unique references channels(id) on delete cascade,
  owner_id text not null references users(id) on delete cascade,
  key_ciphertext text not null,
  key_nonce text not null,
  key_fingerprint text not null,
  key_mask text not null,
  algorithm text not null default 'aes-256-gcm',
  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now()
);

create index if not exists idx_channel_credentials_owner on channel_credentials(owner_id);
create index if not exists idx_channel_credentials_fingerprint on channel_credentials(key_fingerprint);
