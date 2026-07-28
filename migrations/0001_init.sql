-- Phase 0: foundation schema.
-- schema_migrations is created automatically by the migration runner
-- before this file runs; it is not created here.
--
-- Note: the migration runner splits this file into individual statements
-- on top-level semicolons, so statement order matters and dollar-quoted
-- function bodies are not yet supported (fine for Phase 0's plain DDL).

CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE TABLE audit_events (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    action_type   TEXT NOT NULL,
    resource_type TEXT NOT NULL,
    resource_id   TEXT,
    actor         TEXT NOT NULL,
    metadata      JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Audit events are append-only: no update/delete grants are issued here,
-- and application code never issues UPDATE/DELETE against this table.
CREATE INDEX idx_audit_events_created_at ON audit_events (created_at DESC);
CREATE INDEX idx_audit_events_resource ON audit_events (resource_type, resource_id);
