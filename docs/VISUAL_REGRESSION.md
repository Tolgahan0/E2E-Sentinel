# Visual regression testing

`internal/visualdiff` diffs every browser-based test run's screenshot
against a stored baseline for that test case, and surfaces the result
on the *Visual Diffs* page — never as a pass/fail verdict, always as a
separate signal a human reviews.

## Why a separate signal, not a test failure

A test case's assertions ("the page renders without a console error")
and its visual appearance are different questions. A CSS tweak, a copy
change, a new banner — all visually different, none of them bugs. Auto-
failing a test over a pixel difference would either train everyone to
ignore failures (if visual changes are common) or block legitimate
deploys (if they're not). So a visual diff is informational: it shows
up for review, and a human decides — **Accept** it (this becomes the
new expected screenshot) or **Ignore** it (reviewed, but the old
baseline stands). `TestRun.Status` is never touched by any of this.

## Zero setup, by design

There's no baseline to configure ahead of time. The **first** run of a
page-rendering test case (`Framework != "websocket"` and
`RouteMethod == ""` — a browser test, not an API-only one) just saves
its screenshot as the baseline. Nothing to review yet. Every run after
that gets diffed against whatever the current baseline is.

## How the diff itself works

- Every run now captures a full-page screenshot regardless of pass/
  fail (`internal/runs`' Playwright config: `screenshot: { mode: 'on',
  fullPage: true }` — previously `only-on-failure`; see
  [docs/RUNNER_ISOLATION.md](RUNNER_ISOLATION.md)).
- `internal/visualdiff.Compare` decodes both PNGs and measures the
  per-pixel RGB Euclidean distance. A pixel counts as "changed" past a
  fixed distance threshold — small enough to tolerate anti-aliasing/
  compression noise, not so small that font-rendering nondeterminism
  between runs produces spurious diffs. **Known limitation**: this is
  plain pixel comparison, not perceptual/SSIM-based — a 1px content
  shift (e.g. from a slightly different font render) can register a
  nonzero percentage even though nothing meaningfully changed. Treat a
  very small percentage on an otherwise-unchanged page with that in
  mind; a v2 could move to a perceptual metric if this proves noisy in
  practice.
- Mismatched screenshot dimensions (a layout change) never error — the
  non-overlapping region simply counts as fully changed.
- The result is a diff PNG: unchanged areas rendered as a dimmed
  grayscale of the current screenshot (for layout context), changed
  pixels highlighted solid red — the same convention as Percy/reg-suit.
- Pixel-identical (`0%` changed) never creates a review row — nothing
  to add to the queue.
- Both the threshold and the RGB-distance method are fixed constants in
  v1, not configurable per project/test case yet.

## What accept/ignore actually do

- **Accept** (`POST /api/v1/visual-diffs/{id}/accept`): marks the diff
  `accepted` and makes the run's screenshot the new baseline for that
  test case. Every future run is compared against it from then on.
- **Ignore** (`POST /api/v1/visual-diffs/{id}/ignore`): marks the diff
  `ignored`. The baseline is untouched — the *next* run with the same
  (still-different) screenshot produces another pending diff, since
  nothing was accepted.
- Both are audited (`visual_diff.accepted` / `visual_diff.ignored`) and
  gated behind `auth.PermApproveTestPlans` when RBAC is enabled — the
  same permission approving a test case itself uses; no new permission
  was introduced for this.

## Data model

- `visual_baselines` — at most one row per test case (`UNIQUE
  (test_case_id)`), pointing at the `artifacts` row that's currently
  the expected screenshot.
- `visual_diffs` — one row per run that produced a nonzero diff:
  `baseline_artifact_id`, `current_artifact_id`, `diff_artifact_id`
  (all pointers into `internal/artifacts`, bytes never duplicated),
  `percent_changed`, and `status` (`pending_review` / `accepted` /
  `ignored`).

Both tables only ever reference existing `artifacts` rows — the
screenshot/diff bytes themselves are served through the exact same `GET
/api/v1/artifacts/{id}/content` endpoint regular run artifacts use.

## Not yet built (deliberately, not silently dropped)

- Per-project or per-test-case sensitivity (the distance threshold is a
  fixed constant today).
- A perceptual/SSIM-based diff instead of plain RGB distance.
