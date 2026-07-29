package httpserver

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	"e2e-sentinel/apps/api/internal/audit"
	"e2e-sentinel/apps/api/internal/bugreports"
)

type noteResponse struct {
	Author    string `json:"author"`
	Text      string `json:"text"`
	CreatedAt string `json:"created_at"`
}

type bugReportResponse struct {
	ID              string `json:"id"`
	ProjectID       string `json:"project_id"`
	FailureID       string `json:"failure_id"`
	TestCaseID      string `json:"test_case_id"`
	EnvironmentID   string `json:"environment_id"`
	Title           string `json:"title"`
	Severity        string `json:"severity"`
	FailureType     string `json:"failure_type"`
	AffectedService string `json:"affected_service"`
	AffectedRoute   string `json:"affected_route"`

	Preconditions    string   `json:"preconditions"`
	StepsToReproduce []string `json:"steps_to_reproduce"`
	ExpectedResult   string   `json:"expected_result"`
	ActualResult     string   `json:"actual_result"`

	ArtifactIDs  []string `json:"artifact_ids"`
	ErrorMessage string   `json:"error_message"`

	FirstObservedAt string `json:"first_observed_at"`
	LastObservedAt  string `json:"last_observed_at"`
	Frequency       int    `json:"frequency"`

	// RootCauseHypothesis is never a confirmed diagnosis — see
	// RootCauseIsUnverifiedHypothesis, always true (spec §14 acceptance:
	// "Root cause is clearly marked as hypothesis").
	RootCauseHypothesis             string `json:"root_cause_hypothesis"`
	RootCauseConfidence             string `json:"root_cause_confidence"`
	RootCauseIsUnverifiedHypothesis bool   `json:"root_cause_is_unverified_hypothesis"`

	FlakyAssessment       string   `json:"flaky_assessment"`
	RelatedGraphPath      string   `json:"related_graph_path"`
	RegressionTestIDs     []string `json:"regression_test_ids"`
	PossibleDuplicateOfID string   `json:"possible_duplicate_of_id,omitempty"`

	Status string         `json:"status"`
	Notes  []noteResponse `json:"notes"`
}

func toBugReportResponse(b bugreports.BugReport) bugReportResponse {
	notes := make([]noteResponse, 0, len(b.Notes))
	for _, n := range b.Notes {
		notes = append(notes, noteResponse{Author: n.Author, Text: n.Text, CreatedAt: n.CreatedAt.Format(timeFormat)})
	}
	steps := b.StepsToReproduce
	if steps == nil {
		steps = []string{}
	}
	regressionTestIDs := b.RegressionTestIDs
	if regressionTestIDs == nil {
		regressionTestIDs = []string{}
	}
	artifactIDs := b.Evidence.ArtifactIDs
	if artifactIDs == nil {
		artifactIDs = []string{}
	}

	return bugReportResponse{
		ID: b.ID, ProjectID: b.ProjectID, FailureID: b.FailureID, TestCaseID: b.TestCaseID, EnvironmentID: b.EnvironmentID,
		Title: b.Title, Severity: b.Severity, FailureType: b.FailureType,
		AffectedService: b.AffectedService, AffectedRoute: b.AffectedRoute,
		Preconditions: b.Preconditions, StepsToReproduce: steps, ExpectedResult: b.ExpectedResult, ActualResult: b.ActualResult,
		ArtifactIDs: artifactIDs, ErrorMessage: b.Evidence.ErrorMessage,
		FirstObservedAt: b.FirstObservedAt.Format(timeFormat), LastObservedAt: b.LastObservedAt.Format(timeFormat), Frequency: b.Frequency,
		RootCauseHypothesis: b.RootCauseHypothesis, RootCauseConfidence: b.RootCauseConfidence, RootCauseIsUnverifiedHypothesis: true,
		FlakyAssessment: b.FlakyAssessment, RelatedGraphPath: b.RelatedGraphPath, RegressionTestIDs: regressionTestIDs,
		PossibleDuplicateOfID: b.PossibleDuplicateOfID, Status: b.Status, Notes: notes,
	}
}

func handleListBugs(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		list, err := deps.Bugs.List(r.Context(), bugreports.ListFilter{
			ProjectID: q.Get("project_id"), Severity: q.Get("severity"),
			Status: q.Get("status"), EnvironmentID: q.Get("environment_id"), Search: q.Get("search"),
		})
		if err != nil {
			deps.Logger.Error().Err(err).Msg("listing bug reports failed")
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal_error"})
			return
		}
		out := make([]bugReportResponse, 0, len(list))
		for _, b := range list {
			out = append(out, toBugReportResponse(b))
		}
		writeJSON(w, http.StatusOK, map[string]any{"bugs": out})
	}
}

func handleGetBug(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		b, err := deps.Bugs.Get(r.Context(), chi.URLParam(r, "bugID"))
		if errors.Is(err, bugreports.ErrNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "bug_not_found"})
			return
		}
		if err != nil {
			deps.Logger.Error().Err(err).Msg("getting bug report failed")
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal_error"})
			return
		}
		writeJSON(w, http.StatusOK, toBugReportResponse(b))
	}
}

func handleResolveBug(deps Dependencies) http.HandlerFunc {
	return handleSetBugStatus(deps, bugreports.StatusResolved, "bug_report.resolved")
}

func handleReopenBug(deps Dependencies) http.HandlerFunc {
	return handleSetBugStatus(deps, bugreports.StatusReopened, "bug_report.reopened")
}

func handleSetBugStatus(deps Dependencies, status, actionType string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		bugID := chi.URLParam(r, "bugID")
		b, err := deps.Bugs.UpdateStatus(r.Context(), bugID, status)
		if errors.Is(err, bugreports.ErrNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "bug_not_found"})
			return
		}
		if err != nil {
			deps.Logger.Error().Err(err).Msg("updating bug report status failed")
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal_error"})
			return
		}
		if err := deps.Audit.Record(r.Context(), audit.Event{
			ActionType: actionType, ResourceType: "bug_report", ResourceID: bugID, Actor: "user",
		}); err != nil {
			deps.Logger.Error().Err(err).Msg("recording bug report status audit event failed")
		}
		writeJSON(w, http.StatusOK, toBugReportResponse(b))
	}
}

func handleAddBugNote(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Author string `json:"author"`
			Text   string `json:"text"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_json"})
			return
		}
		if body.Text == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "text_required"})
			return
		}
		author := body.Author
		if author == "" {
			author = "user"
		}

		bugID := chi.URLParam(r, "bugID")
		b, err := deps.Bugs.AddNote(r.Context(), bugID, author, body.Text)
		if errors.Is(err, bugreports.ErrNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "bug_not_found"})
			return
		}
		if err != nil {
			deps.Logger.Error().Err(err).Msg("adding bug report note failed")
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal_error"})
			return
		}
		if err := deps.Audit.Record(r.Context(), audit.Event{
			ActionType: "bug_report.note_added", ResourceType: "bug_report", ResourceID: bugID, Actor: "user",
		}); err != nil {
			deps.Logger.Error().Err(err).Msg("recording bug_report.note_added audit event failed")
		}
		writeJSON(w, http.StatusOK, toBugReportResponse(b))
	}
}

// handleExportBugMarkdown and handleExportBugJSON serve downloadable
// exports (spec §14 "Export Markdown"/"Export JSON"). Content is
// system-generated but includes captured error text from the target
// under test, so the same defensive headers as artifact downloads apply
// (spec §23.5): nosniff, forced attachment.
func handleExportBugMarkdown(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		b, err := deps.Bugs.Get(r.Context(), chi.URLParam(r, "bugID"))
		if errors.Is(err, bugreports.ErrNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "bug_not_found"})
			return
		}
		if err != nil {
			deps.Logger.Error().Err(err).Msg("exporting bug report (markdown) failed")
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal_error"})
			return
		}
		w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Content-Disposition", "attachment; filename=\""+b.ID+".md\"")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(bugreports.RenderMarkdown(b)))
	}
}

func handleExportBugJSON(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		b, err := deps.Bugs.Get(r.Context(), chi.URLParam(r, "bugID"))
		if errors.Is(err, bugreports.ErrNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "bug_not_found"})
			return
		}
		if err != nil {
			deps.Logger.Error().Err(err).Msg("exporting bug report (json) failed")
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal_error"})
			return
		}
		data, err := bugreports.RenderJSON(b)
		if err != nil {
			deps.Logger.Error().Err(err).Msg("rendering bug report json failed")
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal_error"})
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Content-Disposition", "attachment; filename=\""+b.ID+".json\"")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(data)
	}
}
