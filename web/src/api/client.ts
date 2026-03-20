import type {
  EventStreamEnvelope,
  PlanAuditStatsResponse,
  SessionDetailResponse,
  SessionListResponse,
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
