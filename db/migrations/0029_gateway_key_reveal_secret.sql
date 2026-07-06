alter table gateway_keys
  add column if not exists key_ciphertext text,
  add column if not exists key_nonce text;
