---
name: tokhub-admin
description: Operate TokHub administrator workflows through the internal admin-agent API, scoped Bearer token, deterministic client script, idempotency guard, and audit verification. Use when an admin asks a skill or agent to inspect production health, manage platform channels, gateways, keys, users, orgs, recommendations, Open API sites, alerts, incidents, audit logs, usage, or admin exports in TokHub. Do not use for ordinary user console workflows, public status API integration, gateway model calls, TokHub code development, or one-off documentation without admin execution.
---

# TokHub Admin

## When To Use

- The user asks for TokHub administrator operations through an agent or skill.
- The task needs `/api/admin/*` reads, probes, validation, changes, exports, or audit checks.
- The work must be executed through `TOKHUB_ADMIN_AGENT_TOKEN`, not a browser session.
- The user needs to connect a local Codex/AI session to a remote TokHub admin URL by entering the target admin URL, username/email, and password.
- The request is to manage the TokHub admin backend directly from Codex/AI. The supported surface is every `/api/admin/*` route listed in `references/operation-catalog.json`, excluding admin-agent token lifecycle bootstrap.

## Do Not Use

- Ordinary `/api/console/*` user workspace workflows.
- Public `/v1/status/*` or `/api/public/*` integration.
- Gateway model invocation under `/gateway/v1/*`.
- TokHub code editing, debugging, or frontend/admin UI development.
- Creating, listing, or revoking admin-agent tokens with a bearer token. Token bootstrap remains an owner browser-session flow.

## Workflow

1. Read `references/admin-agent-contract.md` before any write, export, secret-bearing, or dangerous operation.
2. Inspect `references/operation-catalog.json` to confirm the path, method, scopes, `risk` value, and write guard. This catalog is the route coverage contract for the admin backend.
3. If the local session does not have a token, run the bundled client script, for example `node agent-skills/tokhub-admin/scripts/tokhub-admin.mjs bootstrap --admin-url https://host/admin --save-env ~/.tokhub-admin.env`, and let the user enter username/email plus password in the terminal.
4. Source the saved env file or otherwise provide `TOKHUB_BASE_URL` and `TOKHUB_ADMIN_AGENT_TOKEN`, then run `node agent-skills/tokhub-admin/scripts/tokhub-admin.mjs preflight`.
5. For reads, run `node agent-skills/tokhub-admin/scripts/tokhub-admin.mjs request GET /api/admin/...`.
6. For writes, exports, downloads, deletes, bulk, reset, revoke, disable, credential, import, sync, package build/download, or key actions, require explicit user intent, then pass `--execute --reason "..." --idempotency-key "..."`.
7. After any write, run `audit-verify` with the idempotency key or token id and report whether audit metadata was found.
8. Use JSON bodies with `--json` or `--body`; use multipart file upload with `--form-file file=path.csv` for `/api/admin/channels/import`.
9. Treat script JSON output as redacted by default. Never print plain keys, provider credentials, site keys, gateway keys, or admin-agent tokens outside the bootstrap one-time token handoff. Secret-bearing artifact endpoints such as `/api/admin/channels/export` and `/api/admin/channel-sites/{siteID}/download` must use explicit `--output` and the output path must be treated as key material.

## Output Contract

Return:

- operation performed and target path
- scopes and `risk` value used from the catalog
- execution status and important response fields
- audit verification result for writes
- any blocked precondition, missing env var, missing scope, or refusal reason

## Reference Map

- `references/admin-agent-contract.md`: auth, scopes, idempotency, audit, and dangerous action rules.
- `references/operation-catalog.json`: complete non-token `/api/admin` operation catalog, scope mapping, `risk` value, and write guard.
- `scripts/tokhub-admin.mjs`: deterministic admin-agent client.
- `evals/trigger_cases.json`: trigger boundary cases for this admin-execution skill.
- `reports/output-risk-profile.md`: likely output mistakes and mitigations.
- `reports/trust-boundary.md`: token, network, and audit trust boundary.
