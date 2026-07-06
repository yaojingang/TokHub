#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
DUMP_FILE="${1:-}"
TMP_DUMP=""

cleanup_dump() {
  if [[ -n "$TMP_DUMP" ]]; then
    rm -f "$TMP_DUMP" "$TMP_DUMP.sha256"
  fi
}

if [[ -z "$DUMP_FILE" ]]; then
  DUMP_FILE="$(ls -t "$ROOT_DIR"/backups/tokhub-*.dump 2>/dev/null | head -1 || true)"
fi

if [[ -z "$DUMP_FILE" ]]; then
  mkdir -p "$ROOT_DIR/tmp"
  TMP_DUMP="$(mktemp "$ROOT_DIR/tmp/restore-drill-XXXXXX.dump")"
  "$ROOT_DIR/deploy/scripts/backup.sh" "$TMP_DUMP" >/dev/null
  DUMP_FILE="$TMP_DUMP"
fi

if [[ ! -f "$DUMP_FILE" ]]; then
  echo "usage: $0 /path/to/tokhub.dump" >&2
  cleanup_dump
  exit 2
fi

if [[ -n "${DATABASE_URL:-}" ]]; then
  echo "restore drill currently uses the docker compose db service; unset DATABASE_URL and ensure compose is running." >&2
  cleanup_dump
  exit 2
fi

SERVICE="${COMPOSE_DB_SERVICE:-db}"
DRILL_DB="${DRILL_DB:-tokhub_restore_drill_$(date -u +"%Y%m%d%H%M%S")}"

cleanup() {
  cleanup_dump
  if [[ "${KEEP_RESTORE_DRILL_DB:-}" != "1" ]]; then
    docker compose -f "$ROOT_DIR/docker-compose.yml" exec -T "$SERVICE" \
      dropdb --if-exists --username="${POSTGRES_USER:-tokhub}" "$DRILL_DB" >/dev/null 2>&1 || true
  fi
}
trap cleanup EXIT

docker compose -f "$ROOT_DIR/docker-compose.yml" exec -T "$SERVICE" \
  dropdb --if-exists --username="${POSTGRES_USER:-tokhub}" "$DRILL_DB" >/dev/null 2>&1 || true
docker compose -f "$ROOT_DIR/docker-compose.yml" exec -T "$SERVICE" \
  createdb --username="${POSTGRES_USER:-tokhub}" "$DRILL_DB"

docker compose -f "$ROOT_DIR/docker-compose.yml" exec -T "$SERVICE" \
  pg_restore --no-owner --no-acl --username="${POSTGRES_USER:-tokhub}" --dbname="$DRILL_DB" < "$DUMP_FILE"

docker compose -f "$ROOT_DIR/docker-compose.yml" exec -T "$SERVICE" \
  psql --username="${POSTGRES_USER:-tokhub}" --dbname="$DRILL_DB" -v ON_ERROR_STOP=1 -P pager=off -c "
    select 'users' as table_name, count(*) from users
    union all select 'channels', count(*) from channels
    union all select 'audit_events', count(*) from audit_events
    union all select 'gateway_request_events', count(*) from gateway_request_events
    union all select 'usage_daily_rollups', count(*) from usage_daily_rollups;
  "

echo "restore drill passed with temporary database: $DRILL_DB"
