# TokHub Open Source Release Rules

TokHub public releases use the Apache License, Version 2.0.

Project website:

https://www.tokhub.me/

## Public Release Scope

The first public source release should include:

- core backend source: `cmd/`, `internal/`
- frontend source: `web/index.html`, `web/public/`, `web/src/`
- database assets: `db/migrations/`, `db/queries/`
- generic deployment assets: `Dockerfile`, `docker-compose.yml`, `deploy/compose/`, `deploy/helm/`, selected `deploy/scripts/`
- public docs: `README.md`, `docs/API.md`, `docs/DEPLOYMENT.md`, `docs/RELEASE.md`, `docs/RECOVERY-DRILL.md`, `docs/openapi.yaml`
- English project overview: `docs/README.en.md`
- tests and local quality gates: `tests/`, Go tests, `deploy/scripts/security-scan.sh`, `deploy/scripts/open-source-preflight.sh`
- project governance docs: `LICENSE`, `NOTICE`, `SECURITY.md`, `CONTRIBUTING.md`, `CODE_OF_CONDUCT.md`

## Excluded From First Public Release

Do not publish:

- `.env`, `.env.production`, or any real deployment environment file
- `backups/`, `tmp/`, database dumps, SQL dumps, generated restore drills, or local release packages
- `docs/reviews/` and private phase-review logs
- `skills/` and local agent-operation packages
- `prototype/` design snapshots until they are separately reviewed and sanitized
- `web/static/` generated/static recommendation packages until commercial copy and links are reviewed
- `node_modules/`, `web/dist/`, `test-results/`, `playwright-report/`, `coverage/`
- local binaries such as `/tokhub`

These exclusions are enforced by `.gitignore`, `.dockerignore`, and
`deploy/scripts/open-source-preflight.sh`.

## Local Git Rules

Do not use `git add .` for the first public commit. Use an allowlist and run
the preflight before committing.

Recommended first public staging command:

```bash
git add \
  .dockerignore .env.example .env.production.example .github .gitignore \
  CODE_OF_CONDUCT.md CONTRIBUTING.md Dockerfile LICENSE Makefile NOTICE README.md SECURITY.md \
  db deploy/compose deploy/helm deploy/load deploy/scripts docker-compose.yml \
  docs/API.md docs/DEPLOYMENT.md docs/OPEN_SOURCE.md docs/README.en.md docs/RECOVERY-DRILL.md docs/RELEASE.md docs/admin-agent-api.md docs/admin-agent.openapi.yaml docs/openapi.yaml \
  go.mod go.sum internal cmd package.json package-lock.json playwright.config.ts sqlc.yaml tests tsconfig.json vite.config.ts \
  web/index.html web/public web/src
```

Before commit:

```bash
bash deploy/scripts/open-source-preflight.sh
go test ./...
go vet ./...
npm run typecheck
npm run build
npm run test:security
docker compose config
docker compose -f docker-compose.yml -f deploy/compose/docker-compose.roles.yml config
git status --short
```

Commit format:

```bash
git commit -m "Prepare TokHub open source release"
```

## Remote And Push Rules

Use a fresh public remote. Do not push the current private working history if it
ever contained dumps, secrets, local trial logs, or internal review files.

Recommended remote naming:

```bash
git remote add origin git@github.com:<owner>/tokhub.git
git branch -M main
```

First push:

```bash
git push -u origin main
```

First public tag:

```bash
git tag -a v0.1.0-oss -m "TokHub open source release v0.1.0"
git push origin v0.1.0-oss
```

After the first push, enable repository protection before accepting external
pull requests:

- require pull requests into `main`
- require CI checks to pass
- disable force pushes to `main`
- enable secret scanning and push protection
- require at least one maintainer review
- restrict who can publish releases and tags

## Never Push

Never force-add or push these paths:

```text
.env
.env.*
backups/
tmp/
docs/reviews/
skills/
prototype/
web/static/
node_modules/
web/dist/
test-results/
playwright-report/
coverage/
*.dump
/*.sql
*.sha256
*.pem
*.key
*.p12
*.crt
```

The only allowed `.env.*` files are `.env.example` and
`.env.production.example`.
