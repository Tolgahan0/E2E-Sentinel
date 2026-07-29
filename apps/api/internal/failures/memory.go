package failures

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// MemoryStore is an in-process Store for tests. Not for production use.
type MemoryStore struct {
	mu     sync.Mutex
	nextID int
	byID   map[string]Failure
	order  []string
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{byID: map[string]Failure{}}
}

func (s *MemoryStore) Create(_ context.Context, f Failure) (Failure, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nextID++
	f.ID = fmt.Sprintf("mem-failure-%d", s.nextID)
	f.CreatedAt = time.Now()
	s.byID[f.ID] = f
	s.order = append(s.order, f.ID)
	return f, nil
}

func (s *MemoryStore) Get(_ context.Context, id string) (Failure, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	f, ok := s.byID[id]
	if !ok {
		return Failure{}, ErrNotFound
	}
	return f, nil
}

func (s *MemoryStore) ListByTestCase(_ context.Context, testCaseID string) ([]Failure, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []Failure
	for _, id := range s.order {
		f := s.byID[id]
		if f.TestCaseID == testCaseID {
			out = append(out, f)
		}
	}
	return out, nil
}
