#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
SQL="
do \$\$
begin
  if not exists(select 1 from schema_migrations where version='0010_phase8_release_hardening.sql') then
    raise exception 'phase 8 migration is not applied';
  end if;
  if (select count(*) from channels where owner_type='platform') < 14 then
    raise exception 'expected at least 14 platform channels';
  end if;
  if (select count(*) from pg_indexes where indexname in ('idx_gateway_events_model_time','idx_gateway_events_status_time')) < 2 then
    raise exception 'gateway release indexes are missing';
  end if;
  if (select count(*) from pg_indexes where indexname in ('idx_audit_events_action_time','idx_audit_events_object_time')) < 2 then
    raise exception 'audit release indexes are missing';
  end if;
end \$\$;

select 'phase8_migration_applied' as check_name, count(*)::text as value from schema_migrations where version='0010_phase8_release_hardening.sql'
union all
select 'public_channels', count(*)::text from channels where owner_type='platform'
union all
select 'gateway_events_indexes', count(*)::text from pg_indexes where indexname in ('idx_gateway_events_model_time','idx_gateway_events_status_time')
union all
select 'audit_indexes', count(*)::text from pg_indexes where indexname in ('idx_audit_events_action_time','idx_audit_events_object_time')
union all
select 'slow_query_extension', case when exists(select 1 from pg_extension where extname='pg_stat_statements') then 'enabled' else 'not_enabled' end;
"

if [[ -n "${DATABASE_URL:-}" ]]; then
  psql "$DATABASE_URL" -v ON_ERROR_STOP=1 -P pager=off -c "$SQL"
else
  SERVICE="${COMPOSE_DB_SERVICE:-db}"
  docker compose -f "$ROOT_DIR/docker-compose.yml" exec -T "$SERVICE" \
    psql --username="${POSTGRES_USER:-tokhub}" --dbname="${POSTGRES_DB:-tokhub}" -v ON_ERROR_STOP=1 -P pager=off -c "$SQL"
fi
