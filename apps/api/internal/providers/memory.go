package providers

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
	byID   map[string]Provider
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{byID: map[string]Provider{}}
}

func (s *MemoryStore) Create(_ context.Context, p Provider) (Provider, error) {
	if err := Validate(p); err != nil {
		return Provider{}, err
	}
	if p.TimeoutSeconds == 0 {
		p.TimeoutSeconds = DefaultTimeoutSeconds
	}
	if p.HealthStatus == "" {
		p.HealthStatus = HealthUnknown
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.nextID++
	p.ID = fmt.Sprintf("mem-provider-%d", s.nextID)
	p.CreatedAt = time.Now()
	p.UpdatedAt = p.CreatedAt
	s.byID[p.ID] = p
	return p, nil
}

func (s *MemoryStore) List(_ context.Context) ([]Provider, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Provider, 0, len(s.byID))
	for _, p := range s.byID {
		out = append(out, p)
	}
	return out, nil
}

func (s *MemoryStore) Get(_ context.Context, id string) (Provider, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.byID[id]
	if !ok {
		return Provider{}, ErrNotFound
	}
	return p, nil
}

func (s *MemoryStore) Update(_ context.Context, id string, patch Patch) (Provider, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.byID[id]
	if !ok {
		return Provider{}, ErrNotFound
	}

	if patch.Name != nil {
		p.Name = *patch.Name
	}
	if patch.BaseURL != nil {
		p.BaseURL = *patch.BaseURL
	}
	if patch.Model != nil {
		p.Model = *patch.Model
	}
	if patch.ClearSecretReference {
		p.SecretReferenceID = ""
	} else if patch.SecretReferenceID != nil {
		p.SecretReferenceID = *patch.SecretReferenceID
	}
	if patch.Enabled != nil {
		p.Enabled = *patch.Enabled
	}
	if patch.Capabilities != nil {
		p.Capabilities = *patch.Capabilities
	}
	if patch.TimeoutSeconds != nil {
		p.TimeoutSeconds = *patch.TimeoutSeconds
	}
	if patch.MaxTokens != nil {
		p.MaxTokens = *patch.MaxTokens
	}
	if patch.Temperature != nil {
		p.Temperature = *patch.Temperature
	}
	if p.Name == "" {
		return Provider{}, ErrNameRequired
	}

	p.UpdatedAt = time.Now()
	s.byID[id] = p
	return p, nil
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

func (s *MemoryStore) UpdateHealth(_ context.Context, id, status string, checkedAt time.Time) (Provider, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.byID[id]
	if !ok {
		return Provider{}, ErrNotFound
	}
	p.HealthStatus = status
	p.LastCheckedAt = checkedAt
	p.UpdatedAt = time.Now()
	s.byID[id] = p
	return p, nil
}
