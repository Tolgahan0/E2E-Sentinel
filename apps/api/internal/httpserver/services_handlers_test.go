package httpserver

import (
	"context"
	"encoding/json"
	"net/http"
	"path/filepath"
	"testing"

	"e2e-sentinel/apps/api/internal/dockerclient"
)

type fakeDockerLister struct {
	pingErr    error
	containers []dockerclient.Container
}

func (f fakeDockerLister) Ping(context.Context) error { return f.pingErr }
func (f fakeDockerLister) ListContainers(context.Context) ([]dockerclient.Container, error) {
	return f.containers, nil
}

func TestDiscoverProject_ParsesComposeServices(t *testing.T) {
	deps := newTestDeps(nil, nil)
	router := NewRouter(deps)

	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "docker-compose.yml"), `
services:
  api:
    build: .
    ports:
      - "8080:8080"
  postgres:
    image: postgres:16-alpine
`)

	createRec := doJSON(t, router, http.MethodPost, "/api/v1/projects", map[string]string{"name": "Fixture", "repository_path": dir})
	var project struct {
		ID string `json:"id"`
	}
	json.Unmarshal(createRec.Body.Bytes(), &project)

	discoverRec := doJSON(t, router, http.MethodPost, "/api/v1/projects/"+project.ID+"/discover", nil)
	if discoverRec.Code != http.StatusOK {
		t.Fatalf("discover status = %d, body=%s", discoverRec.Code, discoverRec.Body.String())
	}

	servicesRec := doJSON(t, router, http.MethodGet, "/api/v1/projects/"+project.ID+"/services", nil)
	var body struct {
		Services []struct {
			Name       string `json:"name"`
			Kind       string `json:"kind"`
			Confidence string `json:"confidence"`
		} `json:"services"`
	}
	json.Unmarshal(servicesRec.Body.Bytes(), &body)

	if len(body.Services) != 2 {
		t.Fatalf("expected 2 services, got %+v", body.Services)
	}

	byName := map[string]string{}
	for _, s := range body.Services {
		byName[s.Name] = s.Kind
	}
	if byName["postgres"] != "database" {
		t.Errorf("postgres kind = %q, want database", byName["postgres"])
	}
	if byName["api"] != "api" {
		t.Errorf("api kind = %q, want api (has a published port)", byName["api"])
	}
}

func TestDiscoverProject_EnrichesWithRunningContainerStatus(t *testing.T) {
	deps := newTestDeps(nil, nil)
	deps.Docker = fakeDockerLister{
		containers: []dockerclient.Container{
			{
				Names:  []string{"/fixture-api-1"},
				State:  "running",
				Status: "Up 2 minutes (healthy)",
				Labels: map[string]string{dockerclient.LabelComposeService: "api"},
				Ports:  []dockerclient.ContainerPort{{PrivatePort: 8080, PublicPort: 8080, Type: "tcp"}},
			},
		},
	}
	router := NewRouter(deps)

	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "docker-compose.yml"), `
services:
  api:
    build: .
    ports:
      - "8080:8080"
`)

	createRec := doJSON(t, router, http.MethodPost, "/api/v1/projects", map[string]string{"name": "Fixture", "repository_path": dir})
	var project struct {
		ID string `json:"id"`
	}
	json.Unmarshal(createRec.Body.Bytes(), &project)

	doJSON(t, router, http.MethodPost, "/api/v1/projects/"+project.ID+"/discover", nil)

	servicesRec := doJSON(t, router, http.MethodGet, "/api/v1/projects/"+project.ID+"/services", nil)
	var body struct {
		Services []struct {
			Name          string         `json:"name"`
			ContainerName string         `json:"container_name"`
			Metadata      map[string]any `json:"metadata"`
		} `json:"services"`
	}
	json.Unmarshal(servicesRec.Body.Bytes(), &body)

	if len(body.Services) != 1 {
		t.Fatalf("expected 1 service, got %+v", body.Services)
	}
	svc := body.Services[0]
	if svc.ContainerName != "fixture-api-1" {
		t.Errorf("ContainerName = %q, want fixture-api-1", svc.ContainerName)
	}
	if svc.Metadata["status"] != "running" {
		t.Errorf("Metadata[status] = %v, want running", svc.Metadata["status"])
	}
}

func TestDiscoverProject_DockerUnavailableDegradesGracefully(t *testing.T) {
	deps := newTestDeps(nil, nil)
	deps.Docker = fakeDockerLister{pingErr: dockerclient.ErrUnavailable}
	router := NewRouter(deps)

	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "docker-compose.yml"), "services:\n  api:\n    image: example/api\n")

	createRec := doJSON(t, router, http.MethodPost, "/api/v1/projects", map[string]string{"name": "Fixture", "repository_path": dir})
	var project struct {
		ID string `json:"id"`
	}
	json.Unmarshal(createRec.Body.Bytes(), &project)

	discoverRec := doJSON(t, router, http.MethodPost, "/api/v1/projects/"+project.ID+"/discover", nil)
	if discoverRec.Code != http.StatusOK {
		t.Fatalf("discover status = %d, want 200 even when Docker is unavailable, body=%s", discoverRec.Code, discoverRec.Body.String())
	}

	servicesRec := doJSON(t, router, http.MethodGet, "/api/v1/projects/"+project.ID+"/services", nil)
	var body struct {
		Services []struct {
			Metadata map[string]any `json:"metadata"`
		} `json:"services"`
	}
	json.Unmarshal(servicesRec.Body.Bytes(), &body)
	if len(body.Services) != 1 {
		t.Fatalf("expected the compose-declared service to still be recorded, got %+v", body.Services)
	}
	if body.Services[0].Metadata["status"] != "unknown" {
		t.Errorf("Metadata[status] = %v, want unknown (docker unreachable, not observed)", body.Services[0].Metadata["status"])
	}
}

func TestDiscoverProject_NoComposeFileYieldsNoServices(t *testing.T) {
	deps := newTestDeps(nil, nil)
	router := NewRouter(deps)

	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "go.mod"), "module x\n")

	createRec := doJSON(t, router, http.MethodPost, "/api/v1/projects", map[string]string{"name": "Fixture", "repository_path": dir})
	var project struct {
		ID string `json:"id"`
	}
	json.Unmarshal(createRec.Body.Bytes(), &project)

	doJSON(t, router, http.MethodPost, "/api/v1/projects/"+project.ID+"/discover", nil)

	servicesRec := doJSON(t, router, http.MethodGet, "/api/v1/projects/"+project.ID+"/services", nil)
	var body struct {
		Services []any `json:"services"`
	}
	json.Unmarshal(servicesRec.Body.Bytes(), &body)
	if len(body.Services) != 0 {
		t.Errorf("expected no services without a compose file, got %+v", body.Services)
	}
}
