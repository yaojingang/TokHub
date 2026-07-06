#!/usr/bin/env bash
set -euo pipefail

BASE_URL="${TOKHUB_BASE_URL:-http://localhost:${TOKHUB_HOST_PORT:-8080}}"
BASE_URL="${BASE_URL%/}"
ADMIN_EMAIL="${TOKHUB_ADMIN_EMAIL:-admin@tokhub.local}"
ADMIN_PASSWORD="${TOKHUB_ADMIN_PASSWORD:-ChangeMe123!}"
TMP_DIR="$(mktemp -d)"
trap 'rm -rf "$TMP_DIR"' EXIT

failures=0

pass() {
  echo "PASS: $*"
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

expect_contains() {
  local label="$1"
  local file="$2"
  local pattern="$3"
  if grep -q "$pattern" "$file"; then
    pass "$label"
  else
    fail "$label missing pattern: $pattern"
  fi
}

expect_not_contains() {
  local label="$1"
  local file="$2"
  local pattern="$3"
  if grep -Eiq "$pattern" "$file"; then
    fail "$label contains forbidden pattern: $pattern"
  else
    pass "$label"
  fi
}

check_get() {
  local label="$1"
  local path="$2"
  local pattern="$3"
  local output="$TMP_DIR/${label// /_}.json"
  if fetch "$path" "$output"; then
    expect_contains "$label" "$output" "$pattern"
  else
    fail "$label request failed: $path"
  fi
}

check_get_absent() {
  local label="$1"
  local path="$2"
  local pattern="$3"
  local output="$TMP_DIR/${label// /_}.json"
  if fetch "$path" "$output"; then
    expect_not_contains "$label" "$output" "$pattern"
  else
    fail "$label request failed: $path"
  fi
}

check_page() {
  local path="$1"
  local output="$TMP_DIR/page_${path//\//_}.html"
  if fetch "$path" "$output"; then
    expect_contains "page $path" "$output" "<div id=\"root\""
  else
    fail "page request failed: $path"
  fi
}

login_admin() {
  local jar="$TMP_DIR/cookies.txt"
  local csrf_json="$TMP_DIR/csrf.json"
  local login_json="$TMP_DIR/login.json"
  if ! fetch "/api/auth/csrf" "$csrf_json" -c "$jar"; then
    fail "admin csrf request failed"
    return
  fi
  local token
  token="$(sed -E 's/.*"csrfToken":"([^"]+)".*/\1/' "$csrf_json")"
  if [[ -z "$token" || "$token" == "$csrf_json" ]]; then
    fail "admin csrf token parse failed"
    return
  fi
  if curl -fsS "$BASE_URL/api/auth/login" \
    -b "$jar" -c "$jar" \
    -H "Content-Type: application/json" \
    -H "X-CSRF-Token: $token" \
    -d "{\"email\":\"$ADMIN_EMAIL\",\"password\":\"$ADMIN_PASSWORD\"}" \
    -o "$login_json"; then
    expect_contains "admin login" "$login_json" "\"role\":\"owner\""
  else
    fail "admin login request failed"
  fi
  COOKIE_JAR="$jar"
}

check_auth_get() {
  local label="$1"
  local path="$2"
  local pattern="$3"
  local jar="$4"
  local output="$TMP_DIR/${label// /_}.json"
  if fetch "$path" "$output" -b "$jar"; then
    expect_contains "$label" "$output" "$pattern"
  else
    fail "$label request failed: $path"
  fi
}

check_get "healthz" "/healthz" '"status":"ok"'
check_get "readyz" "/readyz" '"status":"ready"'
check_page "/"
check_page "/login"
check_page "/recommend"
check_page "/admin"
check_page "/console"

check_get "public overview empty" "/api/public/overview" '"total":0'
check_get "public channels empty" "/api/public/channels?page=1&pageSize=5" '"items":\[\]'
check_get "public recommend empty" "/api/public/recommend" '"picks":\[\]'
check_get "public recommend rewards empty" "/api/public/recommend" '"rewards":\[\]'
check_get "public recommend scenarios empty" "/api/public/recommend" '"scenarios":\[\]'
check_get_absent "public recommend no test rank labels" "/api/public/recommend" '"label":"[^"]*((CRUD|UI) Rank Rule|phase|load|test|mock|e2e|pilot|smoke)'
check_get "public provider rank empty" "/api/public/providers/rank" '"items":\[\]'
check_get "public errors empty" "/api/public/errors/summary" '"items":\[\]'

COOKIE_JAR=""
login_admin
if [[ -f "$COOKIE_JAR" ]]; then
  check_auth_get "admin users owner baseline" "/api/admin/users" '"owners":1' "$COOKIE_JAR"
  check_auth_get "admin users no demo" "/api/admin/users" '"demo":0' "$COOKIE_JAR"
  check_auth_get "admin users no test" "/api/admin/users" '"test":0' "$COOKIE_JAR"
  check_auth_get "admin orgs system baseline" "/api/admin/orgs" '"system":1' "$COOKIE_JAR"
  check_auth_get "admin orgs no demo" "/api/admin/orgs" '"demo":0' "$COOKIE_JAR"
  check_auth_get "admin orgs no test" "/api/admin/orgs" '"test":0' "$COOKIE_JAR"
  check_auth_get "admin recommend empty" "/api/admin/recommend" '"channels":\[\]' "$COOKIE_JAR"
  check_auth_get "admin production health seed" "/api/admin/production-health" '"id":"seed_mode".*"status":"pass"' "$COOKIE_JAR"
  check_auth_get "admin production health no demo" "/api/admin/production-health" '"id":"demo_channels".*"status":"pass"' "$COOKIE_JAR"
  check_auth_get "admin production health no demo recommend" "/api/admin/production-health" '"id":"demo_recommend".*"status":"pass"' "$COOKIE_JAR"
  check_auth_get "admin production health no test orgs" "/api/admin/production-health" '"id":"test_orgs".*"status":"pass"' "$COOKIE_JAR"
  check_auth_get "admin production health no test notifications" "/api/admin/production-health" '"id":"test_notifications".*"status":"pass"' "$COOKIE_JAR"
  check_auth_get "admin production health no test alerts" "/api/admin/production-health" '"id":"test_alerts".*"status":"pass"' "$COOKIE_JAR"
  check_auth_get "console settings baseline" "/api/console/settings" '"workspace":{' "$COOKIE_JAR"
  check_auth_get "console settings active workspace" "/api/console/settings" '"status":"active"' "$COOKIE_JAR"
  check_auth_get "console private channels empty" "/api/me/private-channels" '"items":\[\]' "$COOKIE_JAR"
fi

if [[ "$failures" -gt 0 ]]; then
  echo "no-demo smoke failed with $failures issue(s)" >&2
  exit 1
fi

echo "no-demo smoke passed for $BASE_URL"
