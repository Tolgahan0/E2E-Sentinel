// Package testgen deterministically generates a runnable Playwright spec
// file from a planning.TestCase — no AI involved (spec §16.6 no-AI mode
// must support test execution). Generated tests avoid arbitrary sleeps,
// hard-coded secrets, and brittle selectors (spec §10.1); given no
// schema/AI input, they are necessarily smoke-level (page renders
// without a console error; API endpoint doesn't return a 5xx) — that
// honest ceiling is documented, not hidden.
package testgen

import (
	"errors"
	"fmt"
	"strings"
)

// ErrMissingBaseURL is returned when generation is attempted without a
// target base URL — there is nothing to point a generated test at.
var ErrMissingBaseURL = errors.New("testgen: environment has no base_url configured")

// ErrMissingWebSocketURL is returned when a "websocket" framework test
// case has no RoutePath — there is nothing to connect to.
var ErrMissingWebSocketURL = errors.New("testgen: test case has no WebSocket URL (RoutePath)")

// TestCaseInput is the subset of planning.TestCase generation needs.
// Defined locally (rather than importing internal/planning) so this
// package has zero dependency on the planning domain — it only needs
// plain strings.
type TestCaseInput struct {
	ID          string
	Title       string
	RoutePath   string
	RouteMethod string // "" for a browser page
	// Framework selects the generator. "" and "api" both mean Playwright
	// (dispatched below by RouteMethod, as before Phase 11); "websocket"
	// generates a plain Node.js WebSocket smoke-test script instead —
	// RoutePath for that framework already holds a full "ws://"/"wss://"
	// URL (see routes.Route's doc comment), never joined with baseURL.
	Framework string
}

var apiMethodFuncs = map[string]string{
	"GET": "get", "POST": "post", "PUT": "put", "PATCH": "patch", "DELETE": "delete", "HEAD": "head",
}

// GenerateSpec returns a filename and file content for tc, targeting
// baseURL (e.g. "http://localhost:3000"). baseURL is ignored for the
// "websocket" framework, which targets tc.RoutePath directly.
func GenerateSpec(tc TestCaseInput, baseURL string) (filename, content string, err error) {
	if tc.Framework == "websocket" {
		return generateWebSocketSpec(tc)
	}

	if baseURL == "" {
		return "", "", ErrMissingBaseURL
	}
	baseURL = strings.TrimRight(baseURL, "/")
	url := baseURL + tc.RoutePath

	filename = fmt.Sprintf("generated-%s.spec.ts", sanitizeID(tc.ID))
	title := escapeJSString(tc.Title)

	if tc.RouteMethod == "" {
		content = fmt.Sprintf(`import { test, expect } from '@playwright/test';

// Generated deterministically from a suggested test case — no AI
// involved. This is a smoke-level check: the page must render without
// throwing a console or page error. It does not assert business logic,
// since no schema or AI-derived expectation is available.
test('%s', async ({ page }) => {
  const errors: string[] = [];
  page.on('pageerror', (err) => errors.push(err.message));
  page.on('console', (msg) => {
    if (msg.type() === 'error') errors.push(msg.text());
  });

  await page.goto('%s');

  expect(errors, 'unexpected console/page errors: ' + errors.join(', ')).toEqual([]);
});
`, title, escapeJSString(url))
		return filename, content, nil
	}

	fn, ok := apiMethodFuncs[strings.ToUpper(tc.RouteMethod)]
	if !ok {
		fn = "get"
	}
	content = fmt.Sprintf(`import { test, expect } from '@playwright/test';

// Generated deterministically from a suggested test case — no AI
// involved. This is a smoke-level check: the endpoint must not return a
// 5xx server error. It does not assert a specific 2xx/4xx business
// outcome, since no schema or AI-derived expectation is available.
test('%s', async ({ request }) => {
  const response = await request.%s('%s');
  const status = response.status();
  expect(status, 'response status ' + status + ' indicates a server error').toBeLessThan(500);
});
`, title, fn, escapeJSString(url))
	return filename, content, nil
}

// generateWebSocketSpec returns a plain Node.js script (no Playwright,
// no npm project of its own — just the globally-installed "ws" package,
// see deploy/docker/Dockerfile.runner-websocket) that connects to
// tc.RoutePath and requires at least one message within a timeout. This
// is a smoke-level check: it does not assert message content/schema,
// since no AI-derived expectation is available — the same honest
// ceiling as this package's Playwright generators.
func generateWebSocketSpec(tc TestCaseInput) (filename, content string, err error) {
	if tc.RoutePath == "" {
		return "", "", ErrMissingWebSocketURL
	}
	filename = fmt.Sprintf("generated-%s.test.js", sanitizeID(tc.ID))
	content = fmt.Sprintf(`// %s
// Generated deterministically from a suggested test case — no AI
// involved. This is a smoke-level check: the endpoint must accept a
// WebSocket connection and send at least one message within the
// timeout. It does not assert message content/schema, since no
// AI-derived expectation is available.
const WebSocket = require('ws');

const url = %q;
const timeoutMs = 5000;
let settled = false;

const ws = new WebSocket(url);

const timer = setTimeout(() => {
  if (settled) return;
  settled = true;
  console.error('FAIL: no message received from ' + url + ' within ' + timeoutMs + 'ms');
  ws.terminate();
  process.exit(1);
}, timeoutMs);

ws.on('open', () => {
  console.log('connected to ' + url);
});

ws.on('message', (data) => {
  if (settled) return;
  settled = true;
  clearTimeout(timer);
  console.log('PASS: received message: ' + data.toString().slice(0, 200));
  ws.close();
  process.exit(0);
});

ws.on('error', (err) => {
  if (settled) return;
  settled = true;
  clearTimeout(timer);
  console.error('FAIL: connection error: ' + err.message);
  process.exit(1);
});
`, sanitizeComment(tc.Title), tc.RoutePath)
	return filename, content, nil
}

// sanitizeComment strips newlines so an arbitrary test case title can
// never break out of a "//" line comment.
func sanitizeComment(s string) string {
	s = strings.ReplaceAll(s, "\r", " ")
	return strings.ReplaceAll(s, "\n", " ")
}

func escapeJSString(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `'`, `\'`)
	s = strings.ReplaceAll(s, "\n", `\n`)
	return s
}

func sanitizeID(id string) string {
	var b strings.Builder
	for _, r := range id {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' {
			b.WriteRune(r)
		} else {
			b.WriteRune('-')
		}
	}
	return b.String()
}
