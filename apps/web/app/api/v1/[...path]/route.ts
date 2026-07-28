import type { NextRequest } from 'next/server';
import { proxyGet } from '@/lib/apiProxy';

export async function GET(req: NextRequest, context: { params: Promise<{ path: string[] }> }) {
  const { path } = await context.params;
  return proxyGet(`/api/v1/${path.join('/')}`, req.nextUrl.search);
}
