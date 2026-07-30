#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT_DIR"

if ! command -v rg >/dev/null 2>&1; then
  echo "security scan failed: ripgrep (rg) is required" >&2
  exit 1
fi

TMP_FILE="$(mktemp)"
TMP_EXTENSION_DIR="$(mktemp -d)"
TMP_LEGACY_EXTENSION_DIR="$(mktemp -d)"
trap 'rm -f "$TMP_FILE"; rm -rf "$TMP_EXTENSION_DIR" "$TMP_LEGACY_EXTENSION_DIR"' EXIT

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
EXTENSION_ARCHIVE="web/public/downloads/tokhub-ai-login-helper.zip"
LEGACY_EXTENSION_ARCHIVE="web/public/downloads/tokhub-deepseek-session-extension.zip"

node <<'NODE'
const assert = require("node:assert/strict");
const manifest = require("./browser-extension/tokhub-deepseek-session/manifest.json");

assert.deepEqual(manifest.permissions, ["scripting"]);
assert.deepEqual(manifest.host_permissions, [
  "https://chat.deepseek.com/*",
  "http://localhost/*"
]);
assert.deepEqual(manifest.content_scripts?.[0]?.matches, [
  "http://localhost/*",
  "http://127.0.0.1/*",
  "https://tokhub.me/*",
  "https://www.tokhub.me/*"
]);
NODE

node <<'NODE'
const assert = require("node:assert/strict");
const fs = require("node:fs");
const vm = require("node:vm");

let listener;
let tabs = [];
const sandbox = {
  URL,
  chrome: {
    runtime: {
      onMessage: {
        addListener(value) {
          listener = value;
        }
      }
    },
    tabs: {
      async query() {
        return tabs;
      }
    },
    scripting: {
      async executeScript() {
        return [];
      }
    }
  }
};
vm.runInNewContext(
  fs.readFileSync("./browser-extension/tokhub-deepseek-session/background.js", "utf8"),
  sandbox
);
assert.equal(typeof listener, "function");

function send(message, senderURL) {
  return new Promise((resolve) => {
    const keepAlive = listener(message, { tab: { url: senderURL } }, resolve);
    assert.equal(keepAlive, true);
  });
}

(async () => {
  const currentAuthorizationID = "authz_123e4567-e89b-42d3-a456-426614174000";
  tabs = [
    {
      id: 7,
      active: true,
      pendingUrl: "http://localhost:1455/auth/callback?code=stale-code&state=authz_223e4567-e89b-42d3-a456-426614174000.stale-state"
    },
    {
      id: 8,
      active: false,
      pendingUrl: `http://localhost:1455/auth/callback?code=single-use-code&state=${currentAuthorizationID}.bound-state`
    }
  ];
  const detected = await send(
    {
      type: "TOKHUB_READ_CHATGPT_CALLBACK",
      authorizationId: currentAuthorizationID
    },
    "http://localhost:28125/console/connections"
  );
  assert.equal(detected.status, "ok");
  assert.equal(
    detected.callbackUrl,
    `http://localhost:1455/auth/callback?code=single-use-code&state=${currentAuthorizationID}.bound-state`
  );

  tabs = [{
    id: 9,
    active: true,
    url: "http://localhost:1455/other?code=single-use-code&state=bound-state"
  }];
  const missing = await send(
    {
      type: "TOKHUB_READ_CHATGPT_CALLBACK",
      authorizationId: currentAuthorizationID
    },
    "http://localhost:28125/console/connections"
  );
  assert.equal(missing.status, "callback_not_found");

  const invalidAuthorization = await send(
    {
      type: "TOKHUB_READ_CHATGPT_CALLBACK",
      authorizationId: "invalid"
    },
    "http://localhost:28125/console/connections"
  );
  assert.equal(invalidAuthorization.status, "permission_denied");

  let denied;
  const keepAlive = listener(
    {
      type: "TOKHUB_READ_CHATGPT_CALLBACK",
      authorizationId: currentAuthorizationID
    },
    { tab: { url: "https://untrusted.example/console/connections" } },
    (value) => {
      denied = value;
    }
  );
  assert.equal(keepAlive, false);
  assert.equal(denied.status, "permission_denied");
})().catch((error) => {
  process.stderr.write(`${error.stack || error}\n`);
  process.exitCode = 1;
});
NODE

rg -n 'chrome\.(cookies|storage)|document\.cookie|console\.' "$EXTENSION_DIR"/*.js > "$TMP_FILE" || true
if [[ -s "$TMP_FILE" ]]; then
  echo "AI login extension uses a forbidden credential or logging API:" >&2
  cat "$TMP_FILE" >&2
  exit 1
fi

verify_extension_archive() {
  local archive="$1"
  local output_dir="$2"
  unzip -qq "$archive" -d "$output_dir"
  for file in manifest.json background.js content.js README.md; do
    if ! cmp -s "$EXTENSION_DIR/$file" "$output_dir/$file"; then
      echo "AI login extension archive is stale: $archive ($file)" >&2
      exit 1
    fi
  done
  if [[ "$(find "$output_dir" -type f | wc -l | tr -d ' ')" != "4" ]]; then
    echo "AI login extension archive contains unexpected files: $archive" >&2
    exit 1
  fi
}

verify_extension_archive "$EXTENSION_ARCHIVE" "$TMP_EXTENSION_DIR"
verify_extension_archive "$LEGACY_EXTENSION_ARCHIVE" "$TMP_LEGACY_EXTENSION_DIR"

echo "security scan passed"
