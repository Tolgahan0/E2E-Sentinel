// Package bugreports implements structured bug reports (spec §14),
// automatically created or updated from a failed test run. A bug report
// is a durable, reviewable record: it survives across repeated failures
// of the same underlying defect (bumping frequency rather than spawning
// duplicate rows) and carries every field spec §14 requires, including
// an explicitly-labeled root cause hypothesis and confidence.
package bugreports

import (
	"context"
	"errors"
	"time"
)

// Statuses.
const (
	StatusOpen     = "open"
	StatusResolved = "resolved"
	StatusReopened = "reopened"
)

// Evidence is the evidence attached to a bug report — never raw secret
// content, only references and already-captured text (spec §14
// "Evidence").
type Evidence struct {
	ArtifactIDs  []string
	ErrorMessage string
	StackTrace   string
}

// Note is a free-text annotation a user can add to a bug report.
type Note struct {
	Author    string
	Text      string
	CreatedAt time.Time
}

// BugReport is a structured bug report (spec §14).
type BugReport struct {
	ID            string
	ProjectID     string
	FailureID     string
	TestCaseID    string
	EnvironmentID string

	Title           string
	Severity        string
	FailureType     string
	AffectedService string
	AffectedRoute   string

	Preconditions    string
	StepsToReproduce []string
	ExpectedResult   string
	ActualResult     string
	Evidence         Evidence

	FirstObservedAt time.Time
	LastObservedAt  time.Time
	Frequency       int

	// RootCauseHypothesis is never a confirmed fact — always rendered
	// with an explicit "hypothesis" label wherever it's displayed or
	// exported (spec §14 acceptance: "Root cause is clearly marked as
	// hypothesis").
	RootCauseHypothesis string
	RootCauseConfidence string

	FlakyAssessment  string
	RelatedGraphPath string

	RegressionTestIDs []string

	// PossibleDuplicateOfID is a hint, not an automatic merge — the UI
	// surfaces it for a human to confirm (spec §17.6 "Duplicate
	// linking"). Set only when this bug is created, from a different
	// test case with the same failure_type at that time.
	PossibleDuplicateOfID string

	Status string
	Notes  []Note

	CreatedAt time.Time
	UpdatedAt time.Time
}

// ErrNotFound is returned when a bug report ID does not exist.
var ErrNotFound = errors.New("bugreports: not found")

// ErrInvalidStatus is returned for an unrecognized status value.
var ErrInvalidStatus = errors.New("bugreports: invalid status")

// ValidStatus reports whether s is a recognized status.
func ValidStatus(s string) bool {
	return s == StatusOpen || s == StatusResolved || s == StatusReopened
}

// UpsertInput is what UpsertFromFailure needs to create or update a bug
// report from a single failure.
type UpsertInput struct {
	ProjectID     string
	FailureID     string
	TestCaseID    string
	EnvironmentID string

	Title           string
	Severity        string
	FailureType     string
	AffectedService string
	AffectedRoute   string

	Preconditions    string
	StepsToReproduce []string
	ExpectedResult   string
	ActualResult     string
	Evidence         Evidence

	RootCauseHypothesis string
	RootCauseConfidence string
	FlakyAssessment     string
	RelatedGraphPath    string
	RegressionTestIDs   []string

	ObservedAt time.Time
}

// ListFilter narrows List results. Zero-value fields are not filtered on.
type ListFilter struct {
	ProjectID     string
	Severity      string
	Status        string
	EnvironmentID string
	Search        string
}

// Store persists bug reports.
type Store interface {
	// UpsertFromFailure creates a new open bug for
	// (project_id, test_case_id, failure_type) if none exists, or
	// updates the existing one in place (bumping Frequency and
	// LastObservedAt, refreshing evidence/root-cause to the latest
	// failure). A resolved bug flips to StatusReopened rather than
	// silently absorbing the update, since a recurring failure after
	// resolution is itself evidence the fix didn't hold. Returns
	// isNew=true only when a new row was created.
	UpsertFromFailure(ctx context.Context, in UpsertInput) (bug BugReport, isNew bool, err error)
	List(ctx context.Context, filter ListFilter) ([]BugReport, error)
	Get(ctx context.Context, id string) (BugReport, error)
	UpdateStatus(ctx context.Context, id, status string) (BugReport, error)
	AddNote(ctx context.Context, id, author, text string) (BugReport, error)
}
