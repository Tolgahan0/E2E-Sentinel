package environments

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PostgresStore is the production Store backed by the environments table.
type PostgresStore struct {
	pool *pgxpool.Pool
}

func NewPostgresStore(pool *pgxpool.Pool) *PostgresStore {
	return &PostgresStore{pool: pool}
}

func (s *PostgresStore) Create(ctx context.Context, env Environment) (Environment, error) {
	if !ValidClassification(env.Classification) {
		return Environment{}, ErrInvalidClassification
	}
	env = RestrictForClassification(env)

	row := s.pool.QueryRow(ctx, `
		INSERT INTO environments (project_id, name, type, base_url, classification, is_production, allow_mutations, allow_load_tests, allow_active_security_scan)
		VALUES ($1, $2, $3, NULLIF($4, ''), $5, $6, $7, $8, $9)
		RETURNING id, project_id, name, type, COALESCE(base_url, ''), classification, is_production, allow_mutations, allow_load_tests, allow_active_security_scan, created_at, updated_at
	`, env.ProjectID, env.Name, env.Type, env.BaseURL, env.Classification, env.IsProduction, env.AllowMutations, env.AllowLoadTests, env.AllowActiveSecurityScan)

	return scanEnvironment(row)
}

func (s *PostgresStore) ListByProject(ctx context.Context, projectID string) ([]Environment, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, project_id, name, type, COALESCE(base_url, ''), classification, is_production, allow_mutations, allow_load_tests, allow_active_security_scan, created_at, updated_at
		FROM environments WHERE project_id = $1 ORDER BY created_at
	`, projectID)
	if err != nil {
		return nil, fmt.Errorf("environments: listing: %w", err)
	}
	defer rows.Close()

	var out []Environment
	for rows.Next() {
		e, err := scanEnvironment(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func (s *PostgresStore) Get(ctx context.Context, id string) (Environment, error) {
	row := s.pool.QueryRow(ctx, `
		SELECT id, project_id, name, type, COALESCE(base_url, ''), classification, is_production, allow_mutations, allow_load_tests, allow_active_security_scan, created_at, updated_at
		FROM environments WHERE id = $1
	`, id)
	e, err := scanEnvironment(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return Environment{}, ErrNotFound
	}
	return e, err
}

func (s *PostgresStore) UpdateClassification(ctx context.Context, id, classification string) (Environment, error) {
	if !ValidClassification(classification) {
		return Environment{}, ErrInvalidClassification
	}
	restricted := RestrictForClassification(Environment{Classification: classification})

	row := s.pool.QueryRow(ctx, `
		UPDATE environments
		SET classification = $2, is_production = $3, allow_mutations = $4, allow_load_tests = $5, allow_active_security_scan = $6, updated_at = now()
		WHERE id = $1
		RETURNING id, project_id, name, type, COALESCE(base_url, ''), classification, is_production, allow_mutations, allow_load_tests, allow_active_security_scan, created_at, updated_at
	`, id, restricted.Classification, restricted.IsProduction, restricted.AllowMutations, restricted.AllowLoadTests, restricted.AllowActiveSecurityScan)

	e, err := scanEnvironment(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return Environment{}, ErrNotFound
	}
	return e, err
}

func (s *PostgresStore) UpdateBaseURL(ctx context.Context, id, baseURL string) (Environment, error) {
	row := s.pool.QueryRow(ctx, `
		UPDATE environments SET base_url = NULLIF($2, ''), updated_at = now()
		WHERE id = $1
		RETURNING id, project_id, name, type, COALESCE(base_url, ''), classification, is_production, allow_mutations, allow_load_tests, allow_active_security_scan, created_at, updated_at
	`, id, baseURL)
	e, err := scanEnvironment(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return Environment{}, ErrNotFound
	}
	return e, err
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanEnvironment(row rowScanner) (Environment, error) {
	var e Environment
	err := row.Scan(&e.ID, &e.ProjectID, &e.Name, &e.Type, &e.BaseURL, &e.Classification, &e.IsProduction, &e.AllowMutations, &e.AllowLoadTests, &e.AllowActiveSecurityScan, &e.CreatedAt, &e.UpdatedAt)
	if err != nil {
		return Environment{}, fmt.Errorf("environments: scanning row: %w", err)
	}
	return e, nil
}
