.PHONY: dev build test migrate seed compose-up compose-down compose-roles-up compose-roles-down backup restore restore-drill security-check open-source-preflight db-check preflight release-check smoke real-provider-check load-public

dev:
	npm run dev

build:
	npm run build
	go build ./cmd/tokhub

test:
	go test ./...
	npm run typecheck
	npm run test:security

migrate:
	TOKHUB_ROLE=migrate go run ./cmd/tokhub

seed:
	TOKHUB_ROLE=seed go run ./cmd/tokhub

compose-up:
	cp -n .env.example .env || true
	docker compose up -d

compose-down:
	docker compose down

compose-roles-up:
	docker compose -f docker-compose.yml -f deploy/compose/docker-compose.roles.yml up -d --build

compose-roles-down:
	docker compose -f docker-compose.yml -f deploy/compose/docker-compose.roles.yml down

backup:
	deploy/scripts/backup.sh

restore:
	deploy/scripts/restore.sh $(DUMP)

restore-drill:
	deploy/scripts/restore-drill.sh $(DUMP)

security-check:
	npm run test:security

open-source-preflight:
	npm run open-source:preflight

db-check:
	npm run test:ops

preflight:
	deploy/scripts/preflight.sh --env-file .env.production

release-check:
	deploy/scripts/release-check.sh

smoke:
	deploy/scripts/smoke.sh

real-provider-check:
	deploy/scripts/real-provider-check.sh

load-public:
	npm run load:public
