package artifacts

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ErrNotFound is returned when an artifact ID does not exist.
var ErrNotFound = errors.New("artifacts: not found")

// FileStore persists artifact bytes under baseDir on the local
// filesystem and metadata in Postgres. baseDir must already exist and
// be writable.
type FileStore struct {
	pool    *pgxpool.Pool
	baseDir string
}

func NewFileStore(pool *pgxpool.Pool, baseDir string) *FileStore {
	return &FileStore{pool: pool, baseDir: baseDir}
}

func (s *FileStore) Save(ctx context.Context, testRunID, kind, mimeType string, data []byte, retentionUntil time.Time) (Artifact, error) {
	sum := sha256.Sum256(data)
	checksum := hex.EncodeToString(sum[:])

	var id string
	if err := s.pool.QueryRow(ctx, `
		INSERT INTO artifacts (test_run_id, kind, mime_type, size_bytes, checksum, storage_path, retention_until)
		VALUES ($1, $2, $3, $4, $5, '', $6)
		RETURNING id
	`, testRunID, kind, mimeType, len(data), checksum, retentionUntil).Scan(&id); err != nil {
		return Artifact{}, fmt.Errorf("artifacts: inserting metadata: %w", err)
	}

	relPath := filepath.Join(testRunID, id+"-"+kind)
	fullPath := filepath.Join(s.baseDir, relPath)
	if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
		s.deleteOrphanedRow(ctx, id)
		return Artifact{}, fmt.Errorf("artifacts: creating directory: %w", err)
	}
	if err := os.WriteFile(fullPath, data, 0o644); err != nil {
		s.deleteOrphanedRow(ctx, id)
		return Artifact{}, fmt.Errorf("artifacts: writing file: %w", err)
	}

	if _, err := s.pool.Exec(ctx, `UPDATE artifacts SET storage_path = $2 WHERE id = $1`, id, relPath); err != nil {
		s.deleteOrphanedRow(ctx, id)
		return Artifact{}, fmt.Errorf("artifacts: recording storage path: %w", err)
	}

	return Artifact{
		ID: id, TestRunID: testRunID, Kind: kind, MimeType: mimeType,
		SizeBytes: int64(len(data)), Checksum: checksum, StoragePath: relPath,
		RetentionUntil: &retentionUntil,
	}, nil
}

// deleteOrphanedRow removes a metadata row inserted by Save when a
// later step (creating the directory, writing the file, or recording
// the storage path) fails — otherwise that row would have an empty or
// wrong storage_path forever, and Read would fail confusingly instead
// of ErrNotFound. Best-effort: uses its own background context since
// the caller is already unwinding an error, and has no logger to report
// a secondary failure to.
func (s *FileStore) deleteOrphanedRow(_ context.Context, id string) {
	_, _ = s.pool.Exec(context.Background(), `DELETE FROM artifacts WHERE id = $1`, id)
}

func (s *FileStore) ListByRun(ctx context.Context, testRunID string) ([]Artifact, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, test_run_id, kind, mime_type, size_bytes, checksum, storage_path, retention_until, created_at
		FROM artifacts WHERE test_run_id = $1 ORDER BY created_at
	`, testRunID)
	if err != nil {
		return nil, fmt.Errorf("artifacts: listing: %w", err)
	}
	defer rows.Close()

	var out []Artifact
	for rows.Next() {
		a, err := scanArtifact(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (s *FileStore) Read(ctx context.Context, artifactID string) ([]byte, Artifact, error) {
	row := s.pool.QueryRow(ctx, `
		SELECT id, test_run_id, kind, mime_type, size_bytes, checksum, storage_path, retention_until, created_at
		FROM artifacts WHERE id = $1
	`, artifactID)
	a, err := scanArtifact(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, Artifact{}, ErrNotFound
	}
	if err != nil {
		return nil, Artifact{}, err
	}

	data, err := os.ReadFile(filepath.Join(s.baseDir, a.StoragePath))
	if err != nil {
		return nil, Artifact{}, fmt.Errorf("artifacts: reading file: %w", err)
	}
	return data, a, nil
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanArtifact(row rowScanner) (Artifact, error) {
	var a Artifact
	err := row.Scan(&a.ID, &a.TestRunID, &a.Kind, &a.MimeType, &a.SizeBytes, &a.Checksum, &a.StoragePath, &a.RetentionUntil, &a.CreatedAt)
	if err != nil {
		return Artifact{}, fmt.Errorf("artifacts: scanning row: %w", err)
	}
	return a, nil
}
