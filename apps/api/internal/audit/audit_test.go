package audit

import (
	"context"
	"testing"
	"time"
)

func TestEvent_ValidateRequiresCoreFields(t *testing.T) {
	cases := []struct {
		name  string
		event Event
		want  error
	}{
		{"missing all", Event{}, ErrInvalidEvent},
		{"missing actor", Event{ActionType: "x", ResourceType: "y"}, ErrInvalidEvent},
		{"missing resource type", Event{ActionType: "x", Actor: "z"}, ErrInvalidEvent},
		{"valid", Event{ActionType: "x", ResourceType: "y", Actor: "z"}, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.event.validate()
			if tc.want == nil && err != nil {
				t.Errorf("validate() = %v, want nil", err)
			}
			if tc.want != nil && err != tc.want {
				t.Errorf("validate() = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestMemoryRecorder_RecordAndRecent(t *testing.T) {
	rec := NewMemoryRecorder()
	ctx := context.Background()

	if err := rec.Record(ctx, Event{}); err != ErrInvalidEvent {
		t.Fatalf("Record(invalid) = %v, want ErrInvalidEvent", err)
	}

	older := Event{ActionType: "project.discovered", ResourceType: "project", Actor: "system", CreatedAt: time.Now().Add(-time.Hour)}
	newer := Event{ActionType: "test.approved", ResourceType: "test_case", Actor: "admin", CreatedAt: time.Now()}

	if err := rec.Record(ctx, older); err != nil {
		t.Fatalf("Record(older) error: %v", err)
	}
	if err := rec.Record(ctx, newer); err != nil {
		t.Fatalf("Record(newer) error: %v", err)
	}

	events, err := rec.Recent(ctx, 10)
	if err != nil {
		t.Fatalf("Recent() error: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("got %d events, want 2", len(events))
	}
	if events[0].ActionType != "test.approved" {
		t.Errorf("events[0].ActionType = %q, want newest-first ordering", events[0].ActionType)
	}
	for _, e := range events {
		if e.ID == "" {
			t.Errorf("event missing generated ID: %+v", e)
		}
	}
}

func TestMemoryRecorder_RecentRespectsLimit(t *testing.T) {
	rec := NewMemoryRecorder()
	ctx := context.Background()
	for i := 0; i < 5; i++ {
		if err := rec.Record(ctx, Event{ActionType: "a", ResourceType: "b", Actor: "c"}); err != nil {
			t.Fatalf("Record() error: %v", err)
		}
	}

	events, err := rec.Recent(ctx, 2)
	if err != nil {
		t.Fatalf("Recent() error: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("got %d events, want 2", len(events))
	}
}

func TestMemoryRecorder_RecordStampsCreatedAtWhenUnset(t *testing.T) {
	rec := NewMemoryRecorder()
	before := time.Now()
	if err := rec.Record(context.Background(), Event{ActionType: "a", ResourceType: "b", Actor: "c"}); err != nil {
		t.Fatalf("Record() error: %v", err)
	}
	events, _ := rec.Recent(context.Background(), 1)
	if events[0].CreatedAt.Before(before) {
		t.Error("CreatedAt should be stamped to approximately now when the caller doesn't set it")
	}
}

func TestMemoryRecorder_SearchFiltersByActionResourceActor(t *testing.T) {
	rec := NewMemoryRecorder()
	ctx := context.Background()
	rec.Record(ctx, Event{ActionType: "project.added", ResourceType: "project", ResourceID: "p1", Actor: "alice"})
	rec.Record(ctx, Event{ActionType: "test.approved", ResourceType: "test_case", ResourceID: "t1", Actor: "bob"})
	rec.Record(ctx, Event{ActionType: "test.approved", ResourceType: "test_case", ResourceID: "t2", Actor: "alice"})

	byAction, err := rec.Search(ctx, SearchFilter{ActionType: "test.approved"})
	if err != nil {
		t.Fatalf("Search() error: %v", err)
	}
	if len(byAction) != 2 {
		t.Fatalf("Search(action_type) got %d events, want 2", len(byAction))
	}

	byActor, err := rec.Search(ctx, SearchFilter{Actor: "alice"})
	if err != nil {
		t.Fatalf("Search() error: %v", err)
	}
	if len(byActor) != 2 {
		t.Fatalf("Search(actor) got %d events, want 2", len(byActor))
	}

	byBoth, err := rec.Search(ctx, SearchFilter{ActionType: "test.approved", Actor: "alice"})
	if err != nil {
		t.Fatalf("Search() error: %v", err)
	}
	if len(byBoth) != 1 || byBoth[0].ResourceID != "t2" {
		t.Fatalf("Search(action_type, actor) = %+v", byBoth)
	}

	byResource, err := rec.Search(ctx, SearchFilter{ResourceType: "project"})
	if err != nil {
		t.Fatalf("Search() error: %v", err)
	}
	if len(byResource) != 1 {
		t.Fatalf("Search(resource_type) got %d events, want 1", len(byResource))
	}
}

func TestMemoryRecorder_SearchFiltersBySinceUntil(t *testing.T) {
	rec := NewMemoryRecorder()
	ctx := context.Background()
	now := time.Now()
	rec.Record(ctx, Event{ActionType: "a", ResourceType: "b", Actor: "c", CreatedAt: now.Add(-2 * time.Hour)})
	rec.Record(ctx, Event{ActionType: "a", ResourceType: "b", Actor: "c", CreatedAt: now})

	recent, err := rec.Search(ctx, SearchFilter{Since: now.Add(-time.Hour)})
	if err != nil {
		t.Fatalf("Search() error: %v", err)
	}
	if len(recent) != 1 {
		t.Fatalf("Search(since) got %d events, want 1", len(recent))
	}

	old, err := rec.Search(ctx, SearchFilter{Until: now.Add(-time.Hour)})
	if err != nil {
		t.Fatalf("Search() error: %v", err)
	}
	if len(old) != 1 {
		t.Fatalf("Search(until) got %d events, want 1", len(old))
	}
}

func TestMemoryRecorder_SearchOrdersNewestFirstAndRespectsLimit(t *testing.T) {
	rec := NewMemoryRecorder()
	ctx := context.Background()
	now := time.Now()
	rec.Record(ctx, Event{ActionType: "a", ResourceType: "b", Actor: "c", CreatedAt: now.Add(-time.Hour), ResourceID: "old"})
	rec.Record(ctx, Event{ActionType: "a", ResourceType: "b", Actor: "c", CreatedAt: now, ResourceID: "new"})

	events, err := rec.Search(ctx, SearchFilter{Limit: 1})
	if err != nil {
		t.Fatalf("Search() error: %v", err)
	}
	if len(events) != 1 || events[0].ResourceID != "new" {
		t.Fatalf("Search(limit=1) = %+v, want the newest event", events)
	}
}
