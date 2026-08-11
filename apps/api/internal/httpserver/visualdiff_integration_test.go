package httpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"e2e-sentinel/apps/api/internal/artifacts"
	"e2e-sentinel/apps/api/internal/runs"
)

// solidScreenshot encodes a small solid-color PNG — a stand-in for a
// Playwright full-page screenshot, just small enough to keep these
// tests fast.
func solidScreenshot(t *testing.T, c color.Color) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 20, 20))
	for y := 0; y < 20; y++ {
		for x := 0; x < 20; x++ {
			img.Set(x, y, c)
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encoding test screenshot: %v", err)
	}
	return buf.Bytes()
}

// setUpPageProject registers a project with a single Next.js page
// route (app/dashboard/page.tsx, not an API route.ts) — planning turns
// this into exactly one "Page: ... renders without error" test case
// with RouteMethod == "", the predicate processVisualDiff uses to
// decide a run is screenshot-diffable.
func setUpPageProject(t *testing.T, router http.Handler) (projectID, testID string) {
	t.Helper()
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "app", "dashboard", "page.tsx"), `export default function Page() { return null }`)
	projectID = setUpProjectWithDiscovery(t, router, dir)
	testID = approveFirstSuggestedTest(t, router, projectID)
	setEnvironmentBaseURL(t, router, projectID, "http://localhost:3000")
	return projectID, testID
}

func runTestCaseWithScreenshot(t *testing.T, router http.Handler, testID string) testRunResponse {
	t.Helper()
	rec := doJSON(t, router, http.MethodPost, "/api/v1/tests/"+testID+"/run", nil)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("run status = %d, want 202, body=%s", rec.Code, rec.Body.String())
	}
	var run testRunResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &run); err != nil {
		t.Fatalf("decoding run response: %v", err)
	}
	return waitForRunStatus(t, router, run.ID, "passed", 2*time.Second)
}

func listProjectVisualDiffs(t *testing.T, router http.Handler, projectID string) []visualDiffResponse {
	t.Helper()
	rec := doJSON(t, router, http.MethodGet, "/api/v1/projects/"+projectID+"/visual-diffs", nil)
	var body struct {
		VisualDiffs []visualDiffResponse `json:"visual_diffs"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decoding visual diffs response: %v", err)
	}
	return body.VisualDiffs
}

func TestVisualDiff_FirstRunEstablishesBaselineNoDiff(t *testing.T) {
	deps := newTestDeps(nil, nil)
	deps.Runner = &fakeRunner{
		executeFunc: func(context.Context, runs.RunInput) (*runs.RunResult, error) {
			return &runs.RunResult{ExitCode: 0}, nil
		},
		artifacts: []runs.ArtifactFile{{Kind: artifacts.KindScreenshot, MimeType: "image/png", Data: solidScreenshot(t, color.White)}},
	}
	router := NewRouter(deps)

	projectID, testID := setUpPageProject(t, router)
	runTestCaseWithScreenshot(t, router, testID)

	diffs := listProjectVisualDiffs(t, router, projectID)
	if len(diffs) != 0 {
		t.Errorf("visual diffs after the first run = %d, want 0 (first run only sets the baseline)", len(diffs))
	}
}

func TestVisualDiff_SecondRunWithChangedScreenshotCreatesPendingDiff(t *testing.T) {
	fake := &fakeRunner{
		executeFunc: func(context.Context, runs.RunInput) (*runs.RunResult, error) {
			return &runs.RunResult{ExitCode: 0}, nil
		},
		artifacts: []runs.ArtifactFile{{Kind: artifacts.KindScreenshot, MimeType: "image/png", Data: solidScreenshot(t, color.White)}},
	}
	deps := newTestDeps(nil, nil)
	deps.Runner = fake
	router := NewRouter(deps)

	projectID, testID := setUpPageProject(t, router)
	runTestCaseWithScreenshot(t, router, testID) // establishes the baseline (white)

	fake.mu.Lock()
	fake.artifacts = []runs.ArtifactFile{{Kind: artifacts.KindScreenshot, MimeType: "image/png", Data: solidScreenshot(t, color.Black)}}
	fake.mu.Unlock()
	runTestCaseWithScreenshot(t, router, testID) // black now — a full-canvas change

	diffs := listProjectVisualDiffs(t, router, projectID)
	if len(diffs) != 1 {
		t.Fatalf("visual diffs after the second (changed) run = %d, want 1", len(diffs))
	}
	d := diffs[0]
	if d.Status != "pending_review" {
		t.Errorf("Status = %q, want pending_review", d.Status)
	}
	if d.PercentChanged != 100 {
		t.Errorf("PercentChanged = %v, want 100 (white vs black covers the whole canvas)", d.PercentChanged)
	}
	if d.TestCaseID != testID {
		t.Errorf("TestCaseID = %q, want %q", d.TestCaseID, testID)
	}

	// The run's own pass/fail is unaffected by the visual diff — it's a
	// separate signal, never a verdict.
	runsRec := doJSON(t, router, http.MethodGet, "/api/v1/projects/"+projectID+"/runs", nil)
	var runsBody struct {
		Runs []testRunResponse `json:"runs"`
	}
	json.Unmarshal(runsRec.Body.Bytes(), &runsBody)
	for _, r := range runsBody.Runs {
		if r.Status != "passed" {
			t.Errorf("run %s status = %q, want passed (visual diff must not change it)", r.ID, r.Status)
		}
	}
}

func TestVisualDiff_AcceptAdvancesBaseline(t *testing.T) {
	fake := &fakeRunner{
		executeFunc: func(context.Context, runs.RunInput) (*runs.RunResult, error) {
			return &runs.RunResult{ExitCode: 0}, nil
		},
		artifacts: []runs.ArtifactFile{{Kind: artifacts.KindScreenshot, MimeType: "image/png", Data: solidScreenshot(t, color.White)}},
	}
	deps := newTestDeps(nil, nil)
	deps.Runner = fake
	router := NewRouter(deps)

	projectID, testID := setUpPageProject(t, router)
	runTestCaseWithScreenshot(t, router, testID)

	fake.mu.Lock()
	fake.artifacts = []runs.ArtifactFile{{Kind: artifacts.KindScreenshot, MimeType: "image/png", Data: solidScreenshot(t, color.Black)}}
	fake.mu.Unlock()
	runTestCaseWithScreenshot(t, router, testID)

	diffs := listProjectVisualDiffs(t, router, projectID)
	if len(diffs) != 1 {
		t.Fatalf("expected exactly one pending diff, got %d", len(diffs))
	}
	diffID := diffs[0].ID

	acceptRec := doJSON(t, router, http.MethodPost, "/api/v1/visual-diffs/"+diffID+"/accept", nil)
	if acceptRec.Code != http.StatusOK {
		t.Fatalf("accept status = %d, want 200, body=%s", acceptRec.Code, acceptRec.Body.String())
	}
	var accepted visualDiffResponse
	json.Unmarshal(acceptRec.Body.Bytes(), &accepted)
	if accepted.Status != "accepted" {
		t.Errorf("Status = %q, want accepted", accepted.Status)
	}

	// A third run with the SAME (black) screenshot now matches the
	// just-accepted baseline — no new pending diff.
	runTestCaseWithScreenshot(t, router, testID)
	diffsAfter := listProjectVisualDiffs(t, router, projectID)
	pending := 0
	for _, d := range diffsAfter {
		if d.Status == "pending_review" {
			pending++
		}
	}
	if pending != 0 {
		t.Errorf("pending diffs after accepting = %d, want 0 (baseline now matches every subsequent run)", pending)
	}
}

func TestVisualDiff_IgnoreLeavesBaselineUnchanged(t *testing.T) {
	fake := &fakeRunner{
		executeFunc: func(context.Context, runs.RunInput) (*runs.RunResult, error) {
			return &runs.RunResult{ExitCode: 0}, nil
		},
		artifacts: []runs.ArtifactFile{{Kind: artifacts.KindScreenshot, MimeType: "image/png", Data: solidScreenshot(t, color.White)}},
	}
	deps := newTestDeps(nil, nil)
	deps.Runner = fake
	router := NewRouter(deps)

	projectID, testID := setUpPageProject(t, router)
	runTestCaseWithScreenshot(t, router, testID)

	fake.mu.Lock()
	fake.artifacts = []runs.ArtifactFile{{Kind: artifacts.KindScreenshot, MimeType: "image/png", Data: solidScreenshot(t, color.Black)}}
	fake.mu.Unlock()
	runTestCaseWithScreenshot(t, router, testID)

	diffs := listProjectVisualDiffs(t, router, projectID)
	diffID := diffs[0].ID

	ignoreRec := doJSON(t, router, http.MethodPost, "/api/v1/visual-diffs/"+diffID+"/ignore", nil)
	if ignoreRec.Code != http.StatusOK {
		t.Fatalf("ignore status = %d, want 200, body=%s", ignoreRec.Code, ignoreRec.Body.String())
	}

	// The baseline is still white (unchanged by ignore) — running with
	// black again produces ANOTHER pending diff, not a match.
	runTestCaseWithScreenshot(t, router, testID)
	diffsAfter := listProjectVisualDiffs(t, router, projectID)
	pending := 0
	for _, d := range diffsAfter {
		if d.Status == "pending_review" {
			pending++
		}
	}
	if pending != 1 {
		t.Errorf("pending diffs after ignore + another unchanged-vs-baseline run = %d, want 1 (ignore must not advance the baseline)", pending)
	}
}

func TestGetVisualDiff_NotFound(t *testing.T) {
	router := NewRouter(newTestDeps(nil, nil))
	rec := doJSON(t, router, http.MethodGet, "/api/v1/visual-diffs/does-not-exist", nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}
