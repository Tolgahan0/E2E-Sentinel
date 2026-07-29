package httpserver

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"e2e-sentinel/apps/api/internal/auth"
)

func TestSecurityHeaders_SetOnEveryResponse(t *testing.T) {
	router := NewRouter(newTestDeps(nil, nil))
	rec := doJSON(t, router, http.MethodGet, "/api/v1/projects", nil)

	cases := map[string]string{
		"X-Content-Type-Options":    "nosniff",
		"X-Frame-Options":           "DENY",
		"Referrer-Policy":           "no-referrer",
		"Content-Security-Policy":   "default-src 'none'; frame-ancestors 'none'",
		"Strict-Transport-Security": "max-age=63072000; includeSubDomains",
	}
	for header, want := range cases {
		if got := rec.Header().Get(header); got != want {
			t.Errorf("%s = %q, want %q", header, got, want)
		}
	}
}

func TestSecurityHeaders_SetOnHealthEndpointToo(t *testing.T) {
	router := NewRouter(newTestDeps(nil, nil))
	rec := doJSON(t, router, http.MethodGet, "/health", nil)
	if rec.Header().Get("X-Frame-Options") != "DENY" {
		t.Error("security headers should apply to every route, not just /api/v1")
	}
}

func TestCSRFProtection_NoOpWhenAuthDisabled(t *testing.T) {
	router := NewRouter(newTestDeps(nil, nil))
	// A mutating request with no CSRF header must still work when auth
	// is disabled — every prior phase's behavior must be unchanged.
	rec := doJSON(t, router, http.MethodPost, "/api/v1/projects", map[string]string{"name": "x", "repository_path": t.TempDir()})
	if rec.Code == http.StatusForbidden {
		t.Fatalf("status = 403, want CSRF check to be a no-op when auth is disabled, body=%s", rec.Body.String())
	}
}

func TestCSRFProtection_RequiresHeaderWhenAuthEnabled(t *testing.T) {
	deps := authEnabledDeps(t)
	router := NewRouter(deps)
	email, password := createTestUser(t, deps, auth.RoleAdministrator)
	token := loginAs(t, router, email, password)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/providers", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	// Deliberately no X-Sentinel-Csrf header.
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
}

func TestCSRFProtection_GETRequestsExempt(t *testing.T) {
	deps := authEnabledDeps(t)
	router := NewRouter(deps)
	email, password := createTestUser(t, deps, auth.RoleViewer)
	token := loginAs(t, router, email, password)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/projects", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code == http.StatusForbidden {
		t.Fatal("GET requests must be exempt from the CSRF header check")
	}
}

func TestRateLimit_BlocksAfterBurstExceeded(t *testing.T) {
	deps := newTestDeps(nil, nil)
	deps.RateLimitRPS = 1
	deps.RateLimitBurst = 3
	router := NewRouter(deps)

	var lastCode int
	for i := 0; i < 10; i++ {
		rec := doJSON(t, router, http.MethodGet, "/api/v1/projects", nil)
		lastCode = rec.Code
		if lastCode == http.StatusTooManyRequests {
			break
		}
	}
	if lastCode != http.StatusTooManyRequests {
		t.Fatalf("expected a 429 within 10 rapid requests at burst=3, last status = %d", lastCode)
	}
}

func TestRateLimit_GenerousDefaultDoesNotTripNormalUse(t *testing.T) {
	router := NewRouter(newTestDeps(nil, nil))
	for i := 0; i < 20; i++ {
		rec := doJSON(t, router, http.MethodGet, "/api/v1/projects", nil)
		if rec.Code == http.StatusTooManyRequests {
			t.Fatalf("request %d was rate-limited under default settings — burst is too low for normal use", i)
		}
	}
}
