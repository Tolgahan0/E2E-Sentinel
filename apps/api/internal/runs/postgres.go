package runs

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PostgresStore is the production Store backed by test_runs.
type PostgresStore struct {
	pool *pgxpool.Pool
}

func NewPostgresStore(pool *pgxpool.Pool) *PostgresStore {
	return &PostgresStore{pool: pool}
}

const runColumns = `id, project_id, test_case_id, status, runner_type, trigger_type, triggered_by, exit_code, summary, started_at, finished_at, commit_sha`

func (s *PostgresStore) Create(ctx context.Context, run TestRun) (TestRun, error) {
	row := s.pool.QueryRow(ctx, `
		INSERT INTO test_runs (project_id, test_case_id, status, runner_type, trigger_type, triggered_by, commit_sha)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING `+runColumns,
		run.ProjectID, run.TestCaseID, run.Status, run.RunnerType, run.TriggerType, run.TriggeredBy, run.CommitSHA)
	return scanRun(row)
}

func (s *PostgresStore) Get(ctx context.Context, id string) (TestRun, error) {
	row := s.pool.QueryRow(ctx, `SELECT `+runColumns+` FROM test_runs WHERE id = $1`, id)
	run, err := scanRun(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return TestRun{}, ErrNotFound
	}
	return run, err
}

func (s *PostgresStore) ListByProject(ctx context.Context, projectID string) ([]TestRun, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+runColumns+` FROM test_runs WHERE project_id = $1 ORDER BY started_at DESC`, projectID)
	if err != nil {
		return nil, fmt.Errorf("runs: listing: %w", err)
	}
	defer rows.Close()

	var out []TestRun
	for rows.Next() {
		r, err := scanRun(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *PostgresStore) ListByTestCase(ctx context.Context, testCaseID string) ([]TestRun, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+runColumns+` FROM test_runs WHERE test_case_id = $1 ORDER BY started_at`, testCaseID)
	if err != nil {
		return nil, fmt.Errorf("runs: listing by test case: %w", err)
	}
	defer rows.Close()

	var out []TestRun
	for rows.Next() {
		r, err := scanRun(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *PostgresStore) UpdateStatus(ctx context.Context, id, status string, exitCode *int, summary string, finished bool) (TestRun, error) {
	row := s.pool.QueryRow(ctx, `
		UPDATE test_runs SET
			status = $2,
			exit_code = $3,
			summary = $4,
			finished_at = CASE WHEN $5 THEN now() ELSE finished_at END
		WHERE id = $1
		RETURNING `+runColumns, id, status, exitCode, summary, finished)
	run, err := scanRun(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return TestRun{}, ErrNotFound
	}
	return run, err
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanRun(row rowScanner) (TestRun, error) {
	var r TestRun
	err := row.Scan(&r.ID, &r.ProjectID, &r.TestCaseID, &r.Status, &r.RunnerType, &r.TriggerType, &r.TriggeredBy, &r.ExitCode, &r.Summary, &r.StartedAt, &r.FinishedAt, &r.CommitSHA)
	if err != nil {
		return TestRun{}, fmt.Errorf("runs: scanning row: %w", err)
	}
	return r, nil
}
