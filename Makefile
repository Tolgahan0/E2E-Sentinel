.PHONY: dev test lint build up down migrate

# Runs the API and web app locally (not in Docker), for fast iteration.
# Requires `make up` (or an equivalent local Postgres/Redis) running first.
dev:
	@echo "Run these in separate terminals:"
	@echo "  (cd apps/api && SENTINEL_DATABASE_URL=postgres://sentinel:sentinel@localhost:5432/sentinel?sslmode=disable SENTINEL_REDIS_ADDR=localhost:6379 go run ./cmd/sentinel)"
	@echo "  (cd apps/web && SENTINEL_API_URL=http://localhost:8080 npm run dev)"

test:
	cd apps/api && go test ./...
	cd apps/web && npm run typecheck

lint:
	cd apps/api && go vet ./...
	cd apps/web && npm run lint

build:
	cd apps/api && go build ./...
	cd apps/web && npm run build

up:
	@mkdir -p runner-workspaces workspace
	SENTINEL_RUNNER_HOST_WORKSPACE_DIR="$$(pwd)/runner-workspaces" docker compose build playwright-runner
	SENTINEL_RUNNER_HOST_WORKSPACE_DIR="$$(pwd)/runner-workspaces" docker compose up -d --build

down:
	docker compose down

migrate:
	cd apps/api && \
		SENTINEL_DATABASE_URL=$${SENTINEL_DATABASE_URL:-postgres://sentinel:sentinel@localhost:5432/sentinel?sslmode=disable} \
		SENTINEL_REDIS_ADDR=$${SENTINEL_REDIS_ADDR:-localhost:6379} \
		SENTINEL_MIGRATIONS_DIR=$${SENTINEL_MIGRATIONS_DIR:-../../migrations} \
		go run ./cmd/sentinel -migrate-only
