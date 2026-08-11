package githubci

import (
	"context"
	"fmt"
	"sync"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PRTracker remembers the last head SHA this package has already
// triggered a run for, per (project, PR number) — the per-PR
// equivalent of projects.Project.LastCICommitSHA, which only has room
// for one branch tip per project. LastSeenSHA returns "" (not an
// error) for a PR that has never been seen before, matching how a
// fresh project's LastCICommitSHA starts as "" — the caller compares
// against the current head SHA the same way either way.
type PRTracker interface {
	LastSeenSHA(ctx context.Context, projectID string, prNumber int) (string, error)
	SetLastSeenSHA(ctx context.Context, projectID string, prNumber int, sha string) error
}

// PostgresPRTracker is the production PRTracker backed by the
// github_ci_pull_requests table.
type PostgresPRTracker struct {
	pool *pgxpool.Pool
}

func NewPostgresPRTracker(pool *pgxpool.Pool) *PostgresPRTracker {
	return &PostgresPRTracker{pool: pool}
}

func (s *PostgresPRTracker) LastSeenSHA(ctx context.Context, projectID string, prNumber int) (string, error) {
	var sha string
	err := s.pool.QueryRow(ctx,
		`SELECT last_head_sha FROM github_ci_pull_requests WHERE project_id = $1 AND pr_number = $2`,
		projectID, prNumber).Scan(&sha)
	if err == pgx.ErrNoRows {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("githubci: reading last seen PR sha: %w", err)
	}
	return sha, nil
}

func (s *PostgresPRTracker) SetLastSeenSHA(ctx context.Context, projectID string, prNumber int, sha string) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO github_ci_pull_requests (project_id, pr_number, last_head_sha)
		VALUES ($1, $2, $3)
		ON CONFLICT (project_id, pr_number) DO UPDATE SET last_head_sha = $3, updated_at = now()`,
		projectID, prNumber, sha)
	if err != nil {
		return fmt.Errorf("githubci: recording last seen PR sha: %w", err)
	}
	return nil
}

// MemoryPRTracker is an in-memory PRTracker for tests.
type MemoryPRTracker struct {
	mu   sync.Mutex
	seen map[string]string // "projectID/prNumber" -> last head SHA
}

func NewMemoryPRTracker() *MemoryPRTracker {
	return &MemoryPRTracker{seen: make(map[string]string)}
}

func prTrackerKey(projectID string, prNumber int) string {
	return fmt.Sprintf("%s/%d", projectID, prNumber)
}

func (s *MemoryPRTracker) LastSeenSHA(_ context.Context, projectID string, prNumber int) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.seen[prTrackerKey(projectID, prNumber)], nil
}

func (s *MemoryPRTracker) SetLastSeenSHA(_ context.Context, projectID string, prNumber int, sha string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.seen[prTrackerKey(projectID, prNumber)] = sha
	return nil
}
