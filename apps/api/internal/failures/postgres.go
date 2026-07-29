package failures

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PostgresStore is the production Store backed by the failures table
// (migrations/0008_failures_and_bugs.sql).
type PostgresStore struct {
	pool *pgxpool.Pool
}

func NewPostgresStore(pool *pgxpool.Pool) *PostgresStore {
	return &PostgresStore{pool: pool}
}

type evidenceRow struct {
	ArtifactIDs []string `json:"artifact_ids"`
}

func (s *PostgresStore) Create(ctx context.Context, f Failure) (Failure, error) {
	evidence, err := json.Marshal(evidenceRow{ArtifactIDs: f.ArtifactIDs})
	if err != nil {
		return Failure{}, fmt.Errorf("failures: encoding evidence: %w", err)
	}

	row := s.pool.QueryRow(ctx, `
		INSERT INTO failures (
			test_run_id, test_case_id, title, severity, failure_type, expected, actual,
			error_message, stack_trace, root_cause_hypothesis, confidence_score, evidence
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
		RETURNING `+selectColumns,
		f.TestRunID, f.TestCaseID, f.Title, f.Severity, f.FailureType, f.Expected, f.Actual,
		f.ErrorMessage, f.StackTrace, f.RootCauseHypothesis, f.ConfidenceScore, evidence,
	)
	return scanFailure(row)
}

func (s *PostgresStore) Get(ctx context.Context, id string) (Failure, error) {
	row := s.pool.QueryRow(ctx, `SELECT `+selectColumns+` FROM failures WHERE id = $1`, id)
	f, err := scanFailure(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return Failure{}, ErrNotFound
	}
	return f, err
}

func (s *PostgresStore) ListByTestCase(ctx context.Context, testCaseID string) ([]Failure, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+selectColumns+` FROM failures WHERE test_case_id = $1 ORDER BY created_at`, testCaseID)
	if err != nil {
		return nil, fmt.Errorf("failures: listing: %w", err)
	}
	defer rows.Close()

	var out []Failure
	for rows.Next() {
		f, err := scanFailure(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

const selectColumns = `
	id, test_run_id, test_case_id, title, severity, failure_type, expected, actual,
	error_message, stack_trace, root_cause_hypothesis, confidence_score, evidence, created_at
`

type rowScanner interface {
	Scan(dest ...any) error
}

func scanFailure(row rowScanner) (Failure, error) {
	var f Failure
	var evidenceJSON []byte

	err := row.Scan(
		&f.ID, &f.TestRunID, &f.TestCaseID, &f.Title, &f.Severity, &f.FailureType, &f.Expected, &f.Actual,
		&f.ErrorMessage, &f.StackTrace, &f.RootCauseHypothesis, &f.ConfidenceScore, &evidenceJSON, &f.CreatedAt,
	)
	if err != nil {
		return Failure{}, fmt.Errorf("failures: scanning row: %w", err)
	}

	if len(evidenceJSON) > 0 {
		var e evidenceRow
		if err := json.Unmarshal(evidenceJSON, &e); err != nil {
			return Failure{}, fmt.Errorf("failures: decoding evidence: %w", err)
		}
		f.ArtifactIDs = e.ArtifactIDs
	}
	return f, nil
}
