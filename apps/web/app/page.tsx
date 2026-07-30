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
  runsPassed: number;
  runsFailed: number;
  openBugs: number;
  resolvedBugs: number;
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
  runsPassed: 0,
  runsFailed: 0,
  openBugs: 0,
  resolvedBugs: 0,
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
    runsPassed: allRuns.filter((r) => r.status === 'passed').length,
    runsFailed: allRuns.filter((r) => r.status === 'failed' || r.status === 'error').length,
    openBugs: bugs.filter((b) => b.status === 'open' || b.status === 'reopened').length,
    resolvedBugs: bugs.filter((b) => b.status === 'resolved').length,
    pendingFixProposals: allFixProposals.filter((fp) => fp.approval_status === 'pending_review').length,
    aiProvidersEnabled: providers.filter((p) => p.enabled).length,
    aiProvidersHealthy: providers.filter((p) => p.health_status === 'ok').length,
  };
}

// The flow map's own coordinate space — an arbitrary but fixed canvas
// that the SVG viewBox and the HTML overlay's percentage positions both
// reference, so the two layers always land on the same points
// regardless of the rendered size (see .sentinel-flowmap in globals.css).
const FLOW_W = 1000;
const FLOW_H = 380;
const STAGE_ANCHOR_X = 195;
const STAGE_Y: [number, number, number, number, number, number] = [32, 95, 158, 221, 284, 347];
const HUB_X = 340;
const HUB_Y = 190;
const HUB_R = 62;
const CLUSTER_X = 660;
const CLUSTER_Y = 190;
const CLUSTER_R = 96;
const CLUSTER_CENTER_R = 44;
const ISSUE_ANCHOR_X = 805;
const ISSUE_Y: [number, number, number] = [108, 190, 272];

function pct(x: number, total: number) {
  return `${(x / total) * 100}%`;
}

// A gentle horizontal S-curve between two points — the same shape used
// throughout for both the fan-in (stages -> hub) and fan-out
// (cluster -> issues) tracks.
function flowCurve(x1: number, y1: number, x2: number, y2: number) {
  const midX = x1 + (x2 - x1) / 2;
  return `M ${x1} ${y1} C ${midX} ${y1}, ${midX} ${y2}, ${x2} ${y2}`;
}

// Deterministic "sunflower seed" scatter (golden-angle spiral) so the
// cluster's per-run dots are stable across re-renders without resorting
// to Math.random(), which would make them jitter on every poll.
function scatterPoint(index: number, total: number, minR: number, maxR: number) {
  const goldenAngle = Math.PI * (3 - Math.sqrt(5));
  const angle = index * goldenAngle;
  const frac = total <= 1 ? 1 : index / (total - 1);
  const radius = minR + frac * (maxR - minR);
  return { x: Math.cos(angle) * radius, y: Math.sin(angle) * radius };
}

function usePrefersReducedMotion() {
  const [reduced, setReduced] = useState(
    () => typeof window !== 'undefined' && window.matchMedia('(prefers-reduced-motion: reduce)').matches
  );
  useEffect(() => {
    const mq = window.matchMedia('(prefers-reduced-motion: reduce)');
    const onChange = (e: MediaQueryListEvent) => setReduced(e.matches);
    mq.addEventListener('change', onChange);
    return () => mq.removeEventListener('change', onChange);
  }, []);
  return reduced;
}

interface FlowNode {
  key: string;
  href: string;
  label: string;
  value: ReactNode;
  color: string;
  attention?: boolean;
}

function FlowMapNode({ node, y, side }: { node: FlowNode; y: number; side: 'left' | 'right' }) {
  return (
    <Link
      href={node.href}
      className={side === 'left' ? 'sentinel-flowmap-node' : 'sentinel-flowmap-node sentinel-flowmap-node-right'}
      data-attention={node.attention ? 'true' : undefined}
      style={
        {
          left: pct(side === 'left' ? STAGE_ANCHOR_X : ISSUE_ANCHOR_X, FLOW_W),
          top: pct(y, FLOW_H),
          '--node-color': node.color,
        } as CSSProperties
      }
    >
      <span className="sentinel-flowmap-node-dot" />
      <span className="sentinel-flowmap-node-text">
        <span className="sentinel-flowmap-node-label">{node.label}</span>
        <span className="sentinel-flowmap-node-value">{node.value}</span>
      </span>
    </Link>
  );
}

function FlowMap({ stats, loaded }: { stats: PipelineStats; loaded: boolean }) {
  const reducedMotion = usePrefersReducedMotion();

  if (!loaded) {
    return (
      <div className="sentinel-card">
        <p className="sentinel-status-unknown">loading pipeline status&hellip;</p>
      </div>
    );
  }

  const stageNodes: FlowNode[] = [
    {
      key: 'discover',
      href: '/discovery',
      label: '1. Discover',
      value: `${stats.projectsDiscovered}/${stats.projectsTotal} projects`,
      color: 'var(--sentinel-flow-discover)',
    },
    {
      key: 'plan',
      href: '/test-inventory',
      label: '2. Plan',
      value: `${stats.testsTotal} test cases`,
      color: 'var(--sentinel-flow-plan)',
    },
    {
      key: 'approve',
      href: '/approvals',
      label: '3. Approve',
      value: `${stats.testsPendingApproval} pending`,
      color: 'var(--sentinel-warn)',
      attention: stats.testsPendingApproval > 0,
    },
    {
      key: 'run',
      href: '/runs',
      label: '4. Run',
      value:
        stats.runningNow.length > 0 ? (
          <span className="sentinel-flowmap-node-live">{stats.runningNow.length} running</span>
        ) : (
          'idle'
        ),
      color: 'var(--sentinel-accent)',
      attention: stats.runningNow.length > 0,
    },
    {
      key: 'correlate',
      href: '/bugs',
      label: '5. Correlate',
      value: `${stats.openBugs} open bugs`,
      color: 'var(--sentinel-danger)',
      attention: stats.openBugs > 0,
    },
    {
      key: 'fix',
      href: '/fix-proposals',
      label: '6. Fix',
      value: `${stats.pendingFixProposals} awaiting review`,
      color: 'var(--sentinel-ok)',
      attention: stats.pendingFixProposals > 0,
    },
  ];

  const issueNodes: FlowNode[] = [
    {
      key: 'open-bugs',
      href: '/bugs',
      label: 'Open bugs',
      value: stats.openBugs,
      color: 'var(--sentinel-danger)',
      attention: stats.openBugs > 0,
    },
    {
      key: 'pending-fixes',
      href: '/fix-proposals',
      label: 'Pending fixes',
      value: stats.pendingFixProposals,
      color: 'var(--sentinel-warn)',
      attention: stats.pendingFixProposals > 0,
    },
    {
      key: 'resolved',
      href: '/bugs',
      label: 'Resolved',
      value: stats.resolvedBugs,
      color: 'var(--sentinel-ok)',
    },
  ];

  const totalRuns = stats.runsPassed + stats.runsFailed;
  const CLUSTER_DOT_COUNT = 36;
  const passDots = totalRuns > 0 ? Math.round((stats.runsPassed / totalRuns) * CLUSTER_DOT_COUNT) : 0;
  const failDots = totalRuns > 0 ? CLUSTER_DOT_COUNT - passDots : 0;
  const scatterColors: string[] = [];
  for (let i = 0, p = 0, f = 0; i < passDots + failDots; i++) {
    // Round-robin between the two buckets in their real proportion, so
    // the dots read as mixed rather than solid-colored halves.
    if (p / Math.max(passDots, 1) <= f / Math.max(failDots, 1) && p < passDots) {
      scatterColors.push('var(--sentinel-ok)');
      p++;
    } else {
      scatterColors.push('var(--sentinel-danger)');
      f++;
    }
  }

  const passRateTone = stats.passRate === null ? undefined : stats.passRate >= 80 ? 'ok' : stats.passRate >= 50 ? 'warn' : 'danger';

  return (
    <div className="sentinel-card">
      <h3 style={{ marginTop: 0 }}>Pipeline — where everything is right now</h3>
      <p className="sentinel-status-unknown" style={{ fontSize: '0.85rem', marginTop: '-0.5rem' }}>
        Every node links to the page that explains it. A pulsing dot means something there is
        waiting on a human decision or needs attention.
      </p>
      <div className="sentinel-flowmap-scroll">
        <div className="sentinel-flowmap">
          <svg viewBox={`0 0 ${FLOW_W} ${FLOW_H}`} className="sentinel-flowmap-svg" aria-hidden="true">
            {stageNodes.map((n, i) => (
              <path
                key={`track-stage-${n.key}`}
                id={`sentinel-flow-stage-${i}`}
                d={flowCurve(STAGE_ANCHOR_X, STAGE_Y[i]!, HUB_X - HUB_R + 4, HUB_Y)}
                className="sentinel-flowmap-track"
                style={{ '--node-color': n.color } as CSSProperties}
              />
            ))}
            {[-16, 0, 16].map((offset, i) => (
              <path
                key={`track-hub-cluster-${i}`}
                id={`sentinel-flow-hubcluster-${i}`}
                d={flowCurve(HUB_X + HUB_R - 4, HUB_Y + offset * 0.4, CLUSTER_X - CLUSTER_R + 4, HUB_Y + offset)}
                className="sentinel-flowmap-track"
                style={{ '--node-color': 'var(--sentinel-accent)' } as CSSProperties}
              />
            ))}
            {issueNodes.map((n, i) => (
              <path
                key={`track-issue-${n.key}`}
                id={`sentinel-flow-issue-${i}`}
                d={flowCurve(CLUSTER_X + CLUSTER_R - 4, CLUSTER_Y, ISSUE_ANCHOR_X, ISSUE_Y[i]!)}
                className="sentinel-flowmap-track"
                style={{ '--node-color': n.color } as CSSProperties}
              />
            ))}

            {!reducedMotion &&
              stageNodes.map((n, i) => (
                <circle key={`dot-stage-${n.key}`} r="4" fill={n.color} className="sentinel-flowmap-dot" style={{ color: n.color }}>
                  <animateMotion dur={`${2.6 + i * 0.2}s`} begin={`${i * 0.3}s`} repeatCount="indefinite">
                    <mpath href={`#sentinel-flow-stage-${i}`} />
                  </animateMotion>
                </circle>
              ))}
            {!reducedMotion &&
              [0, 1, 2].map((i) => (
                <circle
                  key={`dot-hubcluster-${i}`}
                  r="4"
                  fill="var(--sentinel-accent)"
                  className="sentinel-flowmap-dot"
                  style={{ color: 'var(--sentinel-accent)' }}
                >
                  <animateMotion dur={`${1.9 + i * 0.25}s`} begin={`${i * 0.4}s`} repeatCount="indefinite">
                    <mpath href={`#sentinel-flow-hubcluster-${i}`} />
                  </animateMotion>
                </circle>
              ))}
            {!reducedMotion &&
              issueNodes.map((n, i) => (
                <circle key={`dot-issue-${n.key}`} r="4" fill={n.color} className="sentinel-flowmap-dot" style={{ color: n.color }}>
                  <animateMotion dur={`${2.2 + i * 0.25}s`} begin={`${i * 0.35}s`} repeatCount="indefinite">
                    <mpath href={`#sentinel-flow-issue-${i}`} />
                  </animateMotion>
                </circle>
              ))}

            <circle cx={HUB_X} cy={HUB_Y} r={HUB_R} className="sentinel-flowmap-hub-circle" />
            <circle cx={CLUSTER_X} cy={CLUSTER_Y} r={CLUSTER_R + 6} className="sentinel-flowmap-hub-circle" style={{ fillOpacity: 0.15 }} />
            {scatterColors.map((color, i) => {
              const p = scatterPoint(i, scatterColors.length, CLUSTER_CENTER_R + 12, CLUSTER_R - 6);
              return (
                <circle
                  key={`scatter-${i}`}
                  cx={CLUSTER_X + p.x}
                  cy={CLUSTER_Y + p.y}
                  r={3}
                  fill={color}
                  style={{ color }}
                  className="sentinel-flowmap-scatter-dot"
                />
              );
            })}
            <circle cx={CLUSTER_X} cy={CLUSTER_Y} r={CLUSTER_CENTER_R} className="sentinel-flowmap-hub-circle" />
          </svg>

          <div className="sentinel-flowmap-overlay">
            {stageNodes.map((n, i) => (
              <FlowMapNode key={n.key} node={n} y={STAGE_Y[i]!} side="left" />
            ))}
            {issueNodes.map((n, i) => (
              <FlowMapNode key={n.key} node={n} y={ISSUE_Y[i]!} side="right" />
            ))}
            <div className="sentinel-flowmap-hub-label" style={{ left: pct(HUB_X, FLOW_W), top: pct(HUB_Y, FLOW_H) }}>
              <span className="sentinel-flowmap-hub-value">{stats.projectsTotal}</span>
              <span className="sentinel-flowmap-hub-caption">Projects</span>
            </div>
            <div className="sentinel-flowmap-hub-label" style={{ left: pct(CLUSTER_X, FLOW_W), top: pct(CLUSTER_Y, FLOW_H) }}>
              <span className="sentinel-flowmap-hub-value" data-tone={passRateTone}>
                {stats.passRate === null ? '—' : `${stats.passRate}%`}
              </span>
              <span className="sentinel-flowmap-hub-caption">Pass rate</span>
            </div>
          </div>
        </div>
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

      <FlowMap stats={stats} loaded={loaded} />

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
