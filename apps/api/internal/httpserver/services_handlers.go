package httpserver

import (
	"context"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog"

	"e2e-sentinel/apps/api/internal/compose"
	"e2e-sentinel/apps/api/internal/discovery"
	"e2e-sentinel/apps/api/internal/dockerclient"
	"e2e-sentinel/apps/api/internal/services"
)

// DockerLister is the subset of *dockerclient.Client used here, so tests
// can substitute a fake without a real Docker socket.
type DockerLister interface {
	Ping(ctx context.Context) error
	ListContainers(ctx context.Context) ([]dockerclient.Container, error)
}

type serviceResponse struct {
	ID            string         `json:"id"`
	Name          string         `json:"name"`
	Kind          string         `json:"kind"`
	Runtime       string         `json:"runtime"`
	SourcePath    string         `json:"source_path"`
	ContainerName string         `json:"container_name"`
	Image         string         `json:"image"`
	Ports         []string       `json:"ports"`
	Dependencies  []string       `json:"dependencies"`
	Metadata      map[string]any `json:"metadata"`
	Confidence    string         `json:"confidence"`
}

func toServiceResponse(s services.Service) serviceResponse {
	return serviceResponse{
		ID: s.ID, Name: s.Name, Kind: s.Kind, Runtime: s.Runtime, SourcePath: s.SourcePath,
		ContainerName: s.ContainerName, Image: s.Image, Ports: s.Ports, Dependencies: s.Dependencies,
		Metadata: s.Metadata, Confidence: s.ConfidenceLevel,
	}
}

func handleListServices(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		list, err := deps.Services.ListByProject(r.Context(), chi.URLParam(r, "projectID"))
		if err != nil {
			deps.Logger.Error().Err(err).Msg("listing services failed")
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal_error"})
			return
		}
		out := make([]serviceResponse, 0, len(list))
		for _, s := range list {
			out = append(out, toServiceResponse(s))
		}
		writeJSON(w, http.StatusOK, map[string]any{"services": out})
	}
}

// evidencePaths extracts the "paths" evidence array from a finding,
// tolerating both the in-process []string form (fresh from Scan) and
// the []any form JSON round-trips produce.
func evidencePaths(f discovery.Finding) []string {
	switch paths := f.Evidence["paths"].(type) {
	case []string:
		return paths
	case []any:
		out := make([]string, 0, len(paths))
		for _, p := range paths {
			if s, ok := p.(string); ok {
				out = append(out, s)
			}
		}
		return out
	}
	return nil
}

// discoverServices parses every docker-compose file found by the
// repository scan and, if the Docker daemon is reachable, enriches each
// declared service with its live container status. It never fails the
// caller's request: a parse error or an unreachable daemon just means
// less enrichment, per spec §25 Phase 2 ("Docker-unavailable state is
// handled gracefully").
func discoverServices(ctx context.Context, repositoryPath string, findings []discovery.Finding, docker DockerLister, logger zerolog.Logger) []services.Service {
	var composeRelPaths []string
	for _, f := range findings {
		if f.Category == discovery.CategoryDocker && f.Name == "docker_compose" {
			composeRelPaths = append(composeRelPaths, evidencePaths(f)...)
		}
	}

	var result []services.Service
	for _, relPath := range composeRelPaths {
		parsed, err := compose.ParseFile(filepath.Join(repositoryPath, relPath))
		if err != nil {
			logger.Warn().Err(err).Str("path", relPath).Msg("parsing compose file failed; skipping")
			continue
		}
		for _, svc := range parsed {
			result = append(result, services.FromCompose("", relPath, svc))
		}
	}

	if docker == nil || len(result) == 0 {
		return result
	}
	if err := docker.Ping(ctx); err != nil {
		logger.Info().Msg("docker daemon unavailable; services will show as not observed")
		return result
	}

	containers, err := docker.ListContainers(ctx)
	if err != nil {
		logger.Warn().Err(err).Msg("listing docker containers failed")
		return result
	}

	byServiceName := map[string]dockerclient.Container{}
	for _, c := range containers {
		if name := c.Labels[dockerclient.LabelComposeService]; name != "" {
			byServiceName[name] = c
		}
	}

	for i, svc := range result {
		container, ok := byServiceName[svc.Name]
		if !ok {
			continue
		}
		result[i] = services.ApplyRuntimeStatus(svc, containerDisplayName(container), container.State, container.Status, formatPorts(container.Ports))
	}
	return result
}

func containerDisplayName(c dockerclient.Container) string {
	if len(c.Names) == 0 {
		return c.ID
	}
	return strings.TrimPrefix(c.Names[0], "/")
}

func formatPorts(ports []dockerclient.ContainerPort) []string {
	out := make([]string, 0, len(ports))
	for _, p := range ports {
		if p.PublicPort != 0 {
			out = append(out, fmt.Sprintf("%d->%d/%s", p.PublicPort, p.PrivatePort, p.Type))
		} else {
			out = append(out, fmt.Sprintf("%d/%s", p.PrivatePort, p.Type))
		}
	}
	return out
}
