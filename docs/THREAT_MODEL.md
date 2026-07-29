# Threat Model

Tracks spec §23.1's threat areas against what's actually implemented, by
phase. [docs/SECURITY_MODEL.md](SECURITY_MODEL.md) is the authoritative
"what's mitigated today" reference; this document is the per-threat-area
index into it.

## Threat areas (spec §23.1)

| Threat area | Status | Mitigation |
|---|---|---|
| Docker socket privilege escalation | Mitigated (Phase 2/5) | Socket only mounted for Phase 5 test execution (opt-in via `make up`); `internal/dockerclient` implements only ping/list/create/start/wait/stop/remove/logs — never the full Docker SDK, never pulls/builds an image at runtime. Non-root via `group_add`, not root. |
| Kubernetes token misuse | Mitigated, opt-in (Phase 10) | `internal/kubeclient` issues only GET requests — no create/update/patch/delete call exists anywhere in the package. A [read-only ClusterRole example](../deploy/k8s/read-only-clusterrole.yaml) ships for the real deployment case (spec §7.5's least-privilege requirement). `SENTINEL_KUBE_CONFIG_PATH` defaults to unset (disabled); an `exec`/`auth-provider` kubeconfig credential plugin is rejected outright rather than silently attempted. |
| Malicious repository content / prompt injection | Partially mitigated | Discovery/route extraction only *read* file names/contents to classify them (regex-matched, never evaluated) — Phase 1/3. Phase 8's AI-assisted fix generation never reads repository content at all (evidence-only prompt), sidestepping prompt injection via source comments entirely for that path; `internal/redaction` (Phase 6) exists as a building block for whichever later phase first assembles real repository content into an AI prompt, but isn't wired to anything yet. |
| Generated test code execution | Mitigated (Phase 5) | Deterministic (no-AI) spec generation; execution is always inside a disposable, resource-limited, non-root container — never in `sentinel-api`'s own process. |
| Secret leakage | Mitigated | `internal/secretstore` (Phase 6, AES-256-GCM) for provider keys; `internal/redaction` (Phase 6) for context text; `internal/logging.Redact` for structured log fields; `POSTGRES_PASSWORD` never logged (Phase 0); session tokens (Phase 9) stored only as a SHA-256 hash, passwords only as bcrypt; Kubernetes Secret/ConfigMap values (Phase 10) are never decoded into memory at all — `internal/kubeclient`'s `SecretSummary`/`ConfigMapSummary` types have no field that could carry one. |
| SSRF | Partially mitigated | AI provider `base_url`/health-check/completion requests (Phase 6/8) go wherever the configured URL points — there is no allowlist restricting which hosts a configured provider can point at. This is an accepted MVP gap: provider configuration is an administrator action (and, with RBAC enabled, requires `configure_providers` permission), not attacker-reachable input. The notification webhook URL (see docs/OPERATIONS.md) has the exact same shape and the same mitigation — same permission required, same administrator-only trust level. |
| Path traversal | Mitigated | `projects.ValidateRepositoryPath`/`WithinRoot` (Phase 1) for discovery; the same `WithinRoot` check again for every file a fix proposal's diff touches, in both the temporary workspace and the real repository (Phase 8) — tested explicitly with a `../../../etc/passwd`-shaped diff for both write targets. |
| Command injection | Mitigated | No shell-out anywhere in this codebase: Compose files are parsed as YAML (Phase 2), Docker is driven via its HTTP API (Phase 2/5), unified diffs are parsed and applied in pure Go (Phase 8) — never `git apply`/`patch` as a subprocess. |
| Artifact XSS / stored log injection | Mitigated (Phase 5/9) | `X-Content-Type-Options: nosniff` + forced `Content-Disposition: attachment` (except screenshots) on artifact and bug-export downloads; global security headers (`X-Frame-Options: DENY`, a `default-src 'none'` CSP, `Referrer-Policy`) on every response (Phase 9). |
| Supply-chain / dependency confusion | Monitored (Phase 9) | `make scan` (govulncheck + npm audit) and `.github/workflows/dependency-scan.yml` (push/PR/weekly). Not auto-remediated — see docs/SECURITY_MODEL.md for the current scan output and why open findings aren't blindly auto-fixed. |
| Runner escape | Mitigated (Phase 5) | Memory/CPU/wall-clock limits, non-root fixed user, no Docker socket inside the runner container, no volume beyond its own workspace. |
| Unauthorized patch application | Mitigated (Phase 8/9) | `apply-repository` requires `approval_status == approved` (403 otherwise), runs at most once per proposal (409 on retry), and — with RBAC enabled (Phase 9) — requires the `approve_repository_patches` permission (Approver/Administrator only). |
| Unauthorized action generally | Mitigated, opt-in (Phase 9) | RBAC (`internal/auth`) gates the mutating routes spec §19's example table calls out. Defaults **off** (`SENTINEL_AUTH_ENABLED=false`) — until enabled, anyone who can reach the API can do anything, which is why every deployment doc says not to expose this beyond a trusted network. |

## Not yet implemented

- Helm deployment of E2E Sentinel itself (spec §24.5) is not built —
  Kubernetes *discovery* (Phase 10) is implemented and is a separate
  concern.
- No SSRF allowlist for AI provider `base_url`s (see table above — an
  accepted MVP gap, not an oversight).
- No prompt-injection defense is exercised in practice yet, since no
  phase currently sends repository content to an AI provider at all;
  `internal/redaction` is ready for whichever phase first does.
- RBAC (Phase 9) has a fixed, in-code role→permission mapping — no
  dynamic per-role permission editing, no OIDC/SAML (architecture is
  ready via the `auth.Store` interface, but only local email/password is
  implemented).
