'use client';

import { useCallback, useEffect, useState } from 'react';
import {
  ROLES,
  fetchJSON,
  mutateJSON,
  type AuthStatusResponse,
  type CurrentUser,
  type UsersResponse,
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

export default function SettingsPage() {
  return (
    <>
      <h2>Settings</h2>
      <p>Storage, retention, and RBAC administration for this E2E Sentinel instance.</p>
      <UserManagement />
    </>
  );
}
