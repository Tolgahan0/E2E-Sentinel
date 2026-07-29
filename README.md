# E2E Sentinel

E2E Sentinel is a self-hosted, AI-assisted quality engineering platform. It
discovers a repository and its runtime architecture, produces a reviewable
test inventory and risk-based E2E test plan, generates and executes tests
in isolated runners, correlates failures across layers, and proposes fixes
as reviewable diffs — never changing source code, infrastructure, or data
without explicit approval.

> E2E Sentinel analyzes repository structure, runtime services, API
> schemas, application routes, existing tests, and observed behavior to
> generate high-confidence test recommendations and evidence-backed
> failure reports.

This repository is being built incrementally against
[`E2E_SENTINEL_CODEX_MASTER_SPEC.md`](E2E_SENTINEL_CODEX_MASTER_SPEC.md).
**Phases 0–9** are implemented; see [docs/ROADMAP.md](docs/ROADMAP.md)
for what comes next and [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) /
[ADR 0001](docs/adr/0001-phase0-foundation.md) for the reasoning behind
the foundational choices.

## What's implemented

- Go API (`apps/api`) with structured logging, environment-based config,
  a PostgreSQL-backed audit log, and `/health` / `/ready` / `/api/v1/audit-events`.
- **Projects & repository discovery**: add a project by absolute path
  (validated — must exist, must be a directory, cannot be a system root,
  symlinks resolved before any check), then run a deterministic scan that
  detects languages, frameworks, Docker files, CI pipelines, existing test
  tooling (Playwright/Cypress/Maestro/Detox/Postman), and API schemas —
  each finding carries file-path evidence and a confidence level.
- **Environments**: every project gets a default environment; classifying
  it `production` or `unknown` forces mutation/load-test/security-scan
  permissions off in the same request (spec §2.6).
- **Docker Compose service discovery**: compose files found during
  discovery are parsed directly (no `docker compose` subprocess) into
  declared services — image, ports, dependencies, env var *names* only.
  If the Docker daemon is reachable (never required, and not mounted by
  default), services are enriched with live running status; otherwise
  they show as "not observed", never as a false "not running".
- **Application Graph**: routes are extracted (Next.js file conventions,
  OpenAPI paths, regex-matched Express/Go/Flask router calls) and
  correlated with discovered services into an evidence-backed graph —
  e.g. `Login Page --calls--> POST /api/v1/auth/login --served_by--> api`.
  Every edge carries its evidence and a confidence level; ambiguous
  relationships (e.g. more than one candidate service) produce no edge
  rather than a guess.
- **Test planning**: risk-scored (P0–P3) test case suggestions generated
  from extracted routes using fixed, deterministic rules — no AI call is
  made or possible. Mutating tests default to production-unsafe;
  approving one is blocked (403) while the project has a production or
  unknown-classified environment. Suggestions can be approved, rejected,
  or edited, and regenerating a plan never overwrites a test the user
  already reviewed.
- **Test execution**: an approved test case runs in its own disposable,
  resource-limited Docker container — a real Chromium browser for page
  tests, an HTTP request for API tests. The generated spec is templated
  deterministically (no AI). Pass/fail comes only from the container's
  exit code. Failed runs capture stdout/stderr, a screenshot, a video,
  and a Playwright trace automatically; runner containers are removed
  after every run, including failures. Cancellation stops the container
  by a deterministic name, so it works even though execution runs in a
  background goroutine.
- **AI Providers**: configure Ollama, OpenAI, Anthropic, Gemini, Azure
  OpenAI, or an OpenAI-compatible endpoint; test connectivity live; route
  individual AI-assisted task types (test planning, failure analysis,
  fix generation, etc.) to different providers. API keys are encrypted
  at rest and never returned through the API — only whether one is
  configured. The app is fully usable with zero providers configured
  (spec §16.6 "No-AI Mode"); Phase 8's fix generation is the first
  feature that actually calls a routed provider.
- **Failure analysis & bug reports**: a failed test run is automatically
  classified (17 failure types, a fixed severity mapping, a flaky-test
  assessment) and turned into a structured, evidence-backed bug report —
  deterministically, no AI call involved. Repeated failures of the same
  kind on the same test update one bug (frequency/last-observed) instead
  of spawning duplicates; a different test with the same failure type
  gets an unconfirmed "possible duplicate" hint. Root cause is always
  presented as an explicitly-labeled hypothesis, never a confirmed
  diagnosis. Bugs can be searched/filtered, resolved, reopened, annotated,
  and exported as Markdown or JSON.
- **Fix proposals**: a candidate unified diff for a bug — either pasted
  manually or (evidence-only, no repository source read) generated by
  whichever provider is routed for `fix_generation`. The AI can never
  apply anything itself: every proposal starts `pending_review`, can be
  tested by applying it to a disposable copy of the repository, and only
  reaches the real repository through an explicit `approve` followed by
  `apply-repository` — which re-parses the exact diff that was approved
  and can run at most once per proposal. A path-traversal check runs
  before every write, for both the temporary workspace and the real
  repository.
- **Production hardening**: opt-in RBAC (`SENTINEL_AUTH_ENABLED`, off by
  default) with five roles, bcrypt/bearer-token auth, and a fixed
  permission mapping gating the mutating routes spec §19 calls out;
  security headers, per-IP rate limiting, and a CSRF header check on
  every response/mutating request; searchable, still-immutable audit
  events; a retention sweep for expired artifacts; a `/metrics`
  endpoint; a threat-model table; and `make scan` (govulncheck + npm
  audit, also in CI) for dependency scanning.
- A Next.js panel (`apps/web`) on port `9090`: Dashboard, Audit Logs,
  Projects, Discovery (findings + services), Application Map, Test
  Inventory, Runs, AI Providers, Bugs, and Fix Proposals are functional;
  the remaining nav sections are stub pages pointing at the phase that
  implements them.
- Versioned SQL migrations with a small custom runner.
- Docker Compose stack: `sentinel-api`, `sentinel-web`, `postgres`,
  `redis`, plus disposable Playwright runner containers launched on
  demand.

Nothing yet talks to Kubernetes, and there's no git integration
(commits, branches, pushes, PRs) — those are reserved for later phases.
Discovery still only *reads* file names/contents to classify them. Test
execution runs the generated spec in full isolation — see
[docs/RUNNER_ISOLATION.md](docs/RUNNER_ISOLATION.md) for exactly what
that container can and can't do. Fix proposals are the one place E2E
Sentinel *does* write to a target repository's files directly — never
without an explicit prior approval, and only once per proposal — see
[docs/FIX_PROPOSALS.md](docs/FIX_PROPOSALS.md). See
[docs/AI_PROVIDER_GUIDE.md](docs/AI_PROVIDER_GUIDE.md) for configuring an
AI provider.

## Quick start

```bash
cp .env.example .env
# edit .env and set POSTGRES_PASSWORD

make up          # builds the Playwright runner image, then docker compose up -d --build
```

Then open <http://localhost:9090>. `make up` (not `docker compose up`
directly) is required from Phase 5 on — it computes the host path
`sentinel-api` needs to launch disposable test-runner containers. Note:
this mounts the Docker socket into `sentinel-api` — see
[docs/RUNNER_ISOLATION.md](docs/RUNNER_ISOLATION.md) for what that means
and why.

```bash
make down        # stop the stack
```

See [docs/LOCAL_DEVELOPMENT.md](docs/LOCAL_DEVELOPMENT.md) for running the
API and web app outside Docker, and [docs/DEPLOYMENT.md](docs/DEPLOYMENT.md)
for deployment notes.

## Repository layout

```text
apps/api/         Go backend (cmd/sentinel, internal/*)
apps/web/         Next.js panel
migrations/       Versioned SQL migrations
deploy/docker/    Dockerfiles
tests/integration/  Black-box tests against a running stack
docs/             Architecture, security, operational docs
```

## Safety model

E2E Sentinel starts in read-only observation mode and requires explicit,
action-specific, time-bounded, auditable approval before anything mutating
happens (writing generated tests, applying patches, restarting services,
pushing branches, etc.). See [docs/SECURITY_MODEL.md](docs/SECURITY_MODEL.md)
and [SECURITY.md](SECURITY.md).

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md).
