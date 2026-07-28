// Package artifacts stores test run artifacts (stdout, stderr,
// screenshots, videos, traces) on the local filesystem — the MVP
// storage backend spec §4.1 specifies, with S3-compatible storage as a
// documented later option. Every artifact records a checksum, MIME
// type, and size (spec §12).
package artifacts

import (
	"context"
	"time"
)

// Kinds of artifact.
const (
	KindStdout     = "stdout"
	KindStderr     = "stderr"
	KindScreenshot = "screenshot"
	KindVideo      = "video"
	KindTrace      = "trace"
	KindHAR        = "har"
)

// Retention windows (spec §12): default 14 days, failed runs 30 days,
// passed runs 7 days. Applied at save time based on the run outcome;
// actual periodic deletion is a job-system feature (spec §21) not yet
// implemented — RetentionUntil is recorded now so that job has
// everything it needs later.
const (
	RetentionDefault = 14 * 24 * time.Hour
	RetentionFailed  = 30 * 24 * time.Hour
	RetentionPassed  = 7 * 24 * time.Hour
)

// Artifact is one stored file associated with a test run.
type Artifact struct {
	ID             string
	TestRunID      string
	Kind           string
	MimeType       string
	SizeBytes      int64
	Checksum       string // sha256 hex
	StoragePath    string // path on the artifact filesystem, opaque to callers
	RetentionUntil *time.Time
	CreatedAt      time.Time
}

// RetentionFor returns the retention window for a run's outcome.
func RetentionFor(runStatus string) time.Duration {
	switch runStatus {
	case "failed", "error":
		return RetentionFailed
	case "passed":
		return RetentionPassed
	default:
		return RetentionDefault
	}
}

// Store persists and retrieves artifact bytes plus their metadata.
type Store interface {
	Save(ctx context.Context, testRunID, kind, mimeType string, data []byte, retentionUntil time.Time) (Artifact, error)
	ListByRun(ctx context.Context, testRunID string) ([]Artifact, error)
	Read(ctx context.Context, artifactID string) ([]byte, Artifact, error)
}
