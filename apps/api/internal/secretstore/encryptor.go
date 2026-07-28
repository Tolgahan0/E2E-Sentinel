// Package secretstore encrypts AI provider API keys at rest (spec §16.3,
// §23.6). Plaintext keys are never persisted directly: internal/providers
// stores only an opaque secret_reference_id, and this package is the only
// code path that can turn that reference back into a usable key —
// exclusively for outbound AI provider calls, never for an HTTP response.
package secretstore

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
)

// KeySize is the required length, in bytes, of the encryption key
// (AES-256).
const KeySize = 32

// ErrInvalidKeySize is returned when a key is not exactly KeySize bytes.
var ErrInvalidKeySize = fmt.Errorf("secretstore: encryption key must be exactly %d bytes", KeySize)

// Encryptor performs authenticated (AES-256-GCM) encryption of secret
// values with a single fixed key, supplied at process startup — never
// generated implicitly, since an implicit key would be lost on restart
// and silently orphan every stored secret.
type Encryptor struct {
	gcm cipher.AEAD
}

// NewEncryptor builds an Encryptor from a raw 32-byte key.
func NewEncryptor(key []byte) (*Encryptor, error) {
	if len(key) != KeySize {
		return nil, ErrInvalidKeySize
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("secretstore: building cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("secretstore: building GCM: %w", err)
	}
	return &Encryptor{gcm: gcm}, nil
}

// Encrypt returns ciphertext and the nonce used to produce it. Store both;
// Decrypt needs the exact nonce.
func (e *Encryptor) Encrypt(plaintext string) (ciphertext, nonce []byte, err error) {
	nonce = make([]byte, e.gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, nil, fmt.Errorf("secretstore: generating nonce: %w", err)
	}
	ciphertext = e.gcm.Seal(nil, nonce, []byte(plaintext), nil)
	return ciphertext, nonce, nil
}

// ErrDecryptionFailed is returned when ciphertext cannot be authenticated
// (wrong key, wrong nonce, or the data was tampered with).
var ErrDecryptionFailed = errors.New("secretstore: decryption failed")

// Decrypt reverses Encrypt.
func (e *Encryptor) Decrypt(ciphertext, nonce []byte) (string, error) {
	plaintext, err := e.gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", ErrDecryptionFailed
	}
	return string(plaintext), nil
}
