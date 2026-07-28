import { NextResponse } from 'next/server';

/**
 * Server-only helper backing the /api/* route handlers. SENTINEL_API_URL
 * is read per-request (not baked in at build time), so a single container
 * image works against whatever sentinel-api address the deployment sets.
 */
function apiOrigin(): string {
  return process.env.SENTINEL_API_URL || 'http://localhost:8080';
}

async function proxy(method: string, upstreamPath: string, search: string, body?: string): Promise<NextResponse> {
  try {
    const res = await fetch(`${apiOrigin()}${upstreamPath}${search}`, {
      method,
      cache: 'no-store',
      headers: body !== undefined ? { 'Content-Type': 'application/json' } : undefined,
      body,
    });
    const responseBody = await res.text();
    return new NextResponse(responseBody, {
      status: res.status,
      headers: { 'Content-Type': res.headers.get('content-type') ?? 'application/json' },
    });
  } catch {
    return NextResponse.json({ error: 'sentinel_api_unreachable' }, { status: 502 });
  }
}

export function proxyGet(upstreamPath: string, search: string): Promise<NextResponse> {
  return proxy('GET', upstreamPath, search);
}

export function proxyPost(upstreamPath: string, search: string, body?: string): Promise<NextResponse> {
  return proxy('POST', upstreamPath, search, body);
}

export function proxyPatch(upstreamPath: string, search: string, body?: string): Promise<NextResponse> {
  return proxy('PATCH', upstreamPath, search, body);
}
