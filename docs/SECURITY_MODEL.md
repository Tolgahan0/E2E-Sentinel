# Security Model

This document tracks E2E Sentinel's security posture as it is actually
implemented, phase by phase. See `E2E_SENTINEL_CODEX_MASTER_SPEC.md` §2
and §23 for the full target model; this file states what's true *today*.
[docs/THREAT_MODEL.md](THREAT_MODEL.md) covers threat areas in more depth
as each phase introduces the surface they apply to.

## Current state (Phases 0–6)

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
- **Route extraction reads source, never executes it.**
  `internal/routes.Extract` reads `.js`/`.ts`/`.go`/`.py` file *contents*
  (regex-matched, not parsed/evaluated) to find router calls, and reuses
  the exact same symlink-safe, skip-dir traversal as repository
  discovery (`discovery.Walk`) rather than a separate implementation —
  one walker to keep safe, not two.
- **Docker access is narrow in what it implements, but now mandatory for
  test execution.** As of Phase 5, `docker-compose.yml` mounts the
  Docker socket into `sentinel-api` by default — a deliberate,
  documented trade-off (direct mount chosen over a restricted proxy for
  simplicity; see [docs/RUNNER_ISOLATION.md](RUNNER_ISOLATION.md#why-a-direct-socket-mount))
  in exchange for host capability spec §24.4 warns about. `sentinel-api`
  is added to Docker group `0` (`group_add`) purely to satisfy the
  socket's Unix permission bits — it does not run as root and gains no
  other capability from this. `internal/dockerclient` still implements
  only the operations actually needed (discovery: ping, list containers;
  Phase 5: create/start/wait/stop/remove a container, fetch its logs) —
  never the full Docker SDK, and it never pulls or builds an image at
  runtime (runner images are pre-built via `make up`). Discovery's Docker
  calls still degrade gracefully (`ErrUnavailable`) if the socket becomes
  unreachable; Phase 5's runner calls do not — there is no "run tests
  without Docker" fallback, since Docker *is* the isolation mechanism.
  Compose *files* are still parsed directly as YAML (`internal/compose`)
  — never via a `docker compose` subprocess (spec §23.3) — and env var
  *values* are discarded before leaving the parser (spec §7.4). No
  Kubernetes API access yet (Phase 10).
- **Runner containers are resource-limited and non-root.** Every test
  run gets its own disposable container: memory/CPU limits, a wall-clock
  timeout, no volume beyond its own workspace, and a fixed non-root user
  (`pwuser`, baked into `Dockerfile.runner-playwright` — never
  configurable per-run). Containers are removed immediately after each
  run, verified via `docker ps -a --filter name=sentinel-run-` returning
  empty after both passing and failing runs, including when execution
  fails outright.
- **Artifact downloads set defensive headers.** `GET
  /artifacts/{id}/content` always sets `X-Content-Type-Options: nosniff`
  and forces `Content-Disposition: attachment` for every artifact kind
  except screenshots (spec §23.5) — a trace/log/HAR file is untrusted
  content and must never be interpreted as HTML by a browser tab.
- **Production-unsafe approvals are blocked at the API layer, not just
  the UI.** `POST /tests/{id}/approve` returns 403 for a mutating test
  case when any of the project's environments is classified `production`
  or `unknown` (spec §2.6) — enforced server-side regardless of what
  client makes the request, and covered by both a unit and an
  integration test against a live API.
- **AI provider API keys are encrypted at rest and never returned.**
  `internal/secretstore` encrypts every stored key with AES-256-GCM
  (`SENTINEL_SECRET_ENCRYPTION_KEY`, optional — unset means keyed
  providers can't be created, but keyless ones like local Ollama still
  work). `internal/providers` stores only an opaque
  `secret_reference_id`; every `GET`/`POST`/`PATCH /providers*` response
  is built through `toProviderResponse`, which has no field for the raw
  key or that reference ID — only a `has_api_key` boolean, verified by
  dedicated tests asserting the literal key value never appears in a
  response body. The plaintext key is resolved only server-side,
  immediately before an outbound health-check or (in a later phase) AI
  request — never on a path that returns to an HTTP client.
- **No actual AI call exists yet.** Phase 6 delivers provider
  configuration, a live "test connection" health check, and task
  routing only — no phase before 7 sends any content to an AI provider.
  `internal/redaction` (the pipeline required before that will ever
  happen, spec §16.5) already exists and is independently tested:
  secrets, tokens, credentials, `Authorization`/`Cookie` headers are
  detected and redacted from arbitrary text, with a path allowlist and
  file-size limit as building blocks for whichever later phase first
  assembles real AI context.
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

Authentication/RBAC, approval workflow enforcement for patches,
Kubernetes discovery sandboxing, and patch-application safety all land
in the phases that introduce the corresponding feature (Phases 8, 9 —
see [docs/ROADMAP.md](ROADMAP.md)). Until Phase 9, do not expose this
deployment beyond a trusted local network — this matters more than
before Phase 5, given the Docker socket is mounted by default, and more
than before Phase 6 if an AI provider API key is stored (no RBAC yet
gates who can read `has_api_key`/trigger a health check, even though the
key itself is never exposed through the API).

## Reporting a vulnerability

See [SECURITY.md](../SECURITY.md).
