package httpserver

import (
	"encoding/json"
	"net/http"
	"path/filepath"
	"testing"
)

func setUpProjectWithDiscovery(t *testing.T, router http.Handler, dir string) string {
	t.Helper()
	createRec := doJSON(t, router, http.MethodPost, "/api/v1/projects", map[string]string{"name": "Fixture", "repository_path": dir})
	var project struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(createRec.Body.Bytes(), &project); err != nil {
		t.Fatalf("decoding project response: %v", err)
	}
	discoverRec := doJSON(t, router, http.MethodPost, "/api/v1/projects/"+project.ID+"/discover", nil)
	if discoverRec.Code != http.StatusOK {
		t.Fatalf("discover status = %d, body=%s", discoverRec.Code, discoverRec.Body.String())
	}
	return project.ID
}

func TestGenerateTestPlan_RequiresDiscoveryFirst(t *testing.T) {
	deps := newTestDeps(nil, nil)
	router := NewRouter(deps)

	createRec := doJSON(t, router, http.MethodPost, "/api/v1/projects", map[string]string{"name": "Fixture", "repository_path": t.TempDir()})
	var project struct {
		ID string `json:"id"`
	}
	json.Unmarshal(createRec.Body.Bytes(), &project)

	rec := doJSON(t, router, http.MethodPost, "/api/v1/projects/"+project.ID+"/tests/plan", nil)
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409 (no discovery run yet), body=%s", rec.Code, rec.Body.String())
	}
}

func TestGenerateTestPlan_ProducesReviewableSuggestedTests(t *testing.T) {
	deps := newTestDeps(nil, nil)
	router := NewRouter(deps)

	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "app", "login", "page.tsx"), `export default function Page() { return null }`)
	mustWrite(t, filepath.Join(dir, "app", "api", "v1", "auth", "login", "route.ts"), `export async function POST(req) { return Response.json({}) }`)

	projectID := setUpProjectWithDiscovery(t, router, dir)

	planRec := doJSON(t, router, http.MethodPost, "/api/v1/projects/"+projectID+"/tests/plan", nil)
	if planRec.Code != http.StatusOK {
		t.Fatalf("plan status = %d, body=%s", planRec.Code, planRec.Body.String())
	}

	var body struct {
		Tests []struct {
			ID               string `json:"id"`
			Category         string `json:"category"`
			IsMutating       bool   `json:"is_mutating"`
			IsProductionSafe bool   `json:"is_production_safe"`
			ApprovalStatus   string `json:"approval_status"`
		} `json:"tests"`
	}
	json.Unmarshal(planRec.Body.Bytes(), &body)
	if len(body.Tests) == 0 {
		t.Fatal("expected at least one suggested test case")
	}

	foundMutating := false
	for _, tc := range body.Tests {
		if tc.ApprovalStatus != "pending" {
			t.Errorf("newly suggested test must be pending approval, got %+v", tc)
		}
		if tc.IsMutating {
			foundMutating = true
			if tc.IsProductionSafe {
				t.Errorf("a mutating test must not be marked production-safe: %+v", tc)
			}
		}
	}
	if !foundMutating {
		t.Error("expected at least one mutating test (the login POST route)")
	}

	// GET /tests must reflect the same suggestions.
	listRec := doJSON(t, router, http.MethodGet, "/api/v1/projects/"+projectID+"/tests", nil)
	var listBody struct {
		Tests []any `json:"tests"`
	}
	json.Unmarshal(listRec.Body.Bytes(), &listBody)
	if len(listBody.Tests) != len(body.Tests) {
		t.Errorf("GET /tests returned %d, want %d", len(listBody.Tests), len(body.Tests))
	}
}

func TestApproveTest_BlocksMutatingTestInProductionEnvironment(t *testing.T) {
	deps := newTestDeps(nil, nil)
	router := NewRouter(deps)

	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "app", "api", "v1", "orders", "route.ts"), `export async function POST(req) { return Response.json({}) }`)
	projectID := setUpProjectWithDiscovery(t, router, dir)

	// Classify the project's default environment as production.
	envListRec := doJSON(t, router, http.MethodGet, "/api/v1/projects/"+projectID+"/environments", nil)
	var envList struct {
		Environments []struct {
			ID string `json:"id"`
		} `json:"environments"`
	}
	json.Unmarshal(envListRec.Body.Bytes(), &envList)
	doJSON(t, router, http.MethodPatch, "/api/v1/environments/"+envList.Environments[0].ID, map[string]string{"classification": "production"})

	planRec := doJSON(t, router, http.MethodPost, "/api/v1/projects/"+projectID+"/tests/plan", nil)
	var planBody struct {
		Tests []struct {
			ID         string `json:"id"`
			IsMutating bool   `json:"is_mutating"`
		} `json:"tests"`
	}
	json.Unmarshal(planRec.Body.Bytes(), &planBody)

	var mutatingID string
	for _, tc := range planBody.Tests {
		if tc.IsMutating {
			mutatingID = tc.ID
			break
		}
	}
	if mutatingID == "" {
		t.Fatal("expected at least one mutating suggested test")
	}

	approveRec := doJSON(t, router, http.MethodPost, "/api/v1/tests/"+mutatingID+"/approve", nil)
	if approveRec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 (production-unsafe test must not be approvable), body=%s", approveRec.Code, approveRec.Body.String())
	}
}

func TestApproveTest_SucceedsInLocalEnvironment(t *testing.T) {
	deps := newTestDeps(nil, nil)
	router := NewRouter(deps)

	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "app", "health", "route.ts"), `export async function GET(req) { return Response.json({}) }`)
	projectID := setUpProjectWithDiscovery(t, router, dir)

	planRec := doJSON(t, router, http.MethodPost, "/api/v1/projects/"+projectID+"/tests/plan", nil)
	var planBody struct {
		Tests []struct {
			ID string `json:"id"`
		} `json:"tests"`
	}
	json.Unmarshal(planRec.Body.Bytes(), &planBody)
	if len(planBody.Tests) == 0 {
		t.Fatal("expected at least one suggested test")
	}

	approveRec := doJSON(t, router, http.MethodPost, "/api/v1/tests/"+planBody.Tests[0].ID+"/approve", nil)
	if approveRec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", approveRec.Code, approveRec.Body.String())
	}
	var approved struct {
		ApprovalStatus string `json:"approval_status"`
		Status         string `json:"status"`
	}
	json.Unmarshal(approveRec.Body.Bytes(), &approved)
	if approved.ApprovalStatus != "approved" || approved.Status != "approved" {
		t.Errorf("approved test = %+v, want approval_status=approved status=approved", approved)
	}
}

func TestRejectTest_MarksRejected(t *testing.T) {
	deps := newTestDeps(nil, nil)
	router := NewRouter(deps)

	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "app", "health", "route.ts"), `export async function GET(req) { return Response.json({}) }`)
	projectID := setUpProjectWithDiscovery(t, router, dir)

	planRec := doJSON(t, router, http.MethodPost, "/api/v1/projects/"+projectID+"/tests/plan", nil)
	var planBody struct {
		Tests []struct {
			ID string `json:"id"`
		} `json:"tests"`
	}
	json.Unmarshal(planRec.Body.Bytes(), &planBody)

	rejectRec := doJSON(t, router, http.MethodPost, "/api/v1/tests/"+planBody.Tests[0].ID+"/reject", nil)
	if rejectRec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rejectRec.Code)
	}
	var rejected struct {
		ApprovalStatus string `json:"approval_status"`
	}
	json.Unmarshal(rejectRec.Body.Bytes(), &rejected)
	if rejected.ApprovalStatus != "rejected" {
		t.Errorf("ApprovalStatus = %q, want rejected", rejected.ApprovalStatus)
	}
}

func TestUpdateTest_EditsTitleAndPriority(t *testing.T) {
	deps := newTestDeps(nil, nil)
	router := NewRouter(deps)

	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "app", "health", "route.ts"), `export async function GET(req) { return Response.json({}) }`)
	projectID := setUpProjectWithDiscovery(t, router, dir)

	planRec := doJSON(t, router, http.MethodPost, "/api/v1/projects/"+projectID+"/tests/plan", nil)
	var planBody struct {
		Tests []struct {
			ID string `json:"id"`
		} `json:"tests"`
	}
	json.Unmarshal(planRec.Body.Bytes(), &planBody)

	updateRec := doJSON(t, router, http.MethodPatch, "/api/v1/tests/"+planBody.Tests[0].ID, map[string]string{"title": "Custom title", "priority": "P0"})
	if updateRec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", updateRec.Code, updateRec.Body.String())
	}
	var updated struct {
		Title    string `json:"title"`
		Priority string `json:"priority"`
	}
	json.Unmarshal(updateRec.Body.Bytes(), &updated)
	if updated.Title != "Custom title" || updated.Priority != "P0" {
		t.Errorf("updated = %+v, want title=Custom title priority=P0", updated)
	}
}

func TestGenerateTestPlan_RegenerationDoesNotResetApprovedTest(t *testing.T) {
	deps := newTestDeps(nil, nil)
	router := NewRouter(deps)

	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "app", "health", "route.ts"), `export async function GET(req) { return Response.json({}) }`)
	projectID := setUpProjectWithDiscovery(t, router, dir)

	planRec := doJSON(t, router, http.MethodPost, "/api/v1/projects/"+projectID+"/tests/plan", nil)
	var planBody struct {
		Tests []struct {
			ID string `json:"id"`
		} `json:"tests"`
	}
	json.Unmarshal(planRec.Body.Bytes(), &planBody)
	doJSON(t, router, http.MethodPost, "/api/v1/tests/"+planBody.Tests[0].ID+"/approve", nil)

	// Regenerate the plan (e.g. after re-running discovery).
	planRec2 := doJSON(t, router, http.MethodPost, "/api/v1/projects/"+projectID+"/tests/plan", nil)
	var planBody2 struct {
		Tests []struct {
			ID             string `json:"id"`
			ApprovalStatus string `json:"approval_status"`
		} `json:"tests"`
	}
	json.Unmarshal(planRec2.Body.Bytes(), &planBody2)

	for _, tc := range planBody2.Tests {
		if tc.ID == planBody.Tests[0].ID && tc.ApprovalStatus != "approved" {
			t.Errorf("regenerating the plan must not reset an approved test's approval status, got %+v", tc)
		}
	}
}
