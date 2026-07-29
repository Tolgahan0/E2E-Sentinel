// Package metrics implements a minimal, hand-rolled Prometheus text
// exposition registry (spec §9/§22 "Metrics"). A full client library
// (prometheus/client_golang) was judged unnecessary dependency weight
// for the handful of counters/gauges this project actually has
// meaningful data for; this package is a few dozen lines instead of a
// new dependency tree, consistent with this project's general
// preference for minimal dependencies (e.g. a hand-rolled Docker Engine
// client instead of the full Docker SDK, since Phase 2).
//
// Full OpenTelemetry distributed tracing (spec §22 also asks for
// "Traces") is not implemented — wiring span propagation through every
// internal package is a much larger effort than this phase's remaining
// scope allows, and is documented as a deferred ceiling rather than
// half-built.
package metrics

import (
	"fmt"
	"sort"
	"strings"
	"sync"
)

// Counter is a monotonically-increasing value, optionally partitioned by
// labels.
type Counter struct {
	mu     sync.Mutex
	name   string
	help   string
	values map[string]float64
}

func newCounter(name, help string) *Counter {
	return &Counter{name: name, help: help, values: map[string]float64{}}
}

// Inc increments the series identified by labels by 1.
func (c *Counter) Inc(labels map[string]string) {
	c.Add(1, labels)
}

// Add increments the series identified by labels by delta.
func (c *Counter) Add(delta float64, labels map[string]string) {
	key := labelKey(labels)
	c.mu.Lock()
	defer c.mu.Unlock()
	c.values[key] += delta
}

func (c *Counter) render(sb *strings.Builder) {
	c.mu.Lock()
	defer c.mu.Unlock()
	renderFamily(sb, c.name, c.help, "counter", c.values)
}

// Gauge is a value that can go up or down.
type Gauge struct {
	mu     sync.Mutex
	name   string
	help   string
	values map[string]float64
}

func newGauge(name, help string) *Gauge {
	return &Gauge{name: name, help: help, values: map[string]float64{}}
}

func (g *Gauge) Inc(labels map[string]string) { g.Add(1, labels) }
func (g *Gauge) Dec(labels map[string]string) { g.Add(-1, labels) }

func (g *Gauge) Add(delta float64, labels map[string]string) {
	key := labelKey(labels)
	g.mu.Lock()
	defer g.mu.Unlock()
	g.values[key] += delta
}

func (g *Gauge) Set(value float64, labels map[string]string) {
	key := labelKey(labels)
	g.mu.Lock()
	defer g.mu.Unlock()
	g.values[key] = value
}

func (g *Gauge) render(sb *strings.Builder) {
	g.mu.Lock()
	defer g.mu.Unlock()
	renderFamily(sb, g.name, g.help, "gauge", g.values)
}

func renderFamily(sb *strings.Builder, name, help, typ string, values map[string]float64) {
	fmt.Fprintf(sb, "# HELP %s %s\n# TYPE %s %s\n", name, help, name, typ)
	keys := make([]string, 0, len(values))
	for k := range values {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if k == "" {
			fmt.Fprintf(sb, "%s %g\n", name, values[k])
		} else {
			fmt.Fprintf(sb, "%s{%s} %g\n", name, k, values[k])
		}
	}
}

// labelKey renders labels as a stable, sorted "k1=\"v1\",k2=\"v2\"" string
// so the same label set always maps to the same series regardless of
// map iteration order.
func labelKey(labels map[string]string) string {
	if len(labels) == 0 {
		return ""
	}
	keys := make([]string, 0, len(labels))
	for k := range labels {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(labels))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s=%q", k, labels[k]))
	}
	return strings.Join(parts, ",")
}

// Registry collects every Counter/Gauge created through it, in creation
// order, for Render.
type Registry struct {
	mu       sync.Mutex
	counters []*Counter
	gauges   []*Gauge
}

// NewRegistry builds an empty Registry. Each Dependencies gets its own
// (never a package-level global) so tests never accumulate state across
// unrelated router instances.
func NewRegistry() *Registry {
	return &Registry{}
}

func (r *Registry) NewCounter(name, help string) *Counter {
	c := newCounter(name, help)
	r.mu.Lock()
	r.counters = append(r.counters, c)
	r.mu.Unlock()
	return c
}

func (r *Registry) NewGauge(name, help string) *Gauge {
	g := newGauge(name, help)
	r.mu.Lock()
	r.gauges = append(r.gauges, g)
	r.mu.Unlock()
	return g
}

// Render returns every registered metric in Prometheus text exposition
// format.
func (r *Registry) Render() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	var sb strings.Builder
	for _, c := range r.counters {
		c.render(&sb)
	}
	for _, g := range r.gauges {
		g.render(&sb)
	}
	return sb.String()
}
