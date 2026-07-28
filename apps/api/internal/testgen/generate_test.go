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

func TestGenerateSpec_FilenameIsFilesystemSafe(t *testing.T) {
	filename, _, err := GenerateSpec(TestCaseInput{ID: "../../etc/passwd", Title: "x", RoutePath: "/x"}, "http://localhost:3000")
	if err != nil {
		t.Fatalf("GenerateSpec() error: %v", err)
	}
	if strings.Contains(filename, "/") || strings.Contains(filename, "..") {
		t.Errorf("filename must not contain path separators or traversal: %q", filename)
	}
}
