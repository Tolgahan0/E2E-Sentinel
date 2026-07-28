// Package settings stores small pieces of application-wide configuration
// that aren't tied to a single project — e.g. AI task routing (spec
// §16.4). Values are opaque JSON so new setting keys never require a
// migration.
package settings

import (
	"context"
	"encoding/json"
)

// Store gets and sets a single JSON value per key.
type Store interface {
	// Get returns the raw JSON value for key and true, or false if the
	// key has never been set.
	Get(ctx context.Context, key string) (value json.RawMessage, ok bool, err error)
	Set(ctx context.Context, key string, value json.RawMessage) error
}
