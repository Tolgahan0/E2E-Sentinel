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

func setUpFailingRun(t *testing.T, deps Dependencies, stderr string) (router http.Handler, projectID string, run testRunResponse) {
	t.Helper()
	deps.Runner = &fakeRunner{
		executeFunc: func(context.Context, runs.RunInput) (*runs.RunResult, error) {
			return &runs.RunResult{ExitCode: 1, Stderr: stderr}, nil
		},
	}
	router = NewRouter(deps)

	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "app", "health", "route.ts"), `export async function GET(req) { return Response.json({}) }`)
	projectID = setUpProjectWithDiscovery(t, router, dir)
	testID := approveFirstSuggestedTest(t, router, projectID)
	setEnvironmentBaseURL(t, router, projectID, "http://localhost:3000")

	rec := doJSON(t, router, http.MethodPost, "/api/v1/tests/"+testID+"/run", nil)
	json.Unmarshal(rec.Body.Bytes(), &run)
	waitForRunStatus(t, router, run.ID, "failed", 2*time.Second)
	return router, projectID, run
}

func TestFailedRunCreatesBugCandidate(t *testing.T) {
	router, projectID, _ := setUpFailingRun(t, newTestDeps(nil, nil), "Error: net::ERR_CONNECTION_REFUSED")

	rec := doJSON(t, router, http.MethodGet, "/api/v1/bugs?project_id="+projectID, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		Bugs []bugReportResponse `json:"bugs"`
	}
	json.Unmarshal(rec.Body.Bytes(), &body)
	if len(body.Bugs) != 1 {
		t.Fatalf("got %d bugs, want 1", len(body.Bugs))
	}
	bug := body.Bugs[0]
	if bug.FailureType != "network_failure" {
		t.Errorf("FailureType = %q, want network_failure", bug.FailureType)
	}
	if bug.Severity != "high" {
		t.Errorf("Severity = %q, want high", bug.Severity)
	}
	if bug.Status != "open" {
		t.Errorf("Status = %q, want open", bug.Status)
	}
	if !bug.RootCauseIsUnverifiedHypothesis {
		t.Error("RootCauseIsUnverifiedHypothesis = false, want true")
	}
	if bug.RootCauseHypothesis == "" {
		t.Error("RootCauseHypothesis should not be empty")
	}
	if len(bug.ArtifactIDs) == 0 {
		t.Error("expected the bug's evidence to reference at least one artifact (stderr)")
	}
	if bug.Frequency != 1 {
		t.Errorf("Frequency = %d, want 1", bug.Frequency)
	}
}

func TestRepeatedFailureBumpsFrequencyOnSameBug(t *testing.T) {
	deps := newTestDeps(nil, nil)
	deps.Runner = &fakeRunner{
		executeFunc: func(context.Context, runs.RunInput) (*runs.RunResult, error) {
			return &runs.RunResult{ExitCode: 1, Stderr: "Error: net::ERR_CONNECTION_REFUSED"}, nil
		},
	}
	router := NewRouter(deps)

	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "app", "health", "route.ts"), `export async function GET(req) { return Response.json({}) }`)
	projectID := setUpProjectWithDiscovery(t, router, dir)
	testID := approveFirstSuggestedTest(t, router, projectID)
	setEnvironmentBaseURL(t, router, projectID, "http://localhost:3000")

	for i := 0; i < 2; i++ {
		rec := doJSON(t, router, http.MethodPost, "/api/v1/tests/"+testID+"/run", nil)
		var run testRunResponse
		json.Unmarshal(rec.Body.Bytes(), &run)
		waitForRunStatus(t, router, run.ID, "failed", 2*time.Second)
	}

	listRec := doJSON(t, router, http.MethodGet, "/api/v1/bugs?project_id="+projectID, nil)
	var body struct {
		Bugs []bugReportResponse `json:"bugs"`
	}
	json.Unmarshal(listRec.Body.Bytes(), &body)
	if len(body.Bugs) != 1 {
		t.Fatalf("got %d bugs, want exactly 1 (repeated failures should update, not duplicate)", len(body.Bugs))
	}
	if body.Bugs[0].Frequency != 2 {
		t.Errorf("Frequency = %d, want 2", body.Bugs[0].Frequency)
	}
}

func TestResolveThenReopensOnRecurrence(t *testing.T) {
	deps := newTestDeps(nil, nil)
	router, projectID, _ := setUpFailingRun(t, deps, "Error: net::ERR_CONNECTION_REFUSED")

	listRec := doJSON(t, router, http.MethodGet, "/api/v1/bugs?project_id="+projectID, nil)
	var body struct {
		Bugs []bugReportResponse `json:"bugs"`
	}
	json.Unmarshal(listRec.Body.Bytes(), &body)
	bugID := body.Bugs[0].ID
	testID := body.Bugs[0].TestCaseID

	resolveRec := doJSON(t, router, http.MethodPost, "/api/v1/bugs/"+bugID+"/resolve", nil)
	if resolveRec.Code != http.StatusOK {
		t.Fatalf("resolve status = %d, body=%s", resolveRec.Code, resolveRec.Body.String())
	}
	var resolved bugReportResponse
	json.Unmarshal(resolveRec.Body.Bytes(), &resolved)
	if resolved.Status != "resolved" {
		t.Fatalf("Status = %q, want resolved", resolved.Status)
	}

	// Trigger the same failure again.
	rec := doJSON(t, router, http.MethodPost, "/api/v1/tests/"+testID+"/run", nil)
	var run testRunResponse
	json.Unmarshal(rec.Body.Bytes(), &run)
	waitForRunStatus(t, router, run.ID, "failed", 2*time.Second)

	getRec := doJSON(t, router, http.MethodGet, "/api/v1/bugs/"+bugID, nil)
	var reopened bugReportResponse
	json.Unmarshal(getRec.Body.Bytes(), &reopened)
	if reopened.Status != "reopened" {
		t.Errorf("Status = %q, want reopened after a recurrence of a resolved bug", reopened.Status)
	}
}

func TestAddBugNote(t *testing.T) {
	router, projectID, _ := setUpFailingRun(t, newTestDeps(nil, nil), "Error: net::ERR_CONNECTION_REFUSED")
	listRec := doJSON(t, router, http.MethodGet, "/api/v1/bugs?project_id="+projectID, nil)
	var body struct {
		Bugs []bugReportResponse `json:"bugs"`
	}
	json.Unmarshal(listRec.Body.Bytes(), &body)
	bugID := body.Bugs[0].ID

	rec := doJSON(t, router, http.MethodPost, "/api/v1/bugs/"+bugID+"/notes", map[string]string{"author": "alice", "text": "investigating"})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var updated bugReportResponse
	json.Unmarshal(rec.Body.Bytes(), &updated)
	if len(updated.Notes) != 1 || updated.Notes[0].Text != "investigating" {
		t.Fatalf("Notes = %+v", updated.Notes)
	}
}

func TestAddBugNote_RequiresText(t *testing.T) {
	router, projectID, _ := setUpFailingRun(t, newTestDeps(nil, nil), "Error: net::ERR_CONNECTION_REFUSED")
	listRec := doJSON(t, router, http.MethodGet, "/api/v1/bugs?project_id="+projectID, nil)
	var body struct {
		Bugs []bugReportResponse `json:"bugs"`
	}
	json.Unmarshal(listRec.Body.Bytes(), &body)

	rec := doJSON(t, router, http.MethodPost, "/api/v1/bugs/"+body.Bugs[0].ID+"/notes", map[string]string{"author": "alice"})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestExportBugMarkdown(t *testing.T) {
	router, projectID, _ := setUpFailingRun(t, newTestDeps(nil, nil), "Error: net::ERR_CONNECTION_REFUSED")
	listRec := doJSON(t, router, http.MethodGet, "/api/v1/bugs?project_id="+projectID, nil)
	var body struct {
		Bugs []bugReportResponse `json:"bugs"`
	}
	json.Unmarshal(listRec.Body.Bytes(), &body)

	rec := doJSON(t, router, http.MethodGet, "/api/v1/bugs/"+body.Bugs[0].ID+"/export/markdown", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "text/markdown") {
		t.Errorf("Content-Type = %q, want text/markdown", ct)
	}
	if rec.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Error("missing X-Content-Type-Options: nosniff")
	}
	if !strings.Contains(rec.Header().Get("Content-Disposition"), "attachment") {
		t.Error("expected a forced download Content-Disposition")
	}
	if !strings.Contains(rec.Body.String(), "unverified hypothesis") {
		t.Error("markdown export must label the root cause as an unverified hypothesis")
	}
}

func TestExportBugJSON(t *testing.T) {
	router, projectID, _ := setUpFailingRun(t, newTestDeps(nil, nil), "Error: net::ERR_CONNECTION_REFUSED")
	listRec := doJSON(t, router, http.MethodGet, "/api/v1/bugs?project_id="+projectID, nil)
	var body struct {
		Bugs []bugReportResponse `json:"bugs"`
	}
	json.Unmarshal(listRec.Body.Bytes(), &body)

	rec := doJSON(t, router, http.MethodGet, "/api/v1/bugs/"+body.Bugs[0].ID+"/export/json", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var exported map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &exported); err != nil {
		t.Fatalf("invalid JSON export: %v", err)
	}
	if exported["root_cause_is_unverified_hypothesis"] != true {
		t.Error("JSON export must flag the root cause as an unverified hypothesis")
	}
}

func TestGetBug_NotFound(t *testing.T) {
	router := NewRouter(newTestDeps(nil, nil))
	rec := doJSON(t, router, http.MethodGet, "/api/v1/bugs/does-not-exist", nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestListBugs_FiltersBySeverity(t *testing.T) {
	router, projectID, _ := setUpFailingRun(t, newTestDeps(nil, nil), "Error: net::ERR_CONNECTION_REFUSED")

	rec := doJSON(t, router, http.MethodGet, "/api/v1/bugs?project_id="+projectID+"&severity=low", nil)
	var body struct {
		Bugs []bugReportResponse `json:"bugs"`
	}
	json.Unmarshal(rec.Body.Bytes(), &body)
	if len(body.Bugs) != 0 {
		t.Errorf("severity=low filter returned %d bugs, want 0 (the seeded failure is high severity)", len(body.Bugs))
	}
}
