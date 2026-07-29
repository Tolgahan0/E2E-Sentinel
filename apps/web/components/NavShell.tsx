'use client';

import { useEffect, useState } from 'react';
import Link from 'next/link';
import { usePathname, useRouter } from 'next/navigation';
import {
  clearStoredToken,
  fetchJSON,
  getStoredToken,
  mutateJSON,
  type AuthStatusResponse,
  type CurrentUser,
} from '@/lib/api';

const NAV_ITEMS = [
  { href: '/', label: 'Dashboard' },
  { href: '/projects', label: 'Projects' },
  { href: '/discovery', label: 'Discovery' },
  { href: '/application-map', label: 'Application Map' },
  { href: '/kubernetes', label: 'Kubernetes' },
  { href: '/test-inventory', label: 'Test Inventory' },
  { href: '/runs', label: 'Runs' },
  { href: '/bugs', label: 'Bugs' },
  { href: '/fix-proposals', label: 'Fix Proposals' },
  { href: '/ai-providers', label: 'AI Providers' },
  { href: '/environments', label: 'Environments' },
  { href: '/approvals', label: 'Approvals' },
  { href: '/audit-logs', label: 'Audit Logs' },
  { href: '/settings', label: 'Settings' },
] as const;

// authEnabled starts `null` (unknown, still checking) rather than
// `false`, so a page never briefly renders as if auth were off before
// GET /auth/status has answered.
type AuthState = { enabled: boolean; user: CurrentUser | null } | null;

export function NavShell({ children }: { children: React.ReactNode }) {
  const pathname = usePathname();
  const router = useRouter();
  const [auth, setAuth] = useState<AuthState>(null);

  useEffect(() => {
    (async () => {
      const status = await fetchJSON<AuthStatusResponse>('/api/v1/auth/status');
      const enabled = status?.auth_enabled ?? false;
      if (!enabled) {
        setAuth({ enabled: false, user: null });
        return;
      }
      if (!getStoredToken()) {
        setAuth({ enabled: true, user: null });
        if (pathname !== '/login') router.replace('/login');
        return;
      }
      const user = await fetchJSON<CurrentUser>('/api/v1/auth/me');
      if (!user) {
        clearStoredToken();
        setAuth({ enabled: true, user: null });
        if (pathname !== '/login') router.replace('/login');
        return;
      }
      setAuth({ enabled: true, user });
    })();
  }, [pathname, router]);

  async function logout() {
    await mutateJSON('POST', '/api/v1/auth/logout');
    clearStoredToken();
    setAuth({ enabled: true, user: null });
    router.replace('/login');
  }

  // The login page renders standalone, with no nav chrome — showing the
  // rest of the app's navigation while logged out would be misleading.
  if (pathname === '/login') {
    return <>{children}</>;
  }

  // Auth is enabled but we don't have a confirmed user yet (still
  // checking, or a redirect to /login is in flight) — render nothing
  // rather than flashing protected content.
  if (auth?.enabled && !auth.user) {
    return <p className="sentinel-status-unknown" style={{ padding: '2rem' }}>Checking authentication&hellip;</p>;
  }

  return (
    <div className="sentinel-shell">
      <nav className="sentinel-nav" aria-label="Main navigation">
        <h1>E2E Sentinel</h1>
        <ul>
          {NAV_ITEMS.map((item) => {
            const current = pathname === item.href;
            return (
              <li key={item.href}>
                <Link href={item.href} aria-current={current ? 'page' : undefined}>
                  {item.label}
                </Link>
              </li>
            );
          })}
        </ul>
        {auth?.enabled && auth.user && (
          <div className="sentinel-status-unknown" style={{ marginTop: '1rem', fontSize: '0.85rem' }}>
            Signed in as {auth.user.email} ({auth.user.role})
            <br />
            <button onClick={logout}>Log out</button>
          </div>
        )}
      </nav>
      <main className="sentinel-main">{children}</main>
    </div>
  );
}
