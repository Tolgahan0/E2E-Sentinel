# Roadmap

Tracks delivery against `E2E_SENTINEL_CODEX_MASTER_SPEC.md` §25. Phases
are implemented strictly in order; a phase does not start until the
previous one's acceptance criteria are demonstrated (spec §33).

| Phase | Name | Status |
|---|---|---|
| 0 | Foundation | **Done** |
| 1 | Project & Repository Discovery | **Done** |
| 2 | Docker Compose Discovery | **Done** |
| 3 | Application Graph | **Done** |
| 4 | Test Planning | **Done** |
| 5 | Playwright Runner | **Done** |
| 6 | AI Providers | **Done** |
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

## Phase 3 — Application Graph (done)

- `internal/routes`: best-effort route inventory. Next.js App Router file
  conventions (`page.tsx` / `route.ts`, route groups `(...)` excluded from
  the URL, exported `GET`/`POST`/... detected via regex) and OpenAPI
  `paths:` declarations are high confidence; regex-matched
  Express/Go-chi-gin-fiber/Flask-FastAPI router calls in arbitrary source
  are medium confidence — a regex can't fully understand the language
  it's scanning, so it's never presented as more certain than that.
  Reuses `discovery.Walk` (the same symlink-safe, skip-dir traversal as
  repository scanning) rather than re-implementing it — refactoring that
  out caught a real pre-existing bug where a deleted/inaccessible project
  root silently produced zero findings instead of an error.
- `internal/graph`: `Node`/`Edge` entities (spec §6.4–§6.5) built by
  correlating routes and services: `depends_on` edges from explicit
  compose declarations (high confidence), `served_by` edges only when
  exactly one non-infrastructure service exists (ambiguous otherwise —
  no edge beats a wrong one), `calls` edges from literal `fetch()`/
  `axios()` URLs in page source matched against known routes (medium
  confidence). `ReplaceGraph` deletes-then-inserts a project's full graph
  per discovery run, so it never accumulates duplicates.
- API: `GET /projects/{id}/graph`, populated as part of
  `POST /projects/{id}/discover`. Web: Application Map page — node-type
  filter, label search, and a per-edge evidence drawer. A zoomable
  graphical canvas (React Flow) is deferred; the full node/edge/evidence/
  confidence data model is already exposed via a list-based UI.

Acceptance criteria (spec) — verified: the spec's own example chain
(`Login Page -> POST /api/v1/auth/login -> Auth Handler`) reproduces
end-to-end against a live Postgres-backed API and through the web proxy;
every edge carries evidence; low-confidence edges are visibly marked;
repeated discovery does not duplicate the graph.

## Phase 4 — Test Planning (done)

- `internal/planning`: `TestCase` entity (spec §6.6) and a fixed-rule
  engine (`GeneratePlan`) — **no AI call is made or possible**, since
  Phase 6 (AI providers) doesn't exist yet; this is the "no-AI mode"
  baseline the spec requires to remain the permanent fallback (§16.6).
  Rules cover authentication (valid/invalid credentials), authorization
  (non-admin access), tenant isolation (path contains a tenant segment),
  CRUD + validation (mutating routes), API schema (read-only routes),
  error handling (webhooks), and smoke (health routes) — risk-scored into
  P0–P3 and high/medium/low, derived from route kind and HTTP method
  alone, never upgraded above the source route's own confidence.
- Idempotent regeneration: `CreateIfAbsent` keyed by
  `(project_id, natural_key)` — re-running planning after new discovery
  never overwrites a test case the user already edited, approved, or
  rejected. (Known trade-off: if a route's classification changes on a
  later fix, previously generated suggestions under the old
  classification aren't retroactively removed — only newly-added,
  never-reviewed "pending" ones accumulate that way; approved/rejected
  ones are unaffected either way.)
- Production-safety gate: `POST /tests/{id}/approve` looks up the test's
  project environments and refuses (403) to approve a mutating test
  while any environment is classified `production` or `unknown` — spec
  §25 Phase 4's literal acceptance criterion ("Production-unsafe tests
  cannot be approved accidentally"), verified against a live API.
- API: `POST /projects/{id}/tests/plan`, `GET /projects/{id}/tests`,
  `PATCH /tests/{id}` (edit title/description/priority),
  `POST /tests/{id}/approve`, `POST /tests/{id}/reject`. Web: functional
  Test Inventory page (generate/filter/approve/reject/edit + a coverage-
  confidence summary that's explicit about being "suggested coverage
  only"); Approvals page now points here.

Two real bugs were caught by manual live-stack verification while
building this phase (not by unit tests, which used routes with
kind/path combinations that happened to mask them) and fixed with
regression tests added: (1) sibling test cases generated for the same
route (e.g. "valid credentials" vs "invalid credentials") shared an
identical `NaturalKey`, silently colliding down to one stored row; (2)
the Next.js `route.ts` file-convention handler hardcoded `Kind: KindAPI`
instead of classifying by path, so e.g. `POST /api/v1/auth/login` was
never recognized as an authentication route — the same class of bug
already fixed for `page.tsx` in Phase 3, which should have been a signal
to check the sibling code path at the time.

Acceptance criteria (spec) — verified: suggested tests are reviewable
(GET /tests, web Test Inventory); mutating tests are clearly marked
(`is_mutating`/`is_production_safe` on every response); production-unsafe
tests cannot be approved accidentally (403 gate, integration-tested);
plan generation works without AI using deterministic rules (the only
mode that exists).

## Phase 5 — Playwright Runner (done)

- **Direct Docker socket mount** chosen over a restricted proxy (a
  documented trade-off — see [docs/RUNNER_ISOLATION.md](RUNNER_ISOLATION.md#why-a-direct-socket-mount)):
  `sentinel-api` now launches disposable sibling containers via
  Docker-outside-of-Docker. `internal/dockerclient` gained container
  lifecycle operations (create/start/wait/stop/logs/remove) — still not
  the full Docker SDK, and runner images are pre-built, never pulled at
  runtime (no arbitrary-image-pull surface).
- `internal/testgen`: deterministic (no AI) Playwright spec generation
  from an approved `TestCase` — smoke-level assertions only, honestly
  documented as the ceiling without schema/AI input (spec §16.6, §36).
- `internal/runs`: `Runner` interface (spec §11.2, adapted) +
  `DockerPlaywrightRunner`. Cancellation works by stopping the container
  by a **deterministic name**, not in-memory state — correct across
  goroutines/processes. Resource limits (memory, CPU, wall-clock
  timeout) are enforced per run (spec §11.4).
- `internal/artifacts`: local-filesystem artifact storage (checksum,
  MIME type, size, retention window per spec §12) — stdout/stderr
  always, screenshot/video/trace on failure via Playwright's own
  failure-triggered capture config.
- API: `POST /tests/{id}/run`, `GET /runs/{id}`, `POST /runs/{id}/cancel`,
  `GET /runs/{id}/artifacts`, `GET /artifacts/{id}/content`,
  `GET /projects/{id}/runs`. Environments gained `base_url`
  (`PATCH /environments/{id}`), required before a test can run. Web:
  functional Runs page (start/poll/cancel/view artifacts, including
  inline screenshot preview).

**Three real bugs found and fixed during live-stack verification** (none
caught by unit tests, since none touch a real Docker daemon):
1. Docker Desktop's socket is `root:root` mode `660` — a non-root
   container can't open it without `group_add`.
2. A Docker-managed named volume is root-owned; the distroless artifacts
   store couldn't write to it until a one-shot `artifacts-init`
   container `chown`s it first.
3. Globally-installed `@playwright/test` isn't on Node's `require()`
   resolution path from an arbitrary bind-mounted working directory —
   fixed via `NODE_PATH` in the runner image.

Plus two logic bugs: `artifacts.FileStore.Save` left an orphaned,
unreadable metadata row when the file write failed after the DB insert
(fixed with a compensating delete); and `executeRunAsync` skipped
`Runner.Cleanup` entirely when `Execute` itself failed, leaking the
run's workspace directory on every infra-level failure (fixed, with a
regression test).

Acceptance criteria (spec) — verified against the live stack with a real
Chromium browser in a real disposable container: a passing run (exit
code 0, `status: passed`); a failing run against an unreachable target
(exit code 1, `status: failed`, with screenshot/video/trace/stdout all
captured and downloadable — screenshot confirmed as a valid 1280×720
PNG); the runner container removed after every run
(`docker ps -a --filter name=sentinel-run-` empty); cancellation
covered by both a live check (409 on an already-finished run) and a
unit test with a controllable blocking fake runner; the target
repository was never written to.

## Phase 6 — AI Providers (done)

- `internal/providers`: `Provider` entity (spec §6.11) covering all six
  supported types (Ollama, OpenAI, Anthropic, Gemini, Azure OpenAI,
  OpenAI-compatible). `HealthChecker` performs a real "test connection"
  request per type against each provider's own "list models"-style
  endpoint (`/api/tags` for Ollama, `/models` for OpenAI/compatible,
  `/v1/models` for Anthropic, etc.) — the smallest request that proves
  both reachability and that a stored key is accepted, without sending
  any repository or test content.
- `internal/secretstore`: AES-256-GCM encryption for provider API keys
  (spec §16.3, §23.6). A key is never stored, logged, or returned through
  the API — only an opaque `secret_reference_id`; `Resolve` is called
  exclusively by server-side code about to make an outbound provider
  request (or a health check), never by a response path. The encryption
  key itself (`SENTINEL_SECRET_ENCRYPTION_KEY`) is optional — unset means
  a provider can still be configured and tested if it needs no key (a
  local Ollama instance), but storing a key on any provider returns 503
  until the key is set.
- `internal/redaction`: the context-sanitization pipeline (spec §16.5) —
  detects and redacts secrets, tokens (JWTs, bearer tokens), credentials
  (passwords, URL-embedded `user:pass@`), `Authorization`/`Cookie`
  headers, plus a path allowlist and file-size-limit helper for later
  phases that assemble AI context from repository files. Distinct from
  the pre-existing `internal/logging.Redact` (which redacts a *log field*
  by name) — this scans free-form text content regardless of where a
  secret appears in it.
- `internal/settings`: a small generic key/value store (spec's data model
  §19 `settings` table) — first consumer is `ai.task_routing`, a JSON map
  of task type -> provider ID (spec §16.4: architecture analysis, test
  planning, test generation, failure analysis, fix generation, report
  summarization).
- API: `GET/POST /providers`, `PATCH /providers/{id}` (including API key
  rotation and clearing), `POST /providers/{id}/test`,
  `GET/PATCH /providers/routing`. Web: functional AI Providers page (add/
  list/enable-disable/test-connection + task routing table); no longer a
  stub.
- **No phase yet makes an actual AI call** — Phase 6 delivers
  configuration, health checking, and routing only, per spec's own phase
  order (failure analysis and fix generation, the first real AI
  consumers, are Phases 7–8). The application remains fully usable with
  zero providers configured, which is the literal Phase 6 acceptance
  criterion ("AI can be disabled entirely") and was true before this
  phase started, since nothing before Phase 7 depends on AI.

Acceptance criteria (spec) — verified: a local Ollama provider can be
added and selected with no API key at all; an external provider (OpenAI-
shaped) can be configured with a key; every provider list/get/patch/test
response was checked to confirm the raw key and `secret_reference_id`
never appear, only a `has_api_key` boolean; the redaction test suite
passes (96.7% coverage) proving secrets/tokens/credentials/auth headers/
cookies never survive into redacted output while ordinary text is left
byte-for-byte unchanged; AI remains fully optional — `go test ./...`
passes with `SENTINEL_SECRET_ENCRYPTION_KEY` unset.

## Next: Phase 7 — Failure Analysis and Bug Reports

Per spec §25 Phase 7: failure classification, evidence correlation,
structured bug reports, Markdown/JSON export, duplicate hints, severity
model. This is the first phase that would route through
`internal/providers`' task routing to call an AI provider — always
advisory (spec §2.4), and root cause must be presented as a hypothesis,
never a fact.
