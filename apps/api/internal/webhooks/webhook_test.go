package webhooks

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestSend_EmptyURLIsANoOp(t *testing.T) {
	sender := NewSender()
	if err := sender.Send(context.Background(), "", Event{Type: EventBugReportCreated}); err != nil {
		t.Fatalf("Send() error = %v, want nil for an unconfigured (empty) URL", err)
	}
}

func TestSend_PostsEventAsJSON(t *testing.T) {
	var received Event
	var gotContentType string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		gotContentType = r.Header.Get("Content-Type")
		json.NewDecoder(r.Body).Decode(&received)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	sender := NewSender()
	event := Event{
		Type: EventBugReportCreated, ProjectID: "p1", ResourceType: "bug_report", ResourceID: "b1",
		Title: "Something failed", Severity: "high", OccurredAt: time.Now(),
	}
	if err := sender.Send(context.Background(), server.URL, event); err != nil {
		t.Fatalf("Send() error: %v", err)
	}
	if gotContentType != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", gotContentType)
	}
	if received.Type != EventBugReportCreated || received.ResourceID != "b1" || received.Title != "Something failed" {
		t.Errorf("received event = %+v", received)
	}
}

func TestSend_NonSuccessStatusIsAnError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusInternalServerError)
	}))
	defer server.Close()

	sender := NewSender()
	if err := sender.Send(context.Background(), server.URL, Event{Type: EventBugReportCreated}); err == nil {
		t.Fatal("Send() error = nil, want an error for a 500 response")
	}
}

func TestSend_UnreachableURLIsAnError(t *testing.T) {
	sender := &Sender{HTTPClient: &http.Client{Timeout: 200 * time.Millisecond}}
	if err := sender.Send(context.Background(), "http://127.0.0.1:1", Event{Type: EventBugReportCreated}); err == nil {
		t.Fatal("Send() error = nil, want an error for an unreachable endpoint")
	}
}

func TestSend_ZeroValueSenderIsUsable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	var sender Sender // zero value, no NewSender()
	if err := sender.Send(context.Background(), server.URL, Event{Type: EventBugReportCreated}); err != nil {
		t.Fatalf("Send() error: %v, want the zero-value Sender to work via its own default client", err)
	}
}
