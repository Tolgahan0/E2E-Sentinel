package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

// CompletionRequest is a single-turn chat request — no history, no
// streaming. Used exclusively by Phase 8 fix generation, which asks a
// provider for a unified diff and nothing else.
type CompletionRequest struct {
	SystemPrompt string
	UserPrompt   string
	MaxTokens    int
	Temperature  float64
}

// CompletionResult is the raw text a provider returned.
type CompletionResult struct {
	Text string
}

// ErrNoDiffFound is returned by ExtractUnifiedDiff when the model's
// response contains no fenced diff/patch block — the caller must never
// fabricate one.
var ErrNoDiffFound = fmt.Errorf("providers: no unified diff found in completion response")

// Completer sends a single-turn completion request to a configured
// provider. Every provider type gets its own request/response shape,
// mirroring HealthChecker's per-type switch.
type Completer struct {
	client *http.Client
}

// NewCompleter builds a Completer. If client is nil, a default client is
// used; callers pass a context.WithTimeout to bound the request instead.
func NewCompleter(client *http.Client) *Completer {
	if client == nil {
		client = &http.Client{}
	}
	return &Completer{client: client}
}

// Complete sends req to p, using apiKey (already resolved from
// secretstore by the caller) for providers that require authentication.
func (c *Completer) Complete(ctx context.Context, p Provider, apiKey string, req CompletionRequest) (CompletionResult, error) {
	timeout := time.Duration(p.TimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = DefaultTimeoutSeconds * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	httpReq, parseResponse, err := buildCompletionRequest(ctx, p, apiKey, req)
	if err != nil {
		return CompletionResult{}, err
	}

	resp, err := c.client.Do(httpReq)
	if err != nil {
		return CompletionResult{}, fmt.Errorf("providers: completion request failed: %s", sanitizeNetError(err))
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return CompletionResult{}, fmt.Errorf("providers: reading completion response: %w", err)
	}
	if resp.StatusCode >= 300 {
		return CompletionResult{}, fmt.Errorf("providers: completion request returned HTTP %d", resp.StatusCode)
	}

	text, err := parseResponse(body)
	if err != nil {
		return CompletionResult{}, err
	}
	return CompletionResult{Text: text}, nil
}

type responseParser func(body []byte) (string, error)

func buildCompletionRequest(ctx context.Context, p Provider, apiKey string, req CompletionRequest) (*http.Request, responseParser, error) {
	if p.BaseURL == "" {
		return nil, nil, fmt.Errorf("base_url is required")
	}
	base := strings.TrimSuffix(p.BaseURL, "/")
	maxTokens := req.MaxTokens
	if maxTokens <= 0 {
		maxTokens = 2048
	}

	switch p.Type {
	case TypeOllama:
		payload, _ := json.Marshal(map[string]any{
			"model": p.Model, "stream": false,
			"messages": []map[string]string{
				{"role": "system", "content": req.SystemPrompt},
				{"role": "user", "content": req.UserPrompt},
			},
		})
		httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/api/chat", bytes.NewReader(payload))
		if err != nil {
			return nil, nil, fmt.Errorf("invalid base_url")
		}
		httpReq.Header.Set("Content-Type", "application/json")
		return httpReq, parseOllamaChat, nil

	case TypeOpenAI, TypeOpenAICompatible, TypeAzureOpenAI:
		payload, _ := json.Marshal(map[string]any{
			"model": p.Model, "max_tokens": maxTokens, "temperature": req.Temperature,
			"messages": []map[string]string{
				{"role": "system", "content": req.SystemPrompt},
				{"role": "user", "content": req.UserPrompt},
			},
		})
		target := base + "/chat/completions"
		if p.Type == TypeAzureOpenAI {
			target = base + "/openai/deployments/" + url.PathEscape(p.Model) + "/chat/completions?api-version=2024-02-01"
		}
		httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, target, bytes.NewReader(payload))
		if err != nil {
			return nil, nil, fmt.Errorf("invalid base_url")
		}
		httpReq.Header.Set("Content-Type", "application/json")
		if apiKey != "" {
			if p.Type == TypeAzureOpenAI {
				httpReq.Header.Set("api-key", apiKey)
			} else {
				httpReq.Header.Set("Authorization", "Bearer "+apiKey)
			}
		}
		return httpReq, parseOpenAIChat, nil

	case TypeAnthropic:
		payload, _ := json.Marshal(map[string]any{
			"model": p.Model, "max_tokens": maxTokens, "system": req.SystemPrompt,
			"messages": []map[string]string{{"role": "user", "content": req.UserPrompt}},
		})
		httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/v1/messages", bytes.NewReader(payload))
		if err != nil {
			return nil, nil, fmt.Errorf("invalid base_url")
		}
		httpReq.Header.Set("Content-Type", "application/json")
		if apiKey != "" {
			httpReq.Header.Set("x-api-key", apiKey)
		}
		httpReq.Header.Set("anthropic-version", "2023-06-01")
		return httpReq, parseAnthropicMessages, nil

	case TypeGemini:
		payload, _ := json.Marshal(map[string]any{
			"systemInstruction": map[string]any{"parts": []map[string]string{{"text": req.SystemPrompt}}},
			"contents":          []map[string]any{{"parts": []map[string]string{{"text": req.UserPrompt}}}},
		})
		u, err := url.Parse(base + "/v1beta/models/" + url.PathEscape(p.Model) + ":generateContent")
		if err != nil {
			return nil, nil, fmt.Errorf("invalid base_url")
		}
		if apiKey != "" {
			q := u.Query()
			q.Set("key", apiKey)
			u.RawQuery = q.Encode()
		}
		httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), bytes.NewReader(payload))
		if err != nil {
			return nil, nil, fmt.Errorf("invalid base_url")
		}
		httpReq.Header.Set("Content-Type", "application/json")
		return httpReq, parseGeminiGenerateContent, nil

	default:
		return nil, nil, ErrInvalidType
	}
}

func parseOllamaChat(body []byte) (string, error) {
	var out struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return "", fmt.Errorf("providers: decoding ollama response: %w", err)
	}
	return out.Message.Content, nil
}

func parseOpenAIChat(body []byte) (string, error) {
	var out struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return "", fmt.Errorf("providers: decoding chat completion response: %w", err)
	}
	if len(out.Choices) == 0 {
		return "", fmt.Errorf("providers: chat completion response had no choices")
	}
	return out.Choices[0].Message.Content, nil
}

func parseAnthropicMessages(body []byte) (string, error) {
	var out struct {
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return "", fmt.Errorf("providers: decoding anthropic response: %w", err)
	}
	if len(out.Content) == 0 {
		return "", fmt.Errorf("providers: anthropic response had no content blocks")
	}
	return out.Content[0].Text, nil
}

func parseGeminiGenerateContent(body []byte) (string, error) {
	var out struct {
		Candidates []struct {
			Content struct {
				Parts []struct {
					Text string `json:"text"`
				} `json:"parts"`
			} `json:"content"`
		} `json:"candidates"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return "", fmt.Errorf("providers: decoding gemini response: %w", err)
	}
	if len(out.Candidates) == 0 || len(out.Candidates[0].Content.Parts) == 0 {
		return "", fmt.Errorf("providers: gemini response had no candidates")
	}
	return out.Candidates[0].Content.Parts[0].Text, nil
}

var fencedDiffRe = regexp.MustCompile("(?s)```(?:diff|patch)?\\s*\\n(---.*?)\\n```")

// ExtractUnifiedDiff pulls a unified diff out of a model's free-form
// response. It requires the diff to appear in a fenced code block
// starting with a "---" file header — anything else is too ambiguous to
// safely treat as a diff, so this returns ErrNoDiffFound rather than
// guessing.
func ExtractUnifiedDiff(text string) (string, error) {
	m := fencedDiffRe.FindStringSubmatch(text)
	if m == nil {
		return "", ErrNoDiffFound
	}
	return strings.TrimSpace(m[1]), nil
}
