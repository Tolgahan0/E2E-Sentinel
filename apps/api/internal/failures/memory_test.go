package failures

import (
	"context"
	"testing"
)

func TestMemoryStore_CreateAndGet(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()

	f, err := store.Create(ctx, Failure{TestRunID: "run-1", TestCaseID: "tc-1", Title: "boom", FailureType: TypeUnknown, Severity: SeverityMedium})
	if err != nil {
		t.Fatalf("Create() error: %v", err)
	}
	if f.ID == "" {
		t.Fatal("Create() returned empty ID")
	}

	got, err := store.Get(ctx, f.ID)
	if err != nil {
		t.Fatalf("Get() error: %v", err)
	}
	if got.Title != "boom" {
		t.Errorf("Title = %q, want boom", got.Title)
	}
}

func TestMemoryStore_GetNotFound(t *testing.T) {
	store := NewMemoryStore()
	if _, err := store.Get(context.Background(), "does-not-exist"); err != ErrNotFound {
		t.Fatalf("Get() error = %v, want ErrNotFound", err)
	}
}

func TestMemoryStore_ListByTestCaseOrdersOldestFirst(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()
	store.Create(ctx, Failure{TestRunID: "run-1", TestCaseID: "tc-1", Title: "first"})
	store.Create(ctx, Failure{TestRunID: "run-2", TestCaseID: "tc-1", Title: "second"})
	store.Create(ctx, Failure{TestRunID: "run-3", TestCaseID: "tc-2", Title: "other test case"})

	list, err := store.ListByTestCase(ctx, "tc-1")
	if err != nil {
		t.Fatalf("ListByTestCase() error: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("got %d failures, want 2", len(list))
	}
	if list[0].Title != "first" || list[1].Title != "second" {
		t.Errorf("order = [%q, %q], want [first, second]", list[0].Title, list[1].Title)
	}
}
