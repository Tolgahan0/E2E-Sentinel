# Local Development

## Prerequisites

- Go 1.25+
- Node.js 20+
- Docker with Compose v2 (for Postgres/Redis, or the full stack)

## Running the full stack in Docker

```bash
cp .env.example .env
# edit .env, set a real POSTGRES_PASSWORD
make up
```

This builds the Playwright runner image, then builds and starts
`postgres`, `redis`, `sentinel-api`, `sentinel-web`, and a one-shot
`artifacts-init` container. The panel is at <http://localhost:9090>.
`sentinel-api` is also published on `127.0.0.1:8080` for direct debugging
(`curl localhost:8080/health`).

Use `make up` rather than `docker compose up` directly — it computes
`SENTINEL_RUNNER_HOST_WORKSPACE_DIR` (an absolute host path
`sentinel-api` needs to launch disposable test-runner containers; see
[docs/RUNNER_ISOLATION.md](RUNNER_ISOLATION.md)) and passes it through.
Without it, `docker-compose.yml` fails fast with a clear error rather
than starting with test execution silently broken.

`make down` stops everything. Postgres data persists in the
`sentinel-postgres-data` named volume across restarts; `docker compose down
-v` removes it (destructive — only do this deliberately).

## Running the API and web app outside Docker

Start just the dependencies:

```bash
docker compose up -d postgres redis
```

Then, in one terminal:

```bash
cd apps/api
SENTINEL_DATABASE_URL="postgres://sentinel:<password>@localhost:5432/sentinel?sslmode=disable" \
SENTINEL_REDIS_ADDR="localhost:6379" \
SENTINEL_MIGRATIONS_DIR="../../migrations" \
go run ./cmd/sentinel
```

Running this way, test execution (Phase 5) works with less ceremony
than in Docker: since the process runs directly on your host (not
Docker-outside-of-Docker), `SENTINEL_RUNNER_WORKSPACE_DIR` and
`SENTINEL_RUNNER_HOST_WORKSPACE_DIR` can both point at the same real
path, e.g. `SENTINEL_RUNNER_WORKSPACE_DIR=/tmp/sentinel-runs
SENTINEL_RUNNER_HOST_WORKSPACE_DIR=/tmp/sentinel-runs`. You'll still need
`e2e-sentinel-playwright-runner:latest` built once
(`docker compose build playwright-runner`).

In another:

```bash
cd apps/web
SENTINEL_API_URL="http://localhost:8080" npm run dev
```

The web app listens on `:9090` in dev mode too (`next dev -p 9090`).

## Adding a project (repository discovery)

`sentinel-api` validates and scans `repository_path` on its own
filesystem — when running via Docker Compose, that's the container's
filesystem, not your host's.

- **Outside Docker** (`go run ./cmd/sentinel`): use any absolute host
  path directly, e.g. `/Users/you/code/routa`.
- **Via `make up` / Docker Compose**: place or symlink the target
  repository under `./workspace` in this repo (gitignored, mounted
  read-only into `sentinel-api` at `/workspace`), then use
  `/workspace/<name>` as the `repository_path` in the Add Project form.
  Override the host-side mount with `SENTINEL_WORKSPACE=/other/path make up`
  if you don't want to use `./workspace`.

Either way, `ValidateRepositoryPath` (spec §23.4) rejects paths that
don't exist, aren't directories, or resolve to a system root — nothing is
read until you click "Run discovery".

## Applying migrations without starting the server

```bash
make migrate
```

This runs the API binary with `-migrate-only`, which applies pending
`migrations/*.sql` files and exits — it does not connect to Redis-dependent
routes or start the HTTP server.

## Running tests

```bash
make test     # go test ./... (apps/api) + tsc --noEmit (apps/web)
make lint     # go vet ./... + eslint .
```

Integration tests against a real running stack (`tests/integration/`) are
separate — see [docs/DEPLOYMENT.md](DEPLOYMENT.md#integration-tests) or
run directly:

```bash
docker compose up -d
cd tests/integration
SENTINEL_INTEGRATION_BASE_URL=http://localhost:8080 go test ./... -v
```

They skip (not fail) when `SENTINEL_INTEGRATION_BASE_URL` is unset, so
`go test ./...` from the repo root never depends on Docker being up.

## Configuration reference

All backend configuration is environment-variable based
(`apps/api/internal/config`). There is no default database or Redis
address — both must be set explicitly. See `.env.example` for the
Docker Compose-level variables.
