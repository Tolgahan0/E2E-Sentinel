package metrics

// AppMetrics holds the specific counters/gauges E2E Sentinel populates
// (a modest subset of spec §22's full list — only what has a real,
// already-centralized call site to increment from; see the package doc
// for what's deliberately not implemented).
type AppMetrics struct {
	Registry *Registry

	// HTTPRequestsTotal covers every request through the router.
	HTTPRequestsTotal *Counter
	// TestRunsTotal increments once per run reaching a terminal status
	// (spec §22 "Pass rate"/"Failure rate" are derivable from this by
	// status label).
	TestRunsTotal *Counter
	// ActiveTestRuns tracks in-flight runs (spec §22 "Active runners").
	ActiveTestRuns *Gauge
	// AIRequestsTotal covers every internal/providers.Completer call
	// (spec §22 "AI request duration"/"AI request errors" — duration
	// isn't tracked, since a histogram is a meaningfully bigger addition
	// than this hand-rolled registry's counters/gauges; outcome is).
	AIRequestsTotal *Counter
}

// NewAppMetrics registers every metric E2E Sentinel populates onto r.
func NewAppMetrics(r *Registry) *AppMetrics {
	return &AppMetrics{
		Registry:          r,
		HTTPRequestsTotal: r.NewCounter("e2e_sentinel_http_requests_total", "Total HTTP requests by method and status code"),
		TestRunsTotal:     r.NewCounter("e2e_sentinel_test_runs_total", "Total test runs reaching a terminal status, by status"),
		ActiveTestRuns:    r.NewGauge("e2e_sentinel_active_test_runs", "Test runs currently executing"),
		AIRequestsTotal:   r.NewCounter("e2e_sentinel_ai_requests_total", "Total AI provider completion requests, by provider type and outcome"),
	}
}
