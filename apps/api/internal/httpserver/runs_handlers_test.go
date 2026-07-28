package httpserver

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"e2e-sentinel/apps/api/internal/runs"
)

type fakeRunner struct {
	mu           sync.Mutex
	executeFunc  func(ctx context.Context, input runs.RunInput) (*runs.RunResult, error)
	cancelCalls  []string
	cleanupCalls []string
	artifacts    []runs.ArtifactFile
}

func (f *fakeRunner) Name() string                                  { return "fake-runner" }
func (f *fakeRunner) Validate(context.Context, runs.RunInput) error { return nil }
func (f *fakeRunner) Execute(ctx context.Context, input runs.RunInput) (*runs.RunResult, error) {
	return f.executeFunc(ctx, input)
}
func (f *fakeRunner) CollectArtifacts(context.Context, string) ([]runs.ArtifactFile, error) {
	return f.artifacts, nil
}
func (f *fakeRunner) Cancel(_ context.Context, runID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.cancelCalls = append(f.cancelCalls, runID)
	return nil
}
func (f *fakeRunner) Cleanup(_ context.Context, runID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.cleanupCalls = append(f.cleanupCalls, runID)
	return nil
}

func (f *fakeRunner) cancelledRuns() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, len(f.cancelCalls))
	copy(out, f.cancelCalls)
	return out
}

func (f *fakeRunner) cleanedUpRuns() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, len(f.cleanupCalls))
	copy(out, f.cleanupCalls)
	return out
}

// approveFirstSuggestedTest generates a plan for projectID and approves
// the first suggested test case, returning its ID.
func approveFirstSuggestedTest(t *testing.T, router http.Handler, projectID string) string {
	t.Helper()
	planRec := doJSON(t, router, http.MethodPost, "/api/v1/projects/"+projectID+"/tests/plan", nil)
	var plan struct {
		Tests []struct {
			ID string `json:"id"`
		} `json:"tests"`
	}
	json.Unmarshal(planRec.Body.Bytes(), &plan)
	if len(plan.Tests) == 0 {
		t.Fatal("expected at least one suggested test case")
	}
	testID := plan.Tests[0].ID

	approveRec := doJSON(t, router, http.MethodPost, "/api/v1/tests/"+testID+"/approve", nil)
	if approveRec.Code != http.StatusOK {
		t.Fatalf("approve status = %d, body=%s", approveRec.Code, approveRec.Body.String())
	}
	return testID
}

func setEnvironmentBaseURL(t *testing.T, router http.Handler, projectID, baseURL string) {
	t.Helper()
	envListRec := doJSON(t, router, http.MethodGet, "/api/v1/projects/"+projectID+"/environments", nil)
	var envList struct {
		Environments []struct {
			ID string `json:"id"`
		} `json:"environments"`
	}
	json.Unmarshal(envListRec.Body.Bytes(), &envList)
	if len(envList.Environments) == 0 {
		t.Fatal("expected a default environment")
	}
	rec := doJSON(t, router, http.MethodPatch, "/api/v1/environments/"+envList.Environments[0].ID, map[string]string{"base_url": baseURL})
	if rec.Code != http.StatusOK {
		t.Fatalf("setting base_url status = %d, body=%s", rec.Code, rec.Body.String())
	}
}

func waitForRunStatus(t *testing.T, router http.Handler, runID, want string, timeout time.Duration) testRunResponse {
	t.Helper()
	deadline := time.Now().Add(timeout)
	var last testRunResponse
	for time.Now().Before(deadline) {
		rec := doJSON(t, router, http.MethodGet, "/api/v1/runs/"+runID, nil)
		json.Unmarshal(rec.Body.Bytes(), &last)
		if last.Status == want {
			return last
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("run %s did not reach status %q within %v (last status: %q)", runID, want, timeout, last.Status)
	return testRunResponse{}
}

func TestRunTest_RequiresRunnerConfigured(t *testing.T) {
	deps := newTestDeps(nil, nil)
	router := NewRouter(deps)

	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "app", "health", "route.ts"), `export async function GET(req) { return Response.json({}) }`)
	projectID := setUpProjectWithDiscovery(t, router, dir)
	testID := approveFirstSuggestedTest(t, router, projectID)

	rec := doJSON(t, router, http.MethodPost, "/api/v1/tests/"+testID+"/run", nil)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503 (no runner configured)", rec.Code)
	}
}

func TestRunTest_RequiresApproval(t *testing.T) {
	deps := newTestDeps(nil, nil)
	deps.Runner = &fakeRunner{}
	router := NewRouter(deps)

	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "app", "health", "route.ts"), `export async function GET(req) { return Response.json({}) }`)
	projectID := setUpProjectWithDiscovery(t, router, dir)

	planRec := doJSON(t, router, http.MethodPost, "/api/v1/projects/"+projectID+"/tests/plan", nil)
	var plan struct {
		Tests []struct {
			ID string `json:"id"`
		} `json:"tests"`
	}
	json.Unmarshal(planRec.Body.Bytes(), &plan)

	rec := doJSON(t, router, http.MethodPost, "/api/v1/tests/"+plan.Tests[0].ID+"/run", nil)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 (test not approved), body=%s", rec.Code, rec.Body.String())
	}
}

func TestRunTest_RequiresEnvironmentBaseURL(t *testing.T) {
	deps := newTestDeps(nil, nil)
	deps.Runner = &fakeRunner{}
	router := NewRouter(deps)

	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "app", "health", "route.ts"), `export async function GET(req) { return Response.json({}) }`)
	projectID := setUpProjectWithDiscovery(t, router, dir)
	testID := approveFirstSuggestedTest(t, router, projectID)

	rec := doJSON(t, router, http.MethodPost, "/api/v1/tests/"+testID+"/run", nil)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422 (no base_url set), body=%s", rec.Code, rec.Body.String())
	}
}

func TestRunTest_FullFlow_Passes(t *testing.T) {
	deps := newTestDeps(nil, nil)
	fake := &fakeRunner{
		executeFunc: func(context.Context, runs.RunInput) (*runs.RunResult, error) {
			return &runs.RunResult{ExitCode: 0, Stdout: "1 passed"}, nil
		},
	}
	deps.Runner = fake
	router := NewRouter(deps)

	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "app", "health", "route.ts"), `export async function GET(req) { return Response.json({}) }`)
	projectID := setUpProjectWithDiscovery(t, router, dir)
	testID := approveFirstSuggestedTest(t, router, projectID)
	setEnvironmentBaseURL(t, router, projectID, "http://localhost:3000")

	rec := doJSON(t, router, http.MethodPost, "/api/v1/tests/"+testID+"/run", nil)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202, body=%s", rec.Code, rec.Body.String())
	}
	var run testRunResponse
	json.Unmarshal(rec.Body.Bytes(), &run)
	if run.Status != "queued" {
		t.Errorf("initial Status = %q, want queued", run.Status)
	}

	final := waitForRunStatus(t, router, run.ID, "passed", 2*time.Second)
	if final.ExitCode == nil || *final.ExitCode != 0 {
		t.Errorf("ExitCode = %v, want 0", final.ExitCode)
	}

	artifactsRec := doJSON(t, router, http.MethodGet, "/api/v1/runs/"+run.ID+"/artifacts", nil)
	var artifactsBody struct {
		Artifacts []struct {
			Kind string `json:"kind"`
		} `json:"artifacts"`
	}
	json.Unmarshal(artifactsRec.Body.Bytes(), &artifactsBody)
	foundStdout := false
	for _, a := range artifactsBody.Artifacts {
		if a.Kind == "stdout" {
			foundStdout = true
		}
	}
	if !foundStdout {
		t.Errorf("expected a stdout artifact, got %+v", artifactsBody.Artifacts)
	}
}

func TestRunTest_FullFlow_Fails(t *testing.T) {
	deps := newTestDeps(nil, nil)
	deps.Runner = &fakeRunner{
		executeFunc: func(context.Context, runs.RunInput) (*runs.RunResult, error) {
			return &runs.RunResult{ExitCode: 1, Stderr: "1 failed"}, nil
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

	final := waitForRunStatus(t, router, run.ID, "failed", 2*time.Second)
	if final.ExitCode == nil || *final.ExitCode != 1 {
		t.Errorf("ExitCode = %v, want 1", final.ExitCode)
	}
}

// TestRunTest_ExecuteFailureStillCleansUpWorkspace regression-tests a
// real bug found during manual verification: when Runner.Execute fails
// outright (e.g. the container never started), the workspace directory
// it may have already written the spec file into was never cleaned up,
// because the early-return error path skipped straight past the
// Cleanup call that only ran on the success path.
func TestRunTest_ExecuteFailureStillCleansUpWorkspace(t *testing.T) {
	deps := newTestDeps(nil, nil)
	fake := &fakeRunner{
		executeFunc: func(context.Context, runs.RunInput) (*runs.RunResult, error) {
			return nil, errors.New("simulated infra failure creating the container")
		},
	}
	deps.Runner = fake
	router := NewRouter(deps)

	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "app", "health", "route.ts"), `export async function GET(req) { return Response.json({}) }`)
	projectID := setUpProjectWithDiscovery(t, router, dir)
	testID := approveFirstSuggestedTest(t, router, projectID)
	setEnvironmentBaseURL(t, router, projectID, "http://localhost:3000")

	rec := doJSON(t, router, http.MethodPost, "/api/v1/tests/"+testID+"/run", nil)
	var run testRunResponse
	json.Unmarshal(rec.Body.Bytes(), &run)

	final := waitForRunStatus(t, router, run.ID, "error", 2*time.Second)
	if final.Summary == "" {
		t.Error("expected the run's summary to record the execution error")
	}

	if calls := fake.cleanedUpRuns(); len(calls) != 1 || calls[0] != run.ID {
		t.Errorf("Runner.Cleanup calls = %v, want [%s] (must clean up even when Execute fails)", calls, run.ID)
	}
}

func TestCancelRun_MarksCancelledAndCallsRunnerCancel(t *testing.T) {
	deps := newTestDeps(nil, nil)
	release := make(chan struct{})
	fake := &fakeRunner{
		executeFunc: func(ctx context.Context, input runs.RunInput) (*runs.RunResult, error) {
			<-release // block until the test lets it finish
			return &runs.RunResult{ExitCode: 137}, nil
		},
	}
	deps.Runner = fake
	router := NewRouter(deps)
	defer close(release)

	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "app", "health", "route.ts"), `export async function GET(req) { return Response.json({}) }`)
	projectID := setUpProjectWithDiscovery(t, router, dir)
	testID := approveFirstSuggestedTest(t, router, projectID)
	setEnvironmentBaseURL(t, router, projectID, "http://localhost:3000")

	rec := doJSON(t, router, http.MethodPost, "/api/v1/tests/"+testID+"/run", nil)
	var run testRunResponse
	json.Unmarshal(rec.Body.Bytes(), &run)

	waitForRunStatus(t, router, run.ID, "running", 2*time.Second)

	cancelRec := doJSON(t, router, http.MethodPost, "/api/v1/runs/"+run.ID+"/cancel", nil)
	if cancelRec.Code != http.StatusOK {
		t.Fatalf("cancel status = %d, body=%s", cancelRec.Code, cancelRec.Body.String())
	}
	var cancelled testRunResponse
	json.Unmarshal(cancelRec.Body.Bytes(), &cancelled)
	if cancelled.Status != "cancelled" {
		t.Errorf("Status = %q, want cancelled", cancelled.Status)
	}

	if calls := fake.cancelledRuns(); len(calls) != 1 || calls[0] != run.ID {
		t.Errorf("Runner.Cancel calls = %v, want [%s]", calls, run.ID)
	}

	// A second cancel attempt must be rejected — the run already finished.
	second := doJSON(t, router, http.MethodPost, "/api/v1/runs/"+run.ID+"/cancel", nil)
	if second.Code != http.StatusConflict {
		t.Errorf("second cancel status = %d, want 409", second.Code)
	}
}

func TestGetRun_NotFound(t *testing.T) {
	deps := newTestDeps(nil, nil)
	router := NewRouter(deps)
	rec := doJSON(t, router, http.MethodGet, "/api/v1/runs/does-not-exist", nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}
