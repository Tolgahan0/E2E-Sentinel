package auth

import (
	"context"
	"testing"
)

func TestEnsureBootstrapAdmin_CreatesAdminWhenNoUsersExist(t *testing.T) {
	store := NewMemoryStore()
	created, err := EnsureBootstrapAdmin(context.Background(), store, "admin@example.com", "correct-horse-battery-staple")
	if err != nil {
		t.Fatalf("EnsureBootstrapAdmin() error: %v", err)
	}
	if !created {
		t.Fatal("created = false, want true")
	}

	u, err := store.GetUserByEmail(context.Background(), "admin@example.com")
	if err != nil {
		t.Fatalf("GetUserByEmail() error: %v", err)
	}
	if u.Role != RoleAdministrator {
		t.Errorf("Role = %q, want %q", u.Role, RoleAdministrator)
	}
	if err := VerifyPassword(u.PasswordHash, "correct-horse-battery-staple"); err != nil {
		t.Errorf("VerifyPassword() error: %v", err)
	}
}

func TestEnsureBootstrapAdmin_NoOpWhenUsersAlreadyExist(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()
	store.CreateUser(ctx, User{Email: "existing@example.com", Role: RoleViewer})

	created, err := EnsureBootstrapAdmin(ctx, store, "admin@example.com", "correct-horse-battery-staple")
	if err != nil {
		t.Fatalf("EnsureBootstrapAdmin() error: %v", err)
	}
	if created {
		t.Fatal("created = true, want false — must not create a second admin once users exist")
	}
	if _, err := store.GetUserByEmail(ctx, "admin@example.com"); err != ErrNotFound {
		t.Fatalf("the bootstrap admin should not have been created; GetUserByEmail() error = %v", err)
	}
}

func TestEnsureBootstrapAdmin_RequiresBothEmailAndPassword(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()
	if _, err := EnsureBootstrapAdmin(ctx, store, "", "correct-horse-battery-staple"); err == nil {
		t.Error("expected an error when email is empty")
	}
	if _, err := EnsureBootstrapAdmin(ctx, store, "admin@example.com", ""); err == nil {
		t.Error("expected an error when password is empty")
	}
}
