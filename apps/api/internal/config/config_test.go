package config

import "testing"

func envMap(overrides map[string]string) func(string) string {
	return func(key string) string {
		return overrides[key]
	}
}

func TestLoad_MissingRequiredValues(t *testing.T) {
	_, err := Load(envMap(map[string]string{}))
	if err == nil {
		t.Fatal("expected error when required env vars are missing")
	}
}

func TestLoad_Defaults(t *testing.T) {
	cfg, err := Load(envMap(map[string]string{
		"SENTINEL_DATABASE_URL": "postgres://sentinel:secret@localhost:5432/sentinel?sslmode=disable",
		"SENTINEL_REDIS_ADDR":   "localhost:6379",
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.HTTPAddr != ":8080" {
		t.Errorf("HTTPAddr = %q, want :8080", cfg.HTTPAddr)
	}
	if cfg.MigrationsDir != "migrations" {
		t.Errorf("MigrationsDir = %q, want migrations", cfg.MigrationsDir)
	}
	if cfg.LogLevel != "info" {
		t.Errorf("LogLevel = %q, want info", cfg.LogLevel)
	}
	if cfg.Environment != "local" {
		t.Errorf("Environment = %q, want local", cfg.Environment)
	}
}

func TestLoad_InvalidLogLevel(t *testing.T) {
	_, err := Load(envMap(map[string]string{
		"SENTINEL_DATABASE_URL": "postgres://sentinel:secret@localhost:5432/sentinel",
		"SENTINEL_REDIS_ADDR":   "localhost:6379",
		"SENTINEL_LOG_LEVEL":    "verbose",
	}))
	if err == nil {
		t.Fatal("expected error for invalid log level")
	}
}

func TestLoad_InvalidShutdownTimeout(t *testing.T) {
	_, err := Load(envMap(map[string]string{
		"SENTINEL_DATABASE_URL":             "postgres://sentinel:secret@localhost:5432/sentinel",
		"SENTINEL_REDIS_ADDR":               "localhost:6379",
		"SENTINEL_SHUTDOWN_TIMEOUT_SECONDS": "not-a-number",
	}))
	if err == nil {
		t.Fatal("expected error for invalid shutdown timeout")
	}
}

func TestLoad_NeverExposesDatabaseURLAsDefault(t *testing.T) {
	// Guard against accidentally introducing a hard-coded default DSN,
	// which would violate the "no destructive/silent defaults" rule.
	cfg, err := Load(envMap(map[string]string{
		"SENTINEL_DATABASE_URL": "postgres://sentinel:secret@localhost:5432/sentinel",
		"SENTINEL_REDIS_ADDR":   "localhost:6379",
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.DatabaseURL != "postgres://sentinel:secret@localhost:5432/sentinel" {
		t.Errorf("DatabaseURL was not passed through verbatim: %q", cfg.DatabaseURL)
	}
}
