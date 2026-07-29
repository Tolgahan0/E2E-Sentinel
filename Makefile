.PHONY: dev test lint build up down migrate scan onboard

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
	SENTINEL_RUNNER_HOST_WORKSPACE_DIR="$$(pwd)/runner-workspaces" docker compose build playwright-runner websocket-runner
	SENTINEL_RUNNER_HOST_WORKSPACE_DIR="$$(pwd)/runner-workspaces" docker compose up -d --build

# Integrate an external repository in one command — no AI coding
# assistant required. See docs/QUICKSTART.md and scripts/onboard.sh.
#   make onboard SOURCE=https://github.com/acme/app
#   make onboard SOURCE=../my-app NAME="My App"
onboard:
	@./scripts/onboard.sh "$(SOURCE)" "$(NAME)"

down:
	docker compose down

migrate:
	cd apps/api && \
		SENTINEL_DATABASE_URL=$${SENTINEL_DATABASE_URL:-postgres://sentinel:sentinel@localhost:5432/sentinel?sslmode=disable} \
		SENTINEL_REDIS_ADDR=$${SENTINEL_REDIS_ADDR:-localhost:6379} \
		SENTINEL_MIGRATIONS_DIR=$${SENTINEL_MIGRATIONS_DIR:-../../migrations} \
		go run ./cmd/sentinel -migrate-only

# Dependency scanning (spec §9 Production Hardening). govulncheck checks
# Go module dependencies (and the standard library) against the Go
# vulnerability database; npm audit does the same for the web app's
# dependencies. Also run in CI — see .github/workflows/dependency-scan.yml.
scan:
	cd apps/api && go run golang.org/x/vuln/cmd/govulncheck@latest ./...
	cd apps/web && npm audit --audit-level=high
