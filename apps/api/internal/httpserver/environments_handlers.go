package httpserver

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	"e2e-sentinel/apps/api/internal/audit"
	"e2e-sentinel/apps/api/internal/environments"
)

type environmentResponse struct {
	ID                      string `json:"id"`
	ProjectID               string `json:"project_id"`
	Name                    string `json:"name"`
	Type                    string `json:"type"`
	BaseURL                 string `json:"base_url"`
	Classification          string `json:"classification"`
	IsProduction            bool   `json:"is_production"`
	AllowMutations          bool   `json:"allow_mutations"`
	AllowLoadTests          bool   `json:"allow_load_tests"`
	AllowActiveSecurityScan bool   `json:"allow_active_security_scan"`
}

func toEnvironmentResponse(e environments.Environment) environmentResponse {
	return environmentResponse{
		ID: e.ID, ProjectID: e.ProjectID, Name: e.Name, Type: e.Type, BaseURL: e.BaseURL,
		Classification: e.Classification, IsProduction: e.IsProduction,
		AllowMutations: e.AllowMutations, AllowLoadTests: e.AllowLoadTests,
		AllowActiveSecurityScan: e.AllowActiveSecurityScan,
	}
}

func handleListEnvironments(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		list, err := deps.Environments.ListByProject(r.Context(), chi.URLParam(r, "projectID"))
		if err != nil {
			deps.Logger.Error().Err(err).Msg("listing environments failed")
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal_error"})
			return
		}
		out := make([]environmentResponse, 0, len(list))
		for _, e := range list {
			out = append(out, toEnvironmentResponse(e))
		}
		writeJSON(w, http.StatusOK, map[string]any{"environments": out})
	}
}

func handleUpdateEnvironment(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Classification string  `json:"classification"`
			BaseURL        *string `json:"base_url"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_json"})
			return
		}

		environmentID := chi.URLParam(r, "environmentID")
		var env environments.Environment
		var err error

		if body.Classification != "" {
			if !environments.ValidClassification(body.Classification) {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_classification"})
				return
			}
			env, err = deps.Environments.UpdateClassification(r.Context(), environmentID, body.Classification)
			if errors.Is(err, environments.ErrNotFound) {
				writeJSON(w, http.StatusNotFound, map[string]string{"error": "environment_not_found"})
				return
			}
			if err != nil {
				deps.Logger.Error().Err(err).Msg("updating environment classification failed")
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal_error"})
				return
			}
			if err := deps.Audit.Record(r.Context(), audit.Event{
				ActionType: "environment.classification_changed", ResourceType: "environment", ResourceID: environmentID,
				Actor: "user", Metadata: map[string]any{"classification": env.Classification},
			}); err != nil {
				deps.Logger.Error().Err(err).Msg("recording environment.classification_changed audit event failed")
			}
		}

		if body.BaseURL != nil {
			env, err = deps.Environments.UpdateBaseURL(r.Context(), environmentID, *body.BaseURL)
			if errors.Is(err, environments.ErrNotFound) {
				writeJSON(w, http.StatusNotFound, map[string]string{"error": "environment_not_found"})
				return
			}
			if err != nil {
				deps.Logger.Error().Err(err).Msg("updating environment base_url failed")
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal_error"})
				return
			}
			if err := deps.Audit.Record(r.Context(), audit.Event{
				ActionType: "environment.base_url_changed", ResourceType: "environment", ResourceID: environmentID,
				Actor: "user",
			}); err != nil {
				deps.Logger.Error().Err(err).Msg("recording environment.base_url_changed audit event failed")
			}
		}

		if body.Classification == "" && body.BaseURL == nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "nothing_to_update"})
			return
		}

		writeJSON(w, http.StatusOK, toEnvironmentResponse(env))
	}
}
