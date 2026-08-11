package httpserver

import (
	"context"
	"encoding/json"
	"net/http"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"e2e-sentinel/apps/api/internal/runs"
)

// sequenceRunner is a fakeRunner whose Execute result cycles through a
// fixed sequence of exit codes across successive runs of the SAME test
// case — exactly what's needed to build a real pass/fail history for
// AssessFlakiness through the actual HTTP run pipeline rather than
// seeding TestRun rows directly.
func sequenceRunner(t *testing.T, exitCodes []int) *fakeRunner {
	t.Helper()
	var mu sync.Mutex
	i := 0
	return &fakeRunner{
		executeFunc: func(context.Context, runs.RunInput) (*runs.RunResult, error) {
			mu.Lock()
			defer mu.Unlock()
			if i >= len(exitCodes) {
				t.Fatalf("sequenceRunner: more runs requested (%d) than exit codes provided (%d)", i+1, len(exitCodes))
			}
			code := exitCodes[i]
			i++
			return &runs.RunResult{ExitCode: code}, nil
		},
	}
}

func listFlakyTests(t *testing.T, router http.Handler, projectID string) []flakyTestResponse {
	t.Helper()
	rec := doJSON(t, router, http.MethodGet, "/api/v1/projects/"+projectID+"/flaky-tests", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		FlakyTests []flakyTestResponse `json:"flaky_tests"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decoding flaky tests response: %v", err)
	}
	return body.FlakyTests
}

func TestFlakyTests_ExcludesNeverRunTestCase(t *testing.T) {
	deps := newTestDeps(nil, nil)
	deps.Runner = sequenceRunner(t, []int{0})
	router := NewRouter(deps)

	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "app", "health", "route.ts"), `export async function GET(req) { return Response.json({}) }`)
	mustWrite(t, filepath.Join(dir, "app", "widgets", "route.ts"), `export async function POST(req) { return Response.json({}) }`)
	projectID := setUpProjectWithDiscovery(t, router, dir)
	setEnvironmentBaseURL(t, router, projectID, "http://localhost:3000")

	planRec := doJSON(t, router, http.MethodPost, "/api/v1/projects/"+projectID+"/tests/plan", nil)
	var plan struct {
		Tests []struct {
			ID    string `json:"id"`
			Title string `json:"title"`
		} `json:"tests"`
	}
	json.Unmarshal(planRec.Body.Bytes(), &plan)
	if len(plan.Tests) < 2 {
		t.Fatalf("expected at least 2 suggested test cases (GET + POST routes), got %d", len(plan.Tests))
	}

	// Approve and run only the first suggested test case; every other
	// suggested test case (from the POST route) is left pending/never
	// run.
	ranTestID := plan.Tests[0].ID
	approveRec := doJSON(t, router, http.MethodPost, "/api/v1/tests/"+ranTestID+"/approve", nil)
	if approveRec.Code != http.StatusOK {
		t.Fatalf("approve status = %d, body=%s", approveRec.Code, approveRec.Body.String())
	}

	runRec := doJSON(t, router, http.MethodPost, "/api/v1/tests/"+ranTestID+"/run", nil)
	if runRec.Code != http.StatusAccepted {
		t.Fatalf("run status = %d, body=%s", runRec.Code, runRec.Body.String())
	}
	var run testRunResponse
	json.Unmarshal(runRec.Body.Bytes(), &run)
	waitForRunStatus(t, router, run.ID, "passed", 2*time.Second)

	flaky := listFlakyTests(t, router, projectID)
	if len(flaky) != 1 {
		t.Fatalf("flaky tests returned = %d, want 1 (only the run test case)", len(flaky))
	}
	if flaky[0].TestCaseID != ranTestID {
		t.Errorf("TestCaseID = %q, want %q", flaky[0].TestCaseID, ranTestID)
	}
	if flaky[0].Assessment != "insufficient_evidence" {
		t.Errorf("Assessment = %q, want insufficient_evidence (only 1 run so far)", flaky[0].Assessment)
	}
	if flaky[0].TotalRuns != 1 {
		t.Errorf("TotalRuns = %d, want 1", flaky[0].TotalRuns)
	}
}

func TestFlakyTests_ComputesRealAssessmentTiers(t *testing.T) {
	cases := []struct {
		name       string
		exitCodes  []int // 0 = pass, nonzero = fail, in run order
		wantAssess string
	}{
		{"suspect: isolated failure after a pass", []int{0, 1}, "suspect"},
		{"flaky: 75% failure rate with at least one pass", []int{0, 1, 1, 1}, "flaky"},
		{"flaky_candidate: mixed but under threshold", []int{0, 0, 1, 0}, "flaky_candidate"},
		{"likely_real_defect: every run failed", []int{1, 1}, "likely_real_defect"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			deps := newTestDeps(nil, nil)
			deps.Runner = sequenceRunner(t, c.exitCodes)
			router := NewRouter(deps)

			dir := t.TempDir()
			mustWrite(t, filepath.Join(dir, "app", "health", "route.ts"), `export async function GET(req) { return Response.json({}) }`)
			projectID := setUpProjectWithDiscovery(t, router, dir)
			setEnvironmentBaseURL(t, router, projectID, "http://localhost:3000")
			testID := approveFirstSuggestedTest(t, router, projectID)

			for i, code := range c.exitCodes {
				runRec := doJSON(t, router, http.MethodPost, "/api/v1/tests/"+testID+"/run", nil)
				if runRec.Code != http.StatusAccepted {
					t.Fatalf("run #%d status = %d, body=%s", i+1, runRec.Code, runRec.Body.String())
				}
				var run testRunResponse
				json.Unmarshal(runRec.Body.Bytes(), &run)
				wantStatus := "passed"
				if code != 0 {
					wantStatus = "failed"
				}
				waitForRunStatus(t, router, run.ID, wantStatus, 2*time.Second)
			}

			flaky := listFlakyTests(t, router, projectID)
			if len(flaky) != 1 {
				t.Fatalf("flaky tests returned = %d, want 1", len(flaky))
			}
			if flaky[0].Assessment != c.wantAssess {
				t.Errorf("Assessment = %q, want %q", flaky[0].Assessment, c.wantAssess)
			}
			if flaky[0].TotalRuns != len(c.exitCodes) {
				t.Errorf("TotalRuns = %d, want %d", flaky[0].TotalRuns, len(c.exitCodes))
			}
			if len(flaky[0].RecentStatuses) != len(c.exitCodes) {
				t.Errorf("len(RecentStatuses) = %d, want %d", len(flaky[0].RecentStatuses), len(c.exitCodes))
			}
		})
	}
}
