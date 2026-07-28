// Package integration contains black-box tests against a fully running
// E2E Sentinel stack (Postgres, Redis, sentinel-api all up, migrations
// applied). It exercises the HTTP surface only — no internal packages —
// so it stays valid across future phases.
//
// It requires SENTINEL_INTEGRATION_BASE_URL (e.g. http://localhost:8080,
// which is what `docker compose up -d` exposes). Per spec §26.2/§34, these
// tests must skip rather than fail when no environment is available, so
// `go test ./...` from a laptop with nothing running stays informative
// instead of red.
//
// Run with:
//
//	docker compose up -d
//	SENTINEL_INTEGRATION_BASE_URL=http://localhost:8080 go test ./...
package integration

import (
	"encoding/json"
	"net/http"
	"os"
	"testing"
	"time"
)

func baseURL(t *testing.T) string {
	t.Helper()
	url := os.Getenv("SENTINEL_INTEGRATION_BASE_URL")
	if url == "" {
		t.Skip("SENTINEL_INTEGRATION_BASE_URL not set; skipping integration test (run `docker compose up -d` first)")
	}
	return url
}

func getJSON(t *testing.T, url string, out any) *http.Response {
	t.Helper()
	client := &http.Client{Timeout: 10 * time.Second}
	res, err := client.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer res.Body.Close()

	if out != nil {
		if err := json.NewDecoder(res.Body).Decode(out); err != nil {
			t.Fatalf("decoding response from %s: %v", url, err)
		}
	}
	return res
}

func TestHealth_ReturnsOK(t *testing.T) {
	base := baseURL(t)

	var body struct {
		Status string `json:"status"`
	}
	res := getJSON(t, base+"/health", &body)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", res.StatusCode)
	}
	if body.Status != "ok" {
		t.Errorf("status field = %q, want ok", body.Status)
	}
}

func TestReady_DependenciesAreHealthy(t *testing.T) {
	base := baseURL(t)

	var body struct {
		Ready  bool              `json:"ready"`
		Checks map[string]string `json:"checks"`
	}
	res := getJSON(t, base+"/ready", &body)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200 (postgres/redis should be reachable): checks=%+v", res.StatusCode, body.Checks)
	}
	if !body.Ready {
		t.Errorf("ready = false, checks=%+v", body.Checks)
	}
	for _, dep := range []string{"postgres", "redis"} {
		if body.Checks[dep] != "ok" {
			t.Errorf("checks[%s] = %q, want ok", dep, body.Checks[dep])
		}
	}
}

func TestAuditEvents_ContainsSystemStartup(t *testing.T) {
	base := baseURL(t)

	var body struct {
		Events []struct {
			ActionType string `json:"ActionType"`
		} `json:"events"`
	}
	res := getJSON(t, base+"/api/v1/audit-events?limit=100", &body)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", res.StatusCode)
	}

	found := false
	for _, e := range body.Events {
		if e.ActionType == "system.startup" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected a system.startup audit event among %d events", len(body.Events))
	}
}
