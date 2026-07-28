package settings

import (
	"context"
	"encoding/json"
	"sync"
)

// MemoryStore is an in-process Store for tests. Not for production use.
type MemoryStore struct {
	mu   sync.Mutex
	data map[string]json.RawMessage
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{data: map[string]json.RawMessage{}}
}

func (s *MemoryStore) Get(_ context.Context, key string) (json.RawMessage, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	value, ok := s.data[key]
	return value, ok, nil
}

func (s *MemoryStore) Set(_ context.Context, key string, value json.RawMessage) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data[key] = value
	return nil
}
