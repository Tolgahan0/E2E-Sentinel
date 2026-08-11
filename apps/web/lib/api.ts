export interface ReadyResponse {
  ready: boolean;
  checks: Record<string, string>;
  // Which concrete runner backs test execution right now — the
  // Name() of the configured runs.Runner (e.g. "playwright-docker",
  // "playwright-local"), or "unconfigured" if none is. See
  // docs/RUNNER_ISOLATION.md's "Local process execution mode".
  test_execution: string;
  websocket_execution: string;
}

export interface HealthResponse {
  status: string;
}

export interface VersionResponse {
  current_version: string;
  latest_version: string;
  update_available: boolean;
  release_url: string;
  checked_at: string;
  check_error: string;
  update_check_enabled: boolean;
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
  // GitHub CI (internal/githubci) — github_ci_configured means a token
  // is stored; the token itself is never returned by the API.
  github_repo: string;
  github_ci_configured: boolean;
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
  // trigger_type is "manual" (someone clicked Run) or "ci" (internal/
  // githubci polled a new commit) — see commit_sha, only set for "ci".
  trigger_type: string;
  commit_sha: string;
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

// A visual diff never changes a run's pass/fail — it's a separate
// signal a human accepts (making current_artifact_id the new baseline)
// or ignores. baseline/current/diff_artifact_id are all served via the
// same GET /api/v1/artifacts/{id}/content endpoint regular screenshots use.
export interface VisualDiff {
  id: string;
  project_id: string;
  test_run_id: string;
  test_case_id: string;
  baseline_artifact_id: string;
  current_artifact_id: string;
  diff_artifact_id: string;
  percent_changed: number;
  status: 'pending_review' | 'accepted' | 'ignored';
  reviewed_by: string | null;
  reviewed_at: string | null;
  created_at: string;
}

export interface VisualDiffsResponse {
  visual_diffs: VisualDiff[] | null;
}

// Computed on read from a test case's real run history
// (internal/failures.AssessFlakiness) — never persisted, so this is
// always current, not a stale snapshot from the last failure.
export type FlakyAssessment = 'insufficient_evidence' | 'suspect' | 'flaky_candidate' | 'flaky' | 'likely_real_defect';

export interface FlakyTest {
  test_case_id: string;
  title: string;
  assessment: FlakyAssessment;
  total_runs: number;
  // Oldest-first, capped to the last 10 runs — a sparkline, not the
  // full history (see the Runs page for that).
  recent_statuses: RunStatus[];
}

export interface FlakyTestsResponse {
  flaky_tests: FlakyTest[] | null;
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

export interface BugNote {
  author: string;
  text: string;
  created_at: string;
}

export interface BugReport {
  id: string;
  project_id: string;
  failure_id: string;
  test_case_id: string;
  environment_id: string;
  title: string;
  severity: 'critical' | 'high' | 'medium' | 'low' | 'informational';
  failure_type: string;
  affected_service: string;
  affected_route: string;
  preconditions: string;
  steps_to_reproduce: string[];
  expected_result: string;
  actual_result: string;
  artifact_ids: string[];
  error_message: string;
  first_observed_at: string;
  last_observed_at: string;
  frequency: number;
  root_cause_hypothesis: string;
  root_cause_confidence: string;
  root_cause_is_unverified_hypothesis: boolean;
  flaky_assessment: string;
  related_graph_path: string;
  regression_test_ids: string[];
  possible_duplicate_of_id?: string;
  status: 'open' | 'resolved' | 'reopened';
  notes: BugNote[];
}

export interface BugsResponse {
  bugs: BugReport[] | null;
}

export interface FileApplyResult {
  path: string;
  action: 'created' | 'modified' | 'deleted';
  applied: boolean;
  error?: string;
}

export interface FixProposal {
  id: string;
  project_id: string;
  bug_id: string;
  title: string;
  description: string;
  risk_level: 'low' | 'medium' | 'high';
  assumptions: string;
  potential_side_effects: string;
  rollback_guidance: string;
  files_changed: string[];
  unified_diff: string;
  regression_test_ids: string[];
  ai_provider: string;
  ai_model: string;
  generated_at: string;
  approval_status: 'pending_review' | 'approved' | 'rejected' | 'revision_requested';
  workspace_dir?: string;
  workspace_apply_results?: FileApplyResult[];
  workspace_applied_at: string | null;
  repository_apply_results?: FileApplyResult[];
  repository_applied_at: string | null;
}

export interface FixProposalsResponse {
  fix_proposals: FixProposal[] | null;
}

export interface FixProposalApplyResult {
  fix_proposal: FixProposal;
  all_applied: boolean;
}

// Phase 10 Kubernetes discovery (spec §7.5) — opt-in, see GET
// /kube/status. Every resource kind that has no meaningful replica/pod
// health concept (namespace, service, ingress, gateway, configmap,
// secret, cronjob) reports status "not_applicable", never a guess.
export interface KubeStatusResponse {
  configured: boolean;
  namespace: string;
}

export type KubeResourceStatus = 'healthy' | 'degraded' | 'unknown' | 'not_applicable';

export interface KubeResource {
  id: string;
  namespace: string;
  kind: string;
  name: string;
  desired_replicas: number | null;
  ready_replicas: number | null;
  restart_count: number | null;
  status: KubeResourceStatus;
  metadata: Record<string, unknown>;
  last_seen_at: string;
}

export interface KubeResourcesResponse {
  resources: KubeResource[] | null;
}

export interface KubeDiscoverResponse {
  resources: KubeResource[] | null;
  warnings: string[] | null;
}

export interface KubeEvent {
  namespace: string;
  involved_kind: string;
  involved_name: string;
  reason: string;
  message: string;
  type: string;
  count: number;
  last_timestamp: string;
}

export interface KubeEventsResponse {
  events: KubeEvent[] | null;
}

export interface KubePodLogsResponse {
  logs: string;
}

// Outbound notifications (v1: bug_report.created, fix_proposal.pending_review)
// — one webhook URL, no retry queue, no delivery tracking; see
// docs/OPERATIONS.md for the documented ceiling.
export interface WebhookConfigResponse {
  configured: boolean;
  url: string;
}

// Phase 9 RBAC (opt-in — see GET /auth/status). The bearer token is
// stored client-side only in this tab's memory-backed storage
// (sessionStorage, not localStorage: it should not silently persist
// across a shared machine's browser restarts) and attached to every
// same-origin API call below when present. When auth is disabled
// (the default), no token ever exists and every call behaves exactly
// as in Phases 0-8.
const TOKEN_STORAGE_KEY = 'sentinel_auth_token';

export function getStoredToken(): string | null {
  if (typeof window === 'undefined') return null;
  return window.sessionStorage.getItem(TOKEN_STORAGE_KEY);
}

export function setStoredToken(token: string): void {
  if (typeof window === 'undefined') return;
  window.sessionStorage.setItem(TOKEN_STORAGE_KEY, token);
}

export function clearStoredToken(): void {
  if (typeof window === 'undefined') return;
  window.sessionStorage.removeItem(TOKEN_STORAGE_KEY);
}

export interface AuthStatusResponse {
  auth_enabled: boolean;
}

export interface CurrentUser {
  id: string;
  email: string;
  role: string;
}

export interface LoginResponse {
  token: string;
  expires_at: string;
  user: CurrentUser;
}

export const ROLES: { value: string; label: string }[] = [
  { value: 'viewer', label: 'Viewer' },
  { value: 'tester', label: 'Tester' },
  { value: 'developer', label: 'Developer' },
  { value: 'approver', label: 'Approver' },
  { value: 'administrator', label: 'Administrator' },
];

export interface UsersResponse {
  users: CurrentUser[] | null;
}

function authHeaders(): Record<string, string> {
  const token = getStoredToken();
  return token ? { Authorization: `Bearer ${token}` } : {};
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
    const res = await fetch(path, { cache: 'no-store', headers: authHeaders() });
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
 * mutateJSON POSTs/PATCHes/DELETEs a same-origin /api/* route and
 * always resolves (never throws), surfacing the API's error string on
 * failure so forms can show it inline. A 204 No Content response (e.g.
 * a successful DELETE) has no JSON body — data resolves to null, which
 * is expected there, not a parse failure.
 */
export async function mutateJSON<T>(
  method: 'POST' | 'PATCH' | 'DELETE',
  path: string,
  body?: unknown,
): Promise<MutationResult<T>> {
  try {
    const res = await fetch(path, {
      method,
      headers: { 'Content-Type': 'application/json', ...authHeaders() },
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
