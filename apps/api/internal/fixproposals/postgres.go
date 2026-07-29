package fixproposals

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PostgresStore is the production Store backed by the fix_proposals
// table (migrations/0009_fix_proposals.sql).
type PostgresStore struct {
	pool *pgxpool.Pool
}

func NewPostgresStore(pool *pgxpool.Pool) *PostgresStore {
	return &PostgresStore{pool: pool}
}

func (s *PostgresStore) Create(ctx context.Context, fp FixProposal) (FixProposal, error) {
	if fp.ApprovalStatus == "" {
		fp.ApprovalStatus = StatusPendingReview
	}

	filesChanged, err := json.Marshal(nonNil(fp.FilesChanged))
	if err != nil {
		return FixProposal{}, fmt.Errorf("fixproposals: encoding files_changed: %w", err)
	}
	regressionTestIDs, err := json.Marshal(nonNil(fp.RegressionTestIDs))
	if err != nil {
		return FixProposal{}, fmt.Errorf("fixproposals: encoding regression_test_ids: %w", err)
	}

	row := s.pool.QueryRow(ctx, `
		INSERT INTO fix_proposals (
			project_id, bug_id, title, description, risk_level, assumptions, potential_side_effects,
			rollback_guidance, files_changed, unified_diff, regression_test_ids, ai_provider, ai_model,
			generated_at, approval_status
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)
		RETURNING `+selectColumns,
		fp.ProjectID, fp.BugID, fp.Title, fp.Description, fp.RiskLevel, fp.Assumptions, fp.PotentialSideEffects,
		fp.RollbackGuidance, filesChanged, fp.UnifiedDiff, regressionTestIDs, fp.AIProvider, fp.AIModel,
		fp.GeneratedAt, fp.ApprovalStatus,
	)
	return scanFixProposal(row)
}

func (s *PostgresStore) Get(ctx context.Context, id string) (FixProposal, error) {
	row := s.pool.QueryRow(ctx, `SELECT `+selectColumns+` FROM fix_proposals WHERE id = $1`, id)
	fp, err := scanFixProposal(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return FixProposal{}, ErrNotFound
	}
	return fp, err
}

func (s *PostgresStore) ListByProject(ctx context.Context, projectID string) ([]FixProposal, error) {
	return s.list(ctx, "project_id", projectID)
}

func (s *PostgresStore) ListByBug(ctx context.Context, bugID string) ([]FixProposal, error) {
	return s.list(ctx, "bug_id", bugID)
}

func (s *PostgresStore) list(ctx context.Context, column, value string) ([]FixProposal, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+selectColumns+` FROM fix_proposals WHERE `+column+` = $1 ORDER BY created_at`, value)
	if err != nil {
		return nil, fmt.Errorf("fixproposals: listing: %w", err)
	}
	defer rows.Close()

	var out []FixProposal
	for rows.Next() {
		fp, err := scanFixProposal(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, fp)
	}
	return out, rows.Err()
}

func (s *PostgresStore) UpdateApprovalStatus(ctx context.Context, id, status string) (FixProposal, error) {
	if !ValidStatus(status) {
		return FixProposal{}, ErrInvalidStatus
	}
	row := s.pool.QueryRow(ctx, `
		UPDATE fix_proposals SET approval_status = $2, updated_at = now() WHERE id = $1 RETURNING `+selectColumns,
		id, status,
	)
	fp, err := scanFixProposal(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return FixProposal{}, ErrNotFound
	}
	return fp, err
}

func (s *PostgresStore) UpdateRegressionTestIDs(ctx context.Context, id string, testIDs []string) (FixProposal, error) {
	encoded, err := json.Marshal(nonNil(testIDs))
	if err != nil {
		return FixProposal{}, fmt.Errorf("fixproposals: encoding regression_test_ids: %w", err)
	}
	row := s.pool.QueryRow(ctx, `
		UPDATE fix_proposals SET regression_test_ids = $2, updated_at = now() WHERE id = $1 RETURNING `+selectColumns,
		id, encoded,
	)
	fp, err := scanFixProposal(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return FixProposal{}, ErrNotFound
	}
	return fp, err
}

func (s *PostgresStore) RecordWorkspaceApplication(ctx context.Context, id, workspaceDir string, results []FileResult, appliedAt time.Time) (FixProposal, error) {
	encoded, err := json.Marshal(results)
	if err != nil {
		return FixProposal{}, fmt.Errorf("fixproposals: encoding workspace_apply_results: %w", err)
	}
	row := s.pool.QueryRow(ctx, `
		UPDATE fix_proposals SET
			workspace_dir = $2, workspace_apply_results = $3, workspace_applied_at = $4, updated_at = now()
		WHERE id = $1
		RETURNING `+selectColumns,
		id, workspaceDir, encoded, appliedAt,
	)
	fp, err := scanFixProposal(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return FixProposal{}, ErrNotFound
	}
	return fp, err
}

func (s *PostgresStore) RecordRepositoryApplication(ctx context.Context, id string, results []FileResult, appliedAt time.Time) (FixProposal, error) {
	current, err := s.Get(ctx, id)
	if err != nil {
		return FixProposal{}, err
	}
	if current.RepositoryAppliedAt != nil {
		return FixProposal{}, ErrAlreadyAppliedToRepository
	}

	encoded, err := json.Marshal(results)
	if err != nil {
		return FixProposal{}, fmt.Errorf("fixproposals: encoding repository_apply_results: %w", err)
	}
	row := s.pool.QueryRow(ctx, `
		UPDATE fix_proposals SET
			repository_apply_results = $2, repository_applied_at = $3, updated_at = now()
		WHERE id = $1
		RETURNING `+selectColumns,
		id, encoded, appliedAt,
	)
	fp, err := scanFixProposal(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return FixProposal{}, ErrNotFound
	}
	return fp, err
}

const selectColumns = `
	id, project_id, bug_id, title, description, risk_level, assumptions, potential_side_effects,
	rollback_guidance, files_changed, unified_diff, regression_test_ids, ai_provider, ai_model,
	generated_at, approval_status, workspace_dir, workspace_apply_results, workspace_applied_at,
	repository_apply_results, repository_applied_at, created_at, updated_at
`

type rowScanner interface {
	Scan(dest ...any) error
}

func scanFixProposal(row rowScanner) (FixProposal, error) {
	var fp FixProposal
	var filesChangedJSON, regressionTestIDsJSON, workspaceResultsJSON, repositoryResultsJSON []byte

	err := row.Scan(
		&fp.ID, &fp.ProjectID, &fp.BugID, &fp.Title, &fp.Description, &fp.RiskLevel, &fp.Assumptions, &fp.PotentialSideEffects,
		&fp.RollbackGuidance, &filesChangedJSON, &fp.UnifiedDiff, &regressionTestIDsJSON, &fp.AIProvider, &fp.AIModel,
		&fp.GeneratedAt, &fp.ApprovalStatus, &fp.WorkspaceDir, &workspaceResultsJSON, &fp.WorkspaceAppliedAt,
		&repositoryResultsJSON, &fp.RepositoryAppliedAt, &fp.CreatedAt, &fp.UpdatedAt,
	)
	if err != nil {
		return FixProposal{}, fmt.Errorf("fixproposals: scanning row: %w", err)
	}

	if err := decodeIfPresent(filesChangedJSON, &fp.FilesChanged); err != nil {
		return FixProposal{}, err
	}
	if err := decodeIfPresent(regressionTestIDsJSON, &fp.RegressionTestIDs); err != nil {
		return FixProposal{}, err
	}
	if err := decodeIfPresent(workspaceResultsJSON, &fp.WorkspaceApplyResults); err != nil {
		return FixProposal{}, err
	}
	if err := decodeIfPresent(repositoryResultsJSON, &fp.RepositoryApplyResults); err != nil {
		return FixProposal{}, err
	}
	return fp, nil
}

func decodeIfPresent(data []byte, dest any) error {
	if len(data) == 0 {
		return nil
	}
	if err := json.Unmarshal(data, dest); err != nil {
		return fmt.Errorf("fixproposals: decoding: %w", err)
	}
	return nil
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}
