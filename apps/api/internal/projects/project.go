// Package projects manages the Project entity: the repository/application
// under test. Adding a project never touches the target repository
// beyond validating that its path exists — no files are read here.
package projects

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"time"
)

// Project represents a repository under test.
type Project struct {
	ID               string
	Name             string
	Slug             string
	RepositoryPath   string
	RepositoryType   string
	DefaultBranch    string
	DiscoveryStatus  string
	CurrentMode      string
	LastDiscoveredAt *time.Time
	CreatedAt        time.Time
	UpdatedAt        time.Time

	// GitHubRepo is "owner/repo" on GitHub, or "" — empty means the
	// GitHub CI integration (internal/githubci) is disabled for this
	// project. Not the same thing as RepositoryPath, which is a local
	// filesystem path discovery reads from; this is only ever used to
	// poll GitHub's API and report a commit status back.
	GitHubRepo string
	// GitHubTokenSecretReferenceID is an opaque reference into
	// secretstore.Store (same pattern as providers.Provider.
	// SecretReferenceID) — never the plaintext token itself. Empty
	// means no token has been configured yet.
	GitHubTokenSecretReferenceID string
	// LastCICommitSHA is the most recent commit on GitHubRepo's default
	// branch that internal/githubci has already triggered runs for —
	// lets a poll tick tell "nothing new" apart from "needs a run"
	// without re-running on every tick.
	LastCICommitSHA string
}

const (
	DiscoveryStatusNeverRun = "never_run"
	DiscoveryStatusRunning  = "running"
	DiscoveryStatusComplete = "completed"
	DiscoveryStatusFailed   = "failed"
)

const ModeObserve = "observe"

var (
	// ErrNotFound is returned when a project ID does not exist.
	ErrNotFound = errors.New("projects: not found")
	// ErrInvalidInput is returned for validation failures on create/update.
	ErrInvalidInput = errors.New("projects: invalid input")
)

// Store persists and retrieves projects.
type Store interface {
	Create(ctx context.Context, p Project) (Project, error)
	Get(ctx context.Context, id string) (Project, error)
	List(ctx context.Context) ([]Project, error)
	UpdateName(ctx context.Context, id, name string) (Project, error)
	SetDiscoveryStatus(ctx context.Context, id, status string, lastDiscoveredAt *time.Time) error
	SlugExists(ctx context.Context, slug string) (bool, error)
	// Delete removes a project and (via foreign-key cascade) everything
	// derived from it — see the Postgres implementation's doc comment
	// for the full list.
	Delete(ctx context.Context, id string) error
	// SetGitHubCI configures (or clears, when both arguments are empty)
	// the GitHub CI integration for a project. tokenSecretReferenceID is
	// stored as-is — resolving/creating the secret itself is the
	// caller's job (internal/secretstore), same division of
	// responsibility as providers.Provider.SecretReferenceID.
	SetGitHubCI(ctx context.Context, id, githubRepo, tokenSecretReferenceID string) (Project, error)
	// SetLastCICommitSHA records the most recent commit internal/githubci
	// has triggered runs for, so the next poll tick can tell "nothing
	// new" apart from "needs a run".
	SetLastCICommitSHA(ctx context.Context, id, sha string) error
	// ListWithGitHubCI returns every project with a non-empty GitHubRepo
	// — the working set internal/githubci.RunLoop polls each tick.
	ListWithGitHubCI(ctx context.Context) ([]Project, error)
}

var slugSanitizer = regexp.MustCompile(`[^a-z0-9]+`)

// Slugify derives a URL-safe slug from a project name.
func Slugify(name string) string {
	lower := strings.ToLower(strings.TrimSpace(name))
	slug := slugSanitizer.ReplaceAllString(lower, "-")
	slug = strings.Trim(slug, "-")
	if slug == "" {
		slug = "project"
	}
	return slug
}

// ValidateName reports whether name is acceptable for a new project.
func ValidateName(name string) error {
	if strings.TrimSpace(name) == "" {
		return ErrInvalidInput
	}
	return nil
}
