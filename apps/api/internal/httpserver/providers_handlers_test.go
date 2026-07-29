package httpserver

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"e2e-sentinel/apps/api/internal/secretstore"
)

func depsWithSecrets(t *testing.T) Dependencies {
	t.Helper()
	enc, err := secretstore.NewEncryptor(make([]byte, secretstore.KeySize))
	if err != nil {
		t.Fatalf("NewEncryptor() error: %v", err)
	}
	deps := newTestDeps(nil, nil)
	deps.Secrets = secretstore.NewMemoryStore(enc)
	return deps
}

func TestCreateProvider_RejectsInvalidType(t *testing.T) {
	router := NewRouter(newTestDeps(nil, nil))
	rec := doJSON(t, router, http.MethodPost, "/api/v1/providers", map[string]any{"type": "not-real", "name": "x"})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestCreateProvider_RejectsMissingName(t *testing.T) {
	router := NewRouter(newTestDeps(nil, nil))
	rec := doJSON(t, router, http.MethodPost, "/api/v1/providers", map[string]any{"type": "ollama"})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestCreateProvider_LocalOllamaWithoutAPIKeySucceeds(t *testing.T) {
	router := NewRouter(newTestDeps(nil, nil))
	rec := doJSON(t, router, http.MethodPost, "/api/v1/providers", map[string]any{
		"type": "ollama", "name": "Local Ollama", "base_url": "http://host.docker.internal:11434",
		"is_local": true,
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}

	var body providerResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if body.HasAPIKey {
		t.Error("HasAPIKey = true, want false for a keyless local provider")
	}
	if !body.IsLocal {
		t.Error("IsLocal = false, want true")
	}
}

func TestCreateProvider_WithAPIKeyButNoEncryptionConfiguredReturns503(t *testing.T) {
	router := NewRouter(newTestDeps(nil, nil)) // Secrets is nil by default
	rec := doJSON(t, router, http.MethodPost, "/api/v1/providers", map[string]any{
		"type": "openai", "name": "OpenAI", "api_key": "sk-should-not-be-stored",
	})
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
}

func TestCreateProvider_ResponseNeverIncludesAPIKey(t *testing.T) {
	router := NewRouter(depsWithSecrets(t))
	rec := doJSON(t, router, http.MethodPost, "/api/v1/providers", map[string]any{
		"type": "openai", "name": "OpenAI", "model": "gpt-4", "api_key": "sk-super-secret-value",
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}

	if strings.Contains(rec.Body.String(), "sk-super-secret-value") {
		t.Fatalf("API key leaked into response body: %s", rec.Body.String())
	}

	var body providerResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if !body.HasAPIKey {
		t.Error("HasAPIKey = false, want true")
	}
}

func TestListProviders_NeverIncludesAPIKeys(t *testing.T) {
	deps := depsWithSecrets(t)
	router := NewRouter(deps)
	doJSON(t, router, http.MethodPost, "/api/v1/providers", map[string]any{
		"type": "anthropic", "name": "Claude", "api_key": "sk-ant-hidden-value",
	})

	rec := doJSON(t, router, http.MethodGet, "/api/v1/providers", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "sk-ant-hidden-value") {
		t.Fatalf("API key leaked into list response: %s", rec.Body.String())
	}
}

func TestPatchProvider_UpdatesFields(t *testing.T) {
	router := NewRouter(newTestDeps(nil, nil))
	createRec := doJSON(t, router, http.MethodPost, "/api/v1/providers", map[string]any{"type": "ollama", "name": "Local"})
	var created providerResponse
	json.Unmarshal(createRec.Body.Bytes(), &created)

	rec := doJSON(t, router, http.MethodPatch, "/api/v1/providers/"+created.ID, map[string]any{"model": "llama3", "enabled": false})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var updated providerResponse
	json.Unmarshal(rec.Body.Bytes(), &updated)
	if updated.Model != "llama3" {
		t.Errorf("Model = %q, want llama3", updated.Model)
	}
	if updated.Enabled {
		t.Error("Enabled = true, want false")
	}
}

func TestPatchProvider_RotateAndClearAPIKey(t *testing.T) {
	router := NewRouter(depsWithSecrets(t))
	createRec := doJSON(t, router, http.MethodPost, "/api/v1/providers", map[string]any{
		"type": "openai", "name": "OpenAI", "api_key": "sk-original",
	})
	var created providerResponse
	json.Unmarshal(createRec.Body.Bytes(), &created)

	rotateRec := doJSON(t, router, http.MethodPatch, "/api/v1/providers/"+created.ID, map[string]any{"api_key": "sk-rotated"})
	if rotateRec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rotateRec.Code, rotateRec.Body.String())
	}
	if strings.Contains(rotateRec.Body.String(), "sk-rotated") {
		t.Fatal("rotated API key leaked into response")
	}

	clearRec := doJSON(t, router, http.MethodPatch, "/api/v1/providers/"+created.ID, map[string]any{"clear_api_key": true})
	var cleared providerResponse
	json.Unmarshal(clearRec.Body.Bytes(), &cleared)
	if cleared.HasAPIKey {
		t.Error("HasAPIKey = true after clear_api_key, want false")
	}
}

func TestPatchProvider_NotFound(t *testing.T) {
	router := NewRouter(newTestDeps(nil, nil))
	rec := doJSON(t, router, http.MethodPatch, "/api/v1/providers/does-not-exist", map[string]any{"model": "x"})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestDeleteProvider_NotFound(t *testing.T) {
	router := NewRouter(newTestDeps(nil, nil))
	rec := doJSON(t, router, http.MethodDelete, "/api/v1/providers/does-not-exist", nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestDeleteProvider_Succeeds(t *testing.T) {
	router := NewRouter(newTestDeps(nil, nil))
	createRec := doJSON(t, router, http.MethodPost, "/api/v1/providers", map[string]any{
		"type": "ollama", "name": "Temp", "base_url": "http://host.docker.internal:11434", "is_local": true,
	})
	var provider providerResponse
	json.Unmarshal(createRec.Body.Bytes(), &provider)

	deleteRec := doJSON(t, router, http.MethodDelete, "/api/v1/providers/"+provider.ID, nil)
	if deleteRec.Code != http.StatusNoContent {
		t.Fatalf("delete status = %d, want 204, body=%s", deleteRec.Code, deleteRec.Body.String())
	}

	listRec := doJSON(t, router, http.MethodGet, "/api/v1/providers", nil)
	var list struct {
		Providers []providerResponse `json:"providers"`
	}
	json.Unmarshal(listRec.Body.Bytes(), &list)
	for _, p := range list.Providers {
		if p.ID == provider.ID {
			t.Fatalf("deleted provider %s still present in list", provider.ID)
		}
	}
}

func TestTestProviderConnection_UpdatesHealthStatus(t *testing.T) {
	fakeOllama := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer fakeOllama.Close()

	router := NewRouter(newTestDeps(nil, nil))
	createRec := doJSON(t, router, http.MethodPost, "/api/v1/providers", map[string]any{
		"type": "ollama", "name": "Local", "base_url": fakeOllama.URL, "is_local": true,
	})
	var created providerResponse
	json.Unmarshal(createRec.Body.Bytes(), &created)

	rec := doJSON(t, router, http.MethodPost, "/api/v1/providers/"+created.ID+"/test", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}

	var result struct {
		Status   string           `json:"status"`
		Provider providerResponse `json:"provider"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if result.Status != "ok" {
		t.Errorf("Status = %q, want ok", result.Status)
	}
	if result.Provider.HealthStatus != "ok" {
		t.Errorf("Provider.HealthStatus = %q, want ok", result.Provider.HealthStatus)
	}
	if result.Provider.LastCheckedAt == nil {
		t.Error("LastCheckedAt should be set after a test")
	}
}

func TestTestProviderConnection_UnreachableReportsError(t *testing.T) {
	router := NewRouter(newTestDeps(nil, nil))
	createRec := doJSON(t, router, http.MethodPost, "/api/v1/providers", map[string]any{
		"type": "ollama", "name": "Local", "base_url": "http://127.0.0.1:1",
	})
	var created providerResponse
	json.Unmarshal(createRec.Body.Bytes(), &created)

	rec := doJSON(t, router, http.MethodPost, "/api/v1/providers/"+created.ID+"/test", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var result struct {
		Status string `json:"status"`
	}
	json.Unmarshal(rec.Body.Bytes(), &result)
	if result.Status != "error" {
		t.Errorf("Status = %q, want error", result.Status)
	}
}

func TestTestProviderConnection_NotFound(t *testing.T) {
	router := NewRouter(newTestDeps(nil, nil))
	rec := doJSON(t, router, http.MethodPost, "/api/v1/providers/does-not-exist/test", nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestTaskRouting_EmptyByDefault(t *testing.T) {
	router := NewRouter(newTestDeps(nil, nil))
	rec := doJSON(t, router, http.MethodGet, "/api/v1/providers/routing", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		Routes map[string]string `json:"routes"`
	}
	json.Unmarshal(rec.Body.Bytes(), &body)
	if len(body.Routes) != 0 {
		t.Errorf("Routes = %v, want empty", body.Routes)
	}
}

func TestTaskRouting_SetGetAndClear(t *testing.T) {
	router := NewRouter(newTestDeps(nil, nil))
	createRec := doJSON(t, router, http.MethodPost, "/api/v1/providers", map[string]any{"type": "ollama", "name": "Local"})
	var provider providerResponse
	json.Unmarshal(createRec.Body.Bytes(), &provider)

	setRec := doJSON(t, router, http.MethodPatch, "/api/v1/providers/routing", map[string]any{
		"routes": map[string]string{"test_planning": provider.ID},
	})
	if setRec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", setRec.Code, setRec.Body.String())
	}

	getRec := doJSON(t, router, http.MethodGet, "/api/v1/providers/routing", nil)
	var got struct {
		Routes map[string]string `json:"routes"`
	}
	json.Unmarshal(getRec.Body.Bytes(), &got)
	if got.Routes["test_planning"] != provider.ID {
		t.Fatalf("Routes[test_planning] = %q, want %q", got.Routes["test_planning"], provider.ID)
	}

	clearRec := doJSON(t, router, http.MethodPatch, "/api/v1/providers/routing", map[string]any{
		"routes": map[string]string{"test_planning": ""},
	})
	if clearRec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", clearRec.Code, clearRec.Body.String())
	}
	var cleared struct {
		Routes map[string]string `json:"routes"`
	}
	json.Unmarshal(clearRec.Body.Bytes(), &cleared)
	if _, ok := cleared.Routes["test_planning"]; ok {
		t.Errorf("Routes still contains test_planning after clearing: %v", cleared.Routes)
	}
}

func TestTaskRouting_RejectsInvalidTaskType(t *testing.T) {
	router := NewRouter(newTestDeps(nil, nil))
	rec := doJSON(t, router, http.MethodPatch, "/api/v1/providers/routing", map[string]any{
		"routes": map[string]string{"not_a_real_task": "prov-1"},
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestTaskRouting_RejectsUnknownProvider(t *testing.T) {
	router := NewRouter(newTestDeps(nil, nil))
	rec := doJSON(t, router, http.MethodPatch, "/api/v1/providers/routing", map[string]any{
		"routes": map[string]string{"test_planning": "does-not-exist"},
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}
