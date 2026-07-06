#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT_DIR"

fail() {
  echo "open-source preflight failed: $*" >&2
  exit 1
}

require_file() {
  [[ -f "$1" ]] || fail "missing required file: $1"
}

check_forbidden_path() {
  local path="$1"
  case "$path" in
    .env.example|.env.production.example)
      return 0
      ;;
    .env|.env.*)
      fail "environment file must not be published: $path"
      ;;
    backups/*|tmp/*|node_modules/*|web/dist/*|web/static/*|test-results/*|playwright-report/*|coverage/*)
      fail "generated or private directory must not be published: $path"
      ;;
    docs/reviews/*|skills/*|prototype/*)
      fail "private review, skill, or prototype asset must not be published: $path"
      ;;
    agent-skills/*/.env|agent-skills/*.env|agent-skills/*.csv|agent-skills/*.zip|agent-skills/*.tar|agent-skills/*.tar.gz)
      fail "secret-bearing skill artifact must not be published: $path"
      ;;
    *.sql)
      case "$path" in
        db/migrations/*|db/queries/*)
          return 0
          ;;
      esac
      fail "SQL dump outside db/migrations or db/queries must not be published: $path"
      ;;
    *.dump|*.sha256|*.sqlite|*.db|*.pem|*.key|*.p12|*.crt|*.log|*.DS_Store)
      fail "sensitive or generated artifact must not be published: $path"
      ;;
    tokhub|*/tokhub)
      if [[ "$path" != "cmd/tokhub/main.go" ]]; then
        fail "local binary or generated tokhub artifact must not be published: $path"
      fi
      ;;
  esac
}

require_file LICENSE
require_file NOTICE
require_file SECURITY.md
require_file CONTRIBUTING.md
require_file CODE_OF_CONDUCT.md
require_file docs/OPEN_SOURCE.md

grep -q "Apache License" LICENSE || fail "LICENSE must be Apache-2.0"
grep -q "https://www.tokhub.me/" NOTICE || fail "NOTICE must include project website"

while IFS= read -r path; do
  [[ -n "$path" ]] || continue
  check_forbidden_path "$path"
done < <({ git ls-files; git ls-files --others --exclude-standard; } | sort -u)

scan_file="$(mktemp)"
tmp_file="$(mktemp)"
trap 'rm -f "$scan_file" "$tmp_file"' EXIT

rg -n -I --hidden \
  --with-filename \
  --glob '!.git' \
  --glob '!.git/**' \
  --glob '!node_modules/**' \
  --glob '!web/dist/**' \
  --glob '!web/static/**' \
  --glob '!test-results/**' \
  --glob '!playwright-report/**' \
  --glob '!coverage/**' \
  --glob '!tmp/**' \
  --glob '!backups/**' \
  --glob '!docs/reviews/**' \
  --glob '!skills/**' \
  --glob '!prototype/**' \
  --glob '!deploy/scripts/open-source-preflight.sh' \
  'TokHub@[0-9A-Za-z!]+|Phase121TrialAdminPassword|(admin|user)@tokhub\.run|/Users/laoyao|/tmp/tokhub|192\.168\.|核心API资料|BEGIN (RSA|OPENSSH|PRIVATE) KEY' \
  . > "$scan_file" || true

while IFS= read -r match; do
  case "$match" in
    *'internal/api/channel_intro_fetch.go:'*'netip.MustParsePrefix("192.168.0.0/16")'*)
      continue
      ;;
    *'internal/api/channel_intro_fetch_test.go:'*'10.0.0.8", "192.168.1.20"'*)
      continue
      ;;
  esac
  echo "$match" >> "$tmp_file"
done < "$scan_file"

if [[ -s "$tmp_file" ]]; then
  echo "potential internal or secret-bearing content found:" >&2
  cat "$tmp_file" >&2
  exit 1
fi

npm run test:security

echo "open-source preflight passed"
