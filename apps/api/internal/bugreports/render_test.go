package bugreports

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func sampleBug() BugReport {
	now := time.Now()
	return BugReport{
		ID: "bug-1", ProjectID: "proj-1", TestCaseID: "tc-1", EnvironmentID: "env-1",
		Title: "Login crashes on 500", Severity: "high", FailureType: "network_failure",
		AffectedService: "api", AffectedRoute: "POST /api/v1/auth/login",
		Preconditions: "User exists", StepsToReproduce: []string{"Open /login", "Submit credentials"},
		ExpectedResult: "A friendly error", ActualResult: "Unhandled exception",
		Evidence:        Evidence{ArtifactIDs: []string{"artifact-1"}, ErrorMessage: "net::ERR_CONNECTION_REFUSED"},
		FirstObservedAt: now, LastObservedAt: now, Frequency: 3,
		RootCauseHypothesis: "The backend may have been unreachable.", RootCauseConfidence: "medium",
		FlakyAssessment: "likely_real_defect", RelatedGraphPath: "Login Page --calls--> POST /api/v1/auth/login",
		RegressionTestIDs: []string{"tc-1"}, Status: StatusOpen,
	}
}

func TestRenderMarkdown_LabelsRootCauseAsHypothesis(t *testing.T) {
	md := RenderMarkdown(sampleBug())
	if !strings.Contains(md, "unverified hypothesis") {
		t.Error("Markdown export must explicitly label the root cause as an unverified hypothesis")
	}
	if !strings.Contains(md, "not a confirmed diagnosis") {
		t.Error("Markdown export must disclaim the root cause as not a confirmed diagnosis")
	}
}

func TestRenderMarkdown_IncludesEvidenceAndCoreFields(t *testing.T) {
	md := RenderMarkdown(sampleBug())
	for _, want := range []string{
		"Login crashes on 500", "HIGH", "network_failure", "api", "POST /api/v1/auth/login",
		"Open /login", "A friendly error", "Unhandled exception", "artifact-1",
		"net::ERR_CONNECTION_REFUSED", "3 occurrence(s)", "likely_real_defect",
	} {
		if !strings.Contains(md, want) {
			t.Errorf("Markdown export missing %q:\n%s", want, md)
		}
	}
}

func TestRenderMarkdown_PossibleDuplicateIsMarkedUnconfirmed(t *testing.T) {
	b := sampleBug()
	b.PossibleDuplicateOfID = "bug-0"
	md := RenderMarkdown(b)
	if !strings.Contains(md, "unconfirmed") {
		t.Error("a possible duplicate hint must be marked unconfirmed in the export")
	}
}

func TestRenderJSON_MarksHypothesisFlag(t *testing.T) {
	data, err := RenderJSON(sampleBug())
	if err != nil {
		t.Fatalf("RenderJSON() error: %v", err)
	}
	var out map[string]any
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if out["root_cause_is_unverified_hypothesis"] != true {
		t.Error("JSON export must flag the root cause as an unverified hypothesis")
	}
	if out["root_cause_hypothesis"] != "The backend may have been unreachable." {
		t.Errorf("root_cause_hypothesis = %v", out["root_cause_hypothesis"])
	}
}

func TestRenderJSON_OmitsDuplicateFieldWhenUnset(t *testing.T) {
	data, err := RenderJSON(sampleBug())
	if err != nil {
		t.Fatalf("RenderJSON() error: %v", err)
	}
	if strings.Contains(string(data), "possible_duplicate_of_id") {
		t.Error("possible_duplicate_of_id should be omitted when not set")
	}
}
