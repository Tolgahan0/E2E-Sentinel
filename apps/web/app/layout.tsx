import type { Metadata } from 'next';
import { NavShell } from '@/components/NavShell';
import './globals.css';

export const metadata: Metadata = {
  title: 'E2E Sentinel',
  description:
    'E2E Sentinel analyzes repository structure, runtime services, API schemas, application routes, existing tests, and observed behavior to generate high-confidence test recommendations and evidence-backed failure reports.',
};

export default function RootLayout({ children }: { children: React.ReactNode }) {
  return (
    <html lang="en">
      <body>
        <NavShell>{children}</NavShell>
      </body>
    </html>
  );
}
