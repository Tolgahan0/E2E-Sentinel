package projects

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// MemoryStore is an in-process Store for tests. Not for production use.
type MemoryStore struct {
	mu     sync.Mutex
	byID   map[string]Project
	nextID int
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{byID: map[string]Project{}}
}

func (s *MemoryStore) Create(_ context.Context, p Project) (Project, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.nextID++
	p.ID = fmt.Sprintf("mem-project-%d", s.nextID)
	p.CreatedAt = time.Now()
	p.UpdatedAt = p.CreatedAt
	if p.DiscoveryStatus == "" {
		p.DiscoveryStatus = DiscoveryStatusNeverRun
	}
	if p.CurrentMode == "" {
		p.CurrentMode = ModeObserve
	}
	if p.VisualDiffThreshold == 0 {
		p.VisualDiffThreshold = DefaultVisualDiffThreshold
	}
	s.byID[p.ID] = p
	return p, nil
}

func (s *MemoryStore) Get(_ context.Context, id string) (Project, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.byID[id]
	if !ok {
		return Project{}, ErrNotFound
	}
	return p, nil
}

func (s *MemoryStore) List(_ context.Context) ([]Project, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Project, 0, len(s.byID))
	for _, p := range s.byID {
		out = append(out, p)
	}
	return out, nil
}

func (s *MemoryStore) UpdateName(_ context.Context, id, name string) (Project, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.byID[id]
	if !ok {
		return Project{}, ErrNotFound
	}
	p.Name = name
	p.UpdatedAt = time.Now()
	s.byID[id] = p
	return p, nil
}

func (s *MemoryStore) SetDiscoveryStatus(_ context.Context, id, status string, lastDiscoveredAt *time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.byID[id]
	if !ok {
		return ErrNotFound
	}
	p.DiscoveryStatus = status
	if lastDiscoveredAt != nil {
		p.LastDiscoveredAt = lastDiscoveredAt
	}
	p.UpdatedAt = time.Now()
	s.byID[id] = p
	return nil
}

func (s *MemoryStore) SetGitHubCI(_ context.Context, id, githubRepo, tokenSecretReferenceID string) (Project, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.byID[id]
	if !ok {
		return Project{}, ErrNotFound
	}
	p.GitHubRepo = githubRepo
	p.GitHubTokenSecretReferenceID = tokenSecretReferenceID
	p.UpdatedAt = time.Now()
	s.byID[id] = p
	return p, nil
}

func (s *MemoryStore) SetLastCICommitSHA(_ context.Context, id, sha string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.byID[id]
	if !ok {
		return ErrNotFound
	}
	p.LastCICommitSHA = sha
	p.UpdatedAt = time.Now()
	s.byID[id] = p
	return nil
}

func (s *MemoryStore) SetVisualDiffThreshold(_ context.Context, id string, threshold float64) (Project, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.byID[id]
	if !ok {
		return Project{}, ErrNotFound
	}
	p.VisualDiffThreshold = threshold
	p.UpdatedAt = time.Now()
	s.byID[id] = p
	return p, nil
}

func (s *MemoryStore) ListWithGitHubCI(_ context.Context) ([]Project, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []Project
	for _, p := range s.byID {
		if p.GitHubRepo != "" {
			out = append(out, p)
		}
	}
	return out, nil
}

func (s *MemoryStore) Delete(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.byID[id]; !ok {
		return ErrNotFound
	}
	delete(s.byID, id)
	return nil
}

func (s *MemoryStore) SlugExists(_ context.Context, slug string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, p := range s.byID {
		if p.Slug == slug {
			return true, nil
		}
	}
	return false, nil
}
