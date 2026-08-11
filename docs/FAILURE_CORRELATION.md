# Failure Correlation

Every failed test run is automatically classified and turned into (or
merged into) a structured bug report — deterministically, with no AI
call (spec §13, §16.6). See [docs/AI_PROVIDER_GUIDE.md](AI_PROVIDER_GUIDE.md)
for why: Phase 6 delivers provider configuration only, and the first
real AI consumer is a later phase — failure analysis stays on the no-AI
baseline the same way test planning (Phase 4) and test generation
(Phase 5) do.

## Classification (`internal/failures`)

`Classify(stdout, stderr, exit_code)` pattern-matches the run's captured
output against an ordered list of signatures — authentication before
generic API errors, browser crashes before generic runner failures, and
so on — and returns:

- `failure_type` — one of the 17 types from spec §13.1 (assertion
  failure, network failure, browser crash, runner failure, tenant
  isolation failure, `unknown`, …). An unmatched failure classifies as
  `unknown` rather than being left blank, so every failure still gets a
  severity.
- `severity` — a **fixed** mapping from `failure_type` to critical/high/
  medium/low/informational (spec §14). The same failure_type always
  gets the same severity; it is never adjusted per-run.
- `root_cause_hypothesis` and `root_cause_confidence` (`medium`|`low`,
  **never** `high` — a regex match over log text is never that certain).

## Evidence correlation

"Evidence" here means real, already-captured data — never a fabricated
multi-layer log stream E2E Sentinel doesn't actually have:

- The run's own stdout/stderr and any Playwright-captured screenshot/
  video/trace artifacts (Phase 5), referenced by ID.
- **Related graph path**: the failing test case's route is looked up in
  the Application Graph (Phase 3) built for the project. If an incoming
  edge (e.g. a page's `calls` edge into the route) or an outgoing edge
  (e.g. the route's `served_by` edge to a service) exists, they're
  rendered as a short trail — e.g. `Login Page --calls--> POST
  /api/v1/auth/login --served_by--> api`. Whichever side doesn't exist
  in the graph is simply omitted; nothing is guessed.
- The environment the test ran against (`environment_id`), if one has a
  `base_url` set.

## Flaky detection (`internal/failures.AssessFlakiness`, spec §13.2)

Given a test case's run history (oldest first, ending with the current
failure):

| Pattern | Label |
|---|---|
| Fewer than 2 runs exist | `insufficient_evidence` |
| Every run failed | `likely_real_defect` (deterministic, same-step) |
| This is the only failure amid an otherwise-passing history | `suspect` |
| Failure rate ≥ 60% (with at least one pass) | `flaky` (confirmed) |
| Mixed results below that threshold | `flaky_candidate` |

The assessment is attached to the bug report as `flaky_assessment` and
always shown — a flaky label never hides or suppresses a bug (spec
§13.2 "do not silently hide flaky tests").

This reactive assessment only fires on a failing run, and only the
latest value survives (overwritten on the bug report each time). A
test case that is *currently* passing — even if it flaked several runs
ago — has no signal anywhere under this path.

### Flaky Tests dashboard

`GET /projects/{projectID}/flaky-tests` (`handleListFlakyTests`) closes
that gap with a proactive, project-wide view, computed on read instead
of stored: for every test case with at least one run, it fetches the
full run history and calls `AssessFlakiness` directly, independent of
whether the test case's most recent run passed or failed. Test cases
with zero runs are excluded (nothing to assess yet); everything else is
included, sorted most-actionable-first (`flaky`, `flaky_candidate`,
`suspect`, `likely_real_defect`, then `insufficient_evidence` last).

Nothing is persisted — no migration, no new column — so the result is
always current, at the cost of one `ListByTestCase` query per test case
(an N+1 pattern, accepted at today's scale rather than pre-optimized).
The web panel's Flaky Tests page renders this list with a pill per
assessment tier and a dot-row sparkline of each test case's last 10
runs.

## Bug reports (`internal/bugreports`)

`UpsertFromFailure` is keyed by `(project_id, test_case_id,
failure_type)`:

- First occurrence → a new bug, status `open`, frequency 1.
- Same test case, same failure type again → the **same row** updates:
  frequency increments, evidence and root cause refresh to the latest
  failure, `last_observed_at` advances. This is what makes "a failed
  test creates a bug candidate" (spec's Phase 7 acceptance criterion)
  not spam a new row on every flaky re-failure.
- If that bug had been marked `resolved`, a recurrence flips it to
  `reopened` rather than silently reabsorbing the update — a failure
  after resolution is itself evidence the fix didn't hold.
- **Duplicate hint** (spec §17.6 "Duplicate linking"): when a *different*
  test case fails with the same `failure_type` while another bug of that
  type is still open, the new bug gets `possible_duplicate_of_id` set to
  it. This is a hint only — surfaced in the API/UI/exports as
  "unconfirmed", never auto-merged.

## Root cause is always a hypothesis

Every surface that shows `root_cause_hypothesis` also shows it's
unverified:

- API/export JSON: a sibling field
  `root_cause_is_unverified_hypothesis: true` is always present.
- Markdown export: the section header is literally "Likely root cause
  (unverified hypothesis)", immediately followed by "this is a
  hypothesis derived from pattern-matching the failure output, not a
  confirmed diagnosis."
- Web (Bugs page): rendered under an explicit "Likely root cause
  (unverified hypothesis — *confidence* confidence)" label.

## API

```text
GET    /bugs?project_id=&severity=&status=&environment_id=&search=
GET    /bugs/{id}
POST   /bugs/{id}/resolve
POST   /bugs/{id}/reopen
POST   /bugs/{id}/notes            {author, text}
GET    /bugs/{id}/export/markdown  (forced download, spec §23.5 headers)
GET    /bugs/{id}/export/json      (forced download, spec §23.5 headers)
```

`POST /tests/{id}/run` (Phase 5) triggers classification automatically
when the run finishes `failed`, and also when `Runner.Execute` itself
fails outright (an infra-level failure classifies as `runner_failure`,
same treatment as a product defect). Bug creation/update happens
**before** the run's status flips to its terminal value, so a client
polling `GET /runs/{id}` never observes a completed run whose bug report
doesn't exist yet.
