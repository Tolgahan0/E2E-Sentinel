'use client';

import { Suspense, useEffect, useMemo, useState } from 'react';
import { useSearchParams } from 'next/navigation';
import { fetchJSON, type GraphEdge, type GraphNode, type GraphResponse, type Project, type ProjectsResponse } from '@/lib/api';

const CONFIDENCE_CLASS: Record<GraphNode['confidence'], string> = {
  high: 'sentinel-status-ok',
  medium: 'sentinel-status-unknown',
  low: 'sentinel-status-bad',
};

function EvidenceDrawer({ evidence }: { evidence: Record<string, unknown> }) {
  const [open, setOpen] = useState(false);
  return (
    <>
      <button onClick={() => setOpen((v) => !v)} style={{ fontSize: '0.8rem' }}>
        {open ? 'hide evidence' : 'show evidence'}
      </button>
      {open && (
        <pre
          style={{
            fontSize: '0.75rem',
            background: 'var(--sentinel-bg)',
            padding: '0.5rem',
            borderRadius: '6px',
            overflowX: 'auto',
            marginTop: '0.4rem',
          }}
        >
          {JSON.stringify(evidence, null, 2)}
        </pre>
      )}
    </>
  );
}

function ApplicationMapContent() {
  const searchParams = useSearchParams();
  const initialProjectID = searchParams.get('project') ?? '';

  const [projects, setProjects] = useState<Project[]>([]);
  const [selectedID, setSelectedID] = useState(initialProjectID);
  const [nodes, setNodes] = useState<GraphNode[]>([]);
  const [edges, setEdges] = useState<GraphEdge[]>([]);
  const [typeFilter, setTypeFilter] = useState('');
  const [search, setSearch] = useState('');

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
    fetchJSON<GraphResponse>(`/api/v1/projects/${selectedID}/graph`).then((res) => {
      setNodes(res?.nodes ?? []);
      setEdges(res?.edges ?? []);
    });
  }, [selectedID]);

  const nodeTypes = useMemo(() => Array.from(new Set(nodes.map((n) => n.node_type))).sort(), [nodes]);

  const filteredNodes = useMemo(() => {
    return nodes.filter((n) => {
      if (typeFilter && n.node_type !== typeFilter) return false;
      if (search && !n.label.toLowerCase().includes(search.toLowerCase())) return false;
      return true;
    });
  }, [nodes, typeFilter, search]);

  const visibleLabels = useMemo(() => new Set(filteredNodes.map((n) => n.label)), [filteredNodes]);
  const filteredEdges = useMemo(
    () => edges.filter((e) => visibleLabels.has(e.source_label) || visibleLabels.has(e.target_label)),
    [edges, visibleLabels],
  );

  return (
    <>
      <h2>Application Map</h2>
      <p>
        Evidence-backed relationships between pages, API endpoints, and Docker Compose services,
        built from the same discovery run as the Discovery page. Every edge below shows its
        confidence and the evidence that produced it — low-confidence edges are never presented as
        confirmed dependencies.
      </p>
      <p className="sentinel-status-unknown" style={{ fontSize: '0.85rem' }}>
        A zoomable graphical canvas is planned as a future enhancement; this list view already
        carries the full node/edge/evidence/confidence data model.
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
          Filter by node type
          <br />
          <select value={typeFilter} onChange={(e) => setTypeFilter(e.target.value)}>
            <option value="">All types</option>
            {nodeTypes.map((t) => (
              <option key={t} value={t}>
                {t}
              </option>
            ))}
          </select>
        </label>
        <label>
          Search
          <br />
          <input value={search} onChange={(e) => setSearch(e.target.value)} placeholder="label contains…" />
        </label>
      </div>

      {selectedID && nodes.length === 0 && (
        <div className="sentinel-card" style={{ marginTop: '1rem' }}>
          <p className="sentinel-status-unknown">
            No graph yet for this project — run discovery on the Discovery page first.
          </p>
        </div>
      )}

      {filteredEdges.length > 0 && (
        <div className="sentinel-card" style={{ marginTop: '1rem' }}>
          <h3>Relationships</h3>
          <table className="sentinel-table">
            <thead>
              <tr>
                <th>Source</th>
                <th>Relation</th>
                <th>Target</th>
                <th>Confidence</th>
                <th>Evidence</th>
              </tr>
            </thead>
            <tbody>
              {filteredEdges.map((e) => (
                <tr key={e.id}>
                  <td>
                    <code>{e.source_label}</code>
                    <br />
                    <span className="sentinel-status-unknown" style={{ fontSize: '0.75rem' }}>
                      {e.source_node_type}
                    </span>
                  </td>
                  <td>→ {e.relation_type} →</td>
                  <td>
                    <code>{e.target_label}</code>
                    <br />
                    <span className="sentinel-status-unknown" style={{ fontSize: '0.75rem' }}>
                      {e.target_node_type}
                    </span>
                  </td>
                  <td className={CONFIDENCE_CLASS[e.confidence]}>{e.confidence}</td>
                  <td>
                    <EvidenceDrawer evidence={e.evidence} />
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}

      {filteredNodes.length > 0 && (
        <div className="sentinel-card" style={{ marginTop: '1rem' }}>
          <h3>Nodes</h3>
          <table className="sentinel-table">
            <thead>
              <tr>
                <th>Type</th>
                <th>Label</th>
                <th>Source</th>
                <th>Confidence</th>
              </tr>
            </thead>
            <tbody>
              {filteredNodes.map((n) => (
                <tr key={n.id}>
                  <td>{n.node_type}</td>
                  <td>
                    <code>{n.label}</code>
                  </td>
                  <td>{n.source_reference || n.runtime_reference || '—'}</td>
                  <td className={CONFIDENCE_CLASS[n.confidence]}>{n.confidence}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </>
  );
}

export default function ApplicationMapPage() {
  return (
    <Suspense fallback={<p className="sentinel-status-unknown">loading&hellip;</p>}>
      <ApplicationMapContent />
    </Suspense>
  );
}
