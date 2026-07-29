import type { NextRequest } from 'next/server';
import { proxyDelete, proxyGet, proxyPatch, proxyPost } from '@/lib/apiProxy';

type RouteContext = { params: Promise<{ path: string[] }> };

export async function GET(req: NextRequest, context: RouteContext) {
  const { path } = await context.params;
  return proxyGet(`/api/v1/${path.join('/')}`, req.nextUrl.search, req.headers.get('authorization'));
}

export async function POST(req: NextRequest, context: RouteContext) {
  const { path } = await context.params;
  return proxyPost(`/api/v1/${path.join('/')}`, req.nextUrl.search, await req.text(), req.headers.get('authorization'));
}

export async function PATCH(req: NextRequest, context: RouteContext) {
  const { path } = await context.params;
  return proxyPatch(`/api/v1/${path.join('/')}`, req.nextUrl.search, await req.text(), req.headers.get('authorization'));
}

export async function DELETE(req: NextRequest, context: RouteContext) {
  const { path } = await context.params;
  return proxyDelete(`/api/v1/${path.join('/')}`, req.nextUrl.search, req.headers.get('authorization'));
}
