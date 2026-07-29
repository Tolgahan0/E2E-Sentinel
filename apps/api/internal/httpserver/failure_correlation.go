package httpserver

import (
	"context"
	"strings"
	"time"

	"e2e-sentinel/apps/api/internal/audit"
	"e2e-sentinel/apps/api/internal/bugreports"
	"e2e-sentinel/apps/api/internal/failures"
	"e2e-sentinel/apps/api/internal/graph"
	"e2e-sentinel/apps/api/internal/runs"
	"e2e-sentinel/apps/api/internal/webhooks"
)

// recordFailureAndBug turns a failed/errored run into a Failure record
// and an up-to-date bug report (spec §13, §14). It is best-effort: any
// lookup failure is logged and skipped rather than blocking run
// completion, since the run's own pass/fail result (already recorded by
// the caller) must never depend on this succeeding.
func recordFailureAndBug(ctx context.Context, deps Dependencies, run runs.TestRun, result *runs.RunResult, artifactIDs []string) {
	logger := deps.Logger

	tc, err := deps.Planning.Get(ctx, run.TestCaseID)
	if err != nil {
		logger.Warn().Err(err).Str("run_id", run.ID).Msg("failure correlation: loading test case failed")
		return
	}

	classification := failures.Classify(failures.ClassifyInput{ExitCode: result.ExitCode, Stdout: result.Stdout, Stderr: result.Stderr})

	f, err := deps.Failures.Create(ctx, failures.Failure{
		TestRunID: run.ID, TestCaseID: tc.ID, Title: classification.Title, Severity: classification.Severity,
		FailureType: classification.FailureType, Expected: classification.Expected, Actual: classification.Actual,
		ErrorMessage: classification.ErrorMessage, StackTrace: classification.StackTrace,
		RootCauseHypothesis: classification.RootCauseHypothesis, ConfidenceScore: classification.RootCauseConfidence,
		ArtifactIDs: artifactIDs,
	})
	if err != nil {
		logger.Warn().Err(err).Str("run_id", run.ID).Msg("failure correlation: recording failure failed")
		return
	}

	var environmentID string
	if envs, err := deps.Environments.ListByProject(ctx, run.ProjectID); err == nil {
		for _, env := range envs {
			if env.BaseURL != "" {
				environmentID = env.ID
				break
			}
		}
	}

	routeLabel := tc.RoutePath
	if tc.RouteMethod != "" {
		routeLabel = tc.RouteMethod + " " + tc.RoutePath
	}
	var relatedGraphPath, affectedService string
	if _, edges, err := deps.Graph.Get(ctx, run.ProjectID); err == nil {
		relatedGraphPath, affectedService = buildRelatedGraphPath(edges, routeLabel)
	}

	var passed []bool
	if history, err := deps.Runs.ListByTestCase(ctx, tc.ID); err == nil {
		for _, r := range history {
			switch r.Status {
			case runs.StatusPassed:
				passed = append(passed, true)
			case runs.StatusFailed, runs.StatusError:
				passed = append(passed, false)
			}
		}
	}
	flakyAssessment := failures.AssessFlakiness(passed)

	bug, isNew, err := deps.Bugs.UpsertFromFailure(ctx, bugreports.UpsertInput{
		ProjectID: run.ProjectID, FailureID: f.ID, TestCaseID: tc.ID, EnvironmentID: environmentID,
		Title: classification.Title, Severity: classification.Severity, FailureType: classification.FailureType,
		AffectedService: affectedService, AffectedRoute: routeLabel,
		Preconditions: tc.Preconditions, StepsToReproduce: tc.Steps,
		ExpectedResult: classification.Expected, ActualResult: classification.Actual,
		Evidence:            bugreports.Evidence{ArtifactIDs: artifactIDs, ErrorMessage: classification.ErrorMessage, StackTrace: classification.StackTrace},
		RootCauseHypothesis: classification.RootCauseHypothesis, RootCauseConfidence: classification.RootCauseConfidence,
		FlakyAssessment: flakyAssessment, RelatedGraphPath: relatedGraphPath, RegressionTestIDs: []string{tc.ID},
		ObservedAt: time.Now(),
	})
	if err != nil {
		logger.Warn().Err(err).Str("run_id", run.ID).Msg("failure correlation: upserting bug report failed")
		return
	}

	actionType := "bug_report.updated"
	if isNew {
		actionType = "bug_report.created"
	}
	if err := deps.Audit.Record(ctx, audit.Event{
		ActionType: actionType, ResourceType: "bug_report", ResourceID: bug.ID, Actor: "system",
		Metadata: map[string]any{"severity": bug.Severity, "failure_type": bug.FailureType, "frequency": bug.Frequency},
	}); err != nil {
		logger.Warn().Err(err).Msg("recording bug_report audit event failed")
	}

	// Only the first time this exact (project, test case, failure type)
	// combination is seen — a recurring failure just bumps the existing
	// bug's frequency, which would otherwise notify on every single
	// re-occurrence.
	if isNew {
		notifyAsync(deps, webhooks.Event{
			Type: webhooks.EventBugReportCreated, ProjectID: bug.ProjectID, ResourceType: "bug_report",
			ResourceID: bug.ID, Title: bug.Title, Severity: bug.Severity, OccurredAt: time.Now(),
		})
	}
}

// buildRelatedGraphPath looks for a graph edge into routeLabel (e.g. a
// page's "calls" edge) and an edge out of it (e.g. a "served_by" edge to
// a service), and renders whichever side(s) exist as a short evidence
// trail. Returns ("", "") when the route isn't in the graph at all —
// never a guessed path.
func buildRelatedGraphPath(edges []graph.ResolvedEdge, routeLabel string) (path string, affectedService string) {
	if routeLabel == "" {
		return "", ""
	}

	var incoming, outgoing *graph.ResolvedEdge
	for i := range edges {
		e := &edges[i]
		if e.TargetLabel == routeLabel && incoming == nil {
			incoming = e
		}
		if e.SourceLabel == routeLabel && outgoing == nil {
			outgoing = e
		}
	}

	var parts []string
	if incoming != nil {
		parts = append(parts, incoming.SourceLabel, "--"+incoming.RelationType+"-->")
	}
	parts = append(parts, routeLabel)
	if outgoing != nil {
		parts = append(parts, "--"+outgoing.RelationType+"-->", outgoing.TargetLabel)
		affectedService = outgoing.TargetLabel
	}

	if incoming == nil && outgoing == nil {
		return "", ""
	}
	return strings.Join(parts, " "), affectedService
}
