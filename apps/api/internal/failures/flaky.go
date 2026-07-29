package failures

// Flaky assessment labels (spec §13.2). A test is never silently
// downgraded or hidden because it's labeled flaky — the label is
// informational, attached to the bug report, and still surfaced.
const (
	// FlakyInsufficientEvidence means fewer than two runs exist for this
	// test case — there isn't enough history to say anything.
	FlakyInsufficientEvidence = "insufficient_evidence"
	// FlakySuspect: this is the only failure amid an otherwise-passing
	// history — "failed once, passed on retry" read in reverse: an
	// isolated failure surrounded by passes.
	FlakySuspect = "suspect"
	// FlakyCandidate: a mix of passes and failures, but not frequent
	// enough to cross the "flaky" threshold below.
	FlakyCandidate = "flaky_candidate"
	// FlakyConfirmed: failure rate meets or exceeds flakyRateThreshold,
	// with at least one pass — genuinely intermittent, not deterministic.
	FlakyConfirmed = "flaky"
	// FlakyLikelyRealDefect: every recent run failed — a deterministic,
	// same-step failure, the strongest signal this is a real defect.
	FlakyLikelyRealDefect = "likely_real_defect"
)

// flakyRateThreshold is the failure-rate cutoff, of runs that had at
// least one pass, above which a test is called confirmed-flaky rather
// than just a "candidate" (spec §13.2 "failure rate threshold
// exceeded").
const flakyRateThreshold = 0.6

// AssessFlakiness classifies a test case's flakiness from its recent run
// outcomes. passed must be ordered oldest-first and end with the
// current (failing) run's own outcome — i.e. the last element reflects
// the failure that triggered this assessment.
func AssessFlakiness(passed []bool) string {
	n := len(passed)
	if n < 2 {
		return FlakyInsufficientEvidence
	}

	passCount := 0
	for _, p := range passed {
		if p {
			passCount++
		}
	}
	if passCount == 0 {
		return FlakyLikelyRealDefect
	}

	failCount := n - passCount
	failureRate := float64(failCount) / float64(n)

	if failCount == 1 && passed[n-2] {
		return FlakySuspect
	}
	if failureRate >= flakyRateThreshold {
		return FlakyConfirmed
	}
	return FlakyCandidate
}
