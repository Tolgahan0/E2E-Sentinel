# AGENTS.md — orientation for any AI coding agent working in this repo

This file is the entry point for an AI-based CLI or IDE assistant (Claude
Code, Cursor, Codex, Windsurf, Copilot, etc.) opening this repository for
the first time. Read this before touching code. It tells you what the
project is, where everything lives, what's already decided, and how to
verify your own work the same way every phase of this project has been
verified so far.

## What this is

E2E Sentinel is a self-hosted, AI-assisted quality engineering platform:
it discovers a repository and its runtime architecture, produces a
reviewable test inventory and risk-based E2E test plan, generates and
executes tests in isolated runners, correlates failures across layers,
and proposes fixes as reviewable diffs — **never** changing source code,
infrastructure, or data without explicit human approval.

The full specification is
[`E2E_SENTINEL_CODEX_MASTER_SPEC.md`](E2E_SENTINEL_CODEX_MASTER_SPEC.md)
at the repo root. It is the ground truth for scope and acceptance
criteria — if this file and the spec ever disagree, the spec wins and
this file is stale.

**Current status: Phases 0–11 (all spec phases) are implemented.**
Phase 11 is intentionally partial — see
[`docs/TEST_ADAPTERS.md`](docs/TEST_ADAPTERS.md) for exactly what's
built versus explicitly documented as deferred, and why. Every other
phase is fully implemented, tested, and live-verified. See
[`docs/ROADMAP.md`](docs/ROADMAP.md) for the phase-by-phase delivery
log — read it before assuming something is missing; it usually explains
what exists and what's a deliberate, documented ceiling.

## Repository map

```text
.
├── E2E_SENTINEL_CODEX_MASTER_SPEC.md   the spec — ground truth
├── AGENTS.md                            this file
├── README.md                            human-facing project overview
├── docker-compose.yml, Makefile         local stack (see below)
├── migrations/                          versioned SQL, one file per phase
├── deploy/
│   ├── docker/Dockerfile.*              API/web/runner images
│   └── k8s/read-only-clusterrole.yaml   example RBAC for Phase 10
├── apps/
│   ├── api/                             Go backend
│   │   ├── cmd/sentinel/main.go         composition root — wires every Dependencies field
│   │   └── internal/<package>/          one package per domain concept (see below)
│   └── web/                             Next.js panel (App Router, TS strict), port 9090
├── tests/integration/                   black-box HTTP tests against a live `docker compose up` stack
└── docs/                                one file per subsystem — index below
```

`apps/api/internal/` packages, roughly in the order a request touches
them: `config` → `projects`/`environments` → `discovery` (repo scan) →
`compose`/`services`/`dockerclient` (Docker Compose discovery) →
`routes`/`graph` (application graph) → `planning`/`testgen` (test
planning + generation) → `runs`/`artifacts` (execution + evidence) →
`failures`/`bugreports` (failure correlation) → `providers`/
`secretstore`/`redaction` (AI gateway, all optional) → `fixproposals`
(patch proposals) → `auth`/`metrics` (Phase 9 hardening) →
`kubeclient`/`kubediscovery` (Phase 10) → `httpserver` (all HTTP
handlers + route wiring, `server.go`'s `Dependencies` struct is the
single source of truth for what's wired to what).

## Docs index — read the relevant one before working in that area

| File | What's in it |
|---|---|
| [README.md](README.md) | The end-user walkthrough (moved here from a separate doc so GitHub renders it as the repo landing page) — pipeline flowchart, one section per panel page, a data-lineage diagram, and the test-case/run status lifecycle. **Start here if the question is "how does a person actually use this."** |
| [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) | Package tree, data model (every table), domain-flow walkthroughs for each major feature, config reference. **Start here for "how does X actually work end to end."** |
| [docs/ROADMAP.md](docs/ROADMAP.md) | Phase-by-phase delivery log: what was built, what was verified, acceptance criteria per phase. **Start here for "is X already done."** |
| [docs/TEST_ADAPTERS.md](docs/TEST_ADAPTERS.md) | Phase 11: WebSocket adapter (done) vs. Maestro/Detox/k6/ZAP/Nuclei/Schemathesis/Pact/Kafka (deferred, with the extension pattern to follow). |
| [docs/KUBERNETES_DISCOVERY.md](docs/KUBERNETES_DISCOVERY.md) | Phase 10: what's detected, RBAC, connection modes, configuration. |
| [docs/DOCKER_DISCOVERY.md](docs/DOCKER_DISCOVERY.md) | Phase 2: Compose parsing + live container status, socket-mount trade-offs. |
| [docs/RUNNER_ISOLATION.md](docs/RUNNER_ISOLATION.md) | Phase 5: disposable-container test execution, Docker-outside-of-Docker mechanics, the `Runner` interface. |
| [docs/TEST_GENERATION.md](docs/TEST_GENERATION.md) | Planning → generation pipeline, what a generated test actually asserts (and doesn't). |
| [docs/APPROVAL_MODEL.md](docs/APPROVAL_MODEL.md) | Every human-approval gate in the system (test plans, mutating tests, repository patches) — read this before adding anything that acts without approval. |
| [docs/FAILURE_CORRELATION.md](docs/FAILURE_CORRELATION.md) | Phase 7: deterministic failure classification, bug report dedup. |
| [docs/FIX_PROPOSALS.md](docs/FIX_PROPOSALS.md) | Phase 8: diff parsing/applying, workspace vs. repository writes, one-shot semantics. |
| [docs/AI_PROVIDER_GUIDE.md](docs/AI_PROVIDER_GUIDE.md) | Phase 6: provider gateway, No-AI Mode, which features need AI (almost none). |
| [docs/SECURITY_MODEL.md](docs/SECURITY_MODEL.md) | Phase 9 and later: RBAC, headers, rate limiting, CSRF, dependency scanning — current, honest state including known findings. |
| [docs/THREAT_MODEL.md](docs/THREAT_MODEL.md) | Every spec §23.1 threat area mapped to its actual mitigation and phase. |
| [docs/OPERATIONS.md](docs/OPERATIONS.md) | Health/readiness, logs, audit trail, retention, backups. |
| [docs/DEPLOYMENT.md](docs/DEPLOYMENT.md) | Docker Compose single-host deployment shape. |
| [docs/LOCAL_DEVELOPMENT.md](docs/LOCAL_DEVELOPMENT.md) | Prerequisites, running the stack locally. |
| [docs/TROUBLESHOOTING.md](docs/TROUBLESHOOTING.md) | Common setup failures and fixes. |
| [docs/adr/](docs/adr/) | Architecture Decision Records — read before reversing a past decision. |

## How this project has been built — the rules that matter

These are load-bearing conventions, not style preferences. Follow them
or you will contradict ~90k lines of existing code and tests:

1. **Deterministic-first, AI is optional and never gates pass/fail.**
   Discovery, planning, and test generation never require AI. Pass/fail
   for a test run comes only from the runner process's exit code —
   never an AI judgment call. AI, when used, is scoped to narrow tasks
   (fix-diff generation) and every AI output is still gated by human
   approval before it can do anything.
2. **Safe default, explicit capability.** Every optional integration
   (Docker socket, AI provider keys, RBAC, Kubernetes, the second test
   runner) defaults to **off/absent**, and the system is fully usable
   without it. Wiring pattern: a nullable field on `httpserver.Dependencies`
   with a doc comment saying what nil means, checked once at the top of
   each handler that needs it. Follow this exact pattern for a new
   capability — don't invent a different one (e.g. a generic registry)
   without a strong reason; see `runs_handlers.go`'s `Runner`/
   `WebSocketRunner` fields for the precedent when a capability needs
   more than one implementation.
3. **Never shell out.** Docker Compose files, unified diffs, kubeconfig
   YAML — all parsed in Go, never via `docker compose config`, `git
   apply`, or similar subprocess calls. This closes an entire class of
   command-injection risk (spec §23.3) and is treated as non-negotiable
   throughout the codebase.
4. **Hand-roll minimal clients over heavy SDKs.** `internal/dockerclient`
   and `internal/kubeclient` implement only the handful of REST calls
   actually needed, not the full Docker SDK / `client-go`. Same logic
   applies to `internal/metrics` (no `client_golang`). If you're tempted
   to add a large dependency for a narrow need, hand-roll it instead,
   matching this precedent.
5. **Confidence levels are honest, always.** Every discovered fact
   (a route, a service, a Kubernetes resource) carries a confidence
   level (`high`/`medium`/`low`) and evidence (source file, pattern
   matched). Never present a regex-matched guess as equally certain as
   an explicit declaration (spec §9.4, §36).
6. **Audit everything that mutates state**, via `internal/audit` — and
   the audit log itself has no mutating route at any HTTP verb (there's
   a dedicated test proving this; keep it true).
7. **Test conventions**: every package has unit tests using an in-memory
   fake for its external dependency (a fake `ContainerClient`, a fake
   `KubeAPI`, a fake HTTP server via `httptest`) — never a mock
   framework. `tests/integration/` is separate: black-box HTTP tests
   against a real running `docker compose up` stack, which must *skip*
   (not fail) when no stack is reachable.
8. **Commit messages document what was found, not just what was built**
   — several past phases surfaced and fixed real bugs during live
   verification (not caught by unit tests); the commit messages explain
   them. Do the same: if live-verifying your change surfaces a bug, fix
   it and say so in the commit, don't silently patch over it.

## How to verify your own work before calling it done

This mirrors exactly how every phase in this project was verified —
don't skip steps.

**1. Backend unit tests + build:**
```bash
cd apps/api
go build ./... && go vet ./...
go test ./... -count=1 -cover
```
All packages must pass. Coverage regressions on a touched package are a
signal to add tests, not to ignore.

**2. Web unit-level checks:**
```bash
cd apps/web
npm run typecheck && npm run lint && npm run build
```

**3. Live-verify against the real stack** (this project's standard bar —
unit tests alone are not considered "done"):
```bash
# first time / after a Dockerfile or dependency change:
make up
# subsequent restarts after a code change:
export SENTINEL_RUNNER_HOST_WORKSPACE_DIR="$(pwd)/runner-workspaces"
docker compose up -d --build
```
Then exercise the actual HTTP surface you changed with `curl` (every
prior phase did this — see `docs/ROADMAP.md`'s per-phase sections for
concrete examples of exactly what was curled and what response proved
the feature worked). Check `docker logs e2e-sentinel-sentinel-api-1`
for startup errors or unexpected warnings.

**4. Integration suite against the live stack:**
```bash
cd tests/integration
SENTINEL_INTEGRATION_BASE_URL=http://localhost:8080 go test ./... -count=1 -v
```
This must skip cleanly (not fail) if the stack isn't running, and pass
if it is.

**5. Clean up any fixture data you created** (test projects, temporary
containers, scratch files under `workspace/`) before finishing — this
repo's `workspace/` and the database accumulate fixture projects from
past phases' live verification; don't leave new clutter either.

**6. Open the web panel and click through the feature you touched** —
`http://localhost:9090` once the stack is up. A curl-only check proves
the API works; it doesn't prove the panel renders it correctly.

## Making changes

- Match the phase-gated discipline this project was built with: implement
  → unit test → live-verify → document → commit. Don't skip the
  live-verify or docs step even for a "small" change — this codebase's
  reliability comes from that discipline being applied uniformly, not
  selectively.
- Update the relevant `docs/*.md` file(s) when you change behavior they
  describe — stale docs are worse than no docs, and an agent (including
  a future one, including yourself in a later session) will trust them.
- Only commit when explicitly asked to, and never force-push.
