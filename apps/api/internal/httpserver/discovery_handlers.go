package httpserver

import (
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"e2e-sentinel/apps/api/internal/audit"
	"e2e-sentinel/apps/api/internal/discovery"
	"e2e-sentinel/apps/api/internal/projects"
)

type findingResponse struct {
	Category   string         `json:"category"`
	Name       string         `json:"name"`
	Path       string         `json:"path"`
	Confidence string         `json:"confidence"`
	Evidence   map[string]any `json:"evidence"`
}

func toFindingResponse(f discovery.Finding) findingResponse {
	return findingResponse{Category: f.Category, Name: f.Name, Path: f.Path, Confidence: f.Confidence, Evidence: f.Evidence}
}

// handleDiscoverProject runs the deterministic repository scanner
// synchronously. This is intentionally simple for Phase 1 — see
// docs/ROADMAP.md for when this moves onto the async job system.
func handleDiscoverProject(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		projectID := chi.URLParam(r, "projectID")

		project, err := deps.Projects.Get(r.Context(), projectID)
		if errors.Is(err, projects.ErrNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "project_not_found"})
			return
		}
		if err != nil {
			deps.Logger.Error().Err(err).Msg("getting project for discovery failed")
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal_error"})
			return
		}

		if err := deps.Projects.SetDiscoveryStatus(r.Context(), projectID, projects.DiscoveryStatusRunning, nil); err != nil {
			deps.Logger.Error().Err(err).Msg("marking discovery running failed")
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal_error"})
			return
		}

		run, err := deps.Discovery.StartRun(r.Context(), projectID)
		if err != nil {
			deps.Logger.Error().Err(err).Msg("starting discovery run failed")
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal_error"})
			return
		}

		findings, scanErr := discovery.Scan(project.RepositoryPath)
		now := time.Now()
		if scanErr != nil {
			_ = deps.Discovery.FailRun(r.Context(), run.ID, scanErr.Error())
			_ = deps.Projects.SetDiscoveryStatus(r.Context(), projectID, projects.DiscoveryStatusFailed, nil)
			deps.Logger.Error().Err(scanErr).Str("project_id", projectID).Msg("repository scan failed")
			writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": "scan_failed", "detail": scanErr.Error()})
			return
		}

		if err := deps.Discovery.CompleteRun(r.Context(), run.ID, findings); err != nil {
			deps.Logger.Error().Err(err).Msg("recording discovery findings failed")
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal_error"})
			return
		}
		if err := deps.Projects.SetDiscoveryStatus(r.Context(), projectID, projects.DiscoveryStatusComplete, &now); err != nil {
			deps.Logger.Error().Err(err).Msg("marking discovery completed failed")
		}

		if err := deps.Audit.Record(r.Context(), audit.Event{
			ActionType: "repository.scanned", ResourceType: "project", ResourceID: projectID,
			Actor: "user", Metadata: map[string]any{"finding_count": len(findings)},
		}); err != nil {
			deps.Logger.Error().Err(err).Msg("recording repository.scanned audit event failed")
		}

		out := make([]findingResponse, 0, len(findings))
		for _, f := range findings {
			out = append(out, toFindingResponse(f))
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"discovery_run_id": run.ID,
			"status":           discovery.RunStatusCompleted,
			"findings":         out,
		})
	}
}

func handleGetDiscovery(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		projectID := chi.URLParam(r, "projectID")

		if _, err := deps.Projects.Get(r.Context(), projectID); errors.Is(err, projects.ErrNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "project_not_found"})
			return
		}

		run, findings, err := deps.Discovery.LatestCompleted(r.Context(), projectID)
		if errors.Is(err, discovery.ErrNoCompletedRun) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "no_discovery_run"})
			return
		}
		if err != nil {
			deps.Logger.Error().Err(err).Msg("getting latest discovery failed")
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal_error"})
			return
		}

		out := make([]findingResponse, 0, len(findings))
		for _, f := range findings {
			out = append(out, toFindingResponse(f))
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"discovery_run_id": run.ID,
			"completed_at":     run.CompletedAt,
			"findings":         out,
		})
	}
}
