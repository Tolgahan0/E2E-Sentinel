// Command sentinel is the E2E Sentinel API process.
package main

import (
	"context"
	"errors"
	"flag"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"e2e-sentinel/apps/api/internal/artifacts"
	"e2e-sentinel/apps/api/internal/audit"
	"e2e-sentinel/apps/api/internal/config"
	"e2e-sentinel/apps/api/internal/db"
	"e2e-sentinel/apps/api/internal/discovery"
	"e2e-sentinel/apps/api/internal/dockerclient"
	"e2e-sentinel/apps/api/internal/environments"
	"e2e-sentinel/apps/api/internal/graph"
	"e2e-sentinel/apps/api/internal/httpserver"
	"e2e-sentinel/apps/api/internal/logging"
	"e2e-sentinel/apps/api/internal/planning"
	"e2e-sentinel/apps/api/internal/projects"
	"e2e-sentinel/apps/api/internal/runs"
	"e2e-sentinel/apps/api/internal/services"
)

func main() {
	migrateOnly := flag.Bool("migrate-only", false, "apply pending migrations and exit, without starting the HTTP server")
	flag.Parse()

	if err := run(*migrateOnly); err != nil {
		os.Exit(1)
	}
}

func run(migrateOnly bool) error {
	cfg, err := config.Load(os.Getenv)
	if err != nil {
		// Config validation errors happen before a logger exists; print
		// to stderr directly. cfg never contains a hard-coded secret
		// default, so there is nothing sensitive to accidentally print
		// here — only the names of missing variables.
		println(err.Error())
		return err
	}

	logger := logging.New(os.Stdout, cfg.LogLevel)
	logger.Info().Str("environment", cfg.Environment).Msg("starting e2e-sentinel api")

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	pgPool, err := db.NewPostgresPool(ctx, cfg.DatabaseURL)
	if err != nil {
		logger.Error().Err(err).Msg("connecting to postgres failed")
		return err
	}
	defer pgPool.Close()

	appliedMigrations, err := db.Migrate(ctx, pgPool, cfg.MigrationsDir)
	if err != nil {
		logger.Error().Err(err).Msg("running migrations failed")
		return err
	}
	logger.Info().Strs("applied_migrations", appliedMigrations).Msg("migrations up to date")

	if migrateOnly {
		return nil
	}

	redisClient, err := db.NewRedisClient(ctx, cfg.RedisAddr, cfg.RedisPassword)
	if err != nil {
		logger.Error().Err(err).Msg("connecting to redis failed")
		return err
	}
	defer redisClient.Close()

	recorder := audit.NewPostgresRecorder(pgPool)
	if err := recorder.Record(ctx, audit.Event{
		ActionType:   "system.startup",
		ResourceType: "process",
		Actor:        "system",
		Metadata:     map[string]any{"environment": cfg.Environment},
	}); err != nil {
		logger.Error().Err(err).Msg("recording startup audit event failed")
		return err
	}

	// Docker discovery is optional: this client degrades gracefully (see
	// internal/dockerclient) when the socket isn't mounted or reachable,
	// so it's always safe to construct regardless of environment.
	dockerClient := dockerclient.New(cfg.DockerSocketPath)

	if err := os.MkdirAll(cfg.ArtifactsDir, 0o755); err != nil {
		logger.Error().Err(err).Msg("creating artifacts directory failed")
		return err
	}

	// Test execution (Phase 5) is optional: nil until
	// SENTINEL_RUNNER_HOST_WORKSPACE_DIR is configured, since it requires
	// Docker-outside-of-Docker (spec §11 needs sentinel-api to launch
	// sibling containers; see docs/RUNNER_ISOLATION.md).
	var runner runs.Runner
	if cfg.RunnerWorkspaceHostDir != "" {
		runner = &runs.DockerPlaywrightRunner{
			Docker:                dockerClient,
			Image:                 cfg.RunnerImage,
			WorkspaceContainerDir: cfg.RunnerWorkspaceContainerDir,
			WorkspaceHostDir:      cfg.RunnerWorkspaceHostDir,
			MemoryBytes:           cfg.RunnerMemoryBytes,
			NanoCPUs:              cfg.RunnerNanoCPUs,
			Timeout:               cfg.RunnerTimeout,
		}
		logger.Info().Str("image", cfg.RunnerImage).Msg("test runner configured")
	} else {
		logger.Info().Msg("test runner not configured (SENTINEL_RUNNER_HOST_WORKSPACE_DIR unset) — POST /tests/{id}/run will return 503")
	}

	router := httpserver.NewRouter(httpserver.Dependencies{
		Postgres:     httpserver.PostgresPinger{Pool: pgPool},
		Redis:        httpserver.RedisPinger{Client: redisClient},
		Audit:        recorder,
		Projects:     projects.NewPostgresStore(pgPool),
		Environments: environments.NewPostgresStore(pgPool),
		Discovery:    discovery.NewPostgresStore(pgPool),
		Services:     services.NewPostgresStore(pgPool),
		Graph:        graph.NewPostgresStore(pgPool),
		Planning:     planning.NewPostgresStore(pgPool),
		Runs:         runs.NewPostgresStore(pgPool),
		Artifacts:    artifacts.NewFileStore(pgPool, cfg.ArtifactsDir),
		Docker:       dockerClient,
		Runner:       runner,
		Logger:       logger,
	})

	server := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           router,
		ReadHeaderTimeout: 5 * time.Second,
	}

	serveErr := make(chan error, 1)
	go func() {
		logger.Info().Str("addr", cfg.HTTPAddr).Msg("http server listening")
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serveErr <- err
			return
		}
		serveErr <- nil
	}()

	select {
	case <-ctx.Done():
		logger.Info().Msg("shutdown signal received")
	case err := <-serveErr:
		if err != nil {
			logger.Error().Err(err).Msg("http server failed")
			return err
		}
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		logger.Error().Err(err).Msg("graceful shutdown failed")
		return err
	}

	if err := recorder.Record(context.Background(), audit.Event{
		ActionType:   "system.shutdown",
		ResourceType: "process",
		Actor:        "system",
	}); err != nil {
		logger.Error().Err(err).Msg("recording shutdown audit event failed")
	}

	logger.Info().Msg("shutdown complete")
	return nil
}
