#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT_DIR"

PROVIDER_NAME="${TOKHUB_REAL_PROVIDER_NAME:-OpenAI}"
PROVIDER_TYPE="${TOKHUB_REAL_PROVIDER_TYPE:-openai-compatible}"
PROVIDER_ENDPOINT="${TOKHUB_REAL_PROVIDER_ENDPOINT:-}"
PROVIDER_MODEL="${TOKHUB_REAL_PROVIDER_MODEL:-}"
PROVIDER_KEY="${TOKHUB_REAL_PROVIDER_KEY:-}"
BASE_URL="${TOKHUB_BASE_URL:-http://localhost:${TOKHUB_HOST_PORT:-8080}}"
BASE_URL="${BASE_URL%/}"
ADMIN_EMAIL="${TOKHUB_ADMIN_EMAIL:-admin@tokhub.local}"
ADMIN_PASSWORD="${TOKHUB_ADMIN_PASSWORD:-ChangeMe123!}"
INPUT_PRICE="${TOKHUB_REAL_PROVIDER_INPUT_PER_MTOK:-0}"
OUTPUT_PRICE="${TOKHUB_REAL_PROVIDER_OUTPUT_PER_MTOK:-0}"
PUBLIC_VISIBLE="${TOKHUB_REAL_PROVIDER_PUBLIC_VISIBLE:-false}"
TMP_DIR="$(mktemp -d)"
trap 'rm -rf "$TMP_DIR"' EXIT

skip() {
  echo "SKIP: $*"
}

fail() {
  echo "FAIL: $*" >&2
  exit 1
}

pass() {
  echo "PASS: $*"
}

lowercase() {
  printf '%s' "$1" | tr '[:upper:]' '[:lower:]'
}

if [[ -z "$PROVIDER_ENDPOINT" || -z "$PROVIDER_MODEL" || -z "$PROVIDER_KEY" ]]; then
  if [[ "${REQUIRE_REAL_PROVIDER:-0}" == "1" ]]; then
    fail "TOKHUB_REAL_PROVIDER_ENDPOINT, TOKHUB_REAL_PROVIDER_MODEL and TOKHUB_REAL_PROVIDER_KEY are required"
  fi
  skip "real provider check requires TOKHUB_REAL_PROVIDER_ENDPOINT, TOKHUB_REAL_PROVIDER_MODEL and TOKHUB_REAL_PROVIDER_KEY"
  exit 0
fi

PROVIDER_ENDPOINT="${PROVIDER_ENDPOINT%/}"

json_path() {
  node -e '
const fs = require("fs");
let data = JSON.parse(fs.readFileSync(process.argv[1], "utf8"));
for (const key of process.argv[2].split(".")) data = data?.[key];
if (data === undefined || data === null) process.exit(1);
if (typeof data === "object") console.log(JSON.stringify(data));
else console.log(String(data));
' "$1" "$2"
}

provider_curl() {
  local method="$1"
  local path="$2"
  local output="$3"
  local body="${4:-}"
  local clean_path="${path#/}"
  local lower_endpoint
  lower_endpoint="$(lowercase "$PROVIDER_ENDPOINT")"
  local url
  if [[ "$lower_endpoint" == */v1 || "$lower_endpoint" == */v1beta ]]; then
    url="$PROVIDER_ENDPOINT/$clean_path"
  elif [[ "$lower_endpoint" == */anthropic ]]; then
    url="$PROVIDER_ENDPOINT/v1/$clean_path"
  elif [[ "$lower_endpoint" == */openai ]]; then
    url="$PROVIDER_ENDPOINT/v1/$clean_path"
  elif [[ "$PROVIDER_ENDPOINT" == *"://"*"/"* && "$PROVIDER_ENDPOINT" != "${PROVIDER_ENDPOINT%%://*}://"*"/" ]]; then
    url="$PROVIDER_ENDPOINT/$clean_path"
  else
    url="$PROVIDER_ENDPOINT/v1/$clean_path"
  fi
  local args=(-fsS -X "$method" "$url" -H "Content-Type: application/json" -o "$output")
  case "$(lowercase "$PROVIDER_TYPE")" in
    anthropic*)
      args+=(-H "X-API-Key: $PROVIDER_KEY" -H "Anthropic-Version: 2023-06-01")
      ;;
    gemini*|google*)
      args+=(-H "X-Goog-Api-Key: $PROVIDER_KEY")
      ;;
    *)
      args+=(-H "Authorization: Bearer $PROVIDER_KEY")
      ;;
  esac
  if [[ -n "$body" ]]; then
    args+=(-d @"$body")
  fi
  curl "${args[@]}"
}

echo "==> direct provider models"
models_json="$TMP_DIR/provider-models.json"
provider_curl GET "/models" "$models_json"
node -e 'JSON.parse(require("fs").readFileSync(process.argv[1], "utf8"))' "$models_json"
pass "provider models endpoint returned JSON"

echo
echo "==> direct provider minimal generation"
request_json="$TMP_DIR/provider-generation-request.json"
case "$(lowercase "$PROVIDER_TYPE")" in
  anthropic*)
    TOKHUB_REAL_PROVIDER_MODEL="$PROVIDER_MODEL" node > "$request_json" <<'NODE'
process.stdout.write(JSON.stringify({
  model: process.env.TOKHUB_REAL_PROVIDER_MODEL,
  max_tokens: 8,
  temperature: 0,
  messages: [{ role: "user", content: "Reply exactly: OK" }]
}));
NODE
    generation_path="/messages"
    ;;
  gemini*|google*)
    node > "$request_json" <<'NODE'
process.stdout.write(JSON.stringify({
  contents: [{ role: "user", parts: [{ text: "Reply exactly: OK" }] }],
  generationConfig: { maxOutputTokens: 8, temperature: 0 }
}));
NODE
    generation_path="/models/${PROVIDER_MODEL}:generateContent"
    ;;
  *)
    TOKHUB_REAL_PROVIDER_MODEL="$PROVIDER_MODEL" node > "$request_json" <<'NODE'
process.stdout.write(JSON.stringify({
  model: process.env.TOKHUB_REAL_PROVIDER_MODEL,
  max_tokens: 8,
  temperature: 0,
  messages: [{ role: "user", content: "Reply exactly: OK" }]
}));
NODE
    generation_path="/chat/completions"
    ;;
esac
generation_json="$TMP_DIR/provider-generation.json"
provider_curl POST "$generation_path" "$generation_json" "$request_json"
node -e 'JSON.parse(require("fs").readFileSync(process.argv[1], "utf8"))' "$generation_json"
pass "provider minimal generation returned JSON"

if ! curl -fsS "$BASE_URL/healthz" -o "$TMP_DIR/healthz.json"; then
  if [[ "${RUN_REAL_PROVIDER_TOKHUB:-0}" == "1" ]]; then
    fail "TokHub service is not reachable at $BASE_URL"
  fi
  skip "TokHub service is not reachable at $BASE_URL; direct provider checks passed"
  exit 0
fi

csrf_token() {
  local output="$TMP_DIR/csrf.json"
  curl -fsS "$BASE_URL/api/auth/csrf" -b "$COOKIE_JAR" -c "$COOKIE_JAR" -o "$output"
  json_path "$output" "csrfToken"
}

api_post() {
  local path="$1"
  local input="$2"
  local output="$3"
  local token
  token="$(csrf_token)"
  curl -fsS "$BASE_URL$path" \
    -b "$COOKIE_JAR" -c "$COOKIE_JAR" \
    -H "Content-Type: application/json" \
    -H "X-CSRF-Token: $token" \
    -d @"$input" \
    -o "$output"
}

echo
echo "==> TokHub real provider gateway loop"
COOKIE_JAR="$TMP_DIR/cookies.txt"
login_json="$TMP_DIR/login.json"
login_payload="$TMP_DIR/login.json.req"
TOKHUB_ADMIN_EMAIL="$ADMIN_EMAIL" TOKHUB_ADMIN_PASSWORD="$ADMIN_PASSWORD" node > "$login_payload" <<'NODE'
process.stdout.write(JSON.stringify({ email: process.env.TOKHUB_ADMIN_EMAIL, password: process.env.TOKHUB_ADMIN_PASSWORD }));
NODE
api_post "/api/auth/login" "$login_payload" "$login_json"
json_path "$login_json" "user.role" >/dev/null
pass "admin login"

channel_payload="$TMP_DIR/channel.json.req"
TOKHUB_REAL_PROVIDER_KEY="$PROVIDER_KEY" \
TOKHUB_REAL_PROVIDER_ENDPOINT="$PROVIDER_ENDPOINT" \
TOKHUB_REAL_PROVIDER_MODEL="$PROVIDER_MODEL" \
TOKHUB_REAL_PROVIDER_NAME="$PROVIDER_NAME" \
TOKHUB_REAL_PROVIDER_TYPE="$PROVIDER_TYPE" \
TOKHUB_REAL_PROVIDER_INPUT_PER_MTOK="$INPUT_PRICE" \
TOKHUB_REAL_PROVIDER_OUTPUT_PER_MTOK="$OUTPUT_PRICE" \
TOKHUB_REAL_PROVIDER_PUBLIC_VISIBLE="$PUBLIC_VISIBLE" \
node > "$channel_payload" <<'NODE'
process.stdout.write(JSON.stringify({
  name: `RC Real Provider ${new Date().toISOString()}`,
  provider: process.env.TOKHUB_REAL_PROVIDER_NAME,
  type: process.env.TOKHUB_REAL_PROVIDER_TYPE,
  model: process.env.TOKHUB_REAL_PROVIDER_MODEL,
  upstreamModel: process.env.TOKHUB_REAL_PROVIDER_MODEL,
  endpoint: process.env.TOKHUB_REAL_PROVIDER_ENDPOINT,
  apiKey: process.env.TOKHUB_REAL_PROVIDER_KEY,
  probeDaily: 60,
  publicVisible: String(process.env.TOKHUB_REAL_PROVIDER_PUBLIC_VISIBLE || "").toLowerCase() === "true",
  gatewayEnabled: true,
  enabled: true,
  inputPerMtok: Number(process.env.TOKHUB_REAL_PROVIDER_INPUT_PER_MTOK || 0),
  outputPerMtok: Number(process.env.TOKHUB_REAL_PROVIDER_OUTPUT_PER_MTOK || 0),
  providerConfig: { temperature: 0, maxTokens: 8, timeoutMs: 60000 }
}));
NODE
channel_json="$TMP_DIR/channel.json"
api_post "/api/admin/channels" "$channel_payload" "$channel_json"
channel_id="$(json_path "$channel_json" "channel.id")"
pass "created platform channel"

validate_json="$TMP_DIR/channel-validate.json"
api_post "/api/admin/channels/${channel_id}/validate" /dev/null "$validate_json"
json_path "$validate_json" "channel.id" >/dev/null
pass "platform channel validation probe"

probe_json="$TMP_DIR/channel-probe-now.json"
api_post "/api/admin/channels/${channel_id}/probe-now" /dev/null "$probe_json"
json_path "$probe_json" "channel.id" >/dev/null
pass "platform channel probe-now"

gateway_payload="$TMP_DIR/gateway.json.req"
TOKHUB_RC_CHANNEL_ID="$channel_id" node > "$gateway_payload" <<'NODE'
process.stdout.write(JSON.stringify({
  name: `RC Real Gateway ${new Date().toISOString()}`,
  policy: "latency",
  upstreamIds: [process.env.TOKHUB_RC_CHANNEL_ID],
  qpsLimit: 20,
  quotaMonth: 1000
}));
NODE
gateway_json="$TMP_DIR/gateway.json"
api_post "/api/admin/gateways" "$gateway_payload" "$gateway_json"
gateway_id="$(json_path "$gateway_json" "gateway.id")"
gateway_name="$(json_path "$gateway_json" "gateway.name")"
pass "created gateway"

key_payload="$TMP_DIR/key.json.req"
TOKHUB_RC_GATEWAY_ID="$gateway_id" node > "$key_payload" <<'NODE'
process.stdout.write(JSON.stringify({
  gatewayId: process.env.TOKHUB_RC_GATEWAY_ID,
  name: `RC Real Gateway Key ${new Date().toISOString()}`,
  quotaMonth: 1000,
  qpsLimit: 20
}));
NODE
key_json="$TMP_DIR/key.json"
api_post "/api/admin/gateway-keys" "$key_payload" "$key_json"
gateway_key="$(json_path "$key_json" "key.plainKey")"
pass "created one-time gateway key"

gateway_models="$TMP_DIR/gateway-models.json"
curl -fsS "$BASE_URL/gateway/v1/models" \
  -H "Authorization: Bearer $gateway_key" \
  -o "$gateway_models"
node -e 'JSON.parse(require("fs").readFileSync(process.argv[1], "utf8"))' "$gateway_models"
pass "gateway models passthrough"

gateway_request="$TMP_DIR/gateway-generation.json.req"
TOKHUB_REAL_PROVIDER_MODEL="$PROVIDER_MODEL" node > "$gateway_request" <<'NODE'
process.stdout.write(JSON.stringify({
  model: process.env.TOKHUB_REAL_PROVIDER_MODEL,
  messages: [{ role: "user", content: "Reply exactly: OK" }]
}));
NODE
gateway_generation="$TMP_DIR/gateway-generation.json"
curl -fsS "$BASE_URL/gateway/v1/chat/completions" \
  -H "Authorization: Bearer $gateway_key" \
  -H "Content-Type: application/json" \
  -d @"$gateway_request" \
  -o "$gateway_generation"
node -e 'JSON.parse(require("fs").readFileSync(process.argv[1], "utf8"))' "$gateway_generation"
pass "gateway minimal generation passthrough"

usage_json="$TMP_DIR/usage.json"
curl -fsS "$BASE_URL/api/admin/usage" -b "$COOKIE_JAR" -o "$usage_json"
TOKHUB_RC_GATEWAY_NAME="$gateway_name" REQUIRE_REAL_PROVIDER_COST="${REQUIRE_REAL_PROVIDER_COST:-0}" node -e '
const fs = require("fs");
const payload = JSON.parse(fs.readFileSync(process.argv[1], "utf8"));
const gateway = process.env.TOKHUB_RC_GATEWAY_NAME;
const event = (payload.recent || []).find((item) => item.gateway === gateway && item.statusCode < 400 && item.tokens > 0);
if (!event) throw new Error("recent usage event not found");
if (process.env.REQUIRE_REAL_PROVIDER_COST === "1" && !(event.costUsd > 0)) throw new Error("usage cost was not recorded");
' "$usage_json"
pass "usage tokens and cost loop recorded"

echo
echo "real provider release-candidate checks passed"
