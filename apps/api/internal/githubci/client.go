// Package githubci polls GitHub's REST API for new commits on each
// github-ci-configured project's default branch, runs its approved
// test cases, and reports one aggregate commit status back —
// deliberately outbound-only, the same shape as internal/updatecheck.
// sentinel-api is bound to 127.0.0.1 by default (docs/SECURITY_MODEL.md
// "least-exposure networking"); GitHub's webhook servers can't reach
// that, so rather than carve out a public inbound exception, this
// package has sentinel-api call GitHub instead of the other way
// around. See docs/GITHUB_CI.md.
package githubci

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// DefaultBaseURL is GitHub's REST API. Client.BaseURL overrides it —
// tests point it at an httptest.Server instead of the real network.
const DefaultBaseURL = "https://api.github.com"

// Commit status states GitHub's Commit Status API accepts.
const (
	StatusPending = "pending"
	StatusSuccess = "success"
	StatusFailure = "failure"
	StatusError   = "error"
)

// statusContext identifies this integration's status among any others
// (e.g. GitHub Actions' own checks) on the same commit.
const statusContext = "e2e-sentinel"

// Client talks to GitHub's REST API: read a branch's latest commit, and
// report a commit status back. Both calls take a token directly
// (already resolved from secretstore by the caller) rather than storing
// one, since different projects can use different tokens.
type Client struct {
	HTTPClient *http.Client
	// BaseURL overrides DefaultBaseURL — empty means use the default.
	BaseURL string
}

// NewClient builds a Client. If httpClient is nil, a default one with a
// 15s timeout is used.
func NewClient(httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 15 * time.Second}
	}
	return &Client{HTTPClient: httpClient}
}

func (c *Client) baseURL() string {
	if c.BaseURL != "" {
		return c.BaseURL
	}
	return DefaultBaseURL
}

// LatestCommit returns the current HEAD SHA of repo's branch. repo is
// "owner/name". token is a GitHub PAT with at least read access to
// repo — never included in a returned error, even if the underlying
// HTTP error happens to echo request details.
func (c *Client) LatestCommit(ctx context.Context, repo, branch, token string) (string, error) {
	target := fmt.Sprintf("%s/repos/%s/commits/%s", c.baseURL(), repo, branch)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return "", fmt.Errorf("githubci: building request: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Authorization", "token "+token)

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("githubci: GitHub API unreachable: %s", scrubToken(err.Error(), token))
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("githubci: GitHub API returned %d fetching the latest commit", resp.StatusCode)
	}

	var body struct {
		SHA string `json:"sha"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&body); err != nil {
		return "", fmt.Errorf("githubci: parsing commit response: %w", err)
	}
	if body.SHA == "" {
		return "", errors.New("githubci: commit response had no sha")
	}
	return body.SHA, nil
}

// CommitStatus is what SetCommitStatus reports. GitHub's Commit Status
// API needs only a PAT with repo:status scope — no GitHub App, no
// Checks API — matching this project's minimal-setup philosophy (a
// richer Checks-API upgrade with per-test annotations is a documented
// v2, see docs/GITHUB_CI.md).
type CommitStatus struct {
	State       string // pending | success | failure | error
	Description string
}

func (c *Client) SetCommitStatus(ctx context.Context, repo, sha, token string, status CommitStatus) error {
	target := fmt.Sprintf("%s/repos/%s/statuses/%s", c.baseURL(), repo, sha)
	payload, err := json.Marshal(map[string]string{
		"state":       status.State,
		"description": status.Description,
		"context":     statusContext,
	})
	if err != nil {
		return fmt.Errorf("githubci: encoding status payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target, strings.NewReader(string(payload)))
	if err != nil {
		return fmt.Errorf("githubci: building request: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Authorization", "token "+token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("githubci: GitHub API unreachable: %s", scrubToken(err.Error(), token))
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		return fmt.Errorf("githubci: GitHub API returned %d setting the commit status", resp.StatusCode)
	}
	return nil
}

// scrubToken removes a literal token from an error message before it's
// ever logged or returned — defence in depth: the token is sent only in
// an Authorization header, never the URL, so Go's own error wrapping
// shouldn't echo it, but a proxy/transport error message is out of this
// package's control.
func scrubToken(msg, token string) string {
	if token == "" {
		return msg
	}
	return strings.ReplaceAll(msg, token, "[token omitted]")
}
