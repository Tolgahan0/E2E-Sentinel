// Package runs implements Test Run tracking (spec §6.7) and the Runner
// architecture (spec §11): isolated, disposable-container test
// execution. AI has no role here — pass/fail comes only from the
// runner's process exit code (spec §2.4).
package runs

import (
	"context"
	"errors"
	"time"
)

// Statuses.
const (
	StatusQueued    = "queued"
	StatusRunning   = "running"
	StatusPassed    = "passed"
	StatusFailed    = "failed"
	StatusCancelled = "cancelled"
	StatusError     = "error" // infrastructure failure, distinct from a failing assertion
)

// TestRun is one execution of a single test case (spec §6.7, narrowed to
// the fields Phase 5 populates).
type TestRun struct {
	ID          string
	ProjectID   string
	TestCaseID  string
	Status      string
	RunnerType  string
	TriggerType string
	TriggeredBy string
	ExitCode    *int
	Summary     string
	StartedAt   time.Time
	FinishedAt  *time.Time
}

// ErrNotFound is returned when a run ID does not exist.
var ErrNotFound = errors.New("runs: not found")

// Store persists test runs.
type Store interface {
	Create(ctx context.Context, run TestRun) (TestRun, error)
	Get(ctx context.Context, id string) (TestRun, error)
	ListByProject(ctx context.Context, projectID string) ([]TestRun, error)
	// ListByTestCase returns every run of a single test case, oldest
	// first — used to build the outcome history for flaky assessment
	// (spec §13.2).
	ListByTestCase(ctx context.Context, testCaseID string) ([]TestRun, error)
	UpdateStatus(ctx context.Context, id, status string, exitCode *int, summary string, finished bool) (TestRun, error)
}
