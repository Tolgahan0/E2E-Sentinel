// Package routes extracts a best-effort route inventory (spec §7.6):
// browser pages, API endpoints, health/auth/admin/webhook routes. Every
// route carries the source file (or OpenAPI document) that proved it and
// a confidence level — framework file-structure conventions and OpenAPI
// declarations are high confidence; regex-matched router calls in
// arbitrary source are medium confidence, since a regex can't fully
// understand the language it's scanning.
package routes

import "strings"

// Kind values for a Route.
const (
	KindPage      = "page"
	KindAPI       = "api"
	KindHealth    = "health"
	KindAuth      = "auth"
	KindAdmin     = "admin"
	KindWebhook   = "webhook"
	KindWebSocket = "websocket"
)

const (
	ConfidenceHigh   = "high"
	ConfidenceMedium = "medium"
)

// Route is one discovered route.
type Route struct {
	Method string // "" for a browser page route with no single HTTP method
	// Path is a relative URL path (e.g. "/api/users") for every Kind
	// except KindWebSocket, where it holds the full matched
	// "ws://"/"wss://" URL literal instead — a WebSocket client almost
	// always specifies a complete endpoint, not something meaningfully
	// joinable with an environment's HTTP base_url.
	Path       string
	Kind       string
	SourcePath string
	Confidence string
	Evidence   map[string]any
}

// ClassifyPathKind infers a route's kind from its path alone. Order
// matters: more specific categories are checked before the generic
// api/page fallback.
func ClassifyPathKind(path string, hasMethod bool) string {
	lower := strings.ToLower(path)
	for _, sub := range []struct {
		marker string
		kind   string
	}{
		{"/health", KindHealth}, {"/ready", KindHealth}, {"/healthz", KindHealth}, {"/livez", KindHealth},
		{"/webhook", KindWebhook}, {"/callback", KindWebhook},
		{"/admin", KindAdmin},
		{"/auth", KindAuth}, {"/login", KindAuth}, {"/logout", KindAuth}, {"/oauth", KindAuth}, {"/signup", KindAuth}, {"/register", KindAuth},
	} {
		if strings.Contains(lower, sub.marker) {
			return sub.kind
		}
	}
	if hasMethod || strings.Contains(lower, "/api") {
		return KindAPI
	}
	return KindPage
}
