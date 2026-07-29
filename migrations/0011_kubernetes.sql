-- Phase 10: Kubernetes discovery.

CREATE TABLE kube_resources (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id       UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    namespace        TEXT NOT NULL,
    kind             TEXT NOT NULL,
    name             TEXT NOT NULL,
    desired_replicas INTEGER,
    ready_replicas   INTEGER,
    restart_count    INTEGER,
    status           TEXT NOT NULL DEFAULT 'not_applicable',
    metadata         JSONB NOT NULL DEFAULT '{}'::jsonb,
    last_seen_at     TIMESTAMPTZ,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (project_id, namespace, kind, name)
);

CREATE INDEX idx_kube_resources_project ON kube_resources (project_id);
