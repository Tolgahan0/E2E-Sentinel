package httpserver

import (
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	"e2e-sentinel/apps/api/internal/audit"
	"e2e-sentinel/apps/api/internal/visualdiff"
)

type visualDiffResponse struct {
	ID                 string  `json:"id"`
	ProjectID          string  `json:"project_id"`
	TestRunID          string  `json:"test_run_id"`
	TestCaseID         string  `json:"test_case_id"`
	BaselineArtifactID string  `json:"baseline_artifact_id"`
	CurrentArtifactID  string  `json:"current_artifact_id"`
	DiffArtifactID     string  `json:"diff_artifact_id"`
	PercentChanged     float64 `json:"percent_changed"`
	Status             string  `json:"status"`
	ReviewedBy         *string `json:"reviewed_by"`
	ReviewedAt         *string `json:"reviewed_at"`
	CreatedAt          string  `json:"created_at"`
}

func toVisualDiffResponse(d visualdiff.Diff) visualDiffResponse {
	var reviewedAt *string
	if d.ReviewedAt != nil {
		s := d.ReviewedAt.Format(timeFormat)
		reviewedAt = &s
	}
	return visualDiffResponse{
		ID: d.ID, ProjectID: d.ProjectID, TestRunID: d.TestRunID, TestCaseID: d.TestCaseID,
		BaselineArtifactID: d.BaselineArtifactID, CurrentArtifactID: d.CurrentArtifactID, DiffArtifactID: d.DiffArtifactID,
		PercentChanged: d.PercentChanged, Status: d.Status, ReviewedBy: d.ReviewedBy, ReviewedAt: reviewedAt,
		CreatedAt: d.CreatedAt.Format(timeFormat),
	}
}

// handleListProjectVisualDiffs lists a project's visual diffs,
// pending_review ones first (Store.ListByProject's own ordering) — the
// review queue a human actually needs to act on, not buried under
// already-resolved history.
func handleListProjectVisualDiffs(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		list, err := deps.VisualDiffs.ListByProject(r.Context(), chi.URLParam(r, "projectID"))
		if err != nil {
			deps.Logger.Error().Err(err).Msg("listing visual diffs failed")
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal_error"})
			return
		}
		out := make([]visualDiffResponse, 0, len(list))
		for _, d := range list {
			out = append(out, toVisualDiffResponse(d))
		}
		writeJSON(w, http.StatusOK, map[string]any{"visual_diffs": out})
	}
}

func handleGetVisualDiff(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		d, err := deps.VisualDiffs.Get(r.Context(), chi.URLParam(r, "diffID"))
		if errors.Is(err, visualdiff.ErrNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "visual_diff_not_found"})
			return
		}
		if err != nil {
			deps.Logger.Error().Err(err).Msg("getting visual diff failed")
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal_error"})
			return
		}
		writeJSON(w, http.StatusOK, toVisualDiffResponse(d))
	}
}

// handleAcceptVisualDiff marks a diff accepted and makes the run's
// screenshot the new baseline — the one place this feature writes a
// baseline outside the automatic "first run" case, and always an
// explicit human action (spec's approval-gate pattern, same permission
// as approving a test case: auth.PermApproveTestPlans).
func handleAcceptVisualDiff(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		diffID := chi.URLParam(r, "diffID")

		d, err := deps.VisualDiffs.Get(r.Context(), diffID)
		if errors.Is(err, visualdiff.ErrNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "visual_diff_not_found"})
			return
		}
		if err != nil {
			deps.Logger.Error().Err(err).Msg("getting visual diff for accept failed")
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal_error"})
			return
		}

		updated, err := deps.VisualDiffs.UpdateStatus(r.Context(), diffID, visualdiff.StatusAccepted, "user")
		if err != nil {
			deps.Logger.Error().Err(err).Msg("accepting visual diff failed")
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal_error"})
			return
		}
		if _, err := deps.VisualDiffs.SetBaseline(r.Context(), d.TestCaseID, d.CurrentArtifactID, "user"); err != nil {
			deps.Logger.Error().Err(err).Msg("advancing baseline after accept failed")
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal_error"})
			return
		}

		if err := deps.Audit.Record(r.Context(), audit.Event{
			ActionType: "visual_diff.accepted", ResourceType: "visual_diff", ResourceID: diffID,
			Actor: "user", Metadata: map[string]any{"test_case_id": d.TestCaseID, "percent_changed": d.PercentChanged},
		}); err != nil {
			deps.Logger.Error().Err(err).Msg("recording visual_diff.accepted audit event failed")
		}

		writeJSON(w, http.StatusOK, toVisualDiffResponse(updated))
	}
}

// handleIgnoreVisualDiff marks a diff reviewed without touching the
// baseline — the change was noise or a known, not-yet-fixed cosmetic
// issue, but not something to treat as the new "expected" screenshot.
func handleIgnoreVisualDiff(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		diffID := chi.URLParam(r, "diffID")

		updated, err := deps.VisualDiffs.UpdateStatus(r.Context(), diffID, visualdiff.StatusIgnored, "user")
		if errors.Is(err, visualdiff.ErrNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "visual_diff_not_found"})
			return
		}
		if err != nil {
			deps.Logger.Error().Err(err).Msg("ignoring visual diff failed")
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal_error"})
			return
		}

		if err := deps.Audit.Record(r.Context(), audit.Event{
			ActionType: "visual_diff.ignored", ResourceType: "visual_diff", ResourceID: diffID, Actor: "user",
		}); err != nil {
			deps.Logger.Error().Err(err).Msg("recording visual_diff.ignored audit event failed")
		}

		writeJSON(w, http.StatusOK, toVisualDiffResponse(updated))
	}
}
