// Package visualdiff adds visual regression testing: a full-page
// screenshot from every browser-based run (internal/runs' Playwright
// config now captures one on every run, not just failures) diffed
// against a stored baseline for that test case. A diff is never a
// verdict — pass/fail still comes only from the runner's exit code
// (spec §2.4's rule is unchanged) — it's a signal a human explicitly
// accepts (making it the new baseline) or ignores.
package visualdiff

import (
	"context"
	"errors"
	"time"
)

// Diff statuses.
const (
	StatusPendingReview = "pending_review"
	StatusAccepted      = "accepted"
	StatusIgnored       = "ignored"
)

// Baseline is the current "expected" screenshot for a test case — at
// most one per test case (the table enforces this with a UNIQUE
// constraint). ArtifactID points at an existing internal/artifacts row;
// the bytes are never duplicated.
type Baseline struct {
	ID         string
	TestCaseID string
	ArtifactID string
	AcceptedBy string
	AcceptedAt time.Time
}

// Diff is one run's screenshot compared against the baseline that
// existed at the time. BaselineArtifactID/CurrentArtifactID/
// DiffArtifactID all point at internal/artifacts rows — this package
// never stores image bytes itself, only these table-shaped pointers to
// them plus the computed percentage and review status.
type Diff struct {
	ID                 string
	ProjectID          string
	TestRunID          string
	TestCaseID         string
	BaselineArtifactID string
	CurrentArtifactID  string
	DiffArtifactID     string
	PercentChanged     float64
	Status             string
	ReviewedBy         *string
	ReviewedAt         *time.Time
	CreatedAt          time.Time
}

// ErrNotFound is returned when a baseline or diff ID doesn't exist.
var ErrNotFound = errors.New("visualdiff: not found")

// Store persists baselines and diffs.
type Store interface {
	// GetBaseline returns ErrNotFound if testCaseID has none yet — the
	// caller (internal/httpserver's executeRunAsync) treats that as
	// "this run's screenshot becomes the baseline, nothing to diff."
	GetBaseline(ctx context.Context, testCaseID string) (Baseline, error)
	// SetBaseline creates or replaces testCaseID's baseline (the UNIQUE
	// constraint on test_case_id makes this an upsert).
	SetBaseline(ctx context.Context, testCaseID, artifactID, acceptedBy string) (Baseline, error)
	CreateDiff(ctx context.Context, d Diff) (Diff, error)
	Get(ctx context.Context, id string) (Diff, error)
	// ListByProject returns pending_review diffs first (newest first
	// within each group), then everything else — a caller only wants
	// the review queue at the top.
	ListByProject(ctx context.Context, projectID string) ([]Diff, error)
	// UpdateStatus never touches the baseline itself — the caller
	// decides separately whether accepting also calls SetBaseline.
	UpdateStatus(ctx context.Context, id, status, reviewedBy string) (Diff, error)
}
