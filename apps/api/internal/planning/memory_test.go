package planning

import (
	"context"
	"errors"
	"testing"
)

func TestMemoryStore_CreateIfAbsentDoesNotOverwriteExisting(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()

	first, created1, err := store.CreateIfAbsent(ctx, TestCase{ProjectID: "p1", NaturalKey: "smoke|/health", Title: "v1", ApprovalStatus: ApprovalPending})
	if err != nil {
		t.Fatalf("CreateIfAbsent() error: %v", err)
	}
	if !created1 {
		t.Fatal("expected created=true on first insert")
	}

	// Simulate the user approving it before a regeneration happens.
	if _, err := store.UpdateApproval(ctx, first.ID, ApprovalApproved); err != nil {
		t.Fatalf("UpdateApproval() error: %v", err)
	}

	second, created2, err := store.CreateIfAbsent(ctx, TestCase{ProjectID: "p1", NaturalKey: "smoke|/health", Title: "v2 (regenerated)", ApprovalStatus: ApprovalPending})
	if err != nil {
		t.Fatalf("second CreateIfAbsent() error: %v", err)
	}
	if created2 {
		t.Fatal("expected created=false: regenerating must not overwrite an existing test case")
	}
	if second.Title != "v1" {
		t.Errorf("Title = %q, want v1 (the user-approved version must survive regeneration)", second.Title)
	}
	if second.ApprovalStatus != ApprovalApproved {
		t.Errorf("ApprovalStatus = %q, want approved (must not be reset by regeneration)", second.ApprovalStatus)
	}
}

func TestMemoryStore_UpdateApprovalTransitionsStatus(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()
	created, _, _ := store.CreateIfAbsent(ctx, TestCase{ProjectID: "p1", NaturalKey: "k1", Status: StatusSuggested, ApprovalStatus: ApprovalPending})

	updated, err := store.UpdateApproval(ctx, created.ID, ApprovalApproved)
	if err != nil {
		t.Fatalf("UpdateApproval() error: %v", err)
	}
	if updated.Status != StatusApproved {
		t.Errorf("Status = %q, want approved after approval", updated.Status)
	}

	if _, err := store.UpdateApproval(ctx, "missing", ApprovalApproved); !errors.Is(err, ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

func TestMemoryStore_UpdateEditsFieldsSelectively(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()
	created, _, _ := store.CreateIfAbsent(ctx, TestCase{ProjectID: "p1", NaturalKey: "k1", Title: "Old title", Priority: PriorityP2})

	updated, err := store.Update(ctx, created.ID, "New title", "", "")
	if err != nil {
		t.Fatalf("Update() error: %v", err)
	}
	if updated.Title != "New title" {
		t.Errorf("Title = %q, want New title", updated.Title)
	}
	if updated.Priority != PriorityP2 {
		t.Errorf("Priority = %q, want unchanged P2 (empty string means no change)", updated.Priority)
	}

	updated2, err := store.Update(ctx, created.ID, "", "", PriorityP0)
	if err != nil {
		t.Fatalf("Update() error: %v", err)
	}
	if updated2.Priority != PriorityP0 {
		t.Errorf("Priority = %q, want P0", updated2.Priority)
	}
	if updated2.Title != "New title" {
		t.Errorf("Title = %q, want unchanged New title", updated2.Title)
	}
}

func TestMemoryStore_ListFiltersByProject(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()
	store.CreateIfAbsent(ctx, TestCase{ProjectID: "p1", NaturalKey: "k1"})
	store.CreateIfAbsent(ctx, TestCase{ProjectID: "p2", NaturalKey: "k1"})

	list, err := store.List(ctx, "p1")
	if err != nil {
		t.Fatalf("List() error: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("got %d test cases, want 1", len(list))
	}
}
