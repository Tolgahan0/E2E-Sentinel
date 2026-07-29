'use client';

import { Fragment, Suspense, useCallback, useEffect, useState } from 'react';
import { useSearchParams } from 'next/navigation';
import {
  fetchJSON,
  mutateJSON,
  type BugReport,
  type BugsResponse,
  type Project,
  type ProjectsResponse,
} from '@/lib/api';

const STATUS_CLASS: Record<BugReport['status'], string> = {
  open: 'sentinel-status-bad',
  reopened: 'sentinel-status-bad',
  resolved: 'sentinel-status-ok',
};

const SEVERITY_CLASS: Record<BugReport['severity'], string> = {
  critical: 'sentinel-status-bad',
  high: 'sentinel-status-bad',
  medium: 'sentinel-status-unknown',
  low: 'sentinel-status-unknown',
  informational: 'sentinel-status-unknown',
};

function BugDetail({ bug, onChanged }: { bug: BugReport; onChanged: (b: BugReport) => void }) {
  const [note, setNote] = useState('');
  const [submittingNote, setSubmittingNote] = useState(false);

  async function setStatus(action: 'resolve' | 'reopen') {
    const result = await mutateJSON<BugReport>('POST', `/api/v1/bugs/${bug.id}/${action}`);
    if (result.ok && result.data) onChanged(result.data);
  }

  async function addNote() {
    if (!note.trim()) return;
    setSubmittingNote(true);
    const result = await mutateJSON<BugReport>('POST', `/api/v1/bugs/${bug.id}/notes`, { text: note });
    setSubmittingNote(false);
    if (result.ok && result.data) {
      onChanged(result.data);
      setNote('');
    }
  }

  return (
    <div style={{ padding: '0.75rem 0' }}>
      <p>
        <strong>Affected route:</strong> {bug.affected_route || '—'}
        {bug.affected_service && (
          <>
            {' '}
            <strong>Service:</strong> {bug.affected_service}
          </>
        )}
      </p>
      {bug.related_graph_path && (
        <p>
          <strong>Related graph path:</strong> {bug.related_graph_path}
        </p>
      )}
      <p>
        <strong>Expected:</strong> {bug.expected_result || '—'}
        <br />
        <strong>Actual:</strong> {bug.actual_result || '—'}
      </p>
      {bug.error_message && (
        <p>
          <strong>Error:</strong> <code>{bug.error_message}</code>
        </p>
      )}
      <p className="sentinel-status-unknown">
        First observed {new Date(bug.first_observed_at).toLocaleString()} · last observed{' '}
        {new Date(bug.last_observed_at).toLocaleString()} · {bug.frequency} occurrence(s) · flakiness:{' '}
        {bug.flaky_assessment}
      </p>
      <div className="sentinel-card" style={{ background: 'rgba(255,180,0,0.08)' }}>
        <strong>Likely root cause (unverified hypothesis — {bug.root_cause_confidence} confidence):</strong>
        <p style={{ margin: '0.4rem 0 0' }}>{bug.root_cause_hypothesis || '—'}</p>
      </div>
      {bug.possible_duplicate_of_id && (
        <p className="sentinel-status-unknown">
          Possible duplicate of bug {bug.possible_duplicate_of_id} (unconfirmed — review before linking).
        </p>
      )}
      {bug.artifact_ids.length > 0 && (
        <p>
          Evidence:{' '}
          {bug.artifact_ids.map((id) => (
            <a key={id} href={`/api/v1/artifacts/${id}/content`} target="_blank" rel="noreferrer" style={{ marginRight: '0.5rem' }}>
              artifact
            </a>
          ))}
        </p>
      )}

      <div style={{ marginTop: '0.5rem' }}>
        <a href={`/api/v1/bugs/${bug.id}/export/markdown`}>Export Markdown</a>{' '}
        <a href={`/api/v1/bugs/${bug.id}/export/json`}>Export JSON</a>{' '}
        {bug.status === 'resolved' ? (
          <button onClick={() => setStatus('reopen')}>Reopen</button>
        ) : (
          <button onClick={() => setStatus('resolve')}>Resolve</button>
        )}
      </div>

      {bug.notes.length > 0 && (
        <ul style={{ marginTop: '0.5rem' }}>
          {bug.notes.map((n, i) => (
            <li key={i}>
              <em>{n.author}</em> ({new Date(n.created_at).toLocaleString()}): {n.text}
            </li>
          ))}
        </ul>
      )}
      <div style={{ marginTop: '0.5rem', display: 'flex', gap: '0.5rem' }}>
        <input value={note} onChange={(e) => setNote(e.target.value)} placeholder="Add a note" style={{ flex: 1 }} />
        <button onClick={addNote} disabled={submittingNote || !note.trim()}>
          Add note
        </button>
      </div>
    </div>
  );
}

function BugsContent() {
  const searchParams = useSearchParams();
  const initialProjectID = searchParams.get('project') ?? '';

  const [projects, setProjects] = useState<Project[]>([]);
  const [selectedID, setSelectedID] = useState(initialProjectID);
  const [bugs, setBugs] = useState<BugReport[]>([]);
  const [severity, setSeverity] = useState('');
  const [status, setStatus] = useState('');
  const [search, setSearch] = useState('');
  const [expandedID, setExpandedID] = useState<string | null>(null);

  useEffect(() => {
    fetchJSON<ProjectsResponse>('/api/v1/projects').then((res) => {
      const list = res?.projects ?? [];
      setProjects(list);
      if (!selectedID && list[0]) setSelectedID(list[0].id);
    });
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const loadBugs = useCallback(async () => {
    if (!selectedID) return;
    const params = new URLSearchParams({ project_id: selectedID });
    if (severity) params.set('severity', severity);
    if (status) params.set('status', status);
    if (search) params.set('search', search);
    const res = await fetchJSON<BugsResponse>(`/api/v1/bugs?${params.toString()}`);
    setBugs(res?.bugs ?? []);
  }, [selectedID, severity, status, search]);

  useEffect(() => {
    (async () => {
      await loadBugs();
    })();
  }, [loadBugs]);

  function updateBug(updated: BugReport) {
    setBugs((prev) => prev.map((b) => (b.id === updated.id ? updated : b)));
  }

  return (
    <>
      <h2>Bugs</h2>
      <p>
        Bug reports are created automatically from failed test runs — deterministic failure classification, no AI
        involved. Every root cause shown here is an explicitly-labeled hypothesis, never a confirmed diagnosis.
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
          Severity
          <br />
          <select value={severity} onChange={(e) => setSeverity(e.target.value)}>
            <option value="">All</option>
            <option value="critical">Critical</option>
            <option value="high">High</option>
            <option value="medium">Medium</option>
            <option value="low">Low</option>
            <option value="informational">Informational</option>
          </select>
        </label>
        <label>
          Status
          <br />
          <select value={status} onChange={(e) => setStatus(e.target.value)}>
            <option value="">All</option>
            <option value="open">Open</option>
            <option value="reopened">Reopened</option>
            <option value="resolved">Resolved</option>
          </select>
        </label>
        <label>
          Search
          <br />
          <input value={search} onChange={(e) => setSearch(e.target.value)} placeholder="Title contains…" />
        </label>
      </div>

      <div className="sentinel-card" style={{ marginTop: '1rem' }}>
        {bugs.length === 0 ? (
          <p className="sentinel-status-unknown">No bugs match these filters.</p>
        ) : (
          <table className="sentinel-table">
            <thead>
              <tr>
                <th>Title</th>
                <th>Severity</th>
                <th>Failure type</th>
                <th>Status</th>
                <th>Frequency</th>
                <th>Last observed</th>
                <th></th>
              </tr>
            </thead>
            <tbody>
              {bugs.map((b) => (
                <Fragment key={b.id}>
                  <tr>
                    <td>{b.title}</td>
                    <td className={SEVERITY_CLASS[b.severity]}>{b.severity}</td>
                    <td>{b.failure_type}</td>
                    <td className={STATUS_CLASS[b.status]}>{b.status}</td>
                    <td>{b.frequency}</td>
                    <td>{new Date(b.last_observed_at).toLocaleString()}</td>
                    <td>
                      <button onClick={() => setExpandedID(expandedID === b.id ? null : b.id)}>
                        {expandedID === b.id ? 'hide' : 'details'}
                      </button>
                    </td>
                  </tr>
                  {expandedID === b.id && (
                    <tr>
                      <td colSpan={7}>
                        <BugDetail bug={b} onChanged={updateBug} />
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

export default function BugsPage() {
  return (
    <Suspense fallback={<p className="sentinel-status-unknown">loading&hellip;</p>}>
      <BugsContent />
    </Suspense>
  );
}
