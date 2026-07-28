package planning

import (
	"context"
	"fmt"
	"sync"
)

// MemoryStore is an in-process Store for tests. Not for production use.
type MemoryStore struct {
	mu     sync.Mutex
	byID   map[string]TestCase
	byKey  map[string]string // (projectID|naturalKey) -> id
	nextID int
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{byID: map[string]TestCase{}, byKey: map[string]string{}}
}

func (s *MemoryStore) CreateIfAbsent(_ context.Context, tc TestCase) (TestCase, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	key := tc.ProjectID + "|" + tc.NaturalKey
	if existingID, ok := s.byKey[key]; ok {
		return s.byID[existingID], false, nil
	}

	s.nextID++
	tc.ID = fmt.Sprintf("mem-test-%d", s.nextID)
	s.byID[tc.ID] = tc
	s.byKey[key] = tc.ID
	return tc, true, nil
}

func (s *MemoryStore) List(_ context.Context, projectID string) ([]TestCase, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []TestCase
	for _, tc := range s.byID {
		if tc.ProjectID == projectID {
			out = append(out, tc)
		}
	}
	return out, nil
}

func (s *MemoryStore) Get(_ context.Context, id string) (TestCase, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	tc, ok := s.byID[id]
	if !ok {
		return TestCase{}, ErrNotFound
	}
	return tc, nil
}

func (s *MemoryStore) UpdateApproval(_ context.Context, id, approvalStatus string) (TestCase, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	tc, ok := s.byID[id]
	if !ok {
		return TestCase{}, ErrNotFound
	}
	tc.ApprovalStatus = approvalStatus
	if approvalStatus == ApprovalApproved {
		tc.Status = StatusApproved
	}
	s.byID[id] = tc
	return tc, nil
}

func (s *MemoryStore) Update(_ context.Context, id string, title, description, priority string) (TestCase, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	tc, ok := s.byID[id]
	if !ok {
		return TestCase{}, ErrNotFound
	}
	if title != "" {
		tc.Title = title
	}
	if description != "" {
		tc.Description = description
	}
	if priority != "" {
		tc.Priority = priority
	}
	s.byID[id] = tc
	return tc, nil
}
