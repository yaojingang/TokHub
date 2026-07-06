#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT_DIR"

MODE="dry-run"
REPORT_DIR="${TOKHUB_PURGE_REPORT_DIR:-tmp}"

while [[ $# -gt 0 ]]; do
  case "$1" in
    --dry-run)
      MODE="dry-run"
      shift
      ;;
    --confirm)
      if [[ "${2:-}" != "purge-demo" ]]; then
        echo "refusing to purge: --confirm must be exactly 'purge-demo'" >&2
        exit 2
      fi
      MODE="purge"
      shift 2
      ;;
    --report-dir)
      REPORT_DIR="${2:-}"
      shift 2
      ;;
    *)
      echo "unknown argument: $1" >&2
      exit 2
      ;;
  esac
done

psql_exec() {
  if [[ -n "${DATABASE_URL:-}" ]]; then
    psql "$DATABASE_URL" -v ON_ERROR_STOP=1 "$@"
    return
  fi
  docker compose exec -T db psql -U tokhub -d tokhub -v ON_ERROR_STOP=1 "$@"
}

mkdir -p "$REPORT_DIR"
stamp="$(date +%Y%m%d-%H%M%S)"
json_report="$REPORT_DIR/purge-demo-$stamp.json"
md_report="$REPORT_DIR/purge-demo-$stamp.md"

counts="$(
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
select 'users', count(*) from purge_users
union all select 'orgs', count(*) from purge_orgs
union all select 'channels', count(*) from purge_channels
union all select 'gateways', count(*) from purge_gateways
union all select 'gateway_keys', count(*) from gateway_keys where data_origin in ('demo','test') or gateway_id in (select id from purge_gateways) or created_by in (select id from purge_users)
union all select 'gateway_request_events', count(*) from gateway_request_events where gateway_id in (select id from purge_gateways) or upstream_channel_id in (select id from purge_channels)
union all select 'probe_runs', count(*) from probe_runs where channel_id in (select id from purge_channels)
union all select 'probe_results', count(*) from probe_results where channel_id in (select id from purge_channels)
union all select 'channel_status_snapshots', count(*) from channel_status_snapshots where channel_id in (select id from purge_channels) or metadata->>'source' = 'phase2_seed'
union all select 'recommend_picks', count(*) from recommend_picks where data_origin in ('demo','test') or channel_id in (select id from purge_channels)
union all select 'recommend_rewards', count(*) from recommend_rewards where data_origin in ('demo','test') or channel_id in (select id from purge_channels)
union all select 'recommend_scenarios', count(*) from recommend_scenarios where data_origin in ('demo','test') or channel_id in (select id from purge_channels)
union all select 'recommend_rank_rules', count(*) from purge_recommend_rank_rules
union all select 'recommend_click_events', count(*) from recommend_click_events where channel_id in (select id from purge_channels) or user_id in (select id from purge_users)
union all select 'open_api_sites', count(*) from purge_open_api_sites
union all select 'open_api_call_logs', count(*) from open_api_call_logs where site_id in (select id from purge_open_api_sites)
union all select 'notification_channels', count(*) from purge_notification_channels
union all select 'alert_rules', count(*) from purge_alert_rules
union all select 'alert_deliveries', count(*) from alert_deliveries where rule_id in (select id from purge_alert_rules) or notification_channel_id in (select id from purge_notification_channels)
union all select 'usage_daily_rollups', count(*) from usage_daily_rollups where gateway_id in (select id from purge_gateways) or channel_id in (select id from purge_channels) or member_user_id in (select id from purge_users)
union all select 'incidents', count(*) from incidents where channel_id in (select id from purge_channels)
union all select 'incident_events', count(*) from incident_events where incident_id in (select id from incidents where channel_id in (select id from purge_channels))
union all select 'audit_events', count(*) from purge_audit_events;
SQL
)"

{
  echo "{"
  first=1
  while IFS=$'\t' read -r metric count; do
    [[ -z "$metric" ]] && continue
    if [[ "$first" -eq 0 ]]; then
      echo ","
    fi
    first=0
    printf '  "%s": %s' "$metric" "$count"
  done <<< "$counts"
  echo
  echo "}"
} > "$json_report"

{
  echo "# TokHub Demo/Test Data Purge Report"
  echo
  echo "- Mode: $MODE"
  echo "- Generated at: $(date -u +%Y-%m-%dT%H:%M:%SZ)"
  echo
  echo "| Object | Count |"
  echo "| --- | ---: |"
  while IFS=$'\t' read -r metric count; do
    [[ -z "$metric" ]] && continue
    echo "| \`$metric\` | $count |"
  done <<< "$counts"
} > "$md_report"

echo "purge report written:"
echo "  $json_report"
echo "  $md_report"

if [[ "$MODE" == "dry-run" ]]; then
  echo "dry-run only; no data was deleted"
  exit 0
fi

if [[ "${TOKHUB_ENV:-development}" == "production" && "${TOKHUB_ALLOW_DEMO_PURGE:-}" != "true" ]]; then
  echo "refusing to purge production without TOKHUB_ALLOW_DEMO_PURGE=true" >&2
  exit 2
fi

if [[ -z "${TOKHUB_DEMO_PURGE_BACKUP:-}" || ! -f "${TOKHUB_DEMO_PURGE_BACKUP:-}" ]]; then
  echo "refusing to purge without TOKHUB_DEMO_PURGE_BACKUP pointing to an existing backup file" >&2
  exit 2
fi

psql_exec <<'SQL'
begin;

create temporary table purge_users as
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
    );

create temporary table purge_orgs as
  select id from orgs
  where id <> 'org_default'
    and (
      data_origin in ('demo','test')
      or name ~* '(phase|load|test|mock|e2e|crud|pilot|smoke|admin delete|gateway user)'
      or name ~* '^(Codex .*Visual|Shell (Visual|Stability) Check)'
      or name ~* '^Console (Empty|UI|CRUD|Usage|Alert|Owner|Member|Workspace)'
      or slug ~* '(phase|load|test|mock|e2e|crud|pilot|smoke|admin-delete|gateway-user)'
      or slug ~* 'console-(empty|ui|crud|usage|alert)'
    );

create temporary table purge_channels as
  select id from channels
  where data_origin in ('demo','test')
    or endpoint ~* '^https?://[^/]*\.example([/:]|$)'
    or endpoint ~* 'example/'
    or endpoint ~* '^https?://[^/]*\.invalid([/:]|$)'
    or endpoint ~* 'invalid/'
    or endpoint ~* '^https?://(localhost|127\.0\.0\.1|host\.docker\.internal)([/:]|$)'
    or name ~* '(phase|load|test|mock|e2e|crud|pilot|smoke|cc-switch|local claude adapter|测试|admin delete|detail action|rc real provider|ui one time)'
    or name ~* '^Console (Favorite|CRUD|A Usage|B Usage|Alert|Batch Incident|Draft|UI Private)'
    or owner_id in (select id from purge_users);

create temporary table purge_gateways as
  select id from gateways
  where data_origin in ('demo','test')
    or name ~* '(phase|load|test|mock|e2e|crud|pilot|smoke|cc-switch|local claude adapter|测试|admin disable|ui one time)'
    or name ~* '^Console (CRUD|A Usage|B Usage)'
    or org_id in (select id from purge_orgs)
    or created_by in (select id from purge_users);

create temporary table purge_open_api_sites as
  select id from open_api_sites
  where data_origin in ('demo','test')
    or name ~* '(phase|load|test|mock|e2e|crud|pilot|smoke)'
    or name ~* '^Console '
    or created_by in (select id from purge_users);

create temporary table purge_notification_channels as
  select id from notification_channels
  where data_origin in ('demo','test')
    or target ~* '(^|[/:.@-])example([./:@-]|$)'
    or target ~* '(^|[/:.@-])invalid([./:@-]|$)'
    or name ~* '(phase|load|test|mock|e2e|crud|pilot|smoke)'
    or name ~* '^Console ';

create temporary table purge_alert_rules as
  select id from alert_rules
  where data_origin in ('demo','test')
    or name ~* '(phase|load|test|mock|e2e|crud|pilot|smoke)'
    or name ~* '^Console '
    or org_id in (select id from purge_orgs)
    or created_by in (select id from purge_users);

create temporary table purge_recommend_rank_rules as
  select id from recommend_rank_rules
  where data_origin in ('demo','test')
    or label ~* '(^|[[:space:]-])(phase|load|test|mock|e2e|crud|pilot|smoke)([[:space:]-]|$)'
    or label ~* '^(CRUD|UI) Rank Rule';

create temporary table purge_audit_events as
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
    or (a.metadata->>'channel_id') in (select id from purge_channels);

delete from audit_events where id in (select id from purge_audit_events);

delete from alert_deliveries
where rule_id in (select id from purge_alert_rules)
   or notification_channel_id in (select id from purge_notification_channels)
   or incident_id in (select id from incidents where channel_id in (select id from purge_channels));

delete from incident_events
where incident_id in (select id from incidents where channel_id in (select id from purge_channels));

delete from incidents where channel_id in (select id from purge_channels);

delete from usage_daily_rollups
where gateway_id in (select id from purge_gateways)
   or channel_id in (select id from purge_channels)
   or member_user_id in (select id from purge_users);

delete from gateway_request_events
where gateway_id in (select id from purge_gateways)
   or upstream_channel_id in (select id from purge_channels);

delete from probe_results where channel_id in (select id from purge_channels);
delete from probe_runs where channel_id in (select id from purge_channels);
delete from channel_status_snapshots
where channel_id in (select id from purge_channels)
   or metadata->>'source' = 'phase2_seed';

delete from recommend_click_events
where channel_id in (select id from purge_channels)
   or user_id in (select id from purge_users);
delete from recommend_picks where data_origin in ('demo','test') or channel_id in (select id from purge_channels);
delete from recommend_rewards where data_origin in ('demo','test') or channel_id in (select id from purge_channels);
delete from recommend_scenarios where data_origin in ('demo','test') or channel_id in (select id from purge_channels);
delete from recommend_rank_rules where id in (select id from purge_recommend_rank_rules);

delete from open_api_call_logs where site_id in (select id from purge_open_api_sites);
delete from open_api_sites where id in (select id from purge_open_api_sites);

delete from gateway_upstreams
where gateway_id in (select id from purge_gateways)
   or channel_id in (select id from purge_channels);
delete from gateway_keys
where data_origin in ('demo','test')
   or gateway_id in (select id from purge_gateways)
   or created_by in (select id from purge_users);
delete from gateways where id in (select id from purge_gateways);

delete from alert_rules where id in (select id from purge_alert_rules);
delete from notification_channels where id in (select id from purge_notification_channels);

delete from favorites
where user_id in (select id from purge_users)
   or channel_id in (select id from purge_channels);
delete from channel_credentials where channel_id in (select id from purge_channels) or owner_id in (select id from purge_users);
delete from channels where id in (select id from purge_channels);

delete from org_members
where org_id in (select id from purge_orgs)
   or user_id in (select id from purge_users);
delete from auth_sessions where user_id in (select id from purge_users);
delete from email_tokens where user_id in (select id from purge_users);
delete from orgs where id in (select id from purge_orgs);
delete from users where id in (select id from purge_users);

commit;
SQL

echo "demo/test purge completed"
"$ROOT_DIR/deploy/scripts/no-demo-data-check.sh"
