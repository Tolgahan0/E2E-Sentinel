package audit

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"
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
	if event.CreatedAt.IsZero() {
		// Mirrors PostgresRecorder's DB-side `created_at` default —
		// callers throughout this codebase never set CreatedAt
		// themselves, relying on the store to stamp it.
		event.CreatedAt = time.Now()
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

// Search lists recorded events matching filter, newest first.
func (m *MemoryRecorder) Search(_ context.Context, filter SearchFilter) ([]Event, error) {
	limit := filter.Limit
	if limit <= 0 {
		limit = 50
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	var matched []Event
	for _, e := range m.events {
		if filter.ActionType != "" && e.ActionType != filter.ActionType {
			continue
		}
		if filter.ResourceType != "" && e.ResourceType != filter.ResourceType {
			continue
		}
		if filter.ResourceID != "" && e.ResourceID != filter.ResourceID {
			continue
		}
		if filter.Actor != "" && e.Actor != filter.Actor {
			continue
		}
		if !filter.Since.IsZero() && e.CreatedAt.Before(filter.Since) {
			continue
		}
		if !filter.Until.IsZero() && e.CreatedAt.After(filter.Until) {
			continue
		}
		matched = append(matched, e)
	}

	sort.SliceStable(matched, func(i, j int) bool {
		return matched[i].CreatedAt.After(matched[j].CreatedAt)
	})
	if len(matched) > limit {
		matched = matched[:limit]
	}
	return matched, nil
}
