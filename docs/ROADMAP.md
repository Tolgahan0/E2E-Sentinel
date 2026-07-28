# Roadmap

Tracks delivery against `E2E_SENTINEL_CODEX_MASTER_SPEC.md` §25. Phases
are implemented strictly in order; a phase does not start until the
previous one's acceptance criteria are demonstrated (spec §33).

| Phase | Name | Status |
|---|---|---|
| 0 | Foundation | **Done** |
| 1 | Project & Repository Discovery | **Done** |
| 2 | Docker Compose Discovery | Not started |
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

## Next: Phase 2 — Docker Compose Discovery

Per spec §25 Phase 2: Compose parser, running-container detection,
service relationships, ports/networks/health, environment variable
*names* only (never values), discovery UI updates. Docker-unavailable
state must degrade gracefully.
