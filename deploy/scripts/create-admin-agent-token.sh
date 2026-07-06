#!/usr/bin/env bash
set -euo pipefail

BASE_URL="${TOKHUB_BASE_URL:-http://localhost:${TOKHUB_HOST_PORT:-8080}}"
BASE_URL="${BASE_URL%/}"
ADMIN_IDENTIFIER="${TOKHUB_ADMIN_IDENTIFIER:-${TOKHUB_ADMIN_EMAIL:-admin@tokhub.local}}"
ADMIN_PASSWORD="${TOKHUB_ADMIN_PASSWORD:-}"
TOKEN_NAME="${TOKHUB_ADMIN_AGENT_TOKEN_NAME:-codex-admin-agent}"
TOKEN_SCOPES="${TOKHUB_ADMIN_AGENT_TOKEN_SCOPES:-admin:*}"
TOKEN_TTL_HOURS="${TOKHUB_ADMIN_AGENT_TOKEN_TTL_HOURS:-24}"
OUTPUT_MODE="${TOKHUB_ADMIN_AGENT_TOKEN_OUTPUT:-plain}"
TMP_DIR="$(mktemp -d)"
trap 'rm -rf "$TMP_DIR"' EXIT

fail() {
  echo "FAIL: $*" >&2
  exit 1
}

json_get() {
  local path="$1"
  local key="$2"
  python3 - "$path" "$key" <<'PY'
import json
import sys

with open(sys.argv[1], "r", encoding="utf-8") as fh:
    data = json.load(fh)

value = data
for part in sys.argv[2].split("."):
    value = value[part]
print(value)
PY
}

json_body_login() {
  python3 - <<'PY'
import json
import os

print(json.dumps({
    "identifier": os.environ["ADMIN_IDENTIFIER"],
    "password": os.environ["ADMIN_PASSWORD"],
}, separators=(",", ":")))
PY
}

json_body_token() {
  python3 - <<'PY'
import json
import os

ttl = os.environ.get("TOKEN_TTL_HOURS", "").strip()
body = {
    "name": os.environ["TOKEN_NAME"],
    "scopes": [item.strip() for item in os.environ["TOKEN_SCOPES"].split(",") if item.strip()],
}
if ttl:
    body["ttlHours"] = int(ttl)
print(json.dumps(body, separators=(",", ":")))
PY
}

if [[ -z "$ADMIN_PASSWORD" ]]; then
  fail "TOKHUB_ADMIN_PASSWORD is required"
fi
if [[ "$OUTPUT_MODE" != "plain" && "$OUTPUT_MODE" != "json" ]]; then
  fail "TOKHUB_ADMIN_AGENT_TOKEN_OUTPUT must be plain or json"
fi

cookie_jar="$TMP_DIR/cookies.txt"
csrf_json="$TMP_DIR/csrf.json"
login_json="$TMP_DIR/login.json"
token_json="$TMP_DIR/token.json"

curl -fsS -c "$cookie_jar" -b "$cookie_jar" "$BASE_URL/api/auth/csrf" -o "$csrf_json" \
  || fail "could not fetch CSRF token from $BASE_URL"
csrf_token="$(json_get "$csrf_json" csrfToken)"

login_body="$(json_body_login)"
curl -fsS -c "$cookie_jar" -b "$cookie_jar" -X POST "$BASE_URL/api/auth/login" \
  -H "Content-Type: application/json" \
  -H "X-CSRF-Token: $csrf_token" \
  -d "$login_body" \
  -o "$login_json" \
  || fail "admin login failed for $ADMIN_IDENTIFIER"

token_body="$(json_body_token)"
curl -fsS -c "$cookie_jar" -b "$cookie_jar" -X POST "$BASE_URL/api/admin/agent-tokens" \
  -H "Content-Type: application/json" \
  -H "X-CSRF-Token: $csrf_token" \
  -d "$token_body" \
  -o "$token_json" \
  || fail "admin-agent token creation failed"

if [[ "$OUTPUT_MODE" == "json" ]]; then
  cat "$token_json"
  printf '\n'
else
  json_get "$token_json" token.plainToken
fi
