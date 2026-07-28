-- Phase 2: Docker Compose discovery.

CREATE TABLE discovered_services (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id     UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    name           TEXT NOT NULL,
    kind           TEXT NOT NULL DEFAULT 'unknown',
    runtime        TEXT NOT NULL DEFAULT 'docker',
    source_path    TEXT,
    container_name TEXT,
    image          TEXT,
    ports          JSONB NOT NULL DEFAULT '[]'::jsonb,
    dependencies   JSONB NOT NULL DEFAULT '[]'::jsonb,
    metadata       JSONB NOT NULL DEFAULT '{}'::jsonb,
    confidence     TEXT NOT NULL CHECK (confidence IN ('high', 'medium', 'low')),
    last_seen_at   TIMESTAMPTZ,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (project_id, name)
);

CREATE INDEX idx_discovered_services_project ON discovered_services (project_id);
