package httpserver

import (
	"encoding/json"
	"net/http"
	"testing"

	"e2e-sentinel/apps/api/internal/secretstore"
)

func TestUpdateGitHubCI_RequiresSecretsConfiguredForAToken(t *testing.T) {
	deps := newTestDeps(nil, nil) // Secrets is nil by default
	router := NewRouter(deps)
	projectID := createTestProject(t, router)

	rec := doJSON(t, router, http.MethodPatch, "/api/v1/projects/"+projectID+"/github-ci", map[string]string{
		"github_repo": "acme/widget", "github_token": "shh",
	})
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503 (no secret encryption configured), body=%s", rec.Code, rec.Body.String())
	}
}

func TestUpdateGitHubCI_SetsRepoWithoutRequiringAToken(t *testing.T) {
	deps := newTestDeps(nil, nil)
	router := NewRouter(deps)
	projectID := createTestProject(t, router)

	rec := doJSON(t, router, http.MethodPatch, "/api/v1/projects/"+projectID+"/github-ci", map[string]string{
		"github_repo": "acme/widget",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", rec.Code, rec.Body.String())
	}

	var body struct {
		GitHubRepo         string `json:"github_repo"`
		GitHubCIConfigured bool   `json:"github_ci_configured"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decoding response: %v", err)
	}
	if body.GitHubRepo != "acme/widget" {
		t.Errorf("github_repo = %q, want acme/widget", body.GitHubRepo)
	}
	if body.GitHubCIConfigured {
		t.Error("github_ci_configured = true, want false (no token set yet)")
	}
}

func TestUpdateGitHubCI_StoresTokenAndNeverReturnsIt(t *testing.T) {
	deps := newTestDeps(nil, nil)
	enc, err := secretstore.NewEncryptor(make([]byte, secretstore.KeySize))
	if err != nil {
		t.Fatalf("NewEncryptor() error: %v", err)
	}
	deps.Secrets = secretstore.NewMemoryStore(enc)
	router := NewRouter(deps)
	projectID := createTestProject(t, router)

	rec := doJSON(t, router, http.MethodPatch, "/api/v1/projects/"+projectID+"/github-ci", map[string]string{
		"github_repo": "acme/widget", "github_token": "shh",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", rec.Code, rec.Body.String())
	}
	if bytesContain(rec.Body.Bytes(), []byte("shh")) {
		t.Fatalf("response body leaked the raw token: %s", rec.Body.String())
	}

	var body struct {
		GitHubCIConfigured bool `json:"github_ci_configured"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decoding response: %v", err)
	}
	if !body.GitHubCIConfigured {
		t.Error("github_ci_configured = false, want true")
	}

	// GET /projects/{id} must also never leak the token, only the
	// has_api_key-style boolean.
	getRec := doJSON(t, router, http.MethodGet, "/api/v1/projects/"+projectID, nil)
	if bytesContain(getRec.Body.Bytes(), []byte("shh")) {
		t.Fatalf("GET project response leaked the raw token: %s", getRec.Body.String())
	}
}

func TestUpdateGitHubCI_ClearingRepoDisablesWithoutDiscardingToken(t *testing.T) {
	deps := newTestDeps(nil, nil)
	enc, err := secretstore.NewEncryptor(make([]byte, secretstore.KeySize))
	if err != nil {
		t.Fatalf("NewEncryptor() error: %v", err)
	}
	deps.Secrets = secretstore.NewMemoryStore(enc)
	router := NewRouter(deps)
	projectID := createTestProject(t, router)

	doJSON(t, router, http.MethodPatch, "/api/v1/projects/"+projectID+"/github-ci", map[string]string{
		"github_repo": "acme/widget", "github_token": "shh",
	})

	rec := doJSON(t, router, http.MethodPatch, "/api/v1/projects/"+projectID+"/github-ci", map[string]string{
		"github_repo": "",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", rec.Code, rec.Body.String())
	}

	var body struct {
		GitHubRepo         string `json:"github_repo"`
		GitHubCIConfigured bool   `json:"github_ci_configured"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decoding response: %v", err)
	}
	if body.GitHubRepo != "" {
		t.Errorf("github_repo = %q, want empty after clearing", body.GitHubRepo)
	}
	if !body.GitHubCIConfigured {
		t.Error("github_ci_configured = false, want true (clearing the repo shouldn't discard the stored token)")
	}
}

func TestUpdateGitHubCI_ProjectNotFound(t *testing.T) {
	router := NewRouter(newTestDeps(nil, nil))
	rec := doJSON(t, router, http.MethodPatch, "/api/v1/projects/does-not-exist/github-ci", map[string]string{
		"github_repo": "acme/widget",
	})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404, body=%s", rec.Code, rec.Body.String())
	}
}

func bytesContain(haystack, needle []byte) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if string(haystack[i:i+len(needle)]) == string(needle) {
			return true
		}
	}
	return false
}
