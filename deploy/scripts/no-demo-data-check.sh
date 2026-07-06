#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT_DIR"

psql_exec() {
  if [[ -n "${DATABASE_URL:-}" ]]; then
    psql "$DATABASE_URL" -v ON_ERROR_STOP=1 "$@"
    return
  fi
  docker compose exec -T db psql -U tokhub -d tokhub -v ON_ERROR_STOP=1 "$@"
}

failures=0

fail() {
  failures=$((failures + 1))
  echo "FAIL: $*" >&2
}

if [[ "${TOKHUB_ENV:-}" == "production" && "${TOKHUB_SEED_MODE:-prod}" != "prod" ]]; then
  fail "TOKHUB_SEED_MODE must be prod when TOKHUB_ENV=production"
fi

if [[ "${TOKHUB_ENV:-}" == "production" && "${TOKHUB_UPSTREAM_MODE:-real}" == "mock" ]]; then
  fail "TOKHUB_UPSTREAM_MODE must not be mock when TOKHUB_ENV=production"
fi

checks="$(
  psql_exec -At -F $'\t' <<'SQL'
with
purge_users as (
  select id from users
  where role <> 'owner'
    and (
      data_origin in ('demo','test')
      or email ~* '(^|[+._-])(phase[0-9]*|load|closed|test|e2e|smoke)([+._@-]|$)'
      or email ~* '(^|[+._-])(crud|pilot)([+._@-]|$)'
      or email ~* '(gateway-user|admin-delete)'
      or email ~* '(^|[+._-])console-(empty-bulk|ui-crud|crud|usage|alert)([+._@-]|$)'
      or name ~* '(^|[[:space:]-])(phase|load|test|e2e|smoke)([[:space:]-]|$)'
      or name ~* '^Console (Empty|UI|CRUD|Usage|Alert|Owner|Member)'
      or name ~* '^(CRUD|Admin Delete|Pilot|UI One Time)'
      or name ~* '^(Codex .*Visual|Shell (Visual|Stability) Check)'
      or name ~* 'Gateway User'
    )
),
purge_orgs as (
  select id from orgs
  where id <> 'org_default'
    and (
      data_origin in ('demo','test')
      or name ~* '(phase|load|test|mock|e2e|crud|pilot|smoke|admin delete|gateway user)'
      or name ~* '^(Codex .*Visual|Shell (Visual|Stability) Check)'
      or name ~* '^Console (Empty|UI|CRUD|Usage|Alert|Owner|Member|Workspace)'
      or slug ~* '(phase|load|test|mock|e2e|crud|pilot|smoke|admin-delete|gateway-user)'
      or slug ~* 'console-(empty|ui|crud|usage|alert)'
    )
),
purge_channels as (
  select id from channels
  where data_origin in ('demo','test')
    or endpoint ~* '^https?://[^/]*\.example([/:]|$)'
    or endpoint ~* 'example/'
    or endpoint ~* '^https?://[^/]*\.invalid([/:]|$)'
    or endpoint ~* 'invalid/'
    or endpoint ~* '^https?://(localhost|127\.0\.0\.1|host\.docker\.internal)([/:]|$)'
    or name ~* '(phase|load|test|mock|e2e|crud|pilot|smoke|cc-switch|local claude adapter|测试|admin delete|detail action|rc real provider|ui one time)'
    or name ~* '^Console (Favorite|CRUD|A Usage|B Usage|Alert|Batch Incident|Draft|UI Private)'
    or owner_id in (select id from purge_users)
),
purge_gateways as (
  select id from gateways
  where data_origin in ('demo','test')
    or name ~* '(phase|load|test|mock|e2e|crud|pilot|smoke|cc-switch|local claude adapter|测试|admin disable|ui one time)'
    or name ~* '^Console (CRUD|A Usage|B Usage)'
    or org_id in (select id from purge_orgs)
    or created_by in (select id from purge_users)
),
purge_open_api_sites as (
  select id from open_api_sites
  where data_origin in ('demo','test')
    or name ~* '(phase|load|test|mock|e2e|crud|pilot|smoke)'
    or name ~* '^Console '
    or created_by in (select id from purge_users)
),
purge_notification_channels as (
  select id from notification_channels
  where data_origin in ('demo','test')
    or target ~* '(^|[/:.@-])example([./:@-]|$)'
    or target ~* '(^|[/:.@-])invalid([./:@-]|$)'
    or name ~* '(phase|load|test|mock|e2e|crud|pilot|smoke)'
    or name ~* '^Console '
),
purge_alert_rules as (
  select id from alert_rules
  where data_origin in ('demo','test')
    or name ~* '(phase|load|test|mock|e2e|crud|pilot|smoke)'
    or name ~* '^Console '
    or org_id in (select id from purge_orgs)
    or created_by in (select id from purge_users)
),
purge_recommend_rank_rules as (
  select id from recommend_rank_rules
  where data_origin in ('demo','test')
    or label ~* '(^|[[:space:]-])(phase|load|test|mock|e2e|crud|pilot|smoke)([[:space:]-]|$)'
    or label ~* '^(CRUD|UI) Rank Rule'
),
purge_audit_events as (
  select id from audit_events a
  where (a.actor_type='user' and coalesce(a.actor_id,'') <> '' and not exists(select 1 from users u where u.id=a.actor_id))
    or a.actor_id in (select id from purge_users)
    or (a.object_type='user' and coalesce(a.object_id,'') <> '' and (a.object_id in (select id from purge_users) or not exists(select 1 from users u where u.id=a.object_id)))
    or (a.object_type='org' and coalesce(a.object_id,'') <> '' and (a.object_id in (select id from purge_orgs) or not exists(select 1 from orgs o where o.id=a.object_id)))
    or (a.object_type='channel' and coalesce(a.object_id,'') <> '' and (a.object_id in (select id from purge_channels) or not exists(select 1 from channels c where c.id=a.object_id)))
    or (a.object_type='gateway' and coalesce(a.object_id,'') <> '' and (a.object_id in (select id from purge_gateways) or not exists(select 1 from gateways g where g.id=a.object_id)))
    or (a.object_type='gateway_key' and coalesce(a.object_id,'') <> '' and (a.object_id in (select id from gateway_keys where data_origin in ('demo','test') or gateway_id in (select id from purge_gateways) or created_by in (select id from purge_users)) or not exists(select 1 from gateway_keys k where k.id=a.object_id)))
    or (a.object_type='alert_rule' and coalesce(a.object_id,'') <> '' and (a.object_id in (select id from purge_alert_rules) or not exists(select 1 from alert_rules r where r.id=a.object_id)))
    or (a.object_type='notification_channel' and coalesce(a.object_id,'') <> '' and (a.object_id in (select id from purge_notification_channels) or not exists(select 1 from notification_channels n where n.id=a.object_id)))
    or (a.object_type='open_api_site' and coalesce(a.object_id,'') <> '' and (a.object_id in (select id from purge_open_api_sites) or not exists(select 1 from open_api_sites s where s.id=a.object_id)))
    or (a.object_type='incident' and coalesce(a.object_id,'') <> '' and not exists(select 1 from incidents i where i.id=a.object_id))
    or (a.metadata->>'gateway_id') in (select id from purge_gateways)
    or (a.metadata->>'channel_id') in (select id from purge_channels)
)
select 'users_test_origin_or_name', count(*) from purge_users
union all select 'orgs_test_origin_or_name', count(*) from purge_orgs
union all select 'channels_demo_or_example', count(*) from purge_channels
union all select 'gateways_test_origin_or_name', count(*) from purge_gateways
union all select 'gateway_keys_test_scope', count(*) from gateway_keys where data_origin in ('demo','test') or gateway_id in (select id from purge_gateways) or created_by in (select id from purge_users)
union all select 'gateway_request_events_test_scope', count(*) from gateway_request_events where gateway_id in (select id from purge_gateways) or upstream_channel_id in (select id from purge_channels)
union all select 'probe_runs_test_scope', count(*) from probe_runs where channel_id in (select id from purge_channels)
union all select 'probe_results_test_scope', count(*) from probe_results where channel_id in (select id from purge_channels)
union all select 'channel_status_snapshots_test_scope', count(*) from channel_status_snapshots where channel_id in (select id from purge_channels) or metadata->>'source' = 'phase2_seed'
union all select 'recommend_picks_demo_or_test', count(*) from recommend_picks where data_origin in ('demo','test') or channel_id in (select id from purge_channels)
union all select 'recommend_rewards_demo_or_test', count(*) from recommend_rewards where data_origin in ('demo','test') or channel_id in (select id from purge_channels)
union all select 'recommend_scenarios_demo_or_test', count(*) from recommend_scenarios where data_origin in ('demo','test') or channel_id in (select id from purge_channels)
union all select 'recommend_rank_rules_demo_or_test', count(*) from purge_recommend_rank_rules
union all select 'recommend_click_events_test_scope', count(*) from recommend_click_events where channel_id in (select id from purge_channels) or user_id in (select id from purge_users)
union all select 'open_api_sites_test_scope', count(*) from purge_open_api_sites
union all select 'open_api_call_logs_test_scope', count(*) from open_api_call_logs where site_id in (select id from purge_open_api_sites)
union all select 'notification_channels_demo_or_example', count(*) from purge_notification_channels
union all select 'alert_rules_test_scope', count(*) from purge_alert_rules
union all select 'alert_deliveries_test_scope', count(*) from alert_deliveries where rule_id in (select id from purge_alert_rules) or notification_channel_id in (select id from purge_notification_channels)
union all select 'usage_daily_rollups_test_scope', count(*) from usage_daily_rollups where gateway_id in (select id from purge_gateways) or channel_id in (select id from purge_channels) or member_user_id in (select id from purge_users)
union all select 'incidents_test_scope', count(*) from incidents where channel_id in (select id from purge_channels)
union all select 'incident_events_test_scope', count(*) from incident_events where incident_id in (select id from incidents where channel_id in (select id from purge_channels))
union all select 'audit_events_test_scope', count(*) from purge_audit_events;
SQL
)"

while IFS=$'\t' read -r name count; do
  [[ -z "$name" ]] && continue
  if [[ "$count" != "0" ]]; then
    fail "$name has $count remaining row(s)"
  else
    echo "PASS: $name"
  fi
done <<< "$checks"

if [[ "$failures" -gt 0 ]]; then
  echo "no-demo-data check failed with $failures issue(s)" >&2
  exit 1
fi

echo "no-demo-data check passed"
