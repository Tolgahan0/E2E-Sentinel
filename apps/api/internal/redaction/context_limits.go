package redaction

import "strings"

// DefaultMaxFileBytes bounds a single file's contribution to AI context
// (spec §16.5 step 8). Applied by future phases that assemble AI context
// from repository files; defined here so the limit lives next to the
// rest of the redaction pipeline it's part of.
const DefaultMaxFileBytes = 200_000

// PathAllowed reports whether path is permitted to be included in AI
// context, per an allowlist of path prefixes (spec §16.5 step 7). An
// empty allowlist permits nothing — callers must opt paths in explicitly
// rather than defaulting to "everything is allowed".
func PathAllowed(path string, allowlist []string) bool {
	for _, prefix := range allowlist {
		if prefix == "" {
			continue
		}
		if path == prefix || strings.HasPrefix(path, strings.TrimSuffix(prefix, "/")+"/") {
			return true
		}
	}
	return false
}

// WithinSizeLimit reports whether size is within maxBytes. A maxBytes of
// 0 means "use DefaultMaxFileBytes".
func WithinSizeLimit(size int64, maxBytes int64) bool {
	if maxBytes <= 0 {
		maxBytes = DefaultMaxFileBytes
	}
	return size <= maxBytes
}
