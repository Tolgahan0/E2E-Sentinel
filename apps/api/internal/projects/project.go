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
