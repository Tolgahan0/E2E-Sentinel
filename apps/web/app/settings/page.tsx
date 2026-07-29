'use client';

import { useCallback, useEffect, useState } from 'react';
import {
  ROLES,
  fetchJSON,
  mutateJSON,
  type AuthStatusResponse,
  type CurrentUser,
  type UsersResponse,
  type WebhookConfigResponse,
} from '@/lib/api';

function emptyForm() {
  return { email: '', password: '', role: 'viewer' };
}

function CreateUserForm({ onCreated }: { onCreated: () => void }) {
  const [form, setForm] = useState(emptyForm());
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);

  async function submit() {
    setSubmitting(true);
    setError(null);
    const result = await mutateJSON<CurrentUser>('POST', '/api/v1/users', form);
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
        Email
        <br />
        <input type="email" value={form.email} onChange={(e) => setForm({ ...form, email: e.target.value })} />
      </label>
      <label>
        Password
        <br />
        <input
          type="password"
          value={form.password}
          onChange={(e) => setForm({ ...form, password: e.target.value })}
          placeholder="12+ characters"
        />
      </label>
      <label>
        Role
        <br />
        <select value={form.role} onChange={(e) => setForm({ ...form, role: e.target.value })}>
          {ROLES.map((r) => (
            <option key={r.value} value={r.value}>
              {r.label}
            </option>
          ))}
        </select>
      </label>
      <button onClick={submit} disabled={submitting || !form.email || !form.password}>
        {submitting ? 'Creating…' : 'Create user'}
      </button>
      {error && <span className="sentinel-status-bad">{error}</span>}
    </div>
  );
}

function UserManagement() {
  const [authEnabled, setAuthEnabled] = useState<boolean | null>(null);
  const [users, setUsers] = useState<CurrentUser[]>([]);

  const load = useCallback(async () => {
    const [status, usersRes] = await Promise.all([
      fetchJSON<AuthStatusResponse>('/api/v1/auth/status'),
      fetchJSON<UsersResponse>('/api/v1/users'),
    ]);
    setAuthEnabled(status?.auth_enabled ?? false);
    setUsers(usersRes?.users ?? []);
  }, []);

  useEffect(() => {
    (async () => {
      await load();
    })();
  }, [load]);

  return (
    <div className="sentinel-card">
      <h3>Users &amp; roles</h3>
      {authEnabled === false && (
        <p className="sentinel-status-unknown">
          RBAC is disabled (SENTINEL_AUTH_ENABLED is not set) — every request is treated as fully privileged and
          login is not required. Accounts can still be created here in advance of turning RBAC on.
        </p>
      )}
      <CreateUserForm onCreated={load} />
      <table className="sentinel-table" style={{ marginTop: '1rem' }}>
        <thead>
          <tr>
            <th>Email</th>
            <th>Role</th>
          </tr>
        </thead>
        <tbody>
          {users.length === 0 ? (
            <tr>
              <td colSpan={2} className="sentinel-status-unknown">
                No users yet.
              </td>
            </tr>
          ) : (
            users.map((u) => (
              <tr key={u.id}>
                <td>{u.email}</td>
                <td>{u.role}</td>
              </tr>
            ))
          )}
        </tbody>
      </table>
    </div>
  );
}

function NotificationSettings() {
  const [url, setUrl] = useState('');
  const [configured, setConfigured] = useState(false);
  const [saving, setSaving] = useState(false);
  const [testing, setTesting] = useState(false);
  const [message, setMessage] = useState<{ text: string; ok: boolean } | null>(null);

  const load = useCallback(async () => {
    const res = await fetchJSON<WebhookConfigResponse>('/api/v1/notifications/webhook');
    setUrl(res?.url ?? '');
    setConfigured(res?.configured ?? false);
  }, []);

  useEffect(() => {
    (async () => {
      await load();
    })();
  }, [load]);

  async function save() {
    setSaving(true);
    setMessage(null);
    const result = await mutateJSON<WebhookConfigResponse>('PATCH', '/api/v1/notifications/webhook', { url });
    setSaving(false);
    if (result.ok) {
      setConfigured(result.data?.configured ?? false);
      setMessage({ text: url ? 'Saved.' : 'Cleared — notifications are now off.', ok: true });
    } else {
      setMessage({ text: result.error ?? 'save_failed', ok: false });
    }
  }

  async function sendTest() {
    setTesting(true);
    setMessage(null);
    const result = await mutateJSON('POST', '/api/v1/notifications/webhook/test');
    setTesting(false);
    setMessage(
      result.ok
        ? { text: 'Test notification delivered.', ok: true }
        : { text: `Delivery failed: ${result.error ?? 'unknown error'}`, ok: false },
    );
  }

  return (
    <div className="sentinel-card" style={{ marginTop: '1rem' }}>
      <h3>Notifications</h3>
      <p className="sentinel-status-unknown">
        A single webhook URL, POSTed to when a new bug report is created or a fix proposal starts
        waiting on review — so you don&apos;t have to keep the panel open to notice. No retry queue or
        delivery history in this version: a failed delivery is simply skipped.
      </p>
      <div style={{ display: 'flex', gap: '0.75rem', alignItems: 'flex-end', flexWrap: 'wrap' }}>
        <label style={{ flex: 1, minWidth: '20rem' }}>
          Webhook URL
          <br />
          <input
            value={url}
            onChange={(e) => setUrl(e.target.value)}
            placeholder="https://hooks.example.com/e2e-sentinel"
            style={{ width: '100%' }}
          />
        </label>
        <button onClick={save} disabled={saving}>
          {saving ? 'Saving…' : 'Save'}
        </button>
        <button onClick={sendTest} disabled={testing || !configured}>
          {testing ? 'Sending…' : 'Send test notification'}
        </button>
      </div>
      {message && <p className={message.ok ? 'sentinel-status-ok' : 'sentinel-status-bad'}>{message.text}</p>}
    </div>
  );
}

export default function SettingsPage() {
  return (
    <>
      <h2>Settings</h2>
      <p>Storage, retention, and RBAC administration for this E2E Sentinel instance.</p>
      <UserManagement />
      <NotificationSettings />
    </>
  );
}
