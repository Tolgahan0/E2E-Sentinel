'use client';

import { Suspense, useEffect, useMemo, useState } from 'react';
import { useSearchParams } from 'next/navigation';
import {
  fetchJSON,
  mutateJSON,
  type DiscoveredService,
  type DiscoveryFinding,
  type DiscoveryResponse,
  type Project,
  type ProjectsResponse,
  type ServicesResponse,
} from '@/lib/api';

const CONFIDENCE_CLASS: Record<DiscoveryFinding['confidence'], string> = {
  high: 'sentinel-status-ok',
  medium: 'sentinel-status-unknown',
  low: 'sentinel-status-bad',
};

function ServiceStatusBadge({ service }: { service: DiscoveredService }) {
  const status = service.metadata.status;
  if (status === 'running') return <span className="sentinel-status-ok">running</span>;
  if (status === 'unknown' || !status) return <span className="sentinel-status-unknown">not observed</span>;
  return <span className="sentinel-status-bad">{status}</span>;
}

function ServicesTable({ services }: { services: DiscoveredService[] }) {
  if (services.length === 0) {
    return (
      <p className="sentinel-status-unknown">
        No Docker Compose services found. If this project uses Compose, run discovery again after
        adding a docker-compose.yml.
      </p>
    );
  }

  return (
    <table className="sentinel-table">
      <thead>
        <tr>
          <th>Name</th>
          <th>Kind</th>
          <th>Image</th>
          <th>Ports</th>
          <th>Depends on</th>
          <th>Status</th>
          <th>Confidence</th>
        </tr>
      </thead>
      <tbody>
        {services.map((s) => (
          <tr key={s.id}>
            <td>{s.name}</td>
            <td>{s.kind}</td>
            <td>
              <code>{s.image || (s.metadata.has_build ? 'built locally' : '—')}</code>
            </td>
            <td>{s.ports.length > 0 ? s.ports.join(', ') : '—'}</td>
            <td>{s.dependencies.length > 0 ? s.dependencies.join(', ') : '—'}</td>
            <td>
              <ServiceStatusBadge service={s} />
            </td>
            <td className={CONFIDENCE_CLASS[s.confidence]}>{s.confidence}</td>
          </tr>
        ))}
      </tbody>
    </table>
  );
}

function DiscoveryContent() {
  const searchParams = useSearchParams();
  const initialProjectID = searchParams.get('project') ?? '';

  const [projects, setProjects] = useState<Project[]>([]);
  const [selectedID, setSelectedID] = useState(initialProjectID);
  const [findings, setFindings] = useState<DiscoveryFinding[] | null>(null);
  const [services, setServices] = useState<DiscoveredService[]>([]);
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
    fetchJSON<ServicesResponse>(`/api/v1/projects/${selectedID}/services`).then((res) => {
      setServices(res?.services ?? []);
    });
  }, [selectedID]);

  async function runDiscovery() {
    if (!selectedID) return;
    setStatus('scanning');
    const result = await mutateJSON<DiscoveryResponse>('POST', `/api/v1/projects/${selectedID}/discover`);
    if (result.ok && result.data) {
      setFindings(result.data.findings ?? []);
    }
    const servicesRes = await fetchJSON<ServicesResponse>(`/api/v1/projects/${selectedID}/services`);
    setServices(servicesRes?.services ?? []);
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

      {selectedID && (services.length > 0 || (findings && findings.length > 0)) && (
        <div className="sentinel-card" style={{ marginTop: '1rem' }}>
          <h3>Docker Compose services</h3>
          <p className="sentinel-status-unknown" style={{ fontSize: '0.85rem' }}>
            Running status requires the Docker daemon to be reachable from sentinel-api (see
            docs/DOCKER_DISCOVERY.md) — otherwise every service shows &quot;not observed&quot;, which is
            distinct from confirming it isn&apos;t running.
          </p>
          <ServicesTable services={services} />
        </div>
      )}

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
