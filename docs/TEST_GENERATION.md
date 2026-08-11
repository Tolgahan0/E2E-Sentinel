# Test Generation

Implemented as of Phase 5. Two stages:

1. **Planning** (Phase 4, `internal/planning`) — deterministic rules
   produce reviewable, editable `TestCase` rows from extracted routes:
   approve/reject/edit-title/edit-priority via the API, `is_mutating`/
   `is_production_safe` always shown, a coverage-confidence summary on
   the Test Inventory page that never claims complete coverage.
2. **Spec generation** (Phase 5, `internal/testgen`) — a *deterministic
   template*, not AI, turns an approved `TestCase` into a runnable
   Playwright spec file at the moment `POST /tests/{id}/run` is called.
   See [docs/RUNNER_ISOLATION.md](RUNNER_ISOLATION.md#test-generation)
   for exactly what the generated assertions do and don't check — given
   no schema or AI input, they're smoke-level by design (page renders
   without a console error; API endpoint doesn't 5xx), never presented
   as more than that.

Generated tests avoid arbitrary sleeps, hard-coded secrets, and
environment-specific URLs baked into the spec itself (the target URL is
injected from the project's `Environment.base_url` at generation time,
not hard-coded in a template). They don't yet use accessible-role-based
locators or reusable fixtures beyond the fixed Playwright config
(`screenshot: { mode: 'on', fullPage: true }` — every run, not just
failures, since internal/visualdiff diffs it against a baseline;
`video: retain-on-failure`, `trace: retain-on-failure` stay
failure-only) — there's no page structure to derive a
richer locator from without AI or a schema, which is the honest ceiling
documented above.

## What's not implemented yet

Regenerating/refining a generated spec after a run (e.g. adding more
specific assertions once a human reviews the failure) is not built — a
generated spec is fixed at generation time. AI-assisted test generation
(richer assertions, realistic request bodies) is deferred to Phase 6+.
