package providers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PostgresStore is the production Store backed by the ai_providers table
// (migrations/0007_ai_providers.sql).
type PostgresStore struct {
	pool *pgxpool.Pool
}

func NewPostgresStore(pool *pgxpool.Pool) *PostgresStore {
	return &PostgresStore{pool: pool}
}

func (s *PostgresStore) Create(ctx context.Context, p Provider) (Provider, error) {
	if err := Validate(p); err != nil {
		return Provider{}, err
	}
	if p.TimeoutSeconds == 0 {
		p.TimeoutSeconds = DefaultTimeoutSeconds
	}
	if p.HealthStatus == "" {
		p.HealthStatus = HealthUnknown
	}

	capabilities, err := marshalCapabilities(p.Capabilities)
	if err != nil {
		return Provider{}, err
	}

	row := s.pool.QueryRow(ctx, `
		INSERT INTO ai_providers (type, name, base_url, model, secret_reference_id, is_local, enabled, capabilities, timeout_seconds, max_tokens, temperature, health_status)
		VALUES ($1, $2, $3, $4, NULLIF($5, '')::uuid, $6, $7, $8, $9, $10, $11, $12)
		RETURNING `+selectColumns,
		p.Type, p.Name, p.BaseURL, p.Model, p.SecretReferenceID, p.IsLocal, p.Enabled,
		capabilities, p.TimeoutSeconds, p.MaxTokens, p.Temperature, p.HealthStatus,
	)
	return scanProvider(row)
}

func (s *PostgresStore) List(ctx context.Context) ([]Provider, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+selectColumns+` FROM ai_providers ORDER BY created_at`)
	if err != nil {
		return nil, fmt.Errorf("providers: listing: %w", err)
	}
	defer rows.Close()

	var out []Provider
	for rows.Next() {
		p, err := scanProvider(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *PostgresStore) Get(ctx context.Context, id string) (Provider, error) {
	row := s.pool.QueryRow(ctx, `SELECT `+selectColumns+` FROM ai_providers WHERE id = $1`, id)
	p, err := scanProvider(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return Provider{}, ErrNotFound
	}
	return p, err
}

func (s *PostgresStore) Update(ctx context.Context, id string, patch Patch) (Provider, error) {
	current, err := s.Get(ctx, id)
	if err != nil {
		return Provider{}, err
	}

	if patch.Name != nil {
		current.Name = *patch.Name
	}
	if patch.BaseURL != nil {
		current.BaseURL = *patch.BaseURL
	}
	if patch.Model != nil {
		current.Model = *patch.Model
	}
	if patch.ClearSecretReference {
		current.SecretReferenceID = ""
	} else if patch.SecretReferenceID != nil {
		current.SecretReferenceID = *patch.SecretReferenceID
	}
	if patch.Enabled != nil {
		current.Enabled = *patch.Enabled
	}
	if patch.Capabilities != nil {
		current.Capabilities = *patch.Capabilities
	}
	if patch.TimeoutSeconds != nil {
		current.TimeoutSeconds = *patch.TimeoutSeconds
	}
	if patch.MaxTokens != nil {
		current.MaxTokens = *patch.MaxTokens
	}
	if patch.Temperature != nil {
		current.Temperature = *patch.Temperature
	}
	if current.Name == "" {
		return Provider{}, ErrNameRequired
	}

	capabilities, err := marshalCapabilities(current.Capabilities)
	if err != nil {
		return Provider{}, err
	}

	row := s.pool.QueryRow(ctx, `
		UPDATE ai_providers SET
			name = $2, base_url = $3, model = $4, secret_reference_id = NULLIF($5, '')::uuid,
			enabled = $6, capabilities = $7, timeout_seconds = $8, max_tokens = $9, temperature = $10,
			updated_at = now()
		WHERE id = $1
		RETURNING `+selectColumns,
		id, current.Name, current.BaseURL, current.Model, current.SecretReferenceID,
		current.Enabled, capabilities, current.TimeoutSeconds, current.MaxTokens, current.Temperature,
	)
	p, err := scanProvider(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return Provider{}, ErrNotFound
	}
	return p, err
}

func (s *PostgresStore) UpdateHealth(ctx context.Context, id, status string, checkedAt time.Time) (Provider, error) {
	row := s.pool.QueryRow(ctx, `
		UPDATE ai_providers SET health_status = $2, last_checked_at = $3, updated_at = now()
		WHERE id = $1
		RETURNING `+selectColumns,
		id, status, checkedAt,
	)
	p, err := scanProvider(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return Provider{}, ErrNotFound
	}
	return p, err
}

// Delete removes a provider's own row only. Its secret_reference_id (if
// any) is left in place as an orphaned secret_references row —
// harmless ciphertext with nothing left pointing at it, not cleaned up
// here since it has no user-visible effect (spec §16.3's API-key-never-
// returned guarantee already held while the provider existed). Task
// routing entries (ai.task_routing in settings) that reference this
// provider's ID become dangling; providers.HealthChecker/Completer
// callers already handle a routed provider ID that no longer resolves.
func (s *PostgresStore) Delete(ctx context.Context, id string) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM ai_providers WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("providers: deleting: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

const selectColumns = `
	id, type, name, base_url, model, COALESCE(secret_reference_id::text, ''), is_local, enabled,
	capabilities, timeout_seconds, max_tokens, temperature, health_status,
	last_checked_at, created_at, updated_at
`

type rowScanner interface {
	Scan(dest ...any) error
}

func scanProvider(row rowScanner) (Provider, error) {
	var p Provider
	var capabilitiesJSON []byte
	var lastCheckedAt *time.Time

	err := row.Scan(
		&p.ID, &p.Type, &p.Name, &p.BaseURL, &p.Model, &p.SecretReferenceID, &p.IsLocal, &p.Enabled,
		&capabilitiesJSON, &p.TimeoutSeconds, &p.MaxTokens, &p.Temperature, &p.HealthStatus,
		&lastCheckedAt, &p.CreatedAt, &p.UpdatedAt,
	)
	if err != nil {
		return Provider{}, fmt.Errorf("providers: scanning row: %w", err)
	}

	if lastCheckedAt != nil {
		p.LastCheckedAt = *lastCheckedAt
	}
	if len(capabilitiesJSON) > 0 {
		if err := json.Unmarshal(capabilitiesJSON, &p.Capabilities); err != nil {
			return Provider{}, fmt.Errorf("providers: decoding capabilities: %w", err)
		}
	}
	return p, nil
}

func marshalCapabilities(capabilities []string) ([]byte, error) {
	if capabilities == nil {
		capabilities = []string{}
	}
	data, err := json.Marshal(capabilities)
	if err != nil {
		return nil, fmt.Errorf("providers: encoding capabilities: %w", err)
	}
	return data, nil
}
