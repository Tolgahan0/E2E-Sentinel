'use client';

import { Fragment, Suspense, useCallback, useEffect, useState } from 'react';
import { useSearchParams } from 'next/navigation';
import {
  fetchJSON,
  mutateJSON,
  type FileApplyResult,
  type FixProposal,
  type FixProposalApplyResult,
  type FixProposalsResponse,
  type Project,
  type ProjectsResponse,
} from '@/lib/api';

const STATUS_CLASS: Record<FixProposal['approval_status'], string> = {
  pending_review: 'sentinel-status-unknown',
  approved: 'sentinel-status-ok',
  rejected: 'sentinel-status-bad',
  revision_requested: 'sentinel-status-unknown',
};

const RISK_CLASS: Record<FixProposal['risk_level'], string> = {
  low: 'sentinel-status-ok',
  medium: 'sentinel-status-unknown',
  high: 'sentinel-status-bad',
};

// A plain, colored +/- diff viewer. Full Monaco Editor integration (spec
// §17.1) is deferred — a heavy dependency for marginal review value over
// a readable colored diff at this stage; documented in docs/FIX_PROPOSALS.md.
function DiffViewer({ diff }: { diff: string }) {
  return (
    <pre
      style={{
        background: 'rgba(127,127,127,0.08)',
        padding: '0.6rem',
        borderRadius: '6px',
        overflowX: 'auto',
        fontSize: '0.85rem',
        maxHeight: '24rem',
      }}
    >
      {diff.split('\n').map((line, i) => {
        let color: string | undefined;
        if (line.startsWith('+') && !line.startsWith('+++')) color = 'var(--sentinel-ok, #2e7d32)';
        else if (line.startsWith('-') && !line.startsWith('---')) color = 'var(--sentinel-bad, #c62828)';
        return (
          <div key={i} style={{ color }}>
            {line || ' '}
          </div>
        );
      })}
    </pre>
  );
}

function FileResultsList({ results }: { results?: FileApplyResult[] }) {
  if (!results || results.length === 0) return null;
  return (
    <ul style={{ margin: '0.4rem 0', paddingLeft: '1.2rem' }}>
      {results.map((r, i) => (
        <li key={i} className={r.applied ? 'sentinel-status-ok' : 'sentinel-status-bad'}>
          {r.action} {r.path} {r.applied ? '' : `— ${r.error}`}
        </li>
      ))}
    </ul>
  );
}

function FixProposalDetail({ fp, onChanged }: { fp: FixProposal; onChanged: (updated: FixProposal) => void }) {
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  async function setStatus(action: 'approve' | 'reject' | 'request-revision') {
    setBusy(true);
    setError(null);
    const res = await mutateJSON<FixProposal>('POST', `/api/v1/fix-proposals/${fp.id}/${action}`);
    setBusy(false);
    if (res.ok && res.data) onChanged(res.data);
    else setError(res.error ?? 'action_failed');
  }

  async function applyWorkspace() {
    setBusy(true);
    setError(null);
    const res = await mutateJSON<FixProposalApplyResult>('POST', `/api/v1/fix-proposals/${fp.id}/apply-workspace`);
    setBusy(false);
    if (res.ok && res.data) onChanged(res.data.fix_proposal);
    else setError(res.error ?? 'apply_workspace_failed');
  }

  async function applyRepository() {
    if (!confirm('This writes the approved diff directly to the project repository. Continue?')) return;
    setBusy(true);
    setError(null);
    const res = await mutateJSON<FixProposalApplyResult>('POST', `/api/v1/fix-proposals/${fp.id}/apply-repository`);
    setBusy(false);
    if (res.ok && res.data) onChanged(res.data.fix_proposal);
    else setError(res.error ?? 'apply_repository_failed');
  }

  return (
    <div style={{ padding: '0.75rem 0' }}>
      <p>{fp.description}</p>
      <p>
        <strong>Risk:</strong> <span className={RISK_CLASS[fp.risk_level]}>{fp.risk_level}</span>{' '}
        <strong>Source:</strong> {fp.ai_provider ? `AI (${fp.ai_provider}${fp.ai_model ? `/${fp.ai_model}` : ''})` : 'Manual'}
      </p>
      {fp.assumptions && (
        <p>
          <strong>Assumptions:</strong> {fp.assumptions}
        </p>
      )}
      {fp.potential_side_effects && (
        <p>
          <strong>Potential side effects:</strong> {fp.potential_side_effects}
        </p>
      )}
      {fp.rollback_guidance && (
        <p>
          <strong>Rollback guidance:</strong> {fp.rollback_guidance}
        </p>
      )}
      <p>
        <strong>Files changed:</strong> {fp.files_changed.join(', ') || '—'}
      </p>
      {fp.regression_test_ids.length > 0 && (
        <p>
          <strong>Regression tests to rerun:</strong>{' '}
          <a href={`/runs?project=${fp.project_id}`}>{fp.regression_test_ids.join(', ')} (run on the Runs page)</a>
        </p>
      )}

      <DiffViewer diff={fp.unified_diff} />

      <div style={{ marginTop: '0.5rem', display: 'flex', gap: '0.5rem', flexWrap: 'wrap' }}>
        <button onClick={() => setStatus('approve')} disabled={busy || fp.approval_status === 'approved'}>
          Approve
        </button>
        <button onClick={() => setStatus('reject')} disabled={busy || fp.approval_status === 'rejected'}>
          Reject
        </button>
        <button onClick={() => setStatus('request-revision')} disabled={busy}>
          Request revision
        </button>
        <button onClick={applyWorkspace} disabled={busy}>
          Apply to temporary workspace
        </button>
        <button onClick={applyRepository} disabled={busy || fp.approval_status !== 'approved' || !!fp.repository_applied_at}>
          Apply to repository
        </button>
      </div>
      {error && <p className="sentinel-status-bad">{error}</p>}

      {fp.workspace_applied_at && (
        <div style={{ marginTop: '0.5rem' }}>
          <strong>Workspace application</strong> ({new Date(fp.workspace_applied_at).toLocaleString()}, {fp.workspace_dir}):
          <FileResultsList results={fp.workspace_apply_results} />
        </div>
      )}
      {fp.repository_applied_at && (
        <div style={{ marginTop: '0.5rem' }} className="sentinel-status-ok">
          <strong>Applied to the repository</strong> at {new Date(fp.repository_applied_at).toLocaleString()}:
          <FileResultsList results={fp.repository_apply_results} />
        </div>
      )}
    </div>
  );
}

function FixProposalsContent() {
  const searchParams = useSearchParams();
  const initialProjectID = searchParams.get('project') ?? '';

  const [projects, setProjects] = useState<Project[]>([]);
  const [selectedID, setSelectedID] = useState(initialProjectID);
  const [proposals, setProposals] = useState<FixProposal[]>([]);
  const [expandedID, setExpandedID] = useState<string | null>(null);

  useEffect(() => {
    fetchJSON<ProjectsResponse>('/api/v1/projects').then((res) => {
      const list = res?.projects ?? [];
      setProjects(list);
      if (!selectedID && list[0]) setSelectedID(list[0].id);
    });
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const load = useCallback(async () => {
    if (!selectedID) return;
    const res = await fetchJSON<FixProposalsResponse>(`/api/v1/projects/${selectedID}/fix-proposals`);
    setProposals(res?.fix_proposals ?? []);
  }, [selectedID]);

  useEffect(() => {
    (async () => {
      await load();
    })();
  }, [load]);

  function updateProposal(updated: FixProposal) {
    setProposals((prev) => prev.map((p) => (p.id === updated.id ? updated : p)));
  }

  return (
    <>
      <h2>Fix Proposals</h2>
      <p>
        A fix proposal is a candidate patch — from AI (evidence-only, never reading repository source) or a manually
        pasted diff — reviewed here before anything touches the real repository. The AI can never apply a patch
        itself; only an explicit approval followed by &quot;Apply to repository&quot; ever writes to the target
        project, and only once per proposal.
      </p>

      <div className="sentinel-card" style={{ display: 'flex', gap: '0.75rem', alignItems: 'flex-end' }}>
        <label>
          Project
          <br />
          <select value={selectedID} onChange={(e) => setSelectedID(e.target.value)}>
            <option value="" disabled>
              Select a project
            </option>
            {projects.map((p) => (
              <option key={p.id} value={p.id}>
                {p.name}
              </option>
            ))}
          </select>
        </label>
        <a href={`/bugs?project=${selectedID}`}>Generate one from a bug &rarr;</a>
      </div>

      <div className="sentinel-card" style={{ marginTop: '1rem' }}>
        {proposals.length === 0 ? (
          <p className="sentinel-status-unknown">No fix proposals yet.</p>
        ) : (
          <table className="sentinel-table">
            <thead>
              <tr>
                <th>Title</th>
                <th>Risk</th>
                <th>Source</th>
                <th>Status</th>
                <th></th>
              </tr>
            </thead>
            <tbody>
              {proposals.map((fp) => (
                <Fragment key={fp.id}>
                  <tr>
                    <td>{fp.title}</td>
                    <td className={RISK_CLASS[fp.risk_level]}>{fp.risk_level}</td>
                    <td>{fp.ai_provider || 'manual'}</td>
                    <td className={STATUS_CLASS[fp.approval_status]}>{fp.approval_status}</td>
                    <td>
                      <button onClick={() => setExpandedID(expandedID === fp.id ? null : fp.id)}>
                        {expandedID === fp.id ? 'hide' : 'review'}
                      </button>
                    </td>
                  </tr>
                  {expandedID === fp.id && (
                    <tr>
                      <td colSpan={5}>
                        <FixProposalDetail fp={fp} onChanged={updateProposal} />
                      </td>
                    </tr>
                  )}
                </Fragment>
              ))}
            </tbody>
          </table>
        )}
      </div>
    </>
  );
}

export default function FixProposalsPage() {
  return (
    <Suspense fallback={<p className="sentinel-status-unknown">loading&hellip;</p>}>
      <FixProposalsContent />
    </Suspense>
  );
}
