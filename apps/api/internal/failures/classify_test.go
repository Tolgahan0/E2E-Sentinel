package failures

import "testing"

func TestClassify_AssertionFailure(t *testing.T) {
	c := Classify(ClassifyInput{
		ExitCode: 1,
		Stdout:   "Error: expect(received).toBe(expected)\n\nExpected: \"Welcome back\"\nReceived: \"Login failed\"",
	})
	if c.FailureType != TypeAssertionFailure {
		t.Errorf("FailureType = %q, want %q", c.FailureType, TypeAssertionFailure)
	}
	if c.Severity != SeverityMedium {
		t.Errorf("Severity = %q, want %q", c.Severity, SeverityMedium)
	}
	if c.Expected != `"Welcome back"` {
		t.Errorf("Expected = %q", c.Expected)
	}
	if c.Actual != `"Login failed"` {
		t.Errorf("Actual = %q", c.Actual)
	}
}

func TestClassify_NetworkFailure(t *testing.T) {
	c := Classify(ClassifyInput{ExitCode: 1, Stderr: "Error: net::ERR_CONNECTION_REFUSED at http://example.com/"})
	if c.FailureType != TypeNetworkFailure {
		t.Errorf("FailureType = %q, want %q", c.FailureType, TypeNetworkFailure)
	}
	if c.Severity != SeverityHigh {
		t.Errorf("Severity = %q, want %q", c.Severity, SeverityHigh)
	}
}

func TestClassify_AuthenticationBeforeGenericAPIError(t *testing.T) {
	c := Classify(ClassifyInput{ExitCode: 1, Stdout: "response status 401 Unauthorized"})
	if c.FailureType != TypeAuthenticationFailure {
		t.Errorf("FailureType = %q, want %q (auth should win over generic api_error)", c.FailureType, TypeAuthenticationFailure)
	}
}

func TestClassify_AuthorizationFailure(t *testing.T) {
	c := Classify(ClassifyInput{ExitCode: 1, Stdout: "403 Forbidden: insufficient permissions"})
	if c.FailureType != TypeAuthorizationFailure {
		t.Errorf("FailureType = %q, want %q", c.FailureType, TypeAuthorizationFailure)
	}
}

func TestClassify_BrowserCrash(t *testing.T) {
	c := Classify(ClassifyInput{ExitCode: 1, Stderr: "Error: Target page, context or browser has been closed"})
	if c.FailureType != TypeBrowserCrash {
		t.Errorf("FailureType = %q, want %q", c.FailureType, TypeBrowserCrash)
	}
	if c.Severity != SeverityCritical {
		t.Errorf("Severity = %q, want %q", c.Severity, SeverityCritical)
	}
}

func TestClassify_RunnerFailure(t *testing.T) {
	c := Classify(ClassifyInput{ExitCode: 1, Stderr: "Error: Cannot find module '@playwright/test'"})
	if c.FailureType != TypeRunnerFailure {
		t.Errorf("FailureType = %q, want %q", c.FailureType, TypeRunnerFailure)
	}
}

func TestClassify_ServiceUnavailable(t *testing.T) {
	c := Classify(ClassifyInput{ExitCode: 1, Stdout: "502 Bad Gateway"})
	if c.FailureType != TypeServiceUnavailable {
		t.Errorf("FailureType = %q, want %q", c.FailureType, TypeServiceUnavailable)
	}
}

func TestClassify_UILocatorFailure(t *testing.T) {
	c := Classify(ClassifyInput{ExitCode: 1, Stderr: "Error: waiting for selector \"#submit\" failed: timeout exceeded"})
	// waiting for selector should win as a locator failure signal
	if c.FailureType != TypeUILocatorFailure {
		t.Errorf("FailureType = %q, want %q", c.FailureType, TypeUILocatorFailure)
	}
}

func TestClassify_UnknownWhenNoPatternMatches(t *testing.T) {
	c := Classify(ClassifyInput{ExitCode: 1, Stdout: "something odd happened"})
	if c.FailureType != TypeUnknown {
		t.Errorf("FailureType = %q, want %q", c.FailureType, TypeUnknown)
	}
	if c.RootCauseConfidence != ConfidenceLow {
		t.Errorf("RootCauseConfidence = %q, want %q for an unrecognized failure", c.RootCauseConfidence, ConfidenceLow)
	}
	if c.Severity != SeverityMedium {
		t.Errorf("Severity = %q, want %q (never underclaim an unknown failure)", c.Severity, SeverityMedium)
	}
}

func TestClassify_NeverReturnsHighConfidence(t *testing.T) {
	inputs := []ClassifyInput{
		{Stdout: "expect(received).toBe(expected)"},
		{Stderr: "net::ERR_CONNECTION_REFUSED"},
		{Stdout: "totally unrecognized text"},
	}
	for _, in := range inputs {
		c := Classify(in)
		if c.RootCauseConfidence == "high" {
			t.Errorf("Classify(%+v).RootCauseConfidence = high, want never-high (a hypothesis must never claim high confidence)", in)
		}
	}
}

func TestClassify_TitleFallsBackWhenNoOutput(t *testing.T) {
	c := Classify(ClassifyInput{ExitCode: 1})
	if c.Title != "Test failed with no output captured" {
		t.Errorf("Title = %q", c.Title)
	}
}

func TestSeverityForType_UnknownTypeDefaultsToMedium(t *testing.T) {
	if got := SeverityForType("not-a-real-type"); got != SeverityMedium {
		t.Errorf("SeverityForType(unknown) = %q, want %q", got, SeverityMedium)
	}
}
