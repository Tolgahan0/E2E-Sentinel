package httpserver

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"e2e-sentinel/apps/api/internal/audit"
	"e2e-sentinel/apps/api/internal/providers"
)

// providerResponse never includes an API key or the raw
// secret_reference_id — only whether one is configured (spec §16.3
// "Keys never return through the API").
type providerResponse struct {
	ID             string   `json:"id"`
	Type           string   `json:"type"`
	Name           string   `json:"name"`
	BaseURL        string   `json:"base_url"`
	Model          string   `json:"model"`
	HasAPIKey      bool     `json:"has_api_key"`
	IsLocal        bool     `json:"is_local"`
	Enabled        bool     `json:"enabled"`
	Capabilities   []string `json:"capabilities"`
	TimeoutSeconds int      `json:"timeout_seconds"`
	MaxTokens      int      `json:"max_tokens"`
	Temperature    float64  `json:"temperature"`
	HealthStatus   string   `json:"health_status"`
	LastCheckedAt  *string  `json:"last_checked_at"`
}

func toProviderResponse(p providers.Provider) providerResponse {
	var lastChecked *string
	if !p.LastCheckedAt.IsZero() {
		s := p.LastCheckedAt.Format(timeFormat)
		lastChecked = &s
	}
	capabilities := p.Capabilities
	if capabilities == nil {
		capabilities = []string{}
	}
	return providerResponse{
		ID: p.ID, Type: p.Type, Name: p.Name, BaseURL: p.BaseURL, Model: p.Model,
		HasAPIKey: p.SecretReferenceID != "", IsLocal: p.IsLocal, Enabled: p.Enabled,
		Capabilities: capabilities, TimeoutSeconds: p.TimeoutSeconds, MaxTokens: p.MaxTokens,
		Temperature: p.Temperature, HealthStatus: p.HealthStatus, LastCheckedAt: lastChecked,
	}
}

func handleListProviders(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		list, err := deps.Providers.List(r.Context())
		if err != nil {
			deps.Logger.Error().Err(err).Msg("listing providers failed")
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal_error"})
			return
		}
		out := make([]providerResponse, 0, len(list))
		for _, p := range list {
			out = append(out, toProviderResponse(p))
		}
		writeJSON(w, http.StatusOK, map[string]any{"providers": out})
	}
}

type createProviderRequest struct {
	Type           string   `json:"type"`
	Name           string   `json:"name"`
	BaseURL        string   `json:"base_url"`
	Model          string   `json:"model"`
	APIKey         string   `json:"api_key"`
	IsLocal        bool     `json:"is_local"`
	Enabled        *bool    `json:"enabled"`
	Capabilities   []string `json:"capabilities"`
	TimeoutSeconds int      `json:"timeout_seconds"`
	MaxTokens      int      `json:"max_tokens"`
	Temperature    float64  `json:"temperature"`
}

func handleCreateProvider(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var body createProviderRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_json"})
			return
		}

		if !providers.ValidType(body.Type) {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_type"})
			return
		}
		if body.Name == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name_required"})
			return
		}

		var secretReferenceID string
		if body.APIKey != "" {
			if deps.Secrets == nil {
				writeJSON(w, http.StatusServiceUnavailable, map[string]string{
					"error":  "secret_encryption_not_configured",
					"detail": "SENTINEL_SECRET_ENCRYPTION_KEY must be set before a provider can store an API key",
				})
				return
			}
			id, err := deps.Secrets.Create(r.Context(), body.APIKey)
			if err != nil {
				deps.Logger.Error().Err(err).Msg("encrypting provider API key failed")
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal_error"})
				return
			}
			secretReferenceID = id
		}

		enabled := true
		if body.Enabled != nil {
			enabled = *body.Enabled
		}

		p, err := deps.Providers.Create(r.Context(), providers.Provider{
			Type: body.Type, Name: body.Name, BaseURL: body.BaseURL, Model: body.Model,
			SecretReferenceID: secretReferenceID, IsLocal: body.IsLocal, Enabled: enabled,
			Capabilities: body.Capabilities, TimeoutSeconds: body.TimeoutSeconds,
			MaxTokens: body.MaxTokens, Temperature: body.Temperature,
		})
		if err != nil {
			deps.Logger.Error().Err(err).Msg("creating provider failed")
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal_error"})
			return
		}

		if err := deps.Audit.Record(r.Context(), audit.Event{
			ActionType: "provider.created", ResourceType: "ai_provider", ResourceID: p.ID,
			Actor: "user", Metadata: map[string]any{"type": p.Type, "name": p.Name, "has_api_key": secretReferenceID != ""},
		}); err != nil {
			deps.Logger.Error().Err(err).Msg("recording provider.created audit event failed")
		}

		writeJSON(w, http.StatusCreated, toProviderResponse(p))
	}
}

type patchProviderRequest struct {
	Name           *string   `json:"name"`
	BaseURL        *string   `json:"base_url"`
	Model          *string   `json:"model"`
	APIKey         *string   `json:"api_key"`
	ClearAPIKey    bool      `json:"clear_api_key"`
	Enabled        *bool     `json:"enabled"`
	Capabilities   *[]string `json:"capabilities"`
	TimeoutSeconds *int      `json:"timeout_seconds"`
	MaxTokens      *int      `json:"max_tokens"`
	Temperature    *float64  `json:"temperature"`
}

func handlePatchProvider(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		providerID := chi.URLParam(r, "providerID")

		var body patchProviderRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_json"})
			return
		}

		patch := providers.Patch{
			Name: body.Name, BaseURL: body.BaseURL, Model: body.Model, Enabled: body.Enabled,
			Capabilities: body.Capabilities, TimeoutSeconds: body.TimeoutSeconds,
			MaxTokens: body.MaxTokens, Temperature: body.Temperature,
		}

		if body.ClearAPIKey {
			patch.ClearSecretReference = true
		} else if body.APIKey != nil && *body.APIKey != "" {
			if deps.Secrets == nil {
				writeJSON(w, http.StatusServiceUnavailable, map[string]string{
					"error":  "secret_encryption_not_configured",
					"detail": "SENTINEL_SECRET_ENCRYPTION_KEY must be set before a provider can store an API key",
				})
				return
			}
			id, err := deps.Secrets.Create(r.Context(), *body.APIKey)
			if err != nil {
				deps.Logger.Error().Err(err).Msg("encrypting provider API key failed")
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal_error"})
				return
			}
			patch.SecretReferenceID = &id
		}

		p, err := deps.Providers.Update(r.Context(), providerID, patch)
		if errors.Is(err, providers.ErrNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "provider_not_found"})
			return
		}
		if errors.Is(err, providers.ErrNameRequired) {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name_required"})
			return
		}
		if err != nil {
			deps.Logger.Error().Err(err).Msg("updating provider failed")
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal_error"})
			return
		}

		if err := deps.Audit.Record(r.Context(), audit.Event{
			ActionType: "provider.updated", ResourceType: "ai_provider", ResourceID: p.ID, Actor: "user",
		}); err != nil {
			deps.Logger.Error().Err(err).Msg("recording provider.updated audit event failed")
		}

		writeJSON(w, http.StatusOK, toProviderResponse(p))
	}
}

// handleTestProviderConnection performs a live reachability check (spec
// §16.3 "Test connection"). It resolves the stored API key server-side
// only to attach it to the outbound health-check request — the key is
// never included in the HTTP response.
func handleTestProviderConnection(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		providerID := chi.URLParam(r, "providerID")

		p, err := deps.Providers.Get(r.Context(), providerID)
		if errors.Is(err, providers.ErrNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "provider_not_found"})
			return
		}
		if err != nil {
			deps.Logger.Error().Err(err).Msg("getting provider for test failed")
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal_error"})
			return
		}

		var apiKey string
		if p.SecretReferenceID != "" {
			if deps.Secrets == nil {
				writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "secret_encryption_not_configured"})
				return
			}
			apiKey, err = deps.Secrets.Resolve(r.Context(), p.SecretReferenceID)
			if err != nil {
				deps.Logger.Error().Err(err).Msg("resolving provider API key failed")
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal_error"})
				return
			}
		}

		result := deps.ProviderHealth.Check(r.Context(), p, apiKey)

		updated, err := deps.Providers.UpdateHealth(r.Context(), providerID, result.Status, time.Now())
		if err != nil {
			deps.Logger.Error().Err(err).Msg("recording provider health status failed")
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal_error"})
			return
		}

		if err := deps.Audit.Record(r.Context(), audit.Event{
			ActionType: "provider.tested", ResourceType: "ai_provider", ResourceID: providerID,
			Actor: "user", Metadata: map[string]any{"status": result.Status},
		}); err != nil {
			deps.Logger.Error().Err(err).Msg("recording provider.tested audit event failed")
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"provider":   toProviderResponse(updated),
			"status":     result.Status,
			"message":    result.Message,
			"latency_ms": result.LatencyMS,
		})
	}
}

// handleGetTaskRouting returns the current task-type -> provider-ID
// routing map (spec §16.4). Unset tasks are omitted, not defaulted to
// any provider — an unrouted task simply has no AI assistance available.
func handleGetTaskRouting(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		routes, err := loadTaskRouting(r.Context(), deps)
		if err != nil {
			deps.Logger.Error().Err(err).Msg("loading task routing failed")
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal_error"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"routes": routes})
	}
}

// handleUpdateTaskRouting merges the given routes into the stored map. An
// empty provider_id value clears that task's route.
func handleUpdateTaskRouting(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Routes map[string]string `json:"routes"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_json"})
			return
		}

		for task, providerID := range body.Routes {
			if !providers.ValidTask(task) {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_task_type", "detail": task})
				return
			}
			if providerID != "" {
				if _, err := deps.Providers.Get(r.Context(), providerID); errors.Is(err, providers.ErrNotFound) {
					writeJSON(w, http.StatusBadRequest, map[string]string{"error": "provider_not_found", "detail": providerID})
					return
				} else if err != nil {
					deps.Logger.Error().Err(err).Msg("looking up provider for routing failed")
					writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal_error"})
					return
				}
			}
		}

		routes, err := loadTaskRouting(r.Context(), deps)
		if err != nil {
			deps.Logger.Error().Err(err).Msg("loading task routing failed")
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal_error"})
			return
		}
		for task, providerID := range body.Routes {
			if providerID == "" {
				delete(routes, task)
			} else {
				routes[task] = providerID
			}
		}

		encoded, err := json.Marshal(routes)
		if err != nil {
			deps.Logger.Error().Err(err).Msg("encoding task routing failed")
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal_error"})
			return
		}
		if err := deps.Settings.Set(r.Context(), providers.RoutingSettingsKey, encoded); err != nil {
			deps.Logger.Error().Err(err).Msg("saving task routing failed")
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal_error"})
			return
		}

		if err := deps.Audit.Record(r.Context(), audit.Event{
			ActionType: "provider.routing_updated", ResourceType: "settings", ResourceID: providers.RoutingSettingsKey,
			Actor: "user",
		}); err != nil {
			deps.Logger.Error().Err(err).Msg("recording provider.routing_updated audit event failed")
		}

		writeJSON(w, http.StatusOK, map[string]any{"routes": routes})
	}
}

func loadTaskRouting(ctx context.Context, deps Dependencies) (map[string]string, error) {
	value, ok, err := deps.Settings.Get(ctx, providers.RoutingSettingsKey)
	if err != nil {
		return nil, err
	}
	routes := map[string]string{}
	if !ok {
		return routes, nil
	}
	if err := json.Unmarshal(value, &routes); err != nil {
		return nil, err
	}
	return routes, nil
}
