package fixproposals

import (
	"context"
	"testing"
	"time"
)

func TestMemoryStore_CreateDefaultsToPendingReview(t *testing.T) {
	store := NewMemoryStore()
	fp, err := store.Create(context.Background(), FixProposal{ProjectID: "p1", BugID: "b1", Title: "Fix login", UnifiedDiff: "---\n+++\n"})
	if err != nil {
		t.Fatalf("Create() error: %v", err)
	}
	if fp.ApprovalStatus != StatusPendingReview {
		t.Errorf("ApprovalStatus = %q, want %q", fp.ApprovalStatus, StatusPendingReview)
	}
	if fp.ID == "" {
		t.Fatal("Create() did not assign an ID")
	}
}

func TestMemoryStore_GetNotFound(t *testing.T) {
	store := NewMemoryStore()
	if _, err := store.Get(context.Background(), "does-not-exist"); err != ErrNotFound {
		t.Fatalf("Get() error = %v, want ErrNotFound", err)
	}
}

func TestMemoryStore_ListByProjectAndBug(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()
	store.Create(ctx, FixProposal{ProjectID: "p1", BugID: "b1", Title: "x"})
	store.Create(ctx, FixProposal{ProjectID: "p1", BugID: "b2", Title: "y"})
	store.Create(ctx, FixProposal{ProjectID: "p2", BugID: "b3", Title: "z"})

	byProject, err := store.ListByProject(ctx, "p1")
	if err != nil {
		t.Fatalf("ListByProject() error: %v", err)
	}
	if len(byProject) != 2 {
		t.Fatalf("got %d proposals for p1, want 2", len(byProject))
	}

	byBug, err := store.ListByBug(ctx, "b2")
	if err != nil {
		t.Fatalf("ListByBug() error: %v", err)
	}
	if len(byBug) != 1 || byBug[0].Title != "y" {
		t.Fatalf("ListByBug(b2) = %+v", byBug)
	}
}

func TestMemoryStore_UpdateApprovalStatus(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()
	fp, _ := store.Create(ctx, FixProposal{ProjectID: "p1", BugID: "b1"})

	updated, err := store.UpdateApprovalStatus(ctx, fp.ID, StatusApproved)
	if err != nil {
		t.Fatalf("UpdateApprovalStatus() error: %v", err)
	}
	if updated.ApprovalStatus != StatusApproved {
		t.Errorf("ApprovalStatus = %q, want %q", updated.ApprovalStatus, StatusApproved)
	}
}

func TestMemoryStore_UpdateApprovalStatus_RejectsInvalid(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()
	fp, _ := store.Create(ctx, FixProposal{ProjectID: "p1", BugID: "b1"})

	if _, err := store.UpdateApprovalStatus(ctx, fp.ID, "not-a-real-status"); err != ErrInvalidStatus {
		t.Fatalf("UpdateApprovalStatus() error = %v, want ErrInvalidStatus", err)
	}
}

func TestMemoryStore_UpdateRegressionTestIDs(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()
	fp, _ := store.Create(ctx, FixProposal{ProjectID: "p1", BugID: "b1"})

	updated, err := store.UpdateRegressionTestIDs(ctx, fp.ID, []string{"tc-1", "tc-2"})
	if err != nil {
		t.Fatalf("UpdateRegressionTestIDs() error: %v", err)
	}
	if len(updated.RegressionTestIDs) != 2 {
		t.Errorf("RegressionTestIDs = %v", updated.RegressionTestIDs)
	}
}

func TestMemoryStore_RecordWorkspaceApplication(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()
	fp, _ := store.Create(ctx, FixProposal{ProjectID: "p1", BugID: "b1"})

	now := time.Now()
	updated, err := store.RecordWorkspaceApplication(ctx, fp.ID, "/tmp/fix-123", []FileResult{{Path: "main.go", Action: "modified", Applied: true}}, now)
	if err != nil {
		t.Fatalf("RecordWorkspaceApplication() error: %v", err)
	}
	if updated.WorkspaceDir != "/tmp/fix-123" {
		t.Errorf("WorkspaceDir = %q", updated.WorkspaceDir)
	}
	if updated.WorkspaceAppliedAt == nil || !updated.WorkspaceAppliedAt.Equal(now) {
		t.Error("WorkspaceAppliedAt was not recorded")
	}
	if len(updated.WorkspaceApplyResults) != 1 {
		t.Errorf("WorkspaceApplyResults = %+v", updated.WorkspaceApplyResults)
	}
}

func TestMemoryStore_RecordRepositoryApplication(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()
	fp, _ := store.Create(ctx, FixProposal{ProjectID: "p1", BugID: "b1"})

	now := time.Now()
	updated, err := store.RecordRepositoryApplication(ctx, fp.ID, []FileResult{{Path: "main.go", Action: "modified", Applied: true}}, now)
	if err != nil {
		t.Fatalf("RecordRepositoryApplication() error: %v", err)
	}
	if updated.RepositoryAppliedAt == nil {
		t.Fatal("RepositoryAppliedAt was not recorded")
	}
}

func TestMemoryStore_RecordRepositoryApplication_RejectsSecondApplication(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()
	fp, _ := store.Create(ctx, FixProposal{ProjectID: "p1", BugID: "b1"})

	now := time.Now()
	if _, err := store.RecordRepositoryApplication(ctx, fp.ID, nil, now); err != nil {
		t.Fatalf("first RecordRepositoryApplication() error: %v", err)
	}
	if _, err := store.RecordRepositoryApplication(ctx, fp.ID, nil, now); err != ErrAlreadyAppliedToRepository {
		t.Fatalf("second RecordRepositoryApplication() error = %v, want ErrAlreadyAppliedToRepository", err)
	}
}
