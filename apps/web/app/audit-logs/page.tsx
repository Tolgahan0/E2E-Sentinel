'use client';

import { useEffect, useState } from 'react';
import { fetchJSON, type AuditEvent, type AuditEventsResponse } from '@/lib/api';

export default function AuditLogsPage() {
  const [events, setEvents] = useState<AuditEvent[]>([]);
  const [loaded, setLoaded] = useState(false);

  useEffect(() => {
    let cancelled = false;
    fetchJSON<AuditEventsResponse>('/api/v1/audit-events?limit=200').then((res) => {
      if (cancelled) return;
      setEvents(res?.events ?? []);
      setLoaded(true);
    });
    return () => {
      cancelled = true;
    };
  }, []);

  return (
    <section className="sentinel-card">
      <h2>Audit Logs</h2>
      <p>
        Every meaningful operation in E2E Sentinel is recorded here as an append-only event. In
        Phase 0 this covers process startup and shutdown; every later phase adds its own events
        (project added, test approved, patch applied, etc.) to this same log.
      </p>
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
              <th>Resource Type</th>
              <th>Resource ID</th>
              <th>Actor</th>
            </tr>
          </thead>
          <tbody>
            {events.map((event) => (
              <tr key={event.ID}>
                <td>{new Date(event.CreatedAt).toLocaleString()}</td>
                <td>{event.ActionType}</td>
                <td>{event.ResourceType}</td>
                <td>{event.ResourceID || '—'}</td>
                <td>{event.Actor}</td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </section>
  );
}
