package providers

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// HealthResult is the outcome of a single connection test (spec §16.3
// "Test connection"). Message never contains the provider's API key.
type HealthResult struct {
	Status    string
	Message   string
	LatencyMS int64
}

// HealthChecker performs a lightweight, provider-type-specific reachability
// check. It never sends any repository or test content — only a
// credential-bearing request to a "list models"-style endpoint, which is
// the smallest request that proves both connectivity and (where
// applicable) that the API key is accepted.
type HealthChecker struct {
	client *http.Client
}

// NewHealthChecker builds a HealthChecker. If client is nil, a default
// client is used with no timeout of its own — callers pass a
// context.WithTimeout instead, so the provider's own configured timeout
// (Provider.TimeoutSeconds) governs the request.
func NewHealthChecker(client *http.Client) *HealthChecker {
	if client == nil {
		client = &http.Client{}
	}
	return &HealthChecker{client: client}
}

// Check probes p, using apiKey (already resolved from secretstore by the
// caller) for providers that require authentication.
func (h *HealthChecker) Check(ctx context.Context, p Provider, apiKey string) HealthResult {
	timeout := time.Duration(p.TimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = DefaultTimeoutSeconds * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	req, err := buildHealthRequest(ctx, p, apiKey)
	if err != nil {
		return HealthResult{Status: HealthError, Message: err.Error()}
	}

	start := time.Now()
	resp, err := h.client.Do(req)
	latency := time.Since(start)
	if err != nil {
		return HealthResult{Status: HealthError, Message: "request failed: " + sanitizeNetError(err), LatencyMS: latency.Milliseconds()}
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return HealthResult{Status: HealthError, Message: fmt.Sprintf("authentication rejected (HTTP %d)", resp.StatusCode), LatencyMS: latency.Milliseconds()}
	}
	if resp.StatusCode >= 300 {
		return HealthResult{Status: HealthError, Message: fmt.Sprintf("unexpected HTTP %d", resp.StatusCode), LatencyMS: latency.Milliseconds()}
	}
	return HealthResult{Status: HealthOK, Message: "reachable", LatencyMS: latency.Milliseconds()}
}

func buildHealthRequest(ctx context.Context, p Provider, apiKey string) (*http.Request, error) {
	if p.BaseURL == "" {
		return nil, fmt.Errorf("base_url is required")
	}
	base := strings.TrimSuffix(p.BaseURL, "/")

	var target, method string
	method = http.MethodGet

	switch p.Type {
	case TypeOllama:
		target = base + "/api/tags"
	case TypeOpenAI, TypeOpenAICompatible:
		target = base + "/models"
	case TypeAnthropic:
		target = base + "/v1/models"
	case TypeAzureOpenAI:
		target = base + "/openai/models?api-version=2024-02-01"
	case TypeGemini:
		u, err := url.Parse(base + "/v1beta/models")
		if err != nil {
			return nil, fmt.Errorf("invalid base_url")
		}
		q := u.Query()
		if apiKey != "" {
			q.Set("key", apiKey)
		}
		u.RawQuery = q.Encode()
		target = u.String()
	default:
		return nil, ErrInvalidType
	}

	req, err := http.NewRequestWithContext(ctx, method, target, nil)
	if err != nil {
		return nil, fmt.Errorf("invalid base_url")
	}

	switch p.Type {
	case TypeOpenAI, TypeOpenAICompatible, TypeAnthropic:
		if apiKey != "" {
			if p.Type == TypeAnthropic {
				req.Header.Set("x-api-key", apiKey)
				req.Header.Set("anthropic-version", "2023-06-01")
			} else {
				req.Header.Set("Authorization", "Bearer "+apiKey)
			}
		}
	case TypeAzureOpenAI:
		if apiKey != "" {
			req.Header.Set("api-key", apiKey)
		}
	}

	return req, nil
}

// sanitizeNetError strips the credential-bearing request URL that Go's
// net/http error wrapping sometimes includes, so a health-check message
// is always safe to log or return through the API.
func sanitizeNetError(err error) string {
	msg := err.Error()
	if idx := strings.Index(msg, `"`); idx != -1 {
		if end := strings.Index(msg[idx+1:], `"`); end != -1 {
			return msg[:idx] + "[url omitted]" + msg[idx+1+end+1:]
		}
	}
	return msg
}
