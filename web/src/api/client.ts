import type {
  ApproveResponse,
  ConfigOverridesResponse,
  ConfigRolesResponse,
  EventStreamEnvelope,
  ModelOverride,
  PlanAuditStatsResponse,
  PlanRunDetailResponse,
  PlanSubmitResponse,
  QueueAddItem,
  QueueState,
  RejectResponse,
  SessionDetailResponse,
  SessionListResponse,
  StatsModelsResponse,
  StatsSummaryResponse,
  StatsTrendsResponse,
  WorkflowDetailResponse,
  WorkflowDiffResponse,
  WorkflowListResponse,
  WorkflowScopeResponse,
  WorkOrderContentResponse,
  WorkOrderListResponse,
  WorkOrderUpdateResponse,
} from "../types/api";

export interface ListSessionsOptions {
  state?: string;
  limit?: number;
}

export interface OpenEventStreamOptions {
  workflowId: string;
  cursor?: number;
  onEvent: (event: EventStreamEnvelope) => void;
  onOpen?: (event: Event) => void;
  onError?: (error: Event) => void;
}

async function fetchJSON<T>(path: string): Promise<T> {
  const response = await fetch(path, {
    headers: {
      Accept: "application/json",
    },
  });

  const payload = (await response.json().catch(() => ({}))) as { error?: string };
  if (!response.ok) {
    throw new Error(payload.error || `Request failed: ${response.status}`);
  }
  return payload as T;
}

export async function listSessions(options: ListSessionsOptions = {}): Promise<SessionListResponse> {
  const params = new URLSearchParams();
  if (options.state) {
    params.set("state", options.state);
  }
  if (typeof options.limit === "number") {
    params.set("limit", String(options.limit));
  }

  const query = params.toString();
  return fetchJSON<SessionListResponse>(query ? `/api/sessions?${query}` : "/api/sessions");
}

export async function getSession(id: string): Promise<SessionDetailResponse> {
  return fetchJSON<SessionDetailResponse>(`/api/sessions/${id}`);
}

export async function getPlanAuditStats(limit = 6): Promise<PlanAuditStatsResponse> {
  return fetchJSON<PlanAuditStatsResponse>(`/api/stats/plan-audit?limit=${limit}`);
}

export async function getPlanRun(id: string): Promise<PlanRunDetailResponse> {
  return fetchJSON<PlanRunDetailResponse>(`/api/plan-runs/${encodeURIComponent(id)}`);
}

export function openEventStream(options: OpenEventStreamOptions): EventSource {
  const params = new URLSearchParams({ workflow_id: options.workflowId });
  if (typeof options.cursor === "number") {
    params.set("cursor", String(options.cursor));
  }

  const source = new EventSource(`/api/events/stream?${params.toString()}`);
  if (options.onOpen) {
    source.onopen = options.onOpen;
  }
  source.onmessage = (event) => {
    options.onEvent(JSON.parse(event.data) as EventStreamEnvelope);
  };
  if (options.onError) {
    source.onerror = options.onError;
  }
  return source;
}

export interface ListWorkflowsOptions {
  status?: string;
  project?: string;
  session_id?: string;
  limit?: number;
  offset?: number;
}

export async function listWorkflows(options: ListWorkflowsOptions = {}): Promise<WorkflowListResponse> {
  const params = new URLSearchParams();
  if (options.status) params.set("status", options.status);
  if (options.project) params.set("project", options.project);
  if (options.session_id) params.set("session_id", options.session_id);
  if (typeof options.limit === "number") params.set("limit", String(options.limit));
  if (typeof options.offset === "number") params.set("offset", String(options.offset));

  const query = params.toString();
  return fetchJSON<WorkflowListResponse>(query ? `/api/workflows?${query}` : "/api/workflows");
}

export async function getWorkflow(id: string): Promise<WorkflowDetailResponse> {
  return fetchJSON<WorkflowDetailResponse>(`/api/workflows/${encodeURIComponent(id)}`);
}

async function postJSON<T>(path: string, body: unknown): Promise<T> {
  const response = await fetch(path, {
    method: "POST",
    headers: {
      Accept: "application/json",
      "Content-Type": "application/json",
    },
    body: JSON.stringify(body),
  });

  const payload = (await response.json().catch(() => ({}))) as { error?: string };
  if (!response.ok) {
    throw new Error(payload.error || `Request failed: ${response.status}`);
  }
  return payload as T;
}

export async function getWorkflowDiff(id: string): Promise<WorkflowDiffResponse> {
  return fetchJSON<WorkflowDiffResponse>(`/api/workflows/${encodeURIComponent(id)}/diff`);
}

export async function getWorkflowScope(id: string): Promise<WorkflowScopeResponse> {
  return fetchJSON<WorkflowScopeResponse>(`/api/workflows/${encodeURIComponent(id)}/scope`);
}

export async function approveWorkflow(
  id: string,
  options: { reindex: boolean },
): Promise<ApproveResponse> {
  return postJSON<ApproveResponse>(
    `/api/workflows/${encodeURIComponent(id)}/approve`,
    options,
  );
}

export async function rejectWorkflow(
  id: string,
  options: { reason?: string },
): Promise<RejectResponse> {
  return postJSON<RejectResponse>(
    `/api/workflows/${encodeURIComponent(id)}/reject`,
    options,
  );
}

// --- Queue API ---

export async function getQueue(): Promise<QueueState> {
  return fetchJSON<QueueState>("/api/queue");
}

export async function addQueueItems(items: QueueAddItem[]): Promise<QueueState> {
  return postJSON<QueueState>("/api/queue", { items });
}

export async function removeQueueItem(id: string): Promise<QueueState> {
  const response = await fetch(`/api/queue/${encodeURIComponent(id)}`, {
    method: "DELETE",
    headers: { Accept: "application/json" },
  });
  const payload = (await response.json().catch(() => ({}))) as { error?: string };
  if (!response.ok) throw new Error(payload.error || `Request failed: ${response.status}`);
  return payload as QueueState;
}

export async function reorderQueue(order: string[]): Promise<QueueState> {
  return postJSON<QueueState>("/api/queue/reorder", { order });
}

export async function startQueue(): Promise<QueueState> {
  return postJSON<QueueState>("/api/queue/start", {});
}

export async function pauseQueue(): Promise<QueueState> {
  return postJSON<QueueState>("/api/queue/pause", {});
}

export async function continueQueue(): Promise<QueueState> {
  return postJSON<QueueState>("/api/queue/continue", {});
}

// --- Work Order & Plan API ---

export async function listWorkOrders(): Promise<WorkOrderListResponse> {
  return fetchJSON<WorkOrderListResponse>("/api/work-orders");
}

export async function getWorkOrder(filename: string): Promise<WorkOrderContentResponse> {
  return fetchJSON<WorkOrderContentResponse>(`/api/work-orders/${encodeURIComponent(filename)}`);
}

export async function updateWorkOrder(filename: string, content: string): Promise<WorkOrderUpdateResponse> {
  const response = await fetch(`/api/work-orders/${encodeURIComponent(filename)}`, {
    method: "PUT",
    headers: { "Content-Type": "application/json", Accept: "application/json" },
    body: JSON.stringify({ content }),
  });
  const payload = (await response.json().catch(() => ({}))) as { error?: string };
  if (!response.ok) throw new Error(payload.error || `Request failed: ${response.status}`);
  return payload as WorkOrderUpdateResponse;
}

export async function submitPlan(options: { spec_content?: string; spec_file_path?: string }): Promise<PlanSubmitResponse> {
  return postJSON<PlanSubmitResponse>("/api/plan", options);
}

async function putJSON<T>(path: string, body: unknown): Promise<T> {
  const response = await fetch(path, {
    method: "PUT",
    headers: {
      Accept: "application/json",
      "Content-Type": "application/json",
    },
    body: JSON.stringify(body),
  });

  const payload = (await response.json().catch(() => ({}))) as { error?: string };
  if (!response.ok) {
    throw new Error(payload.error || `Request failed: ${response.status}`);
  }
  return payload as T;
}

// --- Config API ---

export async function getConfigRoles(): Promise<ConfigRolesResponse> {
  return fetchJSON<ConfigRolesResponse>("/api/config/roles");
}

export async function getConfigOverrides(): Promise<ConfigOverridesResponse> {
  return fetchJSON<ConfigOverridesResponse>("/api/config/overrides");
}

export async function putConfigOverrides(
  overrides: Record<string, ModelOverride>,
): Promise<ConfigOverridesResponse> {
  return putJSON<ConfigOverridesResponse>("/api/config/overrides", { overrides });
}

// --- Stats API ---

export async function getStatsSummary(range_?: string, project?: string): Promise<StatsSummaryResponse> {
  const params = new URLSearchParams();
  if (range_) params.set("range", range_);
  if (project) params.set("project", project);
  const query = params.toString();
  return fetchJSON<StatsSummaryResponse>(query ? `/api/stats/summary?${query}` : "/api/stats/summary");
}

export async function getStatsTrends(range_?: string, project?: string): Promise<StatsTrendsResponse> {
  const params = new URLSearchParams();
  if (range_) params.set("range", range_);
  if (project) params.set("project", project);
  const query = params.toString();
  return fetchJSON<StatsTrendsResponse>(query ? `/api/stats/trends?${query}` : "/api/stats/trends");
}

export async function getStatsModels(range_?: string): Promise<StatsModelsResponse> {
  const params = new URLSearchParams();
  if (range_) params.set("range", range_);
  const query = params.toString();
  return fetchJSON<StatsModelsResponse>(query ? `/api/stats/models?${query}` : "/api/stats/models");
}
