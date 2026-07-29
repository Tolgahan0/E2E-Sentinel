package kubediscovery

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// PostgresStore is the production Store backed by kube_resources.
type PostgresStore struct {
	pool *pgxpool.Pool
}

func NewPostgresStore(pool *pgxpool.Pool) *PostgresStore {
	return &PostgresStore{pool: pool}
}

func (s *PostgresStore) Upsert(ctx context.Context, r Resource) (Resource, error) {
	if r.Metadata == nil {
		r.Metadata = map[string]any{}
	}
	if r.Status == "" {
		r.Status = StatusNotApplicable
	}

	var lastSeenAt any
	if !r.LastSeenAt.IsZero() {
		lastSeenAt = r.LastSeenAt
	}

	row := s.pool.QueryRow(ctx, `
		INSERT INTO kube_resources (project_id, namespace, kind, name, desired_replicas, ready_replicas, restart_count, status, metadata, last_seen_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		ON CONFLICT (project_id, namespace, kind, name) DO UPDATE SET
			desired_replicas = EXCLUDED.desired_replicas,
			ready_replicas = EXCLUDED.ready_replicas,
			restart_count = EXCLUDED.restart_count,
			status = EXCLUDED.status,
			metadata = EXCLUDED.metadata,
			last_seen_at = COALESCE(EXCLUDED.last_seen_at, kube_resources.last_seen_at),
			updated_at = now()
		RETURNING id, project_id, namespace, kind, name, desired_replicas, ready_replicas, restart_count, status, metadata, last_seen_at, created_at, updated_at
	`, r.ProjectID, r.Namespace, r.Kind, r.Name, r.DesiredReplicas, r.ReadyReplicas, r.RestartCount, r.Status, r.Metadata, lastSeenAt)

	return scanResource(row)
}

func (s *PostgresStore) ListByProject(ctx context.Context, projectID string) ([]Resource, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, project_id, namespace, kind, name, desired_replicas, ready_replicas, restart_count, status, metadata, last_seen_at, created_at, updated_at
		FROM kube_resources WHERE project_id = $1 ORDER BY namespace, kind, name
	`, projectID)
	if err != nil {
		return nil, fmt.Errorf("kubediscovery: listing: %w", err)
	}
	defer rows.Close()

	var out []Resource
	for rows.Next() {
		r, err := scanResource(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanResource(row rowScanner) (Resource, error) {
	var r Resource
	var lastSeenAt *time.Time
	err := row.Scan(&r.ID, &r.ProjectID, &r.Namespace, &r.Kind, &r.Name, &r.DesiredReplicas, &r.ReadyReplicas, &r.RestartCount, &r.Status, &r.Metadata, &lastSeenAt, &r.CreatedAt, &r.UpdatedAt)
	if err != nil {
		return Resource{}, fmt.Errorf("kubediscovery: scanning row: %w", err)
	}
	if lastSeenAt != nil {
		r.LastSeenAt = *lastSeenAt
	}
	return r, nil
}
