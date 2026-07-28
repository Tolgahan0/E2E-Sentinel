package routes

import (
	"os"
	"path/filepath"
	"testing"

	"e2e-sentinel/apps/api/internal/discovery"
)

func writeFixture(t *testing.T, root, relPath, content string) {
	t.Helper()
	full := filepath.Join(root, relPath)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatalf("mkdir for %q: %v", relPath, err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatalf("writing %q: %v", relPath, err)
	}
}

func findRoute(routes []Route, method, path string) (Route, bool) {
	for _, r := range routes {
		if r.Method == method && r.Path == path {
			return r, true
		}
	}
	return Route{}, false
}

func TestExtract_NextAppRouterPageAndRoute(t *testing.T) {
	root := t.TempDir()
	writeFixture(t, root, "apps/web/app/login/page.tsx", "export default function Page() { return null }")
	writeFixture(t, root, "apps/web/app/api/v1/auth/login/route.ts", `
export async function POST(req) { return Response.json({}) }
export async function GET(req) { return Response.json({}) }
`)

	routes, err := Extract(root, nil)
	if err != nil {
		t.Fatalf("Extract() error: %v", err)
	}

	page, ok := findRoute(routes, "", "/login")
	if !ok {
		t.Fatalf("expected page route /login, got %+v", routes)
	}
	if page.Kind != KindAuth {
		t.Errorf("page.Kind = %q, want auth (path contains /login)", page.Kind)
	}
	if page.Confidence != ConfidenceHigh {
		t.Errorf("page.Confidence = %q, want high", page.Confidence)
	}

	post, ok := findRoute(routes, "POST", "/api/v1/auth/login")
	if !ok {
		t.Fatalf("expected POST /api/v1/auth/login, got %+v", routes)
	}
	if post.Confidence != ConfidenceHigh {
		t.Errorf("post.Confidence = %q, want high", post.Confidence)
	}

	if _, ok := findRoute(routes, "GET", "/api/v1/auth/login"); !ok {
		t.Errorf("expected GET /api/v1/auth/login (both exported methods), got %+v", routes)
	}
}

func TestExtract_RouteGroupsExcludedFromPath(t *testing.T) {
	root := t.TempDir()
	writeFixture(t, root, "app/(marketing)/pricing/page.tsx", "export default function Page() { return null }")

	routes, err := Extract(root, nil)
	if err != nil {
		t.Fatalf("Extract() error: %v", err)
	}
	if _, ok := findRoute(routes, "", "/pricing"); !ok {
		t.Errorf("expected route group (marketing) to be excluded from the URL, got %+v", routes)
	}
	if _, ok := findRoute(routes, "", "/(marketing)/pricing"); ok {
		t.Error("route group parens leaked into the URL path")
	}
}

func TestExtract_ExpressStyleRoutes(t *testing.T) {
	root := t.TempDir()
	writeFixture(t, root, "server.js", `
app.get('/health', (req, res) => res.send('ok'));
router.post("/api/v1/orders", createOrder);
`)

	routes, err := Extract(root, nil)
	if err != nil {
		t.Fatalf("Extract() error: %v", err)
	}

	health, ok := findRoute(routes, "GET", "/health")
	if !ok {
		t.Fatalf("expected GET /health, got %+v", routes)
	}
	if health.Kind != KindHealth {
		t.Errorf("health.Kind = %q, want health", health.Kind)
	}
	if health.Confidence != ConfidenceMedium {
		t.Errorf("health.Confidence = %q, want medium (regex-matched)", health.Confidence)
	}

	if _, ok := findRoute(routes, "POST", "/api/v1/orders"); !ok {
		t.Errorf("expected POST /api/v1/orders, got %+v", routes)
	}
}

func TestExtract_GoChiStyleRoutes(t *testing.T) {
	root := t.TempDir()
	writeFixture(t, root, "internal/httpserver/server.go", `
r.Get("/health", handleHealth)
r.Post("/api/v1/projects", handleCreateProject(deps))
`)

	routes, err := Extract(root, nil)
	if err != nil {
		t.Fatalf("Extract() error: %v", err)
	}
	if _, ok := findRoute(routes, "GET", "/health"); !ok {
		t.Errorf("expected GET /health from Go chi-style call, got %+v", routes)
	}
	if _, ok := findRoute(routes, "POST", "/api/v1/projects"); !ok {
		t.Errorf("expected POST /api/v1/projects, got %+v", routes)
	}
}

func TestExtract_PythonDecoratorRoutes(t *testing.T) {
	root := t.TempDir()
	writeFixture(t, root, "app.py", `
@app.get("/api/v1/users")
def list_users():
    pass

@app.post('/api/v1/users')
def create_user():
    pass
`)

	routes, err := Extract(root, nil)
	if err != nil {
		t.Fatalf("Extract() error: %v", err)
	}
	if _, ok := findRoute(routes, "GET", "/api/v1/users"); !ok {
		t.Errorf("expected GET /api/v1/users, got %+v", routes)
	}
	if _, ok := findRoute(routes, "POST", "/api/v1/users"); !ok {
		t.Errorf("expected POST /api/v1/users, got %+v", routes)
	}
}

func TestExtract_OpenAPIPathsAreHighConfidence(t *testing.T) {
	root := t.TempDir()
	writeFixture(t, root, "openapi.yaml", `
paths:
  /api/v1/orders:
    get:
      summary: list orders
    post:
      summary: create order
`)

	findings := []discovery.Finding{
		{Category: discovery.CategoryAPISchema, Name: "openapi", Evidence: map[string]any{"paths": []string{"openapi.yaml"}}},
	}

	routes, err := Extract(root, findings)
	if err != nil {
		t.Fatalf("Extract() error: %v", err)
	}

	get, ok := findRoute(routes, "GET", "/api/v1/orders")
	if !ok {
		t.Fatalf("expected GET /api/v1/orders from OpenAPI, got %+v", routes)
	}
	if get.Confidence != ConfidenceHigh {
		t.Errorf("Confidence = %q, want high (OpenAPI-declared)", get.Confidence)
	}
	if _, ok := findRoute(routes, "POST", "/api/v1/orders"); !ok {
		t.Errorf("expected POST /api/v1/orders from OpenAPI, got %+v", routes)
	}
}

func TestExtract_OpenAPIUpgradesRegexMatchConfidence(t *testing.T) {
	root := t.TempDir()
	writeFixture(t, root, "server.js", `router.get('/api/v1/orders', listOrders);`)
	writeFixture(t, root, "openapi.yaml", `
paths:
  /api/v1/orders:
    get:
      summary: list orders
`)
	findings := []discovery.Finding{
		{Category: discovery.CategoryAPISchema, Name: "openapi", Evidence: map[string]any{"paths": []string{"openapi.yaml"}}},
	}

	routes, err := Extract(root, findings)
	if err != nil {
		t.Fatalf("Extract() error: %v", err)
	}
	get, ok := findRoute(routes, "GET", "/api/v1/orders")
	if !ok {
		t.Fatalf("expected GET /api/v1/orders, got %+v", routes)
	}
	if get.Confidence != ConfidenceHigh {
		t.Errorf("Confidence = %q, want high (OpenAPI should upgrade the regex-matched medium confidence)", get.Confidence)
	}
}

func TestExtract_NodeModulesNeverScanned(t *testing.T) {
	root := t.TempDir()
	writeFixture(t, root, "node_modules/express/lib/router.js", `app.get('/should-not-appear', noop);`)

	routes, err := Extract(root, nil)
	if err != nil {
		t.Fatalf("Extract() error: %v", err)
	}
	if _, ok := findRoute(routes, "GET", "/should-not-appear"); ok {
		t.Error("Extract scanned node_modules, which must be skipped")
	}
}

func TestExtract_IsIdempotent(t *testing.T) {
	root := t.TempDir()
	writeFixture(t, root, "server.js", `app.get('/health', noop);`)

	first, err := Extract(root, nil)
	if err != nil {
		t.Fatalf("first Extract() error: %v", err)
	}
	second, err := Extract(root, nil)
	if err != nil {
		t.Fatalf("second Extract() error: %v", err)
	}
	if len(first) != len(second) || len(first) != 1 {
		t.Fatalf("Extract is not idempotent: got %d then %d routes", len(first), len(second))
	}
}

func TestClassifyPathKind(t *testing.T) {
	cases := []struct {
		path      string
		hasMethod bool
		want      string
	}{
		{"/health", false, KindHealth},
		{"/api/v1/ready", true, KindHealth},
		{"/auth/login", true, KindAuth},
		{"/admin/users", true, KindAdmin},
		{"/webhooks/stripe", true, KindWebhook},
		{"/api/v1/orders", true, KindAPI},
		{"/dashboard", false, KindPage},
	}
	for _, tc := range cases {
		if got := ClassifyPathKind(tc.path, tc.hasMethod); got != tc.want {
			t.Errorf("ClassifyPathKind(%q, %v) = %q, want %q", tc.path, tc.hasMethod, got, tc.want)
		}
	}
}
