-- GitHub CI integration: poll a project's default branch for new
-- commits, run its approved test cases, report a commit status back.
-- Outbound-only (internal/githubci) — no inbound webhook, no change to
-- sentinel-api's network exposure (see docs/SECURITY_MODEL.md).

ALTER TABLE projects ADD COLUMN github_repo TEXT NOT NULL DEFAULT '';
ALTER TABLE projects ADD COLUMN github_token_secret_reference_id TEXT;
ALTER TABLE projects ADD COLUMN last_ci_commit_sha TEXT NOT NULL DEFAULT '';

ALTER TABLE test_runs ADD COLUMN commit_sha TEXT NOT NULL DEFAULT '';
