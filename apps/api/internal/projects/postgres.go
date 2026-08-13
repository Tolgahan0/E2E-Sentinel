package projects

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PostgresStore is the production Store backed by the projects table.
type PostgresStore struct {
	pool *pgxpool.Pool
}

func NewPostgresStore(pool *pgxpool.Pool) *PostgresStore {
	return &PostgresStore{pool: pool}
}

const projectColumns = `id, name, slug, repository_path, repository_type, default_branch, discovery_status, current_mode, last_discovered_at, created_at, updated_at, github_repo, github_token_secret_reference_id, last_ci_commit_sha, visual_diff_threshold`

func (s *PostgresStore) Create(ctx context.Context, p Project) (Project, error) {
	row := s.pool.QueryRow(ctx, `
		INSERT INTO projects (name, slug, repository_path, repository_type, default_branch, discovery_status, current_mode)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING `+projectColumns,
		p.Name, p.Slug, p.RepositoryPath, orDefault(p.RepositoryType, "local"), orDefault(p.DefaultBranch, "main"), orDefault(p.DiscoveryStatus, DiscoveryStatusNeverRun), orDefault(p.CurrentMode, ModeObserve))

	return scanProject(row)
}

func (s *PostgresStore) Get(ctx context.Context, id string) (Project, error) {
	row := s.pool.QueryRow(ctx, `SELECT `+projectColumns+` FROM projects WHERE id = $1`, id)

	p, err := scanProject(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return Project{}, ErrNotFound
	}
	return p, err
}

func (s *PostgresStore) List(ctx context.Context) ([]Project, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+projectColumns+` FROM projects ORDER BY created_at DESC`)
	if err != nil {
		return nil, fmt.Errorf("projects: listing: %w", err)
	}
	defer rows.Close()

	var out []Project
	for rows.Next() {
		p, err := scanProject(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *PostgresStore) ListWithGitHubCI(ctx context.Context) ([]Project, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+projectColumns+` FROM projects WHERE github_repo != '' ORDER BY created_at`)
	if err != nil {
		return nil, fmt.Errorf("projects: listing github-ci projects: %w", err)
	}
	defer rows.Close()

	var out []Project
	for rows.Next() {
		p, err := scanProject(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *PostgresStore) UpdateName(ctx context.Context, id, name string) (Project, error) {
	row := s.pool.QueryRow(ctx, `
		UPDATE projects SET name = $2, updated_at = now() WHERE id = $1
		RETURNING `+projectColumns, id, name)

	p, err := scanProject(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return Project{}, ErrNotFound
	}
	return p, err
}

func (s *PostgresStore) SetGitHubCI(ctx context.Context, id, githubRepo, tokenSecretReferenceID string) (Project, error) {
	row := s.pool.QueryRow(ctx, `
		UPDATE projects SET github_repo = $2, github_token_secret_reference_id = $3, updated_at = now() WHERE id = $1
		RETURNING `+projectColumns, id, githubRepo, nullIfEmpty(tokenSecretReferenceID))

	p, err := scanProject(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return Project{}, ErrNotFound
	}
	return p, err
}

func (s *PostgresStore) SetVisualDiffThreshold(ctx context.Context, id string, threshold float64) (Project, error) {
	row := s.pool.QueryRow(ctx, `
		UPDATE projects SET visual_diff_threshold = $2, updated_at = now() WHERE id = $1
		RETURNING `+projectColumns, id, threshold)

	p, err := scanProject(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return Project{}, ErrNotFound
	}
	return p, err
}

func (s *PostgresStore) SetLastCICommitSHA(ctx context.Context, id, sha string) error {
	tag, err := s.pool.Exec(ctx, `UPDATE projects SET last_ci_commit_sha = $2, updated_at = now() WHERE id = $1`, id, sha)
	if err != nil {
		return fmt.Errorf("projects: updating last CI commit sha: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func nullIfEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func (s *PostgresStore) SetDiscoveryStatus(ctx context.Context, id, status string, lastDiscoveredAt *time.Time) error {
	tag, err := s.pool.Exec(ctx, `
		UPDATE projects SET discovery_status = $2, last_discovered_at = COALESCE($3, last_discovered_at), updated_at = now()
		WHERE id = $1
	`, id, status, lastDiscoveredAt)
	if err != nil {
		return fmt.Errorf("projects: updating discovery status: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// Delete removes a project and, via ON DELETE CASCADE on every child
// table's project_id foreign key, everything derived from it —
// environments, discovered services, graph nodes/edges, test cases,
// test runs, failures, bug reports, fix proposals, Kubernetes
// resources. Artifact bytes on the local filesystem are not cleaned up
// here (spec's retention sweep handles that separately); only the
// artifacts row's metadata cascades.
func (s *PostgresStore) Delete(ctx context.Context, id string) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM projects WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("projects: deleting: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *PostgresStore) SlugExists(ctx context.Context, slug string) (bool, error) {
	var exists bool
	err := s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM projects WHERE slug = $1)`, slug).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("projects: checking slug: %w", err)
	}
	return exists, nil
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanProject(row rowScanner) (Project, error) {
	var p Project
	var tokenRef *string
	err := row.Scan(&p.ID, &p.Name, &p.Slug, &p.RepositoryPath, &p.RepositoryType, &p.DefaultBranch, &p.DiscoveryStatus, &p.CurrentMode, &p.LastDiscoveredAt, &p.CreatedAt, &p.UpdatedAt, &p.GitHubRepo, &tokenRef, &p.LastCICommitSHA, &p.VisualDiffThreshold)
	if err != nil {
		return Project{}, fmt.Errorf("projects: scanning row: %w", err)
	}
	if tokenRef != nil {
		p.GitHubTokenSecretReferenceID = *tokenRef
	}
	return p, nil
}

func orDefault(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}
