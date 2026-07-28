'use client';

import { Suspense, useEffect, useMemo, useState } from 'react';
import { useSearchParams } from 'next/navigation';
import { fetchJSON, mutateJSON, type Project, type ProjectsResponse, type TestCase, type TestsResponse } from '@/lib/api';

const CONFIDENCE_CLASS: Record<TestCase['confidence'], string> = {
  high: 'sentinel-status-ok',
  medium: 'sentinel-status-unknown',
  low: 'sentinel-status-bad',
};

const APPROVAL_CLASS: Record<TestCase['approval_status'], string> = {
  pending: 'sentinel-status-unknown',
  approved: 'sentinel-status-ok',
  rejected: 'sentinel-status-bad',
};

function TestRow({ test, onChanged }: { test: TestCase; onChanged: (updated: TestCase) => void }) {
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [editing, setEditing] = useState(false);
  const [title, setTitle] = useState(test.title);
  const [priority, setPriority] = useState(test.priority);

  async function approve() {
    setBusy(true);
    setError(null);
    const result = await mutateJSON<TestCase>('POST', `/api/v1/tests/${test.id}/approve`);
    setBusy(false);
    if (result.ok && result.data) {
      onChanged(result.data);
    } else {
      setError(result.error ?? 'approve_failed');
    }
  }

  async function reject() {
    setBusy(true);
    const result = await mutateJSON<TestCase>('POST', `/api/v1/tests/${test.id}/reject`);
    setBusy(false);
    if (result.ok && result.data) onChanged(result.data);
  }

  async function saveEdit() {
    setBusy(true);
    const result = await mutateJSON<TestCase>('PATCH', `/api/v1/tests/${test.id}`, { title, priority });
    setBusy(false);
    if (result.ok && result.data) {
      onChanged(result.data);
      setEditing(false);
    }
  }

  return (
    <tr>
      <td>
        {editing ? (
          <input value={title} onChange={(e) => setTitle(e.target.value)} style={{ width: '100%' }} />
        ) : (
          test.title
        )}
        <br />
        <span className="sentinel-status-unknown" style={{ fontSize: '0.75rem' }}>
          {test.category}
        </span>
      </td>
      <td>
        {editing ? (
          <select value={priority} onChange={(e) => setPriority(e.target.value as TestCase['priority'])}>
            {(['P0', 'P1', 'P2', 'P3'] as const).map((p) => (
              <option key={p} value={p}>
                {p}
              </option>
            ))}
          </select>
        ) : (
          test.priority
        )}
      </td>
      <td>{test.is_mutating ? 'mutating' : 'read-only'}</td>
      <td className={test.is_production_safe ? 'sentinel-status-ok' : 'sentinel-status-bad'}>
        {test.is_production_safe ? 'yes' : 'no'}
      </td>
      <td className={CONFIDENCE_CLASS[test.confidence]}>{test.confidence}</td>
      <td className={APPROVAL_CLASS[test.approval_status]}>{test.approval_status}</td>
      <td>
        {editing ? (
          <>
            <button onClick={saveEdit} disabled={busy}>
              Save
            </button>{' '}
            <button onClick={() => setEditing(false)} disabled={busy}>
              Cancel
            </button>
          </>
        ) : (
          <>
            <button onClick={approve} disabled={busy || test.approval_status === 'approved'}>
              Approve
            </button>{' '}
            <button onClick={reject} disabled={busy || test.approval_status === 'rejected'}>
              Reject
            </button>{' '}
            <button onClick={() => setEditing(true)} disabled={busy}>
              Edit
            </button>
          </>
        )}
        {error && (
          <>
            <br />
            <span className="sentinel-status-bad" style={{ fontSize: '0.75rem' }}>
              {error}
            </span>
          </>
        )}
      </td>
    </tr>
  );
}

function TestInventoryContent() {
  const searchParams = useSearchParams();
  const initialProjectID = searchParams.get('project') ?? '';

  const [projects, setProjects] = useState<Project[]>([]);
  const [selectedID, setSelectedID] = useState(initialProjectID);
  const [tests, setTests] = useState<TestCase[]>([]);
  const [categoryFilter, setCategoryFilter] = useState('');
  const [generating, setGenerating] = useState(false);
  const [planError, setPlanError] = useState<string | null>(null);

  useEffect(() => {
    fetchJSON<ProjectsResponse>('/api/v1/projects').then((res) => {
      const list = res?.projects ?? [];
      setProjects(list);
      if (!selectedID && list[0]) {
        setSelectedID(list[0].id);
      }
    });
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  useEffect(() => {
    if (!selectedID) return;
    fetchJSON<TestsResponse>(`/api/v1/projects/${selectedID}/tests`).then((res) => {
      setTests(res?.tests ?? []);
    });
  }, [selectedID]);

  async function generatePlan() {
    if (!selectedID) return;
    setGenerating(true);
    setPlanError(null);
    const result = await mutateJSON<{ tests: TestCase[] }>('POST', `/api/v1/projects/${selectedID}/tests/plan`);
    setGenerating(false);
    if (result.ok && result.data) {
      setTests(result.data.tests ?? []);
    } else {
      setPlanError(result.error === 'no_discovery_run' ? 'Run discovery on this project first (Discovery page).' : result.error ?? 'plan_failed');
    }
  }

  const categories = useMemo(() => Array.from(new Set(tests.map((t) => t.category))).sort(), [tests]);
  const filtered = useMemo(() => (categoryFilter ? tests.filter((t) => t.category === categoryFilter) : tests), [tests, categoryFilter]);

  const coverage = useMemo(() => {
    const byCategory = new Map<string, { total: number; highConfidence: number }>();
    for (const t of tests) {
      const entry = byCategory.get(t.category) ?? { total: 0, highConfidence: 0 };
      entry.total += 1;
      if (t.confidence === 'high') entry.highConfidence += 1;
      byCategory.set(t.category, entry);
    }
    return Array.from(byCategory.entries()).sort(([a], [b]) => a.localeCompare(b));
  }, [tests]);

  function updateTest(updated: TestCase) {
    setTests((prev) => prev.map((t) => (t.id === updated.id ? updated : t)));
  }

  return (
    <>
      <h2>Test Inventory</h2>
      <p>
        Deterministic, rule-based test suggestions — no AI involved. Every mutating test is clearly
        marked and defaults to production-unsafe; approving a mutating test is blocked while the
        project has a production or unknown-classified environment.
      </p>

      <div className="sentinel-card" style={{ display: 'flex', gap: '0.75rem', alignItems: 'flex-end', flexWrap: 'wrap' }}>
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
        <label>
          Filter by category
          <br />
          <select value={categoryFilter} onChange={(e) => setCategoryFilter(e.target.value)}>
            <option value="">All categories</option>
            {categories.map((c) => (
              <option key={c} value={c}>
                {c}
              </option>
            ))}
          </select>
        </label>
        <button onClick={generatePlan} disabled={!selectedID || generating}>
          {generating ? 'Generating…' : 'Generate test plan'}
        </button>
        {planError && <span className="sentinel-status-bad">{planError}</span>}
      </div>

      {coverage.length > 0 && (
        <div className="sentinel-card" style={{ marginTop: '1rem' }}>
          <h3>Coverage confidence</h3>
          <p className="sentinel-status-unknown" style={{ fontSize: '0.85rem' }}>
            Suggested coverage only — this is not a claim of complete test coverage.
          </p>
          {coverage.map(([category, stats]) => (
            <p key={category}>
              {category}: {stats.total} suggested ({stats.highConfidence} high-confidence)
            </p>
          ))}
        </div>
      )}

      <div className="sentinel-card" style={{ marginTop: '1rem' }}>
        {tests.length === 0 ? (
          <p className="sentinel-status-unknown">
            No test cases yet. Run discovery, then click &quot;Generate test plan&quot;.
          </p>
        ) : (
          <table className="sentinel-table">
            <thead>
              <tr>
                <th>Title / category</th>
                <th>Priority</th>
                <th>Mutating</th>
                <th>Production-safe</th>
                <th>Confidence</th>
                <th>Approval</th>
                <th>Actions</th>
              </tr>
            </thead>
            <tbody>
              {filtered.map((t) => (
                <TestRow key={t.id} test={t} onChanged={updateTest} />
              ))}
            </tbody>
          </table>
        )}
      </div>
    </>
  );
}

export default function TestInventoryPage() {
  return (
    <Suspense fallback={<p className="sentinel-status-unknown">loading&hellip;</p>}>
      <TestInventoryContent />
    </Suspense>
  );
}
