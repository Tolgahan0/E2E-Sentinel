package providers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHealthChecker_Ollama_OK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/tags" {
			t.Errorf("path = %q, want /api/tags", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"models":[]}`))
	}))
	defer srv.Close()

	checker := NewHealthChecker(srv.Client())
	result := checker.Check(context.Background(), Provider{Type: TypeOllama, BaseURL: srv.URL, TimeoutSeconds: 5}, "")

	if result.Status != HealthOK {
		t.Fatalf("Status = %q, want %q (message: %s)", result.Status, HealthOK, result.Message)
	}
}

func TestHealthChecker_OpenAI_SendsBearerToken(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		if r.URL.Path != "/models" {
			t.Errorf("path = %q, want /models", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	checker := NewHealthChecker(srv.Client())
	result := checker.Check(context.Background(), Provider{Type: TypeOpenAI, BaseURL: srv.URL, TimeoutSeconds: 5}, "sk-test-key")

	if result.Status != HealthOK {
		t.Fatalf("Status = %q, want %q", result.Status, HealthOK)
	}
	if gotAuth != "Bearer sk-test-key" {
		t.Errorf("Authorization header = %q, want %q", gotAuth, "Bearer sk-test-key")
	}
}

func TestHealthChecker_Anthropic_SendsAPIKeyHeader(t *testing.T) {
	var gotKey, gotVersion string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotKey = r.Header.Get("x-api-key")
		gotVersion = r.Header.Get("anthropic-version")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	checker := NewHealthChecker(srv.Client())
	result := checker.Check(context.Background(), Provider{Type: TypeAnthropic, BaseURL: srv.URL, TimeoutSeconds: 5}, "sk-ant-test")

	if result.Status != HealthOK {
		t.Fatalf("Status = %q, want %q", result.Status, HealthOK)
	}
	if gotKey != "sk-ant-test" {
		t.Errorf("x-api-key = %q, want %q", gotKey, "sk-ant-test")
	}
	if gotVersion == "" {
		t.Error("anthropic-version header was not set")
	}
}

func TestHealthChecker_AzureOpenAI_SendsAPIKeyHeader(t *testing.T) {
	var gotKey string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotKey = r.Header.Get("api-key")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	checker := NewHealthChecker(srv.Client())
	result := checker.Check(context.Background(), Provider{Type: TypeAzureOpenAI, BaseURL: srv.URL, TimeoutSeconds: 5}, "azure-key")

	if result.Status != HealthOK {
		t.Fatalf("Status = %q, want %q", result.Status, HealthOK)
	}
	if gotKey != "azure-key" {
		t.Errorf("api-key header = %q, want %q", gotKey, "azure-key")
	}
}

func TestHealthChecker_Gemini_SendsKeyAsQueryParam(t *testing.T) {
	var gotKey string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotKey = r.URL.Query().Get("key")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	checker := NewHealthChecker(srv.Client())
	result := checker.Check(context.Background(), Provider{Type: TypeGemini, BaseURL: srv.URL, TimeoutSeconds: 5}, "gemini-key")

	if result.Status != HealthOK {
		t.Fatalf("Status = %q, want %q", result.Status, HealthOK)
	}
	if gotKey != "gemini-key" {
		t.Errorf("key query param = %q, want %q", gotKey, "gemini-key")
	}
}

func TestHealthChecker_OpenAICompatible_UsesModelsPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/models" {
			t.Errorf("path = %q, want /models", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	checker := NewHealthChecker(srv.Client())
	result := checker.Check(context.Background(), Provider{Type: TypeOpenAICompatible, BaseURL: srv.URL, TimeoutSeconds: 5}, "")

	if result.Status != HealthOK {
		t.Fatalf("Status = %q, want %q", result.Status, HealthOK)
	}
}

func TestHealthChecker_UnauthorizedIsReportedAsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	checker := NewHealthChecker(srv.Client())
	result := checker.Check(context.Background(), Provider{Type: TypeOpenAI, BaseURL: srv.URL, TimeoutSeconds: 5}, "wrong-key")

	if result.Status != HealthError {
		t.Fatalf("Status = %q, want %q", result.Status, HealthError)
	}
	if result.Message == "" {
		t.Error("expected a non-empty error message")
	}
}

func TestHealthChecker_UnreachableHostIsReportedAsError(t *testing.T) {
	checker := NewHealthChecker(nil)
	result := checker.Check(context.Background(), Provider{Type: TypeOllama, BaseURL: "http://127.0.0.1:1", TimeoutSeconds: 1}, "")

	if result.Status != HealthError {
		t.Fatalf("Status = %q, want %q", result.Status, HealthError)
	}
}

func TestHealthChecker_MissingBaseURL(t *testing.T) {
	checker := NewHealthChecker(nil)
	result := checker.Check(context.Background(), Provider{Type: TypeOllama, TimeoutSeconds: 5}, "")

	if result.Status != HealthError {
		t.Fatalf("Status = %q, want %q", result.Status, HealthError)
	}
}

func TestHealthChecker_InvalidType(t *testing.T) {
	checker := NewHealthChecker(nil)
	result := checker.Check(context.Background(), Provider{Type: "bogus", BaseURL: "http://example.com", TimeoutSeconds: 5}, "")

	if result.Status != HealthError {
		t.Fatalf("Status = %q, want %q", result.Status, HealthError)
	}
}

func TestHealthChecker_ServerErrorIsReportedAsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	checker := NewHealthChecker(srv.Client())
	result := checker.Check(context.Background(), Provider{Type: TypeOllama, BaseURL: srv.URL, TimeoutSeconds: 5}, "")

	if result.Status != HealthError {
		t.Fatalf("Status = %q, want %q", result.Status, HealthError)
	}
}
