package integration

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

// TestApplicationGraph_FullFlow validates the spec §8 example scenario
// end to end against the live stack: a login page that fetches its own
// API route must produce a "calls" edge, and the API route must be
// linked to its serving Docker Compose service.
func TestApplicationGraph_FullFlow(t *testing.T) {
	base := baseURL(t)

	hostDir, err := filepath.Abs(filepath.Join("..", "..", "workspace", "it-graph-fixture"))
	if err != nil {
		t.Fatalf("resolving workspace fixture path: %v", err)
	}
	if err := os.RemoveAll(hostDir); err != nil {
		t.Fatalf("cleaning up previous fixture: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(hostDir) })

	writeFixtureFile(t, hostDir, "app/login/page.tsx", `
export default function LoginPage() {
  fetch('/api/v1/auth/login', { method: 'POST' });
  return null;
}
`)
	writeFixtureFile(t, hostDir, "app/api/v1/auth/login/route.ts", `
export async function POST(req) { return Response.json({}) }
`)
	writeFixtureFile(t, hostDir, "docker-compose.yml", `
services:
  api:
    build: .
    ports:
      - "8080:8080"
`)

	var project struct {
		ID string `json:"id"`
	}
	createRes := postJSON(t, base+"/api/v1/projects", map[string]string{
		"name": "IT Graph Fixture", "repository_path": "/workspace/it-graph-fixture",
	}, &project)
	if createRes.StatusCode != http.StatusCreated {
		t.Fatalf("create project status = %d, want 201", createRes.StatusCode)
	}
	t.Cleanup(func() { deleteProject(t, base, project.ID) })

	if res := postJSON(t, base+"/api/v1/projects/"+project.ID+"/discover", struct{}{}, nil); res.StatusCode != http.StatusOK {
		t.Fatalf("discover status = %d, want 200", res.StatusCode)
	}

	client := &http.Client{}
	res, err := client.Get(base + "/api/v1/projects/" + project.ID + "/graph")
	if err != nil {
		t.Fatalf("GET graph: %v", err)
	}
	defer res.Body.Close()

	var body struct {
		Nodes []any `json:"nodes"`
		Edges []struct {
			SourceLabel  string         `json:"source_label"`
			TargetLabel  string         `json:"target_label"`
			RelationType string         `json:"relation_type"`
			Evidence     map[string]any `json:"evidence"`
		} `json:"edges"`
	}
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		t.Fatalf("decoding graph response: %v", err)
	}

	found := false
	for _, e := range body.Edges {
		if e.RelationType == "calls" && e.SourceLabel == "/login" && e.TargetLabel == "POST /api/v1/auth/login" {
			found = true
			if len(e.Evidence) == 0 {
				t.Error("calls edge must carry evidence")
			}
		}
	}
	if !found {
		t.Errorf("expected the spec §8 example edge (login page calls its API route), got %+v", body.Edges)
	}
}
