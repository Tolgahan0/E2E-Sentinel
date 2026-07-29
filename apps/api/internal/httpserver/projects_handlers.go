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
