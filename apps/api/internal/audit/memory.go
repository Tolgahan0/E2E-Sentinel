package audit

import (
	"context"
	"fmt"
	"sort"
	"sync"
)

// MemoryRecorder is an in-process Recorder used by tests that exercise
// code depending on audit.Recorder without a real database. It must not
// be used in production: audit events must be durable.
type MemoryRecorder struct {
	mu     sync.Mutex
	events []Event
	nextID int
}

// NewMemoryRecorder builds an empty MemoryRecorder.
func NewMemoryRecorder() *MemoryRecorder {
	return &MemoryRecorder{}
}

// Record validates and appends event.
func (m *MemoryRecorder) Record(_ context.Context, event Event) error {
	if err := event.validate(); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.nextID++
	if event.ID == "" {
		event.ID = fmt.Sprintf("mem-%d", m.nextID)
	}
	m.events = append(m.events, event)
	return nil
}

// Recent returns up to limit most-recently recorded events, newest first.
func (m *MemoryRecorder) Recent(_ context.Context, limit int) ([]Event, error) {
	if limit <= 0 {
		limit = 50
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	ordered := make([]Event, len(m.events))
	copy(ordered, m.events)
	sort.SliceStable(ordered, func(i, j int) bool {
		return ordered[i].CreatedAt.After(ordered[j].CreatedAt)
	})

	if len(ordered) > limit {
		ordered = ordered[:limit]
	}
	return ordered, nil
}
