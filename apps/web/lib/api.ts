export interface ReadyResponse {
  ready: boolean;
  checks: Record<string, string>;
}

export interface HealthResponse {
  status: string;
}

export interface AuditEvent {
  ID: string;
  ActionType: string;
  ResourceType: string;
  ResourceID: string;
  Actor: string;
  Metadata: Record<string, unknown>;
  CreatedAt: string;
}

export interface AuditEventsResponse {
  events: AuditEvent[] | null;
}

/**
 * fetchJSON calls a same-origin API route (proxied to sentinel-api by
 * next.config.js) and never sends credentials cross-origin. It returns
 * null on any network or non-2xx failure rather than throwing, so page
 * components can render an explicit "unavailable" state instead of
 * crashing the dashboard when the API is down.
 */
export async function fetchJSON<T>(path: string): Promise<T | null> {
  try {
    const res = await fetch(path, { cache: 'no-store' });
    if (!res.ok) {
      return null;
    }
    return (await res.json()) as T;
  } catch {
    return null;
  }
}
