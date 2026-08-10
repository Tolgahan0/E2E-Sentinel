// Package httpserver exposes E2E Sentinel's Phase 0 HTTP API: liveness,
// readiness, and read-only audit event listing. It deliberately has no
// routes that mutate anything outside E2E Sentinel's own audit log.
package httpserver

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/rs/zerolog"

	"e2e-sentinel/apps/api/internal/artifacts"
	"e2e-sentinel/apps/api/internal/audit"
	"e2e-sentinel/apps/api/internal/auth"
	"e2e-sentinel/apps/api/internal/bugreports"
	"e2e-sentinel/apps/api/internal/discovery"
	"e2e-sentinel/apps/api/internal/environments"
	"e2e-sentinel/apps/api/internal/failures"
	"e2e-sentinel/apps/api/internal/fixproposals"
	"e2e-sentinel/apps/api/internal/graph"
	"e2e-sentinel/apps/api/internal/kubediscovery"
	"e2e-sentinel/apps/api/internal/metrics"
	"e2e-sentinel/apps/api/internal/planning"
	"e2e-sentinel/apps/api/internal/projects"
	"e2e-sentinel/apps/api/internal/providers"
	"e2e-sentinel/apps/api/internal/runs"
	"e2e-sentinel/apps/api/internal/secretstore"
	"e2e-sentinel/apps/api/internal/services"
	"e2e-sentinel/apps/api/internal/settings"
	"e2e-sentinel/apps/api/internal/updatecheck"
	"e2e-sentinel/apps/api/internal/webhooks"
)

// Pinger checks connectivity to a dependency. Implemented by thin adapters
// over *pgxpool.Pool and *redis.Client so this package can be unit tested
// without real infrastructure.
type Pinger interface {
	Ping(ctx context.Context) error
}

// Dependencies are the collaborators the HTTP layer needs. All fields are
// required.
type Dependencies struct {
	Postgres     Pinger
	Redis        Pinger
	Audit        audit.Recorder
	Projects     projects.Store
	Environments environments.Store
	Discovery    discovery.Store
	Services     services.Store
	Graph        graph.Store
	Planning     planning.Store
	Runs         runs.Store
	Artifacts    artifacts.Store
	Providers    providers.Store
	Settings     settings.Store
	Failures     failures.Store
	Bugs         bugreports.Store
	FixProposals fixproposals.Store
	// ProviderHealth performs the live "test connection" check (spec
	// §16.3). Always set — building it needs no credentials of its own.
	ProviderHealth *providers.HealthChecker
	// Completer generates a candidate patch from a routed provider (spec
	// §15, Phase 8). Always set — building it needs no credentials of
	// its own; whether a provider is actually routed for fix_generation
	// is checked per-request.
	Completer *providers.Completer
	// FixWorkspacesDir is where a fix proposal's diff is applied to a
	// disposable copy of the repository (spec §15.2).
	FixWorkspacesDir string
	// Docker is optional: nil means "no Docker integration configured",
	// handled the same as an unreachable daemon (spec §25 Phase 2).
	Docker DockerLister
	// Runner is optional: nil means "test execution is not configured"
	// (SENTINEL_RUNNER_HOST_WORKSPACE_DIR unset) — every other feature
	// works fine without it; POST /tests/{id}/run returns 503. Used for
	// every TestCase.Framework except "websocket" (spec §25 Phase 11).
	Runner runs.Runner
	// WebSocketRunner is optional, same nil-means-unconfigured pattern
	// as Runner — a separate field (not a generic map) so this stays
	// consistent with every other capability field in this struct.
	// Selected instead of Runner only for TestCase.Framework ==
	// "websocket".
	WebSocketRunner runs.Runner
	// Secrets is optional: nil means "no encryption key configured"
	// (SENTINEL_SECRET_ENCRYPTION_KEY unset) — every feature except
	// storing a provider API key works fine without it, per spec §16.6
	// "No-AI Mode".
	Secrets secretstore.Store
	// Auth and AuthEnabled implement spec §19's RBAC. AuthEnabled
	// defaults to false (SENTINEL_AUTH_ENABLED unset) — every route
	// behaves exactly as in Phases 0-8 unless an operator explicitly
	// turns this on, the same "safe default, explicit capability"
	// pattern used throughout this project (Docker socket, secret
	// encryption, test execution). Auth is nil when disabled.
	Auth        auth.Store
	AuthEnabled bool
	// RateLimitRPS/RateLimitBurst configure the per-client-IP token
	// bucket (spec §9 "Rate limiting"). Zero means "use the package
	// defaults" (DefaultRateLimitRPS/DefaultRateLimitBurst).
	RateLimitRPS   float64
	RateLimitBurst int
	// Metrics is always set — a fresh *metrics.AppMetrics per
	// Dependencies (never a package-level global), so unrelated router
	// instances (notably, each test) never share counters.
	Metrics *metrics.AppMetrics
	// Webhooks is always set (building it needs no credentials, same as
	// ProviderHealth/Completer) — whether a notification actually goes
	// anywhere depends on whether a URL is configured in Settings under
	// webhooks.SettingsKey; an unconfigured Sender.Send is a no-op, not
	// an error.
	Webhooks *webhooks.Sender
	// Kube is optional: nil means "Kubernetes discovery is not
	// configured" (SENTINEL_KUBE_CONFIG_PATH unset and not running
	// in-cluster) — spec §7.5 Phase 10. Every other feature is
	// unaffected; the kube-discover route returns 503.
	Kube          KubeAPI
	KubeResources kubediscovery.Store
	// KubeNamespace scopes discovery to one namespace; empty means
	// cluster-wide.
	KubeNamespace string
	// Version is this deployment's version label (config.Config.Version
	// — "dev" for a source build, the release tag otherwise). Always
	// set; never empty.
	Version string
	// UpdateCheck is always set — a Store starts out reporting no
	// update available even if SENTINEL_UPDATE_CHECK_ENABLED is false,
	// so GET /version never needs a nil check.
	UpdateCheck *updatecheck.Store
	// UpdateCheckEnabled mirrors config.Config.UpdateCheckEnabled —
	// GET /version surfaces it so the panel can tell "checked, up to
	// date" apart from "checking is turned off", rather than treating
	// both the same way.
	UpdateCheckEnabled bool
	Logger             zerolog.Logger
}

// NewRouter builds the chi router for the API.
func NewRouter(deps Dependencies) http.Handler {
	rps := deps.RateLimitRPS
	if rps == 0 {
		rps = DefaultRateLimitRPS
	}
	burst := deps.RateLimitBurst
	if burst == 0 {
		burst = DefaultRateLimitBurst
	}

	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(requestLogger(deps.Logger))
	r.Use(metricsMiddleware(deps))
	r.Use(middleware.Recoverer)
	r.Use(securityHeaders)
	r.Use(rateLimit(rps, burst))

	r.Get("/health", handleHealth)
	r.Get("/ready", handleReady(deps))
	// Unauthenticated, like /health and /ready — a version string and
	// whether a newer one exists is not sensitive, and the panel's
	// Dashboard needs it before a user has logged in.
	r.Get("/version", handleVersion(deps))
	// Unauthenticated, like /health and /ready — a scrape target is
	// conventionally reached by an internal collector, not a browser
	// user; firewall it the same way as the rest of this deployment
	// (see docs/SECURITY_MODEL.md).
	r.Get("/metrics", handleMetrics(deps))

	r.Route("/api/v1", func(r chi.Router) {
		r.Use(csrfProtection(deps))

		r.Get("/auth/status", handleAuthStatus(deps))
		r.Post("/auth/login", handleLogin(deps))
		r.Get("/kube/status", handleKubeStatus(deps))

		r.Group(func(r chi.Router) {
			r.Use(requireAuth(deps))

			r.Post("/auth/logout", handleLogout(deps))
			r.Get("/auth/me", handleGetCurrentUser(deps))

			r.Get("/audit-events", handleListAuditEvents(deps))

			r.Route("/notifications/webhook", func(r chi.Router) {
				r.Get("/", handleGetWebhookConfig(deps))
				r.With(requirePermission(deps, auth.PermConfigureProviders)).Patch("/", handleUpdateWebhookConfig(deps))
				r.With(requirePermission(deps, auth.PermConfigureProviders)).Post("/test", handleTestWebhook(deps))
			})

			r.Route("/projects", func(r chi.Router) {
				r.Post("/", handleCreateProject(deps))
				r.Get("/", handleListProjects(deps))
				r.Route("/{projectID}", func(r chi.Router) {
					r.Get("/", handleGetProject(deps))
					r.Patch("/", handleUpdateProject(deps))
					r.With(requirePermission(deps, auth.PermConfigureEnvironments)).Patch("/github-ci", handleUpdateGitHubCI(deps))
					r.Delete("/", handleDeleteProject(deps))
					r.Post("/discover", handleDiscoverProject(deps))
					r.Get("/discovery", handleGetDiscovery(deps))
					r.Get("/environments", handleListEnvironments(deps))
					r.Get("/services", handleListServices(deps))
					r.Post("/kube-discover", handleDiscoverKube(deps))
					r.Get("/kube-resources", handleListKubeResources(deps))
					r.Get("/kube/events", handleGetKubeEvents(deps))
					r.Get("/kube/pods/{podName}/logs", handleGetKubePodLogs(deps))
					r.Get("/graph", handleGetGraph(deps))
					r.With(requirePermission(deps, auth.PermGenerateTests)).Post("/tests/plan", handleGenerateTestPlan(deps))
					r.Get("/tests", handleListTests(deps))
					r.Get("/runs", handleListProjectRuns(deps))
					r.Get("/fix-proposals", handleListProjectFixProposals(deps))
				})
			})

			r.With(requirePermission(deps, auth.PermConfigureEnvironments)).
				Patch("/environments/{environmentID}", handleUpdateEnvironment(deps))

			r.Route("/tests/{testID}", func(r chi.Router) {
				r.Patch("/", handleUpdateTest(deps))
				r.With(requirePermission(deps, auth.PermApproveTestPlans)).Post("/approve", handleApproveTest(deps))
				r.With(requirePermission(deps, auth.PermApproveTestPlans)).Post("/reject", handleRejectTest(deps))
				r.With(requirePermission(deps, auth.PermRunApprovedTests)).Post("/run", handleRunTest(deps))
			})

			r.Route("/runs/{runID}", func(r chi.Router) {
				r.Get("/", handleGetRun(deps))
				r.With(requirePermission(deps, auth.PermRunApprovedTests)).Post("/cancel", handleCancelRun(deps))
				r.Get("/artifacts", handleListRunArtifacts(deps))
			})

			r.Get("/artifacts/{artifactID}/content", handleGetArtifactContent(deps))

			r.Route("/users", func(r chi.Router) {
				r.With(requirePermission(deps, auth.PermManageUsers)).Get("/", handleListUsers(deps))
				r.With(requirePermission(deps, auth.PermManageUsers)).Post("/", handleCreateUser(deps))
			})

			r.Route("/providers", func(r chi.Router) {
				r.Get("/", handleListProviders(deps))
				r.With(requirePermission(deps, auth.PermConfigureProviders)).Post("/", handleCreateProvider(deps))
				r.Get("/routing", handleGetTaskRouting(deps))
				r.With(requirePermission(deps, auth.PermConfigureProviders)).Patch("/routing", handleUpdateTaskRouting(deps))
				r.Route("/{providerID}", func(r chi.Router) {
					r.With(requirePermission(deps, auth.PermConfigureProviders)).Patch("/", handlePatchProvider(deps))
					r.With(requirePermission(deps, auth.PermConfigureProviders)).Delete("/", handleDeleteProvider(deps))
					r.With(requirePermission(deps, auth.PermConfigureProviders)).Post("/test", handleTestProviderConnection(deps))
				})
			})

			r.Route("/bugs", func(r chi.Router) {
				r.Get("/", handleListBugs(deps))
				r.Route("/{bugID}", func(r chi.Router) {
					r.Get("/", handleGetBug(deps))
					r.Post("/resolve", handleResolveBug(deps))
					r.Post("/reopen", handleReopenBug(deps))
					r.Post("/notes", handleAddBugNote(deps))
					r.Get("/export/markdown", handleExportBugMarkdown(deps))
					r.Get("/export/json", handleExportBugJSON(deps))
					r.With(requirePermission(deps, auth.PermGenerateFixProposals)).Post("/fix-proposal", handleGenerateFixProposal(deps))
				})
			})

			r.Route("/fix-proposals/{fixProposalID}", func(r chi.Router) {
				r.Get("/", handleGetFixProposal(deps))
				r.Patch("/", handleUpdateFixProposalRegressionTests(deps))
				r.With(requirePermission(deps, auth.PermApproveRepositoryPatches)).Post("/approve", handleApproveFixProposal(deps))
				r.With(requirePermission(deps, auth.PermApproveRepositoryPatches)).Post("/reject", handleRejectFixProposal(deps))
				r.With(requirePermission(deps, auth.PermApproveRepositoryPatches)).Post("/request-revision", handleRequestFixProposalRevision(deps))
				r.With(requirePermission(deps, auth.PermApplyWorkspace)).Post("/apply-workspace", handleApplyFixToWorkspace(deps))
				r.With(requirePermission(deps, auth.PermApproveRepositoryPatches)).Post("/apply-repository", handleApplyFixToRepository(deps))
			})
		})
	})

	return r
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	// Audit and health payloads are system-generated, but set a
	// conservative content-type-only policy so nothing rendered here can
	// be interpreted as HTML by a browser (spec §23.5).
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func handleHealth(w http.ResponseWriter, r *http.Request) {
	// Liveness only: no dependency checks, so an overloaded database
	// cannot make the process look unhealthy to a supervisor.
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func handleReady(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		checks := map[string]string{}
		ready := true

		if err := deps.Postgres.Ping(r.Context()); err != nil {
			checks["postgres"] = "unreachable"
			ready = false
		} else {
			checks["postgres"] = "ok"
		}

		if err := deps.Redis.Ping(r.Context()); err != nil {
			checks["redis"] = "unreachable"
			ready = false
		} else {
			checks["redis"] = "ok"
		}

		status := http.StatusOK
		if !ready {
			status = http.StatusServiceUnavailable
		}

		writeJSON(w, status, map[string]any{
			"ready":               ready,
			"checks":              checks,
			"test_execution":      runnerName(deps.Runner),
			"websocket_execution": runnerName(deps.WebSocketRunner),
		})
	}
}

// handleVersion reports this deployment's own version and, if
// SENTINEL_UPDATE_CHECK_ENABLED (default true) hasn't been turned off,
// the latest version published on GitHub Releases and whether it's
// newer — read by the panel's Dashboard (a small "update available"
// banner) and scripts/onboard.sh (a printed notice). update_available
// only ever means "a newer released version exists to look at"; it is
// never acted on automatically.
func handleVersion(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		info := deps.UpdateCheck.Get()
		writeJSON(w, http.StatusOK, map[string]any{
			"current_version":      deps.Version,
			"latest_version":       info.LatestVersion,
			"update_available":     info.UpdateAvailable,
			"release_url":          info.ReleaseURL,
			"checked_at":           info.CheckedAt,
			"check_error":          info.CheckError,
			"update_check_enabled": deps.UpdateCheckEnabled,
		})
	}
}

// runnerName reports which concrete runner (if any) backs a Runner
// field — "unconfigured" for nil, otherwise its Name() (e.g.
// "playwright-docker", "playwright-local") — so a caller (the
// Dashboard's System status card) can show which execution mode is
// actually active without needing its own separate config surface.
func runnerName(r runs.Runner) string {
	if r == nil {
		return "unconfigured"
	}
	return r.Name()
}

// handleListAuditEvents supports spec §9 "Audit search": action_type,
// resource_type, resource_id, actor, since/until (RFC 3339), and limit
// are all optional query filters. There is deliberately no way to
// modify or delete an event through this or any other route — the only
// verb this package's Recorder interface exposes besides Record is
// reading (spec §2.7 "Audit Everything" implies append-only, never
// "audit sometimes").
func handleListAuditEvents(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		filter := audit.SearchFilter{
			ActionType:   q.Get("action_type"),
			ResourceType: q.Get("resource_type"),
			ResourceID:   q.Get("resource_id"),
			Actor:        q.Get("actor"),
		}
		if raw := q.Get("limit"); raw != "" {
			if parsed, err := strconv.Atoi(raw); err == nil {
				filter.Limit = parsed
			}
		}
		if raw := q.Get("since"); raw != "" {
			since, err := time.Parse(time.RFC3339, raw)
			if err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_since", "detail": "must be RFC 3339"})
				return
			}
			filter.Since = since
		}
		if raw := q.Get("until"); raw != "" {
			until, err := time.Parse(time.RFC3339, raw)
			if err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_until", "detail": "must be RFC 3339"})
				return
			}
			filter.Until = until
		}

		events, err := deps.Audit.Search(r.Context(), filter)
		if err != nil {
			deps.Logger.Error().Err(err).Msg("searching audit events failed")
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal_error"})
			return
		}

		writeJSON(w, http.StatusOK, map[string]any{"events": events})
	}
}

// metricsMiddleware counts every request by method and final status code
// (spec §22's implied baseline metric — every other counter/gauge in
// internal/metrics is instrumented at its own specific call site).
func metricsMiddleware(deps Dependencies) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
			next.ServeHTTP(ww, r)
			deps.Metrics.HTTPRequestsTotal.Inc(map[string]string{
				"method": r.Method, "status": strconv.Itoa(ww.Status()),
			})
		})
	}
}

// handleMetrics serves the current metrics snapshot in Prometheus text
// exposition format.
func handleMetrics(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(deps.Metrics.Registry.Render()))
	}
}

func requestLogger(logger zerolog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
			next.ServeHTTP(ww, r)
			logger.Info().
				Str("method", r.Method).
				Str("path", r.URL.Path).
				Int("status", ww.Status()).
				Str("request_id", middleware.GetReqID(r.Context())).
				Msg("http_request")
		})
	}
}
