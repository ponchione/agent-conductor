import type {
  ApproveResponse,
  EventStreamEnvelope,
  PlanAuditStatsResponse,
  RejectResponse,
  SessionDetailResponse,
  SessionListResponse,
  WorkflowDetailResponse,
  WorkflowDiffResponse,
  WorkflowListResponse,
  WorkflowScopeResponse,
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
