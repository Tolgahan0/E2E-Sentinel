package httpserver

import (
	"context"
	"encoding/json"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"e2e-sentinel/apps/api/internal/runs"
)

func TestHandleMetrics_ExposesHTTPRequestCounter(t *testing.T) {
	deps := newTestDeps(nil, nil)
	router := NewRouter(deps)

	doJSON(t, router, http.MethodGet, "/api/v1/projects", nil)

	rec := doJSON(t, router, http.MethodGet, "/metrics", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "e2e_sentinel_http_requests_total") {
		t.Errorf("missing e2e_sentinel_http_requests_total in:\n%s", body)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "text/plain") {
		t.Errorf("Content-Type = %q, want text/plain", ct)
	}
}

func TestHandleMetrics_TracksTestRunOutcome(t *testing.T) {
	deps := newTestDeps(nil, nil)
	deps.Runner = &fakeRunner{
		executeFunc: func(_ context.Context, _ runs.RunInput) (*runs.RunResult, error) {
			return &runs.RunResult{ExitCode: 0, Stdout: "1 passed"}, nil
		},
	}
	router := NewRouter(deps)

	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "app", "health", "route.ts"), `export async function GET(req) { return Response.json({}) }`)
	projectID := setUpProjectWithDiscovery(t, router, dir)
	testID := approveFirstSuggestedTest(t, router, projectID)
	setEnvironmentBaseURL(t, router, projectID, "http://localhost:3000")

	rec := doJSON(t, router, http.MethodPost, "/api/v1/tests/"+testID+"/run", nil)
	var run testRunResponse
	json.Unmarshal(rec.Body.Bytes(), &run)
	waitForRunStatus(t, router, run.ID, "passed", 2*time.Second)

	metricsRec := doJSON(t, router, http.MethodGet, "/metrics", nil)
	body := metricsRec.Body.String()
	if !strings.Contains(body, `e2e_sentinel_test_runs_total{status="passed"} 1`) {
		t.Errorf("missing passed test run counter in:\n%s", body)
	}
	if !strings.Contains(body, "e2e_sentinel_active_test_runs 0") {
		t.Errorf("expected active_test_runs to return to 0 after completion:\n%s", body)
	}
}
