# Troubleshooting (Phase 0)

## `docker compose up` fails with "POSTGRES_PASSWORD must be set"

You haven't created `.env`. Run `cp .env.example .env` and set a real
`POSTGRES_PASSWORD`, then retry.

## `sentinel-api` keeps restarting, logs show a migration error

Check `docker compose logs sentinel-api`. Migrations run once, in a
transaction, on every startup; a failed migration file will crash-loop
the container until fixed. Confirm `migrations/*.sql` is valid and that
`./migrations` is correctly mounted (see `docker-compose.yml`).

## `/ready` returns 503

`GET /ready` reports per-dependency status in its JSON body
(`{"checks": {"postgres": "...", "redis": "..."}}`). Check
`docker compose ps` for the failing dependency's health status and
`docker compose logs postgres` / `docker compose logs redis`.

## The panel loads but shows "sentinel-api unreachable"

`sentinel-web`'s route handlers (`app/api/*`) proxy to
`SENTINEL_API_URL` (default `http://localhost:8080`, set to
`http://sentinel-api:8080` in Docker Compose) at **request time**, not
build time — so this is a live connectivity problem, not a stale build.
Confirm `sentinel-api` is running and reachable from the `sentinel-web`
container: `docker compose exec sentinel-web wget -qO- http://sentinel-api:8080/health`
(the image doesn't ship `curl`).

## Docker isn't running at all

`docker info` will hang or error immediately if the Docker daemon isn't
started. On macOS, open the Docker Desktop app first.
