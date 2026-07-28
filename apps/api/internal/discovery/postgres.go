package discovery

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PostgresStore is the production Store backed by discovery_runs and
// discovery_findings.
type PostgresStore struct {
	pool *pgxpool.Pool
}

func NewPostgresStore(pool *pgxpool.Pool) *PostgresStore {
	return &PostgresStore{pool: pool}
}

func (s *PostgresStore) StartRun(ctx context.Context, projectID string) (Run, error) {
	var run Run
	run.ProjectID = projectID
	err := s.pool.QueryRow(ctx, `
		INSERT INTO discovery_runs (project_id, status) VALUES ($1, $2)
		RETURNING id, project_id, status, COALESCE(error, ''), started_at, completed_at
	`, projectID, RunStatusRunning).Scan(&run.ID, &run.ProjectID, &run.Status, &run.Error, &run.StartedAt, &run.CompletedAt)
	if err != nil {
		return Run{}, fmt.Errorf("discovery: starting run: %w", err)
	}
	return run, nil
}

func (s *PostgresStore) CompleteRun(ctx context.Context, runID string, findings []Finding) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("discovery: beginning transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	var projectID string
	if err := tx.QueryRow(ctx, `SELECT project_id FROM discovery_runs WHERE id = $1`, runID).Scan(&projectID); err != nil {
		return fmt.Errorf("discovery: looking up run %q: %w", runID, err)
	}

	for _, f := range findings {
		_, err := tx.Exec(ctx, `
			INSERT INTO discovery_findings (discovery_run_id, project_id, category, name, path, confidence, evidence)
			VALUES ($1, $2, $3, $4, $5, $6, $7)
		`, runID, projectID, f.Category, f.Name, f.Path, f.Confidence, f.Evidence)
		if err != nil {
			return fmt.Errorf("discovery: inserting finding %s/%s: %w", f.Category, f.Name, err)
		}
	}

	if _, err := tx.Exec(ctx, `
		UPDATE discovery_runs SET status = $2, completed_at = now() WHERE id = $1
	`, runID, RunStatusCompleted); err != nil {
		return fmt.Errorf("discovery: completing run: %w", err)
	}

	return tx.Commit(ctx)
}

func (s *PostgresStore) FailRun(ctx context.Context, runID, errMsg string) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE discovery_runs SET status = $2, error = $3, completed_at = now() WHERE id = $1
	`, runID, RunStatusFailed, errMsg)
	if err != nil {
		return fmt.Errorf("discovery: failing run: %w", err)
	}
	return nil
}

func (s *PostgresStore) LatestCompleted(ctx context.Context, projectID string) (Run, []Finding, error) {
	var run Run
	err := s.pool.QueryRow(ctx, `
		SELECT id, project_id, status, COALESCE(error, ''), started_at, completed_at
		FROM discovery_runs
		WHERE project_id = $1 AND status = $2
		ORDER BY completed_at DESC
		LIMIT 1
	`, projectID, RunStatusCompleted).Scan(&run.ID, &run.ProjectID, &run.Status, &run.Error, &run.StartedAt, &run.CompletedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Run{}, nil, ErrNoCompletedRun
	}
	if err != nil {
		return Run{}, nil, fmt.Errorf("discovery: finding latest run: %w", err)
	}

	rows, err := s.pool.Query(ctx, `
		SELECT category, name, COALESCE(path, ''), confidence, evidence
		FROM discovery_findings
		WHERE discovery_run_id = $1
		ORDER BY category, name
	`, run.ID)
	if err != nil {
		return Run{}, nil, fmt.Errorf("discovery: listing findings: %w", err)
	}
	defer rows.Close()

	var findings []Finding
	for rows.Next() {
		var f Finding
		if err := rows.Scan(&f.Category, &f.Name, &f.Path, &f.Confidence, &f.Evidence); err != nil {
			return Run{}, nil, fmt.Errorf("discovery: scanning finding: %w", err)
		}
		findings = append(findings, f)
	}
	return run, findings, rows.Err()
}
