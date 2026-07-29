package bugreports

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PostgresStore is the production Store backed by the bug_reports table
// (migrations/0008_failures_and_bugs.sql).
type PostgresStore struct {
	pool *pgxpool.Pool
}

func NewPostgresStore(pool *pgxpool.Pool) *PostgresStore {
	return &PostgresStore{pool: pool}
}

type evidenceRow struct {
	ArtifactIDs  []string `json:"artifact_ids"`
	ErrorMessage string   `json:"error_message"`
	StackTrace   string   `json:"stack_trace"`
}

func (s *PostgresStore) UpsertFromFailure(ctx context.Context, in UpsertInput) (BugReport, bool, error) {
	var duplicateOf *string
	err := s.pool.QueryRow(ctx, `
		SELECT id FROM bug_reports
		WHERE project_id = $1 AND test_case_id <> $2 AND failure_type = $3 AND status = 'open'
		ORDER BY last_observed_at DESC LIMIT 1
	`, in.ProjectID, in.TestCaseID, in.FailureType).Scan(&duplicateOf)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return BugReport{}, false, fmt.Errorf("bugreports: finding possible duplicate: %w", err)
	}

	steps, err := json.Marshal(in.StepsToReproduce)
	if err != nil {
		return BugReport{}, false, fmt.Errorf("bugreports: encoding steps: %w", err)
	}
	evidence, err := json.Marshal(evidenceRow{ArtifactIDs: in.Evidence.ArtifactIDs, ErrorMessage: in.Evidence.ErrorMessage, StackTrace: in.Evidence.StackTrace})
	if err != nil {
		return BugReport{}, false, fmt.Errorf("bugreports: encoding evidence: %w", err)
	}
	regressionTestIDs, err := json.Marshal(in.RegressionTestIDs)
	if err != nil {
		return BugReport{}, false, fmt.Errorf("bugreports: encoding regression_test_ids: %w", err)
	}

	row := s.pool.QueryRow(ctx, `
		INSERT INTO bug_reports (
			project_id, failure_id, test_case_id, environment_id, title, severity, failure_type,
			affected_service, affected_route, preconditions, steps_to_reproduce, expected_result, actual_result,
			evidence, first_observed_at, last_observed_at, frequency, root_cause_hypothesis, root_cause_confidence,
			flaky_assessment, related_graph_path, regression_test_ids, possible_duplicate_of_id, status
		) VALUES (
			$1, $2, $3, NULLIF($4, '')::uuid, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $15, 1, $16, $17, $18, $19, $20, NULLIF($21, '')::uuid, 'open'
		)
		ON CONFLICT (project_id, test_case_id, failure_type) DO UPDATE SET
			failure_id = EXCLUDED.failure_id, title = EXCLUDED.title, severity = EXCLUDED.severity,
			affected_service = EXCLUDED.affected_service, affected_route = EXCLUDED.affected_route,
			preconditions = EXCLUDED.preconditions, steps_to_reproduce = EXCLUDED.steps_to_reproduce,
			expected_result = EXCLUDED.expected_result, actual_result = EXCLUDED.actual_result,
			evidence = EXCLUDED.evidence, last_observed_at = EXCLUDED.last_observed_at,
			frequency = bug_reports.frequency + 1, root_cause_hypothesis = EXCLUDED.root_cause_hypothesis,
			root_cause_confidence = EXCLUDED.root_cause_confidence, flaky_assessment = EXCLUDED.flaky_assessment,
			related_graph_path = EXCLUDED.related_graph_path, regression_test_ids = EXCLUDED.regression_test_ids,
			status = CASE WHEN bug_reports.status = 'resolved' THEN 'reopened' ELSE bug_reports.status END,
			updated_at = now()
		RETURNING `+selectColumns+`, (xmax = 0) AS inserted
	`,
		in.ProjectID, in.FailureID, in.TestCaseID, in.EnvironmentID, in.Title, in.Severity, in.FailureType,
		in.AffectedService, in.AffectedRoute, in.Preconditions, steps, in.ExpectedResult, in.ActualResult,
		evidence, in.ObservedAt, in.RootCauseHypothesis, in.RootCauseConfidence,
		in.FlakyAssessment, in.RelatedGraphPath, regressionTestIDs, derefOrEmpty(duplicateOf),
	)

	bug, isNew, err := scanBugReportWithInserted(row)
	if err != nil {
		return BugReport{}, false, err
	}
	return bug, isNew, nil
}

func derefOrEmpty(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func (s *PostgresStore) List(ctx context.Context, filter ListFilter) ([]BugReport, error) {
	query := `SELECT ` + selectColumns + ` FROM bug_reports WHERE 1=1`
	var args []any
	arg := func(v any) string {
		args = append(args, v)
		return fmt.Sprintf("$%d", len(args))
	}
	if filter.ProjectID != "" {
		query += " AND project_id = " + arg(filter.ProjectID)
	}
	if filter.Severity != "" {
		query += " AND severity = " + arg(filter.Severity)
	}
	if filter.Status != "" {
		query += " AND status = " + arg(filter.Status)
	}
	if filter.EnvironmentID != "" {
		query += " AND environment_id = " + arg(filter.EnvironmentID)
	}
	if filter.Search != "" {
		query += " AND title ILIKE " + arg("%"+filter.Search+"%")
	}
	query += " ORDER BY last_observed_at DESC"

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("bugreports: listing: %w", err)
	}
	defer rows.Close()

	var out []BugReport
	for rows.Next() {
		b, err := scanBugReport(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

func (s *PostgresStore) Get(ctx context.Context, id string) (BugReport, error) {
	row := s.pool.QueryRow(ctx, `SELECT `+selectColumns+` FROM bug_reports WHERE id = $1`, id)
	b, err := scanBugReport(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return BugReport{}, ErrNotFound
	}
	return b, err
}

func (s *PostgresStore) UpdateStatus(ctx context.Context, id, status string) (BugReport, error) {
	if !ValidStatus(status) {
		return BugReport{}, ErrInvalidStatus
	}
	row := s.pool.QueryRow(ctx, `
		UPDATE bug_reports SET status = $2, updated_at = now() WHERE id = $1 RETURNING `+selectColumns,
		id, status,
	)
	b, err := scanBugReport(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return BugReport{}, ErrNotFound
	}
	return b, err
}

func (s *PostgresStore) AddNote(ctx context.Context, id, author, text string) (BugReport, error) {
	// Wrapped in a single-element array so jsonb `||` concatenates onto
	// the existing notes array rather than merging as object keys.
	note, err := json.Marshal([]Note{{Author: author, Text: text, CreatedAt: time.Now()}})
	if err != nil {
		return BugReport{}, fmt.Errorf("bugreports: encoding note: %w", err)
	}
	row := s.pool.QueryRow(ctx, `
		UPDATE bug_reports SET notes = notes || $2::jsonb, updated_at = now() WHERE id = $1 RETURNING `+selectColumns,
		id, note,
	)
	b, err := scanBugReport(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return BugReport{}, ErrNotFound
	}
	return b, err
}

const selectColumns = `
	id, project_id, COALESCE(failure_id::text, ''), test_case_id, COALESCE(environment_id::text, ''),
	title, severity, failure_type, affected_service, affected_route, preconditions, steps_to_reproduce,
	expected_result, actual_result, evidence, first_observed_at, last_observed_at, frequency,
	root_cause_hypothesis, root_cause_confidence, flaky_assessment, related_graph_path,
	regression_test_ids, COALESCE(possible_duplicate_of_id::text, ''), status, notes, created_at, updated_at
`

type rowScanner interface {
	Scan(dest ...any) error
}

func scanBugReport(row rowScanner) (BugReport, error) {
	b, _, err := scanBugReportRow(row, false)
	return b, err
}

func scanBugReportWithInserted(row rowScanner) (BugReport, bool, error) {
	return scanBugReportRow(row, true)
}

func scanBugReportRow(row rowScanner, withInserted bool) (BugReport, bool, error) {
	var b BugReport
	var stepsJSON, evidenceJSON, regressionJSON, notesJSON []byte
	var inserted bool

	dest := []any{
		&b.ID, &b.ProjectID, &b.FailureID, &b.TestCaseID, &b.EnvironmentID,
		&b.Title, &b.Severity, &b.FailureType, &b.AffectedService, &b.AffectedRoute, &b.Preconditions, &stepsJSON,
		&b.ExpectedResult, &b.ActualResult, &evidenceJSON, &b.FirstObservedAt, &b.LastObservedAt, &b.Frequency,
		&b.RootCauseHypothesis, &b.RootCauseConfidence, &b.FlakyAssessment, &b.RelatedGraphPath,
		&regressionJSON, &b.PossibleDuplicateOfID, &b.Status, &notesJSON, &b.CreatedAt, &b.UpdatedAt,
	}
	if withInserted {
		dest = append(dest, &inserted)
	}

	if err := row.Scan(dest...); err != nil {
		return BugReport{}, false, fmt.Errorf("bugreports: scanning row: %w", err)
	}

	if len(stepsJSON) > 0 {
		if err := json.Unmarshal(stepsJSON, &b.StepsToReproduce); err != nil {
			return BugReport{}, false, fmt.Errorf("bugreports: decoding steps: %w", err)
		}
	}
	if len(evidenceJSON) > 0 {
		var e evidenceRow
		if err := json.Unmarshal(evidenceJSON, &e); err != nil {
			return BugReport{}, false, fmt.Errorf("bugreports: decoding evidence: %w", err)
		}
		b.Evidence = Evidence{ArtifactIDs: e.ArtifactIDs, ErrorMessage: e.ErrorMessage, StackTrace: e.StackTrace}
	}
	if len(regressionJSON) > 0 {
		if err := json.Unmarshal(regressionJSON, &b.RegressionTestIDs); err != nil {
			return BugReport{}, false, fmt.Errorf("bugreports: decoding regression_test_ids: %w", err)
		}
	}
	if len(notesJSON) > 0 {
		if err := json.Unmarshal(notesJSON, &b.Notes); err != nil {
			return BugReport{}, false, fmt.Errorf("bugreports: decoding notes: %w", err)
		}
	}

	return b, inserted, nil
}
