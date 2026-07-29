package httpserver

import (
	"context"
	"encoding/json"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"e2e-sentinel/apps/api/internal/runs"
)

// approveWebSocketTest generates a plan for projectID and approves the
// first "websocket" framework test case it finds — unlike
// approveFirstSuggestedTest, this doesn't assume it's plan.Tests[0],
// since a fixture may also produce ordinary HTTP routes.
func approveWebSocketTest(t *testing.T, router http.Handler, projectID string) string {
	t.Helper()
	planRec := doJSON(t, router, http.MethodPost, "/api/v1/projects/"+projectID+"/tests/plan", nil)
	var plan struct {
		Tests []struct {
			ID        string `json:"id"`
			Framework string `json:"framework"`
		} `json:"tests"`
	}
	json.Unmarshal(planRec.Body.Bytes(), &plan)

	var testID string
	for _, tc := range plan.Tests {
		if tc.Framework == "websocket" {
			testID = tc.ID
			break
		}
	}
	if testID == "" {
		t.Fatalf("expected a websocket framework test case, got %+v", plan.Tests)
	}

	approveRec := doJSON(t, router, http.MethodPost, "/api/v1/tests/"+testID+"/approve", nil)
	if approveRec.Code != http.StatusOK {
		t.Fatalf("approve status = %d, body=%s", approveRec.Code, approveRec.Body.String())
	}
	return testID
}

func TestRunTest_WebSocketFramework_RequiresWebSocketRunnerConfigured(t *testing.T) {
	deps := newTestDeps(nil, nil)
	deps.Runner = &fakeRunner{} // the OTHER runner is configured, but not WebSocketRunner
	router := NewRouter(deps)

	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "client.js"), `new WebSocket("ws://localhost:8080/socket");`)
	projectID := setUpProjectWithDiscovery(t, router, dir)
	testID := approveWebSocketTest(t, router, projectID)

	rec := doJSON(t, router, http.MethodPost, "/api/v1/tests/"+testID+"/run", nil)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503 (WebSocketRunner not configured, even though Runner is), body=%s", rec.Code, rec.Body.String())
	}
}

func TestRunTest_WebSocketFramework_DoesNotRequireEnvironmentBaseURL(t *testing.T) {
	deps := newTestDeps(nil, nil)
	fake := &fakeRunner{executeFunc: func(context.Context, runs.RunInput) (*runs.RunResult, error) {
		return &runs.RunResult{ExitCode: 0, Stdout: "connected"}, nil
	}}
	deps.WebSocketRunner = fake
	router := NewRouter(deps)

	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "client.js"), `new WebSocket("ws://localhost:8080/socket");`)
	projectID := setUpProjectWithDiscovery(t, router, dir)
	testID := approveWebSocketTest(t, router, projectID)
	// Deliberately NOT calling setEnvironmentBaseURL — a websocket test
	// must run without one.

	rec := doJSON(t, router, http.MethodPost, "/api/v1/tests/"+testID+"/run", nil)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202 (no base_url required for websocket), body=%s", rec.Code, rec.Body.String())
	}
	var run testRunResponse
	json.Unmarshal(rec.Body.Bytes(), &run)
	if run.RunnerType != fake.Name() {
		t.Errorf("RunnerType = %q, want %q", run.RunnerType, fake.Name())
	}

	final := waitForRunStatus(t, router, run.ID, "passed", 2*time.Second)
	if final.ExitCode == nil || *final.ExitCode != 0 {
		t.Errorf("ExitCode = %v, want 0", final.ExitCode)
	}
}

func TestRunTest_WebSocketFramework_GeneratedSpecTargetsRoutePathDirectly(t *testing.T) {
	deps := newTestDeps(nil, nil)
	var gotSpecContent string
	fake := &fakeRunner{executeFunc: func(_ context.Context, input runs.RunInput) (*runs.RunResult, error) {
		gotSpecContent = input.SpecContent
		return &runs.RunResult{ExitCode: 0}, nil
	}}
	deps.WebSocketRunner = fake
	router := NewRouter(deps)

	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "client.js"), `new WebSocket("ws://localhost:9999/echo");`)
	projectID := setUpProjectWithDiscovery(t, router, dir)
	testID := approveWebSocketTest(t, router, projectID)

	rec := doJSON(t, router, http.MethodPost, "/api/v1/tests/"+testID+"/run", nil)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var run testRunResponse
	json.Unmarshal(rec.Body.Bytes(), &run)
	waitForRunStatus(t, router, run.ID, "passed", 2*time.Second)

	if gotSpecContent == "" {
		t.Fatal("Execute was never called with spec content")
	}
	if !contains(gotSpecContent, `"ws://localhost:9999/echo"`) {
		t.Errorf("generated spec does not target the discovered URL directly:\n%s", gotSpecContent)
	}
}

func TestRunTest_WebSocketFramework_FailingConnectionRecordsFailedStatus(t *testing.T) {
	deps := newTestDeps(nil, nil)
	fake := &fakeRunner{executeFunc: func(context.Context, runs.RunInput) (*runs.RunResult, error) {
		return &runs.RunResult{ExitCode: 1, Stderr: "FAIL: connection error"}, nil
	}}
	deps.WebSocketRunner = fake
	router := NewRouter(deps)

	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "client.js"), `new WebSocket("ws://unreachable-host:1/socket");`)
	projectID := setUpProjectWithDiscovery(t, router, dir)
	testID := approveWebSocketTest(t, router, projectID)

	rec := doJSON(t, router, http.MethodPost, "/api/v1/tests/"+testID+"/run", nil)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var run testRunResponse
	json.Unmarshal(rec.Body.Bytes(), &run)

	final := waitForRunStatus(t, router, run.ID, "failed", 2*time.Second)
	if final.ExitCode == nil || *final.ExitCode != 1 {
		t.Errorf("ExitCode = %v, want 1", final.ExitCode)
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && indexOf(s, substr) >= 0
}

func indexOf(s, substr string) int {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}
