#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT_DIR"

VERSION_VALUE="$(tr -d '[:space:]' < VERSION)"
if [[ ! "$VERSION_VALUE" =~ ^[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z][0-9A-Za-z.-]*)?$ ]]; then
  echo "VERSION is not valid SemVer: $VERSION_VALUE" >&2
  exit 1
fi

VERSION_VALUE="$VERSION_VALUE" node <<'NODE'
const fs = require("node:fs");

const version = process.env.VERSION_VALUE;
const failures = [];

function expectJSON(file, path) {
  const value = path.reduce((current, key) => current?.[key], JSON.parse(fs.readFileSync(file, "utf8")));
  if (value !== version) failures.push(`${file}: expected ${path.join(".")}=${version}, got ${JSON.stringify(value)}`);
}

function expectMatch(file, expression, label) {
  const source = fs.readFileSync(file, "utf8");
  if (!expression.test(source)) failures.push(`${file}: missing ${label} ${version}`);
}

expectJSON("package.json", ["version"]);
expectJSON("package-lock.json", ["version"]);
expectJSON("package-lock.json", ["packages", "", "version"]);
expectJSON("agent-skills/tokhub/manifest.json", ["version"]);
expectJSON("agent-skills/tokhub/references/operation-catalog.json", ["version"]);
expectMatch("internal/buildinfo/buildinfo.go", new RegExp(`Version = "${version.replace(/[.*+?^${}()|[\]\\]/g, "\\$&")}"`), "runtime version");
expectMatch("deploy/helm/tokhub/Chart.yaml", new RegExp(`^version: ${version.replace(/[.*+?^${}()|[\]\\]/g, "\\$&")}$`, "m"), "chart version");
expectMatch("deploy/helm/tokhub/Chart.yaml", new RegExp(`^appVersion: "${version.replace(/[.*+?^${}()|[\]\\]/g, "\\$&")}"$`, "m"), "app version");
expectMatch("docs/openapi.yaml", new RegExp(`^  version: ${version.replace(/[.*+?^${}()|[\]\\]/g, "\\$&")}$`, "m"), "OpenAPI version");
expectMatch("docs/admin-agent.openapi.yaml", new RegExp(`^  version: ${version.replace(/[.*+?^${}()|[\]\\]/g, "\\$&")}$`, "m"), "admin-agent OpenAPI version");
expectMatch("docs/RELEASE.md", new RegExp(`tokhub:${version.replace(/[.*+?^${}()|[\]\\]/g, "\\$&")}`), "release image tag");
expectMatch("docs/DEPLOYMENT.md", new RegExp(`image\\.tag=${version.replace(/[.*+?^${}()|[\]\\]/g, "\\$&")}`), "Helm image tag");
expectMatch("docs/OPEN_SOURCE.md", new RegExp(`v${version.replace(/[.*+?^${}()|[\]\\]/g, "\\$&")}`), "open-source tag");
expectMatch("README.md", new RegExp(`当前版本：\`v${version.replace(/[.*+?^${}()|[\]\\]/g, "\\$&")}\``), "README version");
expectMatch("docs/README.en.md", new RegExp(`Current release: \`v${version.replace(/[.*+?^${}()|[\]\\]/g, "\\$&")}\``), "English README version");
expectMatch("CHANGELOG.md", new RegExp(`^## \\[${version.replace(/[.*+?^${}()|[\]\\]/g, "\\$&")}\\]`, "m"), "changelog release");
expectMatch("docs/tokhub-skill-architecture.html", new RegExp(`版本：${version.replace(/[.*+?^${}()|[\]\\]/g, "\\$&")}`), "skill architecture version");
if (!fs.existsSync(`docs/releases/v${version}.md`)) failures.push(`docs/releases/v${version}.md: release notes are missing`);

if (failures.length) {
  for (const failure of failures) console.error(failure);
  process.exit(1);
}

console.log(`version surfaces match ${version}`);
NODE
