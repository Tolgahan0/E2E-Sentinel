package visualdiff

import (
	"context"
	"testing"
)

func TestMemoryStore_GetBaseline_NotFoundInitially(t *testing.T) {
	s := NewMemoryStore()
	if _, err := s.GetBaseline(context.Background(), "tc-1"); err != ErrNotFound {
		t.Errorf("GetBaseline() error = %v, want ErrNotFound", err)
	}
}

func TestMemoryStore_SetBaseline_ThenGetReturnsIt(t *testing.T) {
	s := NewMemoryStore()
	ctx := context.Background()

	created, err := s.SetBaseline(ctx, "tc-1", "artifact-1", "user")
	if err != nil {
		t.Fatalf("SetBaseline() error = %v", err)
	}

	got, err := s.GetBaseline(ctx, "tc-1")
	if err != nil {
		t.Fatalf("GetBaseline() error = %v", err)
	}
	if got.ArtifactID != "artifact-1" || got.ID != created.ID {
		t.Errorf("GetBaseline() = %+v, want ArtifactID=artifact-1 ID=%s", got, created.ID)
	}
}

func TestMemoryStore_SetBaseline_ReplacesExistingRow(t *testing.T) {
	s := NewMemoryStore()
	ctx := context.Background()

	first, _ := s.SetBaseline(ctx, "tc-1", "artifact-1", "user")
	second, err := s.SetBaseline(ctx, "tc-1", "artifact-2", "user")
	if err != nil {
		t.Fatalf("SetBaseline() error = %v", err)
	}
	if second.ID != first.ID {
		t.Errorf("replacing a baseline changed its ID: %s -> %s, want the same row updated", first.ID, second.ID)
	}
	if second.ArtifactID != "artifact-2" {
		t.Errorf("ArtifactID = %q, want artifact-2", second.ArtifactID)
	}
}

func TestMemoryStore_ListByProject_PendingReviewFirst(t *testing.T) {
	s := NewMemoryStore()
	ctx := context.Background()

	accepted, _ := s.CreateDiff(ctx, Diff{ProjectID: "p1", Status: StatusAccepted})
	pending, _ := s.CreateDiff(ctx, Diff{ProjectID: "p1", Status: StatusPendingReview})
	_, _ = s.CreateDiff(ctx, Diff{ProjectID: "p2", Status: StatusPendingReview}) // different project

	list, err := s.ListByProject(ctx, "p1")
	if err != nil {
		t.Fatalf("ListByProject() error = %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("len(list) = %d, want 2 (project-scoped)", len(list))
	}
	if list[0].ID != pending.ID {
		t.Errorf("list[0].ID = %s, want the pending_review diff (%s) first", list[0].ID, pending.ID)
	}
	if list[1].ID != accepted.ID {
		t.Errorf("list[1].ID = %s, want the accepted diff (%s) second", list[1].ID, accepted.ID)
	}
}

func TestMemoryStore_UpdateStatus(t *testing.T) {
	s := NewMemoryStore()
	ctx := context.Background()

	d, _ := s.CreateDiff(ctx, Diff{ProjectID: "p1", Status: StatusPendingReview})

	updated, err := s.UpdateStatus(ctx, d.ID, StatusAccepted, "user")
	if err != nil {
		t.Fatalf("UpdateStatus() error = %v", err)
	}
	if updated.Status != StatusAccepted {
		t.Errorf("Status = %q, want accepted", updated.Status)
	}
	if updated.ReviewedBy == nil || *updated.ReviewedBy != "user" {
		t.Errorf("ReviewedBy = %v, want \"user\"", updated.ReviewedBy)
	}
	if updated.ReviewedAt == nil {
		t.Error("ReviewedAt is nil, want it set")
	}
}

func TestMemoryStore_Get_NotFound(t *testing.T) {
	s := NewMemoryStore()
	if _, err := s.Get(context.Background(), "does-not-exist"); err != ErrNotFound {
		t.Errorf("Get() error = %v, want ErrNotFound", err)
	}
}
