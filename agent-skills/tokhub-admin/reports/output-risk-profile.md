# Output Risk Profile

## Main Risks

- Treating `/api/admin/*` as public API and suggesting third-party integration.
- Executing mutating requests without explicit reason and idempotency key.
- Printing or preserving secret material from one-time key creation responses or raw JSON output.
- Printing CSV exports or package downloads that contain provider credentials or generated site secrets.
- Asking the user to paste owner password or bootstrap token into chat instead of using terminal input.
- Reporting a write as complete without checking audit metadata.
- Confusing browser Cookie + CSRF owner token bootstrap with admin-agent Bearer execution.

## Mitigations

- Route only TokHub administrator agent execution here.
- Use `references/operation-catalog.json` before choosing a method/path.
- Use `scripts/tokhub-admin.mjs`; do not hand-roll curl for writes.
- Require `--execute --reason --idempotency-key` for guarded operations.
- Rely on `scripts/tokhub-admin.mjs` redaction for JSON output and still omit plaintext keys in final responses unless the user explicitly requested owner token creation.
- Require explicit `--output` for secret-bearing downloads and exports; treat platform channel CSV exports and channel-site packages as sensitive artifacts.
- Use `bootstrap` terminal prompts or env vars for owner credentials; never collect passwords in the assistant conversation.
- Include audit verification status after any write.
- Confirm route coverage against `references/operation-catalog.json` before claiming the skill can manage a backend capability.

## Reviewer Checks

- Final answer names operation path, catalog `risk` value, and status.
- Missing env vars, missing scopes, 401/403, or audit-not-found are reported as blockers.
- Secret values are not copied into chat, logs, or committed files.
