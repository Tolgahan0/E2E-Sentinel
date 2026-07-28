// Package audit records the append-only audit trail required by spec
// §2.7 ("Audit Everything"). Every meaningful operation in E2E Sentinel
// must go through a Recorder rather than writing to audit_events directly,
// so that future sinks (e.g. external SIEM export) can be added without
// touching call sites.
package audit

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Event is a single audit record. Metadata must never contain secret
// values — callers are responsible for redacting before constructing an
// Event (see internal/logging.Redact for a helper).
type Event struct {
	ID           string
	ActionType   string
	ResourceType string
	ResourceID   string
	Actor        string
	Metadata     map[string]any
	CreatedAt    time.Time
}

// ErrInvalidEvent is returned when a required field is missing.
var ErrInvalidEvent = errors.New("audit: action_type, resource_type, and actor are required")

func (e Event) validate() error {
	if e.ActionType == "" || e.ResourceType == "" || e.Actor == "" {
		return ErrInvalidEvent
	}
	return nil
}

// Recorder persists audit events and lists recently recorded ones.
type Recorder interface {
	Record(ctx context.Context, event Event) error
	Recent(ctx context.Context, limit int) ([]Event, error)
}

// PostgresRecorder is the default Recorder backed by the audit_events
// table (migrations/0001_init.sql).
type PostgresRecorder struct {
	pool *pgxpool.Pool
}

// NewPostgresRecorder builds a Recorder backed by pool.
func NewPostgresRecorder(pool *pgxpool.Pool) *PostgresRecorder {
	return &PostgresRecorder{pool: pool}
}

// Record validates and inserts a single audit event.
func (r *PostgresRecorder) Record(ctx context.Context, event Event) error {
	if err := event.validate(); err != nil {
		return err
	}

	metadata := event.Metadata
	if metadata == nil {
		metadata = map[string]any{}
	}

	var resourceID *string
	if event.ResourceID != "" {
		resourceID = &event.ResourceID
	}

	_, err := r.pool.Exec(ctx, `
		INSERT INTO audit_events (action_type, resource_type, resource_id, actor, metadata)
		VALUES ($1, $2, $3, $4, $5)
	`, event.ActionType, event.ResourceType, resourceID, event.Actor, metadata)
	if err != nil {
		return fmt.Errorf("audit: recording event: %w", err)
	}
	return nil
}

// Recent returns up to limit most-recent events, newest first. limit is
// clamped to [1, 500] to bound response size.
func (r *PostgresRecorder) Recent(ctx context.Context, limit int) ([]Event, error) {
	if limit <= 0 {
		limit = 50
	}
	if limit > 500 {
		limit = 500
	}

	rows, err := r.pool.Query(ctx, `
		SELECT id, action_type, resource_type, COALESCE(resource_id, ''), actor, metadata, created_at
		FROM audit_events
		ORDER BY created_at DESC
		LIMIT $1
	`, limit)
	if err != nil {
		return nil, fmt.Errorf("audit: listing events: %w", err)
	}
	defer rows.Close()

	var events []Event
	for rows.Next() {
		var e Event
		if err := rows.Scan(&e.ID, &e.ActionType, &e.ResourceType, &e.ResourceID, &e.Actor, &e.Metadata, &e.CreatedAt); err != nil {
			return nil, fmt.Errorf("audit: scanning event: %w", err)
		}
		events = append(events, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("audit: reading events: %w", err)
	}

	return events, nil
}
