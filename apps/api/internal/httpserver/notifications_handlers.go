package httpserver

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"e2e-sentinel/apps/api/internal/audit"
	"e2e-sentinel/apps/api/internal/webhooks"
)

// handleGetWebhookConfig returns whether a notification webhook is
// configured, and its URL if so. There is no secret involved (unlike a
// provider API key) — the URL itself is the only thing to protect
// against SSRF-style misuse, which is the operator's own call to make,
// the same trust level as configuring an AI provider's base_url.
func handleGetWebhookConfig(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		url, err := loadWebhookURL(r.Context(), deps)
		if err != nil {
			deps.Logger.Error().Err(err).Msg("loading webhook config failed")
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal_error"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"url": url, "configured": url != ""})
	}
}

// handleUpdateWebhookConfig sets or clears (empty string) the
// notification webhook URL.
func handleUpdateWebhookConfig(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			URL string `json:"url"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_json"})
			return
		}

		encoded, err := json.Marshal(body.URL)
		if err != nil {
			deps.Logger.Error().Err(err).Msg("encoding webhook url failed")
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal_error"})
			return
		}
		if err := deps.Settings.Set(r.Context(), webhooks.SettingsKey, encoded); err != nil {
			deps.Logger.Error().Err(err).Msg("saving webhook config failed")
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal_error"})
			return
		}

		if err := deps.Audit.Record(r.Context(), audit.Event{
			ActionType: "notifications.webhook_updated", ResourceType: "settings", ResourceID: webhooks.SettingsKey,
			Actor: "user", Metadata: map[string]any{"configured": body.URL != ""},
		}); err != nil {
			deps.Logger.Error().Err(err).Msg("recording notifications.webhook_updated audit event failed")
		}

		writeJSON(w, http.StatusOK, map[string]any{"url": body.URL, "configured": body.URL != ""})
	}
}

// handleTestWebhook sends a synthetic event to the configured URL right
// now, so a user can confirm delivery actually works without waiting
// for a real bug/fix-proposal to trigger one.
func handleTestWebhook(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		url, err := loadWebhookURL(r.Context(), deps)
		if err != nil {
			deps.Logger.Error().Err(err).Msg("loading webhook config failed")
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal_error"})
			return
		}
		if url == "" {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": "webhook_not_configured"})
			return
		}

		err = deps.Webhooks.Send(r.Context(), url, webhooks.Event{
			Type: "test", Title: "Test notification from E2E Sentinel", OccurredAt: time.Now(),
		})
		if err != nil {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": "delivery_failed", "detail": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "sent"})
	}
}

func loadWebhookURL(ctx context.Context, deps Dependencies) (string, error) {
	value, ok, err := deps.Settings.Get(ctx, webhooks.SettingsKey)
	if err != nil {
		return "", err
	}
	if !ok {
		return "", nil
	}
	var url string
	if err := json.Unmarshal(value, &url); err != nil {
		return "", err
	}
	return url, nil
}

// notifyAsync sends event on a background context, in its own
// goroutine, so a webhook delivery (up to the Sender's timeout) never
// adds latency to the HTTP request or background job that triggered it.
// Delivery failures are logged only — a notification is never allowed
// to affect the outcome of whatever happened to trigger it, matching
// this codebase's existing "best-effort" failure-correlation pattern.
func notifyAsync(deps Dependencies, event webhooks.Event) {
	go func() {
		url, err := loadWebhookURL(context.Background(), deps)
		if err != nil {
			deps.Logger.Warn().Err(err).Msg("loading webhook config for notification failed")
			return
		}
		if url == "" {
			return
		}
		if err := deps.Webhooks.Send(context.Background(), url, event); err != nil {
			deps.Logger.Warn().Err(err).Str("event_type", event.Type).Msg("delivering webhook notification failed")
		}
	}()
}
