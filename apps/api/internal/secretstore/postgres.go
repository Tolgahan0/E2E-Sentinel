package secretstore

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PostgresStore is the production Store backed by the secret_references
// table (migrations/0007_ai_providers.sql). Only ciphertext and its nonce
// are stored — the encryption key itself lives only in process memory,
// sourced from SENTINEL_SECRET_ENCRYPTION_KEY.
type PostgresStore struct {
	pool *pgxpool.Pool
	enc  *Encryptor
}

// NewPostgresStore builds a PostgresStore using enc for encryption.
func NewPostgresStore(pool *pgxpool.Pool, enc *Encryptor) *PostgresStore {
	return &PostgresStore{pool: pool, enc: enc}
}

func (s *PostgresStore) Create(ctx context.Context, plaintext string) (string, error) {
	ciphertext, nonce, err := s.enc.Encrypt(plaintext)
	if err != nil {
		return "", err
	}

	var id string
	err = s.pool.QueryRow(ctx, `
		INSERT INTO secret_references (ciphertext, nonce) VALUES ($1, $2) RETURNING id
	`, ciphertext, nonce).Scan(&id)
	if err != nil {
		return "", fmt.Errorf("secretstore: inserting: %w", err)
	}
	return id, nil
}

func (s *PostgresStore) Resolve(ctx context.Context, id string) (string, error) {
	var ciphertext, nonce []byte
	err := s.pool.QueryRow(ctx, `
		SELECT ciphertext, nonce FROM secret_references WHERE id = $1
	`, id).Scan(&ciphertext, &nonce)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", fmt.Errorf("secretstore: selecting: %w", err)
	}
	return s.enc.Decrypt(ciphertext, nonce)
}

func (s *PostgresStore) Delete(ctx context.Context, id string) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM secret_references WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("secretstore: deleting: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
