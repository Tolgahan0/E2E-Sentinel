package logging

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestNew_WritesJSONAtConfiguredLevel(t *testing.T) {
	var buf bytes.Buffer
	logger := New(&buf, "warn")

	logger.Info().Msg("should not appear")
	if buf.Len() != 0 {
		t.Fatalf("info log should be suppressed at warn level, got: %s", buf.String())
	}

	logger.Warn().Str("field", "value").Msg("something happened")

	var entry map[string]any
	if err := json.Unmarshal(buf.Bytes(), &entry); err != nil {
		t.Fatalf("log output is not valid JSON: %v (%s)", err, buf.String())
	}
	if entry["message"] != "something happened" {
		t.Errorf("message = %v, want %q", entry["message"], "something happened")
	}
}

func TestNew_UnknownLevelFallsBackToInfo(t *testing.T) {
	var buf bytes.Buffer
	logger := New(&buf, "not-a-real-level")

	logger.Info().Msg("visible")
	if !strings.Contains(buf.String(), "visible") {
		t.Fatalf("expected info-level message to be emitted, got: %s", buf.String())
	}
}

func TestSensitiveFieldName(t *testing.T) {
	cases := map[string]bool{
		"password":            true,
		"Authorization":        true,
		"db_connection_string": true,
		"api_key":              true,
		"username":             false,
		"status":               false,
	}
	for field, want := range cases {
		if got := SensitiveFieldName(field); got != want {
			t.Errorf("SensitiveFieldName(%q) = %v, want %v", field, got, want)
		}
	}
}

func TestRedact(t *testing.T) {
	if got := Redact("password", "hunter2"); got != "[REDACTED]" {
		t.Errorf("Redact(password) = %q, want [REDACTED]", got)
	}
	if got := Redact("username", "alice"); got != "alice" {
		t.Errorf("Redact(username) = %q, want alice", got)
	}
}
