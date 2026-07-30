-- Durable, owner/provider-scoped safety state for local browser relays.
-- Live QPS protection remains in Redis; these windows and locks survive restarts.
create table if not exists ai_browser_account_risk (
  owner_user_id text not null references users(id) on delete cascade,
  org_id text not null references orgs(id) on delete cascade,
  provider text not null check(provider in ('openai','gemini','deepseek')),
  account_key text not null,
  state text not null default 'normal'
    check(state in ('normal','cooldown','reauth_required','security_locked','adapter_blocked','paused')),
  cooldown_until timestamptz,
  hour_window_started_at timestamptz not null default now(),
  day_window_started_at timestamptz not null default now(),
  rate_limit_window_started_at timestamptz not null default now(),
  requests_hour integer not null default 0 check(requests_hour >= 0),
  requests_day integer not null default 0 check(requests_day >= 0),
  rate_limit_events integer not null default 0 check(rate_limit_events >= 0),
  consecutive_failures integer not null default 0 check(consecutive_failures >= 0),
  last_request_at timestamptz,
  last_success_at timestamptz,
  last_error_at timestamptz,
  last_error_code text not null default '',
  last_challenge_at timestamptz,
  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now(),
  primary key(owner_user_id,org_id,provider,account_key)
);

create index if not exists idx_ai_browser_account_risk_state
  on ai_browser_account_risk(state,cooldown_until,updated_at desc);

create index if not exists idx_ai_browser_account_risk_owner
  on ai_browser_account_risk(owner_user_id,org_id,updated_at desc);
