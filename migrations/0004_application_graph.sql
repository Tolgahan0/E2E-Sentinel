-- Phase 3: Application Graph.

CREATE TABLE graph_nodes (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id        UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    node_type         TEXT NOT NULL,
    label             TEXT NOT NULL,
    source_reference  TEXT,
    runtime_reference TEXT,
    metadata          JSONB NOT NULL DEFAULT '{}'::jsonb,
    confidence        TEXT NOT NULL CHECK (confidence IN ('high', 'medium', 'low')),
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_graph_nodes_project ON graph_nodes (project_id);

CREATE TABLE graph_edges (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id     UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    source_node_id UUID NOT NULL REFERENCES graph_nodes(id) ON DELETE CASCADE,
    target_node_id UUID NOT NULL REFERENCES graph_nodes(id) ON DELETE CASCADE,
    relation_type  TEXT NOT NULL,
    evidence       JSONB NOT NULL DEFAULT '{}'::jsonb,
    confidence     TEXT NOT NULL CHECK (confidence IN ('high', 'medium', 'low')),
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_graph_edges_project ON graph_edges (project_id);
CREATE INDEX idx_graph_edges_source ON graph_edges (source_node_id);
CREATE INDEX idx_graph_edges_target ON graph_edges (target_node_id);
