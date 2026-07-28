'use client';

import { useCallback, useEffect, useMemo, useState } from 'react';
import {
  AI_TASKS,
  PROVIDER_TYPES,
  fetchJSON,
  mutateJSON,
  type Provider,
  type ProviderTestResult,
  type ProviderType,
  type ProvidersResponse,
  type TaskRoutingResponse,
} from '@/lib/api';

const HEALTH_CLASS: Record<Provider['health_status'], string> = {
  unknown: 'sentinel-status-unknown',
  ok: 'sentinel-status-ok',
  error: 'sentinel-status-bad',
};

function emptyForm() {
  return {
    type: 'ollama' as ProviderType,
    name: '',
    base_url: '',
    model: '',
    api_key: '',
    is_local: true,
    timeout_seconds: 30,
  };
}

function AddProviderForm({ onCreated }: { onCreated: () => void }) {
  const [form, setForm] = useState(emptyForm());
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);

  async function submit() {
    if (!form.name.trim()) {
      setError('name_required');
      return;
    }
    setSubmitting(true);
    setError(null);
    const result = await mutateJSON<Provider>('POST', '/api/v1/providers', {
      type: form.type,
      name: form.name,
      base_url: form.base_url,
      model: form.model,
      api_key: form.api_key || undefined,
      is_local: form.is_local,
      timeout_seconds: form.timeout_seconds,
    });
    setSubmitting(false);
    if (result.ok) {
      setForm(emptyForm());
      onCreated();
    } else {
      setError(result.error ?? 'create_failed');
    }
  }

  return (
    <div className="sentinel-card" style={{ display: 'flex', gap: '0.75rem', alignItems: 'flex-end', flexWrap: 'wrap' }}>
      <label>
        Type
        <br />
        <select value={form.type} onChange={(e) => setForm({ ...form, type: e.target.value as ProviderType })}>
          {PROVIDER_TYPES.map((t) => (
            <option key={t.value} value={t.value}>
              {t.label}
            </option>
          ))}
        </select>
      </label>
      <label>
        Display name
        <br />
        <input value={form.name} onChange={(e) => setForm({ ...form, name: e.target.value })} placeholder="e.g. Local Ollama" />
      </label>
      <label>
        Base URL
        <br />
        <input
          value={form.base_url}
          onChange={(e) => setForm({ ...form, base_url: e.target.value })}
          placeholder="http://host.docker.internal:11434"
          style={{ width: '16rem' }}
        />
      </label>
      <label>
        Model
        <br />
        <input value={form.model} onChange={(e) => setForm({ ...form, model: e.target.value })} placeholder="e.g. llama3" />
      </label>
      <label>
        API key
        <br />
        <input
          type="password"
          value={form.api_key}
          onChange={(e) => setForm({ ...form, api_key: e.target.value })}
          placeholder="leave blank if none"
        />
      </label>
      <label>
        <input type="checkbox" checked={form.is_local} onChange={(e) => setForm({ ...form, is_local: e.target.checked })} /> Local
      </label>
      <button onClick={submit} disabled={submitting}>
        {submitting ? 'Adding…' : 'Add provider'}
      </button>
      {error && <span className="sentinel-status-bad">{error}</span>}
    </div>
  );
}

function ProviderRow({ provider, onChanged }: { provider: Provider; onChanged: (p: Provider) => void }) {
  const [testing, setTesting] = useState(false);
  const [testResult, setTestResult] = useState<ProviderTestResult | null>(null);

  async function toggleEnabled() {
    const result = await mutateJSON<Provider>('PATCH', `/api/v1/providers/${provider.id}`, { enabled: !provider.enabled });
    if (result.ok && result.data) onChanged(result.data);
  }

  async function testConnection() {
    setTesting(true);
    setTestResult(null);
    const result = await mutateJSON<ProviderTestResult>('POST', `/api/v1/providers/${provider.id}/test`);
    setTesting(false);
    if (result.ok && result.data) {
      setTestResult(result.data);
      onChanged(result.data.provider);
    }
  }

  return (
    <>
      <tr>
        <td>{provider.name}</td>
        <td>{provider.type}</td>
        <td>{provider.model || '—'}</td>
        <td>{provider.has_api_key ? 'yes' : 'no'}</td>
        <td>{provider.enabled ? 'enabled' : 'disabled'}</td>
        <td className={HEALTH_CLASS[provider.health_status]}>{provider.health_status}</td>
        <td>
          <button onClick={testConnection} disabled={testing}>
            {testing ? 'Testing…' : 'Test connection'}
          </button>{' '}
          <button onClick={toggleEnabled}>{provider.enabled ? 'Disable' : 'Enable'}</button>
        </td>
      </tr>
      {testResult && (
        <tr>
          <td colSpan={7} className={testResult.status === 'ok' ? 'sentinel-status-ok' : 'sentinel-status-bad'}>
            {testResult.message} ({testResult.latency_ms}ms)
          </td>
        </tr>
      )}
    </>
  );
}

function TaskRouting({ providers, routes, onChanged }: { providers: Provider[]; routes: Record<string, string>; onChanged: (r: Record<string, string>) => void }) {
  async function setRoute(task: string, providerID: string) {
    const result = await mutateJSON<TaskRoutingResponse>('PATCH', '/api/v1/providers/routing', {
      routes: { [task]: providerID },
    });
    if (result.ok && result.data) onChanged(result.data.routes);
  }

  return (
    <div className="sentinel-card" style={{ marginTop: '1rem' }}>
      <h3>Task routing</h3>
      <p className="sentinel-status-unknown">Choose which provider handles each AI-assisted task. Leave unset to disable AI for that task.</p>
      <table className="sentinel-table">
        <thead>
          <tr>
            <th>Task</th>
            <th>Provider</th>
          </tr>
        </thead>
        <tbody>
          {AI_TASKS.map((task) => (
            <tr key={task.value}>
              <td>{task.label}</td>
              <td>
                <select value={routes[task.value] ?? ''} onChange={(e) => setRoute(task.value, e.target.value)}>
                  <option value="">None (AI disabled for this task)</option>
                  {providers.map((p) => (
                    <option key={p.id} value={p.id}>
                      {p.name}
                    </option>
                  ))}
                </select>
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

export default function AiProvidersPage() {
  const [providers, setProviders] = useState<Provider[]>([]);
  const [routes, setRoutes] = useState<Record<string, string>>({});

  const load = useCallback(async () => {
    const [providersRes, routingRes] = await Promise.all([
      fetchJSON<ProvidersResponse>('/api/v1/providers'),
      fetchJSON<TaskRoutingResponse>('/api/v1/providers/routing'),
    ]);
    setProviders(providersRes?.providers ?? []);
    setRoutes(routingRes?.routes ?? {});
  }, []);

  useEffect(() => {
    (async () => {
      await load();
    })();
  }, [load]);

  const enabledProviders = useMemo(() => providers.filter((p) => p.enabled), [providers]);

  function updateProvider(updated: Provider) {
    setProviders((prev) => prev.map((p) => (p.id === updated.id ? updated : p)));
  }

  return (
    <>
      <h2>AI Providers</h2>
      <p>
        Configure Ollama, OpenAI, Anthropic, Gemini, Azure OpenAI, or an OpenAI-compatible endpoint. API keys are
        encrypted at rest and never returned through this API — only whether a key is configured. E2E Sentinel remains
        fully usable with no provider configured or enabled.
      </p>

      <AddProviderForm onCreated={load} />

      <div className="sentinel-card" style={{ marginTop: '1rem' }}>
        {providers.length === 0 ? (
          <p className="sentinel-status-unknown">No providers configured yet.</p>
        ) : (
          <table className="sentinel-table">
            <thead>
              <tr>
                <th>Name</th>
                <th>Type</th>
                <th>Model</th>
                <th>API key</th>
                <th>State</th>
                <th>Health</th>
                <th>Actions</th>
              </tr>
            </thead>
            <tbody>
              {providers.map((p) => (
                <ProviderRow key={p.id} provider={p} onChanged={updateProvider} />
              ))}
            </tbody>
          </table>
        )}
      </div>

      <TaskRouting providers={enabledProviders} routes={routes} onChanged={setRoutes} />
    </>
  );
}
