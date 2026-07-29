package failures

import (
	"context"
	"errors"
	"time"
)

// Failure is a persisted record of one failed test run's classification
// (spec §6.8) — the raw, per-run record. internal/bugreports.BugReport
// is the durable, deduplicated-across-runs record built from these.
type Failure struct {
	ID                  string
	TestRunID           string
	TestCaseID          string
	Title               string
	Severity            string
	FailureType         string
	Expected            string
	Actual              string
	ErrorMessage        string
	StackTrace          string
	RootCauseHypothesis string
	ConfidenceScore     string
	ArtifactIDs         []string
	CreatedAt           time.Time
}

// ErrNotFound is returned when a failure ID does not exist.
var ErrNotFound = errors.New("failures: not found")

// Store persists failure records.
type Store interface {
	Create(ctx context.Context, f Failure) (Failure, error)
	Get(ctx context.Context, id string) (Failure, error)
	// ListByTestCase returns every recorded failure for a test case,
	// oldest first — used to build the outcome history that
	// AssessFlakiness needs (spec §13.2).
	ListByTestCase(ctx context.Context, testCaseID string) ([]Failure, error)
}
