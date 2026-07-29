#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT_DIR"

TMP_FILE="$(mktemp)"
TMP_EXTENSION_DIR="$(mktemp -d)"
trap 'rm -f "$TMP_FILE"; rm -rf "$TMP_EXTENSION_DIR"' EXIT

PATTERN='(sk-[A-Za-z0-9_-]{20,}|site-th-[A-Za-z0-9_-]{16,}|BEGIN (RSA|OPENSSH|PRIVATE) KEY|password[[:space:]]*=[[:space:]]*["'\''][^"$'\''{(][^"'\'']{7,})'

rg -n --hidden --glob '!node_modules/**' --glob '!web/dist/**' --glob '!test-results/**' --glob '!playwright-report/**' --glob '!backups/**' --glob '!prototype/**' --glob '!docs/reviews/**' --glob '!tests/**' --glob '!*_test.go' --glob '!*.sum' "$PATTERN" . > "$TMP_FILE" || true

if [[ -s "$TMP_FILE" ]]; then
  echo "potential secrets found:" >&2
  cat "$TMP_FILE" >&2
  exit 1
fi

rg -n --hidden --glob '!node_modules/**' --glob '!web/dist/**' --glob '!test-results/**' --glob '!playwright-report/**' --glob '!prototype/**' 'plain_key|key_plain|api_key text|secret_key text' db/migrations internal > "$TMP_FILE" || true

if [[ -s "$TMP_FILE" ]]; then
  echo "forbidden plaintext key storage pattern found:" >&2
  cat "$TMP_FILE" >&2
  exit 1
fi

rg -n --hidden 'PlainKey[[:space:]]+string[[:space:]]+`json:"plainKey"`' internal web/src > "$TMP_FILE" || true

if [[ -s "$TMP_FILE" ]]; then
  echo "plainKey response fields must use omitempty and one-time response semantics:" >&2
  cat "$TMP_FILE" >&2
  exit 1
fi

EXTENSION_DIR="browser-extension/tokhub-deepseek-session"
EXTENSION_ARCHIVE="web/public/downloads/tokhub-deepseek-session-extension.zip"

node <<'NODE'
const assert = require("node:assert/strict");
const manifest = require("./browser-extension/tokhub-deepseek-session/manifest.json");

assert.deepEqual(manifest.permissions, ["scripting"]);
assert.deepEqual(manifest.host_permissions, ["https://chat.deepseek.com/*"]);
assert.deepEqual(manifest.content_scripts?.[0]?.matches, [
  "http://localhost/*",
  "http://127.0.0.1/*",
  "https://tokhub.me/*",
  "https://www.tokhub.me/*"
]);
NODE

rg -n 'chrome\.(cookies|storage)|document\.cookie|console\.' "$EXTENSION_DIR"/*.js > "$TMP_FILE" || true
if [[ -s "$TMP_FILE" ]]; then
  echo "DeepSeek extension uses a forbidden credential or logging API:" >&2
  cat "$TMP_FILE" >&2
  exit 1
fi

unzip -qq "$EXTENSION_ARCHIVE" -d "$TMP_EXTENSION_DIR"
for file in manifest.json background.js content.js README.md; do
  if ! cmp -s "$EXTENSION_DIR/$file" "$TMP_EXTENSION_DIR/$file"; then
    echo "DeepSeek extension archive is stale: $file" >&2
    exit 1
  fi
done
if [[ "$(find "$TMP_EXTENSION_DIR" -type f | wc -l | tr -d ' ')" != "4" ]]; then
  echo "DeepSeek extension archive contains unexpected files" >&2
  exit 1
fi

echo "security scan passed"
