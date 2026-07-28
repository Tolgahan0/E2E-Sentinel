'use client';

import Link from 'next/link';
import { useEffect, useState } from 'react';
import {
  fetchJSON,
  mutateJSON,
  type Environment,
  type EnvironmentsResponse,
  type Project,
  type ProjectsResponse,
} from '@/lib/api';

const CLASSIFICATIONS = ['local', 'development', 'test', 'staging', 'production', 'unknown'] as const;

function DiscoveryStatusBadge({ status }: { status: Project['discovery_status'] }) {
  const className =
    status === 'completed'
      ? 'sentinel-status-ok'
      : status === 'failed'
        ? 'sentinel-status-bad'
        : 'sentinel-status-unknown';
  return <span className={className}>{status.replace('_', ' ')}</span>;
}

function ClassificationSelect({
  env,
  onChanged,
}: {
  env: Environment;
  onChanged: (updated: Environment) => void;
}) {
  const [saving, setSaving] = useState(false);

  async function handleChange(e: React.ChangeEvent<HTMLSelectElement>) {
    const classification = e.target.value;
    setSaving(true);
    const result = await mutateJSON<Environment>('PATCH', `/api/v1/environments/${env.id}`, { classification });
    setSaving(false);
    if (result.ok && result.data) {
      onChanged(result.data);
    }
  }

  return (
    <select value={env.classification} onChange={handleChange} disabled={saving} aria-label={`Environment classification for ${env.name}`}>
      {CLASSIFICATIONS.map((c) => (
        <option key={c} value={c}>
          {c}
        </option>
      ))}
    </select>
  );
}

export default function ProjectsPage() {
  const [projects, setProjects] = useState<Project[]>([]);
  const [environments, setEnvironments] = useState<Record<string, Environment>>({});
  const [loaded, setLoaded] = useState(false);
  const [name, setName] = useState('');
  const [repositoryPath, setRepositoryPath] = useState('');
  const [formError, setFormError] = useState<string | null>(null);
  const [submitting, setSubmitting] = useState(false);
  const [discoveringID, setDiscoveringID] = useState<string | null>(null);

  async function loadProjects() {
    const res = await fetchJSON<ProjectsResponse>('/api/v1/projects');
    const list = res?.projects ?? [];
    setProjects(list);
    setLoaded(true);

    const envEntries = await Promise.all(
      list.map(async (p) => {
        const envRes = await fetchJSON<EnvironmentsResponse>(`/api/v1/projects/${p.id}/environments`);
        return [p.id, envRes?.environments?.[0]] as const;
      }),
    );
    const envMap: Record<string, Environment> = {};
    for (const [projectID, env] of envEntries) {
      if (env) envMap[projectID] = env;
    }
    setEnvironments(envMap);
  }

  useEffect(() => {
    (async () => {
      await loadProjects();
    })();
  }, []);

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault();
    setFormError(null);
    setSubmitting(true);
    const result = await mutateJSON<Project>('POST', '/api/v1/projects', {
      name,
      repository_path: repositoryPath,
    });
    setSubmitting(false);
    if (!result.ok) {
      setFormError(result.error ?? 'request_failed');
      return;
    }
    setName('');
    setRepositoryPath('');
    await loadProjects();
  }

  async function handleDiscover(projectID: string) {
    setDiscoveringID(projectID);
    await mutateJSON('POST', `/api/v1/projects/${projectID}/discover`);
    setDiscoveringID(null);
    await loadProjects();
  }

  return (
    <>
      <h2>Projects</h2>
      <p>
        Add a local repository by its absolute path. E2E Sentinel validates the path (must exist, must
        be a directory, cannot be a system root) before storing it — nothing in the repository is read
        until you run discovery.
      </p>

      <form onSubmit={handleSubmit} className="sentinel-card" style={{ display: 'flex', gap: '0.75rem', alignItems: 'flex-end', flexWrap: 'wrap' }}>
        <label>
          Name
          <br />
          <input value={name} onChange={(e) => setName(e.target.value)} required placeholder="Routa" />
        </label>
        <label>
          Repository path (absolute)
          <br />
          <input
            value={repositoryPath}
            onChange={(e) => setRepositoryPath(e.target.value)}
            required
            placeholder="/Users/you/code/routa"
            style={{ width: '22rem' }}
          />
        </label>
        <button type="submit" disabled={submitting}>
          {submitting ? 'Adding…' : 'Add project'}
        </button>
        {formError && <span className="sentinel-status-bad">{formError}</span>}
      </form>

      <div className="sentinel-card" style={{ marginTop: '1rem' }}>
        {!loaded ? (
          <p className="sentinel-status-unknown">loading&hellip;</p>
        ) : projects.length === 0 ? (
          <p className="sentinel-status-unknown">No projects yet. Add one above.</p>
        ) : (
          <table className="sentinel-table">
            <thead>
              <tr>
                <th>Name</th>
                <th>Repository path</th>
                <th>Environment</th>
                <th>Discovery</th>
                <th>Actions</th>
              </tr>
            </thead>
            <tbody>
              {projects.map((p) => {
                const env = environments[p.id];
                return (
                <tr key={p.id}>
                  <td>
                    <Link href={`/discovery?project=${p.id}`}>{p.name}</Link>
                  </td>
                  <td>{p.repository_path}</td>
                  <td>
                    {env ? (
                      <ClassificationSelect
                        env={env}
                        onChanged={(updated) => setEnvironments((prev) => ({ ...prev, [p.id]: updated }))}
                      />
                    ) : (
                      '—'
                    )}
                  </td>
                  <td>
                    <DiscoveryStatusBadge status={p.discovery_status} />
                  </td>
                  <td>
                    <button onClick={() => handleDiscover(p.id)} disabled={discoveringID === p.id}>
                      {discoveringID === p.id ? 'Scanning…' : 'Run discovery'}
                    </button>
                  </td>
                </tr>
                );
              })}
            </tbody>
          </table>
        )}
      </div>
    </>
  );
}
