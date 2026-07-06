#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
BACKUP_DIR="${BACKUP_DIR:-"$ROOT_DIR/backups"}"
TIMESTAMP="$(date -u +"%Y%m%dT%H%M%SZ")"
OUT_FILE="${1:-"$BACKUP_DIR/tokhub-$TIMESTAMP.dump"}"

mkdir -p "$(dirname "$OUT_FILE")"

if [[ -n "${DATABASE_URL:-}" ]]; then
  pg_dump --format=custom --no-owner --no-acl --file="$OUT_FILE" "$DATABASE_URL"
else
  SERVICE="${COMPOSE_DB_SERVICE:-db}"
  docker compose -f "$ROOT_DIR/docker-compose.yml" exec -T "$SERVICE" \
    pg_dump --format=custom --no-owner --no-acl --username="${POSTGRES_USER:-tokhub}" --dbname="${POSTGRES_DB:-tokhub}" > "$OUT_FILE"
fi

if [[ ! -s "$OUT_FILE" ]]; then
  echo "backup failed: $OUT_FILE is empty" >&2
  exit 1
fi

sha256sum "$OUT_FILE" > "$OUT_FILE.sha256" 2>/dev/null || shasum -a 256 "$OUT_FILE" > "$OUT_FILE.sha256"
echo "backup written: $OUT_FILE"
