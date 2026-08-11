'use client';

import { Suspense, useCallback, useEffect, useState, type CSSProperties } from 'react';
import { useSearchParams } from 'next/navigation';
import {
  fetchJSON,
  type FlakyAssessment,
  type FlakyTest,
  type FlakyTestsResponse,
  type Project,
  type ProjectsResponse,
  type RunStatus,
} from '@/lib/api';

const ASSESSMENT_TONE: Record<FlakyAssessment, string> = {
  flaky: 'danger',
  flaky_candidate: 'warn',
  likely_real_defect: 'danger',
  suspect: 'muted',
  insufficient_evidence: 'muted',
};

const ASSESSMENT_LABEL: Record<FlakyAssessment, string> = {
  flaky: 'Flaky',
  flaky_candidate: 'Flaky candidate',
  likely_real_defect: 'Likely real defect',
  suspect: 'Suspect',
  insufficient_evidence: 'Insufficient evidence',
};

// Same color language as the Dashboard's "Latest runs" dots
// (RUN_DOT_COLOR in app/page.tsx) — a run's status means the same
// thing wherever it's shown.
const RUN_DOT_COLOR: Record<RunStatus, string> = {
  passed: 'var(--sentinel-ok)',
  failed: 'var(--sentinel-danger)',
  error: 'var(--sentinel-danger)',
  running: 'var(--sentinel-accent)',
  queued: 'var(--sentinel-accent)',
  cancelled: 'var(--sentinel-muted)',
};

function HistoryDots({ statuses }: { statuses: RunStatus[] }) {
  return (
    <span style={{ display: 'inline-flex', gap: '4px' }} title={`Last ${statuses.length} runs, oldest to newest`}>
      {statuses.map((status, i) => (
        <span
          key={i}
          className="sentinel-insights-dot"
          style={{ '--dot-color': RUN_DOT_COLOR[status] } as CSSProperties}
        />
      ))}
    </span>
  );
}

function FlakyTestsContent() {
  const searchParams = useSearchParams();
  const initialProjectID = searchParams.get('project') ?? '';

  const [projects, setProjects] = useState<Project[]>([]);
  const [selectedID, setSelectedID] = useState(initialProjectID);
  const [tests, setTests] = useState<FlakyTest[]>([]);
  const [loaded, setLoaded] = useState(false);

  useEffect(() => {
    fetchJSON<ProjectsResponse>('/api/v1/projects').then((res) => {
      const list = res?.projects ?? [];
      setProjects(list);
      if (!selectedID && list[0]) setSelectedID(list[0].id);
    });
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const loadTests = useCallback(async () => {
    if (!selectedID) return;
    setLoaded(false);
    const res = await fetchJSON<FlakyTestsResponse>(`/api/v1/projects/${selectedID}/flaky-tests`);
    setTests(res?.flaky_tests ?? []);
    setLoaded(true);
  }, [selectedID]);

  useEffect(() => {
    (async () => {
      await loadTests();
    })();
  }, [loadTests]);

  return (
    <>
      <h2>Flaky Tests</h2>
      <p>
        A project-wide, always-current view of test stability — computed from each test case&apos;s
        full run history, not just what happened on its last failure. A test case only appears once
        it has run at least once; a single run always shows as &ldquo;insufficient evidence&rdquo;
        rather than being hidden.
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
        ) : tests.length === 0 ? (
          <p className="sentinel-status-unknown">
            No test cases have run yet in this project — run an approved test case at least once to
            see it here.
          </p>
        ) : (
          <div className="sentinel-card">
            <div className="sentinel-insights-list">
              {tests.map((t) => (
                <div key={t.test_case_id} className="sentinel-insights-row">
                  <span className="sentinel-insights-row-main">
                    <span className="sentinel-insights-row-title">{t.title}</span>
                    <span className="sentinel-insights-row-sub">
                      {t.total_runs} run{t.total_runs === 1 ? '' : 's'}
                    </span>
                  </span>
                  <span className="sentinel-insights-row-end">
                    <HistoryDots statuses={t.recent_statuses} />
                    <span className="sentinel-insights-pill" data-tone={ASSESSMENT_TONE[t.assessment]}>
                      {ASSESSMENT_LABEL[t.assessment]}
                    </span>
                  </span>
                </div>
              ))}
            </div>
          </div>
        )}
      </div>
    </>
  );
}

export default function FlakyTestsPage() {
  return (
    <Suspense fallback={<p className="sentinel-status-unknown">loading&hellip;</p>}>
      <FlakyTestsContent />
    </Suspense>
  );
}
