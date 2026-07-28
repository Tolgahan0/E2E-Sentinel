package httpserver

import (
	"encoding/json"
	"net/http"
	"path/filepath"
	"testing"
)

func TestDiscoverProject_BuildsApplicationGraph(t *testing.T) {
	deps := newTestDeps(nil, nil)
	router := NewRouter(deps)

	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "app", "login", "page.tsx"), `
export default function LoginPage() {
  fetch('/api/v1/auth/login', { method: 'POST' });
  return null;
}
`)
	mustWrite(t, filepath.Join(dir, "app", "api", "v1", "auth", "login", "route.ts"), `
export async function POST(req) { return Response.json({}) }
`)
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

	discoverRec := doJSON(t, router, http.MethodPost, "/api/v1/projects/"+project.ID+"/discover", nil)
	if discoverRec.Code != http.StatusOK {
		t.Fatalf("discover status = %d, body=%s", discoverRec.Code, discoverRec.Body.String())
	}

	graphRec := doJSON(t, router, http.MethodGet, "/api/v1/projects/"+project.ID+"/graph", nil)
	if graphRec.Code != http.StatusOK {
		t.Fatalf("graph status = %d, body=%s", graphRec.Code, graphRec.Body.String())
	}

	var body struct {
		Nodes []struct {
			NodeType string `json:"node_type"`
			Label    string `json:"label"`
		} `json:"nodes"`
		Edges []struct {
			SourceLabel  string         `json:"source_label"`
			TargetLabel  string         `json:"target_label"`
			RelationType string         `json:"relation_type"`
			Confidence   string         `json:"confidence"`
			Evidence     map[string]any `json:"evidence"`
		} `json:"edges"`
	}
	if err := json.Unmarshal(graphRec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decoding graph response: %v", err)
	}

	if len(body.Nodes) == 0 {
		t.Fatal("expected at least one node in the application graph")
	}

	foundCallsEdge := false
	for _, e := range body.Edges {
		if e.RelationType == "calls" && e.SourceLabel == "/login" && e.TargetLabel == "POST /api/v1/auth/login" {
			foundCallsEdge = true
			if len(e.Evidence) == 0 {
				t.Error("calls edge has no evidence, but spec requires every edge to show evidence")
			}
			if e.Confidence == "" {
				t.Error("calls edge has no confidence level")
			}
		}
	}
	if !foundCallsEdge {
		t.Errorf("expected a 'Login Page -> POST /api/v1/auth/login' calls edge (spec §8 example), got edges=%+v", body.Edges)
	}

	// GET /graph must be idempotent-safe across repeated discovery: run
	// discover again and confirm the graph doesn't duplicate.
	doJSON(t, router, http.MethodPost, "/api/v1/projects/"+project.ID+"/discover", nil)
	graphRec2 := doJSON(t, router, http.MethodGet, "/api/v1/projects/"+project.ID+"/graph", nil)
	var body2 struct {
		Nodes []any `json:"nodes"`
	}
	json.Unmarshal(graphRec2.Body.Bytes(), &body2)
	if len(body2.Nodes) != len(body.Nodes) {
		t.Errorf("graph is not idempotent across repeated discovery: %d nodes then %d nodes", len(body.Nodes), len(body2.Nodes))
	}
}
