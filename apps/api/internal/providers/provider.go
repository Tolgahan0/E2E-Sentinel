// Package providers manages AI provider configuration (spec §16) — the
// gateway through which E2E Sentinel would call an LLM in later phases
// (failure analysis, fix generation). This phase delivers configuration,
// health checking, and task routing only: no phase yet makes an actual
// AI call, so the application must and does remain fully usable with
// zero providers configured (spec §16.6 "No-AI Mode").
package providers

import (
	"context"
	"errors"
	"time"
)

// Provider types (spec §16.1).
const (
	TypeOllama           = "ollama"
	TypeOpenAI           = "openai"
	TypeAnthropic        = "anthropic"
	TypeGemini           = "gemini"
	TypeAzureOpenAI      = "azure_openai"
	TypeOpenAICompatible = "openai_compatible"
)

var validTypes = map[string]bool{
	TypeOllama: true, TypeOpenAI: true, TypeAnthropic: true,
	TypeGemini: true, TypeAzureOpenAI: true, TypeOpenAICompatible: true,
}

// ValidType reports whether t is a recognized provider type.
func ValidType(t string) bool {
	return validTypes[t]
}

// Health statuses.
const (
	HealthUnknown = "unknown"
	HealthOK      = "ok"
	HealthError   = "error"
)

// Provider is a configured AI provider connection (spec §6.11).
type Provider struct {
	ID   string
	Type string
	Name string

	BaseURL string
	Model   string

	// SecretReferenceID points into internal/secretstore. Empty means no
	// API key is stored (valid for a local Ollama instance with no auth).
	SecretReferenceID string

	IsLocal bool
	Enabled bool

	// Capabilities lists which task types (see routing.go) this provider
	// may be selected for. Empty means "any task".
	Capabilities []string

	TimeoutSeconds int
	MaxTokens      int
	Temperature    float64

	HealthStatus  string
	LastCheckedAt time.Time

	CreatedAt time.Time
	UpdatedAt time.Time
}

// DefaultTimeoutSeconds is used when a provider is created without an
// explicit timeout.
const DefaultTimeoutSeconds = 30

// ErrNotFound is returned when a provider ID does not exist.
var ErrNotFound = errors.New("providers: not found")

// ErrInvalidType is returned for an unrecognized provider type.
var ErrInvalidType = errors.New("providers: invalid type")

// ErrNameRequired is returned when Name is empty.
var ErrNameRequired = errors.New("providers: name is required")

// Patch describes a partial update to a Provider. A nil field is left
// unchanged. ClearSecretReference removes an existing API key without
// setting a new one (e.g. switching a provider back to unauthenticated).
type Patch struct {
	Name                 *string
	BaseURL              *string
	Model                *string
	SecretReferenceID    *string
	ClearSecretReference bool
	Enabled              *bool
	Capabilities         *[]string
	TimeoutSeconds       *int
	MaxTokens            *int
	Temperature          *float64
}

// Store persists and retrieves providers.
type Store interface {
	Create(ctx context.Context, p Provider) (Provider, error)
	List(ctx context.Context) ([]Provider, error)
	Get(ctx context.Context, id string) (Provider, error)
	Update(ctx context.Context, id string, patch Patch) (Provider, error)
	UpdateHealth(ctx context.Context, id, status string, checkedAt time.Time) (Provider, error)
	Delete(ctx context.Context, id string) error
}

// Validate checks the fields required to create or update a provider,
// independent of storage backend.
func Validate(p Provider) error {
	if !ValidType(p.Type) {
		return ErrInvalidType
	}
	if p.Name == "" {
		return ErrNameRequired
	}
	return nil
}
