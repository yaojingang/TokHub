update gateway_keys
set key_ciphertext = '',
    key_nonce = ''
where coalesce(key_ciphertext, '') <> ''
   or coalesce(key_nonce, '') <> '';
