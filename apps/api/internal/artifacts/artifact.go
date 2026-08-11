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
	// KindScreenshotDiff is a visual regression diff image
	// (internal/visualdiff) — a rendering of a run's screenshot against
	// a stored baseline, not a screenshot Playwright itself produced.
	KindScreenshotDiff = "screenshot_diff"
)

// Retention windows (spec §12): default 14 days, failed runs 30 days,
// passed runs 7 days. Applied at save time based on the run outcome;
// periodic deletion is RunRetentionLoop (spec §9 "Retention jobs") — a
// simple ticker-based sweep, not the full idempotency-key/retry/
// dead-letter job system spec §21 describes, which is a much larger,
// separately-reserved piece of infrastructure.
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
	// DeleteExpired removes every artifact (bytes and metadata) whose
	// RetentionUntil has passed, and returns how many were removed.
	DeleteExpired(ctx context.Context, now time.Time) (int, error)
}
