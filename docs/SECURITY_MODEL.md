# Security Model

This document tracks E2E Sentinel's security posture as it is actually
implemented, phase by phase. See `E2E_SENTINEL_CODEX_MASTER_SPEC.md` §2
and §23 for the full target model; this file states what's true *today*.
[docs/THREAT_MODEL.md](THREAT_MODEL.md) covers threat areas in more depth
as each phase introduces the surface they apply to.

## Current state (Phases 0–2)

- **Target-repository access is read-only and path-validated.**
  `internal/projects.ValidateRepositoryPath` resolves the caller-supplied
  path to an absolute, symlink-free path, rejects it if it doesn't exist,
  isn't a directory, or resolves to a system root (`/`, `/etc`, `/private/etc`,
  etc. — see `dangerousRoots`); `internal/discovery.Scan` additionally
  refuses to follow any symlink whose real target falls outside the
  validated root (`projects.WithinRoot`). Discovery never writes to the
  scanned repository and never executes anything found in it — it only
  reads file names and, for a small allowlist of manifest files
  (`package.json`, `go.mod`, `requirements.txt`, etc.), file contents, to
  classify them. Both properties (no traversal, no symlink escape) are
  covered by dedicated tests in `internal/projects` and
  `internal/discovery`.
- **Docker access is read-only, minimal, and optional.**
  `internal/dockerclient` implements exactly two Docker Engine API calls
  (`/_ping`, `/containers/json`) over the Unix socket — not the full
  Docker SDK — so a bug here has a deliberately small blast radius. The
  socket is **not mounted by default** in `docker-compose.yml` (spec
  §24.4: mounting it grants extensive host capability); every
  `dockerclient` method returns `ErrUnavailable` when it's absent, and
  callers treat that as a normal, expected state (a service still gets
  recorded from its compose declaration, just with `status: "unknown"`).
  Compose *files* are parsed directly as YAML (`internal/compose`) —
  never via a `docker compose` subprocess — which avoids the
  command-injection surface a shell-out would introduce (spec §23.3) and
  discards environment variable *values* before they ever leave the
  parser (spec §7.4). No Kubernetes API access yet (Phase 10).
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
introduce the corresponding feature (Phases 4, 6, 8, 9 — see
[docs/ROADMAP.md](ROADMAP.md)). Until Phase 9, do not expose this
deployment beyond a trusted local network.

## Reporting a vulnerability

See [SECURITY.md](../SECURITY.md).
