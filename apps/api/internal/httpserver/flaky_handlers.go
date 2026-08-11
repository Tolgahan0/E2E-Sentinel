package httpserver

import (
	"net/http"
	"sort"

	"github.com/go-chi/chi/v5"

	"e2e-sentinel/apps/api/internal/failures"
	"e2e-sentinel/apps/api/internal/runs"
)

// flakyTierRank orders assessment tiers most-actionable-first for the
// dashboard: a confirmed-flaky test needs attention before a merely
// "suspect" one, and likely_real_defect (a consistently failing test —
// a different problem than flakiness) sits below the flaky tiers
// rather than competing for top attention here. insufficient_evidence
// (only one run so far) is real information, never hidden, just last.
var flakyTierRank = map[string]int{
	failures.FlakyConfirmed:            0,
	failures.FlakyCandidate:            1,
	failures.FlakySuspect:              2,
	failures.FlakyLikelyRealDefect:     3,
	failures.FlakyInsufficientEvidence: 4,
}

// recentHistoryLimit bounds the dot-row sparkline the web UI renders —
// enough to see a pattern, not so much the response balloons for a
// test case with hundreds of runs.
const recentHistoryLimit = 10

type flakyTestResponse struct {
	TestCaseID     string   `json:"test_case_id"`
	Title          string   `json:"title"`
	Assessment     string   `json:"assessment"`
	TotalRuns      int      `json:"total_runs"`
	RecentStatuses []string `json:"recent_statuses"`
}

// handleListFlakyTests computes internal/failures.AssessFlakiness for
// every test case in the project that has at least one run — proactive
// and project-wide, unlike the only other place this assessment is
// computed today (recordFailureAndBug in failure_correlation.go, which
// only runs reactively on a failing run and only ever surfaces the
// latest value on that failure's bug report). Computed on read, no new
// migration or persisted field — see docs/FAILURE_CORRELATION.md for
// the N+1-query trade-off this implies.
func handleListFlakyTests(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		projectID := chi.URLParam(r, "projectID")

		cases, err := deps.Planning.List(r.Context(), projectID)
		if err != nil {
			deps.Logger.Error().Err(err).Msg("listing test cases for flaky assessment failed")
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal_error"})
			return
		}

		out := make([]flakyTestResponse, 0, len(cases))
		for _, tc := range cases {
			history, err := deps.Runs.ListByTestCase(r.Context(), tc.ID)
			if err != nil {
				deps.Logger.Error().Err(err).Str("test_case_id", tc.ID).Msg("listing run history for flaky assessment failed")
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal_error"})
				return
			}
			if len(history) == 0 {
				continue // never run — nothing to assess
			}

			var passed []bool
			statuses := make([]string, 0, len(history))
			for _, run := range history {
				switch run.Status {
				case runs.StatusPassed:
					passed = append(passed, true)
				case runs.StatusFailed, runs.StatusError:
					passed = append(passed, false)
				}
				statuses = append(statuses, run.Status)
			}
			if len(statuses) > recentHistoryLimit {
				statuses = statuses[len(statuses)-recentHistoryLimit:]
			}

			out = append(out, flakyTestResponse{
				TestCaseID: tc.ID, Title: tc.Title, Assessment: failures.AssessFlakiness(passed),
				TotalRuns: len(history), RecentStatuses: statuses,
			})
		}

		sort.SliceStable(out, func(i, j int) bool {
			return flakyTierRank[out[i].Assessment] < flakyTierRank[out[j].Assessment]
		})
		writeJSON(w, http.StatusOK, map[string]any{"flaky_tests": out})
	}
}
