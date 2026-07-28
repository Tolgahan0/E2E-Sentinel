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

export interface Project {
  id: string;
  name: string;
  slug: string;
  repository_path: string;
  repository_type: string;
  default_branch: string;
  discovery_status: 'never_run' | 'running' | 'completed' | 'failed';
  current_mode: string;
  last_discovered_at: string | null;
  created_at: string;
  updated_at: string;
}

export interface ProjectsResponse {
  projects: Project[] | null;
}

export interface Environment {
  id: string;
  project_id: string;
  name: string;
  type: string;
  base_url: string;
  classification: 'local' | 'development' | 'test' | 'staging' | 'production' | 'unknown';
  is_production: boolean;
  allow_mutations: boolean;
  allow_load_tests: boolean;
  allow_active_security_scan: boolean;
}

export interface EnvironmentsResponse {
  environments: Environment[] | null;
}

export interface DiscoveryFinding {
  category: string;
  name: string;
  path: string;
  confidence: 'high' | 'medium' | 'low';
  evidence: Record<string, unknown>;
}

export interface DiscoveryResponse {
  discovery_run_id: string;
  findings: DiscoveryFinding[] | null;
}

export interface DiscoveredService {
  id: string;
  name: string;
  kind: string;
  runtime: string;
  source_path: string;
  container_name: string;
  image: string;
  ports: string[];
  dependencies: string[];
  metadata: {
    status?: string;
    status_text?: string;
    env_var_names?: string[];
    profiles?: string[];
    has_build?: boolean;
    [key: string]: unknown;
  };
  confidence: 'high' | 'medium' | 'low';
}

export interface ServicesResponse {
  services: DiscoveredService[] | null;
}

export interface GraphNode {
  id: string;
  node_type: string;
  label: string;
  source_reference: string;
  runtime_reference: string;
  metadata: Record<string, unknown>;
  confidence: 'high' | 'medium' | 'low';
}

export interface GraphEdge {
  id: string;
  source_node_id: string;
  target_node_id: string;
  source_node_type: string;
  source_label: string;
  target_node_type: string;
  target_label: string;
  relation_type: string;
  evidence: Record<string, unknown>;
  confidence: 'high' | 'medium' | 'low';
}

export interface GraphResponse {
  nodes: GraphNode[] | null;
  edges: GraphEdge[] | null;
}

export interface TestCase {
  id: string;
  title: string;
  description: string;
  category: string;
  framework: string;
  status: string;
  risk_level: 'high' | 'medium' | 'low';
  priority: 'P0' | 'P1' | 'P2' | 'P3';
  confidence: 'high' | 'medium' | 'low';
  source: string;
  steps: string[];
  assertions: string[];
  required_credentials: string[];
  is_mutating: boolean;
  is_production_safe: boolean;
  approval_status: 'pending' | 'approved' | 'rejected';
}

export interface TestsResponse {
  tests: TestCase[] | null;
}

export type RunStatus = 'queued' | 'running' | 'passed' | 'failed' | 'cancelled' | 'error';

export interface TestRun {
  id: string;
  project_id: string;
  test_case_id: string;
  status: RunStatus;
  runner_type: string;
  exit_code: number | null;
  summary: string;
  started_at: string;
  finished_at: string | null;
}

export interface RunsResponse {
  runs: TestRun[] | null;
}

export interface RunArtifact {
  id: string;
  kind: string;
  mime_type: string;
  size_bytes: number;
  checksum: string;
}

export interface ArtifactsResponse {
  artifacts: RunArtifact[] | null;
}

export type ProviderType = 'ollama' | 'openai' | 'anthropic' | 'gemini' | 'azure_openai' | 'openai_compatible';

export const PROVIDER_TYPES: { value: ProviderType; label: string }[] = [
  { value: 'ollama', label: 'Ollama (local)' },
  { value: 'openai', label: 'OpenAI' },
  { value: 'anthropic', label: 'Anthropic' },
  { value: 'gemini', label: 'Google Gemini' },
  { value: 'azure_openai', label: 'Azure OpenAI' },
  { value: 'openai_compatible', label: 'OpenAI-compatible endpoint' },
];

export interface Provider {
  id: string;
  type: ProviderType;
  name: string;
  base_url: string;
  model: string;
  has_api_key: boolean;
  is_local: boolean;
  enabled: boolean;
  capabilities: string[];
  timeout_seconds: number;
  max_tokens: number;
  temperature: number;
  health_status: 'unknown' | 'ok' | 'error';
  last_checked_at: string | null;
}

export interface ProvidersResponse {
  providers: Provider[] | null;
}

export interface ProviderTestResult {
  provider: Provider;
  status: 'ok' | 'error';
  message: string;
  latency_ms: number;
}

export const AI_TASKS: { value: string; label: string }[] = [
  { value: 'architecture_analysis', label: 'Architecture analysis' },
  { value: 'test_planning', label: 'Test planning' },
  { value: 'test_generation', label: 'Test generation' },
  { value: 'failure_analysis', label: 'Failure analysis' },
  { value: 'fix_generation', label: 'Fix generation' },
  { value: 'report_summarization', label: 'Report summarization' },
];

export interface TaskRoutingResponse {
  routes: Record<string, string>;
}

/**
 * fetchJSON calls a same-origin /api/* route (a server-side Route Handler
 * that proxies to sentinel-api, reading SENTINEL_API_URL at request time)
 * and never sends credentials cross-origin. It returns null on any
 * network or non-2xx failure rather than throwing, so page components can
 * render an explicit "unavailable" state instead of crashing.
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

export interface MutationResult<T> {
  ok: boolean;
  status: number;
  data: T | null;
  error: string | null;
}

/**
 * mutateJSON POSTs/PATCHes a JSON body to a same-origin /api/* route and
 * always resolves (never throws), surfacing the API's error string on
 * failure so forms can show it inline.
 */
export async function mutateJSON<T>(
  method: 'POST' | 'PATCH',
  path: string,
  body?: unknown,
): Promise<MutationResult<T>> {
  try {
    const res = await fetch(path, {
      method,
      headers: { 'Content-Type': 'application/json' },
      body: body !== undefined ? JSON.stringify(body) : undefined,
    });
    const data = (await res.json().catch(() => null)) as (T & { error?: string }) | null;
    return {
      ok: res.ok,
      status: res.status,
      data: res.ok ? data : null,
      error: !res.ok ? data?.error ?? `request_failed_${res.status}` : null,
    };
  } catch {
    return { ok: false, status: 0, data: null, error: 'network_error' };
  }
}
