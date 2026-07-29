package auth

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PostgresStore is the production Store backed by the users/sessions
// tables (migrations/0010_auth.sql).
type PostgresStore struct {
	pool *pgxpool.Pool
}

func NewPostgresStore(pool *pgxpool.Pool) *PostgresStore {
	return &PostgresStore{pool: pool}
}

func (s *PostgresStore) CreateUser(ctx context.Context, u User) (User, error) {
	if !ValidRole(u.Role) {
		return User{}, ErrInvalidRole
	}
	row := s.pool.QueryRow(ctx, `
		INSERT INTO users (email, password_hash, role) VALUES ($1, $2, $3)
		RETURNING id, email, password_hash, role, created_at, updated_at
	`, u.Email, u.PasswordHash, u.Role)
	user, err := scanUser(row)
	if err != nil && strings.Contains(err.Error(), "duplicate key") {
		return User{}, ErrEmailTaken
	}
	return user, err
}

func (s *PostgresStore) GetUserByEmail(ctx context.Context, email string) (User, error) {
	row := s.pool.QueryRow(ctx, `
		SELECT id, email, password_hash, role, created_at, updated_at FROM users WHERE lower(email) = lower($1)
	`, email)
	u, err := scanUser(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return User{}, ErrNotFound
	}
	return u, err
}

func (s *PostgresStore) GetUserByID(ctx context.Context, id string) (User, error) {
	row := s.pool.QueryRow(ctx, `
		SELECT id, email, password_hash, role, created_at, updated_at FROM users WHERE id = $1
	`, id)
	u, err := scanUser(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return User{}, ErrNotFound
	}
	return u, err
}

func (s *PostgresStore) CountUsers(ctx context.Context) (int, error) {
	var count int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM users`).Scan(&count); err != nil {
		return 0, fmt.Errorf("auth: counting users: %w", err)
	}
	return count, nil
}

func (s *PostgresStore) ListUsers(ctx context.Context) ([]User, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, email, password_hash, role, created_at, updated_at FROM users ORDER BY email
	`)
	if err != nil {
		return nil, fmt.Errorf("auth: listing users: %w", err)
	}
	defer rows.Close()
	var users []User
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		users = append(users, u)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("auth: listing users: %w", err)
	}
	return users, nil
}

func (s *PostgresStore) CreateSession(ctx context.Context, sess Session) (Session, error) {
	row := s.pool.QueryRow(ctx, `
		INSERT INTO sessions (user_id, token_hash, expires_at) VALUES ($1, $2, $3)
		RETURNING id, user_id, token_hash, expires_at, created_at
	`, sess.UserID, sess.TokenHash, sess.ExpiresAt)
	return scanSession(row)
}

func (s *PostgresStore) GetSessionByTokenHash(ctx context.Context, tokenHash string) (Session, error) {
	row := s.pool.QueryRow(ctx, `
		SELECT id, user_id, token_hash, expires_at, created_at FROM sessions WHERE token_hash = $1
	`, tokenHash)
	sess, err := scanSession(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return Session{}, ErrSessionNotFound
	}
	if err != nil {
		return Session{}, err
	}
	if time.Now().After(sess.ExpiresAt) {
		return Session{}, ErrSessionExpired
	}
	return sess, nil
}

func (s *PostgresStore) DeleteSession(ctx context.Context, id string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM sessions WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("auth: deleting session: %w", err)
	}
	return nil
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanUser(row rowScanner) (User, error) {
	var u User
	if err := row.Scan(&u.ID, &u.Email, &u.PasswordHash, &u.Role, &u.CreatedAt, &u.UpdatedAt); err != nil {
		return User{}, fmt.Errorf("auth: scanning user row: %w", err)
	}
	return u, nil
}

func scanSession(row rowScanner) (Session, error) {
	var sess Session
	if err := row.Scan(&sess.ID, &sess.UserID, &sess.TokenHash, &sess.ExpiresAt, &sess.CreatedAt); err != nil {
		return Session{}, fmt.Errorf("auth: scanning session row: %w", err)
	}
	return sess, nil
}
