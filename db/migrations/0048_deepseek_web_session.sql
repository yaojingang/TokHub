-- Opt-in DeepSeek consumer web-session authorization for personal relays.
alter table ai_connections
  drop constraint if exists ai_connections_auth_method_check;

alter table ai_connections
  add constraint ai_connections_auth_method_check
  check(auth_method in ('api_key','api_key_guided','oauth','codex_oauth','deepseek_web_token'));

alter table ai_authorization_attempts
  drop constraint if exists ai_authorization_attempts_auth_method_check,
  drop constraint if exists ai_authorization_attempts_completion_mode_check;

alter table ai_authorization_attempts
  add constraint ai_authorization_attempts_auth_method_check
    check(auth_method in ('api_key_guided','oauth','codex_oauth','deepseek_web_token')),
  add constraint ai_authorization_attempts_completion_mode_check
    check(completion_mode in ('redirect_callback','paste_callback','guided_api_key','paste_token'));
