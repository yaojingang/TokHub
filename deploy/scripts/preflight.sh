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

validate_credential_keyring() {
  local active_id="$1"
  local raw_keys="$2"
  local label="$3"
  local found=0
  local pair key_id key_secret
  local -a pairs=()
  IFS=',' read -ra pairs <<< "$raw_keys"
  for pair in "${pairs[@]}"; do
    key_id="${pair%%:*}"
    key_secret="${pair#*:}"
    if [[ "$pair" != *:* || -z "$key_id" || ! "$key_id" =~ ^[A-Za-z0-9._-]+$ ]]; then
      fail "$label contains an invalid key-id:secret entry"
      continue
    fi
    if [[ ${#key_secret} -lt 32 || "$key_secret" == replace-with-* ]]; then
      fail "$label key $key_id must use a non-placeholder secret with at least 32 characters"
    fi
    if [[ "$key_id" == "$active_id" ]]; then
      found=1
    fi
  done
  if [[ -z "$active_id" || "$found" -ne 1 ]]; then
    fail "$label must contain its configured active key id"
  fi
}

reject_shared_credential_key_material() {
  local encryption_keys="$1"
  local fingerprint_keys="$2"
  local encryption_pair fingerprint_pair encryption_secret fingerprint_secret
  local -a encryption_pairs=()
  local -a fingerprint_pairs=()
  IFS=',' read -ra encryption_pairs <<< "$encryption_keys"
  IFS=',' read -ra fingerprint_pairs <<< "$fingerprint_keys"
  for encryption_pair in "${encryption_pairs[@]}"; do
    encryption_secret="${encryption_pair#*:}"
    for fingerprint_pair in "${fingerprint_pairs[@]}"; do
      fingerprint_secret="${fingerprint_pair#*:}"
      if [[ -n "$encryption_secret" && "$encryption_secret" == "$fingerprint_secret" ]]; then
        fail "credential encryption and fingerprint keyrings must use different secret material"
        return
      fi
    done
  done
}

if [[ -z "${TOKHUB_CREDENTIAL_ENCRYPTION_KEYS:-}" && -z "${TOKHUB_CREDENTIAL_FINGERPRINT_KEYS:-}" ]]; then
  fail "dedicated credential encryption and fingerprint keyrings are required in production"
elif [[ -z "${TOKHUB_CREDENTIAL_ENCRYPTION_KEYS:-}" || -z "${TOKHUB_CREDENTIAL_FINGERPRINT_KEYS:-}" ]]; then
  fail "both credential encryption and fingerprint keyrings must be configured together"
else
  validate_credential_keyring "${TOKHUB_CREDENTIAL_ACTIVE_KEY_ID:-}" "${TOKHUB_CREDENTIAL_ENCRYPTION_KEYS}" "TOKHUB_CREDENTIAL_ENCRYPTION_KEYS"
  validate_credential_keyring "${TOKHUB_CREDENTIAL_ACTIVE_FINGERPRINT_KEY_ID:-}" "${TOKHUB_CREDENTIAL_FINGERPRINT_KEYS}" "TOKHUB_CREDENTIAL_FINGERPRINT_KEYS"
  reject_shared_credential_key_material "${TOKHUB_CREDENTIAL_ENCRYPTION_KEYS}" "${TOKHUB_CREDENTIAL_FINGERPRINT_KEYS}"
fi

if [[ "$admin_password_value" == "admin@tokhub.local" || "$admin_password_value" == "ChangeMe123!" || "$admin_password_value" == "replace-with-a-long-random-admin-password" || ${#admin_password_value} -lt 12 ]]; then
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

if [[ "${TOKHUB_AI_WEB_AUTH_ENABLED:-false}" == "true" ]]; then
  if [[ "${TOKHUB_AI_GEMINI_OAUTH_ENABLED:-false}" == "true" ]]; then
    require_env TOKHUB_GOOGLE_OAUTH_CLIENT_ID
    require_env TOKHUB_GOOGLE_OAUTH_CLIENT_SECRET
    if [[ -n "${TOKHUB_GOOGLE_OAUTH_PROJECT_ID:-}" &&
      ! "${TOKHUB_GOOGLE_OAUTH_PROJECT_ID}" =~ ^[a-z][a-z0-9-]{4,28}[a-z0-9]$ ]]; then
      fail "TOKHUB_GOOGLE_OAUTH_PROJECT_ID must be a valid 6-30 character Google Cloud project ID"
    fi
  fi
  if [[ "${TOKHUB_AI_CHATGPT_CODEX_EXPERIMENTAL:-false}" == "true" &&
    "${TOKHUB_AI_EXPERIMENTAL_BRIDGE_ACK:-}" != "I_ACCEPT_CHATGPT_CODEX_EXPERIMENTAL_RISK" ]]; then
    fail "ChatGPT Codex experimental mode requires the exact risk acknowledgement"
  fi
  if [[ "${TOKHUB_AI_DEEPSEEK_WEB_EXPERIMENTAL:-false}" == "true" ]]; then
    require_env TOKHUB_AI_DEEPSEEK_WEB_BRIDGE_URL
    require_env TOKHUB_DEEPSEEK_WEB_BRIDGE_ADMIN_KEY
    if [[ "${TOKHUB_AI_DEEPSEEK_WEB_ACK:-}" != "I_ACCEPT_DEEPSEEK_WEB_SESSION_EXPERIMENTAL_RISK" ]]; then
      fail "DeepSeek web-session experimental mode requires the exact risk acknowledgement"
    fi
    if [[ "${TOKHUB_DEEPSEEK_WEB_BRIDGE_ADMIN_KEY:-}" == "replace-with-a-long-random-bridge-admin-key" ||
      ${#TOKHUB_DEEPSEEK_WEB_BRIDGE_ADMIN_KEY} -lt 24 ]]; then
      fail "TOKHUB_DEEPSEEK_WEB_BRIDGE_ADMIN_KEY must be a non-default value with at least 24 characters"
    fi
    if [[ ! "${TOKHUB_AI_DEEPSEEK_WEB_BRIDGE_URL}" =~ ^https://[A-Za-z0-9][A-Za-z0-9.-]*(:[0-9]+)?/?$ &&
      ! "${TOKHUB_AI_DEEPSEEK_WEB_BRIDGE_URL}" =~ ^http://[A-Za-z0-9][A-Za-z0-9-]*(:[0-9]+)?/?$ ]]; then
      fail "DeepSeek web bridge must use HTTPS or a single-label internal HTTP service name"
    fi
  fi
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
