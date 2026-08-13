package httpserver

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"e2e-sentinel/apps/api/internal/artifacts"
	"e2e-sentinel/apps/api/internal/audit"
	"e2e-sentinel/apps/api/internal/planning"
	"e2e-sentinel/apps/api/internal/runs"
	"e2e-sentinel/apps/api/internal/testgen"
	"e2e-sentinel/apps/api/internal/visualdiff"
)

type testRunResponse struct {
	ID          string  `json:"id"`
	ProjectID   string  `json:"project_id"`
	TestCaseID  string  `json:"test_case_id"`
	Status      string  `json:"status"`
	RunnerType  string  `json:"runner_type"`
	TriggerType string  `json:"trigger_type"`
	CommitSHA   string  `json:"commit_sha"`
	ExitCode    *int    `json:"exit_code"`
	Summary     string  `json:"summary"`
	StartedAt   string  `json:"started_at"`
	FinishedAt  *string `json:"finished_at"`
}

func toTestRunResponse(r runs.TestRun) testRunResponse {
	var finished *string
	if r.FinishedAt != nil {
		s := r.FinishedAt.Format(timeFormat)
		finished = &s
	}
	return testRunResponse{
		ID: r.ID, ProjectID: r.ProjectID, TestCaseID: r.TestCaseID, Status: r.Status,
		RunnerType: r.RunnerType, TriggerType: r.TriggerType, CommitSHA: r.CommitSHA,
		ExitCode: r.ExitCode, Summary: r.Summary,
		StartedAt: r.StartedAt.Format(timeFormat), FinishedAt: finished,
	}
}

type artifactResponse struct {
	ID        string `json:"id"`
	Kind      string `json:"kind"`
	MimeType  string `json:"mime_type"`
	SizeBytes int64  `json:"size_bytes"`
	Checksum  string `json:"checksum"`
}

func toArtifactResponse(a artifacts.Artifact) artifactResponse {
	return artifactResponse{ID: a.ID, Kind: a.Kind, MimeType: a.MimeType, SizeBytes: a.SizeBytes, Checksum: a.Checksum}
}

// handleRunTest starts an approved test case running in an isolated
// runner container. Execution happens in a background goroutine (using
// a fresh, uncancelled context — the HTTP request's own context is
// cancelled the moment this handler returns) so the client gets the
// run's ID back immediately and polls GET /runs/{id} for progress, per
// spec §11.3's live-status expectation. Cancellation (spec §11.1) is
// handled by POST /runs/{id}/cancel, matched to the running container by
// its deterministic name — no in-memory state is needed here.
// runnerFor picks the runner for a test case's framework. "websocket"
// uses WebSocketRunner; everything else (the pre-Phase-11 "playwright"/
// "api" values, and any future default) uses Runner — a two-field
// selection, not a generic registry, to stay consistent with every
// other capability field on Dependencies.
func runnerFor(deps Dependencies, framework string) runs.Runner {
	if framework == "websocket" {
		return deps.WebSocketRunner
	}
	return deps.Runner
}

// runnerByName finds the configured runner whose Name() matches
// runnerType (a TestRun's stored RunnerType) — used where only the run
// itself, not its test case's framework, is available.
func runnerByName(deps Dependencies, runnerType string) runs.Runner {
	for _, r := range []runs.Runner{deps.Runner, deps.WebSocketRunner} {
		if r != nil && r.Name() == runnerType {
			return r
		}
	}
	return nil
}

// Errors TriggerRun can return, checked with errors.Is by both
// handleRunTest and internal/githubci — the latter only logs them and
// moves on to the next test case, so these need to be inspectable
// without parsing an HTTP-shaped message.
var (
	ErrRunnerNotConfigured      = errors.New("httpserver: runner not configured")
	ErrTestNotApproved          = errors.New("httpserver: test case not approved")
	ErrEnvironmentBaseURLNotSet = errors.New("httpserver: environment base_url not set")
	ErrSpecGenerationFailed     = errors.New("httpserver: spec generation failed")
)

func handleRunTest(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		run, err := TriggerRun(r.Context(), deps, chi.URLParam(r, "testID"), runs.TriggerTypeManual, "user", "")
		switch {
		case errors.Is(err, planning.ErrNotFound):
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "test_not_found"})
		case errors.Is(err, ErrRunnerNotConfigured):
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "runner_not_configured"})
		case errors.Is(err, ErrTestNotApproved):
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "test_not_approved", "detail": "only an approved test case can be run"})
		case errors.Is(err, ErrEnvironmentBaseURLNotSet):
			writeJSON(w, http.StatusUnprocessableEntity, map[string]string{
				"error": "environment_base_url_not_set", "detail": "set an environment's base_url before running a test",
			})
		case errors.Is(err, ErrSpecGenerationFailed):
			detail := strings.TrimPrefix(err.Error(), ErrSpecGenerationFailed.Error()+": ")
			writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": "spec_generation_failed", "detail": detail})
		case err != nil:
			deps.Logger.Error().Err(err).Msg("running test failed")
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal_error"})
		default:
			writeJSON(w, http.StatusAccepted, toTestRunResponse(run))
		}
	}
}

// TriggerRun starts an approved test case's run — the one path both
// handleRunTest (a human clicking "Run") and internal/githubci (a
// polled commit) go through, so a CI-triggered run is subject to
// exactly the same rules as a manual one (approved-only, environment
// base_url required, production-classified environments still block
// mutating tests upstream at approval time) and is indistinguishable
// once created except for triggerType/triggeredBy/commitSHA. Execution
// itself happens in a background goroutine (executeRunAsync) using a
// fresh, uncancelled context, since the caller's own context (an HTTP
// request, or one poll tick) ends long before a run finishes.
func TriggerRun(ctx context.Context, deps Dependencies, testCaseID, triggerType, triggeredBy, commitSHA string) (runs.TestRun, error) {
	tc, err := deps.Planning.Get(ctx, testCaseID)
	if err != nil {
		return runs.TestRun{}, err
	}

	runner := runnerFor(deps, tc.Framework)
	if runner == nil {
		return runs.TestRun{}, ErrRunnerNotConfigured
	}

	if tc.ApprovalStatus != planning.ApprovalApproved {
		return runs.TestRun{}, ErrTestNotApproved
	}

	// A WebSocket test's RoutePath is already a full "ws://"/"wss://"
	// URL (see routes.Route's doc comment) — it needs no environment
	// base_url to join against, unlike every Playwright-based test.
	isWebSocket := tc.Framework == "websocket"
	var baseURL string
	if !isWebSocket {
		envs, err := deps.Environments.ListByProject(ctx, tc.ProjectID)
		if err != nil {
			return runs.TestRun{}, fmt.Errorf("listing environments: %w", err)
		}
		for _, env := range envs {
			if env.BaseURL != "" {
				baseURL = env.BaseURL
				break
			}
		}
		if baseURL == "" {
			return runs.TestRun{}, ErrEnvironmentBaseURLNotSet
		}
	}

	run, err := deps.Runs.Create(ctx, runs.TestRun{
		ProjectID: tc.ProjectID, TestCaseID: tc.ID, Status: runs.StatusQueued,
		RunnerType: runner.Name(), TriggerType: triggerType, TriggeredBy: triggeredBy, CommitSHA: commitSHA,
	})
	if err != nil {
		return runs.TestRun{}, fmt.Errorf("creating test run: %w", err)
	}

	filename, content, err := testgen.GenerateSpec(testgen.TestCaseInput{
		ID: tc.ID, Title: tc.Title, RoutePath: tc.RoutePath, RouteMethod: tc.RouteMethod, Framework: tc.Framework,
	}, baseURL)
	if err != nil {
		_, _ = deps.Runs.UpdateStatus(ctx, run.ID, runs.StatusError, nil, err.Error(), true)
		return runs.TestRun{}, fmt.Errorf("%w: %s", ErrSpecGenerationFailed, err.Error())
	}

	if err := deps.Audit.Record(ctx, audit.Event{
		ActionType: "test_run.started", ResourceType: "test_run", ResourceID: run.ID,
		Actor: triggeredBy, Metadata: map[string]any{"test_case_id": tc.ID, "trigger_type": triggerType},
	}); err != nil {
		deps.Logger.Error().Err(err).Msg("recording test_run.started audit event failed")
	}

	go executeRunAsync(deps, runner, run.ID, runs.RunInput{RunID: run.ID, SpecFilename: filename, SpecContent: content})

	return run, nil
}

// executeRunAsync runs entirely on a background context: the triggering
// HTTP request has already returned by the time this typically
// finishes. runner is the one selected by handleRunTest for this test
// case's framework (runnerFor) — passed explicitly rather than read
// from deps.Runner, since which field that is varies by framework.
func executeRunAsync(deps Dependencies, runner runs.Runner, runID string, input runs.RunInput) {
	ctx := context.Background()
	logger := deps.Logger

	if _, err := deps.Runs.UpdateStatus(ctx, runID, runs.StatusRunning, nil, "", false); err != nil {
		logger.Error().Err(err).Str("run_id", runID).Msg("marking run running failed")
	}
	deps.Metrics.ActiveTestRuns.Inc(nil)

	result, err := runner.Execute(ctx, input)
	if err != nil {
		logger.Error().Err(err).Str("run_id", runID).Msg("runner execution failed")
		// Execute may have already created the workspace directory
		// (writing the spec file) before failing to create/start the
		// container — clean it up here too, not just on the success
		// path below, or it leaks on every infra-level failure.
		if err := runner.Cleanup(ctx, runID); err != nil {
			logger.Warn().Err(err).Str("run_id", runID).Msg("runner cleanup after execution failure failed")
		}
		// Failure correlation (and the workspace cleanup above) runs
		// BEFORE the status update below, not after: a caller polling
		// GET /runs/{id} must never observe "error" before its bug
		// report and cleanup already happened.
		if run, getErr := deps.Runs.Get(ctx, runID); getErr == nil {
			// No RunResult exists (Execute failed before producing one) —
			// classify from the error alone so an infra-level failure
			// (spec's "runner failure" type) still becomes a bug
			// candidate, same as an assertion failure would.
			recordFailureAndBug(ctx, deps, run, &runs.RunResult{ExitCode: -1, Stderr: err.Error()}, nil)
		}
		_, _ = deps.Runs.UpdateStatus(ctx, runID, runs.StatusError, nil, err.Error(), true)
		deps.Metrics.ActiveTestRuns.Dec(nil)
		deps.Metrics.TestRunsTotal.Inc(map[string]string{"status": runs.StatusError})
		return
	}

	// A concurrent cancel request may have already marked this run
	// cancelled; don't let a late-arriving result overwrite that with a
	// misleading passed/failed status.
	current, err := deps.Runs.Get(ctx, runID)
	if err == nil && current.Status == runs.StatusCancelled {
		saveRunArtifacts(ctx, deps, runID, result)
		_ = runner.Cleanup(ctx, runID)
		deps.Metrics.ActiveTestRuns.Dec(nil)
		deps.Metrics.TestRunsTotal.Inc(map[string]string{"status": runs.StatusCancelled})
		return
	}

	status := runs.StatusPassed
	if result.ExitCode != 0 {
		status = runs.StatusFailed
	}

	artifactIDs := saveRunArtifacts(ctx, deps, runID, result)

	var screenshotArtifactID string
	var screenshotData []byte

	if artifactFiles, err := runner.CollectArtifacts(ctx, runID); err != nil {
		logger.Warn().Err(err).Str("run_id", runID).Msg("collecting artifacts failed")
	} else {
		retentionUntil := time.Now().Add(artifacts.RetentionFor(status))
		for _, f := range artifactFiles {
			if a, err := deps.Artifacts.Save(ctx, runID, f.Kind, f.MimeType, f.Data, retentionUntil); err != nil {
				logger.Warn().Err(err).Str("run_id", runID).Str("kind", f.Kind).Msg("saving artifact failed")
			} else {
				artifactIDs = append(artifactIDs, a.ID)
				// The Playwright config now captures a screenshot on
				// every run, not just failures (see docker_runner.go) —
				// the first one is what internal/visualdiff compares
				// against this test case's baseline, regardless of
				// whether the run itself passed or failed.
				if f.Kind == artifacts.KindScreenshot && screenshotArtifactID == "" {
					screenshotArtifactID = a.ID
					screenshotData = f.Data
				}
			}
		}
	}

	if screenshotArtifactID != "" {
		processVisualDiff(ctx, deps, current, screenshotArtifactID, screenshotData)
	}

	if err := runner.Cleanup(ctx, runID); err != nil {
		logger.Warn().Err(err).Str("run_id", runID).Msg("runner cleanup failed")
	}

	// Failure correlation runs BEFORE the final status update, not
	// after: a caller polling GET /runs/{id} only ever observes a
	// terminal status once the corresponding bug report already exists,
	// avoiding a race where "failed" is visible before its bug is.
	if status == runs.StatusFailed {
		recordFailureAndBug(ctx, deps, current, result, artifactIDs)
	}

	exitCode := result.ExitCode
	summary := fmt.Sprintf("exit code %d", exitCode)
	if _, err := deps.Runs.UpdateStatus(ctx, runID, status, &exitCode, summary, true); err != nil {
		logger.Error().Err(err).Str("run_id", runID).Msg("recording final run status failed")
	}
	deps.Metrics.ActiveTestRuns.Dec(nil)
	deps.Metrics.TestRunsTotal.Inc(map[string]string{"status": status})

	if err := deps.Audit.Record(ctx, audit.Event{
		ActionType: "test_run.completed", ResourceType: "test_run", ResourceID: runID,
		Actor: "system", Metadata: map[string]any{"status": status, "exit_code": exitCode},
	}); err != nil {
		logger.Error().Err(err).Msg("recording test_run.completed audit event failed")
	}
}

// processVisualDiff runs internal/visualdiff for a page test case's
// fresh screenshot: establishes the baseline on a test case's first
// run, or diffs against the existing baseline and records a
// pending-review row when they differ. Never touches TestRun.Status —
// a visual change is a separate signal a human accepts or ignores, not
// a pass/fail verdict (spec's "pass/fail comes only from the runner's
// exit code" rule is unchanged). Best-effort throughout: any failure
// here is logged and never blocks or alters the run's own lifecycle.
func processVisualDiff(ctx context.Context, deps Dependencies, run runs.TestRun, screenshotArtifactID string, screenshotData []byte) {
	if deps.VisualDiffs == nil {
		return
	}

	tc, err := deps.Planning.Get(ctx, run.TestCaseID)
	if err != nil {
		deps.Logger.Warn().Err(err).Str("run_id", run.ID).Msg("visual diff: getting test case failed")
		return
	}
	// Same page-test predicate handleRunTest/TriggerRun use for
	// isWebSocket — an API-only test (RouteMethod set) never renders a
	// page, so it never produced a meaningful screenshot to diff.
	if tc.Framework == "websocket" || tc.RouteMethod != "" {
		return
	}

	baseline, err := deps.VisualDiffs.GetBaseline(ctx, tc.ID)
	if errors.Is(err, visualdiff.ErrNotFound) {
		if _, err := deps.VisualDiffs.SetBaseline(ctx, tc.ID, screenshotArtifactID, "system"); err != nil {
			deps.Logger.Warn().Err(err).Str("run_id", run.ID).Msg("visual diff: setting initial baseline failed")
		}
		return
	}
	if err != nil {
		deps.Logger.Warn().Err(err).Str("run_id", run.ID).Msg("visual diff: getting baseline failed")
		return
	}

	baselineData, _, err := deps.Artifacts.Read(ctx, baseline.ArtifactID)
	if err != nil {
		deps.Logger.Warn().Err(err).Str("run_id", run.ID).Msg("visual diff: reading baseline artifact failed")
		return
	}

	project, err := deps.Projects.Get(ctx, run.ProjectID)
	if err != nil {
		deps.Logger.Warn().Err(err).Str("run_id", run.ID).Msg("visual diff: getting project for threshold failed")
		return
	}

	result, err := visualdiff.Compare(baselineData, screenshotData, project.VisualDiffThreshold)
	if err != nil {
		deps.Logger.Warn().Err(err).Str("run_id", run.ID).Msg("visual diff: comparing screenshots failed")
		return
	}
	if result.PercentChanged == 0 {
		return // pixel-identical — nothing to add to the review queue
	}

	diffArtifact, err := deps.Artifacts.Save(ctx, run.ID, artifacts.KindScreenshotDiff, "image/png", result.DiffPNG, time.Now().Add(artifacts.RetentionDefault))
	if err != nil {
		deps.Logger.Warn().Err(err).Str("run_id", run.ID).Msg("visual diff: saving diff image failed")
		return
	}

	if _, err := deps.VisualDiffs.CreateDiff(ctx, visualdiff.Diff{
		ProjectID:          run.ProjectID,
		TestRunID:          run.ID,
		TestCaseID:         tc.ID,
		BaselineArtifactID: baseline.ArtifactID,
		CurrentArtifactID:  screenshotArtifactID,
		DiffArtifactID:     diffArtifact.ID,
		PercentChanged:     result.PercentChanged,
		Status:             visualdiff.StatusPendingReview,
	}); err != nil {
		deps.Logger.Warn().Err(err).Str("run_id", run.ID).Msg("visual diff: creating diff row failed")
	}
}

func saveRunArtifacts(ctx context.Context, deps Dependencies, runID string, result *runs.RunResult) []string {
	var artifactIDs []string
	retentionUntil := time.Now().Add(artifacts.RetentionDefault)
	if result.Stdout != "" {
		if a, err := deps.Artifacts.Save(ctx, runID, artifacts.KindStdout, "text/plain; charset=utf-8", []byte(result.Stdout), retentionUntil); err != nil {
			deps.Logger.Warn().Err(err).Str("run_id", runID).Msg("saving stdout artifact failed")
		} else {
			artifactIDs = append(artifactIDs, a.ID)
		}
	}
	if result.Stderr != "" {
		if a, err := deps.Artifacts.Save(ctx, runID, artifacts.KindStderr, "text/plain; charset=utf-8", []byte(result.Stderr), retentionUntil); err != nil {
			deps.Logger.Warn().Err(err).Str("run_id", runID).Msg("saving stderr artifact failed")
		} else {
			artifactIDs = append(artifactIDs, a.ID)
		}
	}
	return artifactIDs
}

func handleGetRun(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		run, err := deps.Runs.Get(r.Context(), chi.URLParam(r, "runID"))
		if errors.Is(err, runs.ErrNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "run_not_found"})
			return
		}
		if err != nil {
			deps.Logger.Error().Err(err).Msg("getting run failed")
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal_error"})
			return
		}
		writeJSON(w, http.StatusOK, toTestRunResponse(run))
	}
}

func handleListProjectRuns(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		list, err := deps.Runs.ListByProject(r.Context(), chi.URLParam(r, "projectID"))
		if err != nil {
			deps.Logger.Error().Err(err).Msg("listing runs failed")
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal_error"})
			return
		}
		out := make([]testRunResponse, 0, len(list))
		for _, run := range list {
			out = append(out, toTestRunResponse(run))
		}
		writeJSON(w, http.StatusOK, map[string]any{"runs": out})
	}
}

func handleCancelRun(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		runID := chi.URLParam(r, "runID")

		run, err := deps.Runs.Get(r.Context(), runID)
		if errors.Is(err, runs.ErrNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "run_not_found"})
			return
		}
		if err != nil {
			deps.Logger.Error().Err(err).Msg("getting run for cancellation failed")
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal_error"})
			return
		}
		if run.Status != runs.StatusQueued && run.Status != runs.StatusRunning {
			writeJSON(w, http.StatusConflict, map[string]string{"error": "run_not_cancellable", "detail": "run has already finished"})
			return
		}

		updated, err := deps.Runs.UpdateStatus(r.Context(), runID, runs.StatusCancelled, nil, "cancelled by user", true)
		if err != nil {
			deps.Logger.Error().Err(err).Msg("marking run cancelled failed")
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal_error"})
			return
		}

		// run.RunnerType (set at Create time from runner.Name()) identifies
		// which runner started this run — needed here since a cancel
		// request doesn't carry the test case's framework, only the run.
		if cancelRunner := runnerByName(deps, run.RunnerType); cancelRunner != nil {
			if err := cancelRunner.Cancel(r.Context(), runID); err != nil {
				deps.Logger.Warn().Err(err).Str("run_id", runID).Msg("stopping runner container failed")
			}
		}

		if err := deps.Audit.Record(r.Context(), audit.Event{
			ActionType: "test_run.cancelled", ResourceType: "test_run", ResourceID: runID, Actor: "user",
		}); err != nil {
			deps.Logger.Error().Err(err).Msg("recording test_run.cancelled audit event failed")
		}

		writeJSON(w, http.StatusOK, toTestRunResponse(updated))
	}
}

func handleListRunArtifacts(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		list, err := deps.Artifacts.ListByRun(r.Context(), chi.URLParam(r, "runID"))
		if err != nil {
			deps.Logger.Error().Err(err).Msg("listing artifacts failed")
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal_error"})
			return
		}
		out := make([]artifactResponse, 0, len(list))
		for _, a := range list {
			out = append(out, toArtifactResponse(a))
		}
		writeJSON(w, http.StatusOK, map[string]any{"artifacts": out})
	}
}

// handleGetArtifactContent serves raw artifact bytes. Content is
// user/tool-generated and untrusted (spec §23.5): nosniff is always
// set, and anything that isn't an image is forced to download rather
// than render inline, so a crafted trace/log file can't execute as HTML
// in a browser tab.
func handleGetArtifactContent(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		data, meta, err := deps.Artifacts.Read(r.Context(), chi.URLParam(r, "artifactID"))
		if errors.Is(err, artifacts.ErrNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "artifact_not_found"})
			return
		}
		if err != nil {
			deps.Logger.Error().Err(err).Msg("reading artifact failed")
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal_error"})
			return
		}

		w.Header().Set("Content-Type", meta.MimeType)
		w.Header().Set("X-Content-Type-Options", "nosniff")
		if meta.Kind != artifacts.KindScreenshot && meta.Kind != artifacts.KindScreenshotDiff {
			w.Header().Set("Content-Disposition", "attachment")
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(data)
	}
}
