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
  |  /api/health, /api/ready, /api/v1/* — Route Handlers proxy to
  |  sentinel-api, reading SENTINEL_API_URL at REQUEST time (not baked
  |  into the build), so one image works against any deployment.
  v
apps/api (Go, chi router)
  |
  +-- internal/config        Environment-based configuration
  +-- internal/logging       Structured (zerolog) logging
  +-- internal/db            Postgres pool, Redis client, migration runner
  +-- internal/audit         Append-only audit event recording
  +-- internal/projects      Project entity, repository-path validation
  +-- internal/environments  Environment entity, restrictive classification
  +-- internal/discovery     Deterministic repository scanner
  +-- internal/compose       Docker Compose file parser (pure, no subprocess)
  +-- internal/dockerclient  Docker Engine API client (discovery + Phase 5 container lifecycle)
  +-- internal/services      DiscoveredService entity
  +-- internal/routes        Best-effort route inventory extraction
  +-- internal/graph         Application Graph nodes/edges + correlation
  +-- internal/planning      Deterministic (no-AI) test case rule engine
  +-- internal/testgen       Deterministic Playwright spec generation
  +-- internal/runs          Runner interface, TestRun tracking
  +-- internal/artifacts     Local-filesystem artifact storage
  +-- internal/providers     AI provider configuration, health checks, task routing
  +-- internal/secretstore   AES-256-GCM encryption for provider API keys
  +-- internal/redaction     Secret/token/credential scrubbing (not yet wired into the Phase 8 AI prompt, which sends only curated bug evidence, never raw repository content)
  +-- internal/settings      Generic key/value store (first use: AI task routing)
  +-- internal/failures      Deterministic failure classification + flaky assessment (spec §13)
  +-- internal/bugreports    Structured, deduplicated bug reports (spec §14)
  +-- internal/fixproposals  Candidate patches: pure-Go diff parser/applier, temp workspace, repo write (spec §15)
  +-- internal/auth          RBAC: users, sessions, fixed role->permission mapping (spec §19, opt-in)
  +-- internal/metrics       Hand-rolled Prometheus-format counters/gauges (spec §22)
  +-- internal/kubeclient    Hand-rolled read-only Kubernetes API client (spec §7.5, opt-in)
  +-- internal/kubediscovery Kubernetes resource discovery + pod/workload correlation (spec §7.5)
  +-- internal/httpserver    HTTP handlers
  |
  +-- PostgreSQL
  +-- Redis
  +-- Docker daemon (required for Phase 5 test execution; optional otherwise)
  +-- disposable Playwright runner containers (Phase 5)
  +-- AI provider APIs (Phase 6; optional, never required)
```

Future phases add sibling packages under `internal/` — `scheduler/` —
exactly as laid out in spec §5.

## Request flow

1. Browser loads `apps/web` on `:9090`.
2. Pages call same-origin `/api/*` paths. Each is a Next.js Route Handler
   (`app/api/**/route.ts`) that forwards to `sentinel-api` and streams the
   response back — the browser never talks to `sentinel-api` directly.
3. `apps/api` answers `/health` (liveness, no dependency checks), `/ready`
   (checks Postgres/Redis), and the `/api/v1/*` resource endpoints below.

## Domain flow: adding a project and running discovery (Phase 1)

```text
POST /api/v1/projects {name, repository_path}
  -> projects.ValidateRepositoryPath: resolve absolute, resolve symlinks,
     reject nonexistent/non-directory/system-root paths
  -> projects.Store.Create (slug auto-generated, uniqued)
  -> environments.Store.Create (default environment, classification=local)
  -> audit: project.added

POST /api/v1/projects/{id}/discover
  -> discovery.Scan(project.RepositoryPath)   [read-only, symlink-safe walk]
  -> discovery.Store.CompleteRun (findings persisted with evidence)
  -> projects.Store.SetDiscoveryStatus(completed)
  -> audit: repository.scanned

PATCH /api/v1/environments/{id} {classification}
  -> environments.RestrictForClassification: "production"/"unknown"
     force allow_mutations/allow_load_tests/allow_active_security_scan
     off unconditionally — the caller cannot set them in the same request
  -> audit: environment.classification_changed
```

Discovery is synchronous in Phase 1 (a normal repository scans in
milliseconds); `internal/discovery.Scan` has no dependency on the HTTP
layer, so moving it onto the async job system (spec §21) in a later phase
is a wiring change, not a rewrite.

### Running discovery via Docker Compose

`sentinel-api` scans `repository_path` on its *own* filesystem. When run
via `docker compose up`, that's the container's filesystem — so target
repositories must be mounted in. `docker-compose.yml` mounts
`./workspace` (gitignored) into the container at `/workspace:ro`; see
[docs/LOCAL_DEVELOPMENT.md](LOCAL_DEVELOPMENT.md#adding-a-project-repository-discovery).

### Docker Compose service discovery (Phase 2)

`POST /projects/{id}/discover` also runs this, right after the repository
scan:

```text
for each docker/docker_compose finding (from discovery.Scan):
  compose.ParseFile(path)              [pure YAML parse, no subprocess]
    -> []compose.Service (image, ports, depends_on, env var NAMES only, ...)
  services.FromCompose(...)            [ClassifyKind: image name -> high
                                         confidence; port/build presence ->
                                         medium confidence heuristic]
  if dockerClient reachable (Ping succeeds):
    ListContainers() -> match by com.docker.compose.service label
    -> services.ApplyRuntimeStatus(...)  [live state, container name, ports]
  else:
    service keeps metadata.status = "unknown" ("not observed", not "down")
services.Store.Upsert(...)             [keyed by (project_id, name) — idempotent]
  -> audit: service.discovered
```

`internal/dockerclient` talks to the Docker Engine API over
`/var/run/docker.sock` using exactly two endpoints (`/_ping`,
`/containers/json`) — not the full Docker SDK — and is never required:
`docker-compose.yml` does not mount the socket by default (see
[docs/DOCKER_DISCOVERY.md](DOCKER_DISCOVERY.md)).

### Application Graph (Phase 3)

Still within the same `POST /projects/{id}/discover` request, after
services are upserted:

```text
routes.Extract(root, findings)        [Next.js file conventions + OpenAPI
                                        paths -> high confidence; regex-
                                        matched Express/Go/Flask calls,
                                        via the same discovery.Walk used
                                        by repository scanning -> medium]
graph.Build(root, routes, services)
  depends_on edges  <- compose depends_on (high confidence)
  served_by edges   <- only if exactly ONE non-infra service exists
                        (ambiguous otherwise -> no edge, per spec §8.2)
  calls edges       <- literal fetch()/axios() URL in a page's source
                        matched against a known route path (medium)
graph.Store.ReplaceGraph(...)          [delete-then-insert per project,
                                         in one transaction — never
                                         accumulates duplicates]
  -> audit: graph.built
```

`GET /projects/{id}/graph` returns nodes and edges with real (resolved)
IDs; `Node.Key()` (`node_type|label`) is only used internally to resolve
an edge's endpoints before they have database IDs.

## Data model

- `schema_migrations` — tracks which migration files have been applied.
- `audit_events` — append-only; see `migrations/0001_init.sql`.
- `projects`, `environments`, `discovery_runs`, `discovery_findings` — see
  `migrations/0002_projects.sql`. `discovery_findings.evidence` is JSONB
  (`{"paths": [...], ...}`); `confidence` is constrained to
  `high`/`medium`/`low` and must never be presented as more certain than
  the detection method warrants (spec §8.2, §9.4).
- `discovered_services` — see `migrations/0003_discovered_services.sql`.
  Upserted by `(project_id, name)`; `metadata` carries env var *names*
  (never values), profiles, and live status when observed.
- `graph_nodes`, `graph_edges` — see `migrations/0004_application_graph.sql`.
  Replaced wholesale per project on every discovery run (no upsert-by-key;
  see `graph.PostgresStore.ReplaceGraph`).
- `test_cases` — see `migrations/0005_test_cases.sql`. Unique on
  `(project_id, natural_key)`; `CreateIfAbsent` never overwrites an
  existing row, so a user's edits/approval survive regeneration.
- `test_runs`, `artifacts` — see `migrations/0006_test_runs.sql`. One row
  per `POST /tests/{id}/run`; artifacts reference their run and store
  metadata only (checksum, MIME type, size, retention window) — bytes
  live on the local filesystem (`internal/artifacts.FileStore`).
- `secret_references`, `ai_providers`, `settings` — see
  `migrations/0007_ai_providers.sql`. `secret_references` holds only
  AES-256-GCM ciphertext + nonce, never plaintext; `ai_providers`
  references it by ID (`ON DELETE SET NULL`, so deleting a key never
  cascades into deleting the provider); `settings` is a generic
  key/value table, first used for `ai.task_routing`.
- `failures`, `bug_reports` — see `migrations/0008_failures_and_bugs.sql`.
  `failures` is one row per classified failed run (append-only, never
  updated). `bug_reports` is unique on `(project_id, test_case_id,
  failure_type)` — repeated failures of the same kind on the same test
  update the existing row (frequency, last_observed_at) via
  `ON CONFLICT ... DO UPDATE` rather than accumulating duplicates;
  `possible_duplicate_of_id` self-references another bug as an
  unconfirmed hint, never an automatic merge.
- `fix_proposals` — see `migrations/0009_fix_proposals.sql`. `unified_diff`
  is set once at creation and never edited in place — approving a
  proposal approves exactly that text, and `apply-repository` re-parses
  this same column, never a regenerated diff (spec §15.2 acceptance:
  "Applied files match approved diff exactly"). `repository_applied_at`
  is set at most once (checked atomically by the store), so a proposal
  can only ever be written to the real repository a single time.
- `users`, `sessions` — see `migrations/0010_auth.sql`. `password_hash`
  is bcrypt; `sessions.token_hash` is a SHA-256 hash of the bearer
  token — the raw token itself is never persisted anywhere, only
  returned once, at login. RBAC built on these tables is opt-in
  (`SENTINEL_AUTH_ENABLED`, default false); every other table is fully
  usable whether or not these two are ever populated.
- `kube_resources` — see `migrations/0011_kubernetes.sql`. Unique on
  `(project_id, namespace, kind, name)`, upserted the same way as
  `discovered_services`. `metadata` is JSONB holding per-kind detail
  (container images/resources/probes, service ports/selector, ingress
  hosts/backends, secret type) — never a Secret/ConfigMap's actual value,
  since `internal/kubeclient`'s types have no field that could carry one
  in the first place. Populated only when Kubernetes discovery is
  configured (`SENTINEL_KUBE_CONFIG_PATH` or in-cluster credentials);
  otherwise this table simply stays empty.

## Domain flow: running a test (Phase 5)

```text
POST /tests/{id}/run
  -> require approval_status == approved (403 otherwise)
  -> require an environment with base_url set (422 otherwise)
  -> runs.Store.Create (status=queued)
  -> testgen.GenerateSpec(test case, environment.base_url)  [deterministic, no AI]
  -> audit: test_run.started
  -> return 202 immediately; execution continues in a background goroutine

[background] executeRunAsync:
  -> runs.Store.UpdateStatus(running)
  -> Runner.Execute: write spec + playwright.config.ts into the run's
     workspace, create+start a disposable container (Docker-outside-of-
     Docker — see docs/RUNNER_ISOLATION.md), wait for exit, fetch logs,
     remove the container
  -> if a concurrent POST /runs/{id}/cancel already marked this
     cancelled, stop here (don't overwrite with passed/failed)
  -> save stdout/stderr as artifacts; Runner.CollectArtifacts finds any
     screenshot/video/trace Playwright wrote on failure; save those too
  -> Runner.Cleanup (removes the workspace directory) — called on EVERY
     exit path, including when Execute itself failed
  -> runs.Store.UpdateStatus(passed|failed, exit_code) — decided only by
     the runner's exit code, never AI (spec §2.4)
  -> audit: test_run.completed

POST /runs/{id}/cancel
  -> runs.Store.UpdateStatus(cancelled) immediately (optimistic)
  -> Runner.Cancel: stop the container by its DETERMINISTIC name
     ("sentinel-run-<runID>") — no in-memory state needed, works across
     goroutines/processes
  -> audit: test_run.cancelled
```

## Domain flow: classifying a failure and updating a bug report (Phase 7)

Runs inside the same `executeRunAsync` as above, BEFORE the run's status
flips to its terminal value — a client polling `GET /runs/{id}` must
never observe "failed" before the corresponding bug report exists:

```text
[background] executeRunAsync, only when the run's outcome is failed
(exit code != 0) or Runner.Execute itself returned an error:
  -> failures.Classify(stdout, stderr, exit_code)  [deterministic, no AI —
     pattern-matched signatures checked in a fixed order; unmatched ->
     failure_type "unknown", never left blank]
  -> failures.Store.Create (one row per run, append-only)
  -> graph.Store.Get(project_id) -> buildRelatedGraphPath: look for a
     graph edge into/out of the test's route (Phase 3's Application
     Graph) — omitted, not guessed, if the route isn't in the graph
  -> runs.Store.ListByTestCase -> failures.AssessFlakiness(history)
     [spec §13.2 policy; always attached, a flaky label never hides a bug]
  -> bugreports.Store.UpsertFromFailure, keyed by
     (project_id, test_case_id, failure_type):
       - no existing row -> INSERT (status=open, frequency=1), plus a
         possible_duplicate_of_id hint if another OPEN bug on a
         DIFFERENT test case shares the same failure_type
       - existing row -> UPDATE in place (frequency+1, evidence/root
         cause refreshed to latest); a resolved bug flips to reopened
         rather than silently absorbing the update
  -> audit: bug_report.created | bug_report.updated
```

`root_cause_hypothesis` is never presented as a confirmed fact: every
response and export carries an explicit
`root_cause_is_unverified_hypothesis: true` alongside it (spec §14
acceptance: "Root cause is clearly marked as hypothesis"). See
[docs/FAILURE_CORRELATION.md](FAILURE_CORRELATION.md) for the full
classification/severity/flaky-detection model.

## Domain flow: proposing and applying a fix (Phase 8)

```text
POST /bugs/{id}/fix-proposal {unified_diff?}
  -> given: fixproposals.ParseUnifiedDiff (validates before storing)
  -> omitted: loadTaskRouting -> providers.Store.Get(routed provider)
     -> secretstore.Store.Resolve (server-side only)
     -> providers.Completer.Complete (evidence-only prompt — no
        repository source is read; see docs/FIX_PROPOSALS.md)
     -> providers.ExtractUnifiedDiff -> fixproposals.ParseUnifiedDiff
        (a response with no valid diff fails here, never fabricated)
  -> fixproposals.Store.Create (status=pending_review, always — an
     AI-generated proposal is never auto-approved)
  -> audit: fix_proposal.created

POST /fix-proposals/{id}/apply-workspace
  -> projects.Store.Get -> repository_path
  -> fixproposals.ApplyToWorkspace: copy repository_path into a fresh
     dir under SENTINEL_FIX_WORKSPACES_DIR (skip .git/node_modules/...),
     parse the diff, apply every file's hunks there — the ORIGINAL
     repository is never touched by this step
  -> every target path checked with projects.WithinRoot before any
     write (a diff's path is untrusted — could escape via "../..")
  -> fixproposals.Store.RecordWorkspaceApplication(per-file results)
  -> audit: fix_proposal.applied_workspace

POST /fix-proposals/{id}/approve | /reject | /request-revision
  -> fixproposals.Store.UpdateApprovalStatus
  -> audit: fix_proposal.approved | .rejected | .revision_requested

POST /fix-proposals/{id}/apply-repository
  -> require approval_status == approved (403 otherwise)
  -> fixproposals.ApplyToRepository(repository_path, the SAME stored
     unified_diff — never regenerated) [same path-traversal check]
  -> fixproposals.Store.RecordRepositoryApplication — refuses (409) if
     repository_applied_at is already set; a proposal can be written to
     the real repository at most once
  -> audit: fix_proposal.applied_repository
```

See [docs/FIX_PROPOSALS.md](FIX_PROPOSALS.md) for the full model,
including why the AI path never reads repository source and the
regression-test trade-off (they run against the environment's live
`base_url`, not an ephemeral deployment of the patched workspace).

## Domain flow: configuring and testing an AI provider (Phase 6)

```text
POST /providers {type, name, base_url, model, api_key?, is_local, ...}
  -> if api_key given: secretstore.Store.Create (AES-256-GCM encrypt;
     requires SENTINEL_SECRET_ENCRYPTION_KEY — 503 otherwise)
  -> providers.Store.Create (stores only the resulting secret_reference_id,
     never the plaintext key)
  -> audit: provider.created

POST /providers/{id}/test
  -> providers.Store.Get
  -> if secret_reference_id set: secretstore.Store.Resolve (server-side
     only — this plaintext is never written to an HTTP response)
  -> providers.HealthChecker.Check: a type-specific "list models" request
     (e.g. GET {base_url}/api/tags for Ollama) — no repository or test
     content is ever sent
  -> providers.Store.UpdateHealth(status, checked_at)
  -> audit: provider.tested (status only, never the key or its message
     if it could contain request internals)

PATCH /providers/routing {routes: {task_type: provider_id}}
  -> validate each task_type (providers.ValidTask) and provider_id
     (providers.Store.Get) before writing anything
  -> settings.Store.Set("ai.task_routing", merged JSON map)
  -> audit: provider.routing_updated
```

Every `GET`/`POST`/`PATCH /providers*` response is built through
`toProviderResponse`, which has no field for the API key or
`secret_reference_id` — only a `has_api_key` boolean. There is no code
path in `internal/httpserver` that can accidentally leak one back out.

## Domain flow: authenticating and enforcing RBAC (Phase 9)

Opt-in — every step below is skipped entirely when
`SENTINEL_AUTH_ENABLED` is unset (the default):

```text
startup:
  -> auth.EnsureBootstrapAdmin: no-op unless zero users exist AND
     SENTINEL_ADMIN_EMAIL/PASSWORD are both set

POST /auth/login {email, password}
  -> auth.Store.GetUserByEmail -> auth.VerifyPassword (bcrypt)
  -> the SAME generic "invalid_credentials" error either way — a caller
     can never tell "wrong password" from "no such account"
  -> auth.GenerateToken (256 bits) -> auth.Store.CreateSession(hash only)
  -> audit: auth.login
  -> return the raw token ONCE; only its hash is ever stored again

[middleware, on every /api/v1 request]
requireAuth:
  -> extract "Authorization: Bearer <token>"; missing/invalid -> 401
  -> auth.Store.GetSessionByTokenHash -> auth.Store.GetUserByID
  -> attach the resolved user to the request context

requirePermission(perm), applied per-route (e.g. approve_repository_patches
  on POST /fix-proposals/{id}/apply-repository):
  -> auth.HasPermission(user.Role, perm) -> 403 if the role doesn't grant it

POST /auth/logout
  -> auth.Store.DeleteSession — the presented token stops working immediately

POST /api/v1/users {email, password, role}  (requires manage_users, i.e. Administrator)
  -> auth.HashPassword -> auth.Store.CreateUser -> audit: user.create
  -> this is the only way to add accounts beyond the bootstrap administrator
GET /api/v1/users  (requires manage_users)
  -> auth.Store.ListUsers, ordered by email
```

Every mutating route spec §19's example permission table names is gated
this way; routes it doesn't mention (e.g. `POST /projects`) require only
that *some* authenticated user is making the request, not a specific
role. See [docs/APPROVAL_MODEL.md](APPROVAL_MODEL.md) for how this
composes with the test-case and fix-proposal approval gates that predate
RBAC and work identically whether or not it's turned on.

## Domain flow: discovering a Kubernetes cluster (Phase 10)

Opt-in — every step below is skipped (`Kube` is `nil` in `Dependencies`)
unless `SENTINEL_KUBE_CONFIG_PATH` is set or the process is running
in-cluster:

```text
startup:
  -> kubeclient.Detect(kubeConfigPath):
       kubeConfigPath != ""        -> kubeclient.LoadKubeconfig (token or
                                       client-cert auth only)
       KUBERNETES_SERVICE_HOST set -> kubeclient.LoadInCluster (projected
                                       ServiceAccount token + CA cert)
       neither                     -> kubeclient.ErrNotConfigured (Kube
                                       stays nil; every kube-* route 503s)

POST /projects/{id}/kube-discover
  -> kubediscovery.Discover(ctx, api, namespace):
       lists Namespaces (only when namespace == "", i.e. cluster-wide),
       Deployments/StatefulSets/DaemonSets (+ Pods, for restart-count
       correlation by label selector), Jobs, CronJobs, Services,
       Ingresses, Gateways (best-effort), ConfigMaps, Secrets (names
       only — see SecretSummary/ConfigMapSummary in internal/kubeclient)
     -> a kind that 403s (RBAC) or 404s (Gateway API not installed)
        becomes a Warning, never a failed request
  -> deps.KubeResources.Upsert per resource, keyed by
     (project_id, namespace, kind, name)
  -> audit: kubernetes.discovered

GET /projects/{id}/kube/events, GET /projects/{id}/kube/pods/{pod}/logs
  -> live proxies to kubeclient.ListEvents/PodLogs — never persisted,
     never a stream/follow, logs capped at kubeclient.MaxLogTailLines
```

See [docs/KUBERNETES_DISCOVERY.md](KUBERNETES_DISCOVERY.md) for the full
detection list and the read-only ClusterRole example
(`deploy/k8s/read-only-clusterrole.yaml`).

## Domain flow: the WebSocket adapter (Phase 11)

The one Phase 11 tool actually implemented — see
[docs/TEST_ADAPTERS.md](TEST_ADAPTERS.md) for why the other seven
(Maestro/Detox/k6/ZAP/Nuclei/Schemathesis/Pact/Kafka) are documented as
deferred rather than built:

```text
repository scan finds a "ws://"/"wss://" URL literal in any scanned
source file (JS/TS/Go/Python)
  -> routes.Route{Kind: KindWebSocket, Path: <the full URL>}
  -> graph.Build passes it through unchanged (NodeType is just r.Kind)

planning.GeneratePlan
  -> KindWebSocket -> one CategoryConnectivity TestCase,
     Framework: "websocket", RoutePath: <the full URL>

POST /tests/{id}/run
  -> runnerFor(tc.Framework) picks deps.WebSocketRunner, not deps.Runner
  -> "websocket" framework skips the environment base_url requirement
     entirely (RoutePath is already a complete URL)
  -> testgen.GenerateSpec dispatches on Framework -> a plain Node.js
     script (globally-installed "ws" package, no Playwright)
  -> DockerWebSocketRunner.Execute: same disposable-container isolation
     as DockerPlaywrightRunner, pointed at a much smaller image
     (deploy/docker/Dockerfile.runner-websocket: node:20-alpine + ws,
     no browser stack)
  -> pass/fail is the script's own process.exit(0)/(1) — connected and
     received a message within 5s, or didn't
```

## Configuration

All configuration is environment-variable based (`internal/config`), with
validation at startup and no silent defaults for security-relevant values
(e.g. there is no default database DSN — it must be set explicitly). See
`.env.example` for the full list.

## What's still not implemented

Per spec §2.2 and §34, there is still no code path that:

- deploys E2E Sentinel itself via Helm (spec §24.5) — Kubernetes
  *discovery* (Phase 10) is implemented, read-only, and optional; Helm
  packaging of this project's own services is a separate, later concern,
- commits, pushes, or creates a branch/PR in git — Phase 8 can write
  files directly to a repository_path after approval, but has no git
  plumbing at all (spec §15.3's "dedicated branch"/"push"/"PR creation"
  safeguards apply to a git-integration feature not yet built),
- executes anything found inside a scanned repository — discovery only
  *reads* file names/contents to classify them, it never runs them,
- reads repository source files to give an AI provider real code
  context — Phase 8's AI-assisted fix generation prompts with only a
  bug's already-curated evidence (see docs/FIX_PROPOSALS.md); a safe
  repository-content pipeline (path allowlist + redaction, building on
  Phase 6's internal/redaction) is reserved for later,
- authenticates by anything other than local email/password — OIDC/SAML
  (spec §19) are architecturally accommodated (`auth.Store` is a plain
  interface) but not implemented,
- traces a request across packages — `internal/metrics` covers counters/
  gauges (spec §22 "Metrics"); OpenTelemetry distributed tracing (spec
  §22 "Traces") is a documented ceiling, not attempted.
- integrates Maestro, Detox, k6, ZAP, Nuclei, Schemathesis, Pact, or
  Kafka/event-stream testing (spec §25 Phase 11) — the WebSocket adapter
  is the one Phase 11 tool actually implemented; see
  [docs/TEST_ADAPTERS.md](TEST_ADAPTERS.md) for exactly why each of the
  other seven is a bigger undertaking than a drop-in `Runner`
  implementation (a new external runtime, a different integration shape,
  or new `TestCase` fields a smoke-test model doesn't have).

These are reserved extension points for later phases.
