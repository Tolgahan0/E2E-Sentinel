// Package httpserver exposes E2E Sentinel's Phase 0 HTTP API: liveness,
// readiness, and read-only audit event listing. It deliberately has no
// routes that mutate anything outside E2E Sentinel's own audit log.
package httpserver

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/rs/zerolog"

	"e2e-sentinel/apps/api/internal/artifacts"
	"e2e-sentinel/apps/api/internal/audit"
	"e2e-sentinel/apps/api/internal/discovery"
	"e2e-sentinel/apps/api/internal/environments"
	"e2e-sentinel/apps/api/internal/graph"
	"e2e-sentinel/apps/api/internal/planning"
	"e2e-sentinel/apps/api/internal/projects"
	"e2e-sentinel/apps/api/internal/runs"
	"e2e-sentinel/apps/api/internal/services"
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
	// Docker is optional: nil means "no Docker integration configured",
	// handled the same as an unreachable daemon (spec §25 Phase 2).
	Docker DockerLister
	// Runner is optional: nil means "test execution is not configured"
	// (SENTINEL_RUNNER_HOST_WORKSPACE_DIR unset) — every other feature
	// works fine without it; POST /tests/{id}/run returns 503.
	Runner runs.Runner
	Logger zerolog.Logger
}

// NewRouter builds the chi router for the API.
func NewRouter(deps Dependencies) http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(requestLogger(deps.Logger))
	r.Use(middleware.Recoverer)

	r.Get("/health", handleHealth)
	r.Get("/ready", handleReady(deps))

	r.Route("/api/v1", func(r chi.Router) {
		r.Get("/audit-events", handleListAuditEvents(deps))

		r.Route("/projects", func(r chi.Router) {
			r.Post("/", handleCreateProject(deps))
			r.Get("/", handleListProjects(deps))
			r.Route("/{projectID}", func(r chi.Router) {
				r.Get("/", handleGetProject(deps))
				r.Patch("/", handleUpdateProject(deps))
				r.Post("/discover", handleDiscoverProject(deps))
				r.Get("/discovery", handleGetDiscovery(deps))
				r.Get("/environments", handleListEnvironments(deps))
				r.Get("/services", handleListServices(deps))
				r.Get("/graph", handleGetGraph(deps))
				r.Post("/tests/plan", handleGenerateTestPlan(deps))
				r.Get("/tests", handleListTests(deps))
				r.Get("/runs", handleListProjectRuns(deps))
			})
		})

		r.Patch("/environments/{environmentID}", handleUpdateEnvironment(deps))

		r.Route("/tests/{testID}", func(r chi.Router) {
			r.Patch("/", handleUpdateTest(deps))
			r.Post("/approve", handleApproveTest(deps))
			r.Post("/reject", handleRejectTest(deps))
			r.Post("/run", handleRunTest(deps))
		})

		r.Route("/runs/{runID}", func(r chi.Router) {
			r.Get("/", handleGetRun(deps))
			r.Post("/cancel", handleCancelRun(deps))
			r.Get("/artifacts", handleListRunArtifacts(deps))
		})

		r.Get("/artifacts/{artifactID}/content", handleGetArtifactContent(deps))
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
			"ready":  ready,
			"checks": checks,
		})
	}
}

func handleListAuditEvents(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		limit := 50
		if raw := r.URL.Query().Get("limit"); raw != "" {
			if parsed, err := strconv.Atoi(raw); err == nil {
				limit = parsed
			}
		}

		events, err := deps.Audit.Recent(r.Context(), limit)
		if err != nil {
			deps.Logger.Error().Err(err).Msg("listing audit events failed")
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal_error"})
			return
		}

		writeJSON(w, http.StatusOK, map[string]any{"events": events})
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
