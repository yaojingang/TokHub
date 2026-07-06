#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT_DIR"

run() {
  echo
  echo "==> $*"
  "$@"
}

run_compose_config() {
  local label="$1"
  shift
  local target
  target="$(mktemp)"
  echo
  echo "==> $label"
  if ! "$@" > "$target"; then
    rm -f "$target"
    return 1
  fi
  rm -f "$target"
}

check_generated_drift() {
  echo
  echo "==> generated artifact drift"
  if ! git rev-parse --verify HEAD >/dev/null 2>&1; then
    echo "generated drift check skipped: repository has no commits yet"
    return
  fi
  local drift
  drift="$(git status --porcelain -- internal/store/db)"
  if [[ -n "$drift" ]]; then
    echo "$drift"
    git diff --stat -- internal/store/db || true
    echo "generated store files changed after sqlc generate" >&2
    exit 1
  fi
}

run_production_sample_preflight() {
  echo
  echo "==> production preflight sample"
  TOKHUB_SECRET_KEY=ci-production-sample-secret-key-32-bytes \
    TOKHUB_ADMIN_PASSWORD=ci-production-admin-password \
    deploy/scripts/preflight.sh --env-file .env.production.example
}

run go test ./...
run go vet ./...
run sqlc generate
check_generated_drift
run npm run typecheck
run npm run lint
run npm run build
run npm run test:security
run_compose_config "docker compose config" docker compose config
run_compose_config "docker compose roles config" docker compose -f docker-compose.yml -f deploy/compose/docker-compose.roles.yml config
run_production_sample_preflight

if [[ -f .env.production ]]; then
  run deploy/scripts/preflight.sh --env-file .env.production
else
  echo
  echo "==> production preflight skipped: .env.production not found"
fi

if [[ "${RUN_DB_CHECK:-0}" == "1" ]]; then
  run npm run test:ops
fi

if [[ "${RUN_RESTORE:-0}" == "1" ]]; then
  run npm run test:restore
fi

if [[ "${RUN_E2E:-0}" == "1" ]]; then
  run npm run test:e2e
fi

if [[ "${RUN_VISUAL:-0}" == "1" ]]; then
  run npm run test:visual
fi

if [[ "${RUN_SMOKE:-0}" == "1" ]]; then
  run npm run test:smoke
fi

if [[ "${RUN_REAL_PROVIDER:-0}" == "1" ]]; then
  run npm run test:real-provider
fi

if [[ "${RUN_PHASE12_EXTERNAL:-0}" == "1" ]]; then
  run npm run test:phase12-external
fi

if [[ "${RUN_NO_DEMO:-0}" == "1" || "${TOKHUB_ENV:-}" == "production" ]]; then
  run deploy/scripts/no-demo-data-check.sh
fi

if [[ "${RUN_NO_DEMO_SMOKE:-0}" == "1" ]]; then
  run npm run test:no-demo-smoke
fi

if [[ "${RUN_FRESH_PROD:-0}" == "1" ]]; then
  run npm run test:fresh-prod
fi

echo
echo "release checks passed"
