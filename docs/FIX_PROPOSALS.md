# Fix Proposals

A fix proposal (spec §6.9, §15) is a candidate unified diff for a bug
report — reviewed, tested in a disposable temporary workspace, and only
ever written to the real repository after an explicit human approval.
The AI itself is never able to write to a repository (spec §3.3): the
completion client (`internal/providers.Completer`) only returns text,
which this feature stores as a proposal for review — no code path
connects a provider response directly to a filesystem write.

## Generating a proposal

`POST /bugs/{id}/fix-proposal`:

- **Manual** — pass `{"unified_diff": "..."}` directly. This is the
  primary, fully-reliable path: a developer (or any tool) supplies a
  real diff against the actual repository content.
- **AI-assisted** — omit `unified_diff`. The task type `fix_generation`
  must be routed to an enabled provider (`PATCH /providers/routing`,
  spec §16.4); the request 503s otherwise.

### The AI path can now read the affected route/service's real source

The AI-assisted path always prompts the routed provider with the bug's
already-curated evidence — title, failure type, severity, affected
route/service, expected/actual result, error message, and the
(explicitly labeled hypothesis) root cause. On top of that, it now
best-effort includes the actual source of the file(s) the Application
Graph (Phase 3) already identifies as implementing the affected route
or service: `graph.Node.SourceReference`, set at discovery time from
real route/service extraction, never a guess. `bug.AffectedRoute` and
`bug.AffectedService` are matched against the project's current graph
node labels; up to two files (the route's own file, and its serving
service's file if different) are read, run through
`internal/redaction.Redact` (secrets/tokens/credentials/auth headers/
cookies replaced with a fixed placeholder — spec §16.5), and appended to
the prompt as labeled, redacted blocks. `internal/redaction.WithinSizeLimit`
excludes any file over 200 KB rather than truncating it silently. Every
file path is re-verified with `projects.WithinRoot` against the
project's validated repository root before it's ever read — the same
containment check every other filesystem-touching feature in this
codebase already uses (`fixproposals/apply.go`, `fixproposals/workspace.go`)
— even though `SourceReference` comes from the graph, not user input.

None of this is required to succeed: no graph match, no readable file,
or a file over the size limit all silently fall back to the
evidence-only prompt — the same one this path has always sent — rather
than blocking generation or returning an error. The model is asked to
respond with only a fenced ` ```diff ` block; if it can't propose
something concrete, it's instructed to say so, and that response fails
to parse as a diff — surfaced as a clear error rather than a fabricated
patch. The returned proposal's `description`/`assumptions` fields
honestly say whether real source was actually included this time, so a
reviewer never has to guess.

Practically: an AI-generated proposal is still a **best-effort sketch**,
especially when no source was available to include. The
temporary-workspace step exists precisely to catch a patch that doesn't
even apply before anyone wastes time reviewing it.

## Diff parsing and application — no shell-out

`internal/fixproposals` implements a small, pure-Go unified diff
parser (`ParseUnifiedDiff`) and applier (`ApplyFileChange`) rather than
shelling out to `git apply`/`patch` — consistent with this project's
established preference for parsing over subprocess invocation (spec
§23.3, first applied to Compose file parsing in Phase 2) and avoiding a
new command-execution surface for a diff body that may come from an AI
provider. It supports the common subset: `---`/`+++` file headers
(`a/`/`b/` prefixes and `/dev/null` for new/deleted files), `@@
-l,s +l,s @@` hunks, and context/add/remove lines. Every context and
removed line is verified against the actual file content before
anything is written — a mismatch (the file has drifted, or the diff is
wrong) fails the whole file with `ErrPatchDoesNotApply` rather than
writing a corrupted result.

**Path traversal is checked before every write.** A diff's file path is
attacker-controlled data (it could come from an AI provider or a
compromised project) — `projects.WithinRoot` (the same containment
check discovery has used since Phase 1) rejects any path that would
resolve outside the target directory, for both the workspace and the
real repository.

## Apply in temporary workspace

`POST /fix-proposals/{id}/apply-workspace` copies the project's
`repository_path` into a **disposable directory** under
`SENTINEL_FIX_WORKSPACES_DIR` (`.git`, `node_modules`, `vendor`,
`.next`, `dist`, `build` are skipped — irrelevant to testing whether a
source-level patch applies) and applies the diff there. The original
repository is never touched by this step. Per-file results
(`created`/`modified`/`deleted`, `applied`, and an error message on
failure) are returned and stored on the proposal.

**Known limitation, documented rather than faked:** spec §15.2's
recommended flow says to run regression tests against this patched
workspace. Actually standing up a live instance of an arbitrary target
repository from source (installing dependencies, building, running its
services) is a much larger undertaking than this phase — E2E Sentinel
doesn't own or manage the target application's build/deploy lifecycle.
Regression tests selected on a fix proposal (`PATCH
/fix-proposals/{id}` with `regression_test_ids`) are run the same way
any other test is (Phase 5's Runs page), against the environment's
currently configured `base_url` — the same live target already used for
every other test run, not an ephemeral deployment of the temporary
workspace. This is a real trade-off: it verifies the *currently deployed*
code's behavior, not the *patched* code's behavior, until whoever
operates the target deploys the change themselves.

## Approval workflow

```text
generate (AI or manual)     -> status: pending_review
review diff / risk / files
apply-workspace (as many times as useful, e.g. after tweaking the diff)
approve | reject | request-revision
apply-repository            -> only if status == approved, only once
```

`POST /fix-proposals/{id}/approve|reject|request-revision` set status;
none of them touch any file. `POST /fix-proposals/{id}/apply-repository`
is the one place E2E Sentinel ever writes to a target repository:

- Refuses (403) unless `approval_status == approved`.
- Refuses (409) if already applied once — `repository_applied_at` is
  set exactly once, atomically, by the store.
- Re-parses the **same stored `unified_diff`** the approval was granted
  for — never a regenerated one — so applied files are guaranteed to
  match the approved diff exactly (spec §15.2 acceptance criterion).

### The target repository must be writable

`docker-compose.yml` mounts `./workspace` **read-only** by default —
every feature through Phase 7 only ever reads it. `apply-repository`
needs to write, so it will fail with a clear permission error against
the default mount. To use it, change that mount to `:rw` yourself
(see the comment next to it in `docker-compose.yml`) — this is an
explicit, documented opt-in, not a default, consistent with how
`SENTINEL_RUNNER_HOST_WORKSPACE_DIR` and
`SENTINEL_SECRET_ENCRYPTION_KEY` are also capabilities you turn on
deliberately rather than getting by default.

## Monaco Editor is deferred

Spec §17.1 calls for a Monaco Editor diff view. The web Fix Proposals
page instead renders a plain, colored (+green/-red) `<pre>` diff viewer —
fully readable, but not a rich code-editor component. Pulling in Monaco
(a multi-megabyte dependency needing its own webpack/dynamic-import
setup) for review-only display was judged not worth the engineering cost
at this stage, the same kind of call already made for Phase 3's
Application Map (a zoomable graph canvas, also deferred in favor of a
list-based UI).

## API

```text
POST   /bugs/{id}/fix-proposal
GET    /fix-proposals/{id}
GET    /projects/{id}/fix-proposals
PATCH  /fix-proposals/{id}                 {regression_test_ids}
POST   /fix-proposals/{id}/approve
POST   /fix-proposals/{id}/reject
POST   /fix-proposals/{id}/request-revision
POST   /fix-proposals/{id}/apply-workspace
POST   /fix-proposals/{id}/apply-repository
```
