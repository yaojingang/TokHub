# Admin-Agent Contract

TokHub admin-agent execution uses the existing `/api/admin/*` handlers with a scoped Bearer token. It is for administrator automation only and is not the public third-party API contract.

## Environment

Required:

- `TOKHUB_BASE_URL`
- `TOKHUB_ADMIN_AGENT_TOKEN`

Optional:

- `TOKHUB_ADMIN_AGENT_REASON`
- `TOKHUB_ADMIN_AGENT_IDEMPOTENCY_KEY`

`TOKHUB_ADMIN_AGENT_ENABLED` must be enabled server-side. It defaults to enabled in development and disabled in production examples.

## Scopes

- `admin:read`: ordinary admin reads.
- `admin:write`: create, update, validate, probe, evaluate, recompute.
- `admin:dangerous`: delete, revoke, disable, reset, bulk, package build/download, and irreversible or semi-irreversible operations.
- `admin:secrets`: operations that create, rotate, return, or affect secrets.
- `admin:export`: CSV export and package download.

`admin:*` is accepted at token creation time and expands to the runtime scopes above.

## Write Guard

Every non-read request and every channel-site package download must include:

- `--execute`
- `--reason`
- `--idempotency-key`

Do not invent a reason. If the user did not provide intent for a risky operation, ask for confirmation before executing.

## Token Bootstrap Boundary

Admin-agent Bearer tokens must not call:

- `GET /api/admin/agent-tokens`
- `POST /api/admin/agent-tokens`
- `POST /api/admin/agent-tokens/{tokenID}/revoke`

Those endpoints require an owner browser session with CSRF.

For local setup, `scripts/tokhub-admin.mjs bootstrap --admin-url https://host/admin` may perform the owner browser-session-equivalent login flow by prompting for username/email and password in the terminal, then creating a short-lived admin-agent token. Do not ask the user to paste the password or resulting token into chat.

All other `/api/admin/*` routes listed in `operation-catalog.json` are intended to be executable through the admin-agent path when the token has the required scopes and guarded operations include reason plus idempotency key.

## Audit

Successful agent writes should create audit events with:

- `actorType=agent`
- `actorId=<admin_agent_token_id>`
- metadata containing `agent_token_id`, `agent_token_name`, `delegated_user_id`, `delegated_user_email`, `agent_reason`, and `idempotency_key`

After a write, run `audit-verify` by idempotency key or token id. Report audit verification as found, not found, or unavailable.

The client checks up to 500 recent audit rows by default. If audit verification is not found in a high-volume environment, rerun with both `--token-id` and `--idempotency-key` before reporting a blocker.

## Secret Handling

Never echo, log, or summarize:

- provider API keys
- gateway key plaintext
- Open API site key plaintext
- channel-site package secrets
- admin-agent token plaintext

The only exception is an owner explicitly creating a token and receiving the one-time `plainToken` response.

`scripts/tokhub-admin.mjs` redacts known plaintext key fields from JSON stdout and JSON `--output` files. Secret-bearing artifact responses, including platform channel CSV exports and channel-site packages, require explicit `--output`; treat the resulting file as key material.
