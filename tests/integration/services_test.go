package integration

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type discoveredServiceJSON struct {
	Name       string         `json:"name"`
	Kind       string         `json:"kind"`
	Image      string         `json:"image"`
	Confidence string         `json:"confidence"`
	Metadata   map[string]any `json:"metadata"`
}

// TestServicesDiscovery_FullFlow validates Docker Compose service
// discovery against the live stack. docker-compose.yml does not mount
// the Docker socket by default (see docs/DOCKER_DISCOVERY.md), so
// services are expected to be recorded with status "unknown" here —
// that's the documented, graceful default, not a failure.
func TestServicesDiscovery_FullFlow(t *testing.T) {
	base := baseURL(t)

	hostDir, err := filepath.Abs(filepath.Join("..", "..", "workspace", "it-services-fixture"))
	if err != nil {
		t.Fatalf("resolving workspace fixture path: %v", err)
	}
	if err := os.RemoveAll(hostDir); err != nil {
		t.Fatalf("cleaning up previous fixture: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(hostDir) })

	writeFixtureFile(t, hostDir, "docker-compose.yml", `
services:
  api:
    build: .
    ports:
      - "8080:8080"
    environment:
      - DATABASE_URL=postgres://should-not-leak@db/app
  postgres:
    image: postgres:16-alpine
`)

	var project struct {
		ID string `json:"id"`
	}
	createRes := postJSON(t, base+"/api/v1/projects", map[string]string{
		"name": "IT Services Fixture", "repository_path": "/workspace/it-services-fixture",
	}, &project)
	if createRes.StatusCode != http.StatusCreated {
		t.Fatalf("create project status = %d, want 201", createRes.StatusCode)
	}
	t.Cleanup(func() { deleteProject(t, base, project.ID) })

	if res := postJSON(t, base+"/api/v1/projects/"+project.ID+"/discover", struct{}{}, nil); res.StatusCode != http.StatusOK {
		t.Fatalf("discover status = %d, want 200", res.StatusCode)
	}

	client := &http.Client{}
	res, err := client.Get(base + "/api/v1/projects/" + project.ID + "/services")
	if err != nil {
		t.Fatalf("GET services: %v", err)
	}
	defer res.Body.Close()

	var body struct {
		Services []discoveredServiceJSON `json:"services"`
	}
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		t.Fatalf("decoding services response: %v", err)
	}

	byName := map[string]discoveredServiceJSON{}
	for _, s := range body.Services {
		byName[s.Name] = s
	}

	postgres, ok := byName["postgres"]
	if !ok {
		t.Fatalf("expected a postgres service, got %+v", body.Services)
	}
	if postgres.Kind != "database" || postgres.Confidence != "high" {
		t.Errorf("postgres = %+v, want kind=database confidence=high", postgres)
	}

	api, ok := byName["api"]
	if !ok {
		t.Fatalf("expected an api service, got %+v", body.Services)
	}
	if api.Metadata["status"] != "unknown" {
		t.Errorf("api status = %v, want unknown (Docker socket is not mounted in this stack)", api.Metadata["status"])
	}

	// The env var value must never appear anywhere in the response.
	raw, _ := json.Marshal(body)
	if strings.Contains(string(raw), "should-not-leak") {
		t.Fatal("environment variable VALUE leaked into the services API response")
	}
}
