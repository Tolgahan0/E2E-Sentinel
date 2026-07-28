package runs

import (
	"context"
	"errors"
	"testing"
)

func TestMemoryStore_CreateAndGet(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()

	created, err := store.Create(ctx, TestRun{ProjectID: "p1", TestCaseID: "t1", Status: StatusQueued, RunnerType: "playwright-docker"})
	if err != nil {
		t.Fatalf("Create() error: %v", err)
	}
	if created.ID == "" {
		t.Fatal("Create() did not assign an ID")
	}

	got, err := store.Get(ctx, created.ID)
	if err != nil {
		t.Fatalf("Get() error: %v", err)
	}
	if got.Status != StatusQueued {
		t.Errorf("Status = %q, want queued", got.Status)
	}

	if _, err := store.Get(ctx, "missing"); !errors.Is(err, ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

func TestMemoryStore_UpdateStatusSetsFinishedAtOnlyWhenFinished(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()
	created, _ := store.Create(ctx, TestRun{ProjectID: "p1", TestCaseID: "t1", Status: StatusQueued})

	running, err := store.UpdateStatus(ctx, created.ID, StatusRunning, nil, "", false)
	if err != nil {
		t.Fatalf("UpdateStatus() error: %v", err)
	}
	if running.FinishedAt != nil {
		t.Error("FinishedAt should be nil while still running")
	}

	exitCode := 0
	passed, err := store.UpdateStatus(ctx, created.ID, StatusPassed, &exitCode, "1 passed", true)
	if err != nil {
		t.Fatalf("UpdateStatus() error: %v", err)
	}
	if passed.FinishedAt == nil {
		t.Error("FinishedAt should be set once finished")
	}
	if passed.ExitCode == nil || *passed.ExitCode != 0 {
		t.Errorf("ExitCode = %v, want 0", passed.ExitCode)
	}
}

func TestMemoryStore_ListByProjectFilters(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()
	store.Create(ctx, TestRun{ProjectID: "p1", TestCaseID: "t1"})
	store.Create(ctx, TestRun{ProjectID: "p2", TestCaseID: "t2"})

	list, err := store.ListByProject(ctx, "p1")
	if err != nil {
		t.Fatalf("ListByProject() error: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("got %d runs, want 1", len(list))
	}
}
