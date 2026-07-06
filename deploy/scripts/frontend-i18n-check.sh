#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"

fail() {
  echo "frontend-i18n-check: $*" >&2
  exit 1
}

[[ -f web/src/i18n/index.ts ]] || fail "missing web/src/i18n/index.ts"
[[ -f web/src/i18n/locales.ts ]] || fail "missing web/src/i18n/locales.ts"

grep -q '"zh-CN"' web/src/i18n/locales.ts || fail "zh-CN locale not registered"
grep -q '"en-US"' web/src/i18n/locales.ts || fail "en-US locale not registered"
for namespace in common admin console public; do
  grep -q "${namespace}:" web/src/i18n/locales.ts || fail "$namespace namespace not registered"
done
grep -q 'usage: "用量数据"' web/src/i18n/locales.ts || fail "admin/console usage label was not normalized to 用量数据"
grep -q 'settings: "系统设置"' web/src/i18n/locales.ts || fail "admin settings label was not normalized to 系统设置"
grep -q 'lookupQuerystring: "lng"' web/src/i18n/index.ts || fail "URL lng detector is not configured"
grep -q 'lookupLocalStorage: "tokhub.lng"' web/src/i18n/index.ts || fail "tokhub.lng localStorage detector is not configured"
grep -q 'I18nextProvider' web/src/main.tsx || fail "React tree is not wrapped by I18nextProvider"
grep -q 'from "./i18n"' web/src/main.tsx || fail "main.tsx does not import i18n"

for file in web/src/modules/registry.tsx web/src/ui/*.tsx; do
  if grep -Eq '[一-龥]' "$file"; then
    fail "foundation file contains hardcoded Chinese: $file"
  fi
done

echo "frontend-i18n-check: ok"
