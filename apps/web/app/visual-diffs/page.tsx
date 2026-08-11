'use client';

import { Suspense, useCallback, useEffect, useState } from 'react';
import { useSearchParams } from 'next/navigation';
import {
  fetchJSON,
  mutateJSON,
  type Project,
  type ProjectsResponse,
  type VisualDiff,
  type VisualDiffsResponse,
} from '@/lib/api';

const STATUS_CLASS: Record<VisualDiff['status'], string> = {
  pending_review: 'sentinel-status-unknown',
  accepted: 'sentinel-status-ok',
  ignored: 'sentinel-status-bad',
};

function artifactURL(id: string) {
  return `/api/v1/artifacts/${id}/content`;
}

function DiffRow({ diff, onChanged }: { diff: VisualDiff; onChanged: (updated: VisualDiff) => void }) {
  const [busy, setBusy] = useState<'accept' | 'ignore' | null>(null);

  async function act(action: 'accept' | 'ignore') {
    setBusy(action);
    const result = await mutateJSON<VisualDiff>('POST', `/api/v1/visual-diffs/${diff.id}/${action}`);
    setBusy(null);
    if (result.ok && result.data) onChanged(result.data);
  }

  return (
    <div className="sentinel-card" style={{ marginBottom: '1rem' }}>
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'baseline', marginBottom: '0.6rem' }}>
        <span>
          <strong>{diff.percent_changed.toFixed(2)}% changed</strong>{' '}
          <span className={STATUS_CLASS[diff.status]}>{diff.status.replace('_', ' ')}</span>
        </span>
        <span className="sentinel-status-unknown" style={{ fontSize: '0.85rem' }}>
          {new Date(diff.created_at).toLocaleString()}
        </span>
      </div>

      <div style={{ display: 'flex', gap: '0.75rem', flexWrap: 'wrap' }}>
        <figure style={{ margin: 0, flex: '1 1 220px' }}>
          <figcaption className="sentinel-status-unknown" style={{ fontSize: '0.8rem' }}>
            Baseline
          </figcaption>
          {/* eslint-disable-next-line @next/next/no-img-element */}
          <img src={artifactURL(diff.baseline_artifact_id)} alt="Baseline screenshot" style={{ width: '100%', borderRadius: '6px' }} />
        </figure>
        <figure style={{ margin: 0, flex: '1 1 220px' }}>
          <figcaption className="sentinel-status-unknown" style={{ fontSize: '0.8rem' }}>
            Current run
          </figcaption>
          {/* eslint-disable-next-line @next/next/no-img-element */}
          <img src={artifactURL(diff.current_artifact_id)} alt="Current screenshot" style={{ width: '100%', borderRadius: '6px' }} />
        </figure>
        <figure style={{ margin: 0, flex: '1 1 220px' }}>
          <figcaption className="sentinel-status-unknown" style={{ fontSize: '0.8rem' }}>
            Diff (red = changed)
          </figcaption>
          {/* eslint-disable-next-line @next/next/no-img-element */}
          <img src={artifactURL(diff.diff_artifact_id)} alt="Visual diff highlighting changed pixels" style={{ width: '100%', borderRadius: '6px' }} />
        </figure>
      </div>

      {diff.status === 'pending_review' && (
        <div style={{ marginTop: '0.6rem', display: 'flex', gap: '0.4rem' }}>
          <button onClick={() => act('accept')} disabled={busy !== null}>
            {busy === 'accept' ? 'Accepting…' : 'Accept as new baseline'}
          </button>
          <button onClick={() => act('ignore')} disabled={busy !== null}>
            {busy === 'ignore' ? 'Ignoring…' : 'Ignore'}
          </button>
        </div>
      )}
    </div>
  );
}

function VisualDiffsContent() {
  const searchParams = useSearchParams();
  const initialProjectID = searchParams.get('project') ?? '';

  const [projects, setProjects] = useState<Project[]>([]);
  const [selectedID, setSelectedID] = useState(initialProjectID);
  const [diffs, setDiffs] = useState<VisualDiff[]>([]);
  const [loaded, setLoaded] = useState(false);

  useEffect(() => {
    fetchJSON<ProjectsResponse>('/api/v1/projects').then((res) => {
      const list = res?.projects ?? [];
      setProjects(list);
      if (!selectedID && list[0]) setSelectedID(list[0].id);
    });
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const loadDiffs = useCallback(async () => {
    if (!selectedID) return;
    const res = await fetchJSON<VisualDiffsResponse>(`/api/v1/projects/${selectedID}/visual-diffs`);
    setDiffs(res?.visual_diffs ?? []);
    setLoaded(true);
  }, [selectedID]);

  useEffect(() => {
    (async () => {
      await loadDiffs();
    })();
  }, [loadDiffs]);

  function updateDiff(updated: VisualDiff) {
    setDiffs((prev) => prev.map((d) => (d.id === updated.id ? updated : d)));
  }

  const pending = diffs.filter((d) => d.status === 'pending_review');
  const resolved = diffs.filter((d) => d.status !== 'pending_review');

  return (
    <>
      <h2>Visual Diffs</h2>
      <p>
        Every browser-based test run now captures a full-page screenshot, diffed against a stored
        baseline for that test case. A visual change never fails a test on its own — it&apos;s a
        separate signal you accept (making the new screenshot the baseline) or ignore. A test
        case&apos;s first-ever run always just establishes its baseline, with nothing to review here.
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
      </div>

      <div style={{ marginTop: '1rem' }}>
        {!loaded ? (
          <p className="sentinel-status-unknown">loading&hellip;</p>
        ) : diffs.length === 0 ? (
          <p className="sentinel-status-unknown">
            No visual diffs yet — run an approved page-rendering test case at least twice (the first
            run just sets the baseline).
          </p>
        ) : (
          <>
            {pending.length > 0 && (
              <>
                <h3>Pending review ({pending.length})</h3>
                {pending.map((d) => (
                  <DiffRow key={d.id} diff={d} onChanged={updateDiff} />
                ))}
              </>
            )}
            {resolved.length > 0 && (
              <>
                <h3>Resolved</h3>
                {resolved.map((d) => (
                  <DiffRow key={d.id} diff={d} onChanged={updateDiff} />
                ))}
              </>
            )}
          </>
        )}
      </div>
    </>
  );
}

export default function VisualDiffsPage() {
  return (
    <Suspense fallback={<p className="sentinel-status-unknown">loading&hellip;</p>}>
      <VisualDiffsContent />
    </Suspense>
  );
}
