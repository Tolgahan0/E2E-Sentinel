package planning

import (
	"testing"

	"e2e-sentinel/apps/api/internal/routes"
)

func findByCategory(cases []TestCase, category string) []TestCase {
	var out []TestCase
	for _, c := range cases {
		if c.Category == category {
			out = append(out, c)
		}
	}
	return out
}

func TestGeneratePlan_LoginRouteProducesP0AuthTests(t *testing.T) {
	login := routes.Route{Method: "POST", Path: "/api/v1/auth/login", Kind: routes.KindAuth, Confidence: routes.ConfidenceHigh}
	cases := GeneratePlan([]routes.Route{login})

	auth := findByCategory(cases, CategoryAuthentication)
	if len(auth) != 2 {
		t.Fatalf("expected 2 authentication test cases (valid + invalid), got %d: %+v", len(auth), auth)
	}
	for _, c := range auth {
		if c.Priority != PriorityP0 {
			t.Errorf("Priority = %q, want P0 for auth route, case=%+v", c.Priority, c)
		}
		if c.RiskLevel != "high" {
			t.Errorf("RiskLevel = %q, want high", c.RiskLevel)
		}
		if !c.IsMutating {
			t.Error("POST auth route should be marked mutating")
		}
		if c.IsProductionSafe {
			t.Error("a mutating test must not be marked production-safe")
		}
	}
}

func TestGeneratePlan_ReadOnlyGetIsProductionSafe(t *testing.T) {
	list := routes.Route{Method: "GET", Path: "/api/v1/orders", Kind: routes.KindAPI, Confidence: routes.ConfidenceHigh}
	cases := GeneratePlan([]routes.Route{list})

	found := findByCategory(cases, CategoryAPISchema)
	if len(found) != 1 {
		t.Fatalf("expected 1 api_schema test case, got %+v", cases)
	}
	if found[0].IsMutating {
		t.Error("GET route must not be marked mutating")
	}
	if !found[0].IsProductionSafe {
		t.Error("a non-mutating test must be production-safe")
	}
}

func TestGeneratePlan_MutatingAPIProducesCRUDAndValidationTests(t *testing.T) {
	create := routes.Route{Method: "POST", Path: "/api/v1/orders", Kind: routes.KindAPI, Confidence: routes.ConfidenceHigh}
	cases := GeneratePlan([]routes.Route{create})

	crud := findByCategory(cases, CategoryCRUD)
	if len(crud) != 2 {
		t.Fatalf("expected 2 CRUD test cases (success + validation), got %d: %+v", len(crud), crud)
	}
	for _, c := range crud {
		if !c.IsMutating || c.IsProductionSafe {
			t.Errorf("mutating CRUD case must be IsMutating=true, IsProductionSafe=false: %+v", c)
		}
		if c.Priority != PriorityP1 {
			t.Errorf("Priority = %q, want P1", c.Priority)
		}
	}
}

func TestGeneratePlan_TenantPathProducesIsolationTest(t *testing.T) {
	route := routes.Route{Method: "GET", Path: "/api/v1/tenants/{tenantId}/orders", Kind: routes.KindAPI, Confidence: routes.ConfidenceHigh}
	cases := GeneratePlan([]routes.Route{route})

	isolation := findByCategory(cases, CategoryTenantIsolation)
	if len(isolation) != 1 {
		t.Fatalf("expected 1 tenant isolation test case, got %+v", cases)
	}
	if isolation[0].Priority != PriorityP0 {
		t.Errorf("Priority = %q, want P0 for tenant isolation", isolation[0].Priority)
	}
}

func TestGeneratePlan_AdminRouteProducesAuthorizationTest(t *testing.T) {
	route := routes.Route{Method: "GET", Path: "/admin/users", Kind: routes.KindAdmin, Confidence: routes.ConfidenceHigh}
	cases := GeneratePlan([]routes.Route{route})

	authz := findByCategory(cases, CategoryAuthorization)
	if len(authz) != 1 {
		t.Fatalf("expected 1 authorization test case, got %+v", cases)
	}
	if authz[0].Priority != PriorityP0 {
		t.Errorf("Priority = %q, want P0", authz[0].Priority)
	}
}

func TestGeneratePlan_HealthRouteIsLowPrioritySmoke(t *testing.T) {
	route := routes.Route{Method: "GET", Path: "/health", Kind: routes.KindHealth, Confidence: routes.ConfidenceHigh}
	cases := GeneratePlan([]routes.Route{route})

	smoke := findByCategory(cases, CategorySmoke)
	if len(smoke) != 1 {
		t.Fatalf("expected 1 smoke test case, got %+v", cases)
	}
	if smoke[0].Priority != PriorityP3 {
		t.Errorf("Priority = %q, want P3 for a health check", smoke[0].Priority)
	}
}

func TestGeneratePlan_WebSocketRouteProducesConnectivityTestWithWebSocketFramework(t *testing.T) {
	route := routes.Route{Path: "ws://localhost:8080/socket", Kind: routes.KindWebSocket, Confidence: routes.ConfidenceMedium}
	cases := GeneratePlan([]routes.Route{route})

	connectivity := findByCategory(cases, CategoryConnectivity)
	if len(connectivity) != 1 {
		t.Fatalf("expected 1 connectivity test case, got %+v", cases)
	}
	tc := connectivity[0]
	if tc.Framework != "websocket" {
		t.Errorf("Framework = %q, want websocket", tc.Framework)
	}
	if tc.RoutePath != "ws://localhost:8080/socket" {
		t.Errorf("RoutePath = %q", tc.RoutePath)
	}
	if tc.IsMutating {
		t.Error("a connectivity smoke test must not be marked mutating")
	}
	if !tc.IsProductionSafe {
		t.Error("a non-mutating connectivity test should be production-safe")
	}
}

func TestGeneratePlan_SiblingTestCasesForSameRouteHaveDistinctNaturalKeys(t *testing.T) {
	login := routes.Route{Method: "POST", Path: "/api/v1/auth/login", Kind: routes.KindAuth, Confidence: routes.ConfidenceHigh}
	cases := GeneratePlan([]routes.Route{login})

	seen := map[string]bool{}
	for _, c := range cases {
		if seen[c.NaturalKey] {
			t.Fatalf("duplicate NaturalKey %q across sibling test cases for the same route: %+v", c.NaturalKey, cases)
		}
		seen[c.NaturalKey] = true
	}
	if len(seen) != 2 {
		t.Fatalf("expected 2 distinct natural keys, got %d", len(seen))
	}
}

func TestGeneratePlan_NaturalKeyIsStableAcrossRegeneration(t *testing.T) {
	route := routes.Route{Method: "GET", Path: "/health", Kind: routes.KindHealth, Confidence: routes.ConfidenceHigh}
	first := GeneratePlan([]routes.Route{route})
	second := GeneratePlan([]routes.Route{route})

	if len(first) != 1 || len(second) != 1 {
		t.Fatalf("expected exactly one test case per run, got %d then %d", len(first), len(second))
	}
	if first[0].NaturalKey != second[0].NaturalKey {
		t.Errorf("NaturalKey changed across identical regenerations: %q vs %q", first[0].NaturalKey, second[0].NaturalKey)
	}
}

func TestGeneratePlan_MediumConfidenceRouteStaysMediumConfidenceTest(t *testing.T) {
	route := routes.Route{Method: "GET", Path: "/api/v1/orders", Kind: routes.KindAPI, Confidence: routes.ConfidenceMedium}
	cases := GeneratePlan([]routes.Route{route})
	if len(cases) == 0 {
		t.Fatal("expected at least one test case")
	}
	if cases[0].Confidence != ConfidenceMedium {
		t.Errorf("Confidence = %q, want medium (never upgrade a medium-confidence route to a high-confidence test)", cases[0].Confidence)
	}
}
