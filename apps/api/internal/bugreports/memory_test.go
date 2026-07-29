package bugreports

import (
	"context"
	"testing"
	"time"
)

func TestUpsertFromFailure_CreatesNewOpenBug(t *testing.T) {
	store := NewMemoryStore()
	now := time.Now()

	bug, isNew, err := store.UpsertFromFailure(context.Background(), UpsertInput{
		ProjectID: "proj-1", FailureID: "fail-1", TestCaseID: "tc-1", Title: "boom",
		Severity: "high", FailureType: "network_failure", ObservedAt: now,
	})
	if err != nil {
		t.Fatalf("UpsertFromFailure() error: %v", err)
	}
	if !isNew {
		t.Error("isNew = false, want true for first occurrence")
	}
	if bug.Status != StatusOpen {
		t.Errorf("Status = %q, want %q", bug.Status, StatusOpen)
	}
	if bug.Frequency != 1 {
		t.Errorf("Frequency = %d, want 1", bug.Frequency)
	}
	if !bug.FirstObservedAt.Equal(now) || !bug.LastObservedAt.Equal(now) {
		t.Error("FirstObservedAt/LastObservedAt should both equal the observation time")
	}
}

func TestUpsertFromFailure_RepeatedFailureBumpsFrequency(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()
	first := time.Now()
	second := first.Add(time.Hour)

	store.UpsertFromFailure(ctx, UpsertInput{ProjectID: "proj-1", TestCaseID: "tc-1", FailureType: "network_failure", ObservedAt: first})
	bug, isNew, err := store.UpsertFromFailure(ctx, UpsertInput{ProjectID: "proj-1", TestCaseID: "tc-1", FailureType: "network_failure", ObservedAt: second})
	if err != nil {
		t.Fatalf("UpsertFromFailure() error: %v", err)
	}
	if isNew {
		t.Error("isNew = true, want false for a repeated failure of the same test+type")
	}
	if bug.Frequency != 2 {
		t.Errorf("Frequency = %d, want 2", bug.Frequency)
	}
	if !bug.LastObservedAt.Equal(second) {
		t.Error("LastObservedAt should advance to the latest observation")
	}
}

func TestUpsertFromFailure_ResolvedBugReopensOnRecurrence(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()

	bug, _, _ := store.UpsertFromFailure(ctx, UpsertInput{ProjectID: "proj-1", TestCaseID: "tc-1", FailureType: "network_failure", ObservedAt: time.Now()})
	if _, err := store.UpdateStatus(ctx, bug.ID, StatusResolved); err != nil {
		t.Fatalf("UpdateStatus() error: %v", err)
	}

	reopened, isNew, err := store.UpsertFromFailure(ctx, UpsertInput{ProjectID: "proj-1", TestCaseID: "tc-1", FailureType: "network_failure", ObservedAt: time.Now()})
	if err != nil {
		t.Fatalf("UpsertFromFailure() error: %v", err)
	}
	if isNew {
		t.Error("isNew = true, want false — this should reopen the existing bug, not create a new one")
	}
	if reopened.Status != StatusReopened {
		t.Errorf("Status = %q, want %q", reopened.Status, StatusReopened)
	}
}

func TestUpsertFromFailure_DifferentFailureTypeCreatesSeparateBug(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()

	store.UpsertFromFailure(ctx, UpsertInput{ProjectID: "proj-1", TestCaseID: "tc-1", FailureType: "network_failure", ObservedAt: time.Now()})
	_, isNew, err := store.UpsertFromFailure(ctx, UpsertInput{ProjectID: "proj-1", TestCaseID: "tc-1", FailureType: "assertion_failure", ObservedAt: time.Now()})
	if err != nil {
		t.Fatalf("UpsertFromFailure() error: %v", err)
	}
	if !isNew {
		t.Error("isNew = false, want true — a different failure_type on the same test is a distinct bug")
	}
}

func TestUpsertFromFailure_SetsPossibleDuplicateHintAcrossTestCases(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()

	first, _, _ := store.UpsertFromFailure(ctx, UpsertInput{ProjectID: "proj-1", TestCaseID: "tc-1", FailureType: "network_failure", ObservedAt: time.Now()})
	second, _, err := store.UpsertFromFailure(ctx, UpsertInput{ProjectID: "proj-1", TestCaseID: "tc-2", FailureType: "network_failure", ObservedAt: time.Now()})
	if err != nil {
		t.Fatalf("UpsertFromFailure() error: %v", err)
	}
	if second.PossibleDuplicateOfID != first.ID {
		t.Errorf("PossibleDuplicateOfID = %q, want %q", second.PossibleDuplicateOfID, first.ID)
	}
	// Still created as its own bug, not merged.
	if second.ID == first.ID {
		t.Error("a possible duplicate must still be its own bug row, not merged")
	}
}

func TestList_FiltersBySeverityStatusAndSearch(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()
	store.UpsertFromFailure(ctx, UpsertInput{ProjectID: "proj-1", TestCaseID: "tc-1", Title: "login is broken", Severity: "high", FailureType: "network_failure", ObservedAt: time.Now()})
	store.UpsertFromFailure(ctx, UpsertInput{ProjectID: "proj-1", TestCaseID: "tc-2", Title: "checkout fails", Severity: "low", FailureType: "assertion_failure", ObservedAt: time.Now()})

	highOnly, err := store.List(ctx, ListFilter{ProjectID: "proj-1", Severity: "high"})
	if err != nil {
		t.Fatalf("List() error: %v", err)
	}
	if len(highOnly) != 1 || highOnly[0].Title != "login is broken" {
		t.Fatalf("List(severity=high) = %+v, want just the login bug", highOnly)
	}

	searchResults, err := store.List(ctx, ListFilter{ProjectID: "proj-1", Search: "checkout"})
	if err != nil {
		t.Fatalf("List() error: %v", err)
	}
	if len(searchResults) != 1 || searchResults[0].Title != "checkout fails" {
		t.Fatalf("List(search=checkout) = %+v", searchResults)
	}
}

func TestUpdateStatus_RejectsInvalidStatus(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()
	bug, _, _ := store.UpsertFromFailure(ctx, UpsertInput{ProjectID: "proj-1", TestCaseID: "tc-1", FailureType: "network_failure", ObservedAt: time.Now()})

	if _, err := store.UpdateStatus(ctx, bug.ID, "not-a-real-status"); err != ErrInvalidStatus {
		t.Fatalf("UpdateStatus() error = %v, want ErrInvalidStatus", err)
	}
}

func TestUpdateStatus_NotFound(t *testing.T) {
	store := NewMemoryStore()
	if _, err := store.UpdateStatus(context.Background(), "does-not-exist", StatusResolved); err != ErrNotFound {
		t.Fatalf("UpdateStatus() error = %v, want ErrNotFound", err)
	}
}

func TestAddNote_AppendsAndPersists(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()
	bug, _, _ := store.UpsertFromFailure(ctx, UpsertInput{ProjectID: "proj-1", TestCaseID: "tc-1", FailureType: "network_failure", ObservedAt: time.Now()})

	updated, err := store.AddNote(ctx, bug.ID, "alice", "investigating")
	if err != nil {
		t.Fatalf("AddNote() error: %v", err)
	}
	if len(updated.Notes) != 1 || updated.Notes[0].Text != "investigating" {
		t.Fatalf("Notes = %+v", updated.Notes)
	}
}

func TestGet_NotFound(t *testing.T) {
	store := NewMemoryStore()
	if _, err := store.Get(context.Background(), "does-not-exist"); err != ErrNotFound {
		t.Fatalf("Get() error = %v, want ErrNotFound", err)
	}
}
