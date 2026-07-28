package secretstore

import (
	"context"
	"testing"
)

func TestMemoryStore_CreateResolveDelete(t *testing.T) {
	enc, err := NewEncryptor(validKey())
	if err != nil {
		t.Fatalf("NewEncryptor() error: %v", err)
	}
	store := NewMemoryStore(enc)
	ctx := context.Background()

	id, err := store.Create(ctx, "sk-my-key")
	if err != nil {
		t.Fatalf("Create() error: %v", err)
	}
	if id == "" {
		t.Fatal("Create() returned empty id")
	}

	got, err := store.Resolve(ctx, id)
	if err != nil {
		t.Fatalf("Resolve() error: %v", err)
	}
	if got != "sk-my-key" {
		t.Errorf("Resolve() = %q, want %q", got, "sk-my-key")
	}

	if err := store.Delete(ctx, id); err != nil {
		t.Fatalf("Delete() error: %v", err)
	}
	if _, err := store.Resolve(ctx, id); err != ErrNotFound {
		t.Fatalf("Resolve() after delete = %v, want ErrNotFound", err)
	}
}

func TestMemoryStore_ResolveUnknownID(t *testing.T) {
	enc, _ := NewEncryptor(validKey())
	store := NewMemoryStore(enc)

	if _, err := store.Resolve(context.Background(), "does-not-exist"); err != ErrNotFound {
		t.Fatalf("Resolve() = %v, want ErrNotFound", err)
	}
}

func TestMemoryStore_DeleteUnknownID(t *testing.T) {
	enc, _ := NewEncryptor(validKey())
	store := NewMemoryStore(enc)

	if err := store.Delete(context.Background(), "does-not-exist"); err != ErrNotFound {
		t.Fatalf("Delete() = %v, want ErrNotFound", err)
	}
}
