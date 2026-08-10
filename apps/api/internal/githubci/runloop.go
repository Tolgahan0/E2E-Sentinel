package githubci

import (
	"context"
	"fmt"
	"time"

	"github.com/rs/zerolog"

	"e2e-sentinel/apps/api/internal/planning"
	"e2e-sentinel/apps/api/internal/projects"
	"e2e-sentinel/apps/api/internal/runs"
	"e2e-sentinel/apps/api/internal/secretstore"
)

// pollRunInterval is how often a just-triggered run is checked for
// completion before this project's aggregate status is reported.
const pollRunInterval = 3 * time.Second

// TriggerFunc starts one approved test case's run — bound to
// httpserver.TriggerRun by the caller (main.go), passed in rather than
// imported directly so this package stays decoupled from httpserver and
// trivially testable with a fake. Matches httpserver.TriggerRun's
// signature exactly.
type TriggerFunc func(ctx context.Context, testCaseID, triggerType, triggeredBy, commitSHA string) (runs.TestRun, error)

// RunLoop checks once immediately, then on interval, until ctx is
// cancelled — the same never-stop-on-a-failed-tick shape as
// artifacts.RunRetentionLoop and updatecheck.RunLoop. One project
// failing to poll (bad token, GitHub API hiccup, etc.) never blocks any
// other project's tick.
func RunLoop(
	ctx context.Context,
	projectStore projects.Store,
	runStore runs.Store,
	planningStore planning.Store,
	secrets secretstore.Store,
	client *Client,
	trigger TriggerFunc,
	interval time.Duration,
	logger zerolog.Logger,
) {
	PollOnce(ctx, projectStore, runStore, planningStore, secrets, client, trigger, logger)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			PollOnce(ctx, projectStore, runStore, planningStore, secrets, client, trigger, logger)
		}
	}
}

// PollOnce checks every github-ci-configured project once. Exported
// (rather than an unexported closure inside RunLoop) so tests can drive
// a single, deterministic pass without a real timer.
func PollOnce(
	ctx context.Context,
	projectStore projects.Store,
	runStore runs.Store,
	planningStore planning.Store,
	secrets secretstore.Store,
	client *Client,
	trigger TriggerFunc,
	logger zerolog.Logger,
) {
	list, err := projectStore.ListWithGitHubCI(ctx)
	if err != nil {
		logger.Warn().Err(err).Msg("github-ci: listing configured projects failed")
		return
	}
	for _, p := range list {
		pollProject(ctx, projectStore, runStore, planningStore, secrets, client, trigger, p, logger)
	}
}

func pollProject(
	ctx context.Context,
	projectStore projects.Store,
	runStore runs.Store,
	planningStore planning.Store,
	secrets secretstore.Store,
	client *Client,
	trigger TriggerFunc,
	p projects.Project,
	logger zerolog.Logger,
) {
	log := logger.With().Str("project_id", p.ID).Str("github_repo", p.GitHubRepo).Logger()

	if secrets == nil || p.GitHubTokenSecretReferenceID == "" {
		log.Warn().Msg("github-ci: no token configured, skipping")
		return
	}
	token, err := secrets.Resolve(ctx, p.GitHubTokenSecretReferenceID)
	if err != nil {
		log.Warn().Err(err).Msg("github-ci: resolving token failed")
		return
	}

	branch := p.DefaultBranch
	if branch == "" {
		branch = "main"
	}

	sha, err := client.LatestCommit(ctx, p.GitHubRepo, branch, token)
	if err != nil {
		log.Warn().Err(err).Msg("github-ci: checking the latest commit failed")
		return
	}
	if sha == p.LastCICommitSHA {
		return // nothing new since the last tick
	}
	log = log.With().Str("commit_sha", sha).Logger()

	if err := client.SetCommitStatus(ctx, p.GitHubRepo, sha, token, CommitStatus{
		State: StatusPending, Description: "E2E Sentinel: running approved test cases",
	}); err != nil {
		// A failed status post is never a reason to skip actually
		// running the tests — log it and continue.
		log.Warn().Err(err).Msg("github-ci: posting pending status failed")
	}

	cases, err := planningStore.List(ctx, p.ID)
	if err != nil {
		log.Warn().Err(err).Msg("github-ci: listing test cases failed")
		return
	}

	var finished []runs.TestRun
	for _, tc := range cases {
		if tc.ApprovalStatus != planning.ApprovalApproved {
			continue
		}
		run, err := trigger(ctx, tc.ID, runs.TriggerTypeCI, "github-ci", sha)
		if err != nil {
			log.Warn().Err(err).Str("test_case_id", tc.ID).Msg("github-ci: triggering run failed")
			continue
		}
		result, err := waitForRun(ctx, runStore, run.ID)
		if err != nil {
			log.Warn().Err(err).Str("run_id", run.ID).Msg("github-ci: waiting for run failed")
			continue
		}
		finished = append(finished, result)
	}

	status := aggregateStatus(finished)
	if err := client.SetCommitStatus(ctx, p.GitHubRepo, sha, token, status); err != nil {
		log.Warn().Err(err).Msg("github-ci: posting final status failed")
	}

	if err := projectStore.SetLastCICommitSHA(ctx, p.ID, sha); err != nil {
		log.Warn().Err(err).Msg("github-ci: recording last CI commit sha failed")
	}
}

// aggregateStatus reports success only if every triggered run passed —
// no approved test cases at all is also reported as success (nothing
// failed), not left pending forever.
func aggregateStatus(finished []runs.TestRun) CommitStatus {
	if len(finished) == 0 {
		return CommitStatus{State: StatusSuccess, Description: "E2E Sentinel: no approved test cases to run"}
	}
	passed := 0
	for _, r := range finished {
		if r.Status == runs.StatusPassed {
			passed++
		}
	}
	state := StatusSuccess
	if passed != len(finished) {
		state = StatusFailure
	}
	return CommitStatus{State: state, Description: fmt.Sprintf("E2E Sentinel: %d/%d passed", passed, len(finished))}
}

// waitForRun polls until run has a terminal status (FinishedAt set) —
// TriggerRun's execution happens in its own background goroutine, so
// this is the only way to know when it's done. Checks once immediately
// (a fast/local run may already be finished) before the first wait.
func waitForRun(ctx context.Context, runStore runs.Store, runID string) (runs.TestRun, error) {
	ticker := time.NewTicker(pollRunInterval)
	defer ticker.Stop()
	for {
		run, err := runStore.Get(ctx, runID)
		if err != nil {
			return runs.TestRun{}, err
		}
		if run.FinishedAt != nil {
			return run, nil
		}
		select {
		case <-ctx.Done():
			return runs.TestRun{}, ctx.Err()
		case <-ticker.C:
		}
	}
}
