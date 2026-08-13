package httpserver

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"e2e-sentinel/apps/api/internal/graph"
)

const validManualDiff = "--- a/main.go\n+++ b/main.go\n@@ -1,3 +1,3 @@\n package main\n \n-func old() {}\n+func new() {}\n"

func setUpBugForFixProposal(t *testing.T, deps Dependencies) (router http.Handler, projectDir, projectID, bugID string) {
	t.Helper()
	router, projectID, _ = setUpFailingRun(t, deps, "Error: net::ERR_CONNECTION_REFUSED")

	listRec := doJSON(t, router, http.MethodGet, "/api/v1/bugs?project_id="+projectID, nil)
	var body struct {
		Bugs []bugReportResponse `json:"bugs"`
	}
	json.Unmarshal(listRec.Body.Bytes(), &body)
	if len(body.Bugs) == 0 {
		t.Fatal("expected at least one bug")
	}

	projRec := doJSON(t, router, http.MethodGet, "/api/v1/projects/"+projectID, nil)
	var proj struct {
		RepositoryPath string `json:"repository_path"`
	}
	json.Unmarshal(projRec.Body.Bytes(), &proj)

	return router, proj.RepositoryPath, projectID, body.Bugs[0].ID
}

func TestGenerateFixProposal_Manual(t *testing.T) {
	router, _, _, bugID := setUpBugForFixProposal(t, newTestDeps(nil, nil))

	rec := doJSON(t, router, http.MethodPost, "/api/v1/bugs/"+bugID+"/fix-proposal", map[string]any{"unified_diff": validManualDiff})
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var fp fixProposalResponse
	json.Unmarshal(rec.Body.Bytes(), &fp)
	if fp.AIProvider != "" {
		t.Errorf("AIProvider = %q, want empty for a manual proposal", fp.AIProvider)
	}
	if fp.ApprovalStatus != "pending_review" {
		t.Errorf("ApprovalStatus = %q, want pending_review", fp.ApprovalStatus)
	}
	if len(fp.FilesChanged) != 1 || fp.FilesChanged[0] != "main.go" {
		t.Errorf("FilesChanged = %v", fp.FilesChanged)
	}
}

func TestGenerateFixProposal_ManualInvalidDiffReturns422(t *testing.T) {
	router, _, _, bugID := setUpBugForFixProposal(t, newTestDeps(nil, nil))

	rec := doJSON(t, router, http.MethodPost, "/api/v1/bugs/"+bugID+"/fix-proposal", map[string]any{"unified_diff": "not a diff"})
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422, body=%s", rec.Code, rec.Body.String())
	}
}

func TestGenerateFixProposal_NoAIConfiguredReturns503(t *testing.T) {
	router, _, _, bugID := setUpBugForFixProposal(t, newTestDeps(nil, nil))

	rec := doJSON(t, router, http.MethodPost, "/api/v1/bugs/"+bugID+"/fix-proposal", nil)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503, body=%s", rec.Code, rec.Body.String())
	}
}

func TestGenerateFixProposal_BugNotFound(t *testing.T) {
	router := NewRouter(newTestDeps(nil, nil))
	rec := doJSON(t, router, http.MethodPost, "/api/v1/bugs/does-not-exist/fix-proposal", map[string]any{"unified_diff": validManualDiff})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestGenerateFixProposal_ViaAI_NeverAutoApproved(t *testing.T) {
	fakeAI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		content := "Here is a fix:\n\n```diff\n" + validManualDiff + "```\n"
		payload, _ := json.Marshal(map[string]any{
			"choices": []map[string]any{{"message": map[string]string{"content": content}}},
		})
		w.Write(payload)
	}))
	defer fakeAI.Close()

	deps := newTestDeps(nil, nil)
	router, _, _, bugID := setUpBugForFixProposal(t, deps)

	createProvRec := doJSON(t, router, http.MethodPost, "/api/v1/providers", map[string]any{
		"type": "openai", "name": "Test Provider", "base_url": fakeAI.URL,
	})
	var provider providerResponse
	json.Unmarshal(createProvRec.Body.Bytes(), &provider)

	routeRec := doJSON(t, router, http.MethodPatch, "/api/v1/providers/routing", map[string]any{
		"routes": map[string]string{"fix_generation": provider.ID},
	})
	if routeRec.Code != http.StatusOK {
		t.Fatalf("routing status = %d, body=%s", routeRec.Code, routeRec.Body.String())
	}

	rec := doJSON(t, router, http.MethodPost, "/api/v1/bugs/"+bugID+"/fix-proposal", nil)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var fp fixProposalResponse
	json.Unmarshal(rec.Body.Bytes(), &fp)
	if fp.AIProvider != "openai" {
		t.Errorf("AIProvider = %q, want openai", fp.AIProvider)
	}
	if fp.ApprovalStatus != "pending_review" {
		t.Errorf("ApprovalStatus = %q, want pending_review — AI must never auto-approve its own proposal", fp.ApprovalStatus)
	}
	if fp.UnifiedDiff == "" {
		t.Error("UnifiedDiff should not be empty")
	}
}

func TestGenerateFixProposal_ViaAI_InvalidDiffFromModelReturns422(t *testing.T) {
	fakeAI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"choices":[{"message":{"content":"I cannot help with that, no diff here."}}]}`))
	}))
	defer fakeAI.Close()

	deps := newTestDeps(nil, nil)
	router, _, _, bugID := setUpBugForFixProposal(t, deps)

	createProvRec := doJSON(t, router, http.MethodPost, "/api/v1/providers", map[string]any{"type": "openai", "name": "Test", "base_url": fakeAI.URL})
	var provider providerResponse
	json.Unmarshal(createProvRec.Body.Bytes(), &provider)
	doJSON(t, router, http.MethodPatch, "/api/v1/providers/routing", map[string]any{"routes": map[string]string{"fix_generation": provider.ID}})

	rec := doJSON(t, router, http.MethodPost, "/api/v1/bugs/"+bugID+"/fix-proposal", nil)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422, body=%s", rec.Code, rec.Body.String())
	}
}

// fakeAICapturingRequest is fakeAI's shape (see
// TestGenerateFixProposal_ViaAI_NeverAutoApproved) plus capturing the
// raw request body it received, so a test can assert on exactly what
// was sent to the "AI provider" — the only way to prove source context
// actually reached the prompt (and that a secret in it didn't).
func fakeAICapturingRequest(t *testing.T, capturedBody *string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		buf, _ := io.ReadAll(r.Body)
		*capturedBody = string(buf)
		content := "Here is a fix:\n\n```diff\n" + validManualDiff + "```\n"
		payload, _ := json.Marshal(map[string]any{
			"choices": []map[string]any{{"message": map[string]string{"content": content}}},
		})
		w.Write(payload)
	}))
}

func routeAIFixGenerationToFake(t *testing.T, router http.Handler, fakeURL string) {
	t.Helper()
	createProvRec := doJSON(t, router, http.MethodPost, "/api/v1/providers", map[string]any{
		"type": "openai", "name": "Test Provider", "base_url": fakeURL,
	})
	var provider providerResponse
	json.Unmarshal(createProvRec.Body.Bytes(), &provider)
	routeRec := doJSON(t, router, http.MethodPatch, "/api/v1/providers/routing", map[string]any{
		"routes": map[string]string{"fix_generation": provider.ID},
	})
	if routeRec.Code != http.StatusOK {
		t.Fatalf("routing status = %d, body=%s", routeRec.Code, routeRec.Body.String())
	}
}

func getBug(t *testing.T, router http.Handler, bugID string) bugReportResponse {
	t.Helper()
	rec := doJSON(t, router, http.MethodGet, "/api/v1/bugs/"+bugID, nil)
	var bug bugReportResponse
	json.Unmarshal(rec.Body.Bytes(), &bug)
	return bug
}

func TestGenerateFixProposal_ViaAI_IncludesGraphMatchedSourceRedactedForSecrets(t *testing.T) {
	deps := newTestDeps(nil, nil)
	router, projectDir, projectID, bugID := setUpBugForFixProposal(t, deps)
	bug := getBug(t, router, bugID)
	if bug.AffectedRoute == "" {
		t.Fatal("expected the seeded bug to have a non-empty AffectedRoute")
	}

	const marker = "const sourceContextMarker = 'reached-the-prompt'"
	const secret = "sk-secret-abc123"
	mustWrite(t, filepath.Join(projectDir, "app", "health", "route.ts"),
		marker+"\nconst API_KEY = \""+secret+"\";\n")

	if err := deps.Graph.ReplaceGraph(context.Background(), projectID, []graph.Node{
		{NodeType: "api", Label: bug.AffectedRoute, SourceReference: "app/health/route.ts", Confidence: graph.ConfidenceHigh},
	}, nil); err != nil {
		t.Fatalf("ReplaceGraph: %v", err)
	}

	var captured string
	fakeAI := fakeAICapturingRequest(t, &captured)
	defer fakeAI.Close()
	routeAIFixGenerationToFake(t, router, fakeAI.URL)

	rec := doJSON(t, router, http.MethodPost, "/api/v1/bugs/"+bugID+"/fix-proposal", nil)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}

	if !strings.Contains(captured, marker) {
		t.Errorf("request sent to the AI provider did not contain the source file's content: %s", captured)
	}
	if strings.Contains(captured, secret) {
		t.Errorf("request sent to the AI provider leaked the secret %q — redaction did not run: %s", secret, captured)
	}

	var fp fixProposalResponse
	json.Unmarshal(rec.Body.Bytes(), &fp)
	if !strings.Contains(fp.Assumptions, "real source content") {
		t.Errorf("Assumptions = %q, want it to reflect that real source content was included", fp.Assumptions)
	}
}

func TestGenerateFixProposal_ViaAI_NoGraphMatchFallsBackToEvidenceOnlyPrompt(t *testing.T) {
	deps := newTestDeps(nil, nil)
	router, _, projectID, bugID := setUpBugForFixProposal(t, deps)
	// setUpBugForFixProposal already runs discovery, which builds a real
	// Application Graph with a node for the route the bug is about — so
	// to actually exercise "no match", the graph has to be cleared
	// explicitly rather than just skipped.
	if err := deps.Graph.ReplaceGraph(context.Background(), projectID, nil, nil); err != nil {
		t.Fatalf("ReplaceGraph: %v", err)
	}

	var captured string
	fakeAI := fakeAICapturingRequest(t, &captured)
	defer fakeAI.Close()
	routeAIFixGenerationToFake(t, router, fakeAI.URL)

	rec := doJSON(t, router, http.MethodPost, "/api/v1/bugs/"+bugID+"/fix-proposal", nil)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if strings.Contains(captured, "Relevant source file(s)") {
		t.Errorf("request sent to the AI provider unexpectedly included a source-context section with no graph match: %s", captured)
	}

	var fp fixProposalResponse
	json.Unmarshal(rec.Body.Bytes(), &fp)
	if !strings.Contains(fp.Assumptions, "not the actual repository source") {
		t.Errorf("Assumptions = %q, want the evidence-only wording when no source was included", fp.Assumptions)
	}
}

func TestGenerateFixProposal_ViaAI_OversizedSourceFileExcluded(t *testing.T) {
	deps := newTestDeps(nil, nil)
	router, projectDir, projectID, bugID := setUpBugForFixProposal(t, deps)
	bug := getBug(t, router, bugID)

	huge := strings.Repeat("x", 250_000) // over redaction.DefaultMaxFileBytes (200_000)
	mustWrite(t, filepath.Join(projectDir, "app", "health", "route.ts"), huge)

	if err := deps.Graph.ReplaceGraph(context.Background(), projectID, []graph.Node{
		{NodeType: "api", Label: bug.AffectedRoute, SourceReference: "app/health/route.ts", Confidence: graph.ConfidenceHigh},
	}, nil); err != nil {
		t.Fatalf("ReplaceGraph: %v", err)
	}

	var captured string
	fakeAI := fakeAICapturingRequest(t, &captured)
	defer fakeAI.Close()
	routeAIFixGenerationToFake(t, router, fakeAI.URL)

	rec := doJSON(t, router, http.MethodPost, "/api/v1/bugs/"+bugID+"/fix-proposal", nil)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if strings.Contains(captured, huge) {
		t.Error("an oversized source file was included in the AI prompt despite exceeding the size limit")
	}
}

func TestFixProposalApprovalWorkflow(t *testing.T) {
	router, _, _, bugID := setUpBugForFixProposal(t, newTestDeps(nil, nil))
	createRec := doJSON(t, router, http.MethodPost, "/api/v1/bugs/"+bugID+"/fix-proposal", map[string]any{"unified_diff": validManualDiff})
	var fp fixProposalResponse
	json.Unmarshal(createRec.Body.Bytes(), &fp)

	revisionRec := doJSON(t, router, http.MethodPost, "/api/v1/fix-proposals/"+fp.ID+"/request-revision", nil)
	var revised fixProposalResponse
	json.Unmarshal(revisionRec.Body.Bytes(), &revised)
	if revised.ApprovalStatus != "revision_requested" {
		t.Errorf("ApprovalStatus = %q, want revision_requested", revised.ApprovalStatus)
	}

	rejectRec := doJSON(t, router, http.MethodPost, "/api/v1/fix-proposals/"+fp.ID+"/reject", nil)
	var rejected fixProposalResponse
	json.Unmarshal(rejectRec.Body.Bytes(), &rejected)
	if rejected.ApprovalStatus != "rejected" {
		t.Errorf("ApprovalStatus = %q, want rejected", rejected.ApprovalStatus)
	}

	approveRec := doJSON(t, router, http.MethodPost, "/api/v1/fix-proposals/"+fp.ID+"/approve", nil)
	var approved fixProposalResponse
	json.Unmarshal(approveRec.Body.Bytes(), &approved)
	if approved.ApprovalStatus != "approved" {
		t.Errorf("ApprovalStatus = %q, want approved", approved.ApprovalStatus)
	}
}

func TestUpdateFixProposalRegressionTestIDs(t *testing.T) {
	router, _, _, bugID := setUpBugForFixProposal(t, newTestDeps(nil, nil))
	createRec := doJSON(t, router, http.MethodPost, "/api/v1/bugs/"+bugID+"/fix-proposal", map[string]any{"unified_diff": validManualDiff})
	var fp fixProposalResponse
	json.Unmarshal(createRec.Body.Bytes(), &fp)

	rec := doJSON(t, router, http.MethodPatch, "/api/v1/fix-proposals/"+fp.ID, map[string]any{"regression_test_ids": []string{"tc-1", "tc-2"}})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var updated fixProposalResponse
	json.Unmarshal(rec.Body.Bytes(), &updated)
	if len(updated.RegressionTestIDs) != 2 {
		t.Errorf("RegressionTestIDs = %v", updated.RegressionTestIDs)
	}
}

func TestApplyFixToWorkspace_AppliesCleanlyWithoutTouchingOriginalRepo(t *testing.T) {
	router, projectDir, _, bugID := setUpBugForFixProposal(t, newTestDeps(nil, nil))
	if err := os.WriteFile(filepath.Join(projectDir, "main.go"), []byte("package main\n\nfunc old() {}\n"), 0o644); err != nil {
		t.Fatalf("writing fixture file: %v", err)
	}

	createRec := doJSON(t, router, http.MethodPost, "/api/v1/bugs/"+bugID+"/fix-proposal", map[string]any{"unified_diff": validManualDiff})
	var fp fixProposalResponse
	json.Unmarshal(createRec.Body.Bytes(), &fp)

	rec := doJSON(t, router, http.MethodPost, "/api/v1/fix-proposals/"+fp.ID+"/apply-workspace", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var result struct {
		FixProposal fixProposalResponse `json:"fix_proposal"`
		AllApplied  bool                `json:"all_applied"`
	}
	json.Unmarshal(rec.Body.Bytes(), &result)
	if !result.AllApplied {
		t.Fatalf("AllApplied = false, results = %+v", result.FixProposal.WorkspaceApplyResults)
	}
	if result.FixProposal.WorkspaceDir == "" {
		t.Fatal("WorkspaceDir was not recorded")
	}

	patched, err := os.ReadFile(filepath.Join(result.FixProposal.WorkspaceDir, "main.go"))
	if err != nil {
		t.Fatalf("reading patched workspace file: %v", err)
	}
	if string(patched) != "package main\n\nfunc new() {}\n" {
		t.Errorf("patched content = %q", patched)
	}

	original, err := os.ReadFile(filepath.Join(projectDir, "main.go"))
	if err != nil {
		t.Fatalf("reading original file: %v", err)
	}
	if string(original) != "package main\n\nfunc old() {}\n" {
		t.Error("the original repository file must never be modified by apply-workspace")
	}
}

func TestApplyFixToRepository_RequiresApproval(t *testing.T) {
	router, projectDir, _, bugID := setUpBugForFixProposal(t, newTestDeps(nil, nil))
	os.WriteFile(filepath.Join(projectDir, "main.go"), []byte("package main\n\nfunc old() {}\n"), 0o644)

	createRec := doJSON(t, router, http.MethodPost, "/api/v1/bugs/"+bugID+"/fix-proposal", map[string]any{"unified_diff": validManualDiff})
	var fp fixProposalResponse
	json.Unmarshal(createRec.Body.Bytes(), &fp)

	rec := doJSON(t, router, http.MethodPost, "/api/v1/fix-proposals/"+fp.ID+"/apply-repository", nil)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 (must not apply an unapproved proposal), body=%s", rec.Code, rec.Body.String())
	}

	original, _ := os.ReadFile(filepath.Join(projectDir, "main.go"))
	if string(original) != "package main\n\nfunc old() {}\n" {
		t.Error("the repository must not be modified before approval")
	}
}

func TestApplyFixToRepository_SucceedsOnceAfterApproval(t *testing.T) {
	router, projectDir, _, bugID := setUpBugForFixProposal(t, newTestDeps(nil, nil))
	os.WriteFile(filepath.Join(projectDir, "main.go"), []byte("package main\n\nfunc old() {}\n"), 0o644)

	createRec := doJSON(t, router, http.MethodPost, "/api/v1/bugs/"+bugID+"/fix-proposal", map[string]any{"unified_diff": validManualDiff})
	var fp fixProposalResponse
	json.Unmarshal(createRec.Body.Bytes(), &fp)

	doJSON(t, router, http.MethodPost, "/api/v1/fix-proposals/"+fp.ID+"/approve", nil)

	rec := doJSON(t, router, http.MethodPost, "/api/v1/fix-proposals/"+fp.ID+"/apply-repository", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var result struct {
		AllApplied bool `json:"all_applied"`
	}
	json.Unmarshal(rec.Body.Bytes(), &result)
	if !result.AllApplied {
		t.Fatal("AllApplied = false")
	}

	applied, err := os.ReadFile(filepath.Join(projectDir, "main.go"))
	if err != nil {
		t.Fatalf("reading applied file: %v", err)
	}
	if string(applied) != "package main\n\nfunc new() {}\n" {
		t.Errorf("applied content = %q, want the exact approved diff applied", applied)
	}

	// A second application must be refused.
	secondRec := doJSON(t, router, http.MethodPost, "/api/v1/fix-proposals/"+fp.ID+"/apply-repository", nil)
	if secondRec.Code != http.StatusConflict {
		t.Fatalf("second apply-repository status = %d, want 409", secondRec.Code)
	}
}

// TestApplyFixToRepository_PartialFailureDoesNotConsumeOneShotSlot is a
// regression test for a real bug found during live-stack verification:
// a repository-application attempt that fails per-file (e.g. because
// the target directory turns out to be read-only) was still recorded
// as "applied", permanently blocking any retry even after the operator
// fixed the underlying issue. It must instead be retryable.
func TestApplyFixToRepository_PartialFailureDoesNotConsumeOneShotSlot(t *testing.T) {
	router, projectDir, _, bugID := setUpBugForFixProposal(t, newTestDeps(nil, nil))
	os.WriteFile(filepath.Join(projectDir, "main.go"), []byte("package main\n\nfunc old() {}\n"), 0o644)

	createRec := doJSON(t, router, http.MethodPost, "/api/v1/bugs/"+bugID+"/fix-proposal", map[string]any{"unified_diff": validManualDiff})
	var fp fixProposalResponse
	json.Unmarshal(createRec.Body.Bytes(), &fp)
	doJSON(t, router, http.MethodPost, "/api/v1/fix-proposals/"+fp.ID+"/approve", nil)

	// Make the target file read-only to force a per-file write failure,
	// simulating (at a smaller scale) a read-only repository mount.
	targetFile := filepath.Join(projectDir, "main.go")
	if err := os.Chmod(targetFile, 0o444); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { os.Chmod(targetFile, 0o644) })

	firstRec := doJSON(t, router, http.MethodPost, "/api/v1/fix-proposals/"+fp.ID+"/apply-repository", nil)
	if firstRec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("first apply-repository status = %d, want 422, body=%s", firstRec.Code, firstRec.Body.String())
	}

	getRec := doJSON(t, router, http.MethodGet, "/api/v1/fix-proposals/"+fp.ID, nil)
	var afterFailure fixProposalResponse
	json.Unmarshal(getRec.Body.Bytes(), &afterFailure)
	if afterFailure.RepositoryAppliedAt != nil {
		t.Fatal("RepositoryAppliedAt should not be set after a failed application")
	}

	// Fix the underlying issue and retry — this must succeed, not 409.
	os.Chmod(targetFile, 0o644)
	retryRec := doJSON(t, router, http.MethodPost, "/api/v1/fix-proposals/"+fp.ID+"/apply-repository", nil)
	if retryRec.Code != http.StatusOK {
		t.Fatalf("retry apply-repository status = %d, want 200, body=%s", retryRec.Code, retryRec.Body.String())
	}
}

func TestGetFixProposal_NotFound(t *testing.T) {
	router := NewRouter(newTestDeps(nil, nil))
	rec := doJSON(t, router, http.MethodGet, "/api/v1/fix-proposals/does-not-exist", nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestListProjectFixProposals(t *testing.T) {
	router, _, projectID, bugID := setUpBugForFixProposal(t, newTestDeps(nil, nil))
	doJSON(t, router, http.MethodPost, "/api/v1/bugs/"+bugID+"/fix-proposal", map[string]any{"unified_diff": validManualDiff})

	rec := doJSON(t, router, http.MethodGet, "/api/v1/projects/"+projectID+"/fix-proposals", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		FixProposals []fixProposalResponse `json:"fix_proposals"`
	}
	json.Unmarshal(rec.Body.Bytes(), &body)
	if len(body.FixProposals) != 1 {
		t.Fatalf("got %d fix proposals, want 1", len(body.FixProposals))
	}
}
