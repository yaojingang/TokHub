#!/usr/bin/env bash
set -euo pipefail

ENV_FILE=""

while [[ $# -gt 0 ]]; do
  case "$1" in
    --env-file)
      ENV_FILE="${2:-}"
      shift 2
      ;;
    *)
      echo "unknown argument: $1" >&2
      exit 2
      ;;
  esac
done

if [[ -n "$ENV_FILE" ]]; then
  if [[ ! -f "$ENV_FILE" ]]; then
    echo "env file not found: $ENV_FILE" >&2
    exit 2
  fi
  while IFS='=' read -r key value; do
    [[ -z "${key// }" || "${key:0:1}" == "#" ]] && continue
    key="$(echo "$key" | xargs)"
    if [[ -n "${!key:-}" ]]; then
      continue
    fi
    value="$(echo "$value" | sed -e 's/^[[:space:]]*//' -e 's/[[:space:]]*$//' -e 's/^"//' -e 's/"$//' -e "s/^'//" -e "s/'$//")"
    export "$key=$value"
  done < "$ENV_FILE"
fi

failures=0

fail() {
  failures=$((failures + 1))
  echo "FAIL: $*" >&2
}

warn() {
  echo "WARN: $*" >&2
}

require_env() {
  local name="$1"
  if [[ -z "${!name:-}" ]]; then
    fail "$name is required"
  fi
}

for name in TOKHUB_ENV TOKHUB_PUBLIC_URL TOKHUB_ADMIN_EMAIL TOKHUB_ADMIN_PASSWORD TOKHUB_SECRET_KEY DATABASE_URL REDIS_URL NATS_URL; do
  require_env "$name"
done

if [[ "${TOKHUB_ENV:-}" != "production" ]]; then
  fail "TOKHUB_ENV must be production"
fi

if [[ "${TOKHUB_PUBLIC_URL:-}" != https://* ]]; then
  fail "TOKHUB_PUBLIC_URL must use https in production"
fi

if [[ "${TOKHUB_PUBLIC_URL:-}" =~ localhost|127\.0\.0\.1|0\.0\.0\.0 ]]; then
  fail "TOKHUB_PUBLIC_URL must not point to a local address"
fi

secret_value="${TOKHUB_SECRET_KEY:-}"
admin_password_value="${TOKHUB_ADMIN_PASSWORD:-}"

if [[ "$secret_value" == "dev-only-change-this-secret-key-32b" || "$secret_value" == "replace-with-at-least-32-random-characters" || ${#secret_value} -lt 32 ]]; then
  fail "TOKHUB_SECRET_KEY must be a non-default secret with at least 32 characters"
fi

if [[ "$admin_password_value" == "ChangeMe123!" || "$admin_password_value" == "replace-with-a-long-random-admin-password" || ${#admin_password_value} -lt 12 ]]; then
  fail "TOKHUB_ADMIN_PASSWORD must be a non-default value with at least 12 characters"
fi

if [[ "${TOKHUB_SESSION_SECURE:-}" != "true" ]]; then
  fail "TOKHUB_SESSION_SECURE must be true in production"
fi

if [[ "${TOKHUB_EXPOSE_DEV_TOKENS:-false}" == "true" ]]; then
  fail "TOKHUB_EXPOSE_DEV_TOKENS must not be true in production"
fi

if [[ "${TOKHUB_SEED_MODE:-prod}" != "prod" ]]; then
  fail "TOKHUB_SEED_MODE must be prod in production"
fi

if [[ "${TOKHUB_UPSTREAM_MODE:-real}" == "mock" ]]; then
  fail "TOKHUB_UPSTREAM_MODE must not be mock in production"
fi

if [[ "${REQUIRE_SMTP:-0}" == "1" && -z "${SMTP_URL:-}" ]]; then
  fail "SMTP_URL is required when REQUIRE_SMTP=1"
fi

if [[ -z "${SMTP_URL:-}" ]]; then
  warn "SMTP_URL is not configured; email verification, password reset and email alert delivery will use local outbox semantics"
fi

if [[ "${REQUIRE_REAL_PROVIDER:-0}" == "1" ]]; then
  for name in TOKHUB_REAL_PROVIDER_ENDPOINT TOKHUB_REAL_PROVIDER_MODEL TOKHUB_REAL_PROVIDER_KEY; do
    if [[ -z "${!name:-}" ]]; then
      fail "$name is required when REQUIRE_REAL_PROVIDER=1"
    fi
  done
fi

if [[ "${DATABASE_URL:-}" != *sslmode=require* && "${DATABASE_URL:-}" != *sslmode=verify-full* && "${DATABASE_URL:-}" != *sslmode=verify-ca* ]]; then
  warn "DATABASE_URL does not require TLS; acceptable only for private trusted networks"
fi

if [[ "${TOKHUB_REGISTRATION_OPEN:-}" == "true" ]]; then
  warn "TOKHUB_REGISTRATION_OPEN=true; verify public registration is intentional"
fi

if [[ "$failures" -gt 0 ]]; then
  echo "preflight failed with $failures issue(s)" >&2
  exit 1
fi

echo "preflight passed"
