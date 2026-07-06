#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT_DIR"

PROJECT="${TOKHUB_FRESH_PROJECT:-tokhub_phase117_$(date +%s)}"
PORT="${TOKHUB_FRESH_PORT:-28117}"
ADMIN_EMAIL="${TOKHUB_ADMIN_EMAIL:-admin@tokhub.local}"
ADMIN_PASSWORD="${TOKHUB_ADMIN_PASSWORD:-Phase117FreshAdminPassword!}"
SECRET_KEY="${TOKHUB_SECRET_KEY:-phase117-fresh-secret-key-32-bytes!!}"
PUBLIC_URL="${TOKHUB_PUBLIC_URL:-http://localhost:${PORT}}"
KEEP="${TOKHUB_FRESH_KEEP:-0}"

compose() {
  COMPOSE_PROJECT_NAME="$PROJECT" \
  TOKHUB_ENV=production \
  TOKHUB_HOST_PORT="$PORT" \
  TOKHUB_PUBLIC_URL="$PUBLIC_URL" \
  TOKHUB_ADMIN_EMAIL="$ADMIN_EMAIL" \
  TOKHUB_ADMIN_PASSWORD="$ADMIN_PASSWORD" \
  TOKHUB_SECRET_KEY="$SECRET_KEY" \
  TOKHUB_SEED_MODE=prod \
  TOKHUB_UPSTREAM_MODE=real \
  TOKHUB_SESSION_SECURE=false \
  TOKHUB_REGISTRATION_OPEN=false \
  TOKHUB_EXPOSE_DEV_TOKENS=false \
  docker compose "$@"
}

cleanup() {
  local code=$?
  if [[ "$KEEP" == "1" ]]; then
    echo "keeping fresh production compose project: $PROJECT"
  else
    compose down -v --remove-orphans >/dev/null 2>&1 || true
  fi
  exit "$code"
}
trap cleanup EXIT

echo "==> fresh production compose project: $PROJECT"
echo "==> fresh production base URL: http://localhost:${PORT}"

compose down -v --remove-orphans >/dev/null 2>&1 || true
compose up -d --build app

echo
echo "==> waiting for fresh app health"
for _ in $(seq 1 60); do
  if curl -fsS "http://localhost:${PORT}/readyz" >/dev/null 2>&1; then
    echo "fresh app is ready"
    break
  fi
  sleep 2
done

curl -fsS "http://localhost:${PORT}/healthz" >/dev/null
curl -fsS "http://localhost:${PORT}/readyz" >/dev/null

echo
echo "==> no-demo data check on fresh DB"
COMPOSE_PROJECT_NAME="$PROJECT" \
TOKHUB_ENV=production \
TOKHUB_SEED_MODE=prod \
TOKHUB_UPSTREAM_MODE=real \
deploy/scripts/no-demo-data-check.sh

echo
echo "==> no-demo HTTP smoke"
TOKHUB_BASE_URL="http://localhost:${PORT}" \
TOKHUB_ADMIN_EMAIL="$ADMIN_EMAIL" \
TOKHUB_ADMIN_PASSWORD="$ADMIN_PASSWORD" \
deploy/scripts/no-demo-smoke.sh

echo
echo "==> no-demo data check after smoke"
COMPOSE_PROJECT_NAME="$PROJECT" \
TOKHUB_ENV=production \
TOKHUB_SEED_MODE=prod \
TOKHUB_UPSTREAM_MODE=real \
deploy/scripts/no-demo-data-check.sh

echo
echo "fresh production check passed"
