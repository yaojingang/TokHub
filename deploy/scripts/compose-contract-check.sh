#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT_DIR"

check_admin_agent_env() {
  local service="$1"
  shift
  TOKHUB_ADMIN_AGENT_ENABLED=true docker compose "$@" config --format json | node -e '
    let raw = "";
    process.stdin.setEncoding("utf8");
    process.stdin.on("data", (chunk) => { raw += chunk; });
    process.stdin.on("end", () => {
      const config = JSON.parse(raw || "{}");
      const service = process.argv[1];
      const value = config.services?.[service]?.environment?.TOKHUB_ADMIN_AGENT_ENABLED;
      if (String(value) !== "true") {
        console.error(`${service} must receive TOKHUB_ADMIN_AGENT_ENABLED=true from Compose`);
        process.exit(1);
      }
    });
  ' "$service"
}

check_admin_agent_env app
check_admin_agent_env api -f docker-compose.yml -f deploy/compose/docker-compose.roles.yml

echo "compose contracts passed"
