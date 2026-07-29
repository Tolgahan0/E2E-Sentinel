'use client';

import Link from 'next/link';
import { useEffect, useState, type CSSProperties, type ReactNode } from 'react';
import {
  fetchJSON,
  type AuditEvent,
  type AuditEventsResponse,
  type BugsResponse,
  type FixProposalsResponse,
  type HealthResponse,
  type Project,
  type ProjectsResponse,
  type ProvidersResponse,
  type ReadyResponse,
  type RunsResponse,
  type TestRun,
  type TestsResponse,
} from '@/lib/api';

function StatusBadge({ label, ok }: { label: string; ok: boolean | null }) {
  const className = ok === null ? 'sentinel-status-unknown' : ok ? 'sentinel-status-ok' : 'sentinel-status-bad';
  const text = ok === null ? 'unknown' : ok ? 'ok' : 'unreachable';
  return (
    <p className={className}>
      {label}: {text}
    </p>
  );
}

interface PipelineStats {
  projectsTotal: number;
  projectsDiscovered: number;
  testsTotal: number;
  testsPendingApproval: number;
  testsApproved: number;
  runningNow: TestRun[];
  passRate: number | null; // 0-100 over the most recent completed runs, or null if none yet
  recentRunsConsidered: number;
  openBugs: number;
  pendingFixProposals: number;
  aiProvidersEnabled: number;
  aiProvidersHealthy: number;
}

const EMPTY_STATS: PipelineStats = {
  projectsTotal: 0,
  projectsDiscovered: 0,
  testsTotal: 0,
  testsPendingApproval: 0,
  testsApproved: 0,
  runningNow: [],
  passRate: null,
  recentRunsConsidered: 0,
  openBugs: 0,
  pendingFixProposals: 0,
  aiProvidersEnabled: 0,
  aiProvidersHealthy: 0,
};

// Aggregates across every project by fetching each project's tests/runs/
// fix-proposals in parallel — there is no cross-project aggregate
// endpoint on the API (each of those is scoped to one project), so this
// is an N+1 client-side rollup. Fine at this project count; a
// deployment with hundreds of projects would want a real aggregate
// endpoint instead.
async function loadPipelineStats(projects: Project[]): Promise<PipelineStats> {
  const [bugsRes, providersRes] = await Promise.all([
    fetchJSON<BugsResponse>('/api/v1/bugs'),
    fetchJSON<ProvidersResponse>('/api/v1/providers'),
  ]);

  const perProject = await Promise.all(
    projects.map(async (p) => {
      const [testsRes, runsRes, fixRes] = await Promise.all([
        fetchJSON<TestsResponse>(`/api/v1/projects/${p.id}/tests`),
        fetchJSON<RunsResponse>(`/api/v1/projects/${p.id}/runs`),
        fetchJSON<FixProposalsResponse>(`/api/v1/projects/${p.id}/fix-proposals`),
      ]);
      return {
        tests: testsRes?.tests ?? [],
        runs: runsRes?.runs ?? [],
        fixProposals: fixRes?.fix_proposals ?? [],
      };
    })
  );

  const allTests = perProject.flatMap((p) => p.tests);
  const allRuns = perProject.flatMap((p) => p.runs);
  const allFixProposals = perProject.flatMap((p) => p.fixProposals);

  const runningNow = allRuns.filter((r) => r.status === 'running' || r.status === 'queued');

  const finishedRuns = allRuns
    .filter((r) => r.status === 'passed' || r.status === 'failed')
    .sort((a, b) => new Date(b.started_at).getTime() - new Date(a.started_at).getTime())
    .slice(0, 50);
  const passRate =
    finishedRuns.length === 0
      ? null
      : Math.round((100 * finishedRuns.filter((r) => r.status === 'passed').length) / finishedRuns.length);

  const bugs = bugsRes?.bugs ?? [];
  const providers = providersRes?.providers ?? [];

  return {
    projectsTotal: projects.length,
    projectsDiscovered: projects.filter((p) => p.discovery_status === 'completed').length,
    testsTotal: allTests.length,
    testsPendingApproval: allTests.filter((t) => t.approval_status === 'pending').length,
    testsApproved: allTests.filter((t) => t.approval_status === 'approved').length,
    runningNow,
    passRate,
    recentRunsConsidered: finishedRuns.length,
    openBugs: bugs.filter((b) => b.status === 'open').length,
    pendingFixProposals: allFixProposals.filter((fp) => fp.approval_status === 'pending_review').length,
    aiProvidersEnabled: providers.filter((p) => p.enabled).length,
    aiProvidersHealthy: providers.filter((p) => p.health_status === 'ok').length,
  };
}

function PipelineStage({
  href,
  label,
  value,
  detail,
  attention,
}: {
  href: string;
  label: string;
  value: ReactNode;
  detail?: string;
  attention?: boolean;
}) {
  return (
    <Link href={href} className="sentinel-pipeline-stage" data-attention={attention ? 'true' : undefined}>
      <span className="sentinel-pipeline-stage-label">{label}</span>
      <span className="sentinel-pipeline-stage-value">{value}</span>
      {detail && <span className="sentinel-pipeline-stage-detail">{detail}</span>}
    </Link>
  );
}

function PipelineConnector({ index }: { index: number }) {
  return (
    <div className="sentinel-pipeline-connector" aria-hidden="true">
      <div className="sentinel-pipeline-connector-pulse" style={{ '--sentinel-flow-delay': `${index * 0.35}s` } as CSSProperties} />
    </div>
  );
}

function PipelineFlow({ stats, loaded }: { stats: PipelineStats; loaded: boolean }) {
  if (!loaded) {
    return (
      <div className="sentinel-card">
        <p className="sentinel-status-unknown">loading pipeline status&hellip;</p>
      </div>
    );
  }

  return (
    <div className="sentinel-card">
      <h3 style={{ marginTop: 0 }}>Pipeline — where everything is right now</h3>
      <p className="sentinel-status-unknown" style={{ fontSize: '0.85rem', marginTop: '-0.5rem' }}>
        Each stage links to the page that explains it. A red badge means something there is
        waiting on a human decision or needs attention.
      </p>
      <div className="sentinel-pipeline">
        <PipelineStage
          href="/discovery"
          label="1. Discover"
          value={`${stats.projectsDiscovered}/${stats.projectsTotal} projects`}
          detail="repo scan + Docker/K8s"
        />
        <PipelineConnector index={0} />
        <PipelineStage
          href="/test-inventory"
          label="2. Plan"
          value={`${stats.testsTotal} test cases`}
          detail="deterministic, no AI required"
        />
        <PipelineConnector index={1} />
        <PipelineStage
          href="/approvals"
          label="3. Approve"
          value={`${stats.testsPendingApproval} pending`}
          detail={`${stats.testsApproved} approved`}
          attention={stats.testsPendingApproval > 0}
        />
        <PipelineConnector index={2} />
        <PipelineStage
          href="/runs"
          label="4. Run"
          value={
            stats.runningNow.length > 0 ? (
              <span className="sentinel-pipeline-stage-live">{stats.runningNow.length} running now</span>
            ) : (
              'idle'
            )
          }
          detail={stats.passRate === null ? 'no completed runs yet' : `${stats.passRate}% pass rate (last ${stats.recentRunsConsidered})`}
          attention={stats.runningNow.length > 0}
        />
        <PipelineConnector index={3} />
        <PipelineStage
          href="/bugs"
          label="5. Correlate failures"
          value={`${stats.openBugs} open bugs`}
          detail="auto-classified, deduplicated"
          attention={stats.openBugs > 0}
        />
        <PipelineConnector index={4} />
        <PipelineStage
          href="/fix-proposals"
          label="6. Fix"
          value={`${stats.pendingFixProposals} awaiting review`}
          detail="AI-assisted, human-approved"
          attention={stats.pendingFixProposals > 0}
        />
      </div>
    </div>
  );
}

export default function DashboardPage() {
  const [health, setHealth] = useState<HealthResponse | null>(null);
  const [ready, setReady] = useState<ReadyResponse | null>(null);
  const [events, setEvents] = useState<AuditEvent[]>([]);
  const [stats, setStats] = useState<PipelineStats>(EMPTY_STATS);
  const [loaded, setLoaded] = useState(false);

  useEffect(() => {
    let cancelled = false;

    async function load() {
      const [healthRes, readyRes, auditRes, projectsRes] = await Promise.all([
        fetchJSON<HealthResponse>('/api/health'),
        fetchJSON<ReadyResponse>('/api/ready'),
        fetchJSON<AuditEventsResponse>('/api/v1/audit-events?limit=10'),
        fetchJSON<ProjectsResponse>('/api/v1/projects'),
      ]);
      if (cancelled) return;
      setHealth(healthRes);
      setReady(readyRes);
      setEvents(auditRes?.events ?? []);

      const projects = projectsRes?.projects ?? [];
      const pipelineStats = await loadPipelineStats(projects);
      if (cancelled) return;
      setStats(pipelineStats);
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

      <PipelineFlow stats={stats} loaded={loaded} />

      {loaded && stats.runningNow.length > 0 && (
        <section className="sentinel-card" style={{ marginTop: '1rem' }}>
          <h3 style={{ marginTop: 0 }}>
            <span className="sentinel-pipeline-stage-live">Currently running</span>
          </h3>
          <table className="sentinel-table">
            <thead>
              <tr>
                <th>Run</th>
                <th>Status</th>
                <th>Started</th>
              </tr>
            </thead>
            <tbody>
              {stats.runningNow.map((r) => (
                <tr key={r.id}>
                  <td>
                    <Link href={`/runs?project=${r.project_id}`}>{r.id}</Link>
                  </td>
                  <td className="sentinel-status-unknown">{r.status}</td>
                  <td>{new Date(r.started_at).toLocaleString()}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </section>
      )}

      <div className="sentinel-grid" style={{ marginTop: '1rem' }}>
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
          <h3>AI providers</h3>
          {!loaded ? (
            <p className="sentinel-status-unknown">checking&hellip;</p>
          ) : stats.aiProvidersEnabled === 0 ? (
            <p className="sentinel-status-unknown">
              None configured — <Link href="/ai-providers">fully optional</Link>, everything else works without it.
            </p>
          ) : (
            <p>
              <Link href="/ai-providers">
                {stats.aiProvidersHealthy}/{stats.aiProvidersEnabled} enabled providers healthy
              </Link>
            </p>
          )}
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
