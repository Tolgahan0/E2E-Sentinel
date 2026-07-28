package httpserver

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	"e2e-sentinel/apps/api/internal/audit"
	"e2e-sentinel/apps/api/internal/discovery"
	"e2e-sentinel/apps/api/internal/environments"
	"e2e-sentinel/apps/api/internal/planning"
	"e2e-sentinel/apps/api/internal/projects"
	"e2e-sentinel/apps/api/internal/routes"
)

type testCaseResponse struct {
	ID                  string   `json:"id"`
	Title               string   `json:"title"`
	Description         string   `json:"description"`
	Category            string   `json:"category"`
	Framework           string   `json:"framework"`
	Status              string   `json:"status"`
	RiskLevel           string   `json:"risk_level"`
	Priority            string   `json:"priority"`
	Confidence          string   `json:"confidence"`
	Source              string   `json:"source"`
	Steps               []string `json:"steps"`
	Assertions          []string `json:"assertions"`
	RequiredCredentials []string `json:"required_credentials"`
	IsMutating          bool     `json:"is_mutating"`
	IsProductionSafe    bool     `json:"is_production_safe"`
	ApprovalStatus      string   `json:"approval_status"`
}

func toTestCaseResponse(tc planning.TestCase) testCaseResponse {
	return testCaseResponse{
		ID: tc.ID, Title: tc.Title, Description: tc.Description, Category: tc.Category,
		Framework: tc.Framework, Status: tc.Status, RiskLevel: tc.RiskLevel, Priority: tc.Priority,
		Confidence: tc.Confidence, Source: tc.Source, Steps: tc.Steps, Assertions: tc.Assertions,
		RequiredCredentials: tc.RequiredCredentials, IsMutating: tc.IsMutating,
		IsProductionSafe: tc.IsProductionSafe, ApprovalStatus: tc.ApprovalStatus,
	}
}

// handleGenerateTestPlan derives suggested test cases from the latest
// completed discovery run's routes, using only deterministic rules (no
// AI call — spec §25 Phase 4 acceptance). Re-running this never
// overwrites a test case the user has already reviewed, edited, or
// approved (planning.Store.CreateIfAbsent).
func handleGenerateTestPlan(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		projectID := chi.URLParam(r, "projectID")

		project, err := deps.Projects.Get(r.Context(), projectID)
		if errors.Is(err, projects.ErrNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "project_not_found"})
			return
		}
		if err != nil {
			deps.Logger.Error().Err(err).Msg("getting project for test planning failed")
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal_error"})
			return
		}

		_, findings, err := deps.Discovery.LatestCompleted(r.Context(), projectID)
		if errors.Is(err, discovery.ErrNoCompletedRun) {
			writeJSON(w, http.StatusConflict, map[string]string{"error": "no_discovery_run", "detail": "run discovery before generating a test plan"})
			return
		}
		if err != nil {
			deps.Logger.Error().Err(err).Msg("getting latest discovery for test planning failed")
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal_error"})
			return
		}

		extractedRoutes, err := routes.Extract(project.RepositoryPath, findings)
		if err != nil {
			deps.Logger.Error().Err(err).Msg("route extraction for test planning failed")
			writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": "route_extraction_failed"})
			return
		}

		suggested := planning.GeneratePlan(extractedRoutes)
		createdCount := 0
		out := make([]testCaseResponse, 0, len(suggested))
		for _, tc := range suggested {
			tc.ProjectID = projectID
			stored, created, err := deps.Planning.CreateIfAbsent(r.Context(), tc)
			if err != nil {
				deps.Logger.Error().Err(err).Str("natural_key", tc.NaturalKey).Msg("storing suggested test case failed")
				continue
			}
			if created {
				createdCount++
			}
			out = append(out, toTestCaseResponse(stored))
		}

		if err := deps.Audit.Record(r.Context(), audit.Event{
			ActionType: "test_plan.generated", ResourceType: "project", ResourceID: projectID,
			Actor: "user", Metadata: map[string]any{"suggested_count": len(suggested), "new_count": createdCount},
		}); err != nil {
			deps.Logger.Error().Err(err).Msg("recording test_plan.generated audit event failed")
		}

		writeJSON(w, http.StatusOK, map[string]any{"tests": out, "new_count": createdCount})
	}
}

func handleListTests(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		list, err := deps.Planning.List(r.Context(), chi.URLParam(r, "projectID"))
		if err != nil {
			deps.Logger.Error().Err(err).Msg("listing test cases failed")
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal_error"})
			return
		}
		out := make([]testCaseResponse, 0, len(list))
		for _, tc := range list {
			out = append(out, toTestCaseResponse(tc))
		}
		writeJSON(w, http.StatusOK, map[string]any{"tests": out})
	}
}

func handleUpdateTest(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Title       string `json:"title"`
			Description string `json:"description"`
			Priority    string `json:"priority"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_json"})
			return
		}
		if body.Priority != "" {
			switch body.Priority {
			case planning.PriorityP0, planning.PriorityP1, planning.PriorityP2, planning.PriorityP3:
			default:
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_priority"})
				return
			}
		}

		tc, err := deps.Planning.Update(r.Context(), chi.URLParam(r, "testID"), body.Title, body.Description, body.Priority)
		if errors.Is(err, planning.ErrNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "test_not_found"})
			return
		}
		if err != nil {
			deps.Logger.Error().Err(err).Msg("updating test case failed")
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal_error"})
			return
		}
		writeJSON(w, http.StatusOK, toTestCaseResponse(tc))
	}
}

// handleApproveTest enforces spec §25 Phase 4's acceptance criterion:
// "Production-unsafe tests cannot be approved accidentally." A mutating
// test cannot be approved while any of the project's environments is
// classified production or unknown.
func handleApproveTest(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		testID := chi.URLParam(r, "testID")

		tc, err := deps.Planning.Get(r.Context(), testID)
		if errors.Is(err, planning.ErrNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "test_not_found"})
			return
		}
		if err != nil {
			deps.Logger.Error().Err(err).Msg("getting test case for approval failed")
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal_error"})
			return
		}

		if tc.IsMutating {
			envs, err := deps.Environments.ListByProject(r.Context(), tc.ProjectID)
			if err != nil {
				deps.Logger.Error().Err(err).Msg("listing environments for approval safety check failed")
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal_error"})
				return
			}
			for _, env := range envs {
				if env.IsProduction || env.Classification == environments.ClassificationUnknown {
					writeJSON(w, http.StatusForbidden, map[string]string{
						"error":  "production_unsafe_test",
						"detail": "this test mutates state and cannot be approved while the project has a production or unknown-classified environment",
					})
					return
				}
			}
		}

		updated, err := deps.Planning.UpdateApproval(r.Context(), testID, planning.ApprovalApproved)
		if err != nil {
			deps.Logger.Error().Err(err).Msg("approving test case failed")
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal_error"})
			return
		}

		if err := deps.Audit.Record(r.Context(), audit.Event{
			ActionType: "test.approved", ResourceType: "test_case", ResourceID: testID,
			Actor: "user", Metadata: map[string]any{"is_mutating": tc.IsMutating},
		}); err != nil {
			deps.Logger.Error().Err(err).Msg("recording test.approved audit event failed")
		}

		writeJSON(w, http.StatusOK, toTestCaseResponse(updated))
	}
}

func handleRejectTest(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		testID := chi.URLParam(r, "testID")

		updated, err := deps.Planning.UpdateApproval(r.Context(), testID, planning.ApprovalRejected)
		if errors.Is(err, planning.ErrNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "test_not_found"})
			return
		}
		if err != nil {
			deps.Logger.Error().Err(err).Msg("rejecting test case failed")
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal_error"})
			return
		}

		if err := deps.Audit.Record(r.Context(), audit.Event{
			ActionType: "test.rejected", ResourceType: "test_case", ResourceID: testID, Actor: "user",
		}); err != nil {
			deps.Logger.Error().Err(err).Msg("recording test.rejected audit event failed")
		}

		writeJSON(w, http.StatusOK, toTestCaseResponse(updated))
	}
}
