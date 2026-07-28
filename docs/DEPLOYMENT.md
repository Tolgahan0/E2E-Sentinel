# Deployment

Currently supports a single deployment shape: Docker Compose on a single
host, running E2E Sentinel's own Postgres, Redis, API, web panel, and
(from Phase 5) disposable Playwright runner containers on the same
host's Docker daemon. Kubernetes deployment (Helm chart, read-only
service account, external Postgres/Redis, S3 artifact storage) is
planned for Phase 10 — see [docs/ROADMAP.md](ROADMAP.md).

## Docker Compose

```bash
cp .env.example .env
# set POSTGRES_PASSWORD to a real secret; never commit .env
make up
```

Use `make up`, not `docker compose up` directly — it builds the
Playwright runner image and computes `SENTINEL_RUNNER_HOST_WORKSPACE_DIR`
(an absolute host path; see
[docs/RUNNER_ISOLATION.md](RUNNER_ISOLATION.md)) before invoking Compose.
Without that variable, `docker-compose.yml` fails fast with a clear
error instead of starting with test execution silently broken.

Services:

- `postgres` (16-alpine) — data in the `sentinel-postgres-data` volume.
- `redis` (7-alpine)
- `artifacts-init` — one-shot container that `chown`s the
  `sentinel-artifacts` volume so distroless `sentinel-api` (no shell) can
  write to it; exits immediately, `sentinel-api` waits for it via
  `depends_on: service_completed_successfully`.
- `sentinel-api` — Go binary in a distroless, non-root image. Applies
  pending migrations on startup before serving traffic. Mounts the
  Docker socket (`group_add` grants access without running as root — see
  [docs/RUNNER_ISOLATION.md](RUNNER_ISOLATION.md)) to launch disposable
  test-runner containers.
- `sentinel-web` — Next.js standalone build, non-root, listens on `9090`.
- `playwright-runner` — build-only (`profiles: [build-only]`); never
  started by `docker compose up`. Produces the
  `e2e-sentinel-playwright-runner:latest` image that `sentinel-api`
  launches disposable containers from — never pulled or built at
  runtime.

Only `sentinel-web` needs to be reachable by end users
(`http://<host>:9090`). `sentinel-api`'s `8080` is bound to `127.0.0.1`
only by default — expose it further only if you need direct API access.

## Environment variables

See `.env.example` for the full list. Notably:

- `POSTGRES_PASSWORD` has no default and compose fails fast
  (`POSTGRES_PASSWORD must be set`) if it's missing.
- `SENTINEL_API_URL` (set automatically for `sentinel-web` inside Compose)
  is read at **runtime**, not baked into the web image at build time — the
  same web image works against any `sentinel-api` address.
- `SENTINEL_RUNNER_HOST_WORKSPACE_DIR` — set by `make up`, not something
  you configure by hand under normal use.
- `SENTINEL_DOCKER_GID` — only needed on a Linux host with rootful Docker,
  where the socket is typically `root:docker` rather than Docker
  Desktop's `root:root`; set it to that group's real GID.

## Integration tests

`tests/integration` runs black-box HTTP tests against a live stack:

```bash
make up
cd tests/integration
SENTINEL_INTEGRATION_BASE_URL=http://localhost:8080 go test ./... -v
```

## What's not implemented yet

No production hardening (RBAC, OIDC, rate limiting, CSRF, backups) is
implemented yet — that's Phase 9. Do not expose this deployment shape
directly to the public internet without first reviewing
[docs/SECURITY_MODEL.md](SECURITY_MODEL.md) and completing the phases that
add authentication. This matters more than before Phase 5: the Docker
socket is now mounted into `sentinel-api` by default (a documented
trade-off — see [docs/RUNNER_ISOLATION.md](RUNNER_ISOLATION.md#why-a-direct-socket-mount)),
so anyone who can reach `sentinel-api`'s API can, in effect, run
arbitrary containers on this host.
