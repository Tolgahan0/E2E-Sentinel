'use client';

import { Suspense, useEffect, useMemo, useState } from 'react';
import { useSearchParams } from 'next/navigation';
import { fetchJSON, mutateJSON, type DiscoveryFinding, type DiscoveryResponse, type Project, type ProjectsResponse } from '@/lib/api';

const CONFIDENCE_CLASS: Record<DiscoveryFinding['confidence'], string> = {
  high: 'sentinel-status-ok',
  medium: 'sentinel-status-unknown',
  low: 'sentinel-status-bad',
};

function DiscoveryContent() {
  const searchParams = useSearchParams();
  const initialProjectID = searchParams.get('project') ?? '';

  const [projects, setProjects] = useState<Project[]>([]);
  const [selectedID, setSelectedID] = useState(initialProjectID);
  const [findings, setFindings] = useState<DiscoveryFinding[] | null>(null);
  const [status, setStatus] = useState<'idle' | 'scanning' | 'not_found'>('idle');

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
    fetchJSON<DiscoveryResponse>(`/api/v1/projects/${selectedID}/discovery`).then((res) => {
      if (!res) {
        setFindings(null);
        setStatus('not_found');
        return;
      }
      setFindings(res.findings ?? []);
      setStatus('idle');
    });
  }, [selectedID]);

  async function runDiscovery() {
    if (!selectedID) return;
    setStatus('scanning');
    const result = await mutateJSON<DiscoveryResponse>('POST', `/api/v1/projects/${selectedID}/discover`);
    if (result.ok && result.data) {
      setFindings(result.data.findings ?? []);
    }
    setStatus('idle');
  }

  const grouped = useMemo(() => {
    const map = new Map<string, DiscoveryFinding[]>();
    for (const f of findings ?? []) {
      const list = map.get(f.category) ?? [];
      list.push(f);
      map.set(f.category, list);
    }
    return Array.from(map.entries()).sort(([a], [b]) => a.localeCompare(b));
  }, [findings]);

  return (
    <>
      <h2>Discovery</h2>
      <p>
        Deterministic, evidence-based repository scanning: languages, frameworks, Docker files, CI
        pipelines, existing test tooling, and API schemas — each with the file path that proved it and
        a confidence level. Nothing here is AI-inferred.
      </p>

      <div className="sentinel-card" style={{ display: 'flex', gap: '0.75rem', alignItems: 'center', flexWrap: 'wrap' }}>
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
        <button onClick={runDiscovery} disabled={!selectedID || status === 'scanning'}>
          {status === 'scanning' ? 'Scanning…' : 'Run discovery'}
        </button>
      </div>

      <div className="sentinel-card" style={{ marginTop: '1rem' }}>
        {projects.length === 0 ? (
          <p className="sentinel-status-unknown">Add a project on the Projects page first.</p>
        ) : status === 'not_found' ? (
          <p className="sentinel-status-unknown">No discovery run yet for this project. Click &quot;Run discovery&quot;.</p>
        ) : grouped.length === 0 ? (
          <p className="sentinel-status-unknown">No findings.</p>
        ) : (
          grouped.map(([category, items]) => (
            <div key={category} style={{ marginBottom: '1.25rem' }}>
              <h3 style={{ textTransform: 'capitalize' }}>{category.replace('_', ' ')}</h3>
              <table className="sentinel-table">
                <thead>
                  <tr>
                    <th>Name</th>
                    <th>Evidence path</th>
                    <th>Confidence</th>
                  </tr>
                </thead>
                <tbody>
                  {items.map((f) => (
                    <tr key={f.category + f.name}>
                      <td>{f.name}</td>
                      <td>
                        <code>{f.path}</code>
                      </td>
                      <td className={CONFIDENCE_CLASS[f.confidence]}>{f.confidence}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          ))
        )}
      </div>
    </>
  );
}

export default function DiscoveryPage() {
  return (
    <Suspense fallback={<p className="sentinel-status-unknown">loading&hellip;</p>}>
      <DiscoveryContent />
    </Suspense>
  );
}
