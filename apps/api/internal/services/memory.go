package services

import (
	"context"
	"fmt"
	"sync"
)

// MemoryStore is an in-process Store for tests. Not for production use.
type MemoryStore struct {
	mu     sync.Mutex
	byKey  map[string]Service // key: projectID + "|" + name
	nextID int
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{byKey: map[string]Service{}}
}

func (s *MemoryStore) Upsert(_ context.Context, svc Service) (Service, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	key := svc.ProjectID + "|" + svc.Name
	if existing, ok := s.byKey[key]; ok {
		svc.ID = existing.ID
	} else {
		s.nextID++
		svc.ID = fmt.Sprintf("mem-service-%d", s.nextID)
	}
	s.byKey[key] = svc
	return svc, nil
}

func (s *MemoryStore) ListByProject(_ context.Context, projectID string) ([]Service, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var out []Service
	for _, svc := range s.byKey {
		if svc.ProjectID == projectID {
			out = append(out, svc)
		}
	}
	return out, nil
}
