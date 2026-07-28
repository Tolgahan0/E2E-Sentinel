package services

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// PostgresStore is the production Store backed by discovered_services.
type PostgresStore struct {
	pool *pgxpool.Pool
}

func NewPostgresStore(pool *pgxpool.Pool) *PostgresStore {
	return &PostgresStore{pool: pool}
}

func (s *PostgresStore) Upsert(ctx context.Context, svc Service) (Service, error) {
	if svc.Ports == nil {
		svc.Ports = []string{}
	}
	if svc.Dependencies == nil {
		svc.Dependencies = []string{}
	}
	if svc.Metadata == nil {
		svc.Metadata = map[string]any{}
	}

	var lastSeenAt any
	if !svc.LastSeenAt.IsZero() {
		lastSeenAt = svc.LastSeenAt
	}

	row := s.pool.QueryRow(ctx, `
		INSERT INTO discovered_services (project_id, name, kind, runtime, source_path, container_name, image, ports, dependencies, metadata, confidence, last_seen_at)
		VALUES ($1, $2, $3, $4, $5, NULLIF($6, ''), $7, $8, $9, $10, $11, $12)
		ON CONFLICT (project_id, name) DO UPDATE SET
			kind = EXCLUDED.kind,
			runtime = EXCLUDED.runtime,
			source_path = EXCLUDED.source_path,
			container_name = EXCLUDED.container_name,
			image = EXCLUDED.image,
			ports = EXCLUDED.ports,
			dependencies = EXCLUDED.dependencies,
			metadata = EXCLUDED.metadata,
			confidence = EXCLUDED.confidence,
			last_seen_at = COALESCE(EXCLUDED.last_seen_at, discovered_services.last_seen_at),
			updated_at = now()
		RETURNING id, project_id, name, kind, runtime, COALESCE(source_path, ''), COALESCE(container_name, ''), COALESCE(image, ''), ports, dependencies, metadata, confidence, last_seen_at
	`, svc.ProjectID, svc.Name, svc.Kind, svc.Runtime, svc.SourcePath, svc.ContainerName, svc.Image, svc.Ports, svc.Dependencies, svc.Metadata, svc.ConfidenceLevel, lastSeenAt)

	return scanService(row)
}

func (s *PostgresStore) ListByProject(ctx context.Context, projectID string) ([]Service, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, project_id, name, kind, runtime, COALESCE(source_path, ''), COALESCE(container_name, ''), COALESCE(image, ''), ports, dependencies, metadata, confidence, last_seen_at
		FROM discovered_services WHERE project_id = $1 ORDER BY name
	`, projectID)
	if err != nil {
		return nil, fmt.Errorf("services: listing: %w", err)
	}
	defer rows.Close()

	var out []Service
	for rows.Next() {
		svc, err := scanService(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, svc)
	}
	return out, rows.Err()
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanService(row rowScanner) (Service, error) {
	var svc Service
	var lastSeenAt *time.Time
	err := row.Scan(&svc.ID, &svc.ProjectID, &svc.Name, &svc.Kind, &svc.Runtime, &svc.SourcePath, &svc.ContainerName, &svc.Image, &svc.Ports, &svc.Dependencies, &svc.Metadata, &svc.ConfidenceLevel, &lastSeenAt)
	if err != nil {
		return Service{}, fmt.Errorf("services: scanning row: %w", err)
	}
	if lastSeenAt != nil {
		svc.LastSeenAt = *lastSeenAt
	}
	return svc, nil
}
