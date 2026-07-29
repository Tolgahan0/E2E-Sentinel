package auth

import (
	"errors"
	"fmt"

	"golang.org/x/crypto/bcrypt"
)

// ErrInvalidCredentials is returned by VerifyPassword (and surfaced by
// login as a generic failure) when the password doesn't match — the
// caller must never distinguish "wrong password" from "unknown email"
// in a response, to avoid leaking which emails have accounts.
var ErrInvalidCredentials = errors.New("auth: invalid credentials")

// MinPasswordLength is enforced when creating or changing a password.
const MinPasswordLength = 12

// ErrPasswordTooShort is returned for a password below MinPasswordLength.
var ErrPasswordTooShort = fmt.Errorf("auth: password must be at least %d characters", MinPasswordLength)

// HashPassword hashes a plaintext password with bcrypt.
func HashPassword(plaintext string) (string, error) {
	if len(plaintext) < MinPasswordLength {
		return "", ErrPasswordTooShort
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(plaintext), bcrypt.DefaultCost)
	if err != nil {
		return "", fmt.Errorf("auth: hashing password: %w", err)
	}
	return string(hash), nil
}

// VerifyPassword reports whether plaintext matches hash.
func VerifyPassword(hash, plaintext string) error {
	if err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(plaintext)); err != nil {
		return ErrInvalidCredentials
	}
	return nil
}
