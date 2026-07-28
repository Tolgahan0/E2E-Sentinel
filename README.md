# E2E Sentinel

E2E Sentinel is a self-hosted, AI-assisted quality engineering platform. It
discovers a repository and its runtime architecture, produces a reviewable
test inventory and risk-based E2E test plan, generates and executes tests
in isolated runners, correlates failures across layers, and proposes fixes
as reviewable diffs — never changing source code, infrastructure, or data
without explicit approval.

> E2E Sentinel analyzes repository structure, runtime services, API
> schemas, application routes, existing tests, and observed behavior to
> generate high-confidence test recommendations and evidence-backed
> failure reports.

This repository is being built incrementally against
[`E2E_SENTINEL_CODEX_MASTER_SPEC.md`](E2E_SENTINEL_CODEX_MASTER_SPEC.md).
**Phases 0–2** are implemented; see [docs/ROADMAP.md](docs/ROADMAP.md)
for what comes next and [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) /
[ADR 0001](docs/adr/0001-phase0-foundation.md) for the reasoning behind
the foundational choices.

## What's implemented

- Go API (`apps/api`) with structured logging, environment-based config,
  a PostgreSQL-backed audit log, and `/health` / `/ready` / `/api/v1/audit-events`.
- **Projects & repository discovery**: add a project by absolute path
  (validated — must exist, must be a directory, cannot be a system root,
  symlinks resolved before any check), then run a deterministic scan that
  detects languages, frameworks, Docker files, CI pipelines, existing test
  tooling (Playwright/Cypress/Maestro/Detox/Postman), and API schemas —
  each finding carries file-path evidence and a confidence level.
- **Environments**: every project gets a default environment; classifying
  it `production` or `unknown` forces mutation/load-test/security-scan
  permissions off in the same request (spec §2.6).
- **Docker Compose service discovery**: compose files found during
  discovery are parsed directly (no `docker compose` subprocess) into
  declared services — image, ports, dependencies, env var *names* only.
  If the Docker daemon is reachable (never required, and not mounted by
  default), services are enriched with live running status; otherwise
  they show as "not observed", never as a false "not running".
- A Next.js panel (`apps/web`) on port `9090`: Dashboard, Audit Logs,
  Projects, and Discovery (findings + services) are functional; the
  remaining nav sections are stub pages pointing at the phase that
  implements them.
- Versioned SQL migrations with a small custom runner.
- Docker Compose stack: `sentinel-api`, `sentinel-web`, `postgres`, `redis`.

Nothing yet talks to Kubernetes, calls an AI provider, or
writes/commits/pushes code anywhere — those are reserved for later
phases. Discovery only *reads* file names/contents to classify them; it
never executes anything in the scanned repository.

## Quick start

```bash
cp .env.example .env
# edit .env and set POSTGRES_PASSWORD

make up          # docker compose up -d --build
```

Then open <http://localhost:9090>.

```bash
make down        # stop the stack
```

See [docs/LOCAL_DEVELOPMENT.md](docs/LOCAL_DEVELOPMENT.md) for running the
API and web app outside Docker, and [docs/DEPLOYMENT.md](docs/DEPLOYMENT.md)
for deployment notes.

## Repository layout

```text
apps/api/         Go backend (cmd/sentinel, internal/*)
apps/web/         Next.js panel
migrations/       Versioned SQL migrations
deploy/docker/    Dockerfiles
tests/integration/  Black-box tests against a running stack
docs/             Architecture, security, operational docs
```

## Safety model

E2E Sentinel starts in read-only observation mode and requires explicit,
action-specific, time-bounded, auditable approval before anything mutating
happens (writing generated tests, applying patches, restarting services,
pushing branches, etc.). See [docs/SECURITY_MODEL.md](docs/SECURITY_MODEL.md)
and [SECURITY.md](SECURITY.md).

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md).
