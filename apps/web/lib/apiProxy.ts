import { NextResponse } from 'next/server';

/**
 * Server-only helper backing the /api/* route handlers. SENTINEL_API_URL
 * is read per-request (not baked in at build time), so a single container
 * image works against whatever sentinel-api address the deployment sets.
 */
function apiOrigin(): string {
  return process.env.SENTINEL_API_URL || 'http://localhost:8080';
}

async function proxy(method: string, upstreamPath: string, search: string, body?: string, authorization?: string | null): Promise<NextResponse> {
  try {
    const headers: Record<string, string> = {
      // This server-side proxy IS the trusted app — a request only
      // reaches sentinel-api through here, never directly from a
      // browser. sentinel-api's CSRF check (opt-in, only enforced once
      // RBAC is on) requires this custom header on mutating requests;
      // attaching it unconditionally here costs nothing when auth is
      // disabled (the check is a no-op then) and is what makes it a
      // real defense once auth is enabled — a forged cross-site request
      // can't reach this route with a custom header a browser would
      // send on the attacker's behalf.
      'X-Sentinel-Csrf': '1',
    };
    if (body !== undefined) headers['Content-Type'] = 'application/json';
    if (authorization) headers['Authorization'] = authorization;

    const res = await fetch(`${apiOrigin()}${upstreamPath}${search}`, {
      method,
      cache: 'no-store',
      headers,
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

export function proxyGet(upstreamPath: string, search: string, authorization?: string | null): Promise<NextResponse> {
  return proxy('GET', upstreamPath, search, undefined, authorization);
}

export function proxyPost(upstreamPath: string, search: string, body?: string, authorization?: string | null): Promise<NextResponse> {
  return proxy('POST', upstreamPath, search, body, authorization);
}

export function proxyPatch(upstreamPath: string, search: string, body?: string, authorization?: string | null): Promise<NextResponse> {
  return proxy('PATCH', upstreamPath, search, body, authorization);
}
