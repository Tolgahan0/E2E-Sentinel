// Package auth implements role-based access control (spec §19). It is
// opt-in: SENTINEL_AUTH_ENABLED defaults to false, so every existing
// deployment and API behavior from Phases 0-8 is unchanged unless an
// operator explicitly turns it on — the same "safe default, explicit
// capability" pattern already used for the Docker socket mount, secret
// encryption, and test execution. Architecture is ready for OIDC/SAML
// (spec §19) via the Authenticator interface in session.go, but only
// local email/password authentication is implemented.
package auth

import (
	"context"
	"errors"
	"time"
)

// Roles (spec §19). Each role has a fixed set of permissions — see
// RolePermissions in permission.go. There is no dynamic per-role
// permission editing in this MVP; changing what a role can do means
// changing that map, not a database row.
const (
	RoleViewer        = "viewer"
	RoleTester        = "tester"
	RoleDeveloper     = "developer"
	RoleApprover      = "approver"
	RoleAdministrator = "administrator"
)

var validRoles = map[string]bool{
	RoleViewer: true, RoleTester: true, RoleDeveloper: true,
	RoleApprover: true, RoleAdministrator: true,
}

// ValidRole reports whether r is a recognized role.
func ValidRole(r string) bool {
	return validRoles[r]
}

// User is a local account (spec §20 "users" table).
type User struct {
	ID           string
	Email        string
	PasswordHash string
	Role         string
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// ErrNotFound is returned when a user ID/email does not exist.
var ErrNotFound = errors.New("auth: user not found")

// ErrEmailTaken is returned when creating a user with an email already
// in use.
var ErrEmailTaken = errors.New("auth: email already in use")

// ErrInvalidRole is returned for an unrecognized role value.
var ErrInvalidRole = errors.New("auth: invalid role")

// Store persists users and sessions.
type Store interface {
	CreateUser(ctx context.Context, u User) (User, error)
	GetUserByEmail(ctx context.Context, email string) (User, error)
	GetUserByID(ctx context.Context, id string) (User, error)
	CountUsers(ctx context.Context) (int, error)
	ListUsers(ctx context.Context) ([]User, error)

	CreateSession(ctx context.Context, s Session) (Session, error)
	GetSessionByTokenHash(ctx context.Context, tokenHash string) (Session, error)
	DeleteSession(ctx context.Context, id string) error
}
