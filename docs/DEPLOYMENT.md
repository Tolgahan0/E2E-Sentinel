# Deployment (Phase 0)

Phase 0 supports a single deployment shape: Docker Compose on a single
host, running E2E Sentinel's own Postgres, Redis, API, and web panel.
Kubernetes deployment (Helm chart, read-only service account, external
Postgres/Redis, S3 artifact storage) is planned for Phase 10 — see
[docs/ROADMAP.md](ROADMAP.md).

## Docker Compose

```bash
cp .env.example .env
# set POSTGRES_PASSWORD to a real secret; never commit .env
docker compose up -d --build
```

Services:

- `postgres` (16-alpine) — data in the `sentinel-postgres-data` volume.
- `redis` (7-alpine)
- `sentinel-api` — Go binary in a distroless, non-root image. Applies
  pending migrations on startup before serving traffic.
- `sentinel-web` — Next.js standalone build, non-root, listens on `9090`.

Only `sentinel-web` needs to be reachable by end users
(`http://<host>:9090`). `sentinel-api`'s `8080` is bound to `127.0.0.1`
only by default — expose it further only if you need direct API access.

## Environment variables

See `.env.example` for the full list. Notably:

- `POSTGRES_PASSWORD` has no default and compose fails fast
  (`POSTGRES_PASSWORD must be set`) if it's missing — this is deliberate;
  Phase 0 never ships a hard-coded credential.
- `SENTINEL_API_URL` (set automatically for `sentinel-web` inside Compose)
  is read at **runtime**, not baked into the web image at build time — the
  same web image works against any `sentinel-api` address.

## Integration tests

`tests/integration` runs black-box HTTP tests against a live stack:

```bash
docker compose up -d
cd tests/integration
SENTINEL_INTEGRATION_BASE_URL=http://localhost:8080 go test ./... -v
```

## What Phase 0 does not do

No production hardening (RBAC, OIDC, rate limiting, CSRF, backups) is
implemented yet — that's Phase 9. Do not expose this deployment shape
directly to the public internet without first reviewing
[docs/SECURITY_MODEL.md](SECURITY_MODEL.md) and completing the phases that
add authentication.
