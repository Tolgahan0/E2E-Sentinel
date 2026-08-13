package httpserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"e2e-sentinel/apps/api/internal/audit"
	"e2e-sentinel/apps/api/internal/bugreports"
	"e2e-sentinel/apps/api/internal/fixproposals"
	"e2e-sentinel/apps/api/internal/projects"
	"e2e-sentinel/apps/api/internal/providers"
	"e2e-sentinel/apps/api/internal/redaction"
	"e2e-sentinel/apps/api/internal/webhooks"
)

type fileResultResponse struct {
	Path    string `json:"path"`
	Action  string `json:"action"`
	Applied bool   `json:"applied"`
	Error   string `json:"error,omitempty"`
}

func toFileResultResponses(results []fixproposals.FileResult) []fileResultResponse {
	out := make([]fileResultResponse, 0, len(results))
	for _, r := range results {
		out = append(out, fileResultResponse{Path: r.Path, Action: r.Action, Applied: r.Applied, Error: r.Error})
	}
	return out
}

type fixProposalResponse struct {
	ID          string `json:"id"`
	ProjectID   string `json:"project_id"`
	BugID       string `json:"bug_id"`
	Title       string `json:"title"`
	Description string `json:"description"`
	RiskLevel   string `json:"risk_level"`

	Assumptions          string `json:"assumptions"`
	PotentialSideEffects string `json:"potential_side_effects"`
	RollbackGuidance     string `json:"rollback_guidance"`

	FilesChanged []string `json:"files_changed"`
	UnifiedDiff  string   `json:"unified_diff"`

	RegressionTestIDs []string `json:"regression_test_ids"`

	AIProvider  string `json:"ai_provider"`
	AIModel     string `json:"ai_model"`
	GeneratedAt string `json:"generated_at"`

	ApprovalStatus string `json:"approval_status"`

	WorkspaceDir           string               `json:"workspace_dir,omitempty"`
	WorkspaceApplyResults  []fileResultResponse `json:"workspace_apply_results,omitempty"`
	WorkspaceAppliedAt     *string              `json:"workspace_applied_at"`
	RepositoryApplyResults []fileResultResponse `json:"repository_apply_results,omitempty"`
	RepositoryAppliedAt    *string              `json:"repository_applied_at"`
}

func toFixProposalResponse(fp fixproposals.FixProposal) fixProposalResponse {
	var workspaceAppliedAt, repositoryAppliedAt *string
	if fp.WorkspaceAppliedAt != nil {
		s := fp.WorkspaceAppliedAt.Format(timeFormat)
		workspaceAppliedAt = &s
	}
	if fp.RepositoryAppliedAt != nil {
		s := fp.RepositoryAppliedAt.Format(timeFormat)
		repositoryAppliedAt = &s
	}
	filesChanged := fp.FilesChanged
	if filesChanged == nil {
		filesChanged = []string{}
	}
	regressionTestIDs := fp.RegressionTestIDs
	if regressionTestIDs == nil {
		regressionTestIDs = []string{}
	}

	return fixProposalResponse{
		ID: fp.ID, ProjectID: fp.ProjectID, BugID: fp.BugID, Title: fp.Title, Description: fp.Description,
		RiskLevel: fp.RiskLevel, Assumptions: fp.Assumptions, PotentialSideEffects: fp.PotentialSideEffects,
		RollbackGuidance: fp.RollbackGuidance, FilesChanged: filesChanged, UnifiedDiff: fp.UnifiedDiff,
		RegressionTestIDs: regressionTestIDs, AIProvider: fp.AIProvider, AIModel: fp.AIModel,
		GeneratedAt: fp.GeneratedAt.Format(timeFormat), ApprovalStatus: fp.ApprovalStatus,
		WorkspaceDir: fp.WorkspaceDir, WorkspaceApplyResults: toFileResultResponses(fp.WorkspaceApplyResults),
		WorkspaceAppliedAt: workspaceAppliedAt, RepositoryApplyResults: toFileResultResponses(fp.RepositoryApplyResults),
		RepositoryAppliedAt: repositoryAppliedAt,
	}
}

// handleGenerateFixProposal creates a fix proposal for a bug, either from
// a manually-supplied unified_diff (a human/"Developer" role author,
// spec §17.5) or by asking the provider routed for the fix_generation
// task (spec §16.4). The AI path never reads repository source files —
// only the bug's already-curated evidence — since building a safe
// repository-content-to-AI pipeline (redaction, path allowlist) is out
// of scope for this phase; see docs/FIX_PROPOSALS.md. Either way, the
// AI itself never writes anything — this only ever stores a diff for
// human review (spec §3.3 "It must not apply patches").
func handleGenerateFixProposal(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		bugID := chi.URLParam(r, "bugID")
		bug, err := deps.Bugs.Get(r.Context(), bugID)
		if errors.Is(err, bugreports.ErrNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "bug_not_found"})
			return
		}
		if err != nil {
			deps.Logger.Error().Err(err).Msg("getting bug for fix proposal failed")
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal_error"})
			return
		}

		var body struct {
			UnifiedDiff string `json:"unified_diff"`
			Title       string `json:"title"`
			Description string `json:"description"`
			RiskLevel   string `json:"risk_level"`
		}
		if r.ContentLength != 0 {
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_json"})
				return
			}
		}

		var fp fixproposals.FixProposal
		if body.UnifiedDiff != "" {
			fp, err = buildManualProposal(bug, body.UnifiedDiff, body.Title, body.Description, body.RiskLevel)
		} else {
			fp, err = generateProposalViaAI(r.Context(), deps, bug)
		}
		if err != nil {
			status := http.StatusUnprocessableEntity
			if errors.Is(err, errFixGenerationNotConfigured) {
				status = http.StatusServiceUnavailable
			}
			writeJSON(w, status, map[string]string{"error": "fix_proposal_generation_failed", "detail": err.Error()})
			return
		}

		created, err := deps.FixProposals.Create(r.Context(), fp)
		if err != nil {
			deps.Logger.Error().Err(err).Msg("creating fix proposal failed")
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal_error"})
			return
		}

		if err := deps.Audit.Record(r.Context(), audit.Event{
			ActionType: "fix_proposal.created", ResourceType: "fix_proposal", ResourceID: created.ID,
			Actor: "user", Metadata: map[string]any{"bug_id": bugID, "ai_provider": created.AIProvider, "risk_level": created.RiskLevel},
		}); err != nil {
			deps.Logger.Error().Err(err).Msg("recording fix_proposal.created audit event failed")
		}

		// Every fix proposal starts pending_review (never auto-approved,
		// spec §15) — this is always a genuinely new thing waiting on a
		// human decision, unlike bug reports which can be a repeat.
		notifyAsync(deps, webhooks.Event{
			Type: webhooks.EventFixProposalPendingReview, ProjectID: created.ProjectID, ResourceType: "fix_proposal",
			ResourceID: created.ID, Title: created.Title, Severity: created.RiskLevel, OccurredAt: time.Now(),
		})

		writeJSON(w, http.StatusCreated, toFixProposalResponse(created))
	}
}

func buildManualProposal(bug bugreports.BugReport, unifiedDiff, title, description, riskLevel string) (fixproposals.FixProposal, error) {
	changes, err := fixproposals.ParseUnifiedDiff(unifiedDiff)
	if err != nil {
		return fixproposals.FixProposal{}, err
	}
	if title == "" {
		title = "Manual fix for: " + bug.Title
	}
	if riskLevel == "" {
		riskLevel = fixproposals.RiskMedium
	}
	return fixproposals.FixProposal{
		ProjectID: bug.ProjectID, BugID: bug.ID, Title: title, Description: description, RiskLevel: riskLevel,
		FilesChanged: fixproposals.FilesChanged(changes), UnifiedDiff: unifiedDiff,
		RegressionTestIDs: []string{bug.TestCaseID}, GeneratedAt: time.Now(),
	}, nil
}

var errFixGenerationNotConfigured = errors.New("no provider is routed for the fix_generation task")

func generateProposalViaAI(ctx context.Context, deps Dependencies, bug bugreports.BugReport) (fixproposals.FixProposal, error) {
	routes, err := loadTaskRouting(ctx, deps)
	if err != nil {
		return fixproposals.FixProposal{}, fmt.Errorf("loading task routing: %w", err)
	}
	providerID := routes[providers.TaskFixGeneration]
	if providerID == "" {
		return fixproposals.FixProposal{}, errFixGenerationNotConfigured
	}

	provider, err := deps.Providers.Get(ctx, providerID)
	if err != nil {
		return fixproposals.FixProposal{}, fmt.Errorf("loading routed provider: %w", err)
	}
	if !provider.Enabled {
		return fixproposals.FixProposal{}, fmt.Errorf("the provider routed for fix_generation is disabled")
	}

	var apiKey string
	if provider.SecretReferenceID != "" {
		if deps.Secrets == nil {
			return fixproposals.FixProposal{}, fmt.Errorf("secret encryption is not configured")
		}
		apiKey, err = deps.Secrets.Resolve(ctx, provider.SecretReferenceID)
		if err != nil {
			return fixproposals.FixProposal{}, fmt.Errorf("resolving provider API key: %w", err)
		}
	}

	sourceContext := gatherSourceContext(ctx, deps, bug)

	result, err := deps.Completer.Complete(ctx, provider, apiKey, providers.CompletionRequest{
		SystemPrompt: fixGenerationSystemPrompt,
		UserPrompt:   buildFixGenerationPrompt(bug, sourceContext),
		MaxTokens:    2048,
	})
	if err != nil {
		deps.Metrics.AIRequestsTotal.Inc(map[string]string{"provider_type": provider.Type, "outcome": "error"})
		return fixproposals.FixProposal{}, fmt.Errorf("provider request failed: %w", err)
	}
	deps.Metrics.AIRequestsTotal.Inc(map[string]string{"provider_type": provider.Type, "outcome": "ok"})

	diff, err := providers.ExtractUnifiedDiff(result.Text)
	if err != nil {
		return fixproposals.FixProposal{}, fmt.Errorf("provider response contained no usable diff: %w", err)
	}
	changes, err := fixproposals.ParseUnifiedDiff(diff)
	if err != nil {
		return fixproposals.FixProposal{}, fmt.Errorf("provider produced an invalid diff: %w", err)
	}

	description := "Generated from bug evidence only (no repository source was read) — review carefully before applying."
	assumptions := "The proposed change is based solely on the bug's captured evidence, not the actual repository source; it may not match the real file layout or content."
	if sourceContext != "" {
		description = "Generated from bug evidence plus the affected route/service's actual source file(s) — review carefully before applying."
		assumptions = "The proposed change is based on the bug's captured evidence plus the affected route/service's real source content (redacted for secrets); it may still not reflect the full surrounding context."
	}

	return fixproposals.FixProposal{
		ProjectID: bug.ProjectID, BugID: bug.ID, Title: "AI-proposed fix for: " + bug.Title,
		Description:  description,
		RiskLevel:    fixproposals.RiskMedium,
		Assumptions:  assumptions,
		FilesChanged: fixproposals.FilesChanged(changes), UnifiedDiff: diff,
		RegressionTestIDs: []string{bug.TestCaseID},
		AIProvider:        provider.Type, AIModel: provider.Model, GeneratedAt: time.Now(),
	}, nil
}

const fixGenerationSystemPrompt = `You are an expert software engineer helping fix a bug found by an automated end-to-end test. You have the evidence below, plus — only if included — relevant source file excerpt(s) that the platform's dependency graph identified as implementing the affected route or service (already redacted for secrets/tokens/credentials). If no source excerpt is included below, you have no access to the repository's source code at all. Propose a best-effort unified diff patch. Respond with ONLY a single fenced code block starting with ` + "```diff" + ` containing a valid unified diff (---/+++ file headers, @@ hunk headers). If you cannot propose a concrete patch without seeing the source code, respond with a fenced code block containing only the line "# insufficient information to generate a patch" and nothing else.`

func buildFixGenerationPrompt(bug bugreports.BugReport, sourceContext string) string {
	prompt := fmt.Sprintf(
		"Title: %s\nFailure type: %s\nSeverity: %s\nAffected route: %s\nAffected service: %s\nExpected: %s\nActual: %s\nError message: %s\nRoot cause hypothesis (unverified): %s\n",
		bug.Title, bug.FailureType, bug.Severity, bug.AffectedRoute, bug.AffectedService,
		bug.ExpectedResult, bug.ActualResult, bug.Evidence.ErrorMessage, bug.RootCauseHypothesis,
	)
	if sourceContext != "" {
		prompt += "\nRelevant source file(s) (redacted for secrets):\n" + sourceContext
	}
	return prompt
}

// sourceContextMaxFiles bounds how many source files are ever read for
// one fix proposal: the affected route's own file and, if different,
// its serving service's file — never more, regardless of how many
// Application Graph nodes happen to share a label.
const sourceContextMaxFiles = 2

// gatherSourceContext best-effort loads the source file(s) the
// Application Graph already identifies as implementing bug's affected
// route/service (internal/graph.Node.SourceReference, populated at
// discovery time from real route/service extraction — never a guess),
// redacts them, and renders them as labeled blocks for the AI prompt.
// Returns "" (never an error) if the graph has no match, the project
// can't be read, or every candidate file is missing/too large — the
// AI-assisted path already has a well-tested no-context fallback (the
// bug-evidence-only prompt), so any failure here just silently falls
// back to it rather than blocking fix-proposal generation.
func gatherSourceContext(ctx context.Context, deps Dependencies, bug bugreports.BugReport) string {
	project, err := deps.Projects.Get(ctx, bug.ProjectID)
	if err != nil {
		return ""
	}
	nodes, _, err := deps.Graph.Get(ctx, bug.ProjectID)
	if err != nil {
		return ""
	}

	var relPaths []string
	seen := map[string]bool{}
	for _, label := range []string{bug.AffectedRoute, bug.AffectedService} {
		if label == "" || len(relPaths) >= sourceContextMaxFiles {
			continue
		}
		for _, n := range nodes {
			if n.Label == label && n.SourceReference != "" && !seen[n.SourceReference] {
				seen[n.SourceReference] = true
				relPaths = append(relPaths, n.SourceReference)
				break
			}
		}
	}

	var blocks []string
	for _, relPath := range relPaths {
		full := filepath.Join(project.RepositoryPath, relPath)
		if !projects.WithinRoot(project.RepositoryPath, full) {
			continue // never trust a join blindly, even though SourceReference is graph-derived, not user input
		}
		info, err := os.Stat(full)
		if err != nil || info.IsDir() || !redaction.WithinSizeLimit(info.Size(), 0) {
			continue
		}
		raw, err := os.ReadFile(full)
		if err != nil {
			continue
		}
		result := redaction.Redact(string(raw))
		if len(result.Categories) > 0 {
			deps.Logger.Info().Str("bug_id", bug.ID).Str("path", relPath).Any("categories", result.Categories).
				Msg("fix generation: redacted content before sending to AI provider")
		}
		blocks = append(blocks, fmt.Sprintf("--- %s ---\n%s", relPath, result.Text))
	}
	if len(blocks) == 0 {
		return ""
	}
	return strings.Join(blocks, "\n\n")
}

func handleGetFixProposal(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		fp, err := deps.FixProposals.Get(r.Context(), chi.URLParam(r, "fixProposalID"))
		if errors.Is(err, fixproposals.ErrNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "fix_proposal_not_found"})
			return
		}
		if err != nil {
			deps.Logger.Error().Err(err).Msg("getting fix proposal failed")
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal_error"})
			return
		}
		writeJSON(w, http.StatusOK, toFixProposalResponse(fp))
	}
}

func handleListProjectFixProposals(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		list, err := deps.FixProposals.ListByProject(r.Context(), chi.URLParam(r, "projectID"))
		if err != nil {
			deps.Logger.Error().Err(err).Msg("listing fix proposals failed")
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal_error"})
			return
		}
		out := make([]fixProposalResponse, 0, len(list))
		for _, fp := range list {
			out = append(out, toFixProposalResponse(fp))
		}
		writeJSON(w, http.StatusOK, map[string]any{"fix_proposals": out})
	}
}

func handleSetFixProposalStatus(deps Dependencies, status, actionType string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "fixProposalID")
		fp, err := deps.FixProposals.UpdateApprovalStatus(r.Context(), id, status)
		if errors.Is(err, fixproposals.ErrNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "fix_proposal_not_found"})
			return
		}
		if err != nil {
			deps.Logger.Error().Err(err).Msg("updating fix proposal status failed")
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal_error"})
			return
		}
		if err := deps.Audit.Record(r.Context(), audit.Event{
			ActionType: actionType, ResourceType: "fix_proposal", ResourceID: id, Actor: "user",
		}); err != nil {
			deps.Logger.Error().Err(err).Msg("recording fix proposal status audit event failed")
		}
		writeJSON(w, http.StatusOK, toFixProposalResponse(fp))
	}
}

func handleApproveFixProposal(deps Dependencies) http.HandlerFunc {
	return handleSetFixProposalStatus(deps, fixproposals.StatusApproved, "fix_proposal.approved")
}

func handleRejectFixProposal(deps Dependencies) http.HandlerFunc {
	return handleSetFixProposalStatus(deps, fixproposals.StatusRejected, "fix_proposal.rejected")
}

func handleRequestFixProposalRevision(deps Dependencies) http.HandlerFunc {
	return handleSetFixProposalStatus(deps, fixproposals.StatusRevisionRequested, "fix_proposal.revision_requested")
}

func handleUpdateFixProposalRegressionTests(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			RegressionTestIDs []string `json:"regression_test_ids"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_json"})
			return
		}
		id := chi.URLParam(r, "fixProposalID")
		fp, err := deps.FixProposals.UpdateRegressionTestIDs(r.Context(), id, body.RegressionTestIDs)
		if errors.Is(err, fixproposals.ErrNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "fix_proposal_not_found"})
			return
		}
		if err != nil {
			deps.Logger.Error().Err(err).Msg("updating fix proposal regression tests failed")
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal_error"})
			return
		}
		writeJSON(w, http.StatusOK, toFixProposalResponse(fp))
	}
}

// handleApplyFixToWorkspace applies the proposal's diff to a disposable
// COPY of the repository (spec §15.2) — the original repository_path is
// never touched here. This is the step that lets a reviewer see whether
// a patch even applies cleanly before requesting final approval.
func handleApplyFixToWorkspace(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "fixProposalID")
		fp, err := deps.FixProposals.Get(r.Context(), id)
		if errors.Is(err, fixproposals.ErrNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "fix_proposal_not_found"})
			return
		}
		if err != nil {
			deps.Logger.Error().Err(err).Msg("getting fix proposal failed")
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal_error"})
			return
		}

		project, err := deps.Projects.Get(r.Context(), fp.ProjectID)
		if err != nil {
			deps.Logger.Error().Err(err).Msg("getting project for workspace application failed")
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal_error"})
			return
		}

		workspaceDir, results, applyErr := fixproposals.ApplyToWorkspace(project.RepositoryPath, fp.UnifiedDiff, deps.FixWorkspacesDir)
		if results == nil {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": "workspace_application_failed", "detail": applyErr.Error()})
			return
		}

		updated, err := deps.FixProposals.RecordWorkspaceApplication(r.Context(), id, workspaceDir, results, time.Now())
		if err != nil {
			deps.Logger.Error().Err(err).Msg("recording workspace application failed")
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal_error"})
			return
		}

		allApplied := applyErr == nil
		if err := deps.Audit.Record(r.Context(), audit.Event{
			ActionType: "fix_proposal.applied_workspace", ResourceType: "fix_proposal", ResourceID: id,
			Actor: "user", Metadata: map[string]any{"all_applied": allApplied, "files": fp.FilesChanged},
		}); err != nil {
			deps.Logger.Error().Err(err).Msg("recording fix_proposal.applied_workspace audit event failed")
		}

		writeJSON(w, http.StatusOK, map[string]any{"fix_proposal": toFixProposalResponse(updated), "all_applied": allApplied})
	}
}

// handleApplyFixToRepository is the one HTTP path that writes to a
// target repository — gated on explicit prior approval (spec §3.4,
// §15.2 acceptance: "Final repository write requires explicit
// approval") and on never having been applied before (spec §15.2's flow
// is one-shot: approve once, apply once). It re-parses the SAME
// UnifiedDiff that was approved — never a regenerated one — so the
// applied files are guaranteed to match exactly what was reviewed (spec
// acceptance: "Applied files match approved diff exactly").
func handleApplyFixToRepository(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "fixProposalID")
		fp, err := deps.FixProposals.Get(r.Context(), id)
		if errors.Is(err, fixproposals.ErrNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "fix_proposal_not_found"})
			return
		}
		if err != nil {
			deps.Logger.Error().Err(err).Msg("getting fix proposal failed")
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal_error"})
			return
		}
		if fp.ApprovalStatus != fixproposals.StatusApproved {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "fix_proposal_not_approved", "detail": "approve the proposal before applying it to the repository"})
			return
		}
		if fp.RepositoryAppliedAt != nil {
			// Checked up front, before re-attempting the diff: retrying
			// ApplyToRepository against an already-patched file would
			// legitimately fail with a context mismatch (the "before"
			// lines are gone), which must read as "already applied", not
			// as a new, retryable failure.
			writeJSON(w, http.StatusConflict, map[string]string{"error": "already_applied_to_repository"})
			return
		}

		project, err := deps.Projects.Get(r.Context(), fp.ProjectID)
		if err != nil {
			deps.Logger.Error().Err(err).Msg("getting project for repository application failed")
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal_error"})
			return
		}

		results, applyErr := fixproposals.ApplyToRepository(project.RepositoryPath, fp.UnifiedDiff)
		if results == nil {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": "repository_application_failed", "detail": applyErr.Error()})
			return
		}
		if applyErr != nil {
			// A failure here (e.g. the repository is mounted read-only)
			// must NOT consume the one-shot application slot — the
			// operator can fix the underlying issue (e.g. remount
			// read-write) and retry. Only a fully clean application
			// below is ever recorded as "applied".
			deps.Logger.Warn().Err(applyErr).Str("fix_proposal_id", id).Msg("repository application had per-file failures; not recording as applied")
			writeJSON(w, http.StatusUnprocessableEntity, map[string]any{
				"error": "repository_application_failed", "detail": applyErr.Error(),
				"results": toFileResultResponses(results),
			})
			return
		}

		updated, err := deps.FixProposals.RecordRepositoryApplication(r.Context(), id, results, time.Now())
		if errors.Is(err, fixproposals.ErrAlreadyAppliedToRepository) {
			writeJSON(w, http.StatusConflict, map[string]string{"error": "already_applied_to_repository"})
			return
		}
		if err != nil {
			deps.Logger.Error().Err(err).Msg("recording repository application failed")
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal_error"})
			return
		}

		if err := deps.Audit.Record(r.Context(), audit.Event{
			ActionType: "fix_proposal.applied_repository", ResourceType: "fix_proposal", ResourceID: id,
			Actor: "user", Metadata: map[string]any{"files": fp.FilesChanged, "project_id": fp.ProjectID},
		}); err != nil {
			deps.Logger.Error().Err(err).Msg("recording fix_proposal.applied_repository audit event failed")
		}

		writeJSON(w, http.StatusOK, map[string]any{"fix_proposal": toFixProposalResponse(updated), "all_applied": true})
	}
}
