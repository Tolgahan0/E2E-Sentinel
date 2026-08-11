-- GitHub CI v2 (internal/githubci): per-PR triggering. A project row
-- can only hold one "last seen" commit SHA (last_ci_commit_sha), which
-- is enough for polling a single default branch but not for polling
-- every open pull request at once — each PR needs its own last-seen
-- head SHA, tracked here.
--
-- A closed/merged PR simply stops appearing in Client.OpenPullRequests
-- and its row here goes stale and unused; nothing prunes it in v1
-- (documented limitation, see docs/GITHUB_CI.md).

CREATE TABLE github_ci_pull_requests (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id    UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    pr_number     INTEGER NOT NULL,
    last_head_sha TEXT NOT NULL DEFAULT '',
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (project_id, pr_number)
);

CREATE INDEX idx_github_ci_pull_requests_project ON github_ci_pull_requests (project_id);
