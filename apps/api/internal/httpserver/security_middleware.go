package httpserver

import (
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// securityHeaders sets defensive headers on every response (spec §23.5,
// §9 Production Hardening "Security headers"). This is a JSON/binary
// API, never HTML — the strict CSP and frame-denial cost nothing
// functionally but close off the "what if a response ever gets rendered
// as HTML in a browser tab" class of risk entirely (spec §23.5 already
// requires nosniff + forced download for artifacts; this extends the
// same posture to every response).
func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'")
		// Ignored by browsers over plain HTTP; harmless to always set so
		// a TLS-terminating proxy in front of this doesn't need to add
		// it itself.
		h.Set("Strict-Transport-Security", "max-age=63072000; includeSubDomains")
		next.ServeHTTP(w, r)
	})
}

// csrfCustomHeader must be present on every mutating request once auth
// is enabled. Bearer-token authentication (this API never uses cookies)
// is already structurally immune to classic CSRF — a cross-site request
// can't attach a custom Authorization header without this server
// granting CORS permission, which it doesn't. This check is
// defense-in-depth for spec §9's "CSRF protection" deliverable, and
// specifically protects a direct-API-access deployment (spec's
// DEPLOYMENT.md notes sentinel-api's port can be exposed further) where
// some future change might introduce cookie-based sessions.
const csrfCustomHeader = "X-Sentinel-Csrf"

var csrfExemptMethods = map[string]bool{http.MethodGet: true, http.MethodHead: true, http.MethodOptions: true}

// csrfProtection is a no-op when auth is disabled: with no session
// concept active at all, there's no ambient credential for a forged
// request to ride along with, so there's nothing meaningful to protect
// (mirrors requireAuth/requirePermission's same opt-in reasoning).
func csrfProtection(deps Dependencies) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !deps.AuthEnabled || csrfExemptMethods[r.Method] {
				next.ServeHTTP(w, r)
				return
			}
			if r.Header.Get(csrfCustomHeader) == "" {
				writeJSON(w, http.StatusForbidden, map[string]string{
					"error": "csrf_check_failed", "detail": "mutating requests must include the " + csrfCustomHeader + " header",
				})
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// rateLimiter holds one token-bucket limiter per client IP. Constructed
// fresh by rateLimit() for each NewRouter call (never a package-level
// singleton), so it never accumulates state across unrelated router
// instances — notably, each test's own router starts with a clean
// limiter.
type rateLimiter struct {
	mu       sync.Mutex
	limiters map[string]*rateLimiterEntry
	rps      rate.Limit
	burst    int
}

type rateLimiterEntry struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

// staleEntryTTL bounds how long an idle client's limiter is kept before
// being swept, so long-running processes don't accumulate one entry per
// distinct IP forever.
const staleEntryTTL = 10 * time.Minute

func newRateLimiter(rps float64, burst int) *rateLimiter {
	return &rateLimiter{limiters: map[string]*rateLimiterEntry{}, rps: rate.Limit(rps), burst: burst}
}

func (rl *rateLimiter) allow(key string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	entry, ok := rl.limiters[key]
	if !ok {
		entry = &rateLimiterEntry{limiter: rate.NewLimiter(rl.rps, rl.burst)}
		rl.limiters[key] = entry
	}
	entry.lastSeen = now

	// Opportunistic sweep: cheap relative to the map already being
	// walked implicitly by normal traffic, and avoids a separate
	// background goroutine/ticker for what is, at MVP scale, a small map.
	for k, e := range rl.limiters {
		if now.Sub(e.lastSeen) > staleEntryTTL {
			delete(rl.limiters, k)
		}
	}

	return entry.limiter.Allow()
}

// DefaultRateLimitRPS and DefaultRateLimitBurst are generous enough that
// normal use (including a browser polling the Runs/Bugs pages) never
// trips them, while still bounding a runaway or abusive client.
const (
	DefaultRateLimitRPS   = 20.0
	DefaultRateLimitBurst = 60
)

// rateLimit applies a per-client-IP token bucket to every request. Rate
// limiting is unconditional (not gated on AuthEnabled) — protecting the
// server from abuse is independent of whether RBAC is turned on.
func rateLimit(rps float64, burst int) func(http.Handler) http.Handler {
	rl := newRateLimiter(rps, burst)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !rl.allow(clientIP(r)) {
				writeJSON(w, http.StatusTooManyRequests, map[string]string{"error": "rate_limited"})
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func clientIP(r *http.Request) string {
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return host
	}
	return strings.TrimSpace(r.RemoteAddr)
}
