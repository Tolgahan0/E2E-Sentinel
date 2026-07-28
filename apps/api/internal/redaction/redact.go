// Package redaction implements the context-sanitization pipeline required
// before any content is sent to an AI provider (spec §16.5). Unlike
// internal/logging.Redact (which redacts a value based on its *field
// name* in structured logs), this package scans free-form text content —
// file contents, command output, HTTP traffic — for embedded secrets
// regardless of where they appear.
package redaction

import (
	"regexp"
	"sort"
)

// Category identifies why a span of text was redacted.
type Category string

const (
	CategorySecret     Category = "secret"
	CategoryToken      Category = "token"
	CategoryCredential Category = "credential"
	CategoryAuthHeader Category = "authorization_header"
	CategoryCookie     Category = "cookie"
)

const placeholder = "[REDACTED]"

type rule struct {
	category Category
	pattern  *regexp.Regexp
	// replace, if set, computes the replacement text from the full match.
	// Used when only part of the match (e.g. a credential inside a URL)
	// should be redacted rather than the whole matched span.
	replace func(match string) string
}

var rules = []rule{
	{
		category: CategoryAuthHeader,
		pattern:  regexp.MustCompile(`(?im)^(authorization):\s*.+$`),
		replace:  func(string) string { return "Authorization: " + placeholder },
	},
	{
		category: CategoryCookie,
		pattern:  regexp.MustCompile(`(?im)^(cookie|set-cookie):\s*.+$`),
		replace: func(match string) string {
			re := regexp.MustCompile(`(?i)^(cookie|set-cookie):`)
			header := re.FindString(match)
			return header + " " + placeholder
		},
	},
	{
		// Private key blocks (PEM), e.g. -----BEGIN RSA PRIVATE KEY-----.
		category: CategorySecret,
		pattern:  regexp.MustCompile(`-----BEGIN [A-Z ]*PRIVATE KEY-----[\s\S]+?-----END [A-Z ]*PRIVATE KEY-----`),
	},
	{
		// AWS access key IDs.
		category: CategorySecret,
		pattern:  regexp.MustCompile(`\bAKIA[0-9A-Z]{16}\b`),
	},
	{
		// OpenAI/Anthropic-style secret keys.
		category: CategorySecret,
		pattern:  regexp.MustCompile(`\b(sk|sk-ant|sk-proj)-[A-Za-z0-9_\-]{16,}\b`),
	},
	{
		// key/value assignments that look like an API key or secret,
		// e.g. api_key: "abc123...", API_KEY=abc123...
		category: CategorySecret,
		pattern:  regexp.MustCompile(`(?i)\b(api[_-]?key|apikey|client[_-]?secret|access[_-]?key)\b\s*[:=]\s*['"]?[A-Za-z0-9_\-/+=]{12,}['"]?`),
	},
	{
		// Explicit password assignments.
		category: CategoryCredential,
		pattern:  regexp.MustCompile(`(?i)\bpassword\b\s*[:=]\s*['"]?\S+['"]?`),
	},
	{
		// user:password@host credentials embedded in a URL.
		category: CategoryCredential,
		pattern:  regexp.MustCompile(`([a-zA-Z][a-zA-Z0-9+.\-]*://)[^/\s:@]+:[^/\s@]+@`),
		replace: func(match string) string {
			return regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9+.\-]*://`).FindString(match) + placeholder + "@"
		},
	},
	{
		// JSON Web Tokens.
		category: CategoryToken,
		pattern:  regexp.MustCompile(`\beyJ[A-Za-z0-9_\-]+\.[A-Za-z0-9_\-]+\.[A-Za-z0-9_\-]+\b`),
	},
	{
		// Bearer tokens appearing outside a recognized Authorization header line.
		category: CategoryToken,
		pattern:  regexp.MustCompile(`(?i)\bBearer\s+[A-Za-z0-9\-_.=]{8,}`),
		replace:  func(string) string { return "Bearer " + placeholder },
	},
}

// Result is the outcome of running Redact over a piece of text.
type Result struct {
	// Text is the input with every matched span replaced.
	Text string
	// Categories lists, sorted and deduplicated, which kinds of content
	// were found and redacted. Never includes the redacted values
	// themselves — only category labels are safe to log or audit
	// (spec §16.5 step 10).
	Categories []Category
}

// Redact scans input for secrets, tokens, credentials, authorization
// headers, and cookies, replacing each match with a fixed placeholder.
// It never returns the original matched content anywhere in Result,
// including in Categories.
func Redact(input string) Result {
	text := input
	found := map[Category]bool{}

	for _, rl := range rules {
		if !rl.pattern.MatchString(text) {
			continue
		}
		found[rl.category] = true
		if rl.replace != nil {
			text = rl.pattern.ReplaceAllStringFunc(text, rl.replace)
		} else {
			text = rl.pattern.ReplaceAllString(text, placeholder)
		}
	}

	categories := make([]Category, 0, len(found))
	for c := range found {
		categories = append(categories, c)
	}
	sort.Slice(categories, func(i, j int) bool { return categories[i] < categories[j] })

	return Result{Text: text, Categories: categories}
}
