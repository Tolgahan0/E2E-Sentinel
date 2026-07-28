# E2E Sentinel — Codex Master Implementation Specification

## 0. Document Purpose

This document is the authoritative implementation specification for **E2E Sentinel**.

E2E Sentinel is a self-hosted, AI-assisted quality engineering platform that:

- Discovers a repository and its runtime architecture.
- Detects Docker Compose and Kubernetes workloads.
- Identifies applications, services, routes, APIs, databases, queues, workers, WebSocket endpoints, and existing test infrastructure.
- Produces a reviewable test inventory and risk-based E2E test plan.
- Generates and executes tests in isolated runners.
- Collects screenshots, videos, traces, HAR files, console output, service logs, container logs, pod logs, and test artifacts.
- Correlates failures across frontend, API, infrastructure, database, queue, and runtime layers.
- Generates structured bug reports.
- Proposes code fixes as reviewable diffs.
- Never changes source code, infrastructure, deployments, or data without explicit user approval.
- Supports local and external AI providers through a small management panel.
- Runs locally through a web panel, initially at `http://localhost:9090`.

The implementation must prioritize:

1. Safety
2. Deterministic test execution
3. Clear approval boundaries
4. Extensibility
5. Self-hosted operation
6. Provider-independent AI integration
7. Auditability
8. Production readiness

---

# 1. Product Definition

## 1.1 Product Name

**E2E Sentinel**

## 1.2 Product Positioning

E2E Sentinel is not only a Playwright wrapper or an AI test generator.

It is an infrastructure-aware, repository-aware, runtime-aware, approval-gated quality engineering platform.

Core positioning:

> E2E Sentinel discovers an application's architecture, identifies high-value user journeys and technical risks, generates and executes isolated E2E tests, correlates failures across application and infrastructure layers, and proposes fixes only after explicit approval.

## 1.3 Primary Users

- Software developers
- QA engineers
- DevOps engineers
- Platform engineers
- SRE teams
- Small product teams without a dedicated QA department
- Enterprises requiring self-hosted testing
- Teams running Docker Compose or Kubernetes workloads

## 1.4 Initial Product Scope

The first production-capable release must focus on:

- Local repository discovery
- Docker Compose discovery
- Web application discovery
- REST/OpenAPI discovery
- Playwright-based browser E2E
- API smoke and schema tests
- AI-assisted test planning
- Isolated execution
- Evidence collection
- Bug reporting
- Approval-gated fix proposals
- Local AI and external AI provider configuration
- Local web panel on port `9090`

Kubernetes, mobile, advanced event-stream testing, Git provider integrations, and autonomous remediation must be implemented incrementally after the core is stable.

---

# 2. Non-Negotiable Engineering Rules

Codex must obey all rules in this section.

## 2.1 Repository-First Rule

Before changing any code:

1. Inspect the entire repository structure.
2. Detect existing languages, frameworks, conventions, linters, test tools, CI workflows, Docker files, Kubernetes files, and documentation.
3. Read existing architecture and contribution documents.
4. Reuse existing abstractions where appropriate.
5. Do not replace working systems unnecessarily.
6. Do not duplicate existing implementations.
7. Produce a short repository assessment before implementation.

## 2.2 No Destructive Defaults

The platform must start in read-only observation mode.

By default, E2E Sentinel must not:

- Modify repository files
- Commit code
- Push branches
- Open pull requests
- Restart services
- Restart containers
- Restart pods
- Apply Kubernetes resources
- Change Docker Compose definitions
- Modify databases
- Run database migrations
- Delete test data
- Execute destructive production requests
- Read or display secret values
- Run active security scans against production
- Run load tests against production

## 2.3 Explicit Approval Requirement

The following actions require explicit approval:

- Writing generated tests into the repository
- Applying a source-code patch
- Creating a branch
- Creating a commit
- Pushing to a remote
- Opening a pull request
- Restarting a service
- Restarting a deployment
- Applying a Kubernetes manifest
- Changing environment configuration
- Running mutating tests
- Running load tests
- Running active security scans
- Modifying test or application data

Approval must be:

- Action-specific
- Time-bounded
- Audited
- Revocable
- Visible in the UI

A general approval must not silently authorize unrelated future actions.

## 2.4 AI Is Advisory

AI may:

- Explain architecture
- Suggest test cases
- Generate test drafts
- Analyze failures
- Suggest likely root causes
- Generate patch proposals
- Summarize logs
- Recommend regression scope

AI must not be the source of truth for pass/fail status.

Pass/fail must come from deterministic assertions, process exit codes, validated schemas, health checks, or explicit runner results.

## 2.5 Secrets Must Never Be Exposed

Never send secrets to an AI provider.

The platform must redact:

- API keys
- Passwords
- Authorization headers
- Cookies
- Access tokens
- Refresh tokens
- Private keys
- Database connection strings
- Secret environment variables
- Kubernetes Secret values
- Personal data where possible

Secret names may be indexed. Secret values must not be persisted in plaintext.

## 2.6 Production Safety

Production must be detected and treated as restricted.

Production defaults:

- Read-only checks only
- No destructive HTTP methods
- No active security scanning
- No load testing
- No database mutations
- No generated test users
- No automatic fixes
- No automatic deployment
- No service restart
- No traffic replay containing sensitive data

A production override must require explicit administrator approval and a clear warning.

## 2.7 Audit Everything

All meaningful operations must generate an audit event:

- Project added
- Repository scanned
- Service discovered
- AI provider configured
- Test plan generated
- Test approved
- Test executed
- Artifact viewed
- Fix generated
- Fix approved
- Patch applied
- Runner started
- Runner stopped
- Credentials changed
- Environment classification changed
- Restricted action attempted
- Permission denied

---

# 3. Product Modes

The platform must expose four operational modes.

## 3.1 Observe Mode

Allowed:

- Read repository files
- Detect stack
- Parse manifests
- Read Docker metadata
- Read Kubernetes metadata
- Read logs with permission
- Detect routes and schemas
- Build service graph
- Generate test recommendations

Not allowed:

- Execute browser tests
- Execute API tests
- Write files
- Change runtime state

## 3.2 Test Mode

Includes Observe Mode and additionally allows:

- Execute approved tests
- Start isolated test runners
- Create temporary test artifacts
- Send approved requests to approved environments
- Use approved test credentials
- Capture screenshots, video, traces, HAR, and logs

## 3.3 Fix Proposal Mode

Includes Test Mode and additionally allows:

- Generate candidate patches
- Show diffs
- Explain risk
- Recommend regression tests

It must not apply patches.

## 3.4 Approved Fix Mode

Allows only explicitly approved changes:

- Create a local branch
- Apply an approved patch
- Execute regression tests
- Create an optional local commit
- Optionally push or open a PR when Git integration is configured and separately approved

---

# 4. High-Level Architecture

Use a modular monolith for the first production-ready version.

Do not create unnecessary microservices during MVP.

Recommended top-level architecture:

```text
Browser
  |
  v
E2E Sentinel Web UI :9090
  |
  v
E2E Sentinel API
  |
  +-- Project Discovery
  +-- Runtime Discovery
  +-- Application Graph
  +-- Test Inventory
  +-- Test Planning
  +-- AI Gateway
  +-- Approval Engine
  +-- Execution Orchestrator
  +-- Artifact Manager
  +-- Failure Analyzer
  +-- Fix Proposal Engine
  +-- Audit Log
  +-- Scheduler
  |
  +-- PostgreSQL
  +-- Redis / Job Queue
  +-- Local or S3-Compatible Artifact Storage
  |
  +-- Disposable Runner Containers
        +-- Playwright Runner
        +-- API Runner
        +-- Generic Runner
```

## 4.1 Recommended Technology Stack

### Backend

Preferred:

- Go
- Chi or Gin
- PostgreSQL
- Redis
- Asynq for jobs during MVP
- Server-Sent Events or WebSocket for live execution logs
- Docker Engine SDK
- Kubernetes `client-go`
- OpenTelemetry

Use Temporal only if existing repository architecture already uses it or if workflow complexity later justifies it.

### Frontend

Preferred:

- Next.js
- TypeScript
- React
- React Flow for architecture graph
- Monaco Editor for generated test and diff review
- xterm.js for streaming logs
- Accessible component primitives
- Responsive layout

### Runner

- Playwright
- Node.js
- Disposable Docker container per test run
- Read-only repository mount where possible
- Separate writable workspace for generated files and artifacts

### Storage

MVP:

- PostgreSQL for structured metadata
- Local filesystem for artifacts
- Optional MinIO support

Later:

- Amazon S3
- Azure Blob Storage
- Google Cloud Storage

---

# 5. Repository Structure

If starting from an empty repository, use a structure similar to:

```text
e2e-sentinel/
├── apps/
│   ├── api/
│   └── web/
├── cmd/
│   └── sentinel/
├── internal/
│   ├── approval/
│   ├── audit/
│   ├── auth/
│   ├── config/
│   ├── discovery/
│   │   ├── repository/
│   │   ├── docker/
│   │   ├── kubernetes/
│   │   ├── routes/
│   │   ├── openapi/
│   │   └── framework/
│   ├── graph/
│   ├── projects/
│   ├── environments/
│   ├── providers/
│   ├── planning/
│   ├── execution/
│   ├── runners/
│   ├── artifacts/
│   ├── failures/
│   ├── fixes/
│   ├── reports/
│   ├── scheduler/
│   ├── secrets/
│   └── telemetry/
├── runners/
│   ├── playwright/
│   ├── api/
│   └── generic/
├── packages/
│   ├── shared-types/
│   ├── ui/
│   └── provider-sdk/
├── migrations/
├── deploy/
│   ├── docker/
│   └── helm/
├── docs/
├── scripts/
├── tests/
│   ├── integration/
│   ├── e2e/
│   └── fixtures/
├── docker-compose.yml
├── Makefile
├── README.md
└── SECURITY.md
```

Adapt this structure to the existing project if one already exists.

---

# 6. Core Domain Model

Implement explicit domain entities.

## 6.1 Project

Represents a repository or application under test.

Fields:

- `id`
- `name`
- `slug`
- `repository_path`
- `repository_type`
- `default_branch`
- `created_at`
- `updated_at`
- `last_discovered_at`
- `discovery_status`
- `current_mode`
- `environment_id`
- `settings`

## 6.2 Environment

Fields:

- `id`
- `project_id`
- `name`
- `type`
- `base_url`
- `classification`
- `is_production`
- `allow_mutations`
- `allow_load_tests`
- `allow_active_security_scan`
- `credentials_reference`
- `created_at`
- `updated_at`

Valid classifications:

- `local`
- `development`
- `test`
- `staging`
- `production`
- `unknown`

Unknown environments must be handled restrictively.

## 6.3 Discovered Service

Fields:

- `id`
- `project_id`
- `name`
- `kind`
- `runtime`
- `framework`
- `source_path`
- `container_name`
- `image`
- `ports`
- `health_endpoint`
- `dependencies`
- `metadata`
- `confidence_score`
- `last_seen_at`

Kinds may include:

- `web`
- `api`
- `worker`
- `database`
- `cache`
- `queue`
- `stream`
- `websocket`
- `mobile`
- `gateway`
- `proxy`
- `unknown`

## 6.4 Application Graph Node

Fields:

- `id`
- `project_id`
- `node_type`
- `label`
- `source_reference`
- `runtime_reference`
- `metadata`
- `confidence_score`

## 6.5 Application Graph Edge

Fields:

- `id`
- `project_id`
- `source_node_id`
- `target_node_id`
- `relation_type`
- `evidence`
- `confidence_score`

## 6.6 Test Case

Fields:

- `id`
- `project_id`
- `title`
- `description`
- `category`
- `framework`
- `status`
- `risk_level`
- `priority`
- `confidence_score`
- `source`
- `preconditions`
- `steps`
- `assertions`
- `required_credentials`
- `is_mutating`
- `is_production_safe`
- `generated_file_path`
- `approval_status`
- `created_at`
- `updated_at`

Statuses:

- `suggested`
- `approved`
- `generated`
- `ready`
- `running`
- `passed`
- `failed`
- `flaky`
- `blocked`
- `disabled`

## 6.7 Test Run

Fields:

- `id`
- `project_id`
- `environment_id`
- `status`
- `runner_type`
- `started_at`
- `finished_at`
- `exit_code`
- `trigger_type`
- `triggered_by`
- `summary`
- `artifact_manifest`
- `resource_usage`
- `failure_count`
- `pass_count`
- `skip_count`

## 6.8 Failure

Fields:

- `id`
- `test_run_id`
- `test_case_id`
- `title`
- `severity`
- `failure_type`
- `expected`
- `actual`
- `error_message`
- `stack_trace`
- `root_cause_hypothesis`
- `confidence_score`
- `evidence`
- `status`
- `created_at`

## 6.9 Fix Proposal

Fields:

- `id`
- `failure_id`
- `title`
- `description`
- `risk_level`
- `files_changed`
- `unified_diff`
- `explanation`
- `regression_test_ids`
- `approval_status`
- `created_at`
- `applied_at`

## 6.10 Approval

Fields:

- `id`
- `action_type`
- `resource_type`
- `resource_id`
- `requested_by`
- `approved_by`
- `status`
- `scope`
- `expires_at`
- `created_at`
- `resolved_at`

## 6.11 AI Provider

Fields:

- `id`
- `type`
- `name`
- `base_url`
- `model`
- `secret_reference`
- `is_local`
- `enabled`
- `capabilities`
- `health_status`
- `last_checked_at`

---

# 7. Discovery Engine

The discovery engine must be deterministic first and AI-assisted second.

## 7.1 Repository Discovery

Scan for:

- `package.json`
- `pnpm-lock.yaml`
- `yarn.lock`
- `package-lock.json`
- `go.mod`
- `Cargo.toml`
- `requirements.txt`
- `pyproject.toml`
- `pom.xml`
- `build.gradle`
- `composer.json`
- `.csproj`
- `Dockerfile`
- `docker-compose.yml`
- `compose.yml`
- Kubernetes YAML
- Helm charts
- Terraform
- OpenAPI
- Swagger
- GraphQL
- Prisma
- SQL migrations
- Next.js routes
- Express routes
- Go routers
- FastAPI routes
- Django URLs
- Spring controllers
- Existing Playwright tests
- Existing Cypress tests
- Existing Selenium tests
- Existing Maestro tests
- Existing Detox tests
- Existing k6 tests
- Existing Postman collections
- GitHub Actions
- GitLab CI
- Jenkins pipelines
- environment templates
- README files
- architecture documents

## 7.2 Framework Detection

At minimum detect:

- Next.js
- React
- Vue
- Nuxt
- Angular
- Svelte
- Express
- NestJS
- Go HTTP servers
- Gin
- Fiber
- Chi
- FastAPI
- Django
- Flask
- Spring Boot
- ASP.NET
- Expo
- React Native

## 7.3 Docker Discovery

If Docker is available:

- Read Docker daemon health
- List containers
- Inspect containers
- Parse labels
- Parse networks
- Parse exposed ports
- Parse health status
- Parse mounts
- Parse image names
- Parse Compose project labels
- Parse dependency ordering
- Read logs only with permission
- Detect app-to-service relationships

The platform must not assume Docker socket access is safe.

Support two modes:

1. Direct Docker socket
2. Restricted Docker proxy

Recommend restricted proxy in documentation.

## 7.4 Docker Compose Discovery

Parse configuration using normalized Compose output where possible.

Detect:

- Services
- Profiles
- Depends-on
- Ports
- Networks
- Volumes
- Health checks
- Environment variable names
- Build contexts
- Images
- Commands
- Entrypoints

Do not display secret values.

## 7.5 Kubernetes Discovery

Implement after Docker MVP is stable.

Detect:

- Namespaces
- Deployments
- StatefulSets
- DaemonSets
- Jobs
- CronJobs
- Services
- Ingress
- Gateway API
- ConfigMaps
- Secret names
- Pods
- Containers
- Probes
- Replica health
- Restart counts
- Resource requests and limits
- Events
- Relevant logs

Use least-privilege RBAC.

Provide a read-only ClusterRole example.

## 7.6 Route Discovery

Build an inventory of:

- Browser routes
- API endpoints
- GraphQL operations
- WebSocket endpoints
- Health endpoints
- Authentication endpoints
- Admin routes
- Tenant-specific routes
- Callback/webhook routes

Each route must include evidence:

- Source file
- Framework parser
- OpenAPI entry
- Runtime observation
- UI link
- Network request
- AI inference

## 7.7 Existing Test Discovery

Detect and import metadata from existing tests.

Do not overwrite existing tests.

Identify:

- Existing coverage
- Duplicate scenarios
- Untested routes
- Missing negative cases
- Missing authorization tests
- Missing tenant isolation tests
- Missing error handling
- Missing accessibility checks
- Missing mobile viewport checks

---

# 8. Application Graph

Create a graph that connects code, runtime, routes, APIs, and infrastructure.

Example:

```text
Login Page
  -> POST /api/v1/auth/login
  -> Auth Handler
  -> PostgreSQL
  -> JWT Issuer
  -> Dashboard Redirect
```

Example:

```text
Create Order UI
  -> Order API
  -> PostgreSQL
  -> Kafka Topic
  -> Assignment Worker
  -> WebSocket Event
  -> Courier UI
```

## 8.1 Graph Evidence

Every edge must record evidence.

Example evidence:

```json
{
  "type": "source_code",
  "file": "apps/web/app/login/page.tsx",
  "line": 82,
  "detail": "fetch('/api/v1/auth/login')"
}
```

## 8.2 Confidence

Use confidence levels:

- `high`
- `medium`
- `low`

Do not present low-confidence AI inference as a confirmed dependency.

## 8.3 Graph UI

Panel requirements:

- Zoom
- Pan
- Search
- Filter by node type
- Filter by service
- Highlight test-covered nodes
- Highlight failed paths
- Show evidence drawer
- Show confidence
- Show environment
- Show affected tests

---

# 9. Test Planning Engine

## 9.1 Test Categories

At minimum:

- Critical user journeys
- Authentication
- Authorization
- Tenant isolation
- CRUD
- Validation
- Error handling
- Retry behavior
- Network failures
- Accessibility
- Responsive layout
- Browser compatibility
- API schema
- API authorization
- API negative cases
- WebSocket behavior
- Queue/event flow
- Database consistency
- Smoke tests
- Regression tests
- Performance
- Security smoke tests

## 9.2 Risk-Based Prioritization

Calculate priority based on:

- User impact
- Business criticality
- Route exposure
- Authentication sensitivity
- Data mutation risk
- Dependency count
- Change frequency
- Existing test coverage
- Historical failures
- Production incidents
- AI recommendation
- Manual user priority

Example:

```text
P0 — Login and authentication
P0 — Order creation
P0 — Tenant isolation
P1 — Courier assignment
P1 — WebSocket reconnect
P2 — Profile update
P3 — Cosmetic settings
```

## 9.3 Test Plan Approval

Generated plans must be reviewable.

The user must be able to:

- Approve all
- Approve selected tests
- Reject tests
- Edit a scenario
- Change priority
- Mark environment restrictions
- Add credentials
- Mark a flow as out of scope
- Add manual notes

## 9.4 Confidence Reporting

Never claim complete test coverage.

Show a coverage confidence view:

```text
Authentication: 95%
Order lifecycle: 88%
Tenant isolation: 78%
WebSocket recovery: 61%
Payments: 45%
```

Clearly distinguish:

- Discovered coverage
- Executed coverage
- Passing coverage
- Inferred coverage

---

# 10. Test Generation

## 10.1 Default Web Framework

Use Playwright by default.

Generated tests should include:

- Stable locators
- Accessible selectors
- Explicit assertions
- Reusable fixtures
- Authentication state where appropriate
- Trace on retry
- Screenshot on failure
- Video on failure or configured mode
- Console error collection
- Network error collection
- Deterministic test data handling

Avoid:

- Arbitrary sleeps
- Brittle CSS selectors
- Hard-coded secrets
- Environment-specific URLs
- Hidden global state
- Tests that depend on execution order

## 10.2 API Testing

Support:

- OpenAPI-based discovery
- Schema validation
- Positive tests
- Negative tests
- Required field tests
- Invalid type tests
- Invalid enum tests
- Unauthorized tests
- Forbidden tests
- Tenant isolation tests
- Pagination tests
- Idempotency tests
- Rate-limit observation
- Error content-type tests

Potential tools:

- Playwright API
- Schemathesis
- Newman
- Native generated clients

Select the least-complex suitable option.

## 10.3 WebSocket Testing

Support later phase:

- Authentication
- Connection
- Subscription
- Message receipt
- Heartbeat
- Reconnect
- Backoff
- Duplicate event handling
- Message ordering
- Stale connection handling
- Invalid token
- Token refresh
- Multi-tenant isolation

## 10.4 Mobile Testing

Later phase:

- Maestro as the preferred initial mobile runner
- Detox where project requirements justify it
- Expo-aware discovery
- Android emulator support
- iOS simulator support on compatible hosts

## 10.5 Performance Testing

Later phase using k6.

Require explicit approval.

Disallow against production by default.

## 10.6 Security Testing

Treat security testing as a separate category.

Potential integrations:

- OWASP ZAP
- Nuclei
- Trivy
- Semgrep

Do not label these as pure E2E tests.

Require explicit approval for active scanning.

---

# 11. Runner Architecture

## 11.1 Isolation

Every run must execute in an isolated environment.

Preferred MVP:

- Disposable Docker container per run
- CPU and memory limits
- Time limit
- Network allowlist where practical
- Read-only repository mount
- Writable temporary workspace
- Dedicated artifact directory
- No Docker socket inside runner
- No host root filesystem access
- Non-root user

## 11.2 Runner Contract

Define a runner interface:

```go
type Runner interface {
    Name() string
    Validate(ctx context.Context, input RunInput) error
    Prepare(ctx context.Context, input RunInput) (*PreparedRun, error)
    Execute(ctx context.Context, run *PreparedRun, events EventSink) (*RunResult, error)
    Cancel(ctx context.Context, runID string) error
    CollectArtifacts(ctx context.Context, runID string) ([]Artifact, error)
    Cleanup(ctx context.Context, runID string) error
}
```

## 11.3 Live Events

Stream:

- Queue status
- Runner provisioning
- Dependency installation
- Test start
- Step start
- Assertion result
- Console error
- Network error
- Screenshot captured
- Retry
- Test completion
- Artifact upload
- Runner cleanup

## 11.4 Resource Limits

Configurable per runner:

- CPU
- Memory
- Timeout
- Parallel workers
- Browser count
- Disk usage
- Artifact size
- Log size

---

# 12. Artifact Collection

Each failed browser test should collect where supported:

- Screenshot
- Video
- Playwright trace
- HAR
- Browser console logs
- Page errors
- Failed network requests
- Relevant API responses
- Runner stdout
- Runner stderr
- Container logs
- Service health snapshot
- Timestamped step log

Artifacts must have:

- Checksum
- MIME type
- Size
- Retention policy
- Redaction status
- Associated run
- Associated test
- Creation time

Add retention configuration:

- Default: 14 days
- Failed runs: 30 days
- Passed runs: 7 days
- User-pinned: no automatic deletion

---

# 13. Failure Correlation

The platform must correlate failure evidence across layers.

Example:

```text
Browser:
POST /api/v1/auth/login returned 500

API:
panic while parsing database result

Database:
connection pool exhausted

Container:
api service restarted twice

Likely root cause:
database pool exhaustion caused API failure and invalid frontend error handling exposed a JSON parsing exception
```

## 13.1 Failure Types

- Assertion failure
- UI locator failure
- Browser crash
- Network failure
- API error
- Schema mismatch
- Authentication failure
- Authorization failure
- Tenant isolation failure
- Database error
- Queue timeout
- WebSocket error
- Service unavailable
- Environment misconfiguration
- Flaky timing
- Runner failure
- Unknown

## 13.2 Flaky Test Detection

A test may be marked flaky only after evidence.

Suggested policy:

- Failed once, passed on retry: suspect
- Mixed result in repeated runs: flaky candidate
- Failure rate threshold exceeded: flaky
- Deterministic same-step failure: likely real defect

Do not silently hide flaky tests.

---

# 14. Bug Reporting

Generate structured reports.

Required bug fields:

```text
ID
Title
Severity
Environment
Affected service
Affected route
Affected test
Preconditions
Steps to reproduce
Expected result
Actual result
Evidence
First observed
Last observed
Frequency
Likely root cause
Root cause confidence
Suggested fix
Regression tests
Related logs
Related graph path
```

Severity levels:

- Critical
- High
- Medium
- Low
- Informational

Example:

```text
Title:
Login crashes when API returns non-JSON 500 response

Severity:
High

Steps:
1. Open /login
2. Submit valid credentials
3. API responds 500 with text/plain
4. Frontend calls response.json()

Expected:
A friendly login error is displayed

Actual:
Unhandled JSON parsing exception

Evidence:
- Screenshot
- Trace
- Response headers
- Response body
- Browser console
- API logs

Suggested fix:
Check response.ok and Content-Type before parsing JSON.
```

---

# 15. Fix Proposal Engine

## 15.1 Fix Generation Rules

Fix proposals must include:

- Explanation
- Files affected
- Unified diff
- Risk rating
- Assumptions
- Potential side effects
- Tests to rerun
- Rollback guidance
- AI provider and model
- Generation timestamp

## 15.2 Approval Workflow

Buttons:

- View diff
- Approve patch
- Reject
- Request revision
- Edit instructions
- Apply in temporary workspace
- Run regression before repository write

Recommended flow:

```text
Generate proposal
  -> Review diff
  -> Apply to temporary workspace
  -> Run selected regression tests
  -> Show result
  -> Request final approval
  -> Apply to repository
```

## 15.3 Git Safety

When Git support is enabled:

- Never write directly to protected branches
- Create a dedicated branch
- Use configurable prefix such as `e2e-sentinel/`
- Do not force push
- Do not rewrite history
- Show exact files before commit
- Require separate approval for push
- Require separate approval for PR creation

---

# 16. AI Provider Gateway

## 16.1 Supported Providers

Initial support:

- Ollama
- OpenAI
- Anthropic
- Google Gemini
- Azure OpenAI
- OpenAI-compatible endpoint
- Disabled/no-AI mode

## 16.2 Auto-Detection

Attempt safe local discovery:

- Ollama at `http://host.docker.internal:11434`
- Configurable local endpoints

Do not scan arbitrary networks.

## 16.3 Provider Configuration Panel

Fields:

- Provider type
- Display name
- Base URL
- Model
- API key
- Timeout
- Max tokens
- Temperature where applicable
- Enable/disable
- Test connection

API keys must be stored encrypted or in an external secret backend.

## 16.4 Provider Routing

Allow task-specific provider selection:

- Architecture analysis
- Test planning
- Test generation
- Failure analysis
- Fix generation
- Report summarization

## 16.5 Redaction Pipeline

Before sending context to AI:

1. Detect secrets
2. Detect tokens
3. Detect credentials
4. Redact authorization headers
5. Redact cookies
6. Redact personal data where configured
7. Apply path allowlist
8. Apply file-size limits
9. Record what categories were redacted
10. Audit the request metadata without storing secret content

## 16.6 No-AI Mode

The application must remain useful without AI.

No-AI mode must support:

- Deterministic discovery
- Existing test inventory
- Manual test creation
- Test execution
- Artifact capture
- Basic failure classification
- Manual bug reports

---

# 17. Web Panel

Default URL:

```text
http://localhost:9090
```

## 17.1 Main Navigation

- Dashboard
- Projects
- Discovery
- Application Map
- Test Inventory
- Runs
- Bugs
- Fix Proposals
- AI Providers
- Environments
- Approvals
- Audit Logs
- Settings

## 17.2 Dashboard

Show:

- Project health
- Last discovery
- Services detected
- Environment classification
- Last run
- Pass rate
- Failure count
- Critical bugs
- Flaky tests
- Pending approvals
- AI provider health
- Runner health
- Artifact storage usage

## 17.3 Discovery Page

Show:

- Repository stack
- Frameworks
- Services
- Containers
- Ports
- Routes
- API schemas
- Existing tests
- CI pipelines
- Warnings
- Confidence levels

## 17.4 Test Inventory

Filters:

- Category
- Status
- Priority
- Risk
- Framework
- Environment
- Approval
- Mutating/read-only
- Production-safe
- Service
- Route

Bulk actions:

- Approve selected
- Reject selected
- Change priority
- Assign environment
- Generate selected
- Run selected

## 17.5 Runs

Each run page must show:

- Status
- Timeline
- Live logs
- Tests
- Retries
- Screenshots
- Videos
- Traces
- HAR
- Service logs
- Resource usage
- Failure correlation
- Generated report

## 17.6 Bugs

Support:

- Search
- Severity filter
- Status filter
- Environment filter
- Duplicate linking
- Reopen
- Resolve
- Add notes
- Generate fix
- Export Markdown
- Export JSON

## 17.7 Fix Proposals

Show:

- Diff
- Explanation
- Risk
- Changed files
- Test impact
- Regression plan
- Approval status
- Apply-to-workspace action
- Regression result

## 17.8 Accessibility

Panel must support:

- Keyboard navigation
- Visible focus
- Semantic labels
- Screen readers
- Reduced motion
- Sufficient contrast
- Responsive desktop/tablet layout

---

# 18. API Design

Version all APIs.

Base path:

```text
/api/v1
```

Suggested endpoints:

```text
POST   /projects
GET    /projects
GET    /projects/{id}
PATCH  /projects/{id}

POST   /projects/{id}/discover
GET    /projects/{id}/discovery
GET    /projects/{id}/services
GET    /projects/{id}/graph

GET    /projects/{id}/tests
POST   /projects/{id}/tests/plan
POST   /tests/{id}/approve
POST   /tests/{id}/reject
POST   /tests/{id}/generate
POST   /tests/{id}/run

POST   /runs
GET    /runs
GET    /runs/{id}
POST   /runs/{id}/cancel
GET    /runs/{id}/events
GET    /runs/{id}/artifacts

GET    /bugs
GET    /bugs/{id}
POST   /bugs/{id}/fix-proposal

GET    /fix-proposals/{id}
POST   /fix-proposals/{id}/approve
POST   /fix-proposals/{id}/reject
POST   /fix-proposals/{id}/apply-workspace
POST   /fix-proposals/{id}/apply-repository

GET    /approvals
POST   /approvals/{id}/approve
POST   /approvals/{id}/reject

GET    /providers
POST   /providers
PATCH  /providers/{id}
POST   /providers/{id}/test

GET    /audit-events
GET    /health
GET    /ready
```

Use OpenAPI documentation.

---

# 19. Authentication and Authorization

MVP local mode may support a bootstrap administrator.

However, architecture must be ready for:

- Local authentication
- OIDC
- SAML later
- Role-based access control

Roles:

- Viewer
- Tester
- Developer
- Approver
- Administrator

Example permissions:

```text
Viewer:
- Read projects, runs, reports, bugs

Tester:
- Run approved tests
- View artifacts

Developer:
- Generate tests
- Generate fix proposals
- Apply to temporary workspace

Approver:
- Approve test plans
- Approve mutating tests
- Approve repository patches

Administrator:
- Configure providers
- Configure environments
- Manage permissions
- Configure storage
```

---

# 20. Database Requirements

Use versioned migrations.

Tables should include:

- users
- roles
- permissions
- projects
- environments
- discovered_services
- graph_nodes
- graph_edges
- test_cases
- test_runs
- test_run_items
- failures
- bug_reports
- fix_proposals
- approvals
- ai_providers
- secret_references
- artifacts
- audit_events
- jobs
- schedules
- settings

Requirements:

- Foreign keys
- Proper indexes
- Soft deletion where needed
- Immutable audit events
- Idempotent job handling
- UTC timestamps
- JSONB only where schema flexibility is justified

---

# 21. Job System

Jobs:

- Repository discovery
- Docker discovery
- Kubernetes discovery
- Route extraction
- Graph building
- Test planning
- Test generation
- Runner provisioning
- Test execution
- Artifact processing
- Failure correlation
- Fix generation
- Regression execution
- Report generation
- Retention cleanup

Each job must support:

- Idempotency key
- Retry policy
- Timeout
- Cancellation
- Progress
- Structured error
- Audit event
- Dead-letter handling

---

# 22. Observability

Implement OpenTelemetry.

Expose:

- Structured logs
- Metrics
- Traces
- Health endpoint
- Readiness endpoint

Metrics:

- Discovery duration
- Test generation duration
- Queue latency
- Runner startup time
- Test duration
- Pass rate
- Failure rate
- Flaky rate
- Artifact bytes
- AI request duration
- AI request errors
- Redaction counts
- Approval latency
- Active runners
- Job retries

Do not include secret values in telemetry.

---

# 23. Security Requirements

## 23.1 Threat Areas

Consider:

- Docker socket privilege escalation
- Kubernetes token misuse
- Malicious repository content
- Prompt injection from source files
- Generated test code execution
- Secret leakage
- SSRF
- Path traversal
- Command injection
- Artifact XSS
- Stored log injection
- Supply-chain attacks
- Dependency confusion
- Runner escape
- Unauthorized patch application

## 23.2 Prompt Injection Defense

Treat repository content as untrusted data.

Never allow instructions found in source code, README files, comments, logs, issue text, or webpages to override system policy.

Mark AI context clearly as untrusted.

## 23.3 Command Execution

Never construct shell commands through unsafe string concatenation.

Use:

- Argument arrays
- Allowlists
- Path validation
- Timeouts
- Resource limits
- Non-root processes

## 23.4 File Access

Restrict file access to configured project roots.

Prevent:

- `..` traversal
- Symlink escape
- Host filesystem traversal
- Reading SSH keys
- Reading unrelated home directories
- Reading arbitrary Docker mounts

## 23.5 Artifact Rendering

Treat HTML, logs, traces, and reports as untrusted.

Use secure download headers.

Prevent inline script execution.

## 23.6 Encryption

Encrypt sensitive provider configuration at rest.

Document key management for:

- Local development
- Docker deployment
- Kubernetes deployment

---

# 24. Installation

## 24.1 Docker Compose

Provide a production-capable local Compose stack.

Services:

- `sentinel-api`
- `sentinel-web`
- `postgres`
- `redis`
- optional `minio`
- runner support

Default panel:

```text
http://localhost:9090
```

Recommended command:

```bash
docker compose up -d
```

## 24.2 Single Command Development

Provide:

```bash
make dev
make test
make lint
make build
make up
make down
make migrate
```

## 24.3 Project Mount

Example:

```yaml
volumes:
  - ./target-project:/workspace:ro
```

Writing generated files must use a separate temporary directory until approved.

## 24.4 Docker Socket Warning

Document that direct Docker socket mounting grants extensive host capability.

Prefer:

- Restricted socket proxy
- Rootless Docker where possible
- Read-only discovery endpoints where possible

## 24.5 Kubernetes Installation

Later phase:

- Helm chart
- Read-only service account
- Optional namespace-scoped deployment
- External PostgreSQL support
- External Redis support
- S3 artifact storage support
- Ingress
- NetworkPolicy
- PodSecurityContext
- Resource limits

---

# 25. MVP Delivery Phases

## Phase 0 — Foundation

Deliver:

- Repository assessment
- Architecture decision record
- Monorepo or project structure
- Configuration system
- PostgreSQL
- Redis
- Migrations
- Health/readiness
- Structured logging
- Audit event foundation
- Basic UI shell
- Docker Compose

Acceptance:

- `docker compose up -d` starts the system
- Panel opens on port `9090`
- Health and readiness work
- Database migrations run
- No secret values appear in logs

## Phase 1 — Project and Repository Discovery

Deliver:

- Add local project
- Validate project root
- Scan repository
- Detect languages
- Detect frameworks
- Detect manifests
- Detect existing test tools
- Display discovery results
- Store evidence and confidence

Acceptance:

- A Next.js + Go repository is correctly detected
- Docker files are listed
- Existing Playwright tests are discovered
- Source paths cannot escape project root
- Repeated discovery is idempotent

## Phase 2 — Docker Compose Discovery

Deliver:

- Compose parser
- Running container detection
- Service relationships
- Ports
- Networks
- Health
- Environment variable names
- Runtime snapshot
- Discovery UI

Acceptance:

- Compose services appear in panel
- Running status is visible
- Secret values are redacted
- Docker-unavailable state is handled gracefully

## Phase 3 — Application Graph

Deliver:

- Graph node/edge model
- Route extraction
- OpenAPI import
- Runtime-to-source correlation
- Graph UI
- Evidence drawer
- Confidence display

Acceptance:

- UI route -> API endpoint -> service relation can be displayed
- Every edge shows evidence
- Low-confidence edges are visibly marked

## Phase 4 — Test Planning

Deliver:

- Risk model
- Test category engine
- Suggested scenarios
- Approval workflow
- Manual editing
- Confidence coverage view
- AI and no-AI planning

Acceptance:

- Suggested tests are reviewable
- Mutating tests are clearly marked
- Production-unsafe tests cannot be approved accidentally
- Plan generation works without AI using deterministic rules

## Phase 5 — Playwright Runner

Deliver:

- Isolated runner container
- Playwright execution
- Live logs
- Screenshots
- Video
- Trace
- HAR where configured
- Console errors
- Network errors
- Timeouts
- Cancellation
- Resource limits

Acceptance:

- Browser test runs in disposable container
- Failed test produces trace and screenshot
- Runner is cleaned up
- Cancellation works
- Repository remains read-only

## Phase 6 — AI Providers

Deliver:

- Ollama
- OpenAI
- Anthropic
- Gemini
- Azure OpenAI
- OpenAI-compatible provider
- Provider health test
- Secret encryption
- Redaction pipeline
- Task routing

Acceptance:

- Local Ollama can be selected
- External provider can be configured
- Keys never return through the API
- Redaction tests pass
- AI can be disabled entirely

## Phase 7 — Failure Analysis and Bug Reports

Deliver:

- Failure classification
- Evidence correlation
- Structured bug reports
- Markdown export
- JSON export
- Duplicate hints
- Severity model

Acceptance:

- Failed test creates a bug candidate
- Report contains evidence
- Root cause is clearly marked as hypothesis
- Report can be exported

## Phase 8 — Fix Proposals

Deliver:

- Patch generation
- Monaco diff view
- Risk explanation
- Temporary workspace application
- Regression selection
- Approval workflow
- Repository application after final approval

Acceptance:

- AI cannot write directly to repository
- Temporary patch can be tested
- Final repository write requires explicit approval
- Applied files match approved diff exactly
- Audit log records every step

## Phase 9 — Production Hardening

Deliver:

- RBAC
- OIDC-ready architecture
- Rate limiting
- CSRF protection
- Security headers
- Audit search
- Retention jobs
- Backups documentation
- OpenTelemetry
- Metrics
- Threat model
- Dependency scanning
- Security tests

Acceptance:

- Security checklist passes
- Authorization tests pass
- Audit events are immutable through public API
- Secret handling is verified
- Runner isolation tests pass

## Phase 10 — Kubernetes Discovery

Deliver only after Docker MVP is stable:

- Kubernetes connection
- Namespace scoping
- Workload discovery
- Pod health
- Events
- Read-only logs
- Ingress and service mapping
- Helm deployment

## Phase 11 — Advanced Test Adapters

Later:

- WebSocket adapter
- Maestro
- Detox
- k6
- ZAP
- Nuclei
- Schemathesis
- Pact
- Kafka/event testing

---

# 26. Testing Strategy for E2E Sentinel Itself

## 26.1 Unit Tests

Cover:

- Path validation
- Secret redaction
- Framework detection
- Compose parsing
- Environment classification
- Permission checks
- Approval checks
- Risk scoring
- Confidence scoring
- Artifact retention
- Provider routing
- Diff validation

## 26.2 Integration Tests

Use fixture repositories.

Fixtures:

- Next.js + Go + PostgreSQL
- React + Express
- OpenAPI-only service
- Docker Compose multi-service app
- Repository with malicious paths
- Repository containing fake secrets
- Broken Compose file
- Existing Playwright project

## 26.3 E2E Tests

Test E2E Sentinel panel:

- Create project
- Run discovery
- View graph
- Generate test plan
- Approve test
- Execute test
- View artifacts
- Generate bug
- Generate fix
- Reject fix
- Approve temporary fix
- Run regression

## 26.4 Security Tests

Cover:

- Path traversal
- Symlink escape
- Command injection
- SSRF
- Stored XSS in logs
- Prompt injection
- Secret exfiltration
- Unauthorized approval
- Unauthorized repository write
- Runner escape assumptions
- Malicious artifact names

## 26.5 Failure Tests

Simulate:

- Docker daemon unavailable
- Database unavailable
- Redis unavailable
- AI provider timeout
- Runner crash
- Browser crash
- Artifact storage full
- Project removed during scan
- Kubernetes permission denied
- User cancels during execution

---

# 27. Definition of Done

A phase is complete only when:

- Code is implemented
- Unit tests pass
- Integration tests pass
- Relevant E2E tests pass
- Lint passes
- Typecheck passes
- Security checks pass
- Documentation is updated
- Migration is included
- Error states are handled
- Loading and empty states exist
- Audit events exist
- Metrics exist where relevant
- No secrets appear in logs
- No destructive default was introduced
- Acceptance criteria are demonstrated

Do not mark a phase complete with mocked core behavior unless explicitly documented as incomplete.

---

# 28. Coding Standards

- Prefer clear, boring, maintainable code.
- Keep domain logic outside HTTP handlers.
- Use interfaces at external boundaries.
- Avoid premature abstraction.
- Avoid giant files.
- Avoid global mutable state.
- Propagate context cancellation.
- Wrap errors with context.
- Use typed errors for expected cases.
- Validate all external input.
- Use transactions where consistency requires them.
- Make background jobs idempotent.
- Use structured logs.
- Add comments for security-sensitive decisions.
- Keep generated AI output separate from trusted system data.

---

# 29. Required Documentation

Create and maintain:

```text
README.md
docs/ARCHITECTURE.md
docs/SECURITY_MODEL.md
docs/THREAT_MODEL.md
docs/APPROVAL_MODEL.md
docs/AI_PROVIDER_GUIDE.md
docs/DOCKER_DISCOVERY.md
docs/KUBERNETES_DISCOVERY.md
docs/RUNNER_ISOLATION.md
docs/TEST_GENERATION.md
docs/FAILURE_CORRELATION.md
docs/LOCAL_DEVELOPMENT.md
docs/DEPLOYMENT.md
docs/OPERATIONS.md
docs/TROUBLESHOOTING.md
docs/ROADMAP.md
SECURITY.md
CONTRIBUTING.md
```

---

# 30. Initial User Experience

The first-run wizard should:

1. Welcome the user
2. Create or unlock local admin
3. Select project folder
4. Confirm read-only access
5. Detect Docker
6. Detect repository stack
7. Classify environment
8. Configure AI or continue without AI
9. Run first discovery
10. Show services
11. Show suggested test plan
12. Request approval before execution

---

# 31. Example First-Run Flow

```text
Project:
Routa

Detected:
- Next.js Web
- Go API
- PostgreSQL
- Redis
- Kafka
- Docker Compose
- OpenAPI
- Existing Playwright configuration

Environment:
Local

Suggested critical tests:
- Login success
- Login invalid credentials
- Login API 500 non-JSON response
- Tenant isolation
- Order creation
- Order assignment
- WebSocket reconnect
- Courier status update

Pending action:
8 tests require approval before generation.
```

---

# 32. Example Bug and Fix Flow

```text
Test:
Login handles backend failures

Result:
Failed

Observed:
API returned 500 text/plain.
Frontend attempted response.json().
Unhandled SyntaxError occurred.

Evidence:
- Screenshot
- Playwright trace
- Browser console
- Network response
- API logs

Proposed fix:
Check response status and Content-Type before parsing JSON.

Risk:
Low

Regression:
- Login success
- Invalid password
- API 401 JSON
- API 500 JSON
- API 500 text/plain
- Network timeout

Action:
Waiting for approval.
```

---

# 33. Codex Execution Instructions

Codex must implement this project incrementally.

For every phase:

1. Inspect current repository state.
2. Summarize what already exists.
3. List files that will be added or changed.
4. State security implications.
5. Implement the smallest complete vertical slice.
6. Add tests.
7. Run relevant checks.
8. Report exact results.
9. Do not proceed into the next major phase if the current phase is failing.
10. Do not perform destructive operations.
11. Do not claim success without evidence.
12. Preserve backward compatibility where practical.
13. Keep unimplemented future features behind interfaces or documented extension points.
14. Update documentation with every material architectural change.

After each phase, produce:

```text
Phase:
Status:
Implemented:
Files changed:
Database migrations:
Tests added:
Commands executed:
Test results:
Security checks:
Known limitations:
Next recommended phase:
```

---

# 34. First Codex Task

Start with **Phase 0 — Foundation** only.

Do not attempt the entire roadmap in one pass.

The first task is:

1. Inspect the repository.
2. Create a repository assessment.
3. Propose the final directory structure based on what exists.
4. Implement the minimum production-ready foundation.
5. Provide Docker Compose.
6. Start the panel on port `9090`.
7. Add PostgreSQL and Redis.
8. Add migrations.
9. Add health and readiness endpoints.
10. Add structured logging.
11. Add basic audit-event infrastructure.
12. Add a minimal responsive web shell.
13. Add unit and integration tests.
14. Add documentation.
15. Stop after Phase 0 and report results.

Do not implement AI, Playwright execution, Docker socket access, Kubernetes access, or repository patching during Phase 0.

---

# 35. Phase 0 Acceptance Checklist

Phase 0 is accepted only if:

- [ ] Repository assessment exists
- [ ] Architecture decision is documented
- [ ] `docker compose up -d` works
- [ ] Panel is reachable on `http://localhost:9090`
- [ ] API health endpoint works
- [ ] API readiness endpoint verifies dependencies
- [ ] PostgreSQL migrations work
- [ ] Redis connectivity works
- [ ] Structured logs are emitted
- [ ] Audit events can be persisted and queried internally
- [ ] No secret values are logged
- [ ] Unit tests pass
- [ ] Integration tests pass
- [ ] Frontend lint passes
- [ ] Frontend typecheck passes
- [ ] Backend lint/static analysis passes
- [ ] Documentation is complete
- [ ] No destructive capability is implemented
- [ ] Known limitations are documented

---

# 36. Final Product Principles

E2E Sentinel must remain:

- Safe by default
- Read-only by default
- Approval-gated
- AI-provider independent
- Useful without AI
- Deterministic in execution
- Transparent in confidence
- Evidence-driven
- Infrastructure-aware
- Repository-aware
- Runtime-aware
- Self-hosted
- Extensible
- Auditable

The system must never market or represent itself as capable of discovering every possible defect.

Use the following claim:

> E2E Sentinel analyzes repository structure, runtime services, API schemas, application routes, existing tests, and observed behavior to generate high-confidence test recommendations and evidence-backed failure reports.

Do not use claims such as:

- “Finds every bug”
- “Tests everything automatically”
- “Guarantees complete coverage”
- “Autonomously fixes production”

The product's trustworthiness is more important than exaggerated autonomy.
