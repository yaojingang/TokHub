update channel_credentials cc
set key_ciphertext='',
    key_nonce='',
    key_mask='deleted',
    key_fingerprint='deleted:' || cc.channel_id,
    updated_at=now()
from channels c
where c.id=cc.channel_id
  and c.deleted_at is not null
  and (
    cc.key_ciphertext <> ''
    or cc.key_nonce <> ''
    or cc.key_mask <> 'deleted'
    or cc.key_fingerprint <> 'deleted:' || cc.channel_id
  );
