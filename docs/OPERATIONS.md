# Operations

**Status: partial — Phase 0 only covers basic health/log operations.**
Full operational guidance (backups, retention jobs, metrics dashboards,
on-call runbooks) lands alongside Phase 9 (Production Hardening).

## What you can do today

- **Health**: `GET /health` (liveness, no dependency checks) and
  `GET /ready` (checks Postgres + Redis) on `sentinel-api`.
- **Logs**: structured JSON on stdout for both `sentinel-api` (zerolog)
  and `sentinel-web` (Next.js default). `docker compose logs -f
  <service>`.
- **Audit trail**: `GET /api/v1/audit-events` or the panel's Audit Logs
  page. Currently records only process startup/shutdown; every later
  phase adds events for the actions it introduces (spec §2.7).
- **Data**: Postgres data lives in the `sentinel-postgres-data` Docker
  volume. There is no backup automation yet — for Phase 0, treat the
  audit log as non-critical (it only contains startup/shutdown events);
  do not rely on this deployment for data you can't afford to lose.

## Not yet implemented

Metrics/tracing (OpenTelemetry, Phase 9), retention jobs for artifacts
(Phase 5+), rate limiting, and formal backup/restore procedures.
