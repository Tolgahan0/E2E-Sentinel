import { NextResponse } from 'next/server';

/**
 * Server-only helper backing the /api/* route handlers. SENTINEL_API_URL
 * is read per-request (not baked in at build time), so a single container
 * image works against whatever sentinel-api address the deployment sets.
 */
function apiOrigin(): string {
  return process.env.SENTINEL_API_URL || 'http://localhost:8080';
}

export async function proxyGet(upstreamPath: string, search: string): Promise<NextResponse> {
  try {
    const res = await fetch(`${apiOrigin()}${upstreamPath}${search}`, { cache: 'no-store' });
    const body = await res.text();
    return new NextResponse(body, {
      status: res.status,
      headers: { 'Content-Type': res.headers.get('content-type') ?? 'application/json' },
    });
  } catch {
    return NextResponse.json({ error: 'sentinel_api_unreachable' }, { status: 502 });
  }
}
