package visualdiff

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"
)

// MemoryStore is an in-process Store for tests. Not for production use.
type MemoryStore struct {
	mu             sync.Mutex
	baselinesByTC  map[string]Baseline
	diffsByID      map[string]Diff
	nextBaselineID int
	nextDiffID     int
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		baselinesByTC: map[string]Baseline{},
		diffsByID:     map[string]Diff{},
	}
}

func (s *MemoryStore) GetBaseline(_ context.Context, testCaseID string) (Baseline, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, ok := s.baselinesByTC[testCaseID]
	if !ok {
		return Baseline{}, ErrNotFound
	}
	return b, nil
}

func (s *MemoryStore) SetBaseline(_ context.Context, testCaseID, artifactID, acceptedBy string) (Baseline, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	existing, ok := s.baselinesByTC[testCaseID]
	id := existing.ID
	if !ok {
		s.nextBaselineID++
		id = fmt.Sprintf("mem-baseline-%d", s.nextBaselineID)
	}
	b := Baseline{ID: id, TestCaseID: testCaseID, ArtifactID: artifactID, AcceptedBy: acceptedBy, AcceptedAt: time.Now()}
	s.baselinesByTC[testCaseID] = b
	return b, nil
}

func (s *MemoryStore) CreateDiff(_ context.Context, d Diff) (Diff, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nextDiffID++
	d.ID = fmt.Sprintf("mem-diff-%d", s.nextDiffID)
	if d.Status == "" {
		d.Status = StatusPendingReview
	}
	if d.CreatedAt.IsZero() {
		d.CreatedAt = time.Now()
	}
	s.diffsByID[d.ID] = d
	return d, nil
}

func (s *MemoryStore) Get(_ context.Context, id string) (Diff, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	d, ok := s.diffsByID[id]
	if !ok {
		return Diff{}, ErrNotFound
	}
	return d, nil
}

func (s *MemoryStore) ListByProject(_ context.Context, projectID string) ([]Diff, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []Diff
	for _, d := range s.diffsByID {
		if d.ProjectID == projectID {
			out = append(out, d)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		iPending := out[i].Status == StatusPendingReview
		jPending := out[j].Status == StatusPendingReview
		if iPending != jPending {
			return iPending
		}
		return out[i].CreatedAt.After(out[j].CreatedAt)
	})
	return out, nil
}

func (s *MemoryStore) UpdateStatus(_ context.Context, id, status, reviewedBy string) (Diff, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	d, ok := s.diffsByID[id]
	if !ok {
		return Diff{}, ErrNotFound
	}
	d.Status = status
	now := time.Now()
	d.ReviewedBy = &reviewedBy
	d.ReviewedAt = &now
	s.diffsByID[id] = d
	return d, nil
}
