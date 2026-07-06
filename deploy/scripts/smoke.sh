#!/usr/bin/env bash
set -euo pipefail

BASE_URL="${TOKHUB_BASE_URL:-http://localhost:${TOKHUB_HOST_PORT:-8080}}"
BASE_URL="${BASE_URL%/}"
TMP_DIR="$(mktemp -d)"
trap 'rm -rf "$TMP_DIR"' EXIT

failures=0

pass() {
  echo "PASS: $*"
}

skip() {
  echo "SKIP: $*"
}

fail() {
  failures=$((failures + 1))
  echo "FAIL: $*" >&2
}

fetch() {
  local path="$1"
  local output="$2"
  shift 2
  curl -fsS "$@" "$BASE_URL$path" -o "$output"
}

check_contains() {
  local label="$1"
  local path="$2"
  local pattern="$3"
  local output="$TMP_DIR/${label// /_}.out"
  if fetch "$path" "$output"; then
    if grep -q "$pattern" "$output"; then
      pass "$label"
    else
      fail "$label did not contain expected marker: $pattern"
    fi
  else
    fail "$label request failed: $path"
  fi
}

check_contains "healthz" "/healthz" '"status":"ok"'
check_contains "readyz" "/readyz" '"status":"ready"'
check_contains "openapi spec" "/openapi.yaml" "openapi: 3.1.0"
check_contains "public site config" "/api/public/site-config" "brandName"
check_contains "public overview" "/api/public/overview" "healthy"

output="$TMP_DIR/recommend-click.out"
if curl -fsS -X POST "$BASE_URL/api/public/recommend/click" \
  -H "Content-Type: application/json" \
  -d '{"itemType":"cta","itemId":"smoke"}' \
  -o "$output"; then
  if grep -q '"status":"tracked"' "$output"; then
    pass "public recommend click"
  else
    fail "public recommend click did not contain expected marker"
  fi
else
  fail "public recommend click request failed"
fi

if [[ -n "${TOKHUB_SITE_KEY:-}" ]]; then
  output="$TMP_DIR/openapi-overview.out"
  if fetch "/v1/status/overview" "$output" -H "X-Site-Key: ${TOKHUB_SITE_KEY}"; then
    if grep -q "healthy" "$output"; then
      pass "status Open API overview"
    else
      fail "status Open API overview did not contain expected marker"
    fi
  else
    fail "status Open API overview request failed"
  fi
else
  skip "status Open API overview requires TOKHUB_SITE_KEY"
fi

if [[ -n "${TOKHUB_GATEWAY_KEY:-}" ]]; then
  output="$TMP_DIR/gateway-models.out"
  if fetch "/gateway/v1/models" "$output" -H "Authorization: Bearer ${TOKHUB_GATEWAY_KEY}"; then
    if grep -q '"object"' "$output"; then
      pass "gateway models"
    else
      fail "gateway models did not contain expected marker"
    fi
  else
    fail "gateway models request failed"
  fi
else
  skip "gateway models requires TOKHUB_GATEWAY_KEY"
fi

if [[ "$failures" -gt 0 ]]; then
  echo "smoke checks failed with $failures issue(s)" >&2
  exit 1
fi

echo "smoke checks passed for $BASE_URL"
