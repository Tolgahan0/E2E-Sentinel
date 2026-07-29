'use client';

import Link from 'next/link';
import { useCallback, useEffect, useMemo, useState } from 'react';
import { fetchJSON, mutateJSON, type Project, type ProjectsResponse, type TestCase, type TestsResponse } from '@/lib/api';

interface PendingTest extends TestCase {
  projectId: string;
  projectName: string;
}

const CONFIDENCE_CLASS: Record<TestCase['confidence'], string> = {
  high: 'sentinel-status-ok',
  medium: 'sentinel-status-unknown',
  low: 'sentinel-status-bad',
};

// Loads every project's pending test cases in parallel and flattens them
// into one cross-project queue. Same N+1-client-side-rollup trade-off as
// the Dashboard's pipeline stats — there is no cross-project "list
// pending tests" endpoint, since test cases are scoped per project.
async function loadPendingTests(projects: Project[]): Promise<PendingTest[]> {
  const perProject = await Promise.all(
    projects.map(async (p) => {
      const res = await fetchJSON<TestsResponse>(`/api/v1/projects/${p.id}/tests`);
      return (res?.tests ?? [])
        .filter((t) => t.approval_status === 'pending')
        .map((t): PendingTest => ({ ...t, projectId: p.id, projectName: p.name }));
    })
  );
  return perProject.flat();
}

export default function ApprovalsPage() {
  const [pending, setPending] = useState<PendingTest[]>([]);
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const [loaded, setLoaded] = useState(false);
  const [busy, setBusy] = useState(false);
  const [message, setMessage] = useState<string | null>(null);

  const load = useCallback(async () => {
    const projectsRes = await fetchJSON<ProjectsResponse>('/api/v1/projects');
    const projects = projectsRes?.projects ?? [];
    const tests = await loadPendingTests(projects);
    setPending(tests);
    setSelected(new Set());
    setLoaded(true);
  }, []);

  useEffect(() => {
    (async () => {
      await load();
    })();
  }, [load]);

  function toggle(id: string) {
    setSelected((prev) => {
      const next = new Set(prev);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });
  }

  function toggleAll() {
    setSelected((prev) => (prev.size === pending.length ? new Set() : new Set(pending.map((t) => t.id))));
  }

  async function bulkAction(action: 'approve' | 'reject') {
    if (selected.size === 0) return;
    setBusy(true);
    setMessage(null);
    const ids = Array.from(selected);
    const results = await Promise.all(
      ids.map(async (id) => {
        const result = await mutateJSON<TestCase>('POST', `/api/v1/tests/${id}/${action}`);
        return { id, ok: result.ok, error: result.error };
      })
    );
    setBusy(false);

    const succeededIds = new Set(results.filter((r) => r.ok).map((r) => r.id));
    setPending((prev) => prev.filter((t) => !succeededIds.has(t.id)));
    setSelected(new Set());

    const failed = results.filter((r) => !r.ok);
    if (failed.length > 0) {
      setMessage(
        `${succeededIds.size} ${action}d, ${failed.length} failed — most likely a mutating test blocked by a ` +
          `production/unknown-classified environment (see Environments).`,
      );
    } else {
      setMessage(`${succeededIds.size} test case${succeededIds.size === 1 ? '' : 's'} ${action}d.`);
    }
  }

  const allSelected = pending.length > 0 && selected.size === pending.length;

  const byProjectCount = useMemo(() => {
    const counts = new Map<string, number>();
    for (const t of pending) counts.set(t.projectName, (counts.get(t.projectName) ?? 0) + 1);
    return counts;
  }, [pending]);

  return (
    <>
      <h2>Approvals</h2>
      <p>
        Every suggested test case across every project, waiting on a human decision, in one place.
        Nothing runs until it&apos;s approved here (or on a project&apos;s{' '}
        <Link href="/test-inventory">Test Inventory</Link> page — the same action, either place).
        Approving a mutating test is blocked while its project has a production or unknown-classified
        environment (see <Link href="/environments">Environments</Link>).
      </p>

      {loaded && pending.length === 0 ? (
        <div className="sentinel-card">
          <p className="sentinel-status-ok">Nothing pending — every suggested test case has been reviewed.</p>
        </div>
      ) : (
        <>
          <div className="sentinel-card" style={{ display: 'flex', gap: '0.75rem', alignItems: 'center', flexWrap: 'wrap' }}>
            <span>
              <strong>{pending.length}</strong> pending across {byProjectCount.size} project{byProjectCount.size === 1 ? '' : 's'}
            </span>
            <button onClick={() => bulkAction('approve')} disabled={busy || selected.size === 0}>
              {busy ? 'Working…' : `Approve selected (${selected.size})`}
            </button>
            <button onClick={() => bulkAction('reject')} disabled={busy || selected.size === 0}>
              Reject selected ({selected.size})
            </button>
            {message && <span className="sentinel-status-unknown">{message}</span>}
          </div>

          <div className="sentinel-card" style={{ marginTop: '1rem' }}>
            {!loaded ? (
              <p className="sentinel-status-unknown">loading&hellip;</p>
            ) : (
              <table className="sentinel-table">
                <thead>
                  <tr>
                    <th>
                      <input type="checkbox" checked={allSelected} onChange={toggleAll} aria-label="Select all pending test cases" />
                    </th>
                    <th>Project</th>
                    <th>Title / category</th>
                    <th>Priority</th>
                    <th>Mutating</th>
                    <th>Production-safe</th>
                    <th>Confidence</th>
                  </tr>
                </thead>
                <tbody>
                  {pending.map((t) => (
                    <tr key={t.id}>
                      <td>
                        <input type="checkbox" checked={selected.has(t.id)} onChange={() => toggle(t.id)} aria-label={`Select ${t.title}`} />
                      </td>
                      <td>
                        <Link href={`/test-inventory?project=${t.projectId}`}>{t.projectName}</Link>
                      </td>
                      <td>
                        {t.title}
                        <br />
                        <span className="sentinel-status-unknown" style={{ fontSize: '0.75rem' }}>
                          {t.category}
                        </span>
                      </td>
                      <td>{t.priority}</td>
                      <td>{t.is_mutating ? 'mutating' : 'read-only'}</td>
                      <td className={t.is_production_safe ? 'sentinel-status-ok' : 'sentinel-status-bad'}>
                        {t.is_production_safe ? 'yes' : 'no'}
                      </td>
                      <td className={CONFIDENCE_CLASS[t.confidence]}>{t.confidence}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            )}
          </div>
        </>
      )}
    </>
  );
}
