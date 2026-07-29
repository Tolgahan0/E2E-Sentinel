package integration

import (
	"bytes"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func postJSON(t *testing.T, url string, body, out any) *http.Response {
	t.Helper()
	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(body); err != nil {
		t.Fatalf("encoding request body: %v", err)
	}
	client := &http.Client{Timeout: 10 * time.Second}
	res, err := client.Post(url, "application/json", &buf)
	if err != nil {
		t.Fatalf("POST %s: %v", url, err)
	}
	defer res.Body.Close()
	if out != nil {
		if err := json.NewDecoder(res.Body).Decode(out); err != nil {
			t.Fatalf("decoding response from %s: %v", url, err)
		}
	}
	return res
}

// deleteProject removes a project the test created — every test that
// creates one must call this via t.Cleanup, or it accumulates
// permanently in the database on every run of this suite (there being
// no other way to remove a project). Failures are logged, not fatal:
// cleanup best-effort must never mask the test's own result.
func deleteProject(t *testing.T, base, projectID string) {
	t.Helper()
	req, err := http.NewRequest(http.MethodDelete, base+"/api/v1/projects/"+projectID, nil)
	if err != nil {
		t.Logf("building DELETE request for project %s: %v", projectID, err)
		return
	}
	client := &http.Client{Timeout: 10 * time.Second}
	res, err := client.Do(req)
	if err != nil {
		t.Logf("deleting project %s: %v", projectID, err)
		return
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusNoContent {
		t.Logf("deleting project %s: status = %d, want 204", projectID, res.StatusCode)
	}
}

func patchJSON(t *testing.T, url string, body any) *http.Response {
	t.Helper()
	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(body); err != nil {
		t.Fatalf("encoding request body: %v", err)
	}
	req, err := http.NewRequest(http.MethodPatch, url, &buf)
	if err != nil {
		t.Fatalf("building PATCH request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 10 * time.Second}
	res, err := client.Do(req)
	if err != nil {
		t.Fatalf("PATCH %s: %v", url, err)
	}
	defer res.Body.Close()
	return res
}

// workspaceFixtureDir creates a fixture repository under this repo's
// ./workspace directory (host side), which docker-compose.yml mounts
// read-only into sentinel-api at /workspace — see
// docs/LOCAL_DEVELOPMENT.md#adding-a-project-repository-discovery. It
// returns the container-side path to pass as repository_path.
func workspaceFixtureDir(t *testing.T) string {
	t.Helper()

	hostDir, err := filepath.Abs(filepath.Join("..", "..", "workspace", "it-fixture"))
	if err != nil {
		t.Fatalf("resolving workspace fixture path: %v", err)
	}
	if err := os.RemoveAll(hostDir); err != nil {
		t.Fatalf("cleaning up previous fixture: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(hostDir) })

	writeFixtureFile(t, hostDir, "package.json", `{"dependencies":{"next":"14.2.0"}}`)
	writeFixtureFile(t, hostDir, "go.mod", "module example.com/it-fixture\n")
	writeFixtureFile(t, hostDir, "Dockerfile", "FROM alpine")
	writeFixtureFile(t, hostDir, "playwright.config.ts", "export default {}")

	return "/workspace/it-fixture"
}

func writeFixtureFile(t *testing.T, dir, name, content string) {
	t.Helper()
	full := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatalf("mkdir for fixture %q: %v", name, err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatalf("writing fixture %q: %v", name, err)
	}
}

// TestProjectDiscovery_FullFlow validates the documented workspace-mount
// pattern end to end: a repository placed under ./workspace on the host
// is reachable, at /workspace/<name>, by sentinel-api running inside its
// container.
func TestProjectDiscovery_FullFlow(t *testing.T) {
	base := baseURL(t)
	containerPath := workspaceFixtureDir(t)

	var project struct {
		ID              string `json:"id"`
		DiscoveryStatus string `json:"discovery_status"`
	}
	createRes := postJSON(t, base+"/api/v1/projects", map[string]string{
		"name": "IT Fixture", "repository_path": containerPath,
	}, &project)
	if createRes.StatusCode != http.StatusCreated {
		t.Fatalf("create project status = %d, want 201", createRes.StatusCode)
	}
	if project.ID == "" {
		t.Fatal("create project did not return an ID")
	}
	t.Cleanup(func() { deleteProject(t, base, project.ID) })

	var discovery struct {
		Findings []struct {
			Category string `json:"category"`
			Name     string `json:"name"`
		} `json:"findings"`
	}
	discoverRes := postJSON(t, base+"/api/v1/projects/"+project.ID+"/discover", struct{}{}, &discovery)
	if discoverRes.StatusCode != http.StatusOK {
		t.Fatalf("discover status = %d, want 200 (is ./workspace mounted into sentinel-api? see docker-compose.yml)", discoverRes.StatusCode)
	}

	want := map[string]bool{"language/node": false, "language/go": false, "docker/dockerfile": false, "test_tool/playwright": false}
	for _, f := range discovery.Findings {
		key := f.Category + "/" + f.Name
		if _, ok := want[key]; ok {
			want[key] = true
		}
	}
	for key, found := range want {
		if !found {
			t.Errorf("expected finding %q, got: %+v", key, discovery.Findings)
		}
	}
}
