// Package webhooks sends a best-effort, fire-and-forget HTTP POST when
// something happens that a team would want to know about without
// having to keep the panel open — a new bug report, or a fix proposal
// waiting on review.
//
// This is deliberately a v1 ceiling, not a job system: one configured
// URL, no retry queue, no delivery tracking, no request signature. A
// failed delivery is logged by the caller and otherwise has no effect
// — it never blocks or fails whatever triggered the notification (spec
// §21's full job system, with idempotency/retry/dead-letter, is
// separately reserved, larger infrastructure — see
// docs/OPERATIONS.md).
package webhooks

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// SettingsKey is where the configured webhook URL lives in the generic
// settings store (internal/settings) — the same mechanism
// ai.task_routing already uses, rather than a dedicated table for a
// single string.
const SettingsKey = "notifications.webhook_url"

// Event types. Kept as a small closed set (not a generic string) so a
// receiver can switch on Type without guessing what values exist.
const (
	EventBugReportCreated         = "bug_report.created"
	EventFixProposalPendingReview = "fix_proposal.pending_review"
)

// Event is the JSON body posted to the configured webhook URL.
type Event struct {
	Type         string    `json:"type"`
	ProjectID    string    `json:"project_id"`
	ResourceType string    `json:"resource_type"`
	ResourceID   string    `json:"resource_id"`
	Title        string    `json:"title"`
	Severity     string    `json:"severity,omitempty"`
	OccurredAt   time.Time `json:"occurred_at"`
}

// Sender posts an Event to a webhook URL. The zero value is usable.
type Sender struct {
	HTTPClient *http.Client
}

// NewSender builds a Sender with a short timeout — a notification must
// never hang whatever background work is trying to send it.
func NewSender() *Sender {
	return &Sender{HTTPClient: &http.Client{Timeout: 5 * time.Second}}
}

// Send POSTs event as JSON to url. An empty url is a deliberate no-op
// (not configured), never an error — callers don't need to check
// "is this configured" separately before calling Send.
func (s *Sender) Send(ctx context.Context, url string, event Event) error {
	if url == "" {
		return nil
	}

	client := s.HTTPClient
	if client == nil {
		client = NewSender().HTTPClient
	}

	body, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("webhooks: encoding event: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("webhooks: building request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	res, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("webhooks: delivering event: %w", err)
	}
	defer res.Body.Close()

	if res.StatusCode >= 300 {
		return fmt.Errorf("webhooks: endpoint returned status %d", res.StatusCode)
	}
	return nil
}
