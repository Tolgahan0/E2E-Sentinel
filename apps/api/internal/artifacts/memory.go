package artifacts

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sync"
	"time"
)

// MemoryStore is an in-process Store for tests. Not for production use.
type MemoryStore struct {
	mu     sync.Mutex
	data   map[string][]byte
	byID   map[string]Artifact
	byRun  map[string][]string
	nextID int
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{data: map[string][]byte{}, byID: map[string]Artifact{}, byRun: map[string][]string{}}
}

func (s *MemoryStore) Save(_ context.Context, testRunID, kind, mimeType string, data []byte, retentionUntil time.Time) (Artifact, error) {
	sum := sha256.Sum256(data)

	s.mu.Lock()
	defer s.mu.Unlock()
	s.nextID++
	id := fmt.Sprintf("mem-artifact-%d", s.nextID)

	a := Artifact{
		ID: id, TestRunID: testRunID, Kind: kind, MimeType: mimeType,
		SizeBytes: int64(len(data)), Checksum: hex.EncodeToString(sum[:]), StoragePath: id,
		RetentionUntil: &retentionUntil, CreatedAt: time.Now(),
	}
	s.data[id] = data
	s.byID[id] = a
	s.byRun[testRunID] = append(s.byRun[testRunID], id)
	return a, nil
}

func (s *MemoryStore) ListByRun(_ context.Context, testRunID string) ([]Artifact, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []Artifact
	for _, id := range s.byRun[testRunID] {
		out = append(out, s.byID[id])
	}
	return out, nil
}

func (s *MemoryStore) Read(_ context.Context, artifactID string) ([]byte, Artifact, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	a, ok := s.byID[artifactID]
	if !ok {
		return nil, Artifact{}, ErrNotFound
	}
	return s.data[artifactID], a, nil
}
