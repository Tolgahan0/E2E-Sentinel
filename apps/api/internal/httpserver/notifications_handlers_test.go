package httpserver

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"e2e-sentinel/apps/api/internal/runs"
	"e2e-sentinel/apps/api/internal/webhooks"
)

func TestGetWebhookConfig_ReportsUnconfiguredByDefault(t *testing.T) {
	router := NewRouter(newTestDeps(nil, nil))
	rec := doJSON(t, router, http.MethodGet, "/api/v1/notifications/webhook", nil)
	var body struct {
		Configured bool   `json:"configured"`
		URL        string `json:"url"`
	}
	json.Unmarshal(rec.Body.Bytes(), &body)
	if body.Configured || body.URL != "" {
		t.Errorf("body = %+v, want unconfigured", body)
	}
}

func TestUpdateWebhookConfig_SetsAndReadsBack(t *testing.T) {
	router := NewRouter(newTestDeps(nil, nil))
	rec := doJSON(t, router, http.MethodPatch, "/api/v1/notifications/webhook", map[string]string{"url": "https://example.com/hook"})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}

	getRec := doJSON(t, router, http.MethodGet, "/api/v1/notifications/webhook", nil)
	var body struct {
		Configured bool   `json:"configured"`
		URL        string `json:"url"`
	}
	json.Unmarshal(getRec.Body.Bytes(), &body)
	if !body.Configured || body.URL != "https://example.com/hook" {
		t.Errorf("body = %+v, want configured with the saved URL", body)
	}
}

func TestTestWebhook_RequiresConfiguration(t *testing.T) {
	router := NewRouter(newTestDeps(nil, nil))
	rec := doJSON(t, router, http.MethodPost, "/api/v1/notifications/webhook/test", nil)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", rec.Code)
	}
}

func TestTestWebhook_DeliversToConfiguredURL(t *testing.T) {
	var mu sync.Mutex
	var received webhooks.Event
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		json.NewDecoder(r.Body).Decode(&received)
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	router := NewRouter(newTestDeps(nil, nil))
	doJSON(t, router, http.MethodPatch, "/api/v1/notifications/webhook", map[string]string{"url": server.URL})

	rec := doJSON(t, router, http.MethodPost, "/api/v1/notifications/webhook/test", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}

	mu.Lock()
	defer mu.Unlock()
	if received.Type != "test" {
		t.Errorf("received event type = %q, want test", received.Type)
	}
}

// waitForWebhookEvent polls got (guarded by mu) until fn reports it has
// what it's looking for, or fails the test after timeout — the
// notification hooks under test (notifyAsync) run in their own
// goroutine, same as the rest of a run's async completion path.
func waitForWebhookEvent(t *testing.T, mu *sync.Mutex, timeout time.Duration, check func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		mu.Lock()
		ok := check()
		mu.Unlock()
		if ok {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("webhook notification was not received within the timeout")
}

func TestFailedRun_FiresBugReportCreatedWebhookOnlyOnce(t *testing.T) {
	var mu sync.Mutex
	var events []webhooks.Event
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var e webhooks.Event
		json.NewDecoder(r.Body).Decode(&e)
		mu.Lock()
		events = append(events, e)
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	deps := newTestDeps(nil, nil)
	fake := &fakeRunner{executeFunc: func(context.Context, runs.RunInput) (*runs.RunResult, error) {
		return &runs.RunResult{ExitCode: 1, Stderr: "boom"}, nil
	}}
	deps.Runner = fake
	router := NewRouter(deps)
	doJSON(t, router, http.MethodPatch, "/api/v1/notifications/webhook", map[string]string{"url": server.URL})

	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "app", "health", "route.ts"), `export async function GET(req) { return Response.json({}) }`)
	projectID := setUpProjectWithDiscovery(t, router, dir)
	testID := approveFirstSuggestedTest(t, router, projectID)
	setEnvironmentBaseURL(t, router, projectID, "http://localhost:3000")

	runRec := doJSON(t, router, http.MethodPost, "/api/v1/tests/"+testID+"/run", nil)
	var run testRunResponse
	json.Unmarshal(runRec.Body.Bytes(), &run)
	waitForRunStatus(t, router, run.ID, "failed", 2*time.Second)

	waitForWebhookEvent(t, &mu, 2*time.Second, func() bool {
		for _, e := range events {
			if e.Type == webhooks.EventBugReportCreated {
				return true
			}
		}
		return false
	})

	// Run the SAME test again — same failure recurs, updating the
	// existing bug (not creating a new one) — must not fire a second
	// bug_report.created notification.
	runRec2 := doJSON(t, router, http.MethodPost, "/api/v1/tests/"+testID+"/run", nil)
	var run2 testRunResponse
	json.Unmarshal(runRec2.Body.Bytes(), &run2)
	waitForRunStatus(t, router, run2.ID, "failed", 2*time.Second)

	// Give any (incorrect) second notification a moment to arrive before
	// asserting its absence.
	time.Sleep(200 * time.Millisecond)
	mu.Lock()
	defer mu.Unlock()
	createdCount := 0
	for _, e := range events {
		if e.Type == webhooks.EventBugReportCreated {
			createdCount++
		}
	}
	if createdCount != 1 {
		t.Errorf("bug_report.created notifications = %d, want exactly 1 (a recurring failure must not re-notify)", createdCount)
	}
}

func TestGenerateFixProposal_FiresPendingReviewWebhook(t *testing.T) {
	var mu sync.Mutex
	var events []webhooks.Event
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var e webhooks.Event
		json.NewDecoder(r.Body).Decode(&e)
		mu.Lock()
		events = append(events, e)
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	router, _, _, bugID := setUpBugForFixProposal(t, newTestDeps(nil, nil))
	doJSON(t, router, http.MethodPatch, "/api/v1/notifications/webhook", map[string]string{"url": server.URL})

	rec := doJSON(t, router, http.MethodPost, "/api/v1/bugs/"+bugID+"/fix-proposal", map[string]any{"unified_diff": validManualDiff})
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}

	waitForWebhookEvent(t, &mu, 2*time.Second, func() bool {
		for _, e := range events {
			if e.Type == webhooks.EventFixProposalPendingReview {
				return true
			}
		}
		return false
	})
}
