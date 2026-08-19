-- TokHub 2.0 RC2 provider policy containment.
-- Consumer OAuth/web tokens are intentionally destroyed and cannot be restored.

alter table ai_connections drop constraint if exists ai_connections_provider_check;
alter table ai_connections
  add constraint ai_connections_provider_check
  check(provider in ('openai','gemini','kimi','deepseek','grok','doubao','claude','qwen'));

alter table ai_authorization_attempts drop constraint if exists ai_authorization_attempts_provider_check;
alter table ai_authorization_attempts
  add constraint ai_authorization_attempts_provider_check
  check(provider in ('openai','gemini','deepseek','grok'));

update ai_connections
set status='disabled',
    auth_status='disabled',
    risk_level='paused',
    validation_stage='policy_disabled',
    last_error_code='provider_policy_migrated',
    last_error_message='Consumer account credential removed by TokHub 2.0 RC2 provider safety policy',
    policy_version='provider-policy-2026-08-19',
    updated_at=now()
where auth_method in ('codex_oauth','deepseek_web_token')
  and status <> 'deleted';

update ai_connection_secrets s
set ciphertext='',
    nonce='',
    mask='removed by provider policy',
    fingerprint='',
    subject_fingerprint='',
    expires_at=null,
    next_refresh_at=null,
    last_refreshed_at=null,
    refresh_failures=0,
    last_refresh_error_code='provider_policy_migrated',
    rotated_at=now(),
    updated_at=now()
from ai_connections c
where c.id=s.connection_id
  and c.auth_method in ('codex_oauth','deepseek_web_token');

update channels ch
set gateway_enabled=false,
    public_visible=false,
    status='disabled',
    disabled_at=coalesce(disabled_at,now()),
    updated_at=now()
from ai_connections c
where ch.ai_connection_id=c.id
  and c.auth_method in ('codex_oauth','deepseek_web_token','opencli_browser')
  and ch.deleted_at is null;

update ai_connections
set policy_version='provider-policy-2026-08-19', updated_at=now()
where status <> 'deleted';
