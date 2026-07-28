'use client';

import { useEffect, useState } from 'react';
import { fetchJSON, type AuditEvent, type AuditEventsResponse, type HealthResponse, type ReadyResponse } from '@/lib/api';

function StatusBadge({ label, ok }: { label: string; ok: boolean | null }) {
  const className = ok === null ? 'sentinel-status-unknown' : ok ? 'sentinel-status-ok' : 'sentinel-status-bad';
  const text = ok === null ? 'unknown' : ok ? 'ok' : 'unreachable';
  return (
    <p className={className}>
      {label}: {text}
    </p>
  );
}

export default function DashboardPage() {
  const [health, setHealth] = useState<HealthResponse | null>(null);
  const [ready, setReady] = useState<ReadyResponse | null>(null);
  const [events, setEvents] = useState<AuditEvent[]>([]);
  const [loaded, setLoaded] = useState(false);

  useEffect(() => {
    let cancelled = false;

    async function load() {
      const [healthRes, readyRes, auditRes] = await Promise.all([
        fetchJSON<HealthResponse>('/api/health'),
        fetchJSON<ReadyResponse>('/api/ready'),
        fetchJSON<AuditEventsResponse>('/api/v1/audit-events?limit=10'),
      ]);
      if (cancelled) return;
      setHealth(healthRes);
      setReady(readyRes);
      setEvents(auditRes?.events ?? []);
      setLoaded(true);
    }

    void load();
    return () => {
      cancelled = true;
    };
  }, []);

  return (
    <>
      <h2>Dashboard</h2>
      <p>
        E2E Sentinel analyzes repository structure, runtime services, API schemas, application
        routes, existing tests, and observed behavior to generate high-confidence test
        recommendations and evidence-backed failure reports.
      </p>

      <div className="sentinel-grid">
        <div className="sentinel-card">
          <h3>API health</h3>
          {!loaded ? (
            <p className="sentinel-status-unknown">checking&hellip;</p>
          ) : (
            <StatusBadge label="sentinel-api" ok={health?.status === 'ok'} />
          )}
        </div>

        <div className="sentinel-card">
          <h3>Readiness</h3>
          {!loaded ? (
            <p className="sentinel-status-unknown">checking&hellip;</p>
          ) : ready ? (
            <>
              {Object.entries(ready.checks).map(([dep, status]) => (
                <StatusBadge key={dep} label={dep} ok={status === 'ok'} />
              ))}
            </>
          ) : (
            <p className="sentinel-status-bad">sentinel-api unreachable</p>
          )}
        </div>

        <div className="sentinel-card">
          <h3>Projects</h3>
          <p className="sentinel-status-unknown">Available from Phase 1 (Project &amp; Repository Discovery).</p>
        </div>

        <div className="sentinel-card">
          <h3>Runs &amp; pass rate</h3>
          <p className="sentinel-status-unknown">Available from Phase 5 (Playwright Runner).</p>
        </div>

        <div className="sentinel-card">
          <h3>Pending approvals</h3>
          <p className="sentinel-status-unknown">Available from Phase 4 (Test Planning).</p>
        </div>

        <div className="sentinel-card">
          <h3>AI provider health</h3>
          <p className="sentinel-status-unknown">Available from Phase 6 (AI Providers). AI is optional and off by default.</p>
        </div>
      </div>

      <section className="sentinel-card" style={{ marginTop: '1rem' }}>
        <h3>Recent audit events</h3>
        {!loaded ? (
          <p className="sentinel-status-unknown">loading&hellip;</p>
        ) : events.length === 0 ? (
          <p className="sentinel-status-unknown">No audit events recorded yet.</p>
        ) : (
          <table className="sentinel-table">
            <thead>
              <tr>
                <th>Time</th>
                <th>Action</th>
                <th>Resource</th>
                <th>Actor</th>
              </tr>
            </thead>
            <tbody>
              {events.map((event) => (
                <tr key={event.ID}>
                  <td>{new Date(event.CreatedAt).toLocaleString()}</td>
                  <td>{event.ActionType}</td>
                  <td>{event.ResourceType}</td>
                  <td>{event.Actor}</td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </section>
    </>
  );
}
