# GitHub CI integration

`internal/githubci` gives E2E Sentinel a way to react to new code
automatically instead of waiting for someone to click "Run": on an
interval, it checks each configured project's default branch for a new
commit, runs every currently-*approved* test case for that project, and
reports one aggregate status back to the commit on GitHub — the same
green/red check you'd see from a GitHub Actions workflow.

## Why polling, not a webhook

The obvious design is "GitHub sends E2E Sentinel a webhook on push."
This project doesn't do that, on purpose: `sentinel-api` is bound to
`127.0.0.1` by default (see [docs/SECURITY_MODEL.md](SECURITY_MODEL.md)'s
"least-exposure networking" — only `sentinel-web`, port `9090`, is meant
to face anything else), and GitHub's webhook servers, being on the
public internet, can't reach a loopback-bound API. Rather than carve
out a public inbound exception to a deliberate security default, this
integration polls GitHub's REST API instead — outbound-only, the same
shape as `internal/updatecheck` (which polls GitHub Releases). The
trade-off is latency: a new commit is noticed within one poll interval
(`SENTINEL_GITHUB_CI_POLL_SECONDS`, default 90s), not instantly.

## Setting it up

1. Set `SENTINEL_GITHUB_CI_ENABLED=true` and (optionally)
   `SENTINEL_GITHUB_CI_POLL_SECONDS` on `sentinel-api`. This also needs
   `SENTINEL_SECRET_ENCRYPTION_KEY` configured (spec §16.3) — a
   project's GitHub token is stored through the same encrypted
   `internal/secretstore` AI provider API keys use.
2. On the *Projects* page, open a project's "GitHub CI" cell and set:
   - **Repo**: `owner/repo`.
   - **Token**: a GitHub PAT with the `repo:status` scope (classic PAT)
     or, for a public repository only, `public_repo` is enough — this
     is the only permission the integration ever uses. It's write-only
     once saved: the panel only ever shows whether one is configured,
     never the value.
3. Push a commit to that project's default branch. Within one poll
   interval: a `pending` status appears on the commit, every approved
   test case for that project runs, and a `success`/`failure` status
   replaces it once they've all finished.

## What actually runs, and what doesn't

- **Only approved test cases run.** A CI-triggered run goes through the
  exact same `TriggerRun` path a manual `POST /tests/{id}/run` does —
  an unapproved test case is refused identically, and a mutating test
  is still blocked if any of the project's environments is classified
  `production` or `unknown` (spec §2.6). CI triggering changes *when* a
  run starts, never *what's allowed to run*.
- **Every approved test case runs, every time**, for v1 — there's no
  per-test "run in CI" flag yet. For a project with a large suite where
  that's too slow, see "Not yet built" below.
- **Only the default branch is polled.** A pull request's own commits
  aren't checked, and no per-PR status is posted — v1 only reports
  against the branch itself.
- **What GitHub receives back**: a commit status — `state`
  (`pending`/`success`/`failure`), a short `description` (e.g. "3/3
  passed"), and a fixed `context` of `e2e-sentinel`. Never repository
  content, never a run's stdout/stderr/artifacts.

## Not yet built (deliberately, not silently dropped)

- **Per-PR triggering and status.** Polling each open PR's head commit
  (not just the default branch) and posting a status to the PR's own
  commit, so a still-open PR shows its own check instead of only the
  branch it'll eventually land on.
- **GitHub Checks API.** The Commit Status API (what v1 uses) needs
  only a PAT — no GitHub App install, no extra setup. The Checks API is
  richer (per-test annotations grouped under one check run) but needs a
  GitHub App; a natural v2 upgrade once the simpler path has proven
  itself.
- **A per-test "run in CI" flag**, instead of every approved test case
  running on every commit — matters once a suite is large enough that
  running all of it per-commit is too slow or too expensive.
