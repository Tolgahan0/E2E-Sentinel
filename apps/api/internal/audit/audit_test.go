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
