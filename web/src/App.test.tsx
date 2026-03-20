import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { EventStreamEnvelope } from "./types/api";
import App from "./App";

class MockEventSource {
  static instances: MockEventSource[] = [];

  url: string;
  onerror: ((event: Event) => void) | null = null;
  onmessage: ((event: MessageEvent) => void) | null = null;
  onopen: ((event: Event) => void) | null = null;
  close = vi.fn();

  constructor(url: string) {
    this.url = url;
    MockEventSource.instances.push(this);
  }

  open() {
    this.onopen?.(new Event("open"));
  }

  emit(event: EventStreamEnvelope) {
    this.onmessage?.({ data: JSON.stringify(event) } as MessageEvent);
  }

  fail() {
    this.onerror?.(new Event("error"));
  }
}

describe("App", () => {
  beforeEach(() => {
    vi.restoreAllMocks();
    MockEventSource.instances = [];
    window.history.replaceState({}, "", "/observability?view=sessions");
    vi.stubGlobal(
      "EventSource",
      vi.fn((url: string | URL) => new MockEventSource(String(url))),
    );
  });

  afterEach(() => {
    cleanup();
  });

  it("renders session details and switches the selected session", async () => {
    installFetchMock();

    render(<App />);

    await waitFor(() => {
      expect(screen.getByText("worker-scope.yaml")).toBeInTheDocument();
    });

    fireEvent.click(screen.getByRole("button", { name: /batch-reconciler completed/i }));

    await waitFor(() => {
      expect(screen.getByText("reconcile-2026.yaml")).toBeInTheDocument();
      expect(screen.getByText("review_bundle")).toBeInTheDocument();
      expect(window.location.search).toContain("session=session-complete-0002");
    });
  });

  it("filters sessions and keeps the filter in the query string", async () => {
    const fetchMock = installFetchMock();

    render(<App />);

    fireEvent.change(screen.getByLabelText("State Filter"), {
      target: { value: "completed" },
    });

    await waitFor(() => {
      expect(window.location.search).toContain("state=completed");
      expect(screen.getByRole("button", { name: /batch-reconciler completed/i })).toBeInTheDocument();
      expect(screen.queryByRole("button", { name: /agent-conductor running/i })).not.toBeInTheDocument();
      expect(screen.getByText("reconcile-2026.yaml")).toBeInTheDocument();
    });

    expect(
      fetchMock.mock.calls.some(([input]) => String(input).includes("/api/sessions?state=completed&limit=20")),
    ).toBe(true);
  });

  it("honors deep-linked session and filter state", async () => {
    window.history.replaceState(
      {},
      "",
      "/observability?view=sessions&session=session-complete-0002&state=completed",
    );

    installFetchMock();

    render(<App />);

    await waitFor(() => {
      expect(screen.getByRole("heading", { level: 2, name: "Session Dashboard" })).toBeInTheDocument();
      expect(screen.getByText("session-complete-0002")).toBeInTheDocument();
      expect(screen.getByText("reconcile-2026.yaml")).toBeInTheDocument();
      expect((screen.getByLabelText("State Filter") as HTMLSelectElement).value).toBe("completed");
    });
  });

  it("renders the empty state when no sessions match the filter", async () => {
    installFetchMock({ sessions: [] });

    render(<App />);

    await waitFor(() => {
      expect(screen.getByText("No sessions match the current filter.")).toBeInTheDocument();
      expect(screen.getByText("No session selected")).toBeInTheDocument();
      expect(screen.getByText("Select a session to render plan runs, pipeline runs, and artifacts.")).toBeInTheDocument();
    });
  });

  it("renders the session list error state", async () => {
    installFetchMock({ listError: "database unavailable" });

    render(<App />);

    await waitFor(() => {
      expect(screen.getByText("Unable to load sessions: database unavailable")).toBeInTheDocument();
      expect(screen.getByText("No session selected")).toBeInTheDocument();
    });
  });

  it("renders the audit dashboard and switches selected audit runs", async () => {
    window.history.replaceState({}, "", "/observability?view=audit");
    installFetchMock();

    render(<App />);

    await waitFor(() => {
      expect(screen.getByRole("heading", { level: 2, name: "Plan Audit Dashboard" })).toBeInTheDocument();
      expect(screen.getByText("audit-run-0001")).toBeInTheDocument();
      expect(window.location.search).toContain("audit_run=audit-run-0001");
    });

    fireEvent.click(screen.getByRole("button", { name: /reconcile-2026\.yaml unchanged/i }));

    await waitFor(() => {
      expect(screen.getByText("audit-run-0002")).toBeInTheDocument();
      expect(screen.getByText("No audit changes were recorded for this run.")).toBeInTheDocument();
      expect(window.location.search).toContain("audit_run=audit-run-0002");
    });
  });

  it("filters audit runs and keeps audit state in the query string", async () => {
    installFetchMock();
    window.history.replaceState({}, "", "/observability?view=audit");

    render(<App />);

    fireEvent.change(screen.getByLabelText("Audit Run Filter"), {
      target: { value: "unchanged" },
    });

    await waitFor(() => {
      expect(window.location.search).toContain("audit_filter=unchanged");
      expect(screen.queryByRole("button", { name: /ingestion-spec\.yaml changed/i })).not.toBeInTheDocument();
      expect(screen.getByRole("button", { name: /reconcile-2026\.yaml unchanged/i })).toBeInTheDocument();
      expect(screen.getByText("audit-run-0002")).toBeInTheDocument();
    });
  });

  it("honors deep-linked audit run and filter state", async () => {
    window.history.replaceState(
      {},
      "",
      "/observability?view=audit&audit_run=audit-run-0003&audit_filter=changed",
    );
    installFetchMock();

    render(<App />);

    await waitFor(() => {
      expect(screen.getByRole("heading", { level: 2, name: "Plan Audit Dashboard" })).toBeInTheDocument();
      expect((screen.getByLabelText("Audit Run Filter") as HTMLSelectElement).value).toBe("changed");
      expect(screen.getByText("audit-run-0003")).toBeInTheDocument();
      expect(window.location.search).toContain("audit_run=audit-run-0003");
    });
  });

  it("renders the audit empty state", async () => {
    window.history.replaceState({}, "", "/observability?view=audit");
    installFetchMock({ planAudit: emptyPlanAuditStats() });

    render(<App />);

    await waitFor(() => {
      expect(screen.getByText("No audit runs match the current filter.")).toBeInTheDocument();
      expect(screen.getByText("No run selected")).toBeInTheDocument();
      expect(screen.getByText("Select an audit run to inspect its counts, delta, and audit changes.")).toBeInTheDocument();
    });
  });

  it("renders the audit error state", async () => {
    window.history.replaceState({}, "", "/observability?view=audit");
    installFetchMock({ planAuditError: "plan audit unavailable" });

    render(<App />);

    await waitFor(() => {
      expect(screen.getByText("Unable to load plan audit stats: plan audit unavailable")).toBeInTheDocument();
      expect(screen.getByText("Audit detail is unavailable until the stats endpoint recovers.")).toBeInTheDocument();
    });
  });

  it("renders the live monitor and switches workflows", async () => {
    window.history.replaceState({}, "", "/observability?view=live&session=session-complete-0002");
    installFetchMock();

    render(<App />);

    await waitFor(() => {
      expect(screen.getByRole("heading", { level: 2, name: "Live Monitor" })).toBeInTheDocument();
      expect((screen.getByLabelText("Workflow Selection") as HTMLSelectElement).value).toBe("workflow-complete-0002");
      expect(lastEventSource().url).toContain("workflow_id=workflow-complete-0002");
    });

    const firstSource = lastEventSource();
    fireEvent.change(screen.getByLabelText("Workflow Selection"), {
      target: { value: "workflow-followup-0002" },
    });

    await waitFor(() => {
      expect((screen.getByLabelText("Workflow Selection") as HTMLSelectElement).value).toBe("workflow-followup-0002");
      expect(lastEventSource().url).toContain("workflow_id=workflow-followup-0002");
      expect(window.location.search).toContain("workflow=workflow-followup-0002");
    });

    expect(firstSource.close).toHaveBeenCalled();
  });

  it("renders replayed live events and dedupes by event id", async () => {
    window.history.replaceState({}, "", "/observability?view=live&session=session-complete-0002");
    installFetchMock();

    render(<App />);

    await waitFor(() => {
      expect(lastEventSource().url).toContain("workflow_id=workflow-complete-0002");
    });

    const source = lastEventSource();
    source.open();
    source.emit(liveEvent(1, "phase_start", { phase: "build", step: "compile" }));
    source.emit(liveEvent(1, "phase_start", { phase: "build", step: "compile" }));
    source.emit(liveEvent(2, "build_stdout", { output: "compiled 18 packages" }));
    source.emit(liveEvent(3, "run_complete", { result: "approved", status: "completed" }));

    await waitFor(() => {
      expect(screen.getByText("Phase Start")).toBeInTheDocument();
      expect(screen.getByText("Output compiled 18 packages")).toBeInTheDocument();
      expect(screen.getByText("Result approved")).toBeInTheDocument();
      expect(screen.getByText("3 buffered events")).toBeInTheDocument();
    });
  });

  it("shows reconnect state when the live stream errors", async () => {
    window.history.replaceState({}, "", "/observability?view=live&session=session-complete-0002");
    installFetchMock();

    render(<App />);

    await waitFor(() => {
      expect(lastEventSource().url).toContain("workflow_id=workflow-complete-0002");
    });

    const source = lastEventSource();
    source.open();
    source.fail();

    await waitFor(() => {
      expect(screen.getAllByText("reconnecting").length).toBeGreaterThan(0);
    });
  });

  it("renders the live empty workflow state", async () => {
    window.history.replaceState({}, "", "/observability?view=live&session=session-plan-only-0003");
    installFetchMock();

    render(<App />);

    await waitFor(() => {
      expect(screen.getByText("No workflows available for this session.")).toBeInTheDocument();
      expect((screen.getByLabelText("Workflow Selection") as HTMLSelectElement).value).toBe("");
      expect(screen.getAllByText("no workflow available").length).toBeGreaterThan(0);
    });
  });

  it("honors deep-linked live workflow selection", async () => {
    window.history.replaceState(
      {},
      "",
      "/observability?view=live&session=session-complete-0002&workflow=workflow-followup-0002",
    );
    installFetchMock();

    render(<App />);

    await waitFor(() => {
      expect(screen.getByRole("heading", { level: 2, name: "Live Monitor" })).toBeInTheDocument();
      expect((screen.getByLabelText("Workflow Selection") as HTMLSelectElement).value).toBe("workflow-followup-0002");
      expect(lastEventSource().url).toContain("workflow_id=workflow-followup-0002");
    });
  });

  it("refreshes session detail when a terminal SSE event arrives", async () => {
    window.history.replaceState({}, "", "/observability?view=live&session=session-complete-0002");
    const fetchMock = installFetchMock();

    render(<App />);

    await waitFor(() => {
      expect(lastEventSource().url).toContain("workflow_id=workflow-complete-0002");
    });

    // Record how many times the session detail was fetched.
    const detailCallsBefore = fetchMock.mock.calls.filter(
      (call: [RequestInfo | URL]) => String(call[0]).includes("/api/sessions/session-complete-0002"),
    ).length;

    const source = lastEventSource();
    source.open();
    source.emit(liveEvent(10, "run_complete", { final_state: "completed", human_result: "approved" }));

    // After a terminal event, the app should re-fetch sessions and detail.
    await waitFor(() => {
      const detailCallsAfter = fetchMock.mock.calls.filter(
        (call: [RequestInfo | URL]) => String(call[0]).includes("/api/sessions/session-complete-0002"),
      ).length;
      expect(detailCallsAfter).toBeGreaterThan(detailCallsBefore);
    });
  });
});

interface FetchMockOptions {
  sessions?: Array<Record<string, unknown>>;
  listError?: string;
  planAudit?: Record<string, unknown>;
  planAuditError?: string;
}

function installFetchMock(options: FetchMockOptions = {}) {
  const sessions = options.sessions ?? defaultSessions();
  const details = defaultSessionDetails();
  const planAudit = options.planAudit ?? defaultPlanAuditStats();
  const fetchMock = vi.fn((input: RequestInfo | URL) => {
    const url = String(input);

    if (url.startsWith("/api/sessions?") || url === "/api/sessions") {
      if (options.listError) {
        return Promise.resolve(errorResponse(options.listError));
      }

      const state = new URL(url, "http://localhost").searchParams.get("state");
      const filteredSessions = state
        ? sessions.filter((session) => session.state === state)
        : sessions;

      return Promise.resolve(jsonResponse({ sessions: filteredSessions }));
    }

    if (url.startsWith("/api/sessions/")) {
      const id = url.split("/").pop() || "";
      const detail = details[id];
      if (!detail) {
        return Promise.resolve(errorResponse(`missing session ${id}`, 404));
      }
      return Promise.resolve(jsonResponse(detail));
    }

    if (url.startsWith("/api/stats/plan-audit")) {
      if (options.planAuditError) {
        return Promise.resolve(errorResponse(options.planAuditError));
      }
      return Promise.resolve(jsonResponse(planAudit));
    }

    throw new Error(`unexpected fetch URL ${url}`);
  });

  vi.stubGlobal("fetch", fetchMock);
  return fetchMock;
}

function lastEventSource() {
  if (MockEventSource.instances.length === 0) {
    throw new Error("expected an EventSource instance");
  }
  return MockEventSource.instances[MockEventSource.instances.length - 1];
}

function liveEvent(
  id: number,
  eventType: EventStreamEnvelope["event_type"],
  eventData: EventStreamEnvelope["event_data"],
): EventStreamEnvelope {
  return {
    id,
    workflow_id: "workflow-complete-0002",
    task_id: "task-live-1",
    event_type: eventType,
    event_data: eventData,
    created_at: `2026-03-20T00:0${id}:00Z`,
  };
}

function defaultSessions() {
  return [
    {
      id: "session-running-0001",
      kind: "run_only",
      project: "agent-conductor",
      source_spec_path: "specs/worker-scope.yaml",
      state: "running",
      created_at: "2026-03-20T00:00:00Z",
      updated_at: "2026-03-20T00:05:00Z",
      plan_runs_count: 1,
      pipeline_runs_count: 1,
      latest_workflow_state: "verify",
    },
    {
      id: "session-complete-0002",
      kind: "plan_and_run",
      project: "batch-reconciler",
      source_spec_path: "specs/reconcile-2026.yaml",
      state: "completed",
      created_at: "2026-03-19T23:00:00Z",
      updated_at: "2026-03-20T00:15:00Z",
      plan_runs_count: 2,
      pipeline_runs_count: 2,
      latest_workflow_state: "completed",
    },
    {
      id: "session-plan-only-0003",
      kind: "plan_only",
      project: "plan-only",
      source_spec_path: "specs/plan-only.yaml",
      state: "partial",
      created_at: "2026-03-20T01:00:00Z",
      updated_at: "2026-03-20T01:05:00Z",
      plan_runs_count: 1,
      pipeline_runs_count: 0,
      latest_workflow_state: "",
    },
  ];
}

function defaultSessionDetails(): Record<string, unknown> {
  return {
    "session-running-0001": {
      session: {
        id: "session-running-0001",
        kind: "run_only",
        project: "agent-conductor",
        source_spec_path: "specs/worker-scope.yaml",
        state: "running",
        created_at: "2026-03-20T00:00:00Z",
        updated_at: "2026-03-20T00:05:00Z",
        started_at: "2026-03-20T00:00:00Z",
      },
      plan_runs: [
        {
          id: "plan-running-1",
          spec_file: "worker-scope.yaml",
          spec_fingerprint: "spec-running-fingerprint",
          work_orders_generated: 3,
          post_audit_work_order_count: 3,
          work_order_delta: 1,
          audit_changes: ["deduped acceptance criteria"],
          created_at: "2026-03-20T00:01:00Z",
        },
      ],
      pipeline_runs: [
        {
          id: "pipeline-running-1",
          workflow_id: "workflow-running-0001",
          project: "agent-conductor",
          work_order_type: "scope",
          verify_result: "pending",
          human_result: "pending",
          created_at: "2026-03-20T00:02:00Z",
        },
      ],
      artifacts: [
        {
          id: "artifact-running-1",
          artifact_type: "build_log",
          path: "runs/workflow-running-0001/build.log",
          size_bytes: 512,
          workflow_id: "workflow-running-0001",
          task_id: "task-running-1",
          metadata: { lines: 12 },
          created_at: "2026-03-20T00:03:00Z",
        },
      ],
    },
    "session-complete-0002": {
      session: {
        id: "session-complete-0002",
        kind: "plan_and_run",
        project: "batch-reconciler",
        source_spec_path: "specs/reconcile-2026.yaml",
        state: "completed",
        created_at: "2026-03-19T23:00:00Z",
        updated_at: "2026-03-20T00:15:00Z",
        started_at: "2026-03-19T23:01:00Z",
        completed_at: "2026-03-20T00:14:00Z",
      },
      plan_runs: [
        {
          id: "plan-complete-1",
          spec_file: "reconcile-2026.yaml",
          spec_fingerprint: "spec-complete-fingerprint",
          work_orders_generated: 4,
          post_audit_work_order_count: 4,
          work_order_delta: -1,
          audit_changes: ["removed duplicate work order", "normalized owner metadata"],
          created_at: "2026-03-19T23:02:00Z",
        },
      ],
      pipeline_runs: [
        {
          id: "pipeline-complete-1",
          workflow_id: "workflow-complete-0002",
          project: "batch-reconciler",
          work_order_type: "verify",
          verify_result: "passed",
          human_result: "approved",
          created_at: "2026-03-19T23:10:00Z",
        },
        {
          id: "pipeline-followup-1",
          workflow_id: "workflow-followup-0002",
          project: "batch-reconciler",
          work_order_type: "build",
          verify_result: "pending",
          human_result: "pending",
          created_at: "2026-03-19T23:15:00Z",
        },
      ],
      artifacts: [
        {
          id: "artifact-complete-1",
          artifact_type: "review_bundle",
          path: "runs/workflow-complete-0002/review_bundle.json",
          size_bytes: 2048,
          workflow_id: "workflow-complete-0002",
          task_id: "task-complete-1",
          metadata: { reviewer: "qa" },
          created_at: "2026-03-19T23:11:00Z",
        },
      ],
    },
    "session-plan-only-0003": {
      session: {
        id: "session-plan-only-0003",
        kind: "plan_only",
        project: "plan-only",
        source_spec_path: "specs/plan-only.yaml",
        state: "partial",
        created_at: "2026-03-20T01:00:00Z",
        updated_at: "2026-03-20T01:05:00Z",
        started_at: "2026-03-20T01:00:00Z",
      },
      plan_runs: [
        {
          id: "plan-only-1",
          spec_file: "plan-only.yaml",
          spec_fingerprint: "spec-plan-only",
          work_orders_generated: 1,
          post_audit_work_order_count: 1,
          work_order_delta: 0,
          audit_changes: [],
          created_at: "2026-03-20T01:01:00Z",
        },
      ],
      pipeline_runs: [],
      artifacts: [],
    },
  };
}

function defaultPlanAuditStats() {
  return {
    summary: {
      changed_runs: 2,
      unchanged_runs: 1,
      total_runs: 3,
    },
    recent_runs: [
      {
        id: "audit-run-0001",
        session_id: "session-running-0001",
        project: "agent-conductor",
        spec_file: "ingestion-spec.yaml",
        audit_changed: true,
        work_orders_generated: 5,
        pre_audit_work_order_count: 4,
        post_audit_work_order_count: 5,
        work_order_delta: 1,
        audit_changes: ["expanded verification notes"],
        created_at: "2026-03-20T00:04:00Z",
      },
      {
        id: "audit-run-0002",
        session_id: "session-complete-0002",
        project: "batch-reconciler",
        spec_file: "reconcile-2026.yaml",
        audit_changed: false,
        work_orders_generated: 4,
        pre_audit_work_order_count: 4,
        post_audit_work_order_count: 4,
        work_order_delta: 0,
        audit_changes: [],
        created_at: "2026-03-20T00:05:00Z",
      },
      {
        id: "audit-run-0003",
        session_id: "session-complete-0002",
        project: "batch-reconciler",
        spec_file: "batch-cleanup.yaml",
        audit_changed: true,
        work_orders_generated: 2,
        pre_audit_work_order_count: 3,
        post_audit_work_order_count: 2,
        work_order_delta: -1,
        audit_changes: ["removed stale handoff step"],
        created_at: "2026-03-20T00:06:00Z",
      },
    ],
  };
}

function emptyPlanAuditStats() {
  return {
    summary: {
      changed_runs: 0,
      unchanged_runs: 0,
      total_runs: 0,
    },
    recent_runs: [],
  };
}

function jsonResponse(payload: unknown): Response {
  return new Response(JSON.stringify(payload), {
    status: 200,
    headers: {
      "Content-Type": "application/json",
    },
  });
}

function errorResponse(message: string, status = 500): Response {
  return new Response(JSON.stringify({ error: message }), {
    status,
    headers: {
      "Content-Type": "application/json",
    },
  });
}
