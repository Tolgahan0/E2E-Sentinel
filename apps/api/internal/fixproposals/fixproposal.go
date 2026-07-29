package fixproposals

import (
	"context"
	"errors"
	"time"
)

// Risk levels (spec §15.1 "Risk rating").
const (
	RiskLow    = "low"
	RiskMedium = "medium"
	RiskHigh   = "high"
)

// Approval statuses (spec §15.2).
const (
	StatusPendingReview     = "pending_review"
	StatusApproved          = "approved"
	StatusRejected          = "rejected"
	StatusRevisionRequested = "revision_requested"
)

// ValidStatus reports whether s is a recognized approval status.
func ValidStatus(s string) bool {
	switch s {
	case StatusPendingReview, StatusApproved, StatusRejected, StatusRevisionRequested:
		return true
	}
	return false
}

// FixProposal is a candidate patch for a bug report (spec §6.9, §15.1).
// It is immutable once created except for its approval status,
// regression test selection, and the workspace/repository application
// records — the stored UnifiedDiff is exactly what gets reviewed,
// approved, and (only after that) applied, so there is never a gap
// between what a human approved and what gets written.
type FixProposal struct {
	ID        string
	ProjectID string
	BugID     string

	Title                string
	Description          string // "Explanation" (spec §6.9)
	RiskLevel            string
	Assumptions          string
	PotentialSideEffects string
	RollbackGuidance     string

	FilesChanged []string
	UnifiedDiff  string

	RegressionTestIDs []string

	// AIProvider/AIModel are empty for a manually-authored proposal —
	// spec §15.1 "AI provider and model" is only meaningful when one
	// was actually used.
	AIProvider  string
	AIModel     string
	GeneratedAt time.Time

	ApprovalStatus string

	// WorkspaceApplied records the outcome of the last
	// apply-to-temporary-workspace attempt (spec §15.2) — never the
	// real repository.
	WorkspaceAppliedAt    *time.Time
	WorkspaceApplyResults []FileResult
	WorkspaceDir          string

	// RepositoryApplied records the one-time, approval-gated write to
	// the real repository (spec §3.4). Once set, a proposal is done —
	// there is no re-application.
	RepositoryAppliedAt    *time.Time
	RepositoryApplyResults []FileResult

	CreatedAt time.Time
	UpdatedAt time.Time
}

// ErrNotFound is returned when a fix proposal ID does not exist.
var ErrNotFound = errors.New("fixproposals: not found")

// ErrInvalidStatus is returned for an unrecognized approval status.
var ErrInvalidStatus = errors.New("fixproposals: invalid status")

// ErrAlreadyAppliedToRepository is returned when a repository
// application is attempted twice for the same proposal.
var ErrAlreadyAppliedToRepository = errors.New("fixproposals: already applied to the repository")

// ErrNotApproved is returned when a repository application is attempted
// before the proposal has been approved (spec §3.4, §15.2 acceptance:
// "Final repository write requires explicit approval").
var ErrNotApproved = errors.New("fixproposals: not approved")

// Store persists fix proposals.
type Store interface {
	Create(ctx context.Context, fp FixProposal) (FixProposal, error)
	Get(ctx context.Context, id string) (FixProposal, error)
	ListByProject(ctx context.Context, projectID string) ([]FixProposal, error)
	ListByBug(ctx context.Context, bugID string) ([]FixProposal, error)
	UpdateApprovalStatus(ctx context.Context, id, status string) (FixProposal, error)
	UpdateRegressionTestIDs(ctx context.Context, id string, testIDs []string) (FixProposal, error)
	RecordWorkspaceApplication(ctx context.Context, id, workspaceDir string, results []FileResult, appliedAt time.Time) (FixProposal, error)
	RecordRepositoryApplication(ctx context.Context, id string, results []FileResult, appliedAt time.Time) (FixProposal, error)
}
