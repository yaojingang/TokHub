#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT_DIR"

TMP_FILE="$(mktemp)"
trap 'rm -f "$TMP_FILE"' EXIT

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

echo "security scan passed"
