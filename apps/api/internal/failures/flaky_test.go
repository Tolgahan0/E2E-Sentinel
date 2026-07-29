package failures

import "testing"

func TestAssessFlakiness_InsufficientEvidence(t *testing.T) {
	if got := AssessFlakiness([]bool{false}); got != FlakyInsufficientEvidence {
		t.Errorf("AssessFlakiness(single run) = %q, want %q", got, FlakyInsufficientEvidence)
	}
	if got := AssessFlakiness(nil); got != FlakyInsufficientEvidence {
		t.Errorf("AssessFlakiness(no runs) = %q, want %q", got, FlakyInsufficientEvidence)
	}
}

func TestAssessFlakiness_LikelyRealDefectWhenAllFail(t *testing.T) {
	got := AssessFlakiness([]bool{false, false, false, false})
	if got != FlakyLikelyRealDefect {
		t.Errorf("AssessFlakiness(all fail) = %q, want %q", got, FlakyLikelyRealDefect)
	}
}

func TestAssessFlakiness_SuspectWhenIsolatedFailureAfterPasses(t *testing.T) {
	// passed, passed, passed, FAILED (current)
	got := AssessFlakiness([]bool{true, true, true, false})
	if got != FlakySuspect {
		t.Errorf("AssessFlakiness(isolated failure) = %q, want %q", got, FlakySuspect)
	}
}

func TestAssessFlakiness_ConfirmedWhenFailureRateHigh(t *testing.T) {
	// 3 failures out of 5, with at least one pass -> above 0.6 threshold
	got := AssessFlakiness([]bool{true, false, false, false, false})
	if got != FlakyConfirmed {
		t.Errorf("AssessFlakiness(high failure rate) = %q, want %q", got, FlakyConfirmed)
	}
}

func TestAssessFlakiness_CandidateWhenMixedBelowThreshold(t *testing.T) {
	got := AssessFlakiness([]bool{true, false, true, false})
	if got != FlakyCandidate {
		t.Errorf("AssessFlakiness(mixed, below threshold) = %q, want %q", got, FlakyCandidate)
	}
}
