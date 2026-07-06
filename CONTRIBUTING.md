# Contributing to TokHub

Thank you for improving TokHub.

## Development Setup

```bash
npm ci
go mod download
cp -n .env.example .env
docker compose up -d --build
```

Local defaults are for development only. Do not reuse `.env.example` secrets,
passwords, or insecure cookie settings in production.

## Checks Before a Pull Request

Run the fast local checks:

```bash
go test ./...
go vet ./...
npm run typecheck
npm run build
npm run test:security
bash deploy/scripts/open-source-preflight.sh
```

When Docker is available, also run:

```bash
docker compose config
docker compose -f docker-compose.yml -f deploy/compose/docker-compose.roles.yml config
```

## Pull Request Rules

- Keep pull requests focused.
- Include tests for behavior changes.
- Update `docs/API.md` and `docs/openapi.yaml` when public API contracts change.
- Do not commit `.env`, provider credentials, gateway keys, admin-agent tokens,
  database dumps, local screenshots with private data, or generated reports.
- Do not include files ignored by `.gitignore` using `git add -f` unless a
  maintainer explicitly approves the exception.

## License

By contributing, you agree that your contribution is licensed under the
Apache License, Version 2.0.
