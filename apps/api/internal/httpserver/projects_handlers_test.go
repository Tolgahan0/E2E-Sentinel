package httpserver

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func doJSON(t *testing.T, router http.Handler, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatalf("encoding request body: %v", err)
		}
	}
	req := httptest.NewRequest(method, path, &buf)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func TestCreateProject_RejectsMissingName(t *testing.T) {
	router := NewRouter(newTestDeps(nil, nil))
	rec := doJSON(t, router, http.MethodPost, "/api/v1/projects", map[string]string{"repository_path": t.TempDir()})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestCreateProject_RejectsInvalidRepositoryPath(t *testing.T) {
	router := NewRouter(newTestDeps(nil, nil))
	rec := doJSON(t, router, http.MethodPost, "/api/v1/projects", map[string]string{
		"name": "Routa", "repository_path": "/this/path/does/not/exist",
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestCreateProject_RejectsSystemRoot(t *testing.T) {
	router := NewRouter(newTestDeps(nil, nil))
	rec := doJSON(t, router, http.MethodPost, "/api/v1/projects", map[string]string{
		"name": "Everything", "repository_path": "/etc",
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (dangerous root must be rejected)", rec.Code)
	}
}

func TestCreateProject_SucceedsAndCreatesDefaultEnvironment(t *testing.T) {
	deps := newTestDeps(nil, nil)
	router := NewRouter(deps)
	dir := t.TempDir()

	rec := doJSON(t, router, http.MethodPost, "/api/v1/projects", map[string]string{
		"name": "Routa", "repository_path": dir,
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201, body=%s", rec.Code, rec.Body.String())
	}

	var project struct {
		ID              string `json:"id"`
		Slug            string `json:"slug"`
		DiscoveryStatus string `json:"discovery_status"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &project); err != nil {
		t.Fatalf("decoding response: %v", err)
	}
	if project.Slug != "routa" {
		t.Errorf("slug = %q, want routa", project.Slug)
	}
	if project.DiscoveryStatus != "never_run" {
		t.Errorf("discovery_status = %q, want never_run", project.DiscoveryStatus)
	}

	envRec := doJSON(t, router, http.MethodGet, "/api/v1/projects/"+project.ID+"/environments", nil)
	var envBody struct {
		Environments []struct {
			Classification string `json:"classification"`
			IsProduction   bool   `json:"is_production"`
		} `json:"environments"`
	}
	if err := json.Unmarshal(envRec.Body.Bytes(), &envBody); err != nil {
		t.Fatalf("decoding environments response: %v", err)
	}
	if len(envBody.Environments) != 1 {
		t.Fatalf("expected exactly one default environment, got %d", len(envBody.Environments))
	}
	if envBody.Environments[0].Classification != "local" || envBody.Environments[0].IsProduction {
		t.Errorf("default environment = %+v, want classification=local, is_production=false", envBody.Environments[0])
	}
}

func TestCreateProject_DuplicateNameGetsSuffixedSlug(t *testing.T) {
	deps := newTestDeps(nil, nil)
	router := NewRouter(deps)

	rec1 := doJSON(t, router, http.MethodPost, "/api/v1/projects", map[string]string{"name": "Routa", "repository_path": t.TempDir()})
	rec2 := doJSON(t, router, http.MethodPost, "/api/v1/projects", map[string]string{"name": "Routa", "repository_path": t.TempDir()})

	var p1, p2 struct {
		Slug string `json:"slug"`
	}
	json.Unmarshal(rec1.Body.Bytes(), &p1)
	json.Unmarshal(rec2.Body.Bytes(), &p2)

	if p1.Slug == p2.Slug {
		t.Errorf("expected distinct slugs for two projects named Routa, got %q and %q", p1.Slug, p2.Slug)
	}
}

func TestGetProject_NotFound(t *testing.T) {
	router := NewRouter(newTestDeps(nil, nil))
	rec := doJSON(t, router, http.MethodGet, "/api/v1/projects/does-not-exist", nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestDiscoverProject_FullFlow(t *testing.T) {
	deps := newTestDeps(nil, nil)
	router := NewRouter(deps)

	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "package.json"), `{"dependencies":{"next":"14.0.0"}}`)
	mustWrite(t, filepath.Join(dir, "go.mod"), "module x\n")
	mustWrite(t, filepath.Join(dir, "Dockerfile"), "FROM alpine")

	createRec := doJSON(t, router, http.MethodPost, "/api/v1/projects", map[string]string{"name": "Fixture", "repository_path": dir})
	var project struct {
		ID string `json:"id"`
	}
	json.Unmarshal(createRec.Body.Bytes(), &project)

	discoverRec := doJSON(t, router, http.MethodPost, "/api/v1/projects/"+project.ID+"/discover", nil)
	if discoverRec.Code != http.StatusOK {
		t.Fatalf("discover status = %d, want 200, body=%s", discoverRec.Code, discoverRec.Body.String())
	}

	var discoverBody struct {
		Findings []struct {
			Category string `json:"category"`
			Name     string `json:"name"`
		} `json:"findings"`
	}
	json.Unmarshal(discoverRec.Body.Bytes(), &discoverBody)
	if len(discoverBody.Findings) < 3 {
		t.Fatalf("expected at least 3 findings (node, go, docker), got %+v", discoverBody.Findings)
	}

	// GET discovery must reflect the same latest completed run.
	getRec := doJSON(t, router, http.MethodGet, "/api/v1/projects/"+project.ID+"/discovery", nil)
	if getRec.Code != http.StatusOK {
		t.Fatalf("get discovery status = %d, want 200", getRec.Code)
	}

	// The project's discovery_status must now read "completed".
	projectRec := doJSON(t, router, http.MethodGet, "/api/v1/projects/"+project.ID, nil)
	var updated struct {
		DiscoveryStatus string `json:"discovery_status"`
	}
	json.Unmarshal(projectRec.Body.Bytes(), &updated)
	if updated.DiscoveryStatus != "completed" {
		t.Errorf("discovery_status = %q, want completed", updated.DiscoveryStatus)
	}
}

func TestGetDiscovery_NoRunYet(t *testing.T) {
	deps := newTestDeps(nil, nil)
	router := NewRouter(deps)

	createRec := doJSON(t, router, http.MethodPost, "/api/v1/projects", map[string]string{"name": "Fixture", "repository_path": t.TempDir()})
	var project struct {
		ID string `json:"id"`
	}
	json.Unmarshal(createRec.Body.Bytes(), &project)

	rec := doJSON(t, router, http.MethodGet, "/api/v1/projects/"+project.ID+"/discovery", nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 before any discovery run", rec.Code)
	}
}

func TestUpdateEnvironment_ProductionForcesFlagsOff(t *testing.T) {
	deps := newTestDeps(nil, nil)
	router := NewRouter(deps)

	createRec := doJSON(t, router, http.MethodPost, "/api/v1/projects", map[string]string{"name": "Fixture", "repository_path": t.TempDir()})
	var project struct {
		ID string `json:"id"`
	}
	json.Unmarshal(createRec.Body.Bytes(), &project)

	envListRec := doJSON(t, router, http.MethodGet, "/api/v1/projects/"+project.ID+"/environments", nil)
	var envList struct {
		Environments []struct {
			ID string `json:"id"`
		} `json:"environments"`
	}
	json.Unmarshal(envListRec.Body.Bytes(), &envList)
	envID := envList.Environments[0].ID

	rec := doJSON(t, router, http.MethodPatch, "/api/v1/environments/"+envID, map[string]string{"classification": "production"})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", rec.Code, rec.Body.String())
	}
	var updated struct {
		IsProduction   bool `json:"is_production"`
		AllowMutations bool `json:"allow_mutations"`
	}
	json.Unmarshal(rec.Body.Bytes(), &updated)
	if !updated.IsProduction || updated.AllowMutations {
		t.Errorf("updated environment = %+v, want is_production=true, allow_mutations=false", updated)
	}
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("writing fixture %q: %v", path, err)
	}
}
