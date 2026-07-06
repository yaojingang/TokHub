#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"

fail() {
  echo "frontend-ui-contract-check: $*" >&2
  exit 1
}

[[ -f web/src/ui/index.ts ]] || fail "missing UI Kit export"
[[ -f web/src/modules/registry.tsx ]] || fail "missing module registry"
grep -q 'modules.map' web/src/main.tsx || fail "routes are not derived from module registry"
grep -q 'groupedNavModules("admin")' web/src/components/AdminShell.tsx || fail "AdminShell nav is not derived from registry"
grep -q 'groupedNavModules("console")' web/src/components/ConsoleShell.tsx || fail "ConsoleShell nav is not derived from registry"
grep -q '@tanstack/react-table' web/src/ui/DataTable.tsx || fail "DataTable is not backed by TanStack Table"
grep -q '@radix-ui/react-dialog' web/src/ui/Overlays.tsx || fail "Dialog/Drawer are not backed by Radix Dialog"
grep -q '.tk-select-field' web/src/styles/app.css || fail "SelectField CSS contract missing"
grep -q 'cardClassName' web/src/ui/DataTable.tsx || fail "DataTable cardClassName contract missing"
grep -q 'meta?.width' web/src/ui/DataTable.tsx || fail "DataTable column width metadata missing"
grep -q 'tk-filter-bar' web/src/styles/app.css || fail "FilterBar CSS contract missing"
grep -q 'tk-action-bar' web/src/styles/app.css || fail "ActionBar CSS contract missing"
grep -q 'tk-status-badge' web/src/styles/app.css || fail "StatusBadge CSS contract missing"
grep -q 'tk-trend-bars' web/src/styles/app.css || fail "TrendBars CSS contract missing"
grep -q 'TrendBars' web/src/ui/index.ts || fail "UI Kit export missing: TrendBars"
if rg -n 'function TrendBars|const TrendBars' web/src/pages; then
  fail "TrendBars must be implemented in UI Kit, not page files"
fi
grep -q 'TrendBars' web/src/pages/PublicHome.tsx || fail "PublicHome trend bars must use UI Kit TrendBars"
if rg -n 'className="[^"]*page-intro[^"]*"[^>]*>[^<]*(。|\.)\s*</|intro: "[^"]*(。|\.)"' web/src/pages web/src/i18n/locales.ts; then
  fail "page intro copy should omit terminal punctuation"
fi

for export_name in Button SelectField CheckboxField SwitchField CopyButton ConfirmAction StatusBadge; do
  grep -q "$export_name" web/src/ui/index.ts || fail "UI Kit export missing: $export_name"
done

pilot_pages=(
  web/src/pages/AdminUsersPage.tsx
  web/src/pages/AdminOrgsPage.tsx
  web/src/pages/AdminMembersPage.tsx
)

for file in "${pilot_pages[@]}"; do
  grep -q 'from "../ui"' "$file" || fail "$file does not import UI Kit"
  grep -q 'DataTable' "$file" || fail "$file does not use DataTable"
  grep -q 'StatGrid' "$file" || fail "$file does not use StatGrid"
  grep -q 'FilterBar' "$file" || fail "$file does not use FilterBar"
  grep -q 'SelectField' "$file" || fail "$file does not use SelectField"
  if grep -Eq '<select|</select>' "$file"; then
    fail "$file still contains native select markup"
  fi
done

phase15_table_pages=(
  web/src/pages/AdminChannelsPage.tsx
  web/src/pages/AdminUsagePage.tsx
  web/src/pages/AuditPage.tsx
)

for file in "${phase15_table_pages[@]}"; do
  grep -q 'from "../ui"' "$file" || fail "$file does not import UI Kit"
  grep -q 'DataTable' "$file" || fail "$file does not use DataTable"
  grep -q 'StatGrid' "$file" || fail "$file does not use StatGrid"
  grep -q 'FilterBar' "$file" || fail "$file does not use FilterBar"
  grep -q 'SelectField' "$file" || fail "$file does not use SelectField"
  if grep -Eq '<select|</select>' "$file"; then
    fail "$file still contains native select markup"
  fi
  if grep -Eq '<table|</table>' "$file"; then
    fail "$file still contains native table markup"
  fi
done

phase15_select_pages=(
  web/src/pages/AlertsPage.tsx
  web/src/pages/ConsoleSettingsPage.tsx
  web/src/pages/PublicHome.tsx
)

for file in "${phase15_select_pages[@]}"; do
  grep -q 'SelectField' "$file" || fail "$file does not use SelectField"
  if grep -Eq '<select|</select>' "$file"; then
    fail "$file still contains native select markup"
  fi
done

grep -q 'DataTable' web/src/pages/AlertsPage.tsx || fail "AlertsPage does not use DataTable for migrated tables"
grep -q 'incident-table' web/src/pages/AlertsPage.tsx || fail "AlertsPage incident raw-table exemption marker missing"
grep -q '<table className="tk' web/src/pages/PublicHome.tsx || fail "PublicHome prototype-table exemption marker missing"
if grep -q 'React.StrictMode' web/src/main.tsx; then
  fail "React StrictMode is enabled in runtime entry and may reintroduce admin/console flicker"
fi

echo "frontend-ui-contract-check: ok"
