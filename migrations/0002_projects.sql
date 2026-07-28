-- Phase 1: projects, environments, and repository discovery.

CREATE TABLE projects (
    id                 UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name               TEXT NOT NULL,
    slug               TEXT NOT NULL UNIQUE,
    repository_path    TEXT NOT NULL,
    repository_type    TEXT NOT NULL DEFAULT 'local',
    default_branch     TEXT NOT NULL DEFAULT 'main',
    discovery_status   TEXT NOT NULL DEFAULT 'never_run'
        CHECK (discovery_status IN ('never_run', 'running', 'completed', 'failed')),
    current_mode       TEXT NOT NULL DEFAULT 'observe'
        CHECK (current_mode IN ('observe', 'test', 'fix_proposal', 'approved_fix')),
    settings           JSONB NOT NULL DEFAULT '{}'::jsonb,
    last_discovered_at TIMESTAMPTZ,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE environments (
    id                         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id                 UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    name                       TEXT NOT NULL,
    type                       TEXT NOT NULL DEFAULT 'local',
    base_url                   TEXT,
    classification             TEXT NOT NULL DEFAULT 'local'
        CHECK (classification IN ('local', 'development', 'test', 'staging', 'production', 'unknown')),
    is_production              BOOLEAN NOT NULL DEFAULT false,
    allow_mutations            BOOLEAN NOT NULL DEFAULT false,
    allow_load_tests           BOOLEAN NOT NULL DEFAULT false,
    allow_active_security_scan BOOLEAN NOT NULL DEFAULT false,
    credentials_reference      TEXT,
    created_at                 TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at                 TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (project_id, name)
);

CREATE INDEX idx_environments_project ON environments (project_id);

CREATE TABLE discovery_runs (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id   UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    status       TEXT NOT NULL DEFAULT 'running'
        CHECK (status IN ('running', 'completed', 'failed')),
    error        TEXT,
    started_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    completed_at TIMESTAMPTZ
);

CREATE INDEX idx_discovery_runs_project ON discovery_runs (project_id, started_at DESC);

CREATE TABLE discovery_findings (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    discovery_run_id  UUID NOT NULL REFERENCES discovery_runs(id) ON DELETE CASCADE,
    project_id        UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    category          TEXT NOT NULL,
    name              TEXT NOT NULL,
    path              TEXT,
    confidence        TEXT NOT NULL CHECK (confidence IN ('high', 'medium', 'low')),
    evidence          JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_discovery_findings_run ON discovery_findings (discovery_run_id);
CREATE INDEX idx_discovery_findings_project ON discovery_findings (project_id, category);
