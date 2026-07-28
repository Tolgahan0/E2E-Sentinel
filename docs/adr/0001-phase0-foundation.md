# ADR 0001 — Phase 0 Foundation

## Status

Accepted — 2026-07-28

## Context

E2E Sentinel is being built from an empty repository against
`E2E_SENTINEL_CODEX_MASTER_SPEC.md`. The spec mandates an incremental,
phase-gated build starting with **Phase 0 — Foundation** only (spec §25, §34).
Phase 0 must not include AI integration, Playwright execution, Docker socket
access, Kubernetes access, or repository patching.

### Repository assessment

- No prior code, no prior git history. The only pre-existing file is the
  master spec itself.
- The working directory is *not* nested inside any pre-existing E2E Sentinel
  git repository. A stray, empty `git init` was found at the user's home
  directory (`/Users/tolgahanayaz`) with zero commits and the entire home
  directory untracked. That repository is unrelated to this project and is
  left untouched. This project uses its own git repository rooted at
  `Desktop/E2E Sentinel/`.
- Local toolchain available: Docker 29.x with Compose v5, Go 1.25,
  Node 20. No local Postgres/Redis binaries — both are run via Docker
  Compose, consistent with the spec's self-hosted positioning.

## Decision

1. **Modular monolith**, per spec §4. One Go module (`apps/api`) for the
   backend, one Next.js app (`apps/web`) for the panel. No microservices,
   no message broker beyond Redis (used later for jobs, not required by
   Phase 0).
2. **Backend stack**: Go + [chi](https://github.com/go-chi/chi) router,
   `pgx` for PostgreSQL, `go-redis` for Redis connectivity checks,
   `zerolog` for structured logging. All are the spec's preferred choices
   (§4.1) or narrow, boring equivalents.
3. **Migrations**: plain versioned `.sql` files under top-level `migrations/`
   (spec §5, §20), applied by a small custom runner (`internal/db`) that
   tracks applied versions in a `schema_migrations` table inside a single
   transaction per file. No external migration binary dependency in Phase 0
   — keeps the foundation dependency-light and auditable. This can be
   swapped for `golang-migrate` later without changing the file format.
4. **Frontend stack**: Next.js (App Router) + TypeScript, hand-written
   rather than scaffolded via `create-next-app`, to keep the initial
   dependency tree small, deterministic, and fully understood. Serves on
   port `9090` directly (spec §17), and proxies `/api/v1/*` to the API
   service so the browser only ever talks to one origin.
5. **Audit foundation**: `audit_events` is an append-only table
   (`internal/audit`) written through a `Recorder` interface so future
   phases can add sinks (e.g. SIEM export) without touching call sites.
   Phase 0 emits audit events for service startup/shutdown and exposes a
   read-only `/api/v1/audit-events` endpoint — full event catalog (spec
   §2.7) is filled in as each later phase lands the action it audits.
6. **No destructive capability**: Phase 0 contains no code path that can
   write to a target repository, touch Docker/Kubernetes, or call an AI
   provider. Only E2E Sentinel's *own* Postgres/Redis are touched, and only
   through migrations and health checks.
7. **Testing**: Go unit tests ship alongside every package. A separate
   `tests/integration` suite talks to real Postgres/Redis when
   `TEST_DATABASE_URL` / `TEST_REDIS_URL` are set, and skips (not fails)
   otherwise, so `go test ./...` stays green with no external services
   running.

## Consequences

- Later phases (discovery, runners, AI gateway) plug into `internal/*`
  alongside the packages added here, per the target structure in spec §5.
- Because migrations live outside the Go binary's embed tree, the API
  container must have `migrations/` mounted or copied in — this is done via
  `docker-compose.yml` and the Dockerfile's build context.
- Swapping the custom migration runner for `golang-migrate` later is a
  contained change limited to `internal/db`.
