# Architecture

This document describes the architecture of E2E Sentinel as implemented so
far, and the extension points reserved for later phases defined in
`E2E_SENTINEL_CODEX_MASTER_SPEC.md`. See [ADR 0001](adr/0001-phase0-foundation.md)
for the reasoning behind Phase 0's specific choices.

## Overview

```text
Browser
  |
  v
apps/web (Next.js, :9090)
  |  /api/health, /api/ready, /api/v1/* — Route Handlers proxy to
  |  sentinel-api, reading SENTINEL_API_URL at REQUEST time (not baked
  |  into the build), so one image works against any deployment.
  v
apps/api (Go, chi router)
  |
  +-- internal/config        Environment-based configuration
  +-- internal/logging       Structured (zerolog) logging
  +-- internal/db            Postgres pool, Redis client, migration runner
  +-- internal/audit         Append-only audit event recording
  +-- internal/projects      Project entity, repository-path validation
  +-- internal/environments  Environment entity, restrictive classification
  +-- internal/discovery     Deterministic repository scanner
  +-- internal/compose       Docker Compose file parser (pure, no subprocess)
  +-- internal/dockerclient  Docker Engine API client (discovery + Phase 5 container lifecycle)
  +-- internal/services      DiscoveredService entity
  +-- internal/routes        Best-effort route inventory extraction
  +-- internal/graph         Application Graph nodes/edges + correlation
  +-- internal/planning      Deterministic (no-AI) test case rule engine
  +-- internal/testgen       Deterministic Playwright spec generation
  +-- internal/runs          Runner interface, TestRun tracking
  +-- internal/artifacts     Local-filesystem artifact storage
  +-- internal/httpserver    HTTP handlers
  |
  +-- PostgreSQL
  +-- Redis
  +-- Docker daemon (required for Phase 5 test execution; optional otherwise)
  +-- disposable Playwright runner containers (Phase 5)
```

Future phases add sibling packages under `internal/` — `providers/`,
`failures/`, `fixes/`, `approval/`, `secrets/`, `scheduler/`,
`telemetry/` — exactly as laid out in spec §5.

## Request flow

1. Browser loads `apps/web` on `:9090`.
2. Pages call same-origin `/api/*` paths. Each is a Next.js Route Handler
   (`app/api/**/route.ts`) that forwards to `sentinel-api` and streams the
   response back — the browser never talks to `sentinel-api` directly.
3. `apps/api` answers `/health` (liveness, no dependency checks), `/ready`
   (checks Postgres/Redis), and the `/api/v1/*` resource endpoints below.

## Domain flow: adding a project and running discovery (Phase 1)

```text
POST /api/v1/projects {name, repository_path}
  -> projects.ValidateRepositoryPath: resolve absolute, resolve symlinks,
     reject nonexistent/non-directory/system-root paths
  -> projects.Store.Create (slug auto-generated, uniqued)
  -> environments.Store.Create (default environment, classification=local)
  -> audit: project.added

POST /api/v1/projects/{id}/discover
  -> discovery.Scan(project.RepositoryPath)   [read-only, symlink-safe walk]
  -> discovery.Store.CompleteRun (findings persisted with evidence)
  -> projects.Store.SetDiscoveryStatus(completed)
  -> audit: repository.scanned

PATCH /api/v1/environments/{id} {classification}
  -> environments.RestrictForClassification: "production"/"unknown"
     force allow_mutations/allow_load_tests/allow_active_security_scan
     off unconditionally — the caller cannot set them in the same request
  -> audit: environment.classification_changed
```

Discovery is synchronous in Phase 1 (a normal repository scans in
milliseconds); `internal/discovery.Scan` has no dependency on the HTTP
layer, so moving it onto the async job system (spec §21) in a later phase
is a wiring change, not a rewrite.

### Running discovery via Docker Compose

`sentinel-api` scans `repository_path` on its *own* filesystem. When run
via `docker compose up`, that's the container's filesystem — so target
repositories must be mounted in. `docker-compose.yml` mounts
`./workspace` (gitignored) into the container at `/workspace:ro`; see
[docs/LOCAL_DEVELOPMENT.md](LOCAL_DEVELOPMENT.md#adding-a-project-repository-discovery).

### Docker Compose service discovery (Phase 2)

`POST /projects/{id}/discover` also runs this, right after the repository
scan:

```text
for each docker/docker_compose finding (from discovery.Scan):
  compose.ParseFile(path)              [pure YAML parse, no subprocess]
    -> []compose.Service (image, ports, depends_on, env var NAMES only, ...)
  services.FromCompose(...)            [ClassifyKind: image name -> high
                                         confidence; port/build presence ->
                                         medium confidence heuristic]
  if dockerClient reachable (Ping succeeds):
    ListContainers() -> match by com.docker.compose.service label
    -> services.ApplyRuntimeStatus(...)  [live state, container name, ports]
  else:
    service keeps metadata.status = "unknown" ("not observed", not "down")
services.Store.Upsert(...)             [keyed by (project_id, name) — idempotent]
  -> audit: service.discovered
```

`internal/dockerclient` talks to the Docker Engine API over
`/var/run/docker.sock` using exactly two endpoints (`/_ping`,
`/containers/json`) — not the full Docker SDK — and is never required:
`docker-compose.yml` does not mount the socket by default (see
[docs/DOCKER_DISCOVERY.md](DOCKER_DISCOVERY.md)).

### Application Graph (Phase 3)

Still within the same `POST /projects/{id}/discover` request, after
services are upserted:

```text
routes.Extract(root, findings)        [Next.js file conventions + OpenAPI
                                        paths -> high confidence; regex-
                                        matched Express/Go/Flask calls,
                                        via the same discovery.Walk used
                                        by repository scanning -> medium]
graph.Build(root, routes, services)
  depends_on edges  <- compose depends_on (high confidence)
  served_by edges   <- only if exactly ONE non-infra service exists
                        (ambiguous otherwise -> no edge, per spec §8.2)
  calls edges       <- literal fetch()/axios() URL in a page's source
                        matched against a known route path (medium)
graph.Store.ReplaceGraph(...)          [delete-then-insert per project,
                                         in one transaction — never
                                         accumulates duplicates]
  -> audit: graph.built
```

`GET /projects/{id}/graph` returns nodes and edges with real (resolved)
IDs; `Node.Key()` (`node_type|label`) is only used internally to resolve
an edge's endpoints before they have database IDs.

## Data model

- `schema_migrations` — tracks which migration files have been applied.
- `audit_events` — append-only; see `migrations/0001_init.sql`.
- `projects`, `environments`, `discovery_runs`, `discovery_findings` — see
  `migrations/0002_projects.sql`. `discovery_findings.evidence` is JSONB
  (`{"paths": [...], ...}`); `confidence` is constrained to
  `high`/`medium`/`low` and must never be presented as more certain than
  the detection method warrants (spec §8.2, §9.4).
- `discovered_services` — see `migrations/0003_discovered_services.sql`.
  Upserted by `(project_id, name)`; `metadata` carries env var *names*
  (never values), profiles, and live status when observed.
- `graph_nodes`, `graph_edges` — see `migrations/0004_application_graph.sql`.
  Replaced wholesale per project on every discovery run (no upsert-by-key;
  see `graph.PostgresStore.ReplaceGraph`).
- `test_cases` — see `migrations/0005_test_cases.sql`. Unique on
  `(project_id, natural_key)`; `CreateIfAbsent` never overwrites an
  existing row, so a user's edits/approval survive regeneration.
- `test_runs`, `artifacts` — see `migrations/0006_test_runs.sql`. One row
  per `POST /tests/{id}/run`; artifacts reference their run and store
  metadata only (checksum, MIME type, size, retention window) — bytes
  live on the local filesystem (`internal/artifacts.FileStore`).

## Domain flow: running a test (Phase 5)

```text
POST /tests/{id}/run
  -> require approval_status == approved (403 otherwise)
  -> require an environment with base_url set (422 otherwise)
  -> runs.Store.Create (status=queued)
  -> testgen.GenerateSpec(test case, environment.base_url)  [deterministic, no AI]
  -> audit: test_run.started
  -> return 202 immediately; execution continues in a background goroutine

[background] executeRunAsync:
  -> runs.Store.UpdateStatus(running)
  -> Runner.Execute: write spec + playwright.config.ts into the run's
     workspace, create+start a disposable container (Docker-outside-of-
     Docker — see docs/RUNNER_ISOLATION.md), wait for exit, fetch logs,
     remove the container
  -> if a concurrent POST /runs/{id}/cancel already marked this
     cancelled, stop here (don't overwrite with passed/failed)
  -> save stdout/stderr as artifacts; Runner.CollectArtifacts finds any
     screenshot/video/trace Playwright wrote on failure; save those too
  -> Runner.Cleanup (removes the workspace directory) — called on EVERY
     exit path, including when Execute itself failed
  -> runs.Store.UpdateStatus(passed|failed, exit_code) — decided only by
     the runner's exit code, never AI (spec §2.4)
  -> audit: test_run.completed

POST /runs/{id}/cancel
  -> runs.Store.UpdateStatus(cancelled) immediately (optimistic)
  -> Runner.Cancel: stop the container by its DETERMINISTIC name
     ("sentinel-run-<runID>") — no in-memory state needed, works across
     goroutines/processes
  -> audit: test_run.cancelled
```

## Configuration

All configuration is environment-variable based (`internal/config`), with
validation at startup and no silent defaults for security-relevant values
(e.g. there is no default database DSN — it must be set explicitly). See
`.env.example` for the full list.

## What's still not implemented

Per spec §2.2 and §34, there is still no code path that:

- talks to the Kubernetes API (Phase 10) — Docker's is now supported,
  read-only, and optional (Phase 2),
- talks to any AI provider (Phase 6),
- writes, commits, or pushes code anywhere (Phase 8),
- executes anything found inside a scanned repository — discovery only
  *reads* file names/contents to classify them, it never runs them.

These are reserved extension points for later phases.
