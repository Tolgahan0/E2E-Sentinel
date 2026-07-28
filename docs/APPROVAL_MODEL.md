# Approval Model

**Status: not yet implemented.** The `Approval` entity (spec §6.10) and
the enforcement engine that gates mutating actions behind it land in
Phase 4 (test approval) and Phase 8 (patch approval).

## What it will do (spec §2.3)

Approvals must be action-specific, time-bounded, audited, revocable, and
visible in the UI. A general approval must never silently authorize an
unrelated future action. Actions requiring approval include: writing
generated tests into the repository, applying a patch, creating a
branch/commit/PR, restarting a service or deployment, applying a
Kubernetes manifest, running mutating or load tests, running active
security scans, and modifying test or application data.

## What exists today

Nothing in Phase 0 performs any of the above actions, so there is nothing
to gate yet. The `audit_events` table and `internal/audit` package that
the approval engine will log through already exist.
