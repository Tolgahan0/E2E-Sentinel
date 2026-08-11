package githubci

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path"
	"testing"
	"time"

	"github.com/rs/zerolog"

	"e2e-sentinel/apps/api/internal/planning"
	"e2e-sentinel/apps/api/internal/projects"
	"e2e-sentinel/apps/api/internal/runs"
	"e2e-sentinel/apps/api/internal/secretstore"
)

func testEncryptor(t *testing.T) *secretstore.Encryptor {
	t.Helper()
	enc, err := secretstore.NewEncryptor(make([]byte, secretstore.KeySize))
	if err != nil {
		t.Fatalf("NewEncryptor() error: %v", err)
	}
	return enc
}

// newFakeGitHubServer serves a fixed latest-commit SHA and records
// every commit-status POST it receives into statuses, so tests can
// assert on the sequence (pending, then a final success/failure)
// without a real GitHub token.
func newFakeGitHubServer(t *testing.T, sha string, statuses *[]CommitStatus) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet:
			_ = json.NewEncoder(w).Encode(map[string]string{"sha": sha})
		case r.Method == http.MethodPost:
			var body CommitStatus
			_ = json.NewDecoder(r.Body).Decode(&body)
			*statuses = append(*statuses, body)
			w.WriteHeader(http.StatusCreated)
		}
	}))
}

// immediateTrigger fakes httpserver.TriggerRun: it creates an
// already-finished TestRun synchronously (no background goroutine, no
// real runner) so PollOnce's waitForRun returns on its first check —
// exactly what a real TriggerRun does eventually, just without the
// wait, keeping this test deterministic and fast.
func immediateTrigger(store runs.Store, statusByTestCase map[string]string) TriggerFunc {
	return func(ctx context.Context, testCaseID, triggerType, triggeredBy, commitSHA string) (runs.TestRun, error) {
		run, err := store.Create(ctx, runs.TestRun{
			TestCaseID: testCaseID, Status: runs.StatusQueued, RunnerType: "fake",
			TriggerType: triggerType, TriggeredBy: triggeredBy, CommitSHA: commitSHA,
		})
		if err != nil {
			return runs.TestRun{}, err
		}
		status := statusByTestCase[testCaseID]
		if status == "" {
			status = runs.StatusPassed
		}
		return store.UpdateStatus(ctx, run.ID, status, nil, "", true)
	}
}

// postedStatus records one commit-status POST along with which SHA it
// was posted to — newFakeGitHubServer doesn't need this (it only ever
// deals with one SHA), but a PR-polling test has to tell a status
// posted to the branch tip apart from one posted to a PR head.
type postedStatus struct {
	SHA    string
	Status CommitStatus
}

// newFakeGitHubServerWithPRs is newFakeGitHubServer's PR-aware sibling:
// it serves a fixed branch-tip SHA on GET .../commits/*, a fixed list
// of open pull requests on GET .../pulls, and records every
// commit-status POST (with the SHA it targeted) into statuses.
func newFakeGitHubServerWithPRs(t *testing.T, branchSHA string, prs []PullRequest, statuses *[]postedStatus) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && path.Base(r.URL.Path) == "pulls":
			body := make([]map[string]any, 0, len(prs))
			for _, pr := range prs {
				body = append(body, map[string]any{"number": pr.Number, "head": map[string]string{"sha": pr.HeadSHA}})
			}
			_ = json.NewEncoder(w).Encode(body)
		case r.Method == http.MethodGet:
			_ = json.NewEncoder(w).Encode(map[string]string{"sha": branchSHA})
		case r.Method == http.MethodPost:
			var body CommitStatus
			_ = json.NewDecoder(r.Body).Decode(&body)
			*statuses = append(*statuses, postedStatus{SHA: path.Base(r.URL.Path), Status: body})
			w.WriteHeader(http.StatusCreated)
		}
	}))
}

func seedApprovedTestCase(t *testing.T, store planning.Store, projectID, title string) planning.TestCase {
	t.Helper()
	tc, _, err := store.CreateIfAbsent(context.Background(), planning.TestCase{
		ProjectID: projectID, Title: title, Category: planning.CategorySmoke,
		Framework: "playwright", ApprovalStatus: planning.ApprovalApproved, NaturalKey: title,
	})
	if err != nil {
		t.Fatalf("seeding approved test case: %v", err)
	}
	return tc
}

func TestPollOnce_TriggersOnlyApprovedCasesAndReportsSuccess(t *testing.T) {
	var statuses []CommitStatus
	srv := newFakeGitHubServer(t, "sha-1", &statuses)
	defer srv.Close()

	projectStore := projects.NewMemoryStore()
	runStore := runs.NewMemoryStore()
	planningStore := planning.NewMemoryStore()
	secrets := secretstore.NewMemoryStore(testEncryptor(t))

	tokenID, err := secrets.Create(context.Background(), "shh")
	if err != nil {
		t.Fatalf("Create secret: %v", err)
	}
	project, err := projectStore.Create(context.Background(), projects.Project{Name: "acme", Slug: "acme", RepositoryPath: "/tmp/acme"})
	if err != nil {
		t.Fatalf("creating project: %v", err)
	}
	project, err = projectStore.SetGitHubCI(context.Background(), project.ID, "acme/widget", tokenID)
	if err != nil {
		t.Fatalf("SetGitHubCI: %v", err)
	}

	approved := seedApprovedTestCase(t, planningStore, project.ID, "approved case")
	pending, _, err := planningStore.CreateIfAbsent(context.Background(), planning.TestCase{
		ProjectID: project.ID, Title: "pending case", Category: planning.CategorySmoke,
		Framework: "playwright", ApprovalStatus: planning.ApprovalPending, NaturalKey: "pending case",
	})
	if err != nil {
		t.Fatalf("seeding pending test case: %v", err)
	}

	var triggeredIDs []string
	trigger := func(ctx context.Context, testCaseID, triggerType, triggeredBy, commitSHA string) (runs.TestRun, error) {
		triggeredIDs = append(triggeredIDs, testCaseID)
		return immediateTrigger(runStore, nil)(ctx, testCaseID, triggerType, triggeredBy, commitSHA)
	}

	client := NewClient(nil)
	client.BaseURL = srv.URL

	PollOnce(context.Background(), projectStore, runStore, planningStore, secrets, NewMemoryPRTracker(), client, trigger, zerolog.Nop())

	if len(triggeredIDs) != 1 || triggeredIDs[0] != approved.ID {
		t.Fatalf("triggered = %v, want exactly [%s] (pending case %s must not run)", triggeredIDs, approved.ID, pending.ID)
	}

	if len(statuses) != 2 {
		t.Fatalf("posted %d statuses, want 2 (pending, then final)", len(statuses))
	}
	if statuses[0].State != StatusPending {
		t.Errorf("first status = %q, want pending", statuses[0].State)
	}
	if statuses[1].State != StatusSuccess {
		t.Errorf("final status = %q, want success", statuses[1].State)
	}

	updated, err := projectStore.Get(context.Background(), project.ID)
	if err != nil {
		t.Fatalf("Get project: %v", err)
	}
	if updated.LastCICommitSHA != "sha-1" {
		t.Errorf("LastCICommitSHA = %q, want sha-1", updated.LastCICommitSHA)
	}
}

func TestPollOnce_ReportsFailureWhenAnyRunFails(t *testing.T) {
	var statuses []CommitStatus
	srv := newFakeGitHubServer(t, "sha-1", &statuses)
	defer srv.Close()

	projectStore := projects.NewMemoryStore()
	runStore := runs.NewMemoryStore()
	planningStore := planning.NewMemoryStore()
	secrets := secretstore.NewMemoryStore(testEncryptor(t))

	tokenID, _ := secrets.Create(context.Background(), "shh")
	project, _ := projectStore.Create(context.Background(), projects.Project{Name: "acme", Slug: "acme", RepositoryPath: "/tmp/acme"})
	project, _ = projectStore.SetGitHubCI(context.Background(), project.ID, "acme/widget", tokenID)

	passing := seedApprovedTestCase(t, planningStore, project.ID, "passing case")
	failing := seedApprovedTestCase(t, planningStore, project.ID, "failing case")

	trigger := immediateTrigger(runStore, map[string]string{
		passing.ID: runs.StatusPassed,
		failing.ID: runs.StatusFailed,
	})

	client := NewClient(nil)
	client.BaseURL = srv.URL

	PollOnce(context.Background(), projectStore, runStore, planningStore, secrets, NewMemoryPRTracker(), client, trigger, zerolog.Nop())

	if len(statuses) != 2 {
		t.Fatalf("posted %d statuses, want 2", len(statuses))
	}
	if statuses[1].State != StatusFailure {
		t.Errorf("final status = %q, want failure", statuses[1].State)
	}
}

func TestPollOnce_SkipsUnchangedCommit(t *testing.T) {
	var statuses []CommitStatus
	srv := newFakeGitHubServer(t, "sha-1", &statuses)
	defer srv.Close()

	projectStore := projects.NewMemoryStore()
	runStore := runs.NewMemoryStore()
	planningStore := planning.NewMemoryStore()
	secrets := secretstore.NewMemoryStore(testEncryptor(t))

	tokenID, _ := secrets.Create(context.Background(), "shh")
	project, _ := projectStore.Create(context.Background(), projects.Project{Name: "acme", Slug: "acme", RepositoryPath: "/tmp/acme"})
	project, _ = projectStore.SetGitHubCI(context.Background(), project.ID, "acme/widget", tokenID)
	if err := projectStore.SetLastCICommitSHA(context.Background(), project.ID, "sha-1"); err != nil {
		t.Fatalf("SetLastCICommitSHA: %v", err)
	}

	seedApprovedTestCase(t, planningStore, project.ID, "approved case")

	triggerCalls := 0
	trigger := func(ctx context.Context, testCaseID, triggerType, triggeredBy, commitSHA string) (runs.TestRun, error) {
		triggerCalls++
		return immediateTrigger(runStore, nil)(ctx, testCaseID, triggerType, triggeredBy, commitSHA)
	}

	client := NewClient(nil)
	client.BaseURL = srv.URL

	PollOnce(context.Background(), projectStore, runStore, planningStore, secrets, NewMemoryPRTracker(), client, trigger, zerolog.Nop())

	if triggerCalls != 0 {
		t.Errorf("trigger called %d times, want 0 (commit already at last-seen sha)", triggerCalls)
	}
	if len(statuses) != 0 {
		t.Errorf("posted %d statuses, want 0", len(statuses))
	}
}

func TestPollOnce_NoApprovedCasesReportsSuccess(t *testing.T) {
	var statuses []CommitStatus
	srv := newFakeGitHubServer(t, "sha-1", &statuses)
	defer srv.Close()

	projectStore := projects.NewMemoryStore()
	runStore := runs.NewMemoryStore()
	planningStore := planning.NewMemoryStore()
	secrets := secretstore.NewMemoryStore(testEncryptor(t))

	tokenID, _ := secrets.Create(context.Background(), "shh")
	project, _ := projectStore.Create(context.Background(), projects.Project{Name: "acme", Slug: "acme", RepositoryPath: "/tmp/acme"})
	project, _ = projectStore.SetGitHubCI(context.Background(), project.ID, "acme/widget", tokenID)

	trigger := func(context.Context, string, string, string, string) (runs.TestRun, error) {
		t.Fatal("trigger should never be called when there are no approved test cases")
		return runs.TestRun{}, nil
	}

	client := NewClient(nil)
	client.BaseURL = srv.URL

	PollOnce(context.Background(), projectStore, runStore, planningStore, secrets, NewMemoryPRTracker(), client, trigger, zerolog.Nop())

	if len(statuses) != 2 || statuses[1].State != StatusSuccess {
		t.Fatalf("statuses = %+v, want [pending, success]", statuses)
	}
}

func TestPollOnce_TriggersRunAndReportsStatusOnPRHeadSHA(t *testing.T) {
	var statuses []postedStatus
	prs := []PullRequest{{Number: 42, HeadSHA: "pr-sha-1"}}
	srv := newFakeGitHubServerWithPRs(t, "branch-sha-1", prs, &statuses)
	defer srv.Close()

	projectStore := projects.NewMemoryStore()
	runStore := runs.NewMemoryStore()
	planningStore := planning.NewMemoryStore()
	secrets := secretstore.NewMemoryStore(testEncryptor(t))
	prTracker := NewMemoryPRTracker()

	tokenID, err := secrets.Create(context.Background(), "shh")
	if err != nil {
		t.Fatalf("Create secret: %v", err)
	}
	project, err := projectStore.Create(context.Background(), projects.Project{Name: "acme", Slug: "acme", RepositoryPath: "/tmp/acme"})
	if err != nil {
		t.Fatalf("creating project: %v", err)
	}
	project, err = projectStore.SetGitHubCI(context.Background(), project.ID, "acme/widget", tokenID)
	if err != nil {
		t.Fatalf("SetGitHubCI: %v", err)
	}
	// Branch already at latest, so only the open PR should trigger a run.
	if err := projectStore.SetLastCICommitSHA(context.Background(), project.ID, "branch-sha-1"); err != nil {
		t.Fatalf("SetLastCICommitSHA: %v", err)
	}

	seedApprovedTestCase(t, planningStore, project.ID, "approved case")

	var triggeredSHAs []string
	trigger := func(ctx context.Context, testCaseID, triggerType, triggeredBy, commitSHA string) (runs.TestRun, error) {
		triggeredSHAs = append(triggeredSHAs, commitSHA)
		return immediateTrigger(runStore, nil)(ctx, testCaseID, triggerType, triggeredBy, commitSHA)
	}

	client := NewClient(nil)
	client.BaseURL = srv.URL

	PollOnce(context.Background(), projectStore, runStore, planningStore, secrets, prTracker, client, trigger, zerolog.Nop())

	if len(triggeredSHAs) != 1 || triggeredSHAs[0] != "pr-sha-1" {
		t.Fatalf("triggered SHAs = %v, want exactly [pr-sha-1] (branch unchanged, only the PR is new)", triggeredSHAs)
	}

	var prStatuses []CommitStatus
	for _, s := range statuses {
		if s.SHA == "pr-sha-1" {
			prStatuses = append(prStatuses, s.Status)
		}
	}
	if len(prStatuses) != 2 || prStatuses[0].State != StatusPending || prStatuses[1].State != StatusSuccess {
		t.Fatalf("statuses posted to the PR head sha = %+v, want [pending, success]", prStatuses)
	}

	seen, err := prTracker.LastSeenSHA(context.Background(), project.ID, 42)
	if err != nil {
		t.Fatalf("LastSeenSHA: %v", err)
	}
	if seen != "pr-sha-1" {
		t.Errorf("LastSeenSHA = %q, want pr-sha-1 (should be recorded after a successful poll)", seen)
	}
}

func TestPollOnce_SkipsPRWithUnchangedHeadSHA(t *testing.T) {
	var statuses []postedStatus
	prs := []PullRequest{{Number: 42, HeadSHA: "pr-sha-1"}}
	srv := newFakeGitHubServerWithPRs(t, "branch-sha-1", prs, &statuses)
	defer srv.Close()

	projectStore := projects.NewMemoryStore()
	runStore := runs.NewMemoryStore()
	planningStore := planning.NewMemoryStore()
	secrets := secretstore.NewMemoryStore(testEncryptor(t))
	prTracker := NewMemoryPRTracker()

	tokenID, _ := secrets.Create(context.Background(), "shh")
	project, _ := projectStore.Create(context.Background(), projects.Project{Name: "acme", Slug: "acme", RepositoryPath: "/tmp/acme"})
	project, _ = projectStore.SetGitHubCI(context.Background(), project.ID, "acme/widget", tokenID)
	if err := projectStore.SetLastCICommitSHA(context.Background(), project.ID, "branch-sha-1"); err != nil {
		t.Fatalf("SetLastCICommitSHA: %v", err)
	}
	if err := prTracker.SetLastSeenSHA(context.Background(), project.ID, 42, "pr-sha-1"); err != nil {
		t.Fatalf("SetLastSeenSHA: %v", err)
	}

	seedApprovedTestCase(t, planningStore, project.ID, "approved case")

	trigger := func(ctx context.Context, testCaseID, triggerType, triggeredBy, commitSHA string) (runs.TestRun, error) {
		t.Fatalf("trigger should not be called: branch is unchanged and the PR head sha was already seen")
		return runs.TestRun{}, nil
	}

	client := NewClient(nil)
	client.BaseURL = srv.URL

	PollOnce(context.Background(), projectStore, runStore, planningStore, secrets, prTracker, client, trigger, zerolog.Nop())

	if len(statuses) != 0 {
		t.Errorf("posted %d statuses, want 0 (nothing new on either the branch or the PR)", len(statuses))
	}
}

func TestRunLoop_StopsOnContextCancel(t *testing.T) {
	var statuses []CommitStatus
	srv := newFakeGitHubServer(t, "sha-1", &statuses)
	defer srv.Close()

	projectStore := projects.NewMemoryStore()
	runStore := runs.NewMemoryStore()
	planningStore := planning.NewMemoryStore()
	secrets := secretstore.NewMemoryStore(testEncryptor(t))

	client := NewClient(nil)
	client.BaseURL = srv.URL

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		RunLoop(ctx, projectStore, runStore, planningStore, secrets, NewMemoryPRTracker(), client, immediateTrigger(runStore, nil), time.Hour, zerolog.Nop())
		close(done)
	}()

	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("RunLoop did not return after context cancellation")
	}
}
