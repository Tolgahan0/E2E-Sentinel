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
  |  /api/v1/* rewritten to sentinel-api
  v
apps/api (Go, chi router)
  |
  +-- internal/config      Environment-based configuration
  +-- internal/logging     Structured (zerolog) logging
  +-- internal/db          Postgres pool, Redis client, migration runner
  +-- internal/audit       Append-only audit event recording
  +-- internal/httpserver  HTTP handlers: /health, /ready, /api/v1/audit-events
  |
  +-- PostgreSQL
  +-- Redis
```

Future phases add sibling packages under `internal/` — `discovery/`,
`graph/`, `projects/`, `environments/`, `providers/`, `planning/`,
`execution/`, `runners/`, `artifacts/`, `failures/`, `fixes/`, `approval/`,
`secrets/`, `scheduler/`, `telemetry/` — exactly as laid out in spec §5. None
of these exist yet; Phase 0 intentionally implements only the foundation
column above.

## Request flow (Phase 0)

1. Browser loads `apps/web` on `:9090`.
2. The dashboard page calls `/api/v1/health` and `/api/v1/ready` through
   Next.js's rewrite rule (`next.config.js`), which forwards to
   `sentinel-api:8080` inside the Docker network (or `localhost:8080` in
   local dev).
3. `apps/api` answers `/health` (liveness, no dependency checks) and
   `/ready` (checks Postgres and Redis connectivity) and records nothing on
   these read paths (they are not "meaningful operations" per spec §2.7).
4. On process start and stop, `apps/api` writes an `audit_events` row via
   `internal/audit`. `GET /api/v1/audit-events` returns the most recent
   events, paginated, for the dashboard.

## Data model (Phase 0)

Only two tables exist so far:

- `schema_migrations` — tracks which migration files have been applied.
- `audit_events` — append-only; see `migrations/0001_init.sql`. Matches the
  shape of spec §6 `Approval`/audit fields that are already known
  (`action_type`, `resource_type`, `resource_id`, `actor`, `metadata`,
  `created_at`); fields specific to entities that don't exist yet (projects,
  test runs, etc.) are deferred to the phases that introduce those entities.

## Configuration

All configuration is environment-variable based (`internal/config`), with
validation at startup and no silent defaults for security-relevant values
(e.g. there is no default database DSN — it must be set explicitly). See
`.env.example` for the full list.

## What Phase 0 deliberately does not do

Per spec §2.2 and §34, Phase 0 has no code path that:

- reads or mounts a *target* repository (the thing E2E Sentinel will test),
- talks to the Docker or Kubernetes API,
- talks to any AI provider,
- writes, commits, or pushes code anywhere.

These are reserved extension points for Phases 1–11.
