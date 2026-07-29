package httpserver

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"e2e-sentinel/apps/api/internal/artifacts"
	"e2e-sentinel/apps/api/internal/audit"
	"e2e-sentinel/apps/api/internal/bugreports"
	"e2e-sentinel/apps/api/internal/discovery"
	"e2e-sentinel/apps/api/internal/environments"
	"e2e-sentinel/apps/api/internal/failures"
	"e2e-sentinel/apps/api/internal/fixproposals"
	"e2e-sentinel/apps/api/internal/graph"
	"e2e-sentinel/apps/api/internal/planning"
	"e2e-sentinel/apps/api/internal/projects"
	"e2e-sentinel/apps/api/internal/providers"
	"e2e-sentinel/apps/api/internal/runs"
	"e2e-sentinel/apps/api/internal/services"
	"e2e-sentinel/apps/api/internal/settings"
)

type fakePinger struct{ err error }

func (f fakePinger) Ping(ctx context.Context) error { return f.err }

func newTestDeps(pgErr, redisErr error) Dependencies {
	return Dependencies{
		Postgres:         fakePinger{err: pgErr},
		Redis:            fakePinger{err: redisErr},
		Audit:            audit.NewMemoryRecorder(),
		Projects:         projects.NewMemoryStore(),
		Environments:     environments.NewMemoryStore(),
		Discovery:        discovery.NewMemoryStore(),
		Services:         services.NewMemoryStore(),
		Graph:            graph.NewMemoryStore(),
		Planning:         planning.NewMemoryStore(),
		Runs:             runs.NewMemoryStore(),
		Artifacts:        artifacts.NewMemoryStore(),
		Providers:        providers.NewMemoryStore(),
		Settings:         settings.NewMemoryStore(),
		Failures:         failures.NewMemoryStore(),
		Bugs:             bugreports.NewMemoryStore(),
		FixProposals:     fixproposals.NewMemoryStore(),
		ProviderHealth:   providers.NewHealthChecker(nil),
		Completer:        providers.NewCompleter(nil),
		FixWorkspacesDir: testFixWorkspacesDir(),
		Docker:           nil, // no Docker daemon in unit tests; must degrade gracefully
		Runner:           nil, // no runner configured by default; see fakeRunner in runs_handlers_test.go
		Secrets:          nil, // no encryption key configured by default; see providers_handlers_test.go
	}
}

// testFixWorkspacesDir returns a process-wide scratch directory for fix
// proposal workspace tests. Using a fixed path (rather than t.TempDir(),
// which newTestDeps' signature has no *testing.T to call) is fine here:
// ApplyToWorkspace always creates its own uniquely-named subdirectory
// under it via os.MkdirTemp.
func testFixWorkspacesDir() string {
	dir := filepath.Join(os.TempDir(), "e2e-sentinel-test-fix-workspaces")
	_ = os.MkdirAll(dir, 0o755)
	return dir
}

func TestHandleHealth_AlwaysOK(t *testing.T) {
	router := NewRouter(newTestDeps(errors.New("db down"), errors.New("redis down")))

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (health must not depend on downstream services)", rec.Code)
	}
}

func TestHandleReady_AllHealthy(t *testing.T) {
	router := NewRouter(newTestDeps(nil, nil))

	req := httptest.NewRequest(http.MethodGet, "/ready", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid JSON body: %v", err)
	}
	if body["ready"] != true {
		t.Errorf("ready = %v, want true", body["ready"])
	}
}

func TestHandleReady_DependencyDown(t *testing.T) {
	router := NewRouter(newTestDeps(errors.New("db down"), nil))

	req := httptest.NewRequest(http.MethodGet, "/ready", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}

	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid JSON body: %v", err)
	}
	if body["ready"] != false {
		t.Errorf("ready = %v, want false", body["ready"])
	}
	checks, _ := body["checks"].(map[string]any)
	if checks["postgres"] != "unreachable" {
		t.Errorf("checks[postgres] = %v, want unreachable", checks["postgres"])
	}
	if checks["redis"] != "ok" {
		t.Errorf("checks[redis] = %v, want ok", checks["redis"])
	}
}

func TestHandleListAuditEvents_ReturnsRecordedEvents(t *testing.T) {
	deps := newTestDeps(nil, nil)
	if err := deps.Audit.Record(context.Background(), audit.Event{
		ActionType:   "project.added",
		ResourceType: "project",
		Actor:        "admin",
	}); err != nil {
		t.Fatalf("seeding audit event: %v", err)
	}

	router := NewRouter(deps)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/audit-events", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	var body struct {
		Events []audit.Event `json:"events"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid JSON body: %v", err)
	}
	if len(body.Events) != 1 {
		t.Fatalf("got %d events, want 1", len(body.Events))
	}
	if body.Events[0].ActionType != "project.added" {
		t.Errorf("ActionType = %q, want project.added", body.Events[0].ActionType)
	}
}
