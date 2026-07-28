-- Phase 6: AI Provider Gateway.

CREATE TABLE secret_references (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    ciphertext BYTEA NOT NULL,
    nonce      BYTEA NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE ai_providers (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    type                TEXT NOT NULL,
    name                TEXT NOT NULL,
    base_url            TEXT NOT NULL DEFAULT '',
    model               TEXT NOT NULL DEFAULT '',
    -- ON DELETE SET NULL: deleting a secret reference must never cascade
    -- into deleting the provider row itself (spec §6.11 keeps the
    -- provider's own configuration independent of key rotation).
    secret_reference_id UUID REFERENCES secret_references(id) ON DELETE SET NULL,
    is_local            BOOLEAN NOT NULL DEFAULT false,
    enabled             BOOLEAN NOT NULL DEFAULT true,
    capabilities        JSONB NOT NULL DEFAULT '[]'::jsonb,
    timeout_seconds     INTEGER NOT NULL DEFAULT 30,
    max_tokens          INTEGER NOT NULL DEFAULT 0,
    temperature         DOUBLE PRECISION NOT NULL DEFAULT 0,
    health_status       TEXT NOT NULL DEFAULT 'unknown' CHECK (health_status IN ('unknown', 'ok', 'error')),
    last_checked_at     TIMESTAMPTZ,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_ai_providers_enabled ON ai_providers (enabled);

-- Generic application-wide settings (spec data model §19), keyed by an
-- opaque string. First consumer: ai.task_routing, a JSON object mapping
-- task type -> provider id (spec §16.4).
CREATE TABLE settings (
    key        TEXT PRIMARY KEY,
    value      JSONB NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
