package metrics

import (
	"strings"
	"testing"
)

func contains(haystack, needle string) bool {
	return strings.Contains(haystack, needle)
}

func TestCounter_IncAndRender(t *testing.T) {
	r := NewRegistry()
	c := r.NewCounter("test_requests_total", "test help text")
	c.Inc(map[string]string{"method": "GET", "status": "200"})
	c.Inc(map[string]string{"method": "GET", "status": "200"})
	c.Inc(map[string]string{"method": "POST", "status": "201"})

	out := r.Render()
	if !contains(out, `# HELP test_requests_total test help text`) {
		t.Errorf("missing HELP line:\n%s", out)
	}
	if !contains(out, `# TYPE test_requests_total counter`) {
		t.Errorf("missing TYPE line:\n%s", out)
	}
	if !contains(out, `test_requests_total{method="GET",status="200"} 2`) {
		t.Errorf("missing incremented series:\n%s", out)
	}
	if !contains(out, `test_requests_total{method="POST",status="201"} 1`) {
		t.Errorf("missing second series:\n%s", out)
	}
}

func TestCounter_NoLabelsRendersBareName(t *testing.T) {
	r := NewRegistry()
	c := r.NewCounter("test_total", "help")
	c.Inc(nil)
	out := r.Render()
	if !contains(out, "test_total 1") {
		t.Errorf("expected a bare (unlabeled) series:\n%s", out)
	}
}

func TestGauge_IncDecSet(t *testing.T) {
	r := NewRegistry()
	g := r.NewGauge("test_active", "help")
	g.Inc(nil)
	g.Inc(nil)
	g.Dec(nil)
	out := r.Render()
	if !contains(out, "test_active 1") {
		t.Errorf("expected gauge value 1 after Inc,Inc,Dec:\n%s", out)
	}

	g.Set(42, nil)
	out = r.Render()
	if !contains(out, "test_active 42") {
		t.Errorf("expected gauge value 42 after Set:\n%s", out)
	}
}

func TestLabelKey_OrderIndependent(t *testing.T) {
	r := NewRegistry()
	c := r.NewCounter("test_total", "help")
	c.Inc(map[string]string{"a": "1", "b": "2"})
	c.Inc(map[string]string{"b": "2", "a": "1"})
	out := r.Render()
	if !contains(out, `test_total{a="1",b="2"} 2`) {
		t.Errorf("expected label order to not create separate series:\n%s", out)
	}
}

func TestAppMetrics_RegistersExpectedSeries(t *testing.T) {
	app := NewAppMetrics(NewRegistry())
	app.HTTPRequestsTotal.Inc(map[string]string{"method": "GET", "status": "200"})
	app.TestRunsTotal.Inc(map[string]string{"status": "passed"})
	app.ActiveTestRuns.Inc(nil)
	app.AIRequestsTotal.Inc(map[string]string{"provider_type": "openai", "outcome": "ok"})

	out := app.Registry.Render()
	for _, want := range []string{
		"e2e_sentinel_http_requests_total",
		"e2e_sentinel_test_runs_total",
		"e2e_sentinel_active_test_runs",
		"e2e_sentinel_ai_requests_total",
	} {
		if !contains(out, want) {
			t.Errorf("missing metric %q in:\n%s", want, out)
		}
	}
}
