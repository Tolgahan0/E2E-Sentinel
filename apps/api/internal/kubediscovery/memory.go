package kubediscovery

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
	byKey  map[string]Resource // key: project_id/namespace/kind/name
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{byKey: map[string]Resource{}}
}

func key(projectID, namespace, kind, name string) string {
	return projectID + "/" + namespace + "/" + kind + "/" + name
}

func (s *MemoryStore) Upsert(_ context.Context, r Resource) (Resource, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	k := key(r.ProjectID, r.Namespace, r.Kind, r.Name)
	now := time.Now()
	if existing, ok := s.byKey[k]; ok {
		r.ID = existing.ID
		r.CreatedAt = existing.CreatedAt
	} else {
		s.nextID++
		r.ID = fmt.Sprintf("mem-kube-resource-%d", s.nextID)
		r.CreatedAt = now
	}
	r.UpdatedAt = now
	s.byKey[k] = r
	return r, nil
}

func (s *MemoryStore) ListByProject(_ context.Context, projectID string) ([]Resource, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []Resource
	for _, r := range s.byKey {
		if r.ProjectID == projectID {
			out = append(out, r)
		}
	}
	return out, nil
}
