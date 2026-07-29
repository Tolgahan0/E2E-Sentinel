package httpserver

import (
	"encoding/json"
	"errors"
	"net/http"

	"e2e-sentinel/apps/api/internal/audit"
	"e2e-sentinel/apps/api/internal/auth"
)

// handleListUsers and handleCreateUser back the only way (besides the
// bootstrap administrator, see auth.EnsureBootstrapAdmin) to get
// additional accounts into the system — reachable only by an
// Administrator (auth.PermManageUsers), and only once auth is enabled.
func handleListUsers(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		users, err := deps.Auth.ListUsers(r.Context())
		if err != nil {
			deps.Logger.Error().Err(err).Msg("listing users failed")
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal_error"})
			return
		}
		out := make([]userResponse, 0, len(users))
		for _, u := range users {
			out = append(out, toUserResponse(u))
		}
		writeJSON(w, http.StatusOK, map[string]any{"users": out})
	}
}

type createUserRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
	Role     string `json:"role"`
}

// handleCreateUser never returns a password hash and never accepts one —
// the caller supplies a plaintext password that is hashed here, the same
// as EnsureBootstrapAdmin.
func handleCreateUser(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var body createUserRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_json"})
			return
		}
		if body.Email == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "email_required"})
			return
		}
		if !auth.ValidRole(body.Role) {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_role"})
			return
		}
		hash, err := auth.HashPassword(body.Password)
		if err != nil {
			if errors.Is(err, auth.ErrPasswordTooShort) {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "password_too_short"})
				return
			}
			deps.Logger.Error().Err(err).Msg("hashing password for new user failed")
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal_error"})
			return
		}

		user, err := deps.Auth.CreateUser(r.Context(), auth.User{Email: body.Email, PasswordHash: hash, Role: body.Role})
		if errors.Is(err, auth.ErrEmailTaken) {
			writeJSON(w, http.StatusConflict, map[string]string{"error": "email_taken"})
			return
		}
		if err != nil {
			deps.Logger.Error().Err(err).Msg("creating user failed")
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal_error"})
			return
		}

		if actor, ok := userFromContext(r.Context()); ok {
			if err := deps.Audit.Record(r.Context(), audit.Event{
				ActionType: "user.create", ResourceType: "user", ResourceID: user.ID, Actor: actor.Email,
			}); err != nil {
				deps.Logger.Error().Err(err).Msg("recording user.create audit event failed")
			}
		}

		writeJSON(w, http.StatusCreated, toUserResponse(user))
	}
}
