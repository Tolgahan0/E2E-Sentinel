# Security Model

This document tracks E2E Sentinel's security posture as it is actually
implemented, phase by phase. See `E2E_SENTINEL_CODEX_MASTER_SPEC.md` §2
and §23 for the full target model; this file states what's true *today*.
[docs/THREAT_MODEL.md](THREAT_MODEL.md) covers threat areas in more depth
as each phase introduces the surface they apply to.

## Current state (Phase 0)

- **No target-repository access.** Phase 0 never reads, mounts, or writes
  any repository other than E2E Sentinel's own source. Discovery
  (Phase 1+) is the first phase that touches a target project, and it is
  read-only by design (spec §2.2, §3.1).
- **No Docker/Kubernetes API access.** `sentinel-api` never talks to the
  Docker daemon or a Kubernetes API server in Phase 0.
- **No AI provider access.** No outbound calls to any LLM provider exist
  yet.
- **Secrets.** `POSTGRES_PASSWORD` has no default and must be supplied via
  `.env` (gitignored) or the deployment's secret mechanism. It is never
  logged: `internal/logging.SensitiveFieldName`/`Redact` exist so future
  code that logs structured fields has a ready-made redaction path, and
  `internal/config` never returns a value derived from a hard-coded
  secret. Verified manually (see Phase 0 report) that no container log
  contains the Postgres password after a full `docker compose up`.
- **Non-root containers.** `sentinel-api` runs as `nonroot` in a
  distroless image (no shell, no package manager). `sentinel-web` runs as
  a dedicated non-root `sentinel` user.
- **Append-only audit log.** `audit_events` has no UPDATE/DELETE code path
  in `internal/audit`; the HTTP API only exposes a read (`GET
  /api/v1/audit-events`).
- **Least-exposure networking.** Only `sentinel-web` (`9090`) is intended
  for end users. `sentinel-api` (`8080`), `postgres` (`5432`), and `redis`
  (`6379`) are bound to `127.0.0.1` in `docker-compose.yml`, not `0.0.0.0`.

## Not yet implemented

Authentication/RBAC, approval workflow enforcement, secret redaction
pipeline for AI context, Docker/Kubernetes discovery sandboxing, runner
isolation, and patch-application safety all land in the phases that
introduce the corresponding feature (Phases 1, 4, 6, 8, 9 — see
[docs/ROADMAP.md](ROADMAP.md)). Until Phase 9, do not expose this
deployment beyond a trusted local network.

## Reporting a vulnerability

See [SECURITY.md](../SECURITY.md).
