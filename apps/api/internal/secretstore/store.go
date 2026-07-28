package secretstore

import (
	"context"
	"errors"
)

// ErrNotFound is returned when a secret reference ID does not exist.
var ErrNotFound = errors.New("secretstore: not found")

// Store creates and resolves secret references. Create is the only way
// to obtain an ID; Resolve is called exclusively by server-side code that
// is about to make an outbound AI provider request — never by an HTTP
// handler that would return the value to a client.
type Store interface {
	Create(ctx context.Context, plaintext string) (id string, err error)
	Resolve(ctx context.Context, id string) (plaintext string, err error)
	Delete(ctx context.Context, id string) error
}
