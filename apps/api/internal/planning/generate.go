package planning

import (
	"fmt"
	"strings"

	"e2e-sentinel/apps/api/internal/routes"
)

var mutatingMethods = map[string]bool{"POST": true, "PUT": true, "PATCH": true, "DELETE": true}

// GeneratePlan derives suggested test cases from extracted routes using
// fixed, deterministic rules — no AI call is made or required. Every
// rule that fires is traceable to the route(s) that triggered it via
// NaturalKey and Description.
func GeneratePlan(extractedRoutes []routes.Route) []TestCase {
	var out []TestCase
	for _, r := range extractedRoutes {
		out = append(out, testCasesForRoute(r)...)
	}
	return out
}

func testCasesForRoute(r routes.Route) []TestCase {
	isMutating := mutatingMethods[r.Method]
	confidence := mapConfidence(r.Confidence)

	switch r.Kind {
	case routes.KindHealth:
		return []TestCase{tc(r, CategorySmoke, PriorityP3, confidence, isMutating,
			fmt.Sprintf("Smoke test: %s responds healthy", r.Path),
			fmt.Sprintf("Verify %s returns a successful health status.", routeLabel(r)),
			[]string{"Send a request to " + r.Path}, []string{"Response status indicates healthy"})}

	case routes.KindAuth:
		if r.Method == "" {
			// A browser page (e.g. /login) rather than an API endpoint.
			return []TestCase{tc(r, CategoryAuthentication, PriorityP0, confidence, false,
				fmt.Sprintf("Authentication: %s renders and accepts input", r.Path),
				fmt.Sprintf("Verify the %s page renders and its form can be submitted.", r.Path),
				[]string{"Open " + r.Path}, []string{"Page renders", "Form is present and submittable"})}
		}
		tests := []TestCase{tc(r, CategoryAuthentication, PriorityP0, confidence, isMutating,
			fmt.Sprintf("Authentication: %s succeeds with valid credentials", routeLabel(r)),
			fmt.Sprintf("Verify %s succeeds for valid input.", routeLabel(r)),
			[]string{"Call " + routeLabel(r) + " with valid credentials"}, []string{"Response indicates success", "A session/token is issued"})}
		tests = append(tests, tc(r, CategoryAuthentication, PriorityP0, confidence, isMutating,
			fmt.Sprintf("Authentication: %s rejects invalid credentials", routeLabel(r)),
			fmt.Sprintf("Verify %s rejects invalid input without leaking details.", routeLabel(r)),
			[]string{"Call " + routeLabel(r) + " with invalid credentials"}, []string{"Response indicates failure", "No sensitive detail is leaked in the error"}))
		return tests

	case routes.KindAdmin:
		return []TestCase{tc(r, CategoryAuthorization, PriorityP0, confidence, isMutating,
			fmt.Sprintf("Authorization: non-admin cannot access %s", routeLabel(r)),
			fmt.Sprintf("Verify a non-admin user is denied access to %s.", routeLabel(r)),
			[]string{"Authenticate as a non-admin user", "Call " + routeLabel(r)}, []string{"Response is 401/403, not 200"})}

	case routes.KindWebhook:
		return []TestCase{tc(r, CategoryErrorHandling, PriorityP2, confidence, isMutating,
			fmt.Sprintf("Error handling: %s rejects an invalid payload", routeLabel(r)),
			fmt.Sprintf("Verify %s handles a malformed payload without a 5xx error.", routeLabel(r)),
			[]string{"Call " + routeLabel(r) + " with an invalid/malformed body"}, []string{"Response is a well-formed 4xx, not an unhandled 5xx"})}

	case routes.KindAPI:
		if strings.Contains(strings.ToLower(r.Path), "tenant") || strings.Contains(r.Path, "{tenant") || strings.Contains(r.Path, "[tenant") {
			out := []TestCase{tc(r, CategoryTenantIsolation, PriorityP0, confidence, isMutating,
				fmt.Sprintf("Tenant isolation: %s cannot access another tenant's data", routeLabel(r)),
				fmt.Sprintf("Verify %s scopes results to the caller's own tenant.", routeLabel(r)),
				[]string{"Authenticate as tenant A", "Call " + routeLabel(r) + " referencing tenant B's resource"}, []string{"Response is 403/404, not tenant B's data"})}
			return append(out, apiSchemaAndCRUD(r, confidence, isMutating)...)
		}
		return apiSchemaAndCRUD(r, confidence, isMutating)

	case routes.KindPage:
		return []TestCase{tc(r, CategoryCriticalJourney, PriorityP2, confidence, false,
			fmt.Sprintf("Page: %s renders without error", r.Path),
			fmt.Sprintf("Verify %s renders without a console/page error.", r.Path),
			[]string{"Open " + r.Path}, []string{"Page renders", "No uncaught console error"})}

	case routes.KindWebSocket:
		return []TestCase{tc(r, CategoryConnectivity, PriorityP2, confidence, false,
			fmt.Sprintf("Connectivity: %s accepts a connection", r.Path),
			fmt.Sprintf("Verify a WebSocket connection to %s succeeds and yields at least one message within a timeout.", r.Path),
			[]string{"Open a WebSocket connection to " + r.Path}, []string{"Connection is accepted", "At least one message is received before the timeout"})}
	}
	return nil
}

func apiSchemaAndCRUD(r routes.Route, confidence string, isMutating bool) []TestCase {
	if !isMutating {
		return []TestCase{tc(r, CategoryAPISchema, PriorityP2, confidence, false,
			fmt.Sprintf("API schema: %s returns a well-formed response", routeLabel(r)),
			fmt.Sprintf("Verify %s returns a response matching its expected schema.", routeLabel(r)),
			[]string{"Call " + routeLabel(r)}, []string{"Response status is 2xx", "Response body matches the expected schema"})}
	}
	return []TestCase{
		tc(r, CategoryCRUD, PriorityP1, confidence, true,
			fmt.Sprintf("CRUD: %s succeeds with valid input", routeLabel(r)),
			fmt.Sprintf("Verify %s succeeds for a valid request.", routeLabel(r)),
			[]string{"Call " + routeLabel(r) + " with valid input"}, []string{"Response status is 2xx"}),
		tc(r, CategoryCRUD, PriorityP1, confidence, true,
			fmt.Sprintf("Validation: %s rejects invalid input", routeLabel(r)),
			fmt.Sprintf("Verify %s rejects invalid/missing required fields.", routeLabel(r)),
			[]string{"Call " + routeLabel(r) + " with missing/invalid fields"}, []string{"Response status is 4xx", "Error message identifies the invalid field"}),
	}
}

func tc(r routes.Route, category, priority, confidence string, isMutating bool, title, description string, steps, assertions []string) TestCase {
	return TestCase{
		Title: title, Description: description, Category: category,
		Framework:  frameworkFor(r),
		Status:     StatusSuggested,
		RiskLevel:  riskFor(priority),
		Priority:   priority,
		Confidence: confidence,
		Source:     "rule_engine",
		Steps:      steps, Assertions: assertions,
		IsMutating:       isMutating,
		IsProductionSafe: !isMutating,
		ApprovalStatus:   ApprovalPending,
		RoutePath:        r.Path,
		RouteMethod:      r.Method,
		// Title (not just category+route) must be part of the key: a
		// single route can produce multiple sibling test cases in the
		// same category (e.g. "valid credentials" and "invalid
		// credentials" for one login route) that must not collide.
		NaturalKey: category + "|" + routeLabel(r) + "|" + title,
	}
}

func routeLabel(r routes.Route) string {
	if r.Method == "" {
		return r.Path
	}
	return r.Method + " " + r.Path
}

func frameworkFor(r routes.Route) string {
	if r.Kind == routes.KindWebSocket {
		return "websocket"
	}
	if r.Method == "" {
		return "playwright"
	}
	return "api"
}

func riskFor(priority string) string {
	switch priority {
	case PriorityP0:
		return "high"
	case PriorityP1:
		return "medium"
	default:
		return "low"
	}
}

func mapConfidence(routeConfidence string) string {
	switch routeConfidence {
	case routes.ConfidenceHigh:
		return ConfidenceHigh
	default:
		return ConfidenceMedium
	}
}
