# Roadmap

Tracks delivery against `E2E_SENTINEL_CODEX_MASTER_SPEC.md` §25. Phases
are implemented strictly in order; a phase does not start until the
previous one's acceptance criteria are demonstrated (spec §33).

| Phase | Name | Status |
|---|---|---|
| 0 | Foundation | **Done** |
| 1 | Project & Repository Discovery | **Done** |
| 2 | Docker Compose Discovery | **Done** |
| 3 | Application Graph | Not started |
| 4 | Test Planning | Not started |
| 5 | Playwright Runner | Not started |
| 6 | AI Providers | Not started |
| 7 | Failure Analysis & Bug Reports | Not started |
| 8 | Fix Proposals | Not started |
| 9 | Production Hardening | Not started |
| 10 | Kubernetes Discovery | Not started |
| 11 | Advanced Test Adapters (WebSocket, Maestro, Detox, k6, ZAP, Nuclei, Schemathesis, Pact, Kafka) | Not started |

## Phase 0 — Foundation (done)

- Go API + Next.js web shell, PostgreSQL + Redis, versioned migrations,
  structured logging, append-only audit log, health/readiness endpoints,
  Docker Compose stack, unit + integration tests.
- See [ADR 0001](adr/0001-phase0-foundation.md).

## Phase 1 — Project & Repository Discovery (done)

- `Project` and `Environment` domain entities (`internal/projects`,
  `internal/environments`), migration `0002_projects.sql`.
- Path validation (`ValidateRepositoryPath`) rejecting nonexistent paths,
  non-directories, system roots, and resolving symlinks before any
  containment check — never trusts the caller-supplied string as-is.
- Deterministic repository scanner (`internal/discovery`): languages,
  frameworks (via manifest dependencies / go.mod imports / text markers),
  Docker files, CI pipelines, existing test tooling (Playwright, Cypress,
  Maestro, Detox, Postman), API schemas, SQL migration directories — each
  finding carries evidence (file paths) and a confidence level, never
  presented as more certain than the detection method warrants.
- API: `POST/GET /projects`, `GET/PATCH /projects/{id}`,
  `POST /projects/{id}/discover`, `GET /projects/{id}/discovery`,
  `GET /projects/{id}/environments`, `PATCH /environments/{id}`.
- Web: functional Projects page (add/list/discover/classify) and
  Discovery page (per-project findings grouped by category).
- Environments: classifying as `production` or `unknown` forces
  `allow_mutations`/`allow_load_tests`/`allow_active_security_scan` off in
  the same request — cannot be set otherwise (spec §2.6).

Acceptance criteria (spec) — verified: a Next.js + Go repository is
correctly detected; Docker files are listed; existing Playwright tests
are discovered; source paths cannot escape the project root (symlink
escape test + system-root denylist); repeated discovery is idempotent
(same finding set, no duplicate rows). Verified both via unit/integration
tests and manually against a live Postgres-backed API.

## Phase 2 — Docker Compose Discovery (done)

- `internal/compose`: pure YAML parser for Docker Compose files (no
  `docker compose` subprocess — avoids the command-injection surface a
  shell-out would introduce, spec §23.3). Handles both short/long syntax
  for `environment`, `depends_on`, `ports`, `command`/`entrypoint`.
  Environment variable *values* are discarded before leaving the parser —
  only names are ever kept (spec §7.4).
- `internal/dockerclient`: minimal read-only Docker Engine API client
  (Unix socket, two endpoints: `/_ping`, `/containers/json`) rather than
  the full Docker SDK, keeping the capability surface small (spec §7.3).
  Every method returns `ErrUnavailable` when the socket is missing or
  unreachable — never a fatal error.
- `internal/services`: `DiscoveredService` entity (spec §6.3) upserted by
  `(project_id, name)` so re-discovery updates in place. `ClassifyKind`
  infers kind from image name (high confidence) or port/build presence
  (medium confidence) — never overclaimed.
- Docker socket is **not** mounted by default (spec §24.4); when absent,
  every service still gets recorded from the compose file with
  `status: "unknown"`, rendered as "not observed" (distinct from "not
  running"). See [docs/DOCKER_DISCOVERY.md](DOCKER_DISCOVERY.md) for how
  to opt in to live status.
- API: `GET /projects/{id}/services` (populated as part of
  `POST /projects/{id}/discover`). Web: Discovery page shows a services
  table alongside repository findings.

Acceptance criteria (spec) — verified: Compose services appear in the
panel; running status is visible when the daemon is reachable; secret
values are never returned (only env var names) — confirmed by grep
against API responses and container logs; Docker-unavailable state
degrades gracefully (dedicated test + manual verification with the
socket un-mounted).

## Next: Phase 3 — Application Graph

Per spec §25 Phase 3: graph node/edge model, route extraction (Next.js /
Express / Go routers), OpenAPI import, runtime-to-source correlation,
graph UI with evidence drawer and confidence display.
