'use client';

import Link from 'next/link';
import { usePathname } from 'next/navigation';

const NAV_ITEMS = [
  { href: '/', label: 'Dashboard' },
  { href: '/projects', label: 'Projects' },
  { href: '/discovery', label: 'Discovery' },
  { href: '/application-map', label: 'Application Map' },
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

export function NavShell({ children }: { children: React.ReactNode }) {
  const pathname = usePathname();

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
      </nav>
      <main className="sentinel-main">{children}</main>
    </div>
  );
}
