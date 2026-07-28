# Roadmap

Tracks delivery against `E2E_SENTINEL_CODEX_MASTER_SPEC.md` §25. Phases
are implemented strictly in order; a phase does not start until the
previous one's acceptance criteria are demonstrated (spec §33).

| Phase | Name | Status |
|---|---|---|
| 0 | Foundation | **Done** |
| 1 | Project & Repository Discovery | Not started |
| 2 | Docker Compose Discovery | Not started |
| 3 | Application Graph | Not started |
| 4 | Test Planning | Not started |
| 5 | Playwright Runner | Not started |
| 6 | AI Providers | Not started |
| 7 | Failure Analysis & Bug Reports | Not started |
| 8 | Fix Proposals | Not started |
| 9 | Production Hardening | Not started |
| 10 | Kubernetes Discovery | Not started |
| 11 | Advanced Test Adapters (WebSocket, Maestro, Detox, k6, ZAP, Nuclei, Schemathesis, Pact, Kafka) | Not started |

## Phase 0 — Foundation (done)

- Go API + Next.js web shell, PostgreSQL + Redis, versioned migrations,
  structured logging, append-only audit log, health/readiness endpoints,
  Docker Compose stack, unit + integration tests.
- See [ADR 0001](adr/0001-phase0-foundation.md) for decisions and
  the Phase 0 completion report in the project history for verification
  evidence.

## Next: Phase 1 — Project & Repository Discovery

Per spec §25 Phase 1, this adds:

- Add a local project (validated, path-traversal-safe project root)
- Repository scan: languages, frameworks, manifests, existing test tooling
- Persisted discovery results with evidence + confidence scores

Acceptance criteria (spec): a Next.js + Go repository is correctly
detected, Docker files are listed, existing Playwright tests are
discovered, source paths cannot escape the project root, and repeated
discovery is idempotent.
