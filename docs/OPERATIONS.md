# Operations

## Health and readiness

- `GET /health` (liveness, no dependency checks) and `GET /ready` (checks
  Postgres + Redis) on `sentinel-api`.
- `GET /metrics` (Phase 9): a hand-rolled Prometheus text-exposition
  endpoint — `e2e_sentinel_http_requests_total{method,status}`,
  `e2e_sentinel_test_runs_total{status}`, `e2e_sentinel_active_test_runs`,
  `e2e_sentinel_ai_requests_total{provider_type,outcome}`. Unauthenticated
  like `/health`/`/ready` (a scrape target, not a browser user) —
  firewall it the same way as the rest of this deployment (see
  [docs/SECURITY_MODEL.md](SECURITY_MODEL.md)). Full OpenTelemetry
  distributed tracing is not implemented — see that doc for why.

## Logs

Structured JSON on stdout for both `sentinel-api` (zerolog) and
`sentinel-web` (Next.js default). `docker compose logs -f <service>`.

## Audit trail

`GET /api/v1/audit-events` (or the panel's Audit Logs page) supports
search filters (Phase 9): `action_type`, `resource_type`, `resource_id`,
`actor`, `since`/`until` (RFC 3339), `limit`. There is no route, at any
HTTP verb, that modifies or deletes a recorded event — `internal/audit
.Recorder` only exposes `Record`/`Recent`/`Search`.

## Retention

Artifacts (Phase 5) are saved with a `retention_until` timestamp set at
creation (14 days default, 30 for a failed run, 7 for a passed one —
spec §12). `artifacts.RunRetentionLoop` (Phase 9) sweeps expired
artifacts (file, then metadata row) once an hour by default
(`DefaultRetentionSweepInterval`) for the life of the `sentinel-api`
process — no separate cron/scheduler is required. This is a simple
ticker-based sweep, not spec §21's full job system (idempotency keys,
retry policy, dead-letter handling) — that's separately reserved,
larger infrastructure this phase doesn't build.

## Backups

- **Postgres** holds everything except artifact bytes and fix-workspace
  scratch copies: projects, discovery findings, the application graph,
  test cases, runs, bug reports, fix proposals, AI provider config
  (encrypted keys), users/sessions, audit events, settings.
  - **Local development**: `docker compose exec postgres pg_dump -U
    ${POSTGRES_USER:-sentinel} ${POSTGRES_DB:-sentinel} > backup.sql`
    (or `pg_dumpall` for a full cluster dump including roles). Restore
    with `psql ... < backup.sql` against a fresh, empty database.
  - **Docker deployment**: the `sentinel-postgres-data` named volume can
    also be snapshotted directly (`docker run --rm -v
    e2e-sentinel_sentinel-postgres-data:/data -v $(pwd):/backup alpine
    tar czf /backup/postgres-data.tgz /data` with Postgres stopped, for a
    consistent filesystem-level snapshot) — `pg_dump` is preferred when
    Postgres can stay running.
  - **Kubernetes deployment** (Phase 10, not yet implemented): expected
    to delegate to whatever the target cluster's Postgres offering
    provides (a managed database's own backup mechanism, or a
    PVC-snapshot-capable CSI driver) — no E2E-Sentinel-specific tooling
    exists for this yet.
- **Artifacts** (`sentinel-artifacts` volume) and **fix workspaces**
  (`sentinel-fix-workspaces` volume, transient by nature — safe to
  exclude from backups entirely) are local-filesystem bytes referenced
  by Postgres metadata (`storage_path`). Back these up as a filesystem/
  volume snapshot if artifact history matters to you; losing them without
  losing the matching Postgres rows produces 404s on
  `GET /artifacts/{id}/content`, not corruption.
- **`SENTINEL_SECRET_ENCRYPTION_KEY`** (Phase 6) must be backed up
  **separately** from the database, with equal care: losing it makes
  every stored AI provider API key permanently undecryptable, and a
  database backup alone can't substitute for it (see
  [docs/AI_PROVIDER_GUIDE.md](AI_PROVIDER_GUIDE.md)).
- **`.env`** (gitignored) holds `POSTGRES_PASSWORD` and, if set,
  `SENTINEL_ADMIN_PASSWORD` — back it up like any other secret, not as
  part of a routine repository backup (it's deliberately not committed).

## Dependency scanning

`make scan` runs `govulncheck` (Go) and `npm audit --audit-level=high`
(web); `.github/workflows/dependency-scan.yml` runs both on every push/
PR to `main` and weekly. See
[docs/SECURITY_MODEL.md](SECURITY_MODEL.md#current-state-phases-09) for
the specific findings as of when Phase 9 shipped and why they weren't
auto-remediated.

## Notifications (webhook)

`Settings` page has one webhook URL field. When set, a `POST` fires
(fire-and-forget, in its own goroutine, never blocking whatever
triggered it) on exactly two events:

- `bug_report.created` — only the first time a `(project, test case,
  failure type)` combination produces a bug; a recurring failure just
  updates the existing bug's frequency and does not re-notify.
- `fix_proposal.pending_review` — every fix proposal, since one always
  starts `pending_review` (spec §15, never auto-approved).

This is a v1 ceiling, not a job system: one URL, no retry queue, no
delivery tracking, no request signature (spec §21's full job system —
idempotency keys, retry policy, dead-letter handling — is separately
reserved, larger infrastructure). A failed delivery is logged and
otherwise has no effect. `POST /api/v1/notifications/webhook/test`
sends a synthetic event immediately, so a delivery can be confirmed
without waiting for a real bug or fix proposal.

The webhook URL is exactly as trusted as an AI provider's `base_url`
(spec §16.3's SSRF discussion in [docs/SECURITY_MODEL.md](SECURITY_MODEL.md)
applies here too) — configuring it is an administrator action
(`configure_providers` permission once RBAC is on), not attacker-
reachable input.

## Not yet implemented

Kubernetes-specific operational tooling (Phase 10), OpenTelemetry
distributed tracing, and any automated backup *schedule* (the commands
above are manual — wiring them into a cron job or a managed backup
service is left to the operator's own infrastructure).
