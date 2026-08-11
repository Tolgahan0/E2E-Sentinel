// Command sentinel is the E2E Sentinel API process.
package main

import (
	"context"
	"encoding/base64"
	"errors"
	"flag"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"e2e-sentinel/apps/api/internal/artifacts"
	"e2e-sentinel/apps/api/internal/audit"
	"e2e-sentinel/apps/api/internal/auth"
	"e2e-sentinel/apps/api/internal/bugreports"
	"e2e-sentinel/apps/api/internal/config"
	"e2e-sentinel/apps/api/internal/db"
	"e2e-sentinel/apps/api/internal/discovery"
	"e2e-sentinel/apps/api/internal/dockerclient"
	"e2e-sentinel/apps/api/internal/environments"
	"e2e-sentinel/apps/api/internal/failures"
	"e2e-sentinel/apps/api/internal/fixproposals"
	"e2e-sentinel/apps/api/internal/githubci"
	"e2e-sentinel/apps/api/internal/graph"
	"e2e-sentinel/apps/api/internal/httpserver"
	"e2e-sentinel/apps/api/internal/kubeclient"
	"e2e-sentinel/apps/api/internal/kubediscovery"
	"e2e-sentinel/apps/api/internal/logging"
	"e2e-sentinel/apps/api/internal/metrics"
	"e2e-sentinel/apps/api/internal/planning"
	"e2e-sentinel/apps/api/internal/projects"
	"e2e-sentinel/apps/api/internal/providers"
	"e2e-sentinel/apps/api/internal/runs"
	"e2e-sentinel/apps/api/internal/secretstore"
	"e2e-sentinel/apps/api/internal/services"
	"e2e-sentinel/apps/api/internal/settings"
	"e2e-sentinel/apps/api/internal/updatecheck"
	"e2e-sentinel/apps/api/internal/visualdiff"
	"e2e-sentinel/apps/api/internal/webhooks"
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
	if err := os.MkdirAll(cfg.FixWorkspacesDir, 0o755); err != nil {
		logger.Error().Err(err).Msg("creating fix workspaces directory failed")
		return err
	}
	artifactStore := artifacts.NewFileStore(pgPool, cfg.ArtifactsDir)

	// Retention (spec §9 "Retention jobs"): a simple ticker-based sweep,
	// not the full job system spec §21 describes. Runs for the process
	// lifetime; stops when the shutdown signal cancels ctx.
	go artifacts.RunRetentionLoop(ctx, artifactStore, artifacts.DefaultRetentionSweepInterval, logger)

	// Test execution (Phase 5) mode selection (config.Config.ExecutionMode's
	// doc comment has the exact "auto" fallback rules) — decided once at
	// startup, same as every other optional-capability field, not
	// re-evaluated per run. "docker" gets the disposable-container
	// isolation guarantee (spec §11.1); "local" runs directly as a host
	// process instead — no container, no Docker socket, materially
	// weaker isolation (see docs/RUNNER_ISOLATION.md's "Local process
	// execution mode").
	useDocker := false
	switch cfg.ExecutionMode {
	case "docker":
		useDocker = true
	case "auto":
		if cfg.RunnerWorkspaceHostDir != "" {
			pingCtx, cancelPing := context.WithTimeout(ctx, 2*time.Second)
			useDocker = dockerClient.Ping(pingCtx) == nil
			cancelPing()
		}
	}

	var runner, webSocketRunner runs.Runner
	switch {
	case useDocker:
		runner = &runs.DockerPlaywrightRunner{
			Docker:                dockerClient,
			Image:                 cfg.RunnerImage,
			WorkspaceContainerDir: cfg.RunnerWorkspaceContainerDir,
			WorkspaceHostDir:      cfg.RunnerWorkspaceHostDir,
			MemoryBytes:           cfg.RunnerMemoryBytes,
			NanoCPUs:              cfg.RunnerNanoCPUs,
			Timeout:               cfg.RunnerTimeout,
		}
		webSocketRunner = &runs.DockerWebSocketRunner{
			Docker:                dockerClient,
			Image:                 cfg.WebSocketRunnerImage,
			WorkspaceContainerDir: cfg.RunnerWorkspaceContainerDir,
			WorkspaceHostDir:      cfg.RunnerWorkspaceHostDir,
			MemoryBytes:           1 << 28, // 256 MiB — a WebSocket smoke test needs far less than a browser
			NanoCPUs:              cfg.RunnerNanoCPUs,
			Timeout:               cfg.RunnerTimeout,
		}
		logger.Info().Str("image", cfg.RunnerImage).Str("websocket_image", cfg.WebSocketRunnerImage).
			Msg("test execution configured: docker (disposable per-run containers)")
	case cfg.ExecutionMode == "docker":
		// Requested explicitly but unavailable (no host workspace dir
		// configured, or the daemon didn't answer a ping) — never
		// silently substitute local mode's weaker isolation for what
		// was asked for by name.
		logger.Info().Msg("test execution not configured (SENTINEL_EXECUTION_MODE=docker but SENTINEL_RUNNER_HOST_WORKSPACE_DIR is unset or the Docker daemon is unreachable) — POST /tests/{id}/run will return 503")
	default:
		runner = runs.NewLocalPlaywrightRunner(cfg.RunnerWorkspaceContainerDir, cfg.RunnerTimeout)
		webSocketRunner = runs.NewLocalWebSocketRunner(cfg.RunnerWorkspaceContainerDir, cfg.RunnerTimeout)
		logger.Warn().Msg("test execution configured: local process (no per-run container isolation — requires `playwright`/`node` on PATH; see docs/RUNNER_ISOLATION.md)")
	}

	// AI provider API key storage (Phase 6) is optional: nil until
	// SENTINEL_SECRET_ENCRYPTION_KEY is configured. Every feature except
	// storing a provider's API key works fine without it (spec §16.6
	// "No-AI Mode") — providers can still be configured and tested for
	// keyless local endpoints like Ollama.
	var secretStore secretstore.Store
	if cfg.SecretEncryptionKey != "" {
		keyBytes, err := base64.StdEncoding.DecodeString(cfg.SecretEncryptionKey)
		if err != nil {
			logger.Error().Err(err).Msg("SENTINEL_SECRET_ENCRYPTION_KEY is not valid base64")
			return err
		}
		encryptor, err := secretstore.NewEncryptor(keyBytes)
		if err != nil {
			logger.Error().Err(err).Msg("SENTINEL_SECRET_ENCRYPTION_KEY is invalid")
			return err
		}
		secretStore = secretstore.NewPostgresStore(pgPool, encryptor)
		logger.Info().Msg("secret encryption configured — AI provider API keys can be stored")
	} else {
		logger.Info().Msg("secret encryption not configured (SENTINEL_SECRET_ENCRYPTION_KEY unset) — providers requiring an API key cannot be created")
	}

	// RBAC (Phase 9) is opt-in: SENTINEL_AUTH_ENABLED defaults to false,
	// so every route behaves exactly as in Phases 0-8 unless explicitly
	// turned on.
	authStore := auth.NewPostgresStore(pgPool)
	if cfg.AuthEnabled {
		created, err := auth.EnsureBootstrapAdmin(ctx, authStore, cfg.AdminEmail, cfg.AdminPassword)
		if err != nil {
			logger.Error().Err(err).Msg("bootstrapping administrator account failed")
			return err
		}
		if created {
			logger.Info().Str("email", cfg.AdminEmail).Msg("bootstrap administrator account created")
		}
		logger.Info().Msg("RBAC enabled (SENTINEL_AUTH_ENABLED=true) — requests must authenticate via POST /auth/login")
	} else {
		logger.Info().Msg("RBAC not enabled (SENTINEL_AUTH_ENABLED unset) — every route is open, as in Phases 0-8")
	}

	// Kubernetes discovery (Phase 10) is optional: nil until a
	// kubeconfig is configured or the process is running in-cluster
	// (spec §7.5). Every other feature is unaffected — the kube-discover
	// route returns 503 until this is set up.
	var kubeAPI httpserver.KubeAPI
	kubeNamespace := cfg.KubeNamespace
	if client, kubeCfg, err := kubeclient.Detect(cfg.KubeConfigPath); err != nil {
		if errors.Is(err, kubeclient.ErrNotConfigured) {
			logger.Info().Msg("kubernetes discovery not configured (SENTINEL_KUBE_CONFIG_PATH unset, not running in-cluster) — POST /projects/{id}/kube-discover will return 503")
		} else {
			logger.Error().Err(err).Msg("kubernetes configuration is invalid; kubernetes discovery disabled")
		}
	} else {
		kubeAPI = client
		if kubeNamespace == "" {
			kubeNamespace = kubeCfg.Namespace // fall back to the kubeconfig context's own namespace
		}
		logLabel := kubeNamespace
		if logLabel == "" {
			logLabel = "(cluster-wide)"
		}
		logger.Info().Str("namespace", logLabel).Msg("kubernetes discovery configured")
	}

	// Update checking (GET /version) is opt-out: SENTINEL_UPDATE_CHECK_ENABLED
	// defaults to true and only ever performs a GET against GitHub's
	// public Releases API — no project data leaves the process. An
	// air-gapped deployment sets it to false; the store then just
	// reports the current version with no comparison performed.
	updateStore := updatecheck.NewStore(cfg.Version)
	if cfg.UpdateCheckEnabled {
		go updatecheck.RunLoop(ctx, updateStore, &http.Client{Timeout: 10 * time.Second}, updatecheck.DefaultRepo, cfg.Version, updatecheck.DefaultInterval, logger)
	} else {
		logger.Info().Msg("update checking disabled (SENTINEL_UPDATE_CHECK_ENABLED=false) — GET /version will report the current version only")
	}

	deps := httpserver.Dependencies{
		Postgres:           httpserver.PostgresPinger{Pool: pgPool},
		Redis:              httpserver.RedisPinger{Client: redisClient},
		Audit:              recorder,
		Projects:           projects.NewPostgresStore(pgPool),
		Environments:       environments.NewPostgresStore(pgPool),
		Discovery:          discovery.NewPostgresStore(pgPool),
		Services:           services.NewPostgresStore(pgPool),
		Graph:              graph.NewPostgresStore(pgPool),
		Planning:           planning.NewPostgresStore(pgPool),
		Runs:               runs.NewPostgresStore(pgPool),
		Artifacts:          artifactStore,
		Providers:          providers.NewPostgresStore(pgPool),
		Settings:           settings.NewPostgresStore(pgPool),
		Failures:           failures.NewPostgresStore(pgPool),
		Bugs:               bugreports.NewPostgresStore(pgPool),
		FixProposals:       fixproposals.NewPostgresStore(pgPool),
		VisualDiffs:        visualdiff.NewPostgresStore(pgPool),
		ProviderHealth:     providers.NewHealthChecker(nil),
		Completer:          providers.NewCompleter(nil),
		FixWorkspacesDir:   cfg.FixWorkspacesDir,
		Docker:             dockerClient,
		Runner:             runner,
		WebSocketRunner:    webSocketRunner,
		Kube:               kubeAPI,
		KubeResources:      kubediscovery.NewPostgresStore(pgPool),
		KubeNamespace:      kubeNamespace,
		Secrets:            secretStore,
		Auth:               authStore,
		AuthEnabled:        cfg.AuthEnabled,
		Metrics:            metrics.NewAppMetrics(metrics.NewRegistry()),
		Webhooks:           webhooks.NewSender(),
		Version:            cfg.Version,
		UpdateCheck:        updateStore,
		UpdateCheckEnabled: cfg.UpdateCheckEnabled,
		Logger:             logger,
	}

	// GitHub CI (spec-external addition — poll-triggered, see
	// internal/githubci's package doc for why this polls rather than
	// receives a webhook) is opt-in and requires secret encryption to
	// be configured too, since a project's GitHub token is stored
	// through the same secretstore AI providers use.
	if cfg.GitHubCIEnabled && secretStore != nil {
		trigger := func(ctx context.Context, testCaseID, triggerType, triggeredBy, commitSHA string) (runs.TestRun, error) {
			return httpserver.TriggerRun(ctx, deps, testCaseID, triggerType, triggeredBy, commitSHA)
		}
		go githubci.RunLoop(ctx, deps.Projects, deps.Runs, deps.Planning, deps.Secrets, githubci.NewClient(nil), trigger, cfg.GitHubCIPollInterval, logger)
		logger.Info().Dur("poll_interval", cfg.GitHubCIPollInterval).Msg("github CI polling enabled")
	} else if cfg.GitHubCIEnabled {
		logger.Info().Msg("github CI enabled (SENTINEL_GITHUB_CI_ENABLED=true) but SENTINEL_SECRET_ENCRYPTION_KEY is unset — a project's GitHub token can't be stored, so polling is disabled")
	} else {
		logger.Info().Msg("github CI polling not enabled (SENTINEL_GITHUB_CI_ENABLED unset) — projects can still be configured, but nothing will poll them")
	}

	router := httpserver.NewRouter(deps)

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
