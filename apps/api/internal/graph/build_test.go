package graph

import (
	"os"
	"path/filepath"
	"testing"

	"e2e-sentinel/apps/api/internal/routes"
	"e2e-sentinel/apps/api/internal/services"
)

func findEdge(edges []Edge, sourceKey, targetKey, relation string) (Edge, bool) {
	for _, e := range edges {
		if e.SourceKey == sourceKey && e.TargetKey == targetKey && e.RelationType == relation {
			return e, true
		}
	}
	return Edge{}, false
}

func TestBuild_DependsOnEdgesFromComposeDependencies(t *testing.T) {
	svcAPI := services.Service{Name: "api", Kind: services.KindAPI, ConfidenceLevel: services.ConfidenceMedium, Dependencies: []string{"postgres"}}
	svcDB := services.Service{Name: "postgres", Kind: services.KindDatabase, ConfidenceLevel: services.ConfidenceHigh}

	nodes, edges := Build(t.TempDir(), nil, []services.Service{svcAPI, svcDB})

	if len(nodes) != 2 {
		t.Fatalf("got %d nodes, want 2", len(nodes))
	}

	apiKey := (Node{NodeType: "service", Label: "api"}).Key()
	dbKey := (Node{NodeType: "service", Label: "postgres"}).Key()
	edge, ok := findEdge(edges, apiKey, dbKey, RelationDependsOn)
	if !ok {
		t.Fatalf("expected a depends_on edge api -> postgres, got %+v", edges)
	}
	if edge.Confidence != ConfidenceHigh {
		t.Errorf("Confidence = %q, want high (explicit compose depends_on)", edge.Confidence)
	}
}

func TestBuild_ServedByOnlyWhenExactlyOneApplicationService(t *testing.T) {
	route := routes.Route{Method: "GET", Path: "/api/v1/orders", Kind: routes.KindAPI, SourcePath: "server.js", Confidence: routes.ConfidenceMedium}

	// Exactly one application service -> served_by edge created.
	oneService := []services.Service{{Name: "api", Kind: services.KindAPI}}
	_, edges := Build(t.TempDir(), []routes.Route{route}, oneService)
	routeKey := routeLabelKey(route)
	apiServiceKey := (Node{NodeType: "service", Label: "api"}).Key()
	if _, ok := findEdge(edges, routeKey, apiServiceKey, RelationServedBy); !ok {
		t.Errorf("expected a served_by edge with exactly one application service, got %+v", edges)
	}

	// Two application services -> ambiguous, no served_by edge.
	twoServices := []services.Service{{Name: "api", Kind: services.KindAPI}, {Name: "worker", Kind: services.KindWorker}}
	_, edges2 := Build(t.TempDir(), []routes.Route{route}, twoServices)
	for _, e := range edges2 {
		if e.RelationType == RelationServedBy {
			t.Errorf("expected no served_by edges when the application service is ambiguous, got %+v", edges2)
		}
	}
}

func TestBuild_CallsEdgeFromFetchInPageSource(t *testing.T) {
	root := t.TempDir()
	pageSource := filepath.Join(root, "app", "login", "page.tsx")
	if err := os.MkdirAll(filepath.Dir(pageSource), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(pageSource, []byte(`fetch('/api/v1/auth/login', {method: 'POST'})`), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	page := routes.Route{Path: "/login", Kind: routes.KindAuth, SourcePath: "app/login/page.tsx", Confidence: routes.ConfidenceHigh}
	api := routes.Route{Method: "POST", Path: "/api/v1/auth/login", Kind: routes.KindAuth, SourcePath: "app/api/v1/auth/login/route.ts", Confidence: routes.ConfidenceHigh}

	_, edges := Build(root, []routes.Route{page, api}, nil)

	pageKey := routeLabelKey(page)
	apiKey := routeLabelKey(api)
	edge, ok := findEdge(edges, pageKey, apiKey, RelationCalls)
	if !ok {
		t.Fatalf("expected a calls edge from the login page to the login API, got %+v", edges)
	}
	if edge.Confidence != ConfidenceMedium {
		t.Errorf("Confidence = %q, want medium (string-matched, not statically verified)", edge.Confidence)
	}
}

func TestBuild_NoCallsEdgeForExternalURL(t *testing.T) {
	root := t.TempDir()
	pageSource := filepath.Join(root, "page.tsx")
	if err := os.WriteFile(pageSource, []byte(`fetch('https://external.example.com/data')`), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	page := routes.Route{Path: "/home", Kind: routes.KindPage, SourcePath: "page.tsx", Confidence: routes.ConfidenceHigh}

	_, edges := Build(root, []routes.Route{page}, nil)
	for _, e := range edges {
		if e.RelationType == RelationCalls {
			t.Errorf("expected no calls edge for an external URL, got %+v", edges)
		}
	}
}

func TestBuild_IsIdempotent(t *testing.T) {
	route := routes.Route{Method: "GET", Path: "/health", Kind: routes.KindHealth, SourcePath: "server.go", Confidence: routes.ConfidenceHigh}
	svc := services.Service{Name: "api", Kind: services.KindAPI}

	nodes1, edges1 := Build(t.TempDir(), []routes.Route{route}, []services.Service{svc})
	nodes2, edges2 := Build(t.TempDir(), []routes.Route{route}, []services.Service{svc})

	if len(nodes1) != len(nodes2) || len(edges1) != len(edges2) {
		t.Fatalf("Build is not idempotent: (%d nodes, %d edges) vs (%d nodes, %d edges)", len(nodes1), len(edges1), len(nodes2), len(edges2))
	}
}
