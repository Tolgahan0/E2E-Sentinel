package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"time"
)

// Session is an issued bearer token (spec §20 — not itself a spec table
// name, but the natural home for "how a user proves who they are" once
// RBAC exists). Only TokenHash is ever persisted; the raw token is
// returned to the caller exactly once, at login, the same way a secret
// API key is handled in internal/secretstore.
type Session struct {
	ID        string
	UserID    string
	TokenHash string
	ExpiresAt time.Time
	CreatedAt time.Time
}

// ErrSessionNotFound is returned when a token doesn't match any live
// session.
var ErrSessionNotFound = errors.New("auth: session not found")

// ErrSessionExpired is returned when a token matches a session that has
// passed its expiry.
var ErrSessionExpired = errors.New("auth: session expired")

// DefaultSessionTTL bounds how long an issued token stays valid.
const DefaultSessionTTL = 24 * time.Hour

// tokenBytes is the amount of entropy in a raw session token before
// base64 encoding — 256 bits, comfortably unguessable.
const tokenBytes = 32

// GenerateToken returns a new random bearer token and its hash. Only the
// hash is ever stored; HashToken lets a caller re-derive it from a
// presented token to look the session up without ever persisting the
// token itself in plaintext (the same defense-in-depth reasoning as not
// storing plaintext passwords).
func GenerateToken() (token, hash string, err error) {
	raw := make([]byte, tokenBytes)
	if _, err := rand.Read(raw); err != nil {
		return "", "", fmt.Errorf("auth: generating token: %w", err)
	}
	token = base64.RawURLEncoding.EncodeToString(raw)
	return token, HashToken(token), nil
}

// HashToken deterministically hashes a presented bearer token for
// lookup. SHA-256 (not bcrypt) is appropriate here: the input is already
// 256 bits of uniform random entropy, not a low-entropy human password,
// so there's nothing for a slow KDF to protect against beyond what a
// fast, constant-time-comparable hash already provides.
func HashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
