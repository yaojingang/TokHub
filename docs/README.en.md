# TokHub

TokHub is an open-source monitoring, recommendation, and OpenAI-compatible gateway platform for AI API providers and relay services. It combines public status pages, provider rankings, user workspaces, an admin console, layered probes, usage analytics, alerts, audit logs, secret encryption, and Docker-based self-hosting in one runnable system.

Simplified Chinese: [README.md](../README.md)

## What TokHub Is For

TokHub is built for teams that operate or compare multiple AI API upstreams:

- Public status pages often show only "up" or "down", but operators need to know whether the failure is DNS, TLS, authentication, model listing, or real generation.
- Users may bring their own upstream endpoints and API keys, but still need health checks, quota controls, gateway keys, and audit logs.
- A provider recommendation page needs editorial picks, ranking rules, click tracking, and public APIs instead of static copy.
- Enterprise users often want one OpenAI-compatible endpoint backed by several upstreams, routed by latency, success rate, or cost.
- Self-hosting should include production preflight checks, backups, restore drills, security scans, and release gates, not only source code.

TokHub turns those needs into a deployable foundation for AI API monitoring, operations, and gateway routing.

## Core Features

### Public Monitoring And Recommendations

- Public home page, channel list, channel detail pages, provider rankings, and curated recommendations.
- Filters and views for provider, model, status, price, latency, success rate, and health score.
- Admin-managed recommendation slots, newcomer offers, scenario recommendations, and ranking rules.
- Public `/api/public/*` endpoints and third-party `/v1/status/*` Open API.
- Optional generated channel-site assets for standalone public monitoring and recommendation pages.

### User Workspaces

- Users can favorite public channels and create private channels with their own endpoints and keys.
- Private channels support endpoint configuration, model selection, daily probe quota, status tracking, manual probe, and connection validation.
- Each workspace has gateways, Gateway Keys, members, usage analytics, alerts, incidents, and audit logs.
- Workspace data is isolated by organization. Regular users cannot access platform admin data or other workspaces.

### Platform Admin Console

- Manage platform channels, private channels, users, organizations, members, Gateway Keys, Open API sites, and recommendation content.
- Import, export, sync, enable, disable, and delete channels with guardrails such as password confirmation.
- View global usage, request events, cost estimates, audit exports, and governance summaries.
- Maintain site configuration, public copy, model catalog, and model pricing from the admin console.

### OpenAI-Compatible Dedicated Gateway

- Exposes `/gateway/v1/*` with OpenAI-style Models and Chat Completions behavior.
- Each gateway can bind multiple platform upstreams or user-owned private upstreams.
- Gateway Keys support QPS limits, monthly quota, status management, revocation, deletion, and one-time plaintext reveal.
- Supports streaming and non-streaming responses.
- Records request model, upstream channel, status code, tokens, latency, cost, error type, and usage estimation state.

## Probe And Health Model

TokHub separates channel health into three layers so network reachability is not confused with real model availability.

### L1 Connectivity Probe

L1 validates the basic network path:

- Parse the endpoint URL.
- Resolve DNS.
- Open a TCP connection.
- Perform a TLS handshake for HTTPS targets and record certificate expiry.
- Send an HTTP HEAD request.

This layer classifies DNS, TCP, TLS, HTTP, and malformed endpoint failures.

### L2 Model Availability Probe

L2 calls the upstream `/models` endpoint to verify:

- Whether the API key is accepted.
- Whether the upstream returns a usable model list.
- Whether the configured model exists or is available.
- Whether a provider profile intentionally skips model-list probing.

Authentication failures are classified as `auth_error`; missing models are classified as `model_not_found`.

### L3 Real Generation Probe

L3 sends a minimal Chat Completions request and asks the model to return a fixed response. This verifies the real generation path:

- Records latency, estimated first token time, HTTP status, token usage, and cost.
- Checks whether the response content matches the expected output.
- Separates slow responses, rate limits, empty content, auth errors, and model failures.

### Status Synthesis

TokHub combines L1, L2, and L3 into channel states:

- `healthy`: connectivity, model listing, and generation are working.
- `degraded`: usable, but slow, rate limited, partially failing, or inconsistent.
- `connectivity_down`: network or model-list path is unavailable.
- `functional_down`: network may be reachable, but real generation fails.
- `auth_error`: credentials are invalid or unauthorized.
- `unknown`: not enough probe data.

Snapshots store 24-hour uptime, success rate, P95 latency, L1/L2/L3 latency, tokens, cost, and health score.

## Gateway Routing

Dedicated gateways build a route plan from configured upstreams:

1. Skip disabled upstreams.
2. Prefer to filter out `connectivity_down`, `auth_error`, and `functional_down` upstreams.
3. If every upstream is unhealthy, fall back to all enabled upstreams to avoid an empty route.
4. Sort candidates by gateway policy.
5. Skip upstreams currently in short circuit-breaker cooldown.
6. Store the route plan in Redis for observability and later extensions.

Supported policies:

- `latency`: lower P95 latency first, with health score as the tie breaker.
- `success`: higher success rate first, with health score as the tie breaker.
- `cost`: lower cost first, with health score as the tie breaker.

Redis is used for per-second QPS buckets, short circuit-breaker flags, and route-plan caching. If Redis is unavailable, the gateway falls back to in-memory circuit state and database-backed routing.

## Security And Encryption

TokHub treats credentials as production data:

- Upstream API keys, private-channel keys, and notification targets are encrypted with AES-GCM.
- `TOKHUB_SECRET_KEY` is used as the master secret and must be at least 32 characters in production.
- Each encryption operation uses a random nonce. The database stores ciphertext, nonce, mask, and fingerprint.
- Gateway Keys are generated with an `sk-th-` prefix and stored as SHA-256 hashes with a short prefix and mask.
- Full Gateway Keys are shown only once during creation.
- Login passwords are hashed with bcrypt.
- Session tokens are stored as hashes.
- Browser write requests require Cookie plus CSRF token validation.
- Production deployments should set `TOKHUB_SESSION_SECURE=true`.
- Public metadata fetching blocks localhost, private networks, link-local ranges, multicast ranges, reserved ranges, and documentation ranges to reduce SSRF risk.
- Delete and governance flows scrub related key material and write audit events.

## Technology Stack

| Layer | Stack |
| --- | --- |
| Backend | Go, go-chi, pgx, sqlc, bcrypt |
| Frontend | React, Vite, TypeScript, React Router, Radix UI |
| Database | PostgreSQL, TimescaleDB, SQL migrations, sqlc generated queries |
| Cache and rate limits | Redis |
| Events and workers | NATS |
| Probing and gateway | L1/L2/L3 probes, OpenAI-compatible gateway, Anthropic/Gemini/OpenAI adapters |
| Deployment | Dockerfile, Docker Compose, role-split Compose, Helm templates |
| Verification | Go test, go vet, TypeScript, Vite build, Playwright, release scripts, security scans |

## Architecture

### One Binary, Multiple Roles

The backend has one Go entrypoint, `cmd/tokhub`. Runtime behavior is selected with `TOKHUB_ROLE`:

- `all`: runs web, API, gateway, probes, and workers in one process.
- `api`: serves public pages, user console, admin console, and Open API.
- `gateway`: serves the OpenAI-compatible gateway.
- `prober`: runs probe workloads.
- `worker`: runs async worker tasks.
- `migrate`: runs database migrations.
- `seed`: initializes admin user, default organization, site config, and model catalog.

### From Single Container To Split Roles

The default deployment uses one Docker Compose stack for small teams and self-hosted setups. When traffic grows, `deploy/compose/docker-compose.roles.yml` can split API, gateway, prober, and worker roles.

### Operations-Oriented Data Model

TokHub models users, organizations, channels, channel credentials, model catalogs, model prices, probe runs, probe snapshots, incidents, gateways, Gateway Keys, request events, usage rollups, alerts, notification channels, audits, and Open API sites. The schema is designed for real monitoring and gateway operations, not only demo pages.

### Release Hardening

The repository includes open-source preflight checks, production environment preflight, no-demo-data checks, backups, restore drills, security scans, Compose config validation, Docker builds, and smoke tests.

## Quick Start

```bash
cp -n .env.example .env || true
docker compose up -d --build
```

Default endpoints:

- Web / API / Gateway: `http://localhost:8080`
- OpenAPI: `http://localhost:8080/openapi.yaml`
- Metrics: `http://localhost:8080/metrics`
- Gateway: `http://localhost:8080/gateway/v1/*`
- Local admin email: `admin@tokhub.local`
- Local admin password: `ChangeMe123!`

The default credentials are for local development only. Production deployments must replace `TOKHUB_ADMIN_PASSWORD` and `TOKHUB_SECRET_KEY` in `.env.production`.

Smoke test after startup:

```bash
TOKHUB_BASE_URL=http://localhost:8080 npm run test:smoke
```

## Local Verification

Basic checks:

```bash
go test ./...
go vet ./...
sqlc generate
npm run typecheck
npm run lint
npm run build
npm run test:security
docker compose config
```

After the app is running:

```bash
npm run test:ops
npm run test:restore
npm run test:e2e
npm run test:visual
```

Release gate:

```bash
deploy/scripts/release-check.sh
```

Full local release checks, when Docker is available:

```bash
RUN_DB_CHECK=1 RUN_RESTORE=1 RUN_E2E=1 RUN_VISUAL=1 RUN_SMOKE=1 deploy/scripts/release-check.sh
```

## Production Deployment

Do not use development defaults from `.env.example` in production. At minimum, configure:

- `TOKHUB_PUBLIC_URL`
- `TOKHUB_ADMIN_EMAIL`
- `TOKHUB_ADMIN_PASSWORD`
- `TOKHUB_SECRET_KEY`
- `DATABASE_URL`
- `REDIS_URL`
- `NATS_URL`
- `SMTP_URL`, if real email delivery is required

Recommended production settings:

- `TOKHUB_ENV=production`
- `TOKHUB_SEED_MODE=prod`
- `TOKHUB_UPSTREAM_MODE=real`
- `TOKHUB_SESSION_SECURE=true`
- `TOKHUB_EXPOSE_DEV_TOKENS=false`

Single-container deployment:

```bash
cp .env.production.example .env.production
# Fill real secrets, domain, and external service URLs.
deploy/scripts/preflight.sh --env-file .env.production
docker compose --env-file .env.production up -d --build
curl -fsS "$TOKHUB_PUBLIC_URL/healthz"
curl -fsS "$TOKHUB_PUBLIC_URL/readyz"
```

Role-split deployment:

```bash
docker compose --env-file .env.production -f docker-compose.yml -f deploy/compose/docker-compose.roles.yml up -d --build
```

More details:

- [Deployment](DEPLOYMENT.md)
- [Release](RELEASE.md)
- [Recovery drill](RECOVERY-DRILL.md)

## API

- Human-readable API guide: [API.md](API.md)
- Public OpenAPI contract: [openapi.yaml](openapi.yaml)
- Admin Agent API: [admin-agent-api.md](admin-agent-api.md)
- Admin Agent OpenAPI: [admin-agent.openapi.yaml](admin-agent.openapi.yaml)
- Runtime OpenAPI endpoint: `http://localhost:8080/openapi.yaml`

Main API namespaces:

- `/api/public/*`: public page data.
- `/api/auth/*`: registration, login, sessions, email verification, and password reset.
- `/api/me/*`: user favorites and private channels.
- `/api/console/*`: user or enterprise workspace.
- `/api/admin/*`: platform admin console.
- `/v1/status/*`: third-party read-only status Open API.
- `/gateway/v1/*`: OpenAI-compatible dedicated gateway.

## Repository Layout

- `cmd/tokhub/`: single backend entrypoint.
- `internal/`: backend modules for API, auth, crypto, probes, gateway, events, and data access.
- `web/`: React / Vite frontend.
- `db/`: SQL migrations and sqlc queries.
- `deploy/`: Compose, Helm, backup, restore, load test, and release scripts.
- `docs/`: API, deployment, release, recovery, open-source, and machine-contract documentation.
- `tests/`: Playwright end-to-end and visual tests.

## Open-Source Release

Read [OPEN_SOURCE.md](OPEN_SOURCE.md) before the first public release. Do not use `git add .` for the first public commit. Use the documented allowlist so local `.env` files, backups, temporary files, local binaries, private reviews, and prototype assets are not published.

Open-source preflight:

```bash
npm run open-source:preflight
```

## License

TokHub is licensed under the Apache License, Version 2.0. See [LICENSE](../LICENSE) and [NOTICE](../NOTICE).
