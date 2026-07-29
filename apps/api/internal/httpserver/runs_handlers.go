package httpserver

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"e2e-sentinel/apps/api/internal/artifacts"
	"e2e-sentinel/apps/api/internal/audit"
	"e2e-sentinel/apps/api/internal/planning"
	"e2e-sentinel/apps/api/internal/runs"
	"e2e-sentinel/apps/api/internal/testgen"
)

type testRunResponse struct {
	ID         string  `json:"id"`
	ProjectID  string  `json:"project_id"`
	TestCaseID string  `json:"test_case_id"`
	Status     string  `json:"status"`
	RunnerType string  `json:"runner_type"`
	ExitCode   *int    `json:"exit_code"`
	Summary    string  `json:"summary"`
	StartedAt  string  `json:"started_at"`
	FinishedAt *string `json:"finished_at"`
}

func toTestRunResponse(r runs.TestRun) testRunResponse {
	var finished *string
	if r.FinishedAt != nil {
		s := r.FinishedAt.Format(timeFormat)
		finished = &s
	}
	return testRunResponse{
		ID: r.ID, ProjectID: r.ProjectID, TestCaseID: r.TestCaseID, Status: r.Status,
		RunnerType: r.RunnerType, ExitCode: r.ExitCode, Summary: r.Summary,
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
func handleRunTest(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		testID := chi.URLParam(r, "testID")

		if deps.Runner == nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "runner_not_configured"})
			return
		}

		tc, err := deps.Planning.Get(r.Context(), testID)
		if errors.Is(err, planning.ErrNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "test_not_found"})
			return
		}
		if err != nil {
			deps.Logger.Error().Err(err).Msg("getting test case for run failed")
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal_error"})
			return
		}
		if tc.ApprovalStatus != planning.ApprovalApproved {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "test_not_approved", "detail": "only an approved test case can be run"})
			return
		}

		envs, err := deps.Environments.ListByProject(r.Context(), tc.ProjectID)
		if err != nil {
			deps.Logger.Error().Err(err).Msg("listing environments for run failed")
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal_error"})
			return
		}
		var baseURL string
		for _, env := range envs {
			if env.BaseURL != "" {
				baseURL = env.BaseURL
				break
			}
		}
		if baseURL == "" {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]string{
				"error": "environment_base_url_not_set", "detail": "set an environment's base_url before running a test",
			})
			return
		}

		run, err := deps.Runs.Create(r.Context(), runs.TestRun{
			ProjectID: tc.ProjectID, TestCaseID: tc.ID, Status: runs.StatusQueued,
			RunnerType: deps.Runner.Name(), TriggerType: "manual", TriggeredBy: "user",
		})
		if err != nil {
			deps.Logger.Error().Err(err).Msg("creating test run failed")
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal_error"})
			return
		}

		filename, content, err := testgen.GenerateSpec(testgen.TestCaseInput{
			ID: tc.ID, Title: tc.Title, RoutePath: tc.RoutePath, RouteMethod: tc.RouteMethod,
		}, baseURL)
		if err != nil {
			_, _ = deps.Runs.UpdateStatus(r.Context(), run.ID, runs.StatusError, nil, err.Error(), true)
			writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": "spec_generation_failed", "detail": err.Error()})
			return
		}

		if err := deps.Audit.Record(r.Context(), audit.Event{
			ActionType: "test_run.started", ResourceType: "test_run", ResourceID: run.ID,
			Actor: "user", Metadata: map[string]any{"test_case_id": tc.ID},
		}); err != nil {
			deps.Logger.Error().Err(err).Msg("recording test_run.started audit event failed")
		}

		go executeRunAsync(deps, run.ID, runs.RunInput{RunID: run.ID, SpecFilename: filename, SpecContent: content})

		writeJSON(w, http.StatusAccepted, toTestRunResponse(run))
	}
}

// executeRunAsync runs entirely on a background context: the triggering
// HTTP request has already returned by the time this typically finishes.
func executeRunAsync(deps Dependencies, runID string, input runs.RunInput) {
	ctx := context.Background()
	logger := deps.Logger

	if _, err := deps.Runs.UpdateStatus(ctx, runID, runs.StatusRunning, nil, "", false); err != nil {
		logger.Error().Err(err).Str("run_id", runID).Msg("marking run running failed")
	}
	deps.Metrics.ActiveTestRuns.Inc(nil)

	result, err := deps.Runner.Execute(ctx, input)
	if err != nil {
		logger.Error().Err(err).Str("run_id", runID).Msg("runner execution failed")
		// Execute may have already created the workspace directory
		// (writing the spec file) before failing to create/start the
		// container — clean it up here too, not just on the success
		// path below, or it leaks on every infra-level failure.
		if err := deps.Runner.Cleanup(ctx, runID); err != nil {
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
		_ = deps.Runner.Cleanup(ctx, runID)
		deps.Metrics.ActiveTestRuns.Dec(nil)
		deps.Metrics.TestRunsTotal.Inc(map[string]string{"status": runs.StatusCancelled})
		return
	}

	status := runs.StatusPassed
	if result.ExitCode != 0 {
		status = runs.StatusFailed
	}

	artifactIDs := saveRunArtifacts(ctx, deps, runID, result)

	if artifactFiles, err := deps.Runner.CollectArtifacts(ctx, runID); err != nil {
		logger.Warn().Err(err).Str("run_id", runID).Msg("collecting artifacts failed")
	} else {
		retentionUntil := time.Now().Add(artifacts.RetentionFor(status))
		for _, f := range artifactFiles {
			if a, err := deps.Artifacts.Save(ctx, runID, f.Kind, f.MimeType, f.Data, retentionUntil); err != nil {
				logger.Warn().Err(err).Str("run_id", runID).Str("kind", f.Kind).Msg("saving artifact failed")
			} else {
				artifactIDs = append(artifactIDs, a.ID)
			}
		}
	}

	if err := deps.Runner.Cleanup(ctx, runID); err != nil {
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

		if deps.Runner != nil {
			if err := deps.Runner.Cancel(r.Context(), runID); err != nil {
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
		if meta.Kind != artifacts.KindScreenshot {
			w.Header().Set("Content-Disposition", "attachment")
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(data)
	}
}
