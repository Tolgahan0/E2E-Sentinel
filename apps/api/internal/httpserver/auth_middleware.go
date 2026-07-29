package httpserver

import (
	"context"
	"net/http"
	"strings"

	"e2e-sentinel/apps/api/internal/auth"
)

type authContextKey string

const (
	ctxKeyUser    authContextKey = "auth_user"
	ctxKeySession authContextKey = "auth_session"
)

// userFromContext returns the authenticated user, if any. Always empty
// when Dependencies.AuthEnabled is false.
func userFromContext(ctx context.Context) (auth.User, bool) {
	u, ok := ctx.Value(ctxKeyUser).(auth.User)
	return u, ok
}

func sessionFromContext(ctx context.Context) (auth.Session, bool) {
	s, ok := ctx.Value(ctxKeySession).(auth.Session)
	return s, ok
}

// requireAuth extracts and validates a bearer token when auth is
// enabled. When Dependencies.AuthEnabled is false (the default — spec
// §19 "MVP local mode may support a bootstrap administrator", not
// mandates one), this is a complete no-op: no token is required, no
// user is attached, and requirePermission below allows everything
// through unchanged. This keeps every Phase 0-8 deployment and test
// behaving exactly as before unless auth is explicitly turned on.
func requireAuth(deps Dependencies) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !deps.AuthEnabled {
				next.ServeHTTP(w, r)
				return
			}

			header := r.Header.Get("Authorization")
			token, ok := strings.CutPrefix(header, "Bearer ")
			if !ok || token == "" {
				writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "authentication_required"})
				return
			}

			sess, err := deps.Auth.GetSessionByTokenHash(r.Context(), auth.HashToken(token))
			if err != nil {
				writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid_or_expired_session"})
				return
			}
			user, err := deps.Auth.GetUserByID(r.Context(), sess.UserID)
			if err != nil {
				writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid_or_expired_session"})
				return
			}

			ctx := context.WithValue(r.Context(), ctxKeyUser, user)
			ctx = context.WithValue(ctx, ctxKeySession, sess)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// requirePermission gates a route on the authenticated user's role
// granting permission (spec §19's role/permission table). A no-op when
// auth is disabled. requireAuth must run first in the middleware chain
// — if it didn't (a wiring bug, not a runtime possibility a caller can
// trigger), this fails closed (401), never open.
func requirePermission(deps Dependencies, permission string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !deps.AuthEnabled {
				next.ServeHTTP(w, r)
				return
			}

			user, ok := userFromContext(r.Context())
			if !ok {
				writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "authentication_required"})
				return
			}
			if !auth.HasPermission(user.Role, permission) {
				writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden", "detail": "your role does not grant this permission"})
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
