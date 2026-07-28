// Package environments manages the Environment entity. Every project gets
// a default environment on creation, classified "local" by default.
// Unknown/production classifications are handled restrictively: setting a
// classification to "production" or "unknown" forces every mutation-class
// permission back off (spec §2.6, §6.2) — the caller cannot set them in
// the same call.
package environments

import (
	"context"
	"errors"
	"time"
)

const (
	ClassificationLocal       = "local"
	ClassificationDevelopment = "development"
	ClassificationTest        = "test"
	ClassificationStaging     = "staging"
	ClassificationProduction  = "production"
	ClassificationUnknown     = "unknown"
)

var validClassifications = map[string]bool{
	ClassificationLocal: true, ClassificationDevelopment: true, ClassificationTest: true,
	ClassificationStaging: true, ClassificationProduction: true, ClassificationUnknown: true,
}

// ErrNotFound is returned when an environment ID does not exist.
var ErrNotFound = errors.New("environments: not found")

// ErrInvalidClassification is returned for an unrecognized classification value.
var ErrInvalidClassification = errors.New("environments: invalid classification")

// Environment is a named deployment target for a project.
type Environment struct {
	ID                      string
	ProjectID               string
	Name                    string
	Type                    string
	BaseURL                 string
	Classification          string
	IsProduction            bool
	AllowMutations          bool
	AllowLoadTests          bool
	AllowActiveSecurityScan bool
	CreatedAt               time.Time
	UpdatedAt               time.Time
}

// ValidClassification reports whether value is one of the known classifications.
func ValidClassification(value string) bool {
	return validClassifications[value]
}

// RestrictForClassification returns env with every mutation-class
// permission forced off if classification is production or unknown,
// regardless of what was previously set — restrictive-by-default per
// spec §2.6 ("Unknown environments must be handled restrictively").
func RestrictForClassification(env Environment) Environment {
	env.IsProduction = env.Classification == ClassificationProduction
	if env.Classification == ClassificationProduction || env.Classification == ClassificationUnknown {
		env.AllowMutations = false
		env.AllowLoadTests = false
		env.AllowActiveSecurityScan = false
	}
	return env
}

// Store persists and retrieves environments.
type Store interface {
	Create(ctx context.Context, env Environment) (Environment, error)
	ListByProject(ctx context.Context, projectID string) ([]Environment, error)
	Get(ctx context.Context, id string) (Environment, error)
	UpdateClassification(ctx context.Context, id, classification string) (Environment, error)
}

// DefaultForProject builds the environment auto-created alongside every
// new project (first-run wizard step 7, spec §30).
func DefaultForProject(projectID string) Environment {
	return RestrictForClassification(Environment{
		ProjectID:      projectID,
		Name:           "default",
		Type:           "local",
		Classification: ClassificationLocal,
	})
}
