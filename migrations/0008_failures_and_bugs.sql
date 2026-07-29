-- Phase 7: Failure Analysis and Bug Reports.

CREATE TABLE failures (
    id                    UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    test_run_id           UUID NOT NULL REFERENCES test_runs(id) ON DELETE CASCADE,
    test_case_id          UUID NOT NULL REFERENCES test_cases(id) ON DELETE CASCADE,
    title                 TEXT NOT NULL,
    severity              TEXT NOT NULL CHECK (severity IN ('critical', 'high', 'medium', 'low', 'informational')),
    failure_type          TEXT NOT NULL,
    expected              TEXT NOT NULL DEFAULT '',
    actual                TEXT NOT NULL DEFAULT '',
    error_message         TEXT NOT NULL DEFAULT '',
    stack_trace           TEXT NOT NULL DEFAULT '',
    -- root_cause_hypothesis is never a confirmed fact — always rendered
    -- with an explicit "hypothesis" label by internal/bugreports.
    root_cause_hypothesis TEXT NOT NULL DEFAULT '',
    confidence_score      TEXT NOT NULL DEFAULT 'low' CHECK (confidence_score IN ('medium', 'low')),
    evidence              JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at            TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_failures_test_case ON failures (test_case_id);

CREATE TABLE bug_reports (
    id                       UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id               UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    failure_id               UUID REFERENCES failures(id) ON DELETE SET NULL,
    test_case_id             UUID NOT NULL REFERENCES test_cases(id) ON DELETE CASCADE,
    environment_id           UUID REFERENCES environments(id) ON DELETE SET NULL,
    title                    TEXT NOT NULL,
    severity                 TEXT NOT NULL,
    failure_type             TEXT NOT NULL,
    affected_service         TEXT NOT NULL DEFAULT '',
    affected_route           TEXT NOT NULL DEFAULT '',
    preconditions            TEXT NOT NULL DEFAULT '',
    steps_to_reproduce       JSONB NOT NULL DEFAULT '[]'::jsonb,
    expected_result          TEXT NOT NULL DEFAULT '',
    actual_result            TEXT NOT NULL DEFAULT '',
    evidence                 JSONB NOT NULL DEFAULT '{}'::jsonb,
    first_observed_at        TIMESTAMPTZ NOT NULL,
    last_observed_at         TIMESTAMPTZ NOT NULL,
    frequency                INTEGER NOT NULL DEFAULT 1,
    root_cause_hypothesis    TEXT NOT NULL DEFAULT '',
    root_cause_confidence    TEXT NOT NULL DEFAULT 'low',
    flaky_assessment         TEXT NOT NULL DEFAULT 'insufficient_evidence',
    related_graph_path       TEXT NOT NULL DEFAULT '',
    regression_test_ids      JSONB NOT NULL DEFAULT '[]'::jsonb,
    -- A hint, not an automatic merge — see internal/bugreports.
    possible_duplicate_of_id UUID REFERENCES bug_reports(id) ON DELETE SET NULL,
    status                   TEXT NOT NULL DEFAULT 'open' CHECK (status IN ('open', 'resolved', 'reopened')),
    notes                    JSONB NOT NULL DEFAULT '[]'::jsonb,
    created_at               TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at               TIMESTAMPTZ NOT NULL DEFAULT now(),
    -- One bug row per (project, test case, failure type): repeated
    -- failures of the same kind on the same test update this row in
    -- place (frequency/last_observed_at) instead of accumulating
    -- duplicate bug reports.
    UNIQUE (project_id, test_case_id, failure_type)
);

CREATE INDEX idx_bug_reports_project ON bug_reports (project_id);
CREATE INDEX idx_bug_reports_project_status ON bug_reports (project_id, status);
