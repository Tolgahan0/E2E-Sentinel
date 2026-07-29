package kubediscovery

import (
	"context"
	"testing"
)

func TestMemoryStore_UpsertIsIdempotentByProjectNamespaceKindName(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()

	first, err := store.Upsert(ctx, Resource{ProjectID: "p1", Namespace: "default", Kind: KindDeployment, Name: "web", Status: StatusHealthy})
	if err != nil {
		t.Fatalf("Upsert() error: %v", err)
	}
	if first.ID == "" {
		t.Fatal("Upsert() did not assign an ID")
	}

	second, err := store.Upsert(ctx, Resource{ProjectID: "p1", Namespace: "default", Kind: KindDeployment, Name: "web", Status: StatusDegraded})
	if err != nil {
		t.Fatalf("second Upsert() error: %v", err)
	}
	if second.ID != first.ID {
		t.Errorf("ID changed across upserts: %q -> %q, want stable", first.ID, second.ID)
	}
	if second.Status != StatusDegraded {
		t.Errorf("Status = %q, want the second upsert's value to win", second.Status)
	}

	all, err := store.ListByProject(ctx, "p1")
	if err != nil {
		t.Fatalf("ListByProject() error: %v", err)
	}
	if len(all) != 1 {
		t.Fatalf("len = %d, want 1 (repeated discovery must update in place, not duplicate)", len(all))
	}
}

func TestMemoryStore_ListByProjectIsolatesProjects(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()
	store.Upsert(ctx, Resource{ProjectID: "p1", Namespace: "default", Kind: KindService, Name: "web"})
	store.Upsert(ctx, Resource{ProjectID: "p2", Namespace: "default", Kind: KindService, Name: "web"})

	p1, _ := store.ListByProject(ctx, "p1")
	if len(p1) != 1 {
		t.Fatalf("len(p1) = %d, want 1", len(p1))
	}

	p3, _ := store.ListByProject(ctx, "p3-does-not-exist")
	if len(p3) != 0 {
		t.Fatalf("len(p3) = %d, want 0", len(p3))
	}
}

func TestMemoryStore_DifferentNamespacesOrKindsDoNotCollide(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()
	store.Upsert(ctx, Resource{ProjectID: "p1", Namespace: "default", Kind: KindDeployment, Name: "web"})
	store.Upsert(ctx, Resource{ProjectID: "p1", Namespace: "staging", Kind: KindDeployment, Name: "web"})
	store.Upsert(ctx, Resource{ProjectID: "p1", Namespace: "default", Kind: KindService, Name: "web"})

	all, _ := store.ListByProject(ctx, "p1")
	if len(all) != 3 {
		t.Fatalf("len = %d, want 3 (namespace/kind are part of the identity key)", len(all))
	}
}
