# Failure Correlation

**Status: not yet implemented.** Lands in Phase 7. See spec §13.

## Planned behavior

Correlates evidence across browser, API, database, queue, and container
layers into a single root-cause hypothesis with a confidence score —
never presented as certain fact. Flaky-test detection follows an
evidence-based policy (single retry pass ≠ flaky; a failure-rate
threshold and mixed results across repeated runs are required before a
test is labeled flaky), and flaky tests are never silently hidden.

## What exists today

No failure-analysis code exists in Phase 0; there are no test runs yet to
analyze.
