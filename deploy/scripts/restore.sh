#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
DUMP_FILE="${1:-}"

if [[ -z "$DUMP_FILE" || ! -f "$DUMP_FILE" ]]; then
  echo "usage: $0 /path/to/tokhub.dump" >&2
  exit 2
fi

if [[ "${TOKHUB_RESTORE_CONFIRM:-}" != "restore" ]]; then
  echo "refusing to restore without TOKHUB_RESTORE_CONFIRM=restore" >&2
  echo "restore is destructive for existing rows; run after taking a fresh backup." >&2
  exit 3
fi

if [[ -n "${DATABASE_URL:-}" ]]; then
  pg_restore --clean --if-exists --no-owner --no-acl --dbname="$DATABASE_URL" "$DUMP_FILE"
else
  SERVICE="${COMPOSE_DB_SERVICE:-db}"
  docker compose -f "$ROOT_DIR/docker-compose.yml" exec -T "$SERVICE" \
    pg_restore --clean --if-exists --no-owner --no-acl --username="${POSTGRES_USER:-tokhub}" --dbname="${POSTGRES_DB:-tokhub}" < "$DUMP_FILE"
fi

echo "restore completed from: $DUMP_FILE"
