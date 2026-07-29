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
| 7 | Failure Analysis & Bug Reports | **Done** |
| 8 | Fix Proposals | **Done** |
| 9 | Production Hardening | **Done** |
| 10 | Kubernetes Discovery | **Done** |
| 11 | Advanced Test Adapters (WebSocket, Maestro, Detox, k6, ZAP, Nuclei, Schemathesis, Pact, Kafka) | **Done** (WebSocket only — see below) |

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

## Phase 7 — Failure Analysis and Bug Reports (done)

- `internal/failures`: `Classify` deterministically pattern-matches a
  failed run's stdout/stderr/exit code against an ordered list of
  signatures into one of 17 failure types (spec §13.1), a **fixed**
  severity mapping (spec §14), and a root cause hypothesis that is
  **never** assigned "high" confidence — a regex match over log text
  isn't certain enough to claim that. `AssessFlakiness` implements the
  spec §13.2 policy (insufficient evidence / suspect / flaky_candidate /
  confirmed flaky at a 60% failure-rate threshold / likely_real_defect)
  from a test case's run history; a flaky label is attached, never used
  to hide a bug.
- `internal/bugreports`: `BugReport` (spec §14's full field list) with
  `UpsertFromFailure` keyed by `(project_id, test_case_id,
  failure_type)` — a Postgres `ON CONFLICT ... DO UPDATE`, so repeated
  failures of the same kind bump `frequency`/`last_observed_at` in place
  instead of spawning duplicate rows, and a `resolved` bug flips to
  `reopened` on recurrence rather than silently absorbing the update.
  Cross-test-case duplicates get a `possible_duplicate_of_id` **hint**
  (never an automatic merge — spec §17.6 "Duplicate linking" is a manual
  UI action). `RenderMarkdown`/`RenderJSON` always label the root cause
  as an unverified hypothesis, never a diagnosis.
- Evidence correlation reuses real, already-built data rather than
  fabricating a multi-layer log pipeline E2E Sentinel doesn't have: the
  run's own captured stdout/stderr/screenshot/video/trace artifacts
  (Phase 5), and a "related graph path" looked up in the Application
  Graph (Phase 3) — an edge into and/or out of the failing route,
  omitted (not guessed) when the route isn't in the graph.
- Wired into `POST /tests/{id}/run` (Phase 5): classification and the
  bug upsert happen **before** the run's status flips to `failed`/
  `error`, so a client polling `GET /runs/{id}` never observes a
  terminal run whose bug report doesn't exist yet — a real ordering bug
  caught while adding the regression test for this, fixed by moving the
  correlation call ahead of the final `UpdateStatus`.
- API: `GET /bugs`, `GET /bugs/{id}`, `POST /bugs/{id}/resolve`,
  `POST /bugs/{id}/reopen`, `POST /bugs/{id}/notes`,
  `GET /bugs/{id}/export/markdown`, `GET /bugs/{id}/export/json` (the
  latter two forcing a download with the same nosniff/attachment headers
  as artifact downloads, spec §23.5 — a bug report embeds captured
  output from the target application, which is untrusted content). Web:
  functional Bugs page (search/severity/status filters, evidence,
  root-cause-as-hypothesis, resolve/reopen, notes, export links).
- No AI call is made or possible here — this stays on the same
  deterministic, no-AI baseline as test planning (Phase 4) and test
  generation (Phase 5); the first real AI consumer remains a later phase.

Acceptance criteria (spec) — verified: a deliberately-failing test run
(network-failure-shaped stderr) produced exactly one bug candidate with
the correct failure_type/severity and non-empty evidence; running the
same test again bumped frequency to 2 without creating a second bug;
resolving a bug and triggering the same failure again flipped it back to
`reopened`; both exports were checked to literally contain "unverified
hypothesis" (Markdown) / `root_cause_is_unverified_hypothesis: true`
(JSON); severity/status filtering was checked against `GET /bugs`.

## Phase 8 — Fix Proposals (done)

- `internal/providers` gained a `Completer` — the first actual AI call
  anywhere in the codebase, reusing the per-type request/response
  pattern already established for `HealthChecker` across all six
  provider types (Ollama's `/api/chat`, OpenAI/compatible/Azure's
  `/chat/completions` shape, Anthropic's `/v1/messages`, Gemini's
  `generateContent`). `ExtractUnifiedDiff` requires the response to
  contain a fenced ` ```diff ` block starting with a real `---` header —
  anything else is `ErrNoDiffFound`, never fabricated.
- `internal/fixproposals`: `FixProposal` (spec §6.9/§15.1's full field
  list) plus a **pure-Go unified diff parser and applier** —
  `ParseUnifiedDiff`/`ApplyFileChange` — deliberately not shelling out to
  `git apply`/`patch`, the same reasoning as Compose file parsing since
  Phase 2 (spec §23.3): a diff body may come from an AI provider, so it
  must never reach a shell. Every context/removed line is verified
  against the actual file before anything is written; a mismatch fails
  the whole file rather than corrupting it. `ApplyToWorkspace` copies the
  repository into a disposable directory under
  `SENTINEL_FIX_WORKSPACES_DIR` and applies there — the original is
  never touched. Both `ApplyToWorkspace` and the later
  `ApplyToRepository` check every diff file path with
  `projects.WithinRoot` (the same containment check discovery has used
  since Phase 1) before any write — a diff's path is attacker-controlled
  data if it came from an AI provider or a compromised project.
- Fix generation (`POST /bugs/{id}/fix-proposal`) supports two paths: a
  manually-supplied `unified_diff` (validated by parsing before
  storing), or an AI-assisted one routed through Phase 6's task routing
  (`fix_generation`). **The AI path deliberately never reads repository
  source** — only a bug report's own already-curated evidence (title,
  failure type, error message, root cause hypothesis) — since building a
  safe repository-content pipeline (path allowlist + redaction, atop
  Phase 6's `internal/redaction`) is real additional scope reserved for
  later; this is documented as an honest ceiling, the same way Phase 5's
  test generation documented "smoke-level assertions only" as its
  ceiling. Either way, a proposal is always created `pending_review` —
  the AI can never auto-approve its own output (spec §3.3 "It must not
  apply patches").
- Approval workflow: `approve`/`reject`/`request-revision` change only a
  status field. `apply-workspace` is repeatable (review, tweak, re-apply)
  and never touches the real repository. `apply-repository` is the one
  path that does — gated on `approval_status == approved` (403
  otherwise) and on never having run before (409 on a second attempt,
  checked atomically by the store) — and re-parses the exact stored
  `unified_diff`, never a regenerated one, so applied files are
  guaranteed to match the approved diff exactly (spec §15.2 acceptance).
- Regression test selection (`PATCH /fix-proposals/{id}` with
  `regression_test_ids`) is data-level only: selected tests are run the
  normal Phase 5 way, against the environment's live `base_url` — **not**
  an ephemeral deployment of the patched temporary workspace, since
  building/deploying an arbitrary target repository from source is out
  of scope. Documented as a known, honest trade-off in
  docs/FIX_PROPOSALS.md, the same way Phase 5's "no live SSE log
  streaming" was.
- Web: functional Fix Proposals page — diff review via a plain colored
  (+green/-red) viewer rather than the spec's Monaco Editor (a
  multi-megabyte dependency judged not worth the integration cost for
  review-only display at this stage, the same call already made for
  Phase 3's Application Map graph canvas), risk/explanation/assumptions/
  side-effects/rollback guidance, approve/reject/request-revision,
  apply-workspace and apply-repository (with a confirmation prompt)
  actions, per-file apply results. Bugs page gained a "Generate fix
  proposal" section (AI button + a manual-diff textarea).
- `docker-compose.yml` gained a `fix-workspaces-init` one-shot container
  (same root-owned-named-volume problem as `artifacts-init`) and a
  `sentinel-fix-workspaces` volume; `./workspace`'s existing `:ro` mount
  is documented, not changed — `apply-repository` needs it writable, an
  explicit opt-in rather than a new default.

Acceptance criteria (spec) — verified against the live stack: created a
manual fix proposal from a real bug and confirmed `apply-workspace`
patched a throwaway copy of a real fixture repository while the
original file on disk was provably untouched; approved it and confirmed
`apply-repository` wrote the exact approved content to the real file,
then confirmed a second `apply-repository` call was refused (409);
confirmed `apply-repository` before approval is refused (403) and never
touches the file; confirmed a path-escaping diff (`../../../etc/passwd`)
is rejected before any write, for both the workspace and the repository
path, with dedicated regression tests for each; generated a proposal via
a fake AI provider and confirmed its `approval_status` is
`pending_review`, never auto-approved.

## Phase 9 — Production Hardening (done)

- **RBAC** (`internal/auth`): spec §19's five roles (Viewer/Tester/
  Developer/Approver/Administrator) with a fixed, in-code
  role→permission mapping, bcrypt-hashed passwords, and opaque bearer
  session tokens (only a SHA-256 hash of the token is ever stored — same
  never-persist-the-raw-secret principle as Phase 6's provider keys).
  **Opt-in**: `SENTINEL_AUTH_ENABLED` defaults to `false`, so every route
  behaves exactly as in Phases 0–8 unless explicitly turned on — the
  same "safe default, explicit capability" pattern used for the Docker
  socket mount and secret encryption, chosen specifically so retrofitting
  auth onto ~40 existing routes couldn't destabilize the ~500 existing
  tests across every prior phase. `EnsureBootstrapAdmin` creates the
  first administrator from `SENTINEL_ADMIN_EMAIL`/`_PASSWORD` on first
  startup with auth enabled, and is a no-op forever after (spec §19 "MVP
  local mode may support a bootstrap administrator"). Beyond that
  bootstrap account, `POST/GET /api/v1/users` (Administrator-only, gated
  by a new `manage_users` permission) create and list further accounts —
  otherwise RBAC would only ever have exactly one usable account.
  Verified live: an Administrator creates a Viewer account through this
  endpoint, the Viewer logs in and is correctly 403'd both on a
  permission-gated route and on `POST /users` itself.
- **OIDC-ready architecture**: `auth.Store` is a plain interface; only
  local email/password is implemented today, but nothing about the
  session/permission model assumes it.
- **Rate limiting**: a per-client-IP token bucket
  (`golang.org/x/time/rate`), generous defaults (20 req/s, burst 60) so
  normal browser use is never affected, 429 on abuse. Constructed fresh
  per router instance, never a package-level global, so unrelated
  processes (and every test) never share limiter state.
- **CSRF protection**: a custom `X-Sentinel-Csrf` header required on
  mutating requests once auth is enabled. Documented honestly: this
  API's bearer-token auth (never cookies) is already structurally immune
  to classic CSRF — a cross-site request can't attach a custom
  `Authorization` header without CORS permission this server doesn't
  grant — so this is defense-in-depth for a direct-API-access
  deployment, not a fix for a currently-exploitable gap.
- **Security headers**: `X-Frame-Options: DENY`, a `default-src 'none'`
  CSP, `Referrer-Policy: no-referrer`, `Strict-Transport-Security` on
  every response, alongside the existing `X-Content-Type-Options:
  nosniff`.
- **Audit search**: `GET /audit-events` gained `action_type`/
  `resource_type`/`resource_id`/`actor`/`since`/`until`/`limit` query
  filters (`audit.Recorder.Search`), backed by both memory and Postgres
  implementations. Immutability verified by a dedicated test: PATCH/PUT/
  DELETE/POST to `/audit-events` all 404 or 405 — there is no route, at
  any verb, that could modify a recorded event.
- **Retention jobs**: `artifacts.RunRetentionLoop`, a ticker-based sweep
  (default hourly) that deletes artifacts past their `retention_until` —
  not spec §21's full idempotency-key/retry/dead-letter job system,
  which is separately reserved, larger infrastructure.
- **Metrics**: a hand-rolled Prometheus text-exposition registry
  (`internal/metrics`, no new heavy dependency) at `GET /metrics` —
  HTTP requests by method/status, test runs by status, active test runs,
  AI requests by provider type/outcome. Full OpenTelemetry distributed
  tracing is a documented ceiling, not attempted — wiring span
  propagation through every internal package is a materially larger
  effort than this phase's remaining scope.
- **Threat model** (docs/THREAT_MODEL.md): every spec §23.1 threat area
  mapped to its actual mitigation and the phase that introduced it, as a
  table instead of a narrative — easy to audit at a glance.
- **Dependency scanning**: `make scan` (`govulncheck` + `npm audit
  --audit-level=high`) plus `.github/workflows/dependency-scan.yml`
  (push/PR/weekly). Run for real at the time this phase shipped —
  findings (Go-toolchain-version-tied stdlib CVEs, transitive
  `postcss`/`sharp` issues via Next.js) are recorded in
  docs/SECURITY_MODEL.md rather than hidden, along with why they weren't
  blindly auto-fixed (a `next` downgrade is a breaking change not
  undertaken unilaterally here).
- **Security tests**: RBAC authorization tests (a Viewer 403s on
  approving a test, an Approver doesn't; a Developer can generate tests
  but not approve fix proposals), the audit-immutability test above, and
  the Phase 8 path-traversal tests (already existing) together are this
  phase's concrete "security checklist"/"authorization tests pass"
  acceptance evidence — see docs/SECURITY_MODEL.md for the full list
  rather than duplicating it here.

Acceptance criteria (spec) — verified: RBAC enforcement tested end-to-end
(login, wrong-password/unknown-email both return the same generic error,
missing/invalid token both 401, a Viewer 403s on a mutating route an
Approver reaches); confirmed `SENTINEL_AUTH_ENABLED` unset reproduces
Phase 0–8 behavior exactly (every existing test suite passes unmodified);
confirmed no audit-event mutation route exists at any HTTP verb; secret
handling re-verified (provider keys, session tokens, passwords all
stored only as a hash/ciphertext, never raw); Phase 5's runner isolation
tests (already existing, unchanged) still pass.

## Phase 10 — Kubernetes Discovery (done)

- **`internal/kubeclient`**: a hand-rolled, read-only Kubernetes API
  client — the same "small capability surface" philosophy as
  `internal/dockerclient`, not `client-go`'s much larger dependency
  tree. Every method is a GET; there is no create/update/patch/delete
  anywhere (spec §2 forbids applying Kubernetes resources). Two
  connection modes: a kubeconfig file (bearer-token or
  client-certificate auth only — an `exec`/`auth-provider` plugin
  produces a clear startup error, not a silent partial connection) or
  auto-detected in-cluster ServiceAccount credentials (the real
  deployment scenario, matching spec §24.5's read-only ServiceAccount).
  `ErrForbidden`/`ErrNotFound`/`ErrUnavailable` let every caller
  distinguish "RBAC denied this", "this API/CRD isn't installed", and
  "cluster unreachable" instead of one opaque failure.
- **Secret/ConfigMap values are structurally unreachable, not just
  policy.** `SecretSummary`/`ConfigMapSummary` have no `data`/
  `stringData` field at all — the value never gets decoded into memory
  even if a bug tried to log or forward the response, because
  `encoding/json` simply drops JSON fields a destination struct doesn't
  declare. Verified by a test that decodes a fixture Secret response
  containing real-shaped base64 data and asserts it never appears
  anywhere in the resulting struct.
- **`internal/kubediscovery`**: correlates Pods to
  Deployments/StatefulSets/DaemonSets by label selector (not
  `ownerReferences`, avoiding a ReplicaSet lookup) for restart-count
  aggregation; desired/ready replica counts come directly from each
  workload's own `status` fields. `Discover` never fails outright — a
  kind that 403s or 404s (Gateway API not installed) becomes a warning,
  and everything else discovered is still returned and stored.
  Ingress→Service linkage records backend service names even when no
  matching Service was itself discovered (e.g. a cross-namespace
  reference) — the mismatch is informative, not hidden.
- **Least-privilege RBAC example**:
  [`deploy/k8s/read-only-clusterrole.yaml`](../deploy/k8s/read-only-clusterrole.yaml)
  (spec §7.5's explicit requirement) — a ServiceAccount plus a
  ClusterRole granting only `get`/`list` on exactly the resource kinds
  this feature reads, no write verb anywhere.
- **HTTP surface**: `GET /kube/status` (unauthenticated, mirrors
  `/auth/status`), `POST /projects/{id}/kube-discover`, `GET
  /projects/{id}/kube-resources` (the persisted discovery snapshot),
  `GET /projects/{id}/kube/events` and `GET
  /projects/{id}/kube/pods/{pod}/logs` (both live, non-persisted reads
  — events are high-volume/ephemeral, logs are capped and
  non-streaming). All 503 with `kubernetes_not_configured` when unset —
  every other route is unaffected.
- **Web**: a new Kubernetes nav page — a project-scoped resource table
  (namespace/kind/name/replicas/restarts/status), a discovery-warnings
  panel, an on-demand cluster-events viewer, and a pod-logs viewer.
  Hidden behind a "not configured" notice when `SENTINEL_KUBE_CONFIG_PATH`
  is unset, matching the AI Providers page's "fully usable with nothing
  configured" pattern.
- See [docs/KUBERNETES_DISCOVERY.md](KUBERNETES_DISCOVERY.md) for the
  full detection list, configuration, and RBAC details.

Acceptance criteria (spec) — verified: discovery is entirely opt-in
(`SENTINEL_KUBE_CONFIG_PATH` unset behaves exactly as every prior phase,
confirmed by the full existing test suite passing unmodified); a
partial RBAC restriction or a missing Gateway API CRD degrades to a
warning rather than a failed request; Secret/ConfigMap values are never
retained; live-verified against a real (throwaway `kind`) cluster with a
ServiceAccount token scoped to the example ClusterRole — see
docs/KUBERNETES_DISCOVERY.md and the Phase 10 commit for the concrete
verification steps.

## Phase 11 — Advanced Test Adapters (WebSocket done; rest documented as deferred)

Spec §25 marks this phase "Later" — the lowest-priority, broadest, least
specified item in the roadmap. Consistent with this project's rule
against overclaiming (spec §36), one of its eight tools — WebSocket — is
implemented end-to-end and live-verified; the other seven
(Maestro, Detox, k6, ZAP, Nuclei, Schemathesis, Pact, Kafka) are
explicitly documented as a designed-but-unimplemented extension point,
not silently stubbed or assumed. See
[docs/TEST_ADAPTERS.md](TEST_ADAPTERS.md) for the full picture.

- **Detection** (`internal/routes.KindWebSocket`): a new regex-based
  extractor scans every already-scanned source file for a literal
  `ws://`/`wss://` URL — one extractor covers every language this
  project already discovers routes in (JS/TS/Go/Python), since a
  WebSocket URL literal shows up the same way regardless of which
  client library embeds it. Medium confidence, same as every other
  regex-matched route.
- **Planning**: a WebSocket route produces one `connectivity`-category
  test case (`Framework: "websocket"`) — "connection succeeds and
  yields at least one message within a timeout." Non-mutating,
  production-safe.
- **Generation** (`internal/testgen.generateWebSocketSpec`): a plain
  Node.js script (the globally-installed `ws` package, no Playwright)
  that connects, waits up to 5 seconds for any message, and exits
  0/1 accordingly — the same smoke-level, no-AI honesty as every other
  generator in this package.
- **Execution** (`internal/runs.DockerWebSocketRunner` +
  `deploy/docker/Dockerfile.runner-websocket`): the same disposable-
  container isolation model as the Playwright runner (spec §11.1), on a
  dedicated, much smaller image (`node:20-alpine` + `ws`, no browser
  stack). `Dependencies` gained a second named runner field
  (`WebSocketRunner`, not a generic registry — consistent with every
  other optional capability field), selected by `TestCase.Framework` in
  `runs_handlers.go`'s `runnerFor`/`runnerByName`. A WebSocket test skips
  the environment `base_url` requirement entirely, since its `RoutePath`
  is already a complete target URL.
- **Why the other seven are deferred, specifically**: k6 needs new
  `TestCase` fields (RPS/duration/thresholds) a smoke-test model doesn't
  have; ZAP/Nuclei scan a target, not a per-route script, a different
  shape of feature than this package's model; Schemathesis needs to run
  once against a whole OpenAPI document, not per-route; Pact needs a
  broker (stateful infrastructure, not just a runner image); Kafka has
  no literal-URL-style detection signal and needs a running broker to
  test against; Maestro/Detox need a mobile emulator/simulator in the
  runner container — a heavier isolation environment than any adapter
  here. None of these are half-built; see docs/TEST_ADAPTERS.md for the
  full reasoning per tool.

Acceptance criteria (spec) — verified: a real WebSocket echo-server
fixture, discovered end-to-end (repository scan → route extraction →
application graph → deterministic test plan → approval → generated
Node.js script → disposable-container execution → pass/fail recorded
from the process exit code, no AI involved at any step); a broken/
unreachable endpoint correctly produces a `failed` run with a clear
stderr message, not a crash; existing "playwright"/"api" framework
tests are entirely unaffected (full pre-existing test suite passes
unmodified) — see the Phase 11 commit for the concrete live-verification
steps against a real Docker container.
