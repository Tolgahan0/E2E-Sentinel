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

// TestCaseInput is the subset of planning.TestCase generation needs.
// Defined locally (rather than importing internal/planning) so this
// package has zero dependency on the planning domain — it only needs
// plain strings.
type TestCaseInput struct {
	ID          string
	Title       string
	RoutePath   string
	RouteMethod string // "" for a browser page
}

var apiMethodFuncs = map[string]string{
	"GET": "get", "POST": "post", "PUT": "put", "PATCH": "patch", "DELETE": "delete", "HEAD": "head",
}

// GenerateSpec returns a filename and file content for tc, targeting
// baseURL (e.g. "http://localhost:3000").
func GenerateSpec(tc TestCaseInput, baseURL string) (filename, content string, err error) {
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
