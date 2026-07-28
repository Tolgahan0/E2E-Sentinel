# Test Generation

**Status: not yet implemented.** Lands in Phase 4 (planning) and Phase 5
(Playwright execution). See spec §9–§10.

## Planned behavior

Risk-based test planning produces reviewable, editable test cases
(approve all / approve selected / reject / edit / reprioritize) with a
coverage-confidence view that never claims complete coverage. Generated
Playwright tests use stable/accessible locators, explicit assertions,
reusable fixtures, and capture traces/screenshots/video/console/network
errors on failure — never arbitrary sleeps, brittle CSS selectors,
hard-coded secrets, or order-dependent state.

## What exists today

No test-generation code exists in Phase 0.
