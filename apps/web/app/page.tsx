'use client';

import Link from 'next/link';
import { useEffect, useState, type CSSProperties, type ReactNode } from 'react';
import {
  fetchJSON,
  type BugReport,
  type BugsResponse,
  type FixProposal,
  type FixProposalsResponse,
  type HealthResponse,
  type Project,
  type ProjectsResponse,
  type ProvidersResponse,
  type ReadyResponse,
  type RunStatus,
  type RunsResponse,
  type TestRun,
  type TestsResponse,
  type VersionResponse,
} from '@/lib/api';

interface TopProject {
  id: string;
  name: string;
  testsTotal: number;
  openBugs: number;
  passRate: number | null;
}

interface RecentRun {
  id: string;
  projectId: string;
  projectName: string;
  testTitle: string;
  status: RunStatus;
  startedAt: string;
}

interface TopBug {
  id: string;
  projectId: string;
  projectName: string;
  title: string;
  severity: BugReport['severity'];
  frequency: number;
  status: BugReport['status'];
}

interface RecentFixProposal {
  id: string;
  projectId: string;
  projectName: string;
  title: string;
  approvalStatus: FixProposal['approval_status'];
  generatedAt: string;
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
  topProjects: TopProject[];
  recentRuns: RecentRun[];
  topBugs: TopBug[];
  recentFixProposalsList: RecentFixProposal[];
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
  topProjects: [],
  recentRuns: [],
  topBugs: [],
  recentFixProposalsList: [],
};

const SEVERITY_RANK: Record<BugReport['severity'], number> = {
  critical: 0,
  high: 1,
  medium: 2,
  low: 3,
  informational: 4,
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
        id: p.id,
        name: p.name,
        tests: testsRes?.tests ?? [],
        runs: runsRes?.runs ?? [],
        fixProposals: fixRes?.fix_proposals ?? [],
      };
    })
  );

  const projectName = new Map(projects.map((p) => [p.id, p.name]));
  const allTests = perProject.flatMap((p) => p.tests);
  const allRuns = perProject.flatMap((p) => p.runs);
  const allFixProposals = perProject.flatMap((p) => p.fixProposals);
  const testTitle = new Map(allTests.map((t) => [t.id, t.title]));

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
  const openBugsByProject = new Map<string, number>();
  for (const b of bugs) {
    if (b.status === 'open' || b.status === 'reopened') {
      openBugsByProject.set(b.project_id, (openBugsByProject.get(b.project_id) ?? 0) + 1);
    }
  }

  const topProjects: TopProject[] = perProject
    .map((p) => {
      const finished = p.runs.filter((r) => r.status === 'passed' || r.status === 'failed');
      const projectPassRate =
        finished.length === 0 ? null : Math.round((100 * finished.filter((r) => r.status === 'passed').length) / finished.length);
      return {
        id: p.id,
        name: p.name,
        testsTotal: p.tests.length,
        openBugs: openBugsByProject.get(p.id) ?? 0,
        passRate: projectPassRate,
      };
    })
    .sort((a, b) => b.openBugs - a.openBugs || b.testsTotal - a.testsTotal)
    .slice(0, 6);

  const recentRuns: RecentRun[] = [...allRuns]
    .sort((a, b) => new Date(b.started_at).getTime() - new Date(a.started_at).getTime())
    .slice(0, 6)
    .map((r) => ({
      id: r.id,
      projectId: r.project_id,
      projectName: projectName.get(r.project_id) ?? 'unknown project',
      testTitle: testTitle.get(r.test_case_id) ?? r.test_case_id,
      status: r.status,
      startedAt: r.started_at,
    }));

  const topBugs: TopBug[] = [...bugs]
    .filter((b) => b.status !== 'resolved')
    .sort((a, b) => SEVERITY_RANK[a.severity] - SEVERITY_RANK[b.severity] || b.frequency - a.frequency)
    .slice(0, 6)
    .map((b) => ({
      id: b.id,
      projectId: b.project_id,
      projectName: projectName.get(b.project_id) ?? 'unknown project',
      title: b.title,
      severity: b.severity,
      frequency: b.frequency,
      status: b.status,
    }));

  const recentFixProposalsList: RecentFixProposal[] = [...allFixProposals]
    .sort((a, b) => new Date(b.generated_at).getTime() - new Date(a.generated_at).getTime())
    .slice(0, 6)
    .map((fp) => ({
      id: fp.id,
      projectId: fp.project_id,
      projectName: projectName.get(fp.project_id) ?? 'unknown project',
      title: fp.title,
      approvalStatus: fp.approval_status,
      generatedAt: fp.generated_at,
    }));

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
    topProjects,
    recentRuns,
    topBugs,
    recentFixProposalsList,
  };
}

// The flow map's own coordinate space — an arbitrary but fixed canvas
// that the SVG viewBox and the HTML overlay's percentage positions both
// reference, so the two layers always land on the same points
// regardless of the rendered size (see .sentinel-flowmap in globals.css).
const FLOW_W = 1000;
const FLOW_H = 380;
const STAGE_ANCHOR_X = 218;
const STAGE_Y: [number, number, number, number, number, number] = [55, 111, 167, 223, 279, 335];
const HUB_X = 340;
const HUB_Y = 190;
const HUB_R = 62;
const CLUSTER_X = 660;
const CLUSTER_Y = 190;
const CLUSTER_R = 96;
const CLUSTER_CENTER_R = 44;
const ISSUE_ANCHOR_X = 785;
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

// A point on a circle's edge, `spreadDeg` degrees either side of
// `baseDeg` (0 = rightmost point, 180 = leftmost point), for item `i`
// of `count` — item 0 lands above center, the last item below. Used so
// a group of connectors lands at a spread of distinct points around
// the hub/cluster's circumference instead of every single one
// converging on the exact same pixel, which reads as a tangled knot
// rather than a fan (the sign flips between the two base angles: past
// the leftmost point, increasing angle moves up; past the rightmost
// point, increasing angle moves down — cos(baseDeg) encodes that).
function circleFanPoint(cx: number, cy: number, r: number, baseDeg: number, spreadDeg: number, i: number, count: number) {
  const mid = (count - 1) / 2;
  const t = mid === 0 ? 0 : (i - mid) / mid;
  const baseRad = (baseDeg * Math.PI) / 180;
  const rad = baseRad + ((t * spreadDeg * Math.cos(baseRad) * Math.PI) / 180);
  return { x: cx + r * Math.cos(rad), y: cy + r * Math.sin(rad) };
}

// Same fan point as circleFanPoint, plus a control point further out
// along the same ray (radius + controlDist). flowCurve's bezier always
// arrives/departs horizontally, which only matches a circle's true
// radial direction at the 0deg/180deg extremes — every fanned point in
// between meets the circle at an angle, so the curve clips across the
// boundary instead of plugging into it. That's invisible against a
// fully opaque hub fill (the mismatch is hidden behind it), but not
// against the Pass Rate cluster's translucent outer ring. Using this
// control point instead of a flat one aligns the curve's tangent with
// the true radius, so it enters/leaves along the ring instead of
// across it.
function circleFanPointWithControl(
  cx: number,
  cy: number,
  r: number,
  baseDeg: number,
  spreadDeg: number,
  i: number,
  count: number,
  controlDist: number,
) {
  const mid = (count - 1) / 2;
  const t = mid === 0 ? 0 : (i - mid) / mid;
  const baseRad = (baseDeg * Math.PI) / 180;
  const rad = baseRad + ((t * spreadDeg * Math.cos(baseRad) * Math.PI) / 180);
  const point = { x: cx + r * Math.cos(rad), y: cy + r * Math.sin(rad) };
  const control = { x: cx + (r + controlDist) * Math.cos(rad), y: cy + (r + controlDist) * Math.sin(rad) };
  return { point, control };
}

// flowCurve variants for a circle-side endpoint whose tangent should
// follow circleFanPointWithControl's radial control point rather than
// the flat horizontal one flowCurve's midpoint control produces.
function flowCurveEnteringCircle(x1: number, y1: number, target: { point: { x: number; y: number }; control: { x: number; y: number } }) {
  const midX = x1 + (target.point.x - x1) / 2;
  return `M ${x1} ${y1} C ${midX} ${y1}, ${target.control.x} ${target.control.y}, ${target.point.x} ${target.point.y}`;
}

function flowCurveLeavingCircle(source: { point: { x: number; y: number }; control: { x: number; y: number } }, x2: number, y2: number) {
  const midX = source.point.x + (x2 - source.point.x) / 2;
  return `M ${source.point.x} ${source.point.y} C ${source.control.x} ${source.control.y}, ${midX} ${y2}, ${x2} ${y2}`;
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

interface FlowNode {
  key: string;
  href: string;
  label: string;
  value: ReactNode;
  color: string;
  colorRgb: string;
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
          '--node-color-rgb': node.colorRgb,
        } as CSSProperties
      }
    >
      <span className="sentinel-flowmap-node-icon">
        <span className="sentinel-flowmap-node-dot" />
      </span>
      <span className="sentinel-flowmap-node-text">
        <span className="sentinel-flowmap-node-label">{node.label}</span>
        <span className="sentinel-flowmap-node-value">{node.value}</span>
      </span>
    </Link>
  );
}

function FlowMap({ stats, loaded }: { stats: PipelineStats; loaded: boolean }) {
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
      colorRgb: 'var(--sentinel-flow-discover-rgb)',
    },
    {
      key: 'plan',
      href: '/test-inventory',
      label: '2. Plan',
      value: `${stats.testsTotal} test cases`,
      color: 'var(--sentinel-flow-plan)',
      colorRgb: 'var(--sentinel-flow-plan-rgb)',
    },
    {
      key: 'approve',
      href: '/approvals',
      label: '3. Approve',
      value: `${stats.testsPendingApproval} pending`,
      color: 'var(--sentinel-warn)',
      colorRgb: 'var(--sentinel-warn-rgb)',
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
      colorRgb: 'var(--sentinel-accent-rgb)',
      attention: stats.runningNow.length > 0,
    },
    {
      key: 'correlate',
      href: '/bugs',
      label: '5. Correlate',
      value: `${stats.openBugs} open bugs`,
      color: 'var(--sentinel-danger)',
      colorRgb: 'var(--sentinel-danger-rgb)',
      attention: stats.openBugs > 0,
    },
    {
      key: 'fix',
      href: '/fix-proposals',
      label: '6. Fix',
      value: `${stats.pendingFixProposals} awaiting review`,
      color: 'var(--sentinel-ok)',
      colorRgb: 'var(--sentinel-ok-rgb)',
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
      colorRgb: 'var(--sentinel-danger-rgb)',
      attention: stats.openBugs > 0,
    },
    {
      key: 'pending-fixes',
      href: '/fix-proposals',
      label: 'Pending fixes',
      value: stats.pendingFixProposals,
      color: 'var(--sentinel-warn)',
      colorRgb: 'var(--sentinel-warn-rgb)',
      attention: stats.pendingFixProposals > 0,
    },
    {
      key: 'resolved',
      href: '/bugs',
      label: 'Resolved',
      value: stats.resolvedBugs,
      color: 'var(--sentinel-ok)',
      colorRgb: 'var(--sentinel-ok-rgb)',
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
            {stageNodes.map((n, i) => {
              const into = circleFanPoint(HUB_X, HUB_Y, HUB_R - 4, 180, 55, i, stageNodes.length);
              const d = flowCurve(STAGE_ANCHOR_X, STAGE_Y[i]!, into.x, into.y);
              return (
                <g key={`track-stage-${n.key}`} style={{ '--node-color': n.color } as CSSProperties}>
                  <path d={d} pathLength={100} className="sentinel-flowmap-track-glow" />
                  <path d={d} pathLength={100} className="sentinel-flowmap-track" />
                  <path
                    d={d}
                    pathLength={100}
                    className="sentinel-flowmap-track-flow"
                    style={{ animationDelay: `${i * -0.4}s` } as CSSProperties}
                  />
                </g>
              );
            })}
            {[0, 1, 2].map((i) => {
              const out = circleFanPoint(HUB_X, HUB_Y, HUB_R - 4, 0, 22, i, 3);
              const into = circleFanPointWithControl(CLUSTER_X, CLUSTER_Y, CLUSTER_R + 6, 180, 18, i, 3, 70);
              const d = flowCurveEnteringCircle(out.x, out.y, into);
              return (
                <g key={`track-hub-cluster-${i}`} style={{ '--node-color': 'var(--sentinel-accent)' } as CSSProperties}>
                  <path d={d} pathLength={100} className="sentinel-flowmap-track-glow" />
                  <path d={d} pathLength={100} className="sentinel-flowmap-track" />
                  <path
                    d={d}
                    pathLength={100}
                    className="sentinel-flowmap-track-flow"
                    style={{ animationDelay: `${i * -0.5}s` } as CSSProperties}
                  />
                </g>
              );
            })}
            {issueNodes.map((n, i) => {
              // A much smaller controlDist than the hub-cluster track below:
              // the gap between the ring and ISSUE_ANCHOR_X is only ~23-37px
              // here, so the hub-cluster track's 70 would push the control
              // point PAST the card's x position for the fanned (+-30deg)
              // items, looping the curve back on itself.
              const out = circleFanPointWithControl(CLUSTER_X, CLUSTER_Y, CLUSTER_R + 6, 0, 30, i, issueNodes.length, 20);
              const d = flowCurveLeavingCircle(out, ISSUE_ANCHOR_X, ISSUE_Y[i]!);
              return (
                <g key={`track-issue-${n.key}`} style={{ '--node-color': n.color } as CSSProperties}>
                  <path d={d} pathLength={100} className="sentinel-flowmap-track-glow" />
                  <path d={d} pathLength={100} className="sentinel-flowmap-track" />
                  <path
                    d={d}
                    pathLength={100}
                    className="sentinel-flowmap-track-flow"
                    style={{ animationDelay: `${i * -0.45}s` } as CSSProperties}
                  />
                </g>
              );
            })}

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

function relativeTime(iso: string): string {
  const diffSec = Math.round((Date.now() - new Date(iso).getTime()) / 1000);
  if (diffSec < 60) return 'just now';
  const diffMin = Math.round(diffSec / 60);
  if (diffMin < 60) return `${diffMin}m ago`;
  const diffHr = Math.round(diffMin / 60);
  if (diffHr < 24) return `${diffHr}h ago`;
  return `${Math.round(diffHr / 24)}d ago`;
}

const SEVERITY_TONE: Record<BugReport['severity'], 'danger' | 'warn' | 'muted'> = {
  critical: 'danger',
  high: 'danger',
  medium: 'warn',
  low: 'muted',
  informational: 'muted',
};

const RUN_DOT_COLOR: Record<RunStatus, string> = {
  passed: 'var(--sentinel-ok)',
  failed: 'var(--sentinel-danger)',
  error: 'var(--sentinel-danger)',
  running: 'var(--sentinel-accent)',
  queued: 'var(--sentinel-accent)',
  cancelled: 'var(--sentinel-muted)',
};

const FIX_APPROVAL_TONE: Record<FixProposal['approval_status'], 'danger' | 'warn' | 'ok'> = {
  pending_review: 'warn',
  revision_requested: 'warn',
  approved: 'ok',
  rejected: 'danger',
};

const FIX_APPROVAL_LABEL: Record<FixProposal['approval_status'], string> = {
  pending_review: 'Pending review',
  revision_requested: 'Revision requested',
  approved: 'Approved',
  rejected: 'Rejected',
};

// GET /ready's test_execution/websocket_execution is a runs.Runner's
// raw Name() ("playwright-docker", "playwright-local", "websocket-docker",
// "websocket-local") or "unconfigured" — translated here into a label
// and tone that actually says what it means for isolation, rather than
// showing the raw internal string. See docs/RUNNER_ISOLATION.md's
// "Local process execution mode".
function executionModeLabel(name: string): string {
  if (name.endsWith('-docker')) return 'Docker';
  if (name.endsWith('-local')) return 'Local process';
  return 'Not configured';
}

function executionModeTone(name: string): 'ok' | 'warn' | 'muted' {
  if (name.endsWith('-docker')) return 'ok';
  if (name.endsWith('-local')) return 'warn';
  return 'muted';
}

const EXECUTION_MODE_TONE_COLOR: Record<'ok' | 'warn' | 'muted', string> = {
  ok: 'var(--sentinel-ok)',
  warn: 'var(--sentinel-warn)',
  muted: 'var(--sentinel-muted)',
};

function InsightsCard({
  title,
  viewAllHref,
  accent,
  children,
}: {
  title: string;
  viewAllHref: string;
  accent: string;
  children: ReactNode;
}) {
  return (
    <div className="sentinel-card">
      <div className="sentinel-insights-card-header">
        <h3 className="sentinel-insights-title">
          <span className="sentinel-insights-title-icon" style={{ background: accent } as CSSProperties} />
          {title}
        </h3>
        <Link href={viewAllHref} className="sentinel-insights-viewall">
          View all
        </Link>
      </div>
      {children}
    </div>
  );
}

function InsightsGrid({
  stats,
  loaded,
  health,
  ready,
  version,
}: {
  stats: PipelineStats;
  loaded: boolean;
  health: HealthResponse | null;
  ready: ReadyResponse | null;
  version: VersionResponse | null;
}) {
  if (!loaded) {
    return (
      <div className="sentinel-card" style={{ marginTop: '1rem' }}>
        <p className="sentinel-status-unknown">loading insights&hellip;</p>
      </div>
    );
  }

  return (
    <div className="sentinel-insights-grid">
      <InsightsCard title="Top projects" viewAllHref="/projects" accent="var(--sentinel-accent)">
        {stats.topProjects.length === 0 ? (
          <p className="sentinel-insights-empty">No projects yet.</p>
        ) : (
          <div className="sentinel-insights-list">
            {stats.topProjects.map((p, i) => (
              <Link key={p.id} href={`/test-inventory?project=${p.id}`} className="sentinel-insights-row">
                <span className="sentinel-insights-row-rank">{i + 1}</span>
                <span className="sentinel-insights-row-main">
                  <span className="sentinel-insights-row-title">{p.name}</span>
                  <span className="sentinel-insights-row-sub">{p.testsTotal} test cases</span>
                </span>
                <span className="sentinel-insights-row-end">
                  {p.openBugs > 0 ? (
                    <span className="sentinel-insights-pill" data-tone="danger">
                      {p.openBugs} open
                    </span>
                  ) : (
                    <span className="sentinel-insights-pill" data-tone="ok">
                      clean
                    </span>
                  )}
                  {p.passRate !== null && (
                    <span className="sentinel-insights-bar-track">
                      <span
                        className="sentinel-insights-bar-fill"
                        style={
                          {
                            width: `${p.passRate}%`,
                            '--bar-color': p.passRate >= 80 ? 'var(--sentinel-ok)' : p.passRate >= 50 ? 'var(--sentinel-warn)' : 'var(--sentinel-danger)',
                          } as CSSProperties
                        }
                      />
                    </span>
                  )}
                </span>
              </Link>
            ))}
          </div>
        )}
      </InsightsCard>

      <InsightsCard title="Latest runs" viewAllHref="/runs" accent="var(--sentinel-ok)">
        {stats.recentRuns.length === 0 ? (
          <p className="sentinel-insights-empty">No runs yet.</p>
        ) : (
          <div className="sentinel-insights-list">
            {stats.recentRuns.map((r) => (
              <Link key={r.id} href={`/runs?project=${r.projectId}`} className="sentinel-insights-row">
                <span
                  className="sentinel-insights-dot"
                  data-live={r.status === 'running' ? 'true' : undefined}
                  style={{ '--dot-color': RUN_DOT_COLOR[r.status] } as CSSProperties}
                />
                <span className="sentinel-insights-row-main">
                  <span className="sentinel-insights-row-title">{r.testTitle}</span>
                  <span className="sentinel-insights-row-sub">{r.projectName}</span>
                </span>
                <span className="sentinel-insights-row-end">
                  <span className="sentinel-insights-pill" data-tone={r.status === 'passed' ? 'ok' : r.status === 'failed' || r.status === 'error' ? 'danger' : 'muted'}>
                    {r.status}
                  </span>
                  <span className="sentinel-insights-row-sub">{relativeTime(r.startedAt)}</span>
                </span>
              </Link>
            ))}
          </div>
        )}
      </InsightsCard>

      <InsightsCard title="Top bugs" viewAllHref="/bugs" accent="var(--sentinel-danger)">
        {stats.topBugs.length === 0 ? (
          <p className="sentinel-insights-empty">No open bugs.</p>
        ) : (
          <div className="sentinel-insights-list">
            {stats.topBugs.map((b) => (
              <Link key={b.id} href={`/bugs?project=${b.projectId}`} className="sentinel-insights-row">
                <span className="sentinel-insights-pill" data-tone={SEVERITY_TONE[b.severity]}>
                  {b.severity}
                </span>
                <span className="sentinel-insights-row-main">
                  <span className="sentinel-insights-row-title">{b.title}</span>
                  <span className="sentinel-insights-row-sub">{b.projectName}</span>
                </span>
                <span className="sentinel-insights-row-end">
                  <span className="sentinel-insights-row-sub">{b.frequency > 1 ? `${b.frequency}x recurrent` : 'new'}</span>
                </span>
              </Link>
            ))}
          </div>
        )}
      </InsightsCard>

      <InsightsCard title="Latest fix proposals" viewAllHref="/fix-proposals" accent="var(--sentinel-warn)">
        {stats.recentFixProposalsList.length === 0 ? (
          <p className="sentinel-insights-empty">No fix proposals yet.</p>
        ) : (
          <div className="sentinel-insights-list">
            {stats.recentFixProposalsList.map((fp) => (
              <Link key={fp.id} href={`/fix-proposals?project=${fp.projectId}`} className="sentinel-insights-row">
                <span className="sentinel-insights-row-main">
                  <span className="sentinel-insights-row-title">{fp.title}</span>
                  <span className="sentinel-insights-row-sub">
                    {fp.projectName} &middot; {relativeTime(fp.generatedAt)}
                  </span>
                </span>
                <span className="sentinel-insights-row-end">
                  <span className="sentinel-insights-pill" data-tone={FIX_APPROVAL_TONE[fp.approvalStatus]}>
                    {FIX_APPROVAL_LABEL[fp.approvalStatus]}
                  </span>
                </span>
              </Link>
            ))}
          </div>
        )}
      </InsightsCard>

      <InsightsCard title="System status" viewAllHref="/settings" accent="var(--sentinel-flow-discover)">
        <div className="sentinel-insights-list">
          <span className="sentinel-insights-row">
            <span
              className="sentinel-insights-dot"
              style={{ '--dot-color': health?.status === 'ok' ? 'var(--sentinel-ok)' : 'var(--sentinel-danger)' } as CSSProperties}
            />
            <span className="sentinel-insights-row-main">
              <span className="sentinel-insights-row-title">sentinel-api</span>
            </span>
            <span className="sentinel-insights-row-end">
              <span className="sentinel-insights-pill" data-tone={health?.status === 'ok' ? 'ok' : 'danger'}>
                {health?.status === 'ok' ? 'ok' : 'unreachable'}
              </span>
            </span>
          </span>
          {ready &&
            Object.entries(ready.checks).map(([dep, status]) => (
              <span className="sentinel-insights-row" key={dep}>
                <span className="sentinel-insights-dot" style={{ '--dot-color': status === 'ok' ? 'var(--sentinel-ok)' : 'var(--sentinel-danger)' } as CSSProperties} />
                <span className="sentinel-insights-row-main">
                  <span className="sentinel-insights-row-title">{dep}</span>
                </span>
                <span className="sentinel-insights-row-end">
                  <span className="sentinel-insights-pill" data-tone={status === 'ok' ? 'ok' : 'danger'}>
                    {status === 'ok' ? 'ok' : 'unreachable'}
                  </span>
                </span>
              </span>
            ))}
          {ready && (
            <span className="sentinel-insights-row">
              <span className="sentinel-insights-dot" style={{ '--dot-color': EXECUTION_MODE_TONE_COLOR[executionModeTone(ready.test_execution)] } as CSSProperties} />
              <span className="sentinel-insights-row-main">
                <span className="sentinel-insights-row-title">Execution</span>
              </span>
              <span className="sentinel-insights-row-end">
                <span className="sentinel-insights-pill" data-tone={executionModeTone(ready.test_execution)}>
                  {executionModeLabel(ready.test_execution)}
                </span>
              </span>
            </span>
          )}
          {version && (
            <span className="sentinel-insights-row">
              <span
                className="sentinel-insights-dot"
                style={{ '--dot-color': version.update_available ? 'var(--sentinel-warn)' : 'var(--sentinel-ok)' } as CSSProperties}
              />
              <span className="sentinel-insights-row-main">
                <span className="sentinel-insights-row-title">Version</span>
                <span className="sentinel-insights-row-sub">{version.current_version}</span>
              </span>
              <span className="sentinel-insights-row-end">
                {version.update_available ? (
                  <a href={version.release_url || undefined} target="_blank" rel="noreferrer" className="sentinel-insights-pill" data-tone="warn">
                    {version.latest_version} available
                  </a>
                ) : !version.update_check_enabled ? (
                  <span className="sentinel-insights-pill" data-tone="muted">
                    check disabled
                  </span>
                ) : version.check_error ? (
                  <span className="sentinel-insights-pill" data-tone="muted" title={version.check_error}>
                    check failed
                  </span>
                ) : (
                  <span className="sentinel-insights-pill" data-tone="ok">
                    up to date
                  </span>
                )}
              </span>
            </span>
          )}
        </div>
      </InsightsCard>

      <InsightsCard title="AI providers" viewAllHref="/ai-providers" accent="var(--sentinel-flow-plan)">
        {stats.aiProvidersEnabled === 0 ? (
          <p className="sentinel-insights-empty">None configured — fully optional, everything else works without it.</p>
        ) : (
          <div className="sentinel-insights-list">
            <span className="sentinel-insights-row">
              <span className="sentinel-insights-row-main">
                <span className="sentinel-insights-row-title">Enabled providers healthy</span>
              </span>
              <span className="sentinel-insights-row-end">
                <span className="sentinel-insights-pill" data-tone={stats.aiProvidersHealthy === stats.aiProvidersEnabled ? 'ok' : 'warn'}>
                  {stats.aiProvidersHealthy}/{stats.aiProvidersEnabled}
                </span>
              </span>
            </span>
          </div>
        )}
      </InsightsCard>
    </div>
  );
}

export default function DashboardPage() {
  const [health, setHealth] = useState<HealthResponse | null>(null);
  const [ready, setReady] = useState<ReadyResponse | null>(null);
  const [version, setVersion] = useState<VersionResponse | null>(null);
  const [stats, setStats] = useState<PipelineStats>(EMPTY_STATS);
  const [loaded, setLoaded] = useState(false);

  useEffect(() => {
    let cancelled = false;

    async function load() {
      const [healthRes, readyRes, versionRes, projectsRes] = await Promise.all([
        fetchJSON<HealthResponse>('/api/health'),
        fetchJSON<ReadyResponse>('/api/ready'),
        fetchJSON<VersionResponse>('/api/version'),
        fetchJSON<ProjectsResponse>('/api/v1/projects'),
      ]);
      if (cancelled) return;
      setHealth(healthRes);
      setReady(readyRes);
      setVersion(versionRes);

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

      {version?.update_available && (
        <div className="sentinel-update-banner">
          <span>
            A newer version of E2E Sentinel is available: <strong>{version.latest_version}</strong>{' '}
            (currently running {version.current_version}).
          </span>
          {version.release_url && (
            <a href={version.release_url} target="_blank" rel="noreferrer">
              View release
            </a>
          )}
        </div>
      )}

      <FlowMap stats={stats} loaded={loaded} />

      {loaded && stats.runningNow.length > 0 && (
        <InsightsCard title="Currently running" viewAllHref="/runs" accent="var(--sentinel-accent)">
          <div className="sentinel-insights-list">
            {stats.runningNow.map((r) => (
              <Link key={r.id} href={`/runs?project=${r.project_id}`} className="sentinel-insights-row">
                <span className="sentinel-insights-dot" data-live="true" style={{ '--dot-color': 'var(--sentinel-accent)' } as CSSProperties} />
                <span className="sentinel-insights-row-main">
                  <span className="sentinel-insights-row-title">{r.id}</span>
                  <span className="sentinel-insights-row-sub">started {relativeTime(r.started_at)}</span>
                </span>
                <span className="sentinel-insights-row-end">
                  <span className="sentinel-insights-pill" data-tone="muted">
                    {r.status}
                  </span>
                </span>
              </Link>
            ))}
          </div>
        </InsightsCard>
      )}

      <div style={{ marginTop: '1rem' }}>
        <InsightsGrid stats={stats} loaded={loaded} health={health} ready={ready} version={version} />
      </div>
    </>
  );
}
