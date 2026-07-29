package runs

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// MemoryStore is an in-process Store for tests. Not for production use.
type MemoryStore struct {
	mu     sync.Mutex
	byID   map[string]TestRun
	order  []string
	nextID int
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{byID: map[string]TestRun{}}
}

func (s *MemoryStore) Create(_ context.Context, run TestRun) (TestRun, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nextID++
	run.ID = fmt.Sprintf("mem-run-%d", s.nextID)
	if run.StartedAt.IsZero() {
		run.StartedAt = time.Now()
	}
	s.byID[run.ID] = run
	s.order = append(s.order, run.ID)
	return run, nil
}

func (s *MemoryStore) Get(_ context.Context, id string) (TestRun, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	run, ok := s.byID[id]
	if !ok {
		return TestRun{}, ErrNotFound
	}
	return run, nil
}

func (s *MemoryStore) ListByProject(_ context.Context, projectID string) ([]TestRun, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []TestRun
	for _, r := range s.byID {
		if r.ProjectID == projectID {
			out = append(out, r)
		}
	}
	return out, nil
}

func (s *MemoryStore) ListByTestCase(_ context.Context, testCaseID string) ([]TestRun, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []TestRun
	for _, id := range s.order {
		r := s.byID[id]
		if r.TestCaseID == testCaseID {
			out = append(out, r)
		}
	}
	return out, nil
}

func (s *MemoryStore) UpdateStatus(_ context.Context, id, status string, exitCode *int, summary string, finished bool) (TestRun, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	run, ok := s.byID[id]
	if !ok {
		return TestRun{}, ErrNotFound
	}
	run.Status = status
	run.ExitCode = exitCode
	run.Summary = summary
	if finished {
		now := time.Now()
		run.FinishedAt = &now
	}
	s.byID[id] = run
	return run, nil
}
