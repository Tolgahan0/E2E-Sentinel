package environments

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// MemoryStore is an in-process Store for tests. Not for production use.
type MemoryStore struct {
	mu     sync.Mutex
	byID   map[string]Environment
	nextID int
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{byID: map[string]Environment{}}
}

func (s *MemoryStore) Create(_ context.Context, env Environment) (Environment, error) {
	if !ValidClassification(env.Classification) {
		return Environment{}, ErrInvalidClassification
	}
	env = RestrictForClassification(env)

	s.mu.Lock()
	defer s.mu.Unlock()
	s.nextID++
	env.ID = fmt.Sprintf("mem-env-%d", s.nextID)
	env.CreatedAt = time.Now()
	env.UpdatedAt = env.CreatedAt
	s.byID[env.ID] = env
	return env, nil
}

func (s *MemoryStore) ListByProject(_ context.Context, projectID string) ([]Environment, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []Environment
	for _, e := range s.byID {
		if e.ProjectID == projectID {
			out = append(out, e)
		}
	}
	return out, nil
}

func (s *MemoryStore) Get(_ context.Context, id string) (Environment, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.byID[id]
	if !ok {
		return Environment{}, ErrNotFound
	}
	return e, nil
}

func (s *MemoryStore) UpdateClassification(_ context.Context, id, classification string) (Environment, error) {
	if !ValidClassification(classification) {
		return Environment{}, ErrInvalidClassification
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.byID[id]
	if !ok {
		return Environment{}, ErrNotFound
	}
	e.Classification = classification
	e = RestrictForClassification(e)
	e.UpdatedAt = time.Now()
	s.byID[id] = e
	return e, nil
}

func (s *MemoryStore) UpdateBaseURL(_ context.Context, id, baseURL string) (Environment, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.byID[id]
	if !ok {
		return Environment{}, ErrNotFound
	}
	e.BaseURL = baseURL
	e.UpdatedAt = time.Now()
	s.byID[id] = e
	return e, nil
}
