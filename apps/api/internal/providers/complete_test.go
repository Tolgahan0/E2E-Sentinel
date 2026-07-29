package providers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCompleter_Ollama(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/chat" {
			t.Errorf("path = %q, want /api/chat", r.URL.Path)
		}
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		if body["stream"] != false {
			t.Error("expected stream: false")
		}
		w.Write([]byte(`{"message":{"content":"hello from ollama"}}`))
	}))
	defer srv.Close()

	c := NewCompleter(srv.Client())
	result, err := c.Complete(context.Background(), Provider{Type: TypeOllama, BaseURL: srv.URL, Model: "llama3", TimeoutSeconds: 5}, "", CompletionRequest{SystemPrompt: "sys", UserPrompt: "user"})
	if err != nil {
		t.Fatalf("Complete() error: %v", err)
	}
	if result.Text != "hello from ollama" {
		t.Errorf("Text = %q", result.Text)
	}
}

func TestCompleter_OpenAI_SendsBearerToken(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		if r.URL.Path != "/chat/completions" {
			t.Errorf("path = %q, want /chat/completions", r.URL.Path)
		}
		w.Write([]byte(`{"choices":[{"message":{"content":"hello from openai"}}]}`))
	}))
	defer srv.Close()

	c := NewCompleter(srv.Client())
	result, err := c.Complete(context.Background(), Provider{Type: TypeOpenAI, BaseURL: srv.URL, Model: "gpt-4o", TimeoutSeconds: 5}, "sk-test", CompletionRequest{})
	if err != nil {
		t.Fatalf("Complete() error: %v", err)
	}
	if result.Text != "hello from openai" {
		t.Errorf("Text = %q", result.Text)
	}
	if gotAuth != "Bearer sk-test" {
		t.Errorf("Authorization = %q", gotAuth)
	}
}

func TestCompleter_AzureOpenAI_UsesDeploymentPath(t *testing.T) {
	var gotPath, gotKey string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotKey = r.Header.Get("api-key")
		w.Write([]byte(`{"choices":[{"message":{"content":"hello from azure"}}]}`))
	}))
	defer srv.Close()

	c := NewCompleter(srv.Client())
	result, err := c.Complete(context.Background(), Provider{Type: TypeAzureOpenAI, BaseURL: srv.URL, Model: "gpt-4-deploy", TimeoutSeconds: 5}, "azure-key", CompletionRequest{})
	if err != nil {
		t.Fatalf("Complete() error: %v", err)
	}
	if result.Text != "hello from azure" {
		t.Errorf("Text = %q", result.Text)
	}
	if gotPath != "/openai/deployments/gpt-4-deploy/chat/completions" {
		t.Errorf("path = %q", gotPath)
	}
	if gotKey != "azure-key" {
		t.Errorf("api-key = %q", gotKey)
	}
}

func TestCompleter_Anthropic_SendsAPIKeyHeader(t *testing.T) {
	var gotKey string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotKey = r.Header.Get("x-api-key")
		w.Write([]byte(`{"content":[{"text":"hello from claude"}]}`))
	}))
	defer srv.Close()

	c := NewCompleter(srv.Client())
	result, err := c.Complete(context.Background(), Provider{Type: TypeAnthropic, BaseURL: srv.URL, Model: "claude-3", TimeoutSeconds: 5}, "sk-ant", CompletionRequest{})
	if err != nil {
		t.Fatalf("Complete() error: %v", err)
	}
	if result.Text != "hello from claude" {
		t.Errorf("Text = %q", result.Text)
	}
	if gotKey != "sk-ant" {
		t.Errorf("x-api-key = %q", gotKey)
	}
}

func TestCompleter_Gemini_SendsKeyAsQueryParam(t *testing.T) {
	var gotKey string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotKey = r.URL.Query().Get("key")
		w.Write([]byte(`{"candidates":[{"content":{"parts":[{"text":"hello from gemini"}]}}]}`))
	}))
	defer srv.Close()

	c := NewCompleter(srv.Client())
	result, err := c.Complete(context.Background(), Provider{Type: TypeGemini, BaseURL: srv.URL, Model: "gemini-pro", TimeoutSeconds: 5}, "gem-key", CompletionRequest{})
	if err != nil {
		t.Fatalf("Complete() error: %v", err)
	}
	if result.Text != "hello from gemini" {
		t.Errorf("Text = %q", result.Text)
	}
	if gotKey != "gem-key" {
		t.Errorf("key query param = %q", gotKey)
	}
}

func TestCompleter_ServerErrorReturnsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	c := NewCompleter(srv.Client())
	_, err := c.Complete(context.Background(), Provider{Type: TypeOpenAI, BaseURL: srv.URL, TimeoutSeconds: 5}, "", CompletionRequest{})
	if err == nil {
		t.Fatal("expected an error for HTTP 500")
	}
}

func TestCompleter_InvalidType(t *testing.T) {
	c := NewCompleter(nil)
	_, err := c.Complete(context.Background(), Provider{Type: "bogus", BaseURL: "http://example.com", TimeoutSeconds: 5}, "", CompletionRequest{})
	if err != ErrInvalidType {
		t.Fatalf("error = %v, want ErrInvalidType", err)
	}
}

func TestCompleter_MissingBaseURL(t *testing.T) {
	c := NewCompleter(nil)
	_, err := c.Complete(context.Background(), Provider{Type: TypeOpenAI, TimeoutSeconds: 5}, "", CompletionRequest{})
	if err == nil {
		t.Fatal("expected an error for missing base_url")
	}
}

func TestExtractUnifiedDiff_FromFencedDiffBlock(t *testing.T) {
	text := "Here is the fix:\n\n```diff\n--- a/main.go\n+++ b/main.go\n@@ -1,1 +1,1 @@\n-old\n+new\n```\n\nLet me know if you need anything else."
	diff, err := ExtractUnifiedDiff(text)
	if err != nil {
		t.Fatalf("ExtractUnifiedDiff() error: %v", err)
	}
	if diff != "--- a/main.go\n+++ b/main.go\n@@ -1,1 +1,1 @@\n-old\n+new" {
		t.Errorf("diff = %q", diff)
	}
}

func TestExtractUnifiedDiff_FromPlainFence(t *testing.T) {
	text := "```\n--- a/x\n+++ b/x\n@@ -1 +1 @@\n-a\n+b\n```"
	diff, err := ExtractUnifiedDiff(text)
	if err != nil {
		t.Fatalf("ExtractUnifiedDiff() error: %v", err)
	}
	if diff == "" {
		t.Error("expected a non-empty diff")
	}
}

func TestExtractUnifiedDiff_NoFenceReturnsError(t *testing.T) {
	_, err := ExtractUnifiedDiff("I think you should change the login handler but I won't show a diff.")
	if err != ErrNoDiffFound {
		t.Fatalf("error = %v, want ErrNoDiffFound", err)
	}
}
