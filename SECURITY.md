# Security Policy

## Reporting a Vulnerability

Please do not publish security vulnerabilities in public issues.

For now, report sensitive findings through the project website:

https://www.tokhub.me/

Include:

- affected version or commit
- deployment mode
- reproduction steps
- expected impact
- whether credentials, tokens, API keys, or user data may be exposed

Do not include real provider keys, gateway keys, admin-agent tokens, cookies,
database dumps, or private environment files in the report body.

## Supported Versions

The public `main` branch is the supported development line. Security fixes are
expected to land there first unless a release branch is explicitly announced.

## Security Expectations

- Private provider credentials must be encrypted at rest.
- Gateway keys must not be logged or returned after one-time creation; only
  hashes, prefixes, and masks should be retained.
- Browser admin and console routes must keep CSRF, session, and role checks.
- Production deployments must not use the local development defaults from
  `.env.example`.
- Public issues and pull requests must not contain secrets, private URLs,
  database dumps, or production screenshots containing customer data.
