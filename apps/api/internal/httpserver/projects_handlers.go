package httpserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5"

	"e2e-sentinel/apps/api/internal/audit"
	"e2e-sentinel/apps/api/internal/environments"
	"e2e-sentinel/apps/api/internal/projects"
	"e2e-sentinel/apps/api/internal/visualdiff"
)

type projectResponse struct {
	ID               string  `json:"id"`
	Name             string  `json:"name"`
	Slug             string  `json:"slug"`
	RepositoryPath   string  `json:"repository_path"`
	RepositoryType   string  `json:"repository_type"`
	DefaultBranch    string  `json:"default_branch"`
	DiscoveryStatus  string  `json:"discovery_status"`
	CurrentMode      string  `json:"current_mode"`
	LastDiscoveredAt *string `json:"last_discovered_at"`
	CreatedAt        string  `json:"created_at"`
	UpdatedAt        string  `json:"updated_at"`
	// GitHubRepo/GitHubCIConfigured describe internal/githubci's
	// per-project config — the token itself (GitHubTokenSecretReferenceID)
	// is never returned, same has_api_key-style boolean pattern as
	// providers.Provider.
	GitHubRepo         string `json:"github_repo"`
	GitHubCIConfigured bool   `json:"github_ci_configured"`
	// VisualDiffThreshold is this project's internal/visualdiff.Compare
	// sensitivity — see PATCH .../visual-diff-threshold.
	VisualDiffThreshold float64 `json:"visual_diff_threshold"`
}

func toProjectResponse(p projects.Project) projectResponse {
	var lastDiscovered *string
	if p.LastDiscoveredAt != nil {
		s := p.LastDiscoveredAt.Format(timeFormat)
		lastDiscovered = &s
	}
	return projectResponse{
		ID: p.ID, Name: p.Name, Slug: p.Slug, RepositoryPath: p.RepositoryPath,
		RepositoryType: p.RepositoryType, DefaultBranch: p.DefaultBranch,
		DiscoveryStatus: p.DiscoveryStatus, CurrentMode: p.CurrentMode,
		LastDiscoveredAt: lastDiscovered,
		CreatedAt:        p.CreatedAt.Format(timeFormat),
		UpdatedAt:        p.UpdatedAt.Format(timeFormat),
		GitHubRepo:       p.GitHubRepo, GitHubCIConfigured: p.GitHubTokenSecretReferenceID != "",
		VisualDiffThreshold: p.VisualDiffThreshold,
	}
}

const timeFormat = "2006-01-02T15:04:05.999999999Z07:00"

func handleCreateProject(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Name           string `json:"name"`
			RepositoryPath string `json:"repository_path"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_json"})
			return
		}

		if err := projects.ValidateName(body.Name); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name_required"})
			return
		}

		resolvedPath, err := projects.ValidateRepositoryPath(body.RepositoryPath)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_repository_path", "detail": err.Error()})
			return
		}

		slug, err := uniqueSlug(r.Context(), deps.Projects, body.Name)
		if err != nil {
			deps.Logger.Error().Err(err).Msg("generating project slug failed")
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal_error"})
			return
		}

		project, err := deps.Projects.Create(r.Context(), projects.Project{
			Name:           body.Name,
			Slug:           slug,
			RepositoryPath: resolvedPath,
			RepositoryType: "local",
			DefaultBranch:  "main",
		})
		if err != nil {
			deps.Logger.Error().Err(err).Msg("creating project failed")
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal_error"})
			return
		}

		if _, err := deps.Environments.Create(r.Context(), environments.DefaultForProject(project.ID)); err != nil {
			deps.Logger.Error().Err(err).Msg("creating default environment failed")
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal_error"})
			return
		}

		if err := deps.Audit.Record(r.Context(), audit.Event{
			ActionType: "project.added", ResourceType: "project", ResourceID: project.ID,
			Actor: "user", Metadata: map[string]any{"name": project.Name, "slug": project.Slug},
		}); err != nil {
			deps.Logger.Error().Err(err).Msg("recording project.added audit event failed")
		}

		writeJSON(w, http.StatusCreated, toProjectResponse(project))
	}
}

// uniqueSlug derives a slug from name and appends -2, -3, ... until it
// finds one not already used by another project.
func uniqueSlug(ctx context.Context, store projects.Store, name string) (string, error) {
	base := projects.Slugify(name)

	for attempt := 1; attempt <= 1000; attempt++ {
		candidate := base
		if attempt > 1 {
			candidate = fmt.Sprintf("%s-%d", base, attempt)
		}

		exists, err := store.SlugExists(ctx, candidate)
		if err != nil {
			return "", fmt.Errorf("checking slug uniqueness: %w", err)
		}
		if !exists {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("could not find a unique slug for %q after 1000 attempts", name)
}

func handleListProjects(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		list, err := deps.Projects.List(r.Context())
		if err != nil {
			deps.Logger.Error().Err(err).Msg("listing projects failed")
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal_error"})
			return
		}
		out := make([]projectResponse, 0, len(list))
		for _, p := range list {
			out = append(out, toProjectResponse(p))
		}
		writeJSON(w, http.StatusOK, map[string]any{"projects": out})
	}
}

func handleGetProject(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		project, err := deps.Projects.Get(r.Context(), chi.URLParam(r, "projectID"))
		if errors.Is(err, projects.ErrNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "project_not_found"})
			return
		}
		if err != nil {
			deps.Logger.Error().Err(err).Msg("getting project failed")
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal_error"})
			return
		}
		writeJSON(w, http.StatusOK, toProjectResponse(project))
	}
}

// handleDeleteProject permanently removes a project and everything
// derived from it (environments, discovered services, graph, test
// cases, runs, failures, bug reports, fix proposals, Kubernetes
// resources — via foreign-key cascade, see projects.Store.Delete). This
// is destructive and irreversible; the panel confirms with the user
// before calling it (see apps/web/app/projects/page.tsx).
func handleDeleteProject(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		projectID := chi.URLParam(r, "projectID")

		project, err := deps.Projects.Get(r.Context(), projectID)
		if errors.Is(err, projects.ErrNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "project_not_found"})
			return
		}
		if err != nil {
			deps.Logger.Error().Err(err).Msg("getting project for deletion failed")
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal_error"})
			return
		}

		if err := deps.Projects.Delete(r.Context(), projectID); err != nil {
			deps.Logger.Error().Err(err).Msg("deleting project failed")
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal_error"})
			return
		}

		if err := deps.Audit.Record(r.Context(), audit.Event{
			ActionType: "project.deleted", ResourceType: "project", ResourceID: projectID,
			Actor: "user", Metadata: map[string]any{"name": project.Name, "slug": project.Slug},
		}); err != nil {
			deps.Logger.Error().Err(err).Msg("recording project.deleted audit event failed")
		}

		w.WriteHeader(http.StatusNoContent)
	}
}

func handleUpdateProject(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Name string `json:"name"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_json"})
			return
		}
		if err := projects.ValidateName(body.Name); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name_required"})
			return
		}

		project, err := deps.Projects.UpdateName(r.Context(), chi.URLParam(r, "projectID"), body.Name)
		if errors.Is(err, projects.ErrNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "project_not_found"})
			return
		}
		if err != nil {
			deps.Logger.Error().Err(err).Msg("updating project failed")
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal_error"})
			return
		}

		writeJSON(w, http.StatusOK, toProjectResponse(project))
	}
}

// handleUpdateGitHubCI configures (or clears) internal/githubci for a
// project: which "owner/repo" to poll, and the PAT to poll/report with.
// The token is write-only — GET/PATCH responses only ever say whether
// one is configured (GitHubCIConfigured), never the value itself, same
// as handleCreateProvider/handlePatchProvider's api_key handling. An
// empty github_token in the request body leaves whatever token is
// already stored untouched; send github_repo: "" to disable the
// integration for this project without discarding a stored token (it
// simply won't be used while github_repo is empty).
func handleUpdateGitHubCI(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		projectID := chi.URLParam(r, "projectID")

		var body struct {
			GitHubRepo  *string `json:"github_repo"`
			GitHubToken string  `json:"github_token"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_json"})
			return
		}

		existing, err := deps.Projects.Get(r.Context(), projectID)
		if errors.Is(err, projects.ErrNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "project_not_found"})
			return
		}
		if err != nil {
			deps.Logger.Error().Err(err).Msg("getting project for github-ci update failed")
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal_error"})
			return
		}

		githubRepo := existing.GitHubRepo
		if body.GitHubRepo != nil {
			githubRepo = *body.GitHubRepo
		}

		secretReferenceID := existing.GitHubTokenSecretReferenceID
		if body.GitHubToken != "" {
			if deps.Secrets == nil {
				writeJSON(w, http.StatusServiceUnavailable, map[string]string{
					"error":  "secret_encryption_not_configured",
					"detail": "SENTINEL_SECRET_ENCRYPTION_KEY must be set before a GitHub token can be stored",
				})
				return
			}
			id, err := deps.Secrets.Create(r.Context(), body.GitHubToken)
			if err != nil {
				deps.Logger.Error().Err(err).Msg("encrypting github token failed")
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal_error"})
				return
			}
			secretReferenceID = id
		}

		project, err := deps.Projects.SetGitHubCI(r.Context(), projectID, githubRepo, secretReferenceID)
		if errors.Is(err, projects.ErrNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "project_not_found"})
			return
		}
		if err != nil {
			deps.Logger.Error().Err(err).Msg("updating github-ci config failed")
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal_error"})
			return
		}

		if err := deps.Audit.Record(r.Context(), audit.Event{
			ActionType: "project.github_ci_updated", ResourceType: "project", ResourceID: project.ID,
			Actor: "user", Metadata: map[string]any{"github_repo": githubRepo, "github_ci_configured": secretReferenceID != ""},
		}); err != nil {
			deps.Logger.Error().Err(err).Msg("recording project.github_ci_updated audit event failed")
		}

		writeJSON(w, http.StatusOK, toProjectResponse(project))
	}
}

func handleUpdateVisualDiffThreshold(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		projectID := chi.URLParam(r, "projectID")

		var body struct {
			Threshold float64 `json:"threshold"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_json"})
			return
		}
		if body.Threshold < 0 || body.Threshold > visualdiff.MaxColorDistance {
			writeJSON(w, http.StatusBadRequest, map[string]string{
				"error":  "invalid_threshold",
				"detail": fmt.Sprintf("threshold must be between 0 and %v", visualdiff.MaxColorDistance),
			})
			return
		}

		project, err := deps.Projects.SetVisualDiffThreshold(r.Context(), projectID, body.Threshold)
		if errors.Is(err, projects.ErrNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "project_not_found"})
			return
		}
		if err != nil {
			deps.Logger.Error().Err(err).Msg("updating visual-diff threshold failed")
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal_error"})
			return
		}

		if err := deps.Audit.Record(r.Context(), audit.Event{
			ActionType: "project.visual_diff_threshold_updated", ResourceType: "project", ResourceID: project.ID,
			Actor: "user", Metadata: map[string]any{"threshold": body.Threshold},
		}); err != nil {
			deps.Logger.Error().Err(err).Msg("recording project.visual_diff_threshold_updated audit event failed")
		}

		writeJSON(w, http.StatusOK, toProjectResponse(project))
	}
}
