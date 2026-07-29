package bugreports

import (
	"encoding/json"
	"fmt"
	"strings"
)

// ExportView is the JSON export shape (spec §14 "Export JSON"). It's
// distinct from the API's detail response only in that the root cause
// fields are unambiguously labeled as a hypothesis, so a consumer of the
// exported file can't mistake it for a confirmed fact even out of
// context.
type ExportView struct {
	ID               string   `json:"id"`
	Title            string   `json:"title"`
	Severity         string   `json:"severity"`
	FailureType      string   `json:"failure_type"`
	Environment      string   `json:"environment_id"`
	AffectedService  string   `json:"affected_service"`
	AffectedRoute    string   `json:"affected_route"`
	AffectedTest     string   `json:"affected_test_case_id"`
	Preconditions    string   `json:"preconditions"`
	StepsToReproduce []string `json:"steps_to_reproduce"`
	ExpectedResult   string   `json:"expected_result"`
	ActualResult     string   `json:"actual_result"`
	Evidence         Evidence `json:"evidence"`
	FirstObserved    string   `json:"first_observed"`
	LastObserved     string   `json:"last_observed"`
	Frequency        int      `json:"frequency"`

	RootCauseHypothesis             string `json:"root_cause_hypothesis"`
	RootCauseConfidence             string `json:"root_cause_confidence"`
	RootCauseIsUnverifiedHypothesis bool   `json:"root_cause_is_unverified_hypothesis"`

	FlakyAssessment       string   `json:"flaky_assessment"`
	RelatedGraphPath      string   `json:"related_graph_path"`
	RegressionTestIDs     []string `json:"regression_test_ids"`
	PossibleDuplicateOfID string   `json:"possible_duplicate_of_id,omitempty"`
	Status                string   `json:"status"`
}

// ToExportView builds the export representation of b.
func ToExportView(b BugReport) ExportView {
	return ExportView{
		ID: b.ID, Title: b.Title, Severity: b.Severity, FailureType: b.FailureType,
		Environment: b.EnvironmentID, AffectedService: b.AffectedService, AffectedRoute: b.AffectedRoute,
		AffectedTest: b.TestCaseID, Preconditions: b.Preconditions, StepsToReproduce: b.StepsToReproduce,
		ExpectedResult: b.ExpectedResult, ActualResult: b.ActualResult, Evidence: b.Evidence,
		FirstObserved:       b.FirstObservedAt.Format("2006-01-02T15:04:05Z07:00"),
		LastObserved:        b.LastObservedAt.Format("2006-01-02T15:04:05Z07:00"),
		Frequency:           b.Frequency,
		RootCauseHypothesis: b.RootCauseHypothesis, RootCauseConfidence: b.RootCauseConfidence,
		RootCauseIsUnverifiedHypothesis: true,
		FlakyAssessment:                 b.FlakyAssessment, RelatedGraphPath: b.RelatedGraphPath,
		RegressionTestIDs: b.RegressionTestIDs, PossibleDuplicateOfID: b.PossibleDuplicateOfID, Status: b.Status,
	}
}

// RenderJSON returns the pretty-printed JSON export.
func RenderJSON(b BugReport) ([]byte, error) {
	return json.MarshalIndent(ToExportView(b), "", "  ")
}

// RenderMarkdown returns the Markdown export (spec §14 field list).
// "Likely root cause" is always immediately followed by an explicit
// "(unverified hypothesis)" qualifier — never presented as a diagnosis.
func RenderMarkdown(b BugReport) string {
	var sb strings.Builder

	fmt.Fprintf(&sb, "# %s\n\n", b.Title)
	fmt.Fprintf(&sb, "**ID:** %s\n\n", b.ID)
	fmt.Fprintf(&sb, "**Severity:** %s\n\n", strings.ToUpper(b.Severity))
	fmt.Fprintf(&sb, "**Status:** %s\n\n", b.Status)
	fmt.Fprintf(&sb, "**Failure type:** %s\n\n", b.FailureType)
	if b.EnvironmentID != "" {
		fmt.Fprintf(&sb, "**Environment:** %s\n\n", b.EnvironmentID)
	}
	if b.AffectedService != "" {
		fmt.Fprintf(&sb, "**Affected service:** %s\n\n", b.AffectedService)
	}
	if b.AffectedRoute != "" {
		fmt.Fprintf(&sb, "**Affected route:** %s\n\n", b.AffectedRoute)
	}
	fmt.Fprintf(&sb, "**Affected test:** %s\n\n", b.TestCaseID)

	if b.Preconditions != "" {
		fmt.Fprintf(&sb, "## Preconditions\n\n%s\n\n", b.Preconditions)
	}

	if len(b.StepsToReproduce) > 0 {
		sb.WriteString("## Steps to reproduce\n\n")
		for i, step := range b.StepsToReproduce {
			fmt.Fprintf(&sb, "%d. %s\n", i+1, step)
		}
		sb.WriteString("\n")
	}

	fmt.Fprintf(&sb, "## Expected result\n\n%s\n\n", orDash(b.ExpectedResult))
	fmt.Fprintf(&sb, "## Actual result\n\n%s\n\n", orDash(b.ActualResult))

	sb.WriteString("## Evidence\n\n")
	if b.Evidence.ErrorMessage != "" {
		fmt.Fprintf(&sb, "- Error message: `%s`\n", b.Evidence.ErrorMessage)
	}
	for _, id := range b.Evidence.ArtifactIDs {
		fmt.Fprintf(&sb, "- Artifact: %s\n", id)
	}
	if b.Evidence.StackTrace != "" {
		fmt.Fprintf(&sb, "\n```\n%s\n```\n", b.Evidence.StackTrace)
	}
	sb.WriteString("\n")

	fmt.Fprintf(&sb, "**First observed:** %s\n\n", b.FirstObservedAt.Format("2006-01-02T15:04:05Z07:00"))
	fmt.Fprintf(&sb, "**Last observed:** %s\n\n", b.LastObservedAt.Format("2006-01-02T15:04:05Z07:00"))
	fmt.Fprintf(&sb, "**Frequency:** %d occurrence(s)\n\n", b.Frequency)
	if b.FlakyAssessment != "" {
		fmt.Fprintf(&sb, "**Flakiness assessment:** %s\n\n", b.FlakyAssessment)
	}

	sb.WriteString("## Likely root cause (unverified hypothesis)\n\n")
	fmt.Fprintf(&sb, "%s\n\n", orDash(b.RootCauseHypothesis))
	fmt.Fprintf(&sb, "**Root cause confidence:** %s — this is a hypothesis derived from pattern-matching the failure output, not a confirmed diagnosis.\n\n", b.RootCauseConfidence)

	if b.RelatedGraphPath != "" {
		fmt.Fprintf(&sb, "**Related graph path:** %s\n\n", b.RelatedGraphPath)
	}
	if len(b.RegressionTestIDs) > 0 {
		fmt.Fprintf(&sb, "**Regression tests:** %s\n\n", strings.Join(b.RegressionTestIDs, ", "))
	}
	if b.PossibleDuplicateOfID != "" {
		fmt.Fprintf(&sb, "**Possible duplicate of:** %s (unconfirmed — review before linking)\n\n", b.PossibleDuplicateOfID)
	}

	if len(b.Notes) > 0 {
		sb.WriteString("## Notes\n\n")
		for _, n := range b.Notes {
			fmt.Fprintf(&sb, "- *%s* (%s): %s\n", n.Author, n.CreatedAt.Format("2006-01-02T15:04:05Z07:00"), n.Text)
		}
	}

	return sb.String()
}

func orDash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}
