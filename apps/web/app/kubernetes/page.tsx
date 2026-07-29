'use client';

import { Suspense, useEffect, useState } from 'react';
import { useSearchParams } from 'next/navigation';
import {
  fetchJSON,
  mutateJSON,
  type KubeDiscoverResponse,
  type KubeEventsResponse,
  type KubePodLogsResponse,
  type KubeResource,
  type KubeResourceStatus,
  type KubeResourcesResponse,
  type KubeStatusResponse,
  type Project,
  type ProjectsResponse,
} from '@/lib/api';

const STATUS_CLASS: Record<KubeResourceStatus, string> = {
  healthy: 'sentinel-status-ok',
  degraded: 'sentinel-status-bad',
  unknown: 'sentinel-status-unknown',
  not_applicable: 'sentinel-status-unknown',
};

function ResourcesTable({ resources }: { resources: KubeResource[] }) {
  if (resources.length === 0) {
    return <p className="sentinel-status-unknown">No Kubernetes resources discovered yet for this project.</p>;
  }
  return (
    <table className="sentinel-table">
      <thead>
        <tr>
          <th>Namespace</th>
          <th>Kind</th>
          <th>Name</th>
          <th>Replicas</th>
          <th>Restarts</th>
          <th>Status</th>
        </tr>
      </thead>
      <tbody>
        {resources.map((r) => (
          <tr key={r.id}>
            <td>{r.namespace}</td>
            <td>{r.kind}</td>
            <td>{r.name}</td>
            <td>{r.desired_replicas !== null ? `${r.ready_replicas ?? 0}/${r.desired_replicas}` : '—'}</td>
            <td>{r.restart_count !== null ? r.restart_count : '—'}</td>
            <td className={STATUS_CLASS[r.status]}>{r.status.replace('_', ' ')}</td>
          </tr>
        ))}
      </tbody>
    </table>
  );
}

function EventsViewer({ projectID }: { projectID: string }) {
  const [namespace, setNamespace] = useState('');
  const [events, setEvents] = useState<KubeEventsResponse['events']>(null);
  const [loading, setLoading] = useState(false);

  async function load() {
    setLoading(true);
    const query = namespace ? `?namespace=${encodeURIComponent(namespace)}` : '';
    const res = await fetchJSON<KubeEventsResponse>(`/api/v1/projects/${projectID}/kube/events${query}`);
    setEvents(res?.events ?? []);
    setLoading(false);
  }

  return (
    <div className="sentinel-card" style={{ marginTop: '1rem' }}>
      <h3>Cluster events</h3>
      <div style={{ display: 'flex', gap: '0.75rem', alignItems: 'flex-end', flexWrap: 'wrap' }}>
        <label>
          Namespace (optional)
          <br />
          <input value={namespace} onChange={(e) => setNamespace(e.target.value)} placeholder="leave blank for default scope" />
        </label>
        <button onClick={load} disabled={loading}>
          {loading ? 'Loading…' : 'Fetch events'}
        </button>
      </div>
      {events && (
        events.length === 0 ? (
          <p className="sentinel-status-unknown" style={{ marginTop: '0.75rem' }}>No events.</p>
        ) : (
          <table className="sentinel-table" style={{ marginTop: '0.75rem' }}>
            <thead>
              <tr>
                <th>Namespace</th>
                <th>Involved object</th>
                <th>Reason</th>
                <th>Message</th>
                <th>Type</th>
                <th>Count</th>
                <th>Last seen</th>
              </tr>
            </thead>
            <tbody>
              {events.map((e, i) => (
                <tr key={i}>
                  <td>{e.namespace}</td>
                  <td>{e.involved_kind}/{e.involved_name}</td>
                  <td>{e.reason}</td>
                  <td>{e.message}</td>
                  <td className={e.type === 'Warning' ? 'sentinel-status-bad' : 'sentinel-status-unknown'}>{e.type}</td>
                  <td>{e.count}</td>
                  <td>{e.last_timestamp}</td>
                </tr>
              ))}
            </tbody>
          </table>
        )
      )}
    </div>
  );
}

function PodLogsViewer({ projectID }: { projectID: string }) {
  const [namespace, setNamespace] = useState('');
  const [pod, setPod] = useState('');
  const [container, setContainer] = useState('');
  const [tailLines, setTailLines] = useState(200);
  const [logs, setLogs] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(false);

  async function fetchLogs() {
    if (!pod) return;
    setLoading(true);
    setError(null);
    const params = new URLSearchParams();
    if (namespace) params.set('namespace', namespace);
    if (container) params.set('container', container);
    params.set('tail_lines', String(tailLines));
    const res = await fetch(`/api/v1/projects/${projectID}/kube/pods/${encodeURIComponent(pod)}/logs?${params}`, { cache: 'no-store' });
    const data = (await res.json().catch(() => null)) as (KubePodLogsResponse & { error?: string }) | null;
    setLoading(false);
    if (!res.ok || !data) {
      setError(data?.error ?? 'request_failed');
      setLogs(null);
      return;
    }
    setLogs(data.logs);
  }

  return (
    <div className="sentinel-card" style={{ marginTop: '1rem' }}>
      <h3>Pod logs</h3>
      <p className="sentinel-status-unknown" style={{ fontSize: '0.85rem' }}>
        Read-only, non-streaming — the most recent lines only, never a live tail.
      </p>
      <div style={{ display: 'flex', gap: '0.75rem', alignItems: 'flex-end', flexWrap: 'wrap' }}>
        <label>
          Namespace
          <br />
          <input value={namespace} onChange={(e) => setNamespace(e.target.value)} placeholder="leave blank for default scope" />
        </label>
        <label>
          Pod name
          <br />
          <input value={pod} onChange={(e) => setPod(e.target.value)} placeholder="web-abc123" />
        </label>
        <label>
          Container (optional)
          <br />
          <input value={container} onChange={(e) => setContainer(e.target.value)} />
        </label>
        <label>
          Tail lines
          <br />
          <input type="number" value={tailLines} onChange={(e) => setTailLines(Number(e.target.value) || 200)} style={{ width: '6rem' }} />
        </label>
        <button onClick={fetchLogs} disabled={!pod || loading}>
          {loading ? 'Fetching…' : 'Fetch logs'}
        </button>
      </div>
      {error && <p className="sentinel-status-bad">{error}</p>}
      {logs !== null && (
        <pre className="sentinel-card" style={{ marginTop: '0.75rem', maxHeight: '20rem', overflow: 'auto', whiteSpace: 'pre-wrap' }}>
          {logs || '(empty)'}
        </pre>
      )}
    </div>
  );
}

function KubernetesContent() {
  const searchParams = useSearchParams();
  const initialProjectID = searchParams.get('project') ?? '';

  const [kubeStatus, setKubeStatus] = useState<KubeStatusResponse | null>(null);
  const [projects, setProjects] = useState<Project[]>([]);
  const [selectedID, setSelectedID] = useState(initialProjectID);
  const [resources, setResources] = useState<KubeResource[]>([]);
  const [warnings, setWarnings] = useState<string[]>([]);
  const [discovering, setDiscovering] = useState(false);

  useEffect(() => {
    fetchJSON<KubeStatusResponse>('/api/v1/kube/status').then(setKubeStatus);
    fetchJSON<ProjectsResponse>('/api/v1/projects').then((res) => {
      const list = res?.projects ?? [];
      setProjects(list);
      if (!selectedID && list[0]) setSelectedID(list[0].id);
    });
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  useEffect(() => {
    if (!selectedID) return;
    fetchJSON<KubeResourcesResponse>(`/api/v1/projects/${selectedID}/kube-resources`).then((res) => {
      setResources(res?.resources ?? []);
    });
  }, [selectedID]);

  async function runDiscovery() {
    if (!selectedID) return;
    setDiscovering(true);
    const result = await mutateJSON<KubeDiscoverResponse>('POST', `/api/v1/projects/${selectedID}/kube-discover`);
    setDiscovering(false);
    if (result.ok && result.data) {
      setResources(result.data.resources ?? []);
      setWarnings(result.data.warnings ?? []);
    }
  }

  return (
    <>
      <h2>Kubernetes</h2>
      <p>
        Read-only cluster discovery (spec §7.5): namespaces, workloads, pod health, restart counts,
        service/ingress mapping, events, and logs. Never applies or modifies any cluster resource.
      </p>

      {kubeStatus && !kubeStatus.configured && (
        <div className="sentinel-card">
          <p className="sentinel-status-unknown">
            Kubernetes discovery is not configured (<code>SENTINEL_KUBE_CONFIG_PATH</code> unset, and this
            process is not running inside a cluster). See{' '}
            <code>docs/KUBERNETES_DISCOVERY.md</code> to set it up.
          </p>
        </div>
      )}

      {kubeStatus?.configured && (
        <>
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
            <button onClick={runDiscovery} disabled={!selectedID || discovering}>
              {discovering ? 'Discovering…' : 'Run discovery'}
            </button>
            {kubeStatus.namespace && (
              <span className="sentinel-status-unknown">scope: {kubeStatus.namespace}</span>
            )}
          </div>

          {warnings.length > 0 && (
            <div className="sentinel-card" style={{ marginTop: '1rem' }}>
              <p className="sentinel-status-bad">
                Some resource kinds could not be listed (RBAC restriction or a CRD not installed):
              </p>
              <ul>
                {warnings.map((w, i) => (
                  <li key={i}>{w}</li>
                ))}
              </ul>
            </div>
          )}

          <div className="sentinel-card" style={{ marginTop: '1rem' }}>
            {projects.length === 0 ? (
              <p className="sentinel-status-unknown">Add a project on the Projects page first.</p>
            ) : (
              <ResourcesTable resources={resources} />
            )}
          </div>

          {selectedID && (
            <>
              <EventsViewer projectID={selectedID} />
              <PodLogsViewer projectID={selectedID} />
            </>
          )}
        </>
      )}
    </>
  );
}

export default function KubernetesPage() {
  return (
    <Suspense fallback={<p className="sentinel-status-unknown">loading&hellip;</p>}>
      <KubernetesContent />
    </Suspense>
  );
}
