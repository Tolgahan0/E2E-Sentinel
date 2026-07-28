package planning

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PostgresStore is the production Store backed by test_cases.
type PostgresStore struct {
	pool *pgxpool.Pool
}

func NewPostgresStore(pool *pgxpool.Pool) *PostgresStore {
	return &PostgresStore{pool: pool}
}

func (s *PostgresStore) CreateIfAbsent(ctx context.Context, tc TestCase) (TestCase, bool, error) {
	if tc.Steps == nil {
		tc.Steps = []string{}
	}
	if tc.Assertions == nil {
		tc.Assertions = []string{}
	}
	if tc.RequiredCredentials == nil {
		tc.RequiredCredentials = []string{}
	}

	row := s.pool.QueryRow(ctx, `
		INSERT INTO test_cases (project_id, natural_key, title, description, category, framework, status, risk_level, priority, confidence, source, preconditions, steps, assertions, required_credentials, is_mutating, is_production_safe, approval_status, route_path, route_method)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20)
		ON CONFLICT (project_id, natural_key) DO NOTHING
		RETURNING id, project_id, natural_key, title, description, category, framework, status, risk_level, priority, confidence, source, preconditions, steps, assertions, required_credentials, is_mutating, is_production_safe, COALESCE(generated_file_path, ''), approval_status, route_path, route_method
	`, tc.ProjectID, tc.NaturalKey, tc.Title, tc.Description, tc.Category, tc.Framework, tc.Status, tc.RiskLevel, tc.Priority, tc.Confidence, tc.Source, tc.Preconditions, tc.Steps, tc.Assertions, tc.RequiredCredentials, tc.IsMutating, tc.IsProductionSafe, tc.ApprovalStatus, tc.RoutePath, tc.RouteMethod)

	result, err := scanTestCase(row)
	if errors.Is(err, pgx.ErrNoRows) {
		// ON CONFLICT DO NOTHING with no RETURNING row means it already
		// existed; fetch it so the caller always gets the current state.
		existing, getErr := s.getByNaturalKey(ctx, tc.ProjectID, tc.NaturalKey)
		return existing, false, getErr
	}
	if err != nil {
		return TestCase{}, false, err
	}
	return result, true, nil
}

func (s *PostgresStore) getByNaturalKey(ctx context.Context, projectID, naturalKey string) (TestCase, error) {
	row := s.pool.QueryRow(ctx, `
		SELECT id, project_id, natural_key, title, description, category, framework, status, risk_level, priority, confidence, source, preconditions, steps, assertions, required_credentials, is_mutating, is_production_safe, COALESCE(generated_file_path, ''), approval_status, route_path, route_method
		FROM test_cases WHERE project_id = $1 AND natural_key = $2
	`, projectID, naturalKey)
	return scanTestCase(row)
}

func (s *PostgresStore) List(ctx context.Context, projectID string) ([]TestCase, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, project_id, natural_key, title, description, category, framework, status, risk_level, priority, confidence, source, preconditions, steps, assertions, required_credentials, is_mutating, is_production_safe, COALESCE(generated_file_path, ''), approval_status, route_path, route_method
		FROM test_cases WHERE project_id = $1 ORDER BY priority, category, title
	`, projectID)
	if err != nil {
		return nil, fmt.Errorf("planning: listing: %w", err)
	}
	defer rows.Close()

	var out []TestCase
	for rows.Next() {
		tc, err := scanTestCase(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, tc)
	}
	return out, rows.Err()
}

func (s *PostgresStore) Get(ctx context.Context, id string) (TestCase, error) {
	row := s.pool.QueryRow(ctx, `
		SELECT id, project_id, natural_key, title, description, category, framework, status, risk_level, priority, confidence, source, preconditions, steps, assertions, required_credentials, is_mutating, is_production_safe, COALESCE(generated_file_path, ''), approval_status, route_path, route_method
		FROM test_cases WHERE id = $1
	`, id)
	tc, err := scanTestCase(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return TestCase{}, ErrNotFound
	}
	return tc, err
}

func (s *PostgresStore) UpdateApproval(ctx context.Context, id, approvalStatus string) (TestCase, error) {
	status := StatusSuggested
	if approvalStatus == ApprovalApproved {
		status = StatusApproved
	}
	row := s.pool.QueryRow(ctx, `
		UPDATE test_cases SET approval_status = $2, status = CASE WHEN $2 = 'approved' THEN $3 ELSE status END, updated_at = now()
		WHERE id = $1
		RETURNING id, project_id, natural_key, title, description, category, framework, status, risk_level, priority, confidence, source, preconditions, steps, assertions, required_credentials, is_mutating, is_production_safe, COALESCE(generated_file_path, ''), approval_status, route_path, route_method
	`, id, approvalStatus, status)
	tc, err := scanTestCase(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return TestCase{}, ErrNotFound
	}
	return tc, err
}

func (s *PostgresStore) Update(ctx context.Context, id string, title, description, priority string) (TestCase, error) {
	row := s.pool.QueryRow(ctx, `
		UPDATE test_cases SET
			title = CASE WHEN $2 = '' THEN title ELSE $2 END,
			description = CASE WHEN $3 = '' THEN description ELSE $3 END,
			priority = CASE WHEN $4 = '' THEN priority ELSE $4 END,
			updated_at = now()
		WHERE id = $1
		RETURNING id, project_id, natural_key, title, description, category, framework, status, risk_level, priority, confidence, source, preconditions, steps, assertions, required_credentials, is_mutating, is_production_safe, COALESCE(generated_file_path, ''), approval_status, route_path, route_method
	`, id, title, description, priority)
	tc, err := scanTestCase(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return TestCase{}, ErrNotFound
	}
	return tc, err
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanTestCase(row rowScanner) (TestCase, error) {
	var tc TestCase
	err := row.Scan(&tc.ID, &tc.ProjectID, &tc.NaturalKey, &tc.Title, &tc.Description, &tc.Category, &tc.Framework, &tc.Status, &tc.RiskLevel, &tc.Priority, &tc.Confidence, &tc.Source, &tc.Preconditions, &tc.Steps, &tc.Assertions, &tc.RequiredCredentials, &tc.IsMutating, &tc.IsProductionSafe, &tc.GeneratedFilePath, &tc.ApprovalStatus, &tc.RoutePath, &tc.RouteMethod)
	if err != nil {
		return TestCase{}, err
	}
	return tc, nil
}
