package httpserver

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"e2e-sentinel/apps/api/internal/auth"
)

// doAuthJSON is doJSON plus a bearer token header and the CSRF-defense
// header csrfProtection requires on mutating requests once auth is
// enabled (set unconditionally here since it's a no-op when auth is
// disabled).
func doAuthJSON(t *testing.T, router http.Handler, method, path, token string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatalf("encoding request body: %v", err)
		}
	}
	req := httptest.NewRequest(method, path, &buf)
	req.Header.Set(csrfCustomHeader, "1")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func authEnabledDeps(t *testing.T) Dependencies {
	t.Helper()
	deps := newTestDeps(nil, nil)
	deps.AuthEnabled = true
	return deps
}

// createTestUser creates a user with the given role and returns their
// credentials.
func createTestUser(t *testing.T, deps Dependencies, role string) (email, password string) {
	t.Helper()
	email = role + "@example.com"
	password = "correct-horse-battery-staple"
	hash, err := auth.HashPassword(password)
	if err != nil {
		t.Fatalf("HashPassword() error: %v", err)
	}
	if _, err := deps.Auth.CreateUser(t.Context(), auth.User{Email: email, PasswordHash: hash, Role: role}); err != nil {
		t.Fatalf("CreateUser() error: %v", err)
	}
	return email, password
}

func loginAs(t *testing.T, router http.Handler, email, password string) string {
	t.Helper()
	rec := doAuthJSON(t, router, http.MethodPost, "/api/v1/auth/login", "", map[string]string{"email": email, "password": password})
	if rec.Code != http.StatusOK {
		t.Fatalf("login status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		Token string `json:"token"`
	}
	json.Unmarshal(rec.Body.Bytes(), &body)
	if body.Token == "" {
		t.Fatal("login did not return a token")
	}
	return body.Token
}

func TestAuthStatus_ReturnsDisabledByDefault(t *testing.T) {
	router := NewRouter(newTestDeps(nil, nil))
	rec := doJSON(t, router, http.MethodGet, "/api/v1/auth/status", nil)
	var body struct {
		AuthEnabled bool `json:"auth_enabled"`
	}
	json.Unmarshal(rec.Body.Bytes(), &body)
	if body.AuthEnabled {
		t.Error("auth_enabled = true, want false by default")
	}
}

func TestAuthStatus_ReturnsEnabledWhenConfigured(t *testing.T) {
	router := NewRouter(authEnabledDeps(t))
	rec := doJSON(t, router, http.MethodGet, "/api/v1/auth/status", nil)
	var body struct {
		AuthEnabled bool `json:"auth_enabled"`
	}
	json.Unmarshal(rec.Body.Bytes(), &body)
	if !body.AuthEnabled {
		t.Error("auth_enabled = false, want true")
	}
}

func TestLogin_RequiresAuthEnabled(t *testing.T) {
	router := NewRouter(newTestDeps(nil, nil))
	rec := doAuthJSON(t, router, http.MethodPost, "/api/v1/auth/login", "", map[string]string{"email": "a@example.com", "password": "x"})
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
}

func TestLogin_SucceedsWithValidCredentials(t *testing.T) {
	deps := authEnabledDeps(t)
	router := NewRouter(deps)
	email, password := createTestUser(t, deps, auth.RoleViewer)

	token := loginAs(t, router, email, password)

	meRec := doAuthJSON(t, router, http.MethodGet, "/api/v1/auth/me", token, nil)
	if meRec.Code != http.StatusOK {
		t.Fatalf("me status = %d, body=%s", meRec.Code, meRec.Body.String())
	}
	var me userResponse
	json.Unmarshal(meRec.Body.Bytes(), &me)
	if me.Email != email {
		t.Errorf("Email = %q, want %q", me.Email, email)
	}
}

func TestLogin_WrongPasswordReturnsGenericError(t *testing.T) {
	deps := authEnabledDeps(t)
	router := NewRouter(deps)
	email, _ := createTestUser(t, deps, auth.RoleViewer)

	rec := doAuthJSON(t, router, http.MethodPost, "/api/v1/auth/login", "", map[string]string{"email": email, "password": "totally-wrong"})
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
	var body struct {
		Error string `json:"error"`
	}
	json.Unmarshal(rec.Body.Bytes(), &body)
	if body.Error != "invalid_credentials" {
		t.Errorf("error = %q", body.Error)
	}
}

func TestLogin_UnknownEmailReturnsSameGenericError(t *testing.T) {
	router := NewRouter(authEnabledDeps(t))
	rec := doAuthJSON(t, router, http.MethodPost, "/api/v1/auth/login", "", map[string]string{"email": "nobody@example.com", "password": "whatever12345"})
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
	var body struct {
		Error string `json:"error"`
	}
	json.Unmarshal(rec.Body.Bytes(), &body)
	if body.Error != "invalid_credentials" {
		t.Errorf("error = %q, want invalid_credentials (must not reveal the email doesn't exist)", body.Error)
	}
}

func TestRequireAuth_MissingTokenIsRejected(t *testing.T) {
	router := NewRouter(authEnabledDeps(t))
	rec := doJSON(t, router, http.MethodGet, "/api/v1/projects", nil)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

func TestRequireAuth_InvalidTokenIsRejected(t *testing.T) {
	router := NewRouter(authEnabledDeps(t))
	rec := doAuthJSON(t, router, http.MethodGet, "/api/v1/projects", "not-a-real-token", nil)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

func TestLogout_RevokesSession(t *testing.T) {
	deps := authEnabledDeps(t)
	router := NewRouter(deps)
	email, password := createTestUser(t, deps, auth.RoleViewer)
	token := loginAs(t, router, email, password)

	logoutRec := doAuthJSON(t, router, http.MethodPost, "/api/v1/auth/logout", token, nil)
	if logoutRec.Code != http.StatusOK {
		t.Fatalf("logout status = %d, body=%s", logoutRec.Code, logoutRec.Body.String())
	}

	afterRec := doAuthJSON(t, router, http.MethodGet, "/api/v1/auth/me", token, nil)
	if afterRec.Code != http.StatusUnauthorized {
		t.Fatalf("status after logout = %d, want 401 (session must be revoked)", afterRec.Code)
	}
}

func TestRequirePermission_ViewerCannotApproveTest(t *testing.T) {
	deps := authEnabledDeps(t)
	router := NewRouter(deps)
	email, password := createTestUser(t, deps, auth.RoleViewer)
	token := loginAs(t, router, email, password)

	rec := doAuthJSON(t, router, http.MethodPost, "/api/v1/tests/some-test-id/approve", token, nil)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 (a viewer must not be able to approve a test)", rec.Code)
	}
}

func TestRequirePermission_ApproverCanReachApproveTest(t *testing.T) {
	deps := authEnabledDeps(t)
	router := NewRouter(deps)
	email, password := createTestUser(t, deps, auth.RoleApprover)
	token := loginAs(t, router, email, password)

	// The test itself doesn't exist, so this should get past the
	// permission check (not 403) and fail later as 404 — proving the
	// permission gate let an Approver through.
	rec := doAuthJSON(t, router, http.MethodPost, "/api/v1/tests/some-test-id/approve", token, nil)
	if rec.Code == http.StatusForbidden {
		t.Fatalf("status = 403, want an approver to pass the permission check (got further error instead)")
	}
}

func TestRequirePermission_DeveloperCanGenerateTestsButNotApprove(t *testing.T) {
	deps := authEnabledDeps(t)
	router := NewRouter(deps)
	email, password := createTestUser(t, deps, auth.RoleDeveloper)
	token := loginAs(t, router, email, password)

	approveRec := doAuthJSON(t, router, http.MethodPost, "/api/v1/tests/some-test-id/approve", token, nil)
	if approveRec.Code != http.StatusForbidden {
		t.Fatalf("approve status = %d, want 403", approveRec.Code)
	}

	planRec := doAuthJSON(t, router, http.MethodPost, "/api/v1/projects/some-project-id/tests/plan", token, nil)
	if planRec.Code == http.StatusForbidden {
		t.Fatal("a developer must be able to reach test plan generation (got 403)")
	}
}

func TestAuthDisabled_SensitiveRoutesWorkWithoutAToken(t *testing.T) {
	// The default (AuthEnabled: false) must behave exactly like every
	// prior phase: no token required anywhere.
	router := NewRouter(newTestDeps(nil, nil))
	rec := doJSON(t, router, http.MethodPost, "/api/v1/tests/some-test-id/approve", nil)
	if rec.Code == http.StatusUnauthorized || rec.Code == http.StatusForbidden {
		t.Fatalf("status = %d, want auth to be a non-issue when disabled (expected a 404 for the missing test, not an auth error)", rec.Code)
	}
}
