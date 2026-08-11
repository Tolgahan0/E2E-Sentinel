package visualdiff

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PostgresStore is the production Store backed by visual_baselines and
// visual_diffs.
type PostgresStore struct {
	pool *pgxpool.Pool
}

func NewPostgresStore(pool *pgxpool.Pool) *PostgresStore {
	return &PostgresStore{pool: pool}
}

func (s *PostgresStore) GetBaseline(ctx context.Context, testCaseID string) (Baseline, error) {
	row := s.pool.QueryRow(ctx, `
		SELECT id, test_case_id, artifact_id, accepted_by, accepted_at
		FROM visual_baselines WHERE test_case_id = $1
	`, testCaseID)

	var b Baseline
	err := row.Scan(&b.ID, &b.TestCaseID, &b.ArtifactID, &b.AcceptedBy, &b.AcceptedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Baseline{}, ErrNotFound
	}
	if err != nil {
		return Baseline{}, fmt.Errorf("visualdiff: scanning baseline: %w", err)
	}
	return b, nil
}

func (s *PostgresStore) SetBaseline(ctx context.Context, testCaseID, artifactID, acceptedBy string) (Baseline, error) {
	row := s.pool.QueryRow(ctx, `
		INSERT INTO visual_baselines (test_case_id, artifact_id, accepted_by, accepted_at)
		VALUES ($1, $2, $3, now())
		ON CONFLICT (test_case_id) DO UPDATE SET
			artifact_id = EXCLUDED.artifact_id,
			accepted_by = EXCLUDED.accepted_by,
			accepted_at = EXCLUDED.accepted_at
		RETURNING id, test_case_id, artifact_id, accepted_by, accepted_at
	`, testCaseID, artifactID, acceptedBy)

	var b Baseline
	err := row.Scan(&b.ID, &b.TestCaseID, &b.ArtifactID, &b.AcceptedBy, &b.AcceptedAt)
	if err != nil {
		return Baseline{}, fmt.Errorf("visualdiff: upserting baseline: %w", err)
	}
	return b, nil
}

func (s *PostgresStore) CreateDiff(ctx context.Context, d Diff) (Diff, error) {
	if d.Status == "" {
		d.Status = StatusPendingReview
	}
	row := s.pool.QueryRow(ctx, `
		INSERT INTO visual_diffs (project_id, test_run_id, test_case_id, baseline_artifact_id, current_artifact_id, diff_artifact_id, percent_changed, status)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING `+diffColumns,
		d.ProjectID, d.TestRunID, d.TestCaseID, d.BaselineArtifactID, d.CurrentArtifactID, d.DiffArtifactID, d.PercentChanged, d.Status)
	return scanDiff(row)
}

func (s *PostgresStore) Get(ctx context.Context, id string) (Diff, error) {
	row := s.pool.QueryRow(ctx, `SELECT `+diffColumns+` FROM visual_diffs WHERE id = $1`, id)
	d, err := scanDiff(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return Diff{}, ErrNotFound
	}
	return d, err
}

func (s *PostgresStore) ListByProject(ctx context.Context, projectID string) ([]Diff, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT `+diffColumns+` FROM visual_diffs
		WHERE project_id = $1
		ORDER BY (status = 'pending_review') DESC, created_at DESC
	`, projectID)
	if err != nil {
		return nil, fmt.Errorf("visualdiff: listing diffs: %w", err)
	}
	defer rows.Close()

	var out []Diff
	for rows.Next() {
		d, err := scanDiff(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

func (s *PostgresStore) UpdateStatus(ctx context.Context, id, status, reviewedBy string) (Diff, error) {
	row := s.pool.QueryRow(ctx, `
		UPDATE visual_diffs SET status = $2, reviewed_by = $3, reviewed_at = now()
		WHERE id = $1
		RETURNING `+diffColumns, id, status, reviewedBy)
	d, err := scanDiff(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return Diff{}, ErrNotFound
	}
	return d, err
}

const diffColumns = `id, project_id, test_run_id, test_case_id, baseline_artifact_id, current_artifact_id, diff_artifact_id, percent_changed, status, reviewed_by, reviewed_at, created_at`

type rowScanner interface {
	Scan(dest ...any) error
}

func scanDiff(row rowScanner) (Diff, error) {
	var d Diff
	err := row.Scan(&d.ID, &d.ProjectID, &d.TestRunID, &d.TestCaseID, &d.BaselineArtifactID, &d.CurrentArtifactID, &d.DiffArtifactID, &d.PercentChanged, &d.Status, &d.ReviewedBy, &d.ReviewedAt, &d.CreatedAt)
	if err != nil {
		return Diff{}, fmt.Errorf("visualdiff: scanning diff: %w", err)
	}
	return d, nil
}
