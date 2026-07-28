package secretstore

import (
	"context"
	"fmt"
	"sync"
)

// MemoryStore is an in-process Store for tests. Not for production use.
// It still round-trips through the real Encryptor, so tests exercise the
// same encrypt/decrypt path production code does.
type MemoryStore struct {
	enc *Encryptor

	mu     sync.Mutex
	nextID int
	byID   map[string]encryptedSecret
}

type encryptedSecret struct {
	ciphertext []byte
	nonce      []byte
}

// NewMemoryStore builds a MemoryStore using enc for encryption.
func NewMemoryStore(enc *Encryptor) *MemoryStore {
	return &MemoryStore{enc: enc, byID: map[string]encryptedSecret{}}
}

func (s *MemoryStore) Create(_ context.Context, plaintext string) (string, error) {
	ciphertext, nonce, err := s.enc.Encrypt(plaintext)
	if err != nil {
		return "", err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.nextID++
	id := fmt.Sprintf("mem-secret-%d", s.nextID)
	s.byID[id] = encryptedSecret{ciphertext: ciphertext, nonce: nonce}
	return id, nil
}

func (s *MemoryStore) Resolve(_ context.Context, id string) (string, error) {
	s.mu.Lock()
	secret, ok := s.byID[id]
	s.mu.Unlock()
	if !ok {
		return "", ErrNotFound
	}
	return s.enc.Decrypt(secret.ciphertext, secret.nonce)
}

func (s *MemoryStore) Delete(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.byID[id]; !ok {
		return ErrNotFound
	}
	delete(s.byID, id)
	return nil
}
