-- Phase 8: Fix Proposals.

CREATE TABLE fix_proposals (
    id                       UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id               UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    bug_id                   UUID NOT NULL REFERENCES bug_reports(id) ON DELETE CASCADE,
    title                    TEXT NOT NULL,
    description              TEXT NOT NULL DEFAULT '',
    risk_level               TEXT NOT NULL DEFAULT 'medium' CHECK (risk_level IN ('low', 'medium', 'high')),
    assumptions              TEXT NOT NULL DEFAULT '',
    potential_side_effects   TEXT NOT NULL DEFAULT '',
    rollback_guidance        TEXT NOT NULL DEFAULT '',
    files_changed            JSONB NOT NULL DEFAULT '[]'::jsonb,
    -- unified_diff, once set, is never edited in place — approving a
    -- proposal approves exactly this text, and apply-repository re-parses
    -- this same column, never a regenerated version (spec §15.2
    -- acceptance: "Applied files match approved diff exactly").
    unified_diff             TEXT NOT NULL,
    regression_test_ids      JSONB NOT NULL DEFAULT '[]'::jsonb,
    -- ai_provider/ai_model are empty for a manually-authored proposal.
    ai_provider              TEXT NOT NULL DEFAULT '',
    ai_model                 TEXT NOT NULL DEFAULT '',
    generated_at             TIMESTAMPTZ NOT NULL DEFAULT now(),
    approval_status          TEXT NOT NULL DEFAULT 'pending_review'
        CHECK (approval_status IN ('pending_review', 'approved', 'rejected', 'revision_requested')),
    workspace_dir            TEXT NOT NULL DEFAULT '',
    workspace_apply_results  JSONB,
    workspace_applied_at     TIMESTAMPTZ,
    repository_apply_results JSONB,
    -- Once set, apply-repository refuses a second application for this
    -- proposal (see fixproposals.ErrAlreadyAppliedToRepository).
    repository_applied_at    TIMESTAMPTZ,
    created_at               TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at               TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_fix_proposals_project ON fix_proposals (project_id);
CREATE INDEX idx_fix_proposals_bug ON fix_proposals (bug_id);
