package httpserver

import (
	"encoding/json"
	"net/http"
	"testing"

	"e2e-sentinel/apps/api/internal/auth"
)

func TestCreateUser_RequiresManageUsersPermission(t *testing.T) {
	deps := authEnabledDeps(t)
	router := NewRouter(deps)
	email, password := createTestUser(t, deps, auth.RoleDeveloper)
	token := loginAs(t, router, email, password)

	rec := doAuthJSON(t, router, http.MethodPost, "/api/v1/users", token, map[string]string{
		"email": "new@example.com", "password": "correct-horse-battery-staple", "role": auth.RoleViewer,
	})
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 (a developer must not be able to create users)", rec.Code)
	}
}

func TestCreateUser_AdministratorCanCreateAndListUsers(t *testing.T) {
	deps := authEnabledDeps(t)
	router := NewRouter(deps)
	adminEmail, adminPassword := createTestUser(t, deps, auth.RoleAdministrator)
	token := loginAs(t, router, adminEmail, adminPassword)

	createRec := doAuthJSON(t, router, http.MethodPost, "/api/v1/users", token, map[string]string{
		"email": "new-tester@example.com", "password": "correct-horse-battery-staple", "role": auth.RoleTester,
	})
	if createRec.Code != http.StatusCreated {
		t.Fatalf("create status = %d, body=%s", createRec.Code, createRec.Body.String())
	}
	var created userResponse
	json.Unmarshal(createRec.Body.Bytes(), &created)
	if created.Email != "new-tester@example.com" || created.Role != auth.RoleTester {
		t.Fatalf("created = %+v", created)
	}

	listRec := doAuthJSON(t, router, http.MethodGet, "/api/v1/users", token, nil)
	if listRec.Code != http.StatusOK {
		t.Fatalf("list status = %d, body=%s", listRec.Code, listRec.Body.String())
	}
	var listBody struct {
		Users []userResponse `json:"users"`
	}
	json.Unmarshal(listRec.Body.Bytes(), &listBody)
	if len(listBody.Users) != 2 {
		t.Fatalf("users listed = %d, want 2 (bootstrap admin + new tester)", len(listBody.Users))
	}

	loginRec := doAuthJSON(t, router, http.MethodPost, "/api/v1/auth/login", "", map[string]string{
		"email": "new-tester@example.com", "password": "correct-horse-battery-staple",
	})
	if loginRec.Code != http.StatusOK {
		t.Fatalf("new user cannot log in: status = %d, body=%s", loginRec.Code, loginRec.Body.String())
	}
}

func TestCreateUser_RejectsDuplicateEmail(t *testing.T) {
	deps := authEnabledDeps(t)
	router := NewRouter(deps)
	adminEmail, adminPassword := createTestUser(t, deps, auth.RoleAdministrator)
	token := loginAs(t, router, adminEmail, adminPassword)

	rec := doAuthJSON(t, router, http.MethodPost, "/api/v1/users", token, map[string]string{
		"email": adminEmail, "password": "correct-horse-battery-staple", "role": auth.RoleViewer,
	})
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409", rec.Code)
	}
}

func TestCreateUser_RejectsInvalidRole(t *testing.T) {
	deps := authEnabledDeps(t)
	router := NewRouter(deps)
	adminEmail, adminPassword := createTestUser(t, deps, auth.RoleAdministrator)
	token := loginAs(t, router, adminEmail, adminPassword)

	rec := doAuthJSON(t, router, http.MethodPost, "/api/v1/users", token, map[string]string{
		"email": "x@example.com", "password": "correct-horse-battery-staple", "role": "not-a-role",
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestCreateUser_RejectsShortPassword(t *testing.T) {
	deps := authEnabledDeps(t)
	router := NewRouter(deps)
	adminEmail, adminPassword := createTestUser(t, deps, auth.RoleAdministrator)
	token := loginAs(t, router, adminEmail, adminPassword)

	rec := doAuthJSON(t, router, http.MethodPost, "/api/v1/users", token, map[string]string{
		"email": "x@example.com", "password": "short", "role": auth.RoleViewer,
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestListUsers_WorksWhenAuthDisabled(t *testing.T) {
	// requirePermission is a no-op when auth is disabled, matching every
	// other permission-gated route in Phases 0-8.
	router := NewRouter(newTestDeps(nil, nil))
	rec := doJSON(t, router, http.MethodGet, "/api/v1/users", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
}
