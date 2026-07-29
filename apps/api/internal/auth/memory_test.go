package auth

import (
	"context"
	"testing"
	"time"
)

func TestMemoryStore_CreateUserAndGet(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()

	u, err := store.CreateUser(ctx, User{Email: "admin@example.com", PasswordHash: "hash", Role: RoleAdministrator})
	if err != nil {
		t.Fatalf("CreateUser() error: %v", err)
	}
	if u.ID == "" {
		t.Fatal("CreateUser() did not assign an ID")
	}

	byEmail, err := store.GetUserByEmail(ctx, "ADMIN@EXAMPLE.COM")
	if err != nil {
		t.Fatalf("GetUserByEmail() error: %v (email lookup should be case-insensitive)", err)
	}
	if byEmail.ID != u.ID {
		t.Errorf("GetUserByEmail() returned a different user")
	}

	byID, err := store.GetUserByID(ctx, u.ID)
	if err != nil {
		t.Fatalf("GetUserByID() error: %v", err)
	}
	if byID.Email != "admin@example.com" {
		t.Errorf("Email = %q", byID.Email)
	}
}

func TestMemoryStore_CreateUserRejectsInvalidRole(t *testing.T) {
	store := NewMemoryStore()
	if _, err := store.CreateUser(context.Background(), User{Email: "x@example.com", Role: "not-a-role"}); err != ErrInvalidRole {
		t.Fatalf("CreateUser() error = %v, want ErrInvalidRole", err)
	}
}

func TestMemoryStore_CreateUserRejectsDuplicateEmail(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()
	store.CreateUser(ctx, User{Email: "dup@example.com", Role: RoleViewer})
	if _, err := store.CreateUser(ctx, User{Email: "dup@example.com", Role: RoleViewer}); err != ErrEmailTaken {
		t.Fatalf("CreateUser() error = %v, want ErrEmailTaken", err)
	}
}

func TestMemoryStore_GetUserByEmailNotFound(t *testing.T) {
	store := NewMemoryStore()
	if _, err := store.GetUserByEmail(context.Background(), "nobody@example.com"); err != ErrNotFound {
		t.Fatalf("error = %v, want ErrNotFound", err)
	}
}

func TestMemoryStore_CountUsers(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()
	count, _ := store.CountUsers(ctx)
	if count != 0 {
		t.Fatalf("CountUsers() = %d, want 0", count)
	}
	store.CreateUser(ctx, User{Email: "a@example.com", Role: RoleViewer})
	count, _ = store.CountUsers(ctx)
	if count != 1 {
		t.Fatalf("CountUsers() = %d, want 1", count)
	}
}

func TestMemoryStore_ListUsersOrdersByEmail(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()
	store.CreateUser(ctx, User{Email: "zed@example.com", Role: RoleViewer})
	store.CreateUser(ctx, User{Email: "amy@example.com", Role: RoleAdministrator})

	users, err := store.ListUsers(ctx)
	if err != nil {
		t.Fatalf("ListUsers() error: %v", err)
	}
	if len(users) != 2 {
		t.Fatalf("ListUsers() returned %d users, want 2", len(users))
	}
	if users[0].Email != "amy@example.com" || users[1].Email != "zed@example.com" {
		t.Fatalf("ListUsers() order = [%q, %q], want alphabetical by email", users[0].Email, users[1].Email)
	}
}

func TestMemoryStore_SessionLifecycle(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()
	u, _ := store.CreateUser(ctx, User{Email: "a@example.com", Role: RoleViewer})

	token, hash, _ := GenerateToken()
	sess, err := store.CreateSession(ctx, Session{UserID: u.ID, TokenHash: hash, ExpiresAt: time.Now().Add(time.Hour)})
	if err != nil {
		t.Fatalf("CreateSession() error: %v", err)
	}

	got, err := store.GetSessionByTokenHash(ctx, HashToken(token))
	if err != nil {
		t.Fatalf("GetSessionByTokenHash() error: %v", err)
	}
	if got.UserID != u.ID {
		t.Errorf("UserID = %q, want %q", got.UserID, u.ID)
	}

	if err := store.DeleteSession(ctx, sess.ID); err != nil {
		t.Fatalf("DeleteSession() error: %v", err)
	}
	if _, err := store.GetSessionByTokenHash(ctx, HashToken(token)); err != ErrSessionNotFound {
		t.Fatalf("error after delete = %v, want ErrSessionNotFound", err)
	}
}

func TestMemoryStore_ExpiredSessionIsRejected(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()
	u, _ := store.CreateUser(ctx, User{Email: "a@example.com", Role: RoleViewer})

	token, hash, _ := GenerateToken()
	store.CreateSession(ctx, Session{UserID: u.ID, TokenHash: hash, ExpiresAt: time.Now().Add(-time.Hour)})

	if _, err := store.GetSessionByTokenHash(ctx, HashToken(token)); err != ErrSessionExpired {
		t.Fatalf("error = %v, want ErrSessionExpired", err)
	}
}

func TestMemoryStore_UnknownSessionToken(t *testing.T) {
	store := NewMemoryStore()
	if _, err := store.GetSessionByTokenHash(context.Background(), "does-not-exist"); err != ErrSessionNotFound {
		t.Fatalf("error = %v, want ErrSessionNotFound", err)
	}
}
