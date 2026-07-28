package discovery

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// MemoryStore is an in-process Store for tests. Not for production use.
type MemoryStore struct {
	mu       sync.Mutex
	runs     map[string]Run
	findings map[string][]Finding // keyed by run ID
	nextID   int
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{runs: map[string]Run{}, findings: map[string][]Finding{}}
}

func (s *MemoryStore) StartRun(_ context.Context, projectID string) (Run, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nextID++
	run := Run{
		ID:        fmt.Sprintf("mem-run-%d", s.nextID),
		ProjectID: projectID,
		Status:    RunStatusRunning,
		StartedAt: time.Now(),
	}
	s.runs[run.ID] = run
	return run, nil
}

func (s *MemoryStore) CompleteRun(_ context.Context, runID string, findings []Finding) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	run, ok := s.runs[runID]
	if !ok {
		return fmt.Errorf("discovery: run %q not found", runID)
	}
	now := time.Now()
	run.Status = RunStatusCompleted
	run.CompletedAt = &now
	s.runs[runID] = run
	s.findings[runID] = findings
	return nil
}

func (s *MemoryStore) FailRun(_ context.Context, runID, errMsg string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	run, ok := s.runs[runID]
	if !ok {
		return fmt.Errorf("discovery: run %q not found", runID)
	}
	now := time.Now()
	run.Status = RunStatusFailed
	run.Error = errMsg
	run.CompletedAt = &now
	s.runs[runID] = run
	return nil
}

func (s *MemoryStore) LatestCompleted(_ context.Context, projectID string) (Run, []Finding, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var latest Run
	found := false
	for _, run := range s.runs {
		if run.ProjectID != projectID || run.Status != RunStatusCompleted {
			continue
		}
		if !found || run.CompletedAt.After(*latest.CompletedAt) {
			latest = run
			found = true
		}
	}
	if !found {
		return Run{}, nil, ErrNoCompletedRun
	}
	return latest, s.findings[latest.ID], nil
}
