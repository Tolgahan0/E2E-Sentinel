'use client';

import { Suspense, useCallback, useEffect, useMemo, useState } from 'react';
import { useSearchParams } from 'next/navigation';
import {
  fetchJSON,
  mutateJSON,
  type ArtifactsResponse,
  type Project,
  type ProjectsResponse,
  type RunArtifact,
  type RunsResponse,
  type TestCase,
  type TestRun,
  type TestsResponse,
} from '@/lib/api';

const STATUS_CLASS: Record<TestRun['status'], string> = {
  queued: 'sentinel-status-unknown',
  running: 'sentinel-status-unknown',
  passed: 'sentinel-status-ok',
  failed: 'sentinel-status-bad',
  cancelled: 'sentinel-status-unknown',
  error: 'sentinel-status-bad',
};

const ACTIVE_STATUSES = new Set(['queued', 'running']);

function ArtifactsList({ runID }: { runID: string }) {
  const [artifacts, setArtifacts] = useState<RunArtifact[] | null>(null);

  useEffect(() => {
    fetchJSON<ArtifactsResponse>(`/api/v1/runs/${runID}/artifacts`).then((res) => {
      setArtifacts(res?.artifacts ?? []);
    });
  }, [runID]);

  if (artifacts === null) return <p className="sentinel-status-unknown">loading artifacts&hellip;</p>;
  if (artifacts.length === 0) return <p className="sentinel-status-unknown">No artifacts for this run.</p>;

  return (
    <ul style={{ margin: 0, paddingLeft: '1.2rem' }}>
      {artifacts.map((a) => (
        <li key={a.id}>
          <a href={`/api/v1/artifacts/${a.id}/content`} target="_blank" rel="noreferrer">
            {a.kind}
          </a>{' '}
          <span className="sentinel-status-unknown" style={{ fontSize: '0.8rem' }}>
            ({a.mime_type}, {a.size_bytes} bytes)
          </span>
          {a.kind === 'screenshot' && (
            // eslint-disable-next-line @next/next/no-img-element
            <img
              src={`/api/v1/artifacts/${a.id}/content`}
              alt={`Screenshot artifact ${a.id}`}
              style={{ display: 'block', maxWidth: '20rem', marginTop: '0.3rem', borderRadius: '6px' }}
            />
          )}
        </li>
      ))}
    </ul>
  );
}

function RunRow({
  run,
  testTitle,
  githubRepo,
  onChanged,
}: {
  run: TestRun;
  testTitle: string;
  githubRepo?: string;
  onChanged: (updated: TestRun) => void;
}) {
  const [expanded, setExpanded] = useState(false);
  const [cancelling, setCancelling] = useState(false);

  async function cancel() {
    setCancelling(true);
    const result = await mutateJSON<TestRun>('POST', `/api/v1/runs/${run.id}/cancel`);
    setCancelling(false);
    if (result.ok && result.data) onChanged(result.data);
  }

  return (
    <>
      <tr>
        <td>{testTitle}</td>
        <td className={STATUS_CLASS[run.status]}>{run.status}</td>
        <td>
          {run.trigger_type === 'ci' ? 'CI' : 'manual'}
          {run.commit_sha && githubRepo && (
            <>
              {' '}
              <a
                href={`https://github.com/${githubRepo}/commit/${run.commit_sha}`}
                target="_blank"
                rel="noreferrer"
                style={{ fontFamily: 'monospace', fontSize: '0.85em' }}
              >
                {run.commit_sha.slice(0, 7)}
              </a>
            </>
          )}
        </td>
        <td>{run.exit_code ?? '—'}</td>
        <td>{new Date(run.started_at).toLocaleString()}</td>
        <td>
          <button onClick={() => setExpanded((v) => !v)}>{expanded ? 'hide' : 'artifacts'}</button>{' '}
          {ACTIVE_STATUSES.has(run.status) && (
            <button onClick={cancel} disabled={cancelling}>
              Cancel
            </button>
          )}
        </td>
      </tr>
      {expanded && (
        <tr>
          <td colSpan={6}>
            <ArtifactsList runID={run.id} />
          </td>
        </tr>
      )}
    </>
  );
}

function RunsContent() {
  const searchParams = useSearchParams();
  const initialProjectID = searchParams.get('project') ?? '';

  const [projects, setProjects] = useState<Project[]>([]);
  const [selectedID, setSelectedID] = useState(initialProjectID);
  const [tests, setTests] = useState<TestCase[]>([]);
  const [runs, setRuns] = useState<TestRun[]>([]);
  const [selectedTestID, setSelectedTestID] = useState('');
  const [starting, setStarting] = useState(false);
  const [runError, setRunError] = useState<string | null>(null);

  useEffect(() => {
    fetchJSON<ProjectsResponse>('/api/v1/projects').then((res) => {
      const list = res?.projects ?? [];
      setProjects(list);
      if (!selectedID && list[0]) setSelectedID(list[0].id);
    });
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const loadRuns = useCallback(async () => {
    if (!selectedID) return;
    const [testsRes, runsRes] = await Promise.all([
      fetchJSON<TestsResponse>(`/api/v1/projects/${selectedID}/tests`),
      fetchJSON<RunsResponse>(`/api/v1/projects/${selectedID}/runs`),
    ]);
    setTests(testsRes?.tests ?? []);
    setRuns(runsRes?.runs ?? []);
  }, [selectedID]);

  useEffect(() => {
    (async () => {
      await loadRuns();
    })();
  }, [loadRuns]);

  // Poll while any run is still in flight.
  useEffect(() => {
    if (!runs.some((r) => ACTIVE_STATUSES.has(r.status))) return;
    const id = setInterval(() => void loadRuns(), 2000);
    return () => clearInterval(id);
  }, [runs, loadRuns]);

  const approvedTests = useMemo(() => tests.filter((t) => t.approval_status === 'approved'), [tests]);
  const testTitleByID = useMemo(() => new Map(tests.map((t) => [t.id, t.title])), [tests]);
  const selectedProject = useMemo(() => projects.find((p) => p.id === selectedID), [projects, selectedID]);

  async function startRun() {
    if (!selectedTestID) return;
    setStarting(true);
    setRunError(null);
    const result = await mutateJSON<TestRun>('POST', `/api/v1/tests/${selectedTestID}/run`);
    setStarting(false);
    if (result.ok) {
      await loadRuns();
    } else {
      setRunError(result.error ?? 'run_failed');
    }
  }

  function updateRun(updated: TestRun) {
    setRuns((prev) => prev.map((r) => (r.id === updated.id ? updated : r)));
  }

  return (
    <>
      <h2>Runs</h2>
      <p>
        Each run executes in a disposable, resource-limited Docker container. Pass/fail comes only
        from the runner process&apos;s exit code — never from AI. Failed runs capture a screenshot,
        video, and trace automatically.
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
          Approved test
          <br />
          <select value={selectedTestID} onChange={(e) => setSelectedTestID(e.target.value)}>
            <option value="">Select an approved test</option>
            {approvedTests.map((t) => (
              <option key={t.id} value={t.id}>
                {t.title}
              </option>
            ))}
          </select>
        </label>
        <button onClick={startRun} disabled={!selectedTestID || starting}>
          {starting ? 'Starting…' : 'Run test'}
        </button>
        {runError && <span className="sentinel-status-bad">{runError}</span>}
      </div>
      {approvedTests.length === 0 && selectedID && (
        <p className="sentinel-status-unknown" style={{ marginTop: '0.5rem' }}>
          No approved tests yet — approve one on the <a href={`/test-inventory?project=${selectedID}`}>Test Inventory</a> page,
          and make sure an environment has a base_url set.
        </p>
      )}

      <div className="sentinel-card" style={{ marginTop: '1rem' }}>
        {runs.length === 0 ? (
          <p className="sentinel-status-unknown">No runs yet.</p>
        ) : (
          <table className="sentinel-table">
            <thead>
              <tr>
                <th>Test</th>
                <th>Status</th>
                <th>Trigger</th>
                <th>Exit code</th>
                <th>Started</th>
                <th>Actions</th>
              </tr>
            </thead>
            <tbody>
              {runs.map((r) => (
                <RunRow
                  key={r.id}
                  run={r}
                  testTitle={testTitleByID.get(r.test_case_id) ?? r.test_case_id}
                  githubRepo={selectedProject?.github_repo}
                  onChanged={updateRun}
                />
              ))}
            </tbody>
          </table>
        )}
      </div>
    </>
  );
}

export default function RunsPage() {
  return (
    <Suspense fallback={<p className="sentinel-status-unknown">loading&hellip;</p>}>
      <RunsContent />
    </Suspense>
  );
}
