package testgen

import (
	"errors"
	"strings"
	"testing"
)

func TestGenerateSpec_RequiresBaseURL(t *testing.T) {
	_, _, err := GenerateSpec(TestCaseInput{ID: "1", Title: "x", RoutePath: "/x"}, "")
	if !errors.Is(err, ErrMissingBaseURL) {
		t.Fatalf("err = %v, want ErrMissingBaseURL", err)
	}
}

func TestGenerateSpec_PageTest(t *testing.T) {
	filename, content, err := GenerateSpec(TestCaseInput{
		ID: "abc-123", Title: "Login page renders", RoutePath: "/login",
	}, "http://localhost:3000")
	if err != nil {
		t.Fatalf("GenerateSpec() error: %v", err)
	}
	if filename != "generated-abc-123.spec.ts" {
		t.Errorf("filename = %q", filename)
	}
	if !strings.Contains(content, "page.goto('http://localhost:3000/login')") {
		t.Errorf("content missing expected goto call:\n%s", content)
	}
	if !strings.Contains(content, "async ({ page })") {
		t.Errorf("expected a page-fixture test, got:\n%s", content)
	}
	if strings.Contains(content, "sleep") || strings.Contains(content, "waitForTimeout") {
		t.Error("generated test must not use arbitrary sleeps (spec §10.1)")
	}
}

func TestGenerateSpec_APITest(t *testing.T) {
	_, content, err := GenerateSpec(TestCaseInput{
		ID: "def-456", Title: "Create order", RoutePath: "/api/v1/orders", RouteMethod: "POST",
	}, "http://localhost:8080")
	if err != nil {
		t.Fatalf("GenerateSpec() error: %v", err)
	}
	if !strings.Contains(content, "async ({ request })") {
		t.Errorf("expected a request-fixture test, got:\n%s", content)
	}
	if !strings.Contains(content, "request.post('http://localhost:8080/api/v1/orders')") {
		t.Errorf("content missing expected request call:\n%s", content)
	}
	if !strings.Contains(content, "toBeLessThan(500)") {
		t.Errorf("expected a 5xx-guard assertion, got:\n%s", content)
	}
}

func TestGenerateSpec_EscapesQuotesInTitle(t *testing.T) {
	_, content, err := GenerateSpec(TestCaseInput{
		ID: "1", Title: `It's a "test"`, RoutePath: "/x",
	}, "http://localhost:3000")
	if err != nil {
		t.Fatalf("GenerateSpec() error: %v", err)
	}
	if strings.Contains(content, `test('It's a`) {
		t.Errorf("unescaped single quote in title would break the generated JS string literal:\n%s", content)
	}
	if !strings.Contains(content, `It\'s`) {
		t.Errorf("expected escaped apostrophe, got:\n%s", content)
	}
}

func TestGenerateSpec_UnknownMethodDefaultsToGet(t *testing.T) {
	_, content, err := GenerateSpec(TestCaseInput{
		ID: "1", Title: "x", RoutePath: "/x", RouteMethod: "TRACE",
	}, "http://localhost:3000")
	if err != nil {
		t.Fatalf("GenerateSpec() error: %v", err)
	}
	if !strings.Contains(content, "request.get(") {
		t.Errorf("expected fallback to request.get for an unmapped method, got:\n%s", content)
	}
}

func TestGenerateSpec_WebSocketTest(t *testing.T) {
	filename, content, err := GenerateSpec(TestCaseInput{
		ID: "ws-1", Title: "Socket accepts a connection", RoutePath: "ws://localhost:8080/socket", Framework: "websocket",
	}, "")
	if err != nil {
		t.Fatalf("GenerateSpec() error: %v", err)
	}
	if filename != "generated-ws-1.test.js" {
		t.Errorf("filename = %q, want generated-ws-1.test.js", filename)
	}
	if !strings.Contains(content, `require('ws')`) {
		t.Errorf("expected the ws package to be required, got:\n%s", content)
	}
	if !strings.Contains(content, `"ws://localhost:8080/socket"`) {
		t.Errorf("content missing the target URL:\n%s", content)
	}
	if !strings.Contains(content, "process.exit(0)") || !strings.Contains(content, "process.exit(1)") {
		t.Errorf("expected explicit pass/fail exit codes, got:\n%s", content)
	}
}

func TestGenerateSpec_WebSocketIgnoresBaseURL(t *testing.T) {
	// baseURL is irrelevant for websocket framework tests — RoutePath is
	// already a full ws:// URL — so an empty baseURL must not trigger
	// ErrMissingBaseURL the way it would for playwright/api tests.
	_, _, err := GenerateSpec(TestCaseInput{
		ID: "ws-2", Title: "x", RoutePath: "ws://localhost:8080/socket", Framework: "websocket",
	}, "")
	if err != nil {
		t.Fatalf("GenerateSpec() error: %v, want nil even with an empty baseURL", err)
	}
}

func TestGenerateSpec_WebSocketRequiresRoutePath(t *testing.T) {
	_, _, err := GenerateSpec(TestCaseInput{ID: "ws-3", Title: "x", Framework: "websocket"}, "")
	if !errors.Is(err, ErrMissingWebSocketURL) {
		t.Fatalf("err = %v, want ErrMissingWebSocketURL", err)
	}
}

func TestGenerateSpec_WebSocketTitleCannotBreakOutOfComment(t *testing.T) {
	_, content, err := GenerateSpec(TestCaseInput{
		ID: "ws-4", Title: "line one\nprocess.exit(1) // line two", RoutePath: "ws://localhost:8080/socket", Framework: "websocket",
	}, "")
	if err != nil {
		t.Fatalf("GenerateSpec() error: %v", err)
	}
	firstLine := strings.SplitN(content, "\n", 2)[0]
	if !strings.HasPrefix(firstLine, "//") {
		t.Errorf("first line = %q, want a comment (title must not break out of it)", firstLine)
	}
}

func TestGenerateSpec_FilenameIsFilesystemSafe(t *testing.T) {
	filename, _, err := GenerateSpec(TestCaseInput{ID: "../../etc/passwd", Title: "x", RoutePath: "/x"}, "http://localhost:3000")
	if err != nil {
		t.Fatalf("GenerateSpec() error: %v", err)
	}
	if strings.Contains(filename, "/") || strings.Contains(filename, "..") {
		t.Errorf("filename must not contain path separators or traversal: %q", filename)
	}
}
