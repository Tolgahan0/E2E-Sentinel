# Threat Model

**Status: not yet implemented.** This document will be filled in
incrementally as each phase introduces new attack surface, per spec §23.

## Phase 0 surface

The only attack surface today is: E2E Sentinel's own HTTP API
(`/health`, `/ready`, `/api/v1/audit-events` — all read-only, no auth yet
since there is no multi-tenant deployment target in Phase 0), its own
Postgres/Redis, and the Next.js panel. No target-repository, Docker,
Kubernetes, or AI-provider surface exists yet. See
[docs/SECURITY_MODEL.md](SECURITY_MODEL.md) for what's mitigated today.

## Threat areas to formalize per phase (spec §23.1)

- Docker socket privilege escalation — Phase 2
- Kubernetes token misuse — Phase 10
- Malicious repository content / prompt injection — Phase 1, Phase 6
- Generated test code execution — Phase 5
- Secret leakage — Phase 6 (redaction pipeline)
- SSRF, path traversal, command injection — Phase 1 onward (discovery),
  formal test coverage in Phase 9
- Artifact XSS, stored log injection — Phase 5, Phase 9
- Supply-chain / dependency confusion — ongoing, `npm audit` / Go module
  verification in CI (Phase 9)
- Runner escape — Phase 5
- Unauthorized patch application — Phase 8
