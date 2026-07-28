# Contributing

E2E Sentinel is built incrementally against
`E2E_SENTINEL_CODEX_MASTER_SPEC.md`, one phase at a time (see
[docs/ROADMAP.md](docs/ROADMAP.md)). Please read the spec's §2
(Non-Negotiable Engineering Rules) before proposing a change — in
particular:

- No destructive default behavior, ever.
- Anything that mutates a target project, restarts a service, or writes
  to a repository requires an explicit, auditable approval step.
- Secrets must never be logged or sent to an AI provider.

## Before opening a PR

```bash
make lint
make test
```

Both must pass. If you touched `apps/api`, also run `go vet ./...` and add
tests for new packages. If you touched `apps/web`, run `npm run typecheck`
and `npm run lint` inside `apps/web`.

## Code style

- Go: standard `gofmt`, small packages, interfaces at external boundaries,
  wrapped errors with context, no global mutable state.
- TypeScript: strict mode, no `any` without a comment explaining why.
- No comments explaining *what* code does — only *why*, for non-obvious
  constraints (security-sensitive decisions, workarounds, invariants).

## Commit scope

Keep each change to a single vertical slice (spec §33). Don't start a new
phase's work in a PR that hasn't finished the current one's acceptance
criteria.
