// Package config loads E2E Sentinel API configuration from the process
// environment. There are no baked-in defaults for security-relevant values
// (database DSN, redis address) — they must be set explicitly, per the
// spec's "no destructive/silent defaults" principle.
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config holds all runtime configuration for the API process.
type Config struct {
	// HTTPAddr is the address the API HTTP server listens on, e.g. ":8080".
	HTTPAddr string

	// DatabaseURL is the PostgreSQL connection string (e.g.
	// postgres://user:pass@host:5432/dbname?sslmode=disable).
	DatabaseURL string

	// RedisAddr is the host:port of the Redis instance.
	RedisAddr string
	// RedisPassword is optional.
	RedisPassword string

	// MigrationsDir is the directory containing versioned .sql migration
	// files.
	MigrationsDir string

	// LogLevel is one of: debug, info, warn, error.
	LogLevel string

	// Environment is a free-form label (e.g. "local", "development") used
	// only for logging/diagnostics in Phase 0. It is distinct from the
	// per-project Environment entity defined in later phases.
	Environment string

	// ShutdownTimeout bounds graceful shutdown.
	ShutdownTimeout time.Duration

	// DockerSocketPath is where the Docker Engine Unix socket is expected.
	// This has a conventional default (unlike DatabaseURL/RedisAddr) because
	// Docker discovery is optional and self-degrades when the socket isn't
	// there — it is never required for the API to function.
	DockerSocketPath string

	// RunnerImage is the pre-built Playwright runner image tag. Never
	// pulled at runtime — it must already exist on the Docker daemon
	// (built via `make up` / `docker compose build`).
	RunnerImage string

	// RunnerWorkspaceContainerDir is where sentinel-api itself writes
	// generated spec files, from its own container's point of view.
	RunnerWorkspaceContainerDir string

	// RunnerWorkspaceHostDir is the SAME directory's path on the Docker
	// *host* — required because sentinel-api talks to the host's Docker
	// daemon over the mounted socket (Docker-outside-of-Docker), and
	// bind-mount sources for new sibling containers are resolved by the
	// daemon against the host filesystem, not sentinel-api's own
	// container namespace. Empty means "test execution is not
	// configured" — Phase 1-4 features work fine without it; only
	// POST /tests/{id}/run needs it.
	RunnerWorkspaceHostDir string

	// RunnerMemoryBytes and RunnerNanoCPUs bound each disposable runner
	// container's resource usage (spec §11.4).
	RunnerMemoryBytes int64
	RunnerNanoCPUs    int64

	// RunnerTimeout bounds a single test run's wall-clock time.
	RunnerTimeout time.Duration

	// ArtifactsDir is where sentinel-api stores captured run artifacts
	// (stdout/stderr/screenshots/videos/traces) on the local filesystem
	// (spec §4.1 MVP storage backend).
	ArtifactsDir string

	// SecretEncryptionKey encrypts AI provider API keys at rest (spec
	// §16.3, §23.6). Required (no default) whenever AI providers are
	// configured; every other feature works without it. Must decode
	// (base64 standard encoding) to exactly secretstore.KeySize bytes —
	// generate one with `openssl rand -base64 32`.
	SecretEncryptionKey string

	// OllamaAutoDetectURL is the conventional local Ollama endpoint
	// probed by the auto-detect helper (spec §16.2). Never scanned
	// automatically on startup — only on an explicit user request.
	OllamaAutoDetectURL string

	// FixWorkspacesDir is where a fix proposal's unified diff is applied
	// to a throwaway COPY of the target repository (spec §15.2 "Apply in
	// temporary workspace") — never the original repository_path, which
	// stays read-only until POST /fix-proposals/{id}/apply-repository.
	FixWorkspacesDir string

	// AuthEnabled turns on RBAC (spec §19). Defaults to false — every
	// route behaves exactly as in Phases 0-8 unless explicitly opted
	// in, the same pattern as the Docker socket mount and secret
	// encryption.
	AuthEnabled bool
	// AdminEmail and AdminPassword bootstrap the first administrator
	// account (spec §19 "MVP local mode may support a bootstrap
	// administrator") on first startup with AuthEnabled — only used
	// once, when no users exist yet.
	AdminEmail    string
	AdminPassword string
}

// Load reads configuration from environment variables and validates it.
func Load(getenv func(string) string) (Config, error) {
	if getenv == nil {
		getenv = os.Getenv
	}

	cfg := Config{
		HTTPAddr:                    firstNonEmpty(getenv("SENTINEL_HTTP_ADDR"), ":8080"),
		DatabaseURL:                 strings.TrimSpace(getenv("SENTINEL_DATABASE_URL")),
		RedisAddr:                   strings.TrimSpace(getenv("SENTINEL_REDIS_ADDR")),
		RedisPassword:               getenv("SENTINEL_REDIS_PASSWORD"),
		MigrationsDir:               firstNonEmpty(getenv("SENTINEL_MIGRATIONS_DIR"), "migrations"),
		LogLevel:                    firstNonEmpty(strings.ToLower(getenv("SENTINEL_LOG_LEVEL")), "info"),
		Environment:                 firstNonEmpty(getenv("SENTINEL_ENVIRONMENT"), "local"),
		ShutdownTimeout:             10 * time.Second,
		DockerSocketPath:            firstNonEmpty(getenv("SENTINEL_DOCKER_SOCKET"), "/var/run/docker.sock"),
		RunnerImage:                 firstNonEmpty(getenv("SENTINEL_RUNNER_IMAGE"), "e2e-sentinel-playwright-runner:latest"),
		RunnerWorkspaceContainerDir: firstNonEmpty(getenv("SENTINEL_RUNNER_WORKSPACE_DIR"), "/runner-workspaces"),
		RunnerWorkspaceHostDir:      getenv("SENTINEL_RUNNER_HOST_WORKSPACE_DIR"),
		RunnerMemoryBytes:           1 << 30, // 1 GiB
		RunnerNanoCPUs:              1_000_000_000,
		RunnerTimeout:               2 * time.Minute,
		ArtifactsDir:                firstNonEmpty(getenv("SENTINEL_ARTIFACTS_DIR"), "/data/artifacts"),
		SecretEncryptionKey:         strings.TrimSpace(getenv("SENTINEL_SECRET_ENCRYPTION_KEY")),
		OllamaAutoDetectURL:         firstNonEmpty(getenv("SENTINEL_OLLAMA_AUTODETECT_URL"), "http://host.docker.internal:11434"),
		FixWorkspacesDir:            firstNonEmpty(getenv("SENTINEL_FIX_WORKSPACES_DIR"), "/data/fix-workspaces"),
		AdminEmail:                  strings.TrimSpace(getenv("SENTINEL_ADMIN_EMAIL")),
		AdminPassword:               getenv("SENTINEL_ADMIN_PASSWORD"),
	}

	if raw := getenv("SENTINEL_AUTH_ENABLED"); raw != "" {
		enabled, err := strconv.ParseBool(raw)
		if err != nil {
			return Config{}, fmt.Errorf("config: invalid SENTINEL_AUTH_ENABLED: %w", err)
		}
		cfg.AuthEnabled = enabled
	}

	if raw := getenv("SENTINEL_SHUTDOWN_TIMEOUT_SECONDS"); raw != "" {
		seconds, err := strconv.Atoi(raw)
		if err != nil {
			return Config{}, fmt.Errorf("config: invalid SENTINEL_SHUTDOWN_TIMEOUT_SECONDS: %w", err)
		}
		cfg.ShutdownTimeout = time.Duration(seconds) * time.Second
	}

	if raw := getenv("SENTINEL_RUNNER_TIMEOUT_SECONDS"); raw != "" {
		seconds, err := strconv.Atoi(raw)
		if err != nil {
			return Config{}, fmt.Errorf("config: invalid SENTINEL_RUNNER_TIMEOUT_SECONDS: %w", err)
		}
		cfg.RunnerTimeout = time.Duration(seconds) * time.Second
	}

	if err := cfg.validate(); err != nil {
		return Config{}, err
	}

	return cfg, nil
}

func (c Config) validate() error {
	var missing []string
	if c.DatabaseURL == "" {
		missing = append(missing, "SENTINEL_DATABASE_URL")
	}
	if c.RedisAddr == "" {
		missing = append(missing, "SENTINEL_REDIS_ADDR")
	}
	if len(missing) > 0 {
		return fmt.Errorf("config: missing required environment variables: %s", strings.Join(missing, ", "))
	}

	switch c.LogLevel {
	case "debug", "info", "warn", "error":
	default:
		return fmt.Errorf("config: invalid SENTINEL_LOG_LEVEL %q (want debug|info|warn|error)", c.LogLevel)
	}

	return nil
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
