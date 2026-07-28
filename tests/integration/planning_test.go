package integration

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

// TestTestPlanning_FullFlow validates spec §25 Phase 4 acceptance
// end-to-end against the live stack: suggested tests are reviewable, a
// mutating test defaults to production-unsafe, and approving it is
// blocked once the project's environment is classified production.
func TestTestPlanning_FullFlow(t *testing.T) {
	base := baseURL(t)

	hostDir, err := filepath.Abs(filepath.Join("..", "..", "workspace", "it-planning-fixture"))
	if err != nil {
		t.Fatalf("resolving workspace fixture path: %v", err)
	}
	if err := os.RemoveAll(hostDir); err != nil {
		t.Fatalf("cleaning up previous fixture: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(hostDir) })

	writeFixtureFile(t, hostDir, "app/api/v1/orders/route.ts", `
export async function POST(req) { return Response.json({}) }
`)

	var project struct {
		ID string `json:"id"`
	}
	createRes := postJSON(t, base+"/api/v1/projects", map[string]string{
		"name": "IT Planning Fixture", "repository_path": "/workspace/it-planning-fixture",
	}, &project)
	if createRes.StatusCode != http.StatusCreated {
		t.Fatalf("create project status = %d, want 201", createRes.StatusCode)
	}

	if res := postJSON(t, base+"/api/v1/projects/"+project.ID+"/discover", struct{}{}, nil); res.StatusCode != http.StatusOK {
		t.Fatalf("discover status = %d, want 200", res.StatusCode)
	}

	var plan struct {
		Tests []struct {
			ID               string `json:"id"`
			IsMutating       bool   `json:"is_mutating"`
			IsProductionSafe bool   `json:"is_production_safe"`
			ApprovalStatus   string `json:"approval_status"`
		} `json:"tests"`
	}
	planRes := postJSON(t, base+"/api/v1/projects/"+project.ID+"/tests/plan", struct{}{}, &plan)
	if planRes.StatusCode != http.StatusOK {
		t.Fatalf("plan status = %d, want 200", planRes.StatusCode)
	}
	if len(plan.Tests) == 0 {
		t.Fatal("expected at least one suggested test case")
	}

	var mutatingID string
	for _, tc := range plan.Tests {
		if tc.ApprovalStatus != "pending" {
			t.Errorf("newly suggested test must be pending, got %+v", tc)
		}
		if tc.IsMutating {
			mutatingID = tc.ID
			if tc.IsProductionSafe {
				t.Errorf("a mutating test must not be production-safe: %+v", tc)
			}
		}
	}
	if mutatingID == "" {
		t.Fatal("expected at least one mutating test case (the orders POST route)")
	}

	// Classify the project's environment as production.
	var envList struct {
		Environments []struct {
			ID string `json:"id"`
		} `json:"environments"`
	}
	client := &http.Client{}
	envRes, err := client.Get(base + "/api/v1/projects/" + project.ID + "/environments")
	if err != nil {
		t.Fatalf("GET environments: %v", err)
	}
	defer envRes.Body.Close()
	if err := json.NewDecoder(envRes.Body).Decode(&envList); err != nil {
		t.Fatalf("decoding environments: %v", err)
	}
	if len(envList.Environments) == 0 {
		t.Fatal("expected a default environment")
	}
	if res := patchJSON(t, base+"/api/v1/environments/"+envList.Environments[0].ID, map[string]string{"classification": "production"}); res.StatusCode != http.StatusOK {
		t.Fatalf("classifying environment as production status = %d, want 200", res.StatusCode)
	}

	approveRes := postJSON(t, base+"/api/v1/tests/"+mutatingID+"/approve", struct{}{}, nil)
	if approveRes.StatusCode != http.StatusForbidden {
		t.Fatalf("approve status = %d, want 403 (production-unsafe test must not be approvable)", approveRes.StatusCode)
	}
}
