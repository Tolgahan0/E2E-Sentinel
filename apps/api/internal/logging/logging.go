// Package logging provides structured (JSON) logging for E2E Sentinel.
//
// Callers must never pass secret values (API keys, passwords, tokens,
// connection strings) as log fields. Use Redact for any value whose
// origin is untrusted or which might contain a credential fragment.
package logging

import (
	"io"
	"os"
	"strings"

	"github.com/rs/zerolog"
)

// New builds a zerolog.Logger writing structured JSON to w at the given
// level ("debug", "info", "warn", "error"). Unknown levels fall back to
// "info" rather than failing, since logging setup must never be able to
// crash the process.
func New(w io.Writer, level string) zerolog.Logger {
	if w == nil {
		w = os.Stdout
	}

	zerolog.TimeFieldFormat = zerolog.TimeFormatUnix
	logger := zerolog.New(w).With().Timestamp().Logger()

	switch strings.ToLower(level) {
	case "debug":
		logger = logger.Level(zerolog.DebugLevel)
	case "warn":
		logger = logger.Level(zerolog.WarnLevel)
	case "error":
		logger = logger.Level(zerolog.ErrorLevel)
	default:
		logger = logger.Level(zerolog.InfoLevel)
	}

	return logger
}

// redactedFieldNames are substrings that, when found (case-insensitively)
// in a field name, cause Redact to replace the value unconditionally.
var redactedFieldNames = []string{
	"password", "secret", "token", "authorization", "cookie",
	"api_key", "apikey", "private_key", "dsn", "connection_string",
}

// SensitiveFieldName reports whether a field name looks like it holds a
// credential, so callers can decide to redact its value before logging.
func SensitiveFieldName(name string) bool {
	lower := strings.ToLower(name)
	for _, needle := range redactedFieldNames {
		if strings.Contains(lower, needle) {
			return true
		}
	}
	return false
}

// Redact returns "[REDACTED]" if fieldName looks sensitive, otherwise
// returns value unchanged.
func Redact(fieldName, value string) string {
	if SensitiveFieldName(fieldName) {
		return "[REDACTED]"
	}
	return value
}
