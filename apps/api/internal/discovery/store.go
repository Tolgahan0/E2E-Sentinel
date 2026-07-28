package discovery

import (
	"context"
	"errors"
	"time"
)

const (
	RunStatusRunning   = "running"
	RunStatusCompleted = "completed"
	RunStatusFailed    = "failed"
)

// Run is one execution of the scanner against a project.
type Run struct {
	ID          string
	ProjectID   string
	Status      string
	Error       string
	StartedAt   time.Time
	CompletedAt *time.Time
}

// ErrNoCompletedRun is returned when a project has never completed a
// discovery run.
var ErrNoCompletedRun = errors.New("discovery: no completed run for this project")

// Store persists discovery runs and their findings.
type Store interface {
	StartRun(ctx context.Context, projectID string) (Run, error)
	CompleteRun(ctx context.Context, runID string, findings []Finding) error
	FailRun(ctx context.Context, runID, errMsg string) error
	LatestCompleted(ctx context.Context, projectID string) (Run, []Finding, error)
}
