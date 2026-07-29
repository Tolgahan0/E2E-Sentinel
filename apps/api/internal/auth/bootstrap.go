package auth

import (
	"context"
	"fmt"
)

// EnsureBootstrapAdmin creates the first administrator account if (and
// only if) no users exist yet — spec §19 "MVP local mode may support a
// bootstrap administrator". It is a no-op once any user exists, so it's
// safe to call on every startup.
func EnsureBootstrapAdmin(ctx context.Context, store Store, email, password string) (created bool, err error) {
	count, err := store.CountUsers(ctx)
	if err != nil {
		return false, fmt.Errorf("auth: counting users: %w", err)
	}
	if count > 0 {
		return false, nil
	}
	if email == "" || password == "" {
		return false, fmt.Errorf("auth: SENTINEL_ADMIN_EMAIL and SENTINEL_ADMIN_PASSWORD must both be set to bootstrap the first administrator")
	}

	hash, err := HashPassword(password)
	if err != nil {
		return false, err
	}
	if _, err := store.CreateUser(ctx, User{Email: email, PasswordHash: hash, Role: RoleAdministrator}); err != nil {
		return false, fmt.Errorf("auth: creating bootstrap administrator: %w", err)
	}
	return true, nil
}
