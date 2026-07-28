-- Phase 5: Playwright Runner.

ALTER TABLE test_cases ADD COLUMN route_path TEXT NOT NULL DEFAULT '';
ALTER TABLE test_cases ADD COLUMN route_method TEXT NOT NULL DEFAULT '';

CREATE TABLE test_runs (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id     UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    test_case_id   UUID NOT NULL REFERENCES test_cases(id) ON DELETE CASCADE,
    status         TEXT NOT NULL DEFAULT 'queued'
        CHECK (status IN ('queued', 'running', 'passed', 'failed', 'cancelled', 'error')),
    runner_type    TEXT NOT NULL DEFAULT 'playwright-docker',
    trigger_type   TEXT NOT NULL DEFAULT 'manual',
    triggered_by   TEXT NOT NULL DEFAULT 'user',
    exit_code      INTEGER,
    summary        TEXT NOT NULL DEFAULT '',
    started_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    finished_at    TIMESTAMPTZ,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_test_runs_project ON test_runs (project_id, started_at DESC);
CREATE INDEX idx_test_runs_test_case ON test_runs (test_case_id);

CREATE TABLE artifacts (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    test_run_id   UUID NOT NULL REFERENCES test_runs(id) ON DELETE CASCADE,
    kind          TEXT NOT NULL, -- stdout | stderr | screenshot | video | trace | har
    mime_type     TEXT NOT NULL,
    size_bytes    BIGINT NOT NULL,
    checksum      TEXT NOT NULL, -- sha256 hex
    storage_path  TEXT NOT NULL, -- path on the artifact filesystem
    retention_until TIMESTAMPTZ,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_artifacts_test_run ON artifacts (test_run_id);
