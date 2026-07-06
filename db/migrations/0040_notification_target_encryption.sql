alter table notification_channels
  add column if not exists target_ciphertext text not null default '',
  add column if not exists target_nonce text not null default '',
  add column if not exists target_mask text not null default '',
  add column if not exists target_fingerprint text not null default '',
  add column if not exists target_algorithm text not null default 'aes-256-gcm';

update notification_channels
set target_mask = case
  when type in ('webhook','feishu') and target ~* '^https?://[^/]+' then regexp_replace(target, '^(https?://[^/]+).*$','\1/***')
  when type='email' and target ~* '^[^@]+@[^@]+$' then regexp_replace(target, '^(.).*@(.+)$','\1***@\2')
  else '***'
end
where trim(coalesce(target_mask,'')) = ''
  and trim(coalesce(target,'')) <> '';
