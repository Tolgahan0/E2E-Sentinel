-- Visual regression testing (internal/visualdiff): a full-page
-- screenshot from every browser-based run, diffed against a stored
-- baseline. Never gates pass/fail on its own — a visual change may be
-- intentional, so it's surfaced for a human to accept or ignore.

CREATE TABLE visual_baselines (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    test_case_id UUID NOT NULL UNIQUE REFERENCES test_cases(id) ON DELETE CASCADE,
    artifact_id  UUID NOT NULL REFERENCES artifacts(id) ON DELETE CASCADE,
    accepted_by  TEXT NOT NULL DEFAULT 'system',
    accepted_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE visual_diffs (
    id                   UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    -- Denormalized from test_runs.project_id, same precedent as
    -- test_runs itself carrying project_id alongside test_case_id —
    -- lets ListByProject filter directly instead of joining through
    -- test_cases.
    project_id           UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    test_run_id          UUID NOT NULL REFERENCES test_runs(id) ON DELETE CASCADE,
    test_case_id         UUID NOT NULL REFERENCES test_cases(id) ON DELETE CASCADE,
    baseline_artifact_id UUID NOT NULL REFERENCES artifacts(id) ON DELETE CASCADE,
    current_artifact_id  UUID NOT NULL REFERENCES artifacts(id) ON DELETE CASCADE,
    diff_artifact_id     UUID NOT NULL REFERENCES artifacts(id) ON DELETE CASCADE,
    percent_changed      DOUBLE PRECISION NOT NULL,
    status               TEXT NOT NULL DEFAULT 'pending_review'
        CHECK (status IN ('pending_review', 'accepted', 'ignored')),
    reviewed_by          TEXT,
    reviewed_at          TIMESTAMPTZ,
    created_at           TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_visual_diffs_test_case ON visual_diffs (test_case_id, status, created_at DESC);
CREATE INDEX idx_visual_diffs_project ON visual_diffs (project_id, status, created_at DESC);
