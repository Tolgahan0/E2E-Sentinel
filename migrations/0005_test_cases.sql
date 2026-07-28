-- Phase 4: Test Planning.

CREATE TABLE test_cases (
    id                   UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id           UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    natural_key          TEXT NOT NULL,
    title                TEXT NOT NULL,
    description          TEXT NOT NULL DEFAULT '',
    category             TEXT NOT NULL,
    framework            TEXT NOT NULL DEFAULT 'playwright',
    status               TEXT NOT NULL DEFAULT 'suggested'
        CHECK (status IN ('suggested', 'approved', 'generated', 'ready', 'running', 'passed', 'failed', 'flaky', 'blocked', 'disabled')),
    risk_level           TEXT NOT NULL DEFAULT 'low' CHECK (risk_level IN ('high', 'medium', 'low')),
    priority             TEXT NOT NULL DEFAULT 'P2' CHECK (priority IN ('P0', 'P1', 'P2', 'P3')),
    confidence           TEXT NOT NULL CHECK (confidence IN ('high', 'medium', 'low')),
    source               TEXT NOT NULL DEFAULT 'rule_engine',
    preconditions        TEXT NOT NULL DEFAULT '',
    steps                JSONB NOT NULL DEFAULT '[]'::jsonb,
    assertions           JSONB NOT NULL DEFAULT '[]'::jsonb,
    required_credentials JSONB NOT NULL DEFAULT '[]'::jsonb,
    is_mutating          BOOLEAN NOT NULL DEFAULT false,
    is_production_safe   BOOLEAN NOT NULL DEFAULT true,
    generated_file_path  TEXT,
    approval_status      TEXT NOT NULL DEFAULT 'pending' CHECK (approval_status IN ('pending', 'approved', 'rejected')),
    created_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (project_id, natural_key)
);

CREATE INDEX idx_test_cases_project ON test_cases (project_id);
CREATE INDEX idx_test_cases_project_status ON test_cases (project_id, status);
