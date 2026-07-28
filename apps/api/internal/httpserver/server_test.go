package httpserver

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"e2e-sentinel/apps/api/internal/audit"
)

type fakePinger struct{ err error }

func (f fakePinger) Ping(ctx context.Context) error { return f.err }

func newTestDeps(pgErr, redisErr error) Dependencies {
	return Dependencies{
		Postgres: fakePinger{err: pgErr},
		Redis:    fakePinger{err: redisErr},
		Audit:    audit.NewMemoryRecorder(),
	}
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
