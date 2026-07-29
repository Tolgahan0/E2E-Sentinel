package httpserver

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"e2e-sentinel/apps/api/internal/audit"
	"e2e-sentinel/apps/api/internal/auth"
)

// handleAuthStatus is always reachable, unauthenticated — the web panel
// uses it to decide whether to show a login gate at all.
func handleAuthStatus(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]bool{"auth_enabled": deps.AuthEnabled})
	}
}

type userResponse struct {
	ID    string `json:"id"`
	Email string `json:"email"`
	Role  string `json:"role"`
}

func toUserResponse(u auth.User) userResponse {
	return userResponse{ID: u.ID, Email: u.Email, Role: u.Role}
}

// handleLogin issues a bearer token on valid credentials (spec §19).
// The same generic "invalid_credentials" error is returned whether the
// email doesn't exist or the password is wrong, so a caller can never
// use this endpoint to enumerate registered emails.
func handleLogin(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !deps.AuthEnabled {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "auth_not_enabled"})
			return
		}

		var body struct {
			Email    string `json:"email"`
			Password string `json:"password"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_json"})
			return
		}

		user, err := deps.Auth.GetUserByEmail(r.Context(), body.Email)
		if errors.Is(err, auth.ErrNotFound) {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid_credentials"})
			return
		}
		if err != nil {
			deps.Logger.Error().Err(err).Msg("looking up user for login failed")
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal_error"})
			return
		}
		if err := auth.VerifyPassword(user.PasswordHash, body.Password); err != nil {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid_credentials"})
			return
		}

		token, hash, err := auth.GenerateToken()
		if err != nil {
			deps.Logger.Error().Err(err).Msg("generating session token failed")
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal_error"})
			return
		}
		expiresAt := time.Now().Add(auth.DefaultSessionTTL)
		if _, err := deps.Auth.CreateSession(r.Context(), auth.Session{UserID: user.ID, TokenHash: hash, ExpiresAt: expiresAt}); err != nil {
			deps.Logger.Error().Err(err).Msg("creating session failed")
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal_error"})
			return
		}

		if err := deps.Audit.Record(r.Context(), audit.Event{
			ActionType: "auth.login", ResourceType: "user", ResourceID: user.ID, Actor: user.Email,
		}); err != nil {
			deps.Logger.Error().Err(err).Msg("recording auth.login audit event failed")
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"token": token, "expires_at": expiresAt.Format(timeFormat), "user": toUserResponse(user),
		})
	}
}

// handleLogout revokes the presented session. Requires the requireAuth
// middleware to have already validated the token (there is no other way
// to reach this route when auth is enabled).
func handleLogout(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !deps.AuthEnabled {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "auth_not_enabled"})
			return
		}

		sess, ok := sessionFromContext(r.Context())
		if !ok {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "authentication_required"})
			return
		}
		if err := deps.Auth.DeleteSession(r.Context(), sess.ID); err != nil {
			deps.Logger.Error().Err(err).Msg("deleting session failed")
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal_error"})
			return
		}

		if user, ok := userFromContext(r.Context()); ok {
			if err := deps.Audit.Record(r.Context(), audit.Event{
				ActionType: "auth.logout", ResourceType: "user", ResourceID: user.ID, Actor: user.Email,
			}); err != nil {
				deps.Logger.Error().Err(err).Msg("recording auth.logout audit event failed")
			}
		}

		writeJSON(w, http.StatusOK, map[string]string{"status": "logged_out"})
	}
}

// handleGetCurrentUser lets the web panel show who's logged in.
func handleGetCurrentUser(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, ok := userFromContext(r.Context())
		if !ok {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "authentication_required"})
			return
		}
		writeJSON(w, http.StatusOK, toUserResponse(user))
	}
}
