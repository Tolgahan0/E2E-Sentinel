// Package failures implements deterministic failure classification (spec
// §13) — turning a failed test run's stdout/stderr/exit code into a
// failure type, severity, and a clearly-labeled root cause hypothesis.
// No AI call is made or possible here: this is the permanent no-AI
// baseline (spec §16.6), independent of whether Phase 6 has a provider
// configured. Every root cause this package produces is a *hypothesis*
// derived from pattern-matching log text — never presented as a
// confirmed fact, and never assigned "high" confidence.
package failures

import (
	"regexp"
	"strings"
)

// Failure types (spec §13.1).
const (
	TypeAssertionFailure       = "assertion_failure"
	TypeUILocatorFailure       = "ui_locator_failure"
	TypeBrowserCrash           = "browser_crash"
	TypeNetworkFailure         = "network_failure"
	TypeAPIError               = "api_error"
	TypeSchemaMismatch         = "schema_mismatch"
	TypeAuthenticationFailure  = "authentication_failure"
	TypeAuthorizationFailure   = "authorization_failure"
	TypeTenantIsolationFailure = "tenant_isolation_failure"
	TypeDatabaseError          = "database_error"
	TypeQueueTimeout           = "queue_timeout"
	TypeWebSocketError         = "websocket_error"
	TypeServiceUnavailable     = "service_unavailable"
	TypeEnvironmentMisconfig   = "environment_misconfiguration"
	TypeFlakyTiming            = "flaky_timing"
	TypeRunnerFailure          = "runner_failure"
	TypeUnknown                = "unknown"
)

// Severity levels (spec §14).
const (
	SeverityCritical      = "critical"
	SeverityHigh          = "high"
	SeverityMedium        = "medium"
	SeverityLow           = "low"
	SeverityInformational = "informational"
)

// Confidence levels for a root cause hypothesis. Never "high" — a
// regex-matched log line is never certain enough to claim that.
const (
	ConfidenceMedium = "medium"
	ConfidenceLow    = "low"
)

// severityByType is a fixed, deterministic mapping — never adjusted
// based on the specific run, so the same failure_type always gets the
// same severity.
var severityByType = map[string]string{
	TypeBrowserCrash:           SeverityCritical,
	TypeRunnerFailure:          SeverityCritical,
	TypeNetworkFailure:         SeverityHigh,
	TypeServiceUnavailable:     SeverityHigh,
	TypeDatabaseError:          SeverityHigh,
	TypeAuthenticationFailure:  SeverityHigh,
	TypeAuthorizationFailure:   SeverityHigh,
	TypeTenantIsolationFailure: SeverityHigh,
	TypeAPIError:               SeverityMedium,
	TypeSchemaMismatch:         SeverityMedium,
	TypeAssertionFailure:       SeverityMedium,
	TypeQueueTimeout:           SeverityMedium,
	TypeWebSocketError:         SeverityMedium,
	TypeEnvironmentMisconfig:   SeverityMedium,
	TypeUILocatorFailure:       SeverityLow,
	TypeFlakyTiming:            SeverityLow,
	TypeUnknown:                SeverityMedium, // conservative: never underclaim an unclassified failure
}

// SeverityForType returns the fixed severity for a failure type.
func SeverityForType(failureType string) string {
	if s, ok := severityByType[failureType]; ok {
		return s
	}
	return SeverityMedium
}

// ClassifyInput is what Classify needs from a finished run.
type ClassifyInput struct {
	ExitCode int
	Stdout   string
	Stderr   string
}

// Classification is the deterministic output of Classify.
type Classification struct {
	FailureType         string
	Severity            string
	Title               string
	Expected            string
	Actual              string
	ErrorMessage        string
	StackTrace          string
	RootCauseHypothesis string
	RootCauseConfidence string
}

type signature struct {
	failureType string
	pattern     *regexp.Regexp
	rootCause   string
	confidence  string
}

// signatures are checked in order; the first match wins. Order matters:
// more specific patterns (auth/authz) are checked before generic ones
// (api_error) so a 401/403 isn't misclassified as a bare API error.
var signatures = []signature{
	{TypeAuthenticationFailure, regexp.MustCompile(`(?i)\b(401|unauthorized)\b`),
		"The request was rejected as unauthenticated — likely expired, missing, or invalid credentials.", ConfidenceMedium},
	{TypeAuthorizationFailure, regexp.MustCompile(`(?i)\b(403|forbidden)\b`),
		"The request was rejected as unauthorized — likely a permissions or role-check issue.", ConfidenceMedium},
	{TypeTenantIsolationFailure, regexp.MustCompile(`(?i)tenant.*(isolation|leak|mismatch)`),
		"Data belonging to a different tenant may have been returned — a possible tenant isolation defect.", ConfidenceLow},
	{TypeDatabaseError, regexp.MustCompile(`(?i)(connection pool exhausted|database.*(unavailable|connection refused)|sqlstate|deadlock detected)`),
		"The database layer appears to have failed or been exhausted, which likely caused the request to fail.", ConfidenceMedium},
	{TypeQueueTimeout, regexp.MustCompile(`(?i)(queue.*(timeout|timed out)|consumer.*timeout)`),
		"A message queue operation timed out — the consumer or broker may be unavailable or overloaded.", ConfidenceLow},
	{TypeWebSocketError, regexp.MustCompile(`(?i)websocket.*(error|closed|failed)`),
		"A WebSocket connection failed or closed unexpectedly.", ConfidenceLow},
	{TypeBrowserCrash, regexp.MustCompile(`(?i)(target (page|browser).*(closed|crashed)|browser.*crashed|page crashed)`),
		"The browser process crashed or was closed mid-test — likely a browser stability issue, not the application under test.", ConfidenceMedium},
	{TypeRunnerFailure, regexp.MustCompile(`(?i)(cannot find module|enoent|docker.*(daemon|error)|no such file or directory)`),
		"The test runner itself failed before exercising the application — an infrastructure/environment issue, not a product defect.", ConfidenceMedium},
	{TypeNetworkFailure, regexp.MustCompile(`(?i)(net::err_|econnrefused|enotfound|getaddrinfo|dial tcp.*refused)`),
		"The test could not reach the target host — likely a network issue, DNS failure, or the target service being down.", ConfidenceMedium},
	{TypeServiceUnavailable, regexp.MustCompile(`(?i)\b(502|503|504|service unavailable|bad gateway|gateway timeout)\b`),
		"The backend service returned a gateway/unavailable error — it may be down, restarting, or overloaded.", ConfidenceMedium},
	{TypeSchemaMismatch, regexp.MustCompile(`(?i)(unexpected token|json.*(parse|unmarshal).*error|schema validation failed)`),
		"The response body did not match the shape the test expected — a possible API contract/schema change.", ConfidenceLow},
	{TypeUILocatorFailure, regexp.MustCompile(`(?i)(waiting for selector|locator.*not found|element.*not found|no element found)`),
		"An expected page element could not be located — the UI may have changed, or the element wasn't rendered in time.", ConfidenceLow},
	{TypeFlakyTiming, regexp.MustCompile(`(?i)(timeout.*exceeded|timed out after|exceeded while waiting)`),
		"The test exceeded its wait/timeout budget — this can indicate real slowness or test timing sensitivity.", ConfidenceLow},
	{TypeAssertionFailure, regexp.MustCompile(`(?i)(expect\(|assertionerror|toequal|tobe\(|received:.*expected:|expected.*but.*received)`),
		"An assertion in the test failed — the application's actual behavior did not match what the test expected.", ConfidenceMedium},
	{TypeAPIError, regexp.MustCompile(`(?i)\b(4\d\d|5\d\d)\b.*(error|status)`),
		"The API returned an error status code.", ConfidenceLow},
}

var (
	expectedRe = regexp.MustCompile(`(?im)^\s*expected:?\s*(.+)$`)
	actualRe   = regexp.MustCompile(`(?im)^\s*(received|actual):?\s*(.+)$`)
)

// Classify deterministically derives a Classification from a failed
// run's output. It never returns an empty FailureType — an
// unrecognized pattern classifies as TypeUnknown rather than being left
// blank, so every failure is still triaged into the severity model.
func Classify(input ClassifyInput) Classification {
	combined := input.Stdout + "\n" + input.Stderr

	failureType := TypeUnknown
	rootCause := "The failure did not match any known pattern — manual investigation is needed to determine the cause."
	confidence := ConfidenceLow
	for _, sig := range signatures {
		if sig.pattern.MatchString(combined) {
			failureType = sig.failureType
			rootCause = sig.rootCause
			confidence = sig.confidence
			break
		}
	}

	errorMessage := firstNonEmptyLine(input.Stderr)
	if errorMessage == "" {
		errorMessage = firstNonEmptyLine(input.Stdout)
	}

	title := errorMessage
	if title == "" {
		title = "Test failed with no output captured"
	}
	if len(title) > 200 {
		title = title[:200] + "…"
	}

	var expected, actual string
	if m := expectedRe.FindStringSubmatch(combined); m != nil {
		expected = strings.TrimSpace(m[1])
	}
	if m := actualRe.FindStringSubmatch(combined); m != nil {
		actual = strings.TrimSpace(m[2])
	}

	return Classification{
		FailureType:         failureType,
		Severity:            SeverityForType(failureType),
		Title:               title,
		Expected:            expected,
		Actual:              actual,
		ErrorMessage:        errorMessage,
		StackTrace:          truncate(input.Stderr, 4000),
		RootCauseHypothesis: rootCause,
		RootCauseConfidence: confidence,
	}
}

func firstNonEmptyLine(s string) string {
	for _, line := range strings.Split(s, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "…"
}
