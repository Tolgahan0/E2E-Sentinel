'use client';

import { useState, type FormEvent } from 'react';
import { useRouter } from 'next/navigation';
import { mutateJSON, setStoredToken, type LoginResponse } from '@/lib/api';

export default function LoginPage() {
  const router = useRouter();
  const [email, setEmail] = useState('');
  const [password, setPassword] = useState('');
  const [error, setError] = useState<string | null>(null);
  const [submitting, setSubmitting] = useState(false);

  async function submit(e: FormEvent) {
    e.preventDefault();
    setSubmitting(true);
    setError(null);
    const result = await mutateJSON<LoginResponse>('POST', '/api/v1/auth/login', { email, password });
    setSubmitting(false);
    if (result.ok && result.data) {
      setStoredToken(result.data.token);
      router.replace('/');
      router.refresh();
    } else {
      setError(result.error ?? 'login_failed');
    }
  }

  return (
    <div style={{ maxWidth: '22rem', margin: '4rem auto', padding: '0 1rem' }}>
      <h2>Sign in to E2E Sentinel</h2>
      <form onSubmit={submit} className="sentinel-card" style={{ display: 'flex', flexDirection: 'column', gap: '0.75rem' }}>
        <label>
          Email
          <br />
          <input type="email" value={email} onChange={(e) => setEmail(e.target.value)} required style={{ width: '100%' }} />
        </label>
        <label>
          Password
          <br />
          <input type="password" value={password} onChange={(e) => setPassword(e.target.value)} required style={{ width: '100%' }} />
        </label>
        <button type="submit" disabled={submitting}>
          {submitting ? 'Signing in…' : 'Sign in'}
        </button>
        {error && <p className="sentinel-status-bad">{error}</p>}
      </form>
    </div>
  );
}
