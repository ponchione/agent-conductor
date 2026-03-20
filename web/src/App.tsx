import { startTransition, useEffect, useState } from "react";
import { getPlanAuditStats, getSession, listSessions, openEventStream } from "./api/client";
import type {
  Artifact,
  EventPayload,
  EventStreamEnvelope,
  PipelineRun,
  PlanAuditRun,
  PlanAuditStatsResponse,
  PlanRun,
  SessionDetailResponse,
  SessionState,
  SessionSummary,
  ShellView,
} from "./types/api";

const SESSION_LIMIT = 20;
const LIVE_EVENT_LIMIT = 40;

const SESSION_FILTERS: Array<{ value: SessionFilter; label: string }> = [
  { value: "all", label: "All" },
  { value: "running", label: "Running" },
  { value: "awaiting_review", label: "Awaiting Review" },
  { value: "completed", label: "Completed" },
  { value: "failed", label: "Failed" },
  { value: "partial", label: "Partial" },
];

const AUDIT_FILTERS: Array<{ value: AuditFilter; label: string }> = [
  { value: "all", label: "All Runs" },
  { value: "changed", label: "Changed Only" },
  { value: "unchanged", label: "Unchanged Only" },
];

type SessionFilter = "all" | SessionState;
type AuditFilter = "all" | "changed" | "unchanged";

interface QueryState {
  view: ShellView;
  sessionId: string | null;
  sessionFilter: SessionFilter;
  auditRunId: string | null;
  auditFilter: AuditFilter;
  workflowId: string | null;
}

interface LiveSummary {
  phase: string;
  terminalState: string;
  latestEvent: string;
  lastUpdated: string;
}

function readQueryState(): QueryState {
  const params = new URLSearchParams(window.location.search);
  return {
    view: normalizeView(params.get("view")),
    sessionId: params.get("session"),
    sessionFilter: normalizeSessionFilter(params.get("state")),
    auditRunId: params.get("audit_run"),
    auditFilter: normalizeAuditFilter(params.get("audit_filter")),
    workflowId: params.get("workflow"),
  };
}

function normalizeView(value: string | null): ShellView {
  switch (value) {
    case "audit":
    case "live":
      return value;
    default:
      return "sessions";
  }
}

function normalizeSessionFilter(value: string | null): SessionFilter {
  switch (value) {
    case "running":
    case "awaiting_review":
    case "completed":
    case "failed":
    case "partial":
      return value;
    default:
      return "all";
  }
}

function normalizeAuditFilter(value: string | null): AuditFilter {
  switch (value) {
    case "changed":
    case "unchanged":
      return value;
    default:
      return "all";
  }
}

function syncQuery(
  view: ShellView,
  sessionId: string | null,
  sessionFilter: SessionFilter,
  auditRunId: string | null,
  auditFilter: AuditFilter,
  workflowId: string | null,
) {
  const params = new URLSearchParams(window.location.search);
  params.set("view", view);

  if (sessionId) {
    params.set("session", sessionId);
  } else {
    params.delete("session");
  }

  if (sessionFilter === "all") {
    params.delete("state");
  } else {
    params.set("state", sessionFilter);
  }

  if (auditRunId) {
    params.set("audit_run", auditRunId);
  } else {
    params.delete("audit_run");
  }

  if (auditFilter === "all") {
    params.delete("audit_filter");
  } else {
    params.set("audit_filter", auditFilter);
  }

  if (workflowId) {
    params.set("workflow", workflowId);
  } else {
    params.delete("workflow");
  }

  const query = params.toString();
  const nextURL = query ? `?${query}` : window.location.pathname;
  window.history.replaceState({}, "", nextURL);
}

export default function App() {
  const queryState = readQueryState();
  const [view, setView] = useState<ShellView>(queryState.view);
  const [sessionFilter, setSessionFilter] = useState<SessionFilter>(queryState.sessionFilter);
  const [selectedSessionId, setSelectedSessionId] = useState<string | null>(queryState.sessionId);
  const [auditFilter, setAuditFilter] = useState<AuditFilter>(queryState.auditFilter);
  const [selectedAuditRunId, setSelectedAuditRunId] = useState<string | null>(queryState.auditRunId);
  const [selectedWorkflowId, setSelectedWorkflowId] = useState<string | null>(queryState.workflowId);
  const [sessions, setSessions] = useState<SessionSummary[]>([]);
  const [sessionsError, setSessionsError] = useState<string>("");
  const [selectedDetail, setSelectedDetail] = useState<SessionDetailResponse | null>(null);
  const [detailError, setDetailError] = useState<string>("");
  const [planAudit, setPlanAudit] = useState<PlanAuditStatsResponse | null>(null);
  const [planAuditError, setPlanAuditError] = useState<string>("");
  const [liveEvents, setLiveEvents] = useState<EventStreamEnvelope[]>([]);
  const [streamStatus, setStreamStatus] = useState<string>("idle");
  const [refreshKey, setRefreshKey] = useState(0);

  const filteredAuditRuns = filterAuditRuns(planAudit?.recent_runs, auditFilter);
  const selectedAuditRun =
    filteredAuditRuns.find((run) => run.id === selectedAuditRunId) ?? null;
  const workflowRuns = uniqueWorkflowRuns(selectedDetail?.pipeline_runs);
  const selectedWorkflowRun =
    workflowRuns.find((run) => run.workflow_id === selectedWorkflowId) ?? null;
  const liveSummary = summarizeLiveWorkflow(selectedWorkflowRun, liveEvents);

  useEffect(() => {
    syncQuery(view, selectedSessionId, sessionFilter, selectedAuditRunId, auditFilter, selectedWorkflowId);
  }, [auditFilter, selectedAuditRunId, selectedSessionId, selectedWorkflowId, sessionFilter, view]);

  useEffect(() => {
    let cancelled = false;
    listSessions({
      limit: SESSION_LIMIT,
      state: sessionFilter === "all" ? undefined : sessionFilter,
    })
      .then((payload) => {
        if (cancelled) {
          return;
        }

        startTransition(() => {
          setSessions(payload.sessions);
          setSessionsError("");

          if (payload.sessions.length === 0) {
            setSelectedSessionId(null);
            return;
          }

          setSelectedSessionId((current) => {
            if (current && payload.sessions.some((session) => session.id === current)) {
              return current;
            }
            return payload.sessions[0].id;
          });
        });
      })
      .catch((error: Error) => {
        if (!cancelled) {
          setSessions([]);
          setSelectedSessionId(null);
          setSessionsError(error.message);
        }
      });

    return () => {
      cancelled = true;
    };
  }, [sessionFilter, refreshKey]);

  useEffect(() => {
    if (!selectedSessionId) {
      setSelectedDetail(null);
      setDetailError("");
      setSelectedWorkflowId(null);
      return;
    }

    let cancelled = false;
    setSelectedDetail(null);
    setDetailError("");
    getSession(selectedSessionId)
      .then((payload) => {
        if (!cancelled) {
          startTransition(() => {
            setSelectedDetail(payload);
            setDetailError("");
          });
        }
      })
      .catch((error: Error) => {
        if (!cancelled) {
          setSelectedDetail(null);
          setDetailError(error.message);
        }
      });

    return () => {
      cancelled = true;
    };
  }, [selectedSessionId, refreshKey]);

  useEffect(() => {
    let cancelled = false;
    getPlanAuditStats()
      .then((payload) => {
        if (!cancelled) {
          startTransition(() => {
            setPlanAudit(payload);
            setPlanAuditError("");
          });
        }
      })
      .catch((error: Error) => {
        if (!cancelled) {
          setPlanAudit(null);
          setPlanAuditError(error.message);
        }
      });

    return () => {
      cancelled = true;
    };
  }, []);

  useEffect(() => {
    if (!planAudit) {
      return;
    }

    const nextRuns = filterAuditRuns(planAudit.recent_runs, auditFilter);
    startTransition(() => {
      if (nextRuns.length === 0) {
        setSelectedAuditRunId(null);
        return;
      }

      setSelectedAuditRunId((current) => {
        if (current && nextRuns.some((run) => run.id === current)) {
          return current;
        }
        return nextRuns[0].id;
      });
    });
  }, [auditFilter, planAudit]);

  useEffect(() => {
    if (!selectedDetail) {
      return;
    }

    const nextWorkflowRuns = uniqueWorkflowRuns(selectedDetail.pipeline_runs);
    startTransition(() => {
      if (nextWorkflowRuns.length === 0) {
        setSelectedWorkflowId(null);
        return;
      }

      setSelectedWorkflowId((current) => {
        if (current && nextWorkflowRuns.some((run) => run.workflow_id === current)) {
          return current;
        }
        return nextWorkflowRuns[0].workflow_id;
      });
    });
  }, [selectedDetail]);

  useEffect(() => {
    if (view !== "live") {
      setStreamStatus("idle");
      return;
    }

    if (!selectedDetail) {
      setStreamStatus("loading session detail");
      return;
    }

    if (workflowRuns.length === 0) {
      setLiveEvents([]);
      setStreamStatus("no workflow available");
      return;
    }

    if (!selectedWorkflowRun) {
      setLiveEvents([]);
      setStreamStatus("waiting for a workflow selection");
      return;
    }

    setStreamStatus("connecting");
    setLiveEvents([]);
    const stream = openEventStream({
      workflowId: selectedWorkflowRun.workflow_id,
      onOpen: () => {
        setStreamStatus("streaming");
      },
      onEvent: (event) => {
        startTransition(() => {
          setLiveEvents((current) => upsertLiveEvent(current, event));
          setStreamStatus("streaming");
          if (event.event_type === "run_complete" || event.event_type === "run_awaiting_review") {
            setRefreshKey((k) => k + 1);
          }
        });
      },
      onError: () => {
        setStreamStatus("reconnecting");
      },
    });

    return () => {
      stream.close();
    };
  }, [selectedDetail, selectedWorkflowRun, view, workflowRuns.length]);

  return (
    <div className="app-shell" data-testid="app-shell">
      <aside className="rail">
        <div>
          <p className="eyebrow">Agent Conductor</p>
          <h1>Observability Console</h1>
          <p className="lede">
            Session, plan-audit, and live workflow monitoring now run on React over the typed
            backend contracts.
          </p>
        </div>

        <nav className="nav">
          <button className={navClass(view, "sessions")} onClick={() => setView("sessions")} type="button">
            Session Dashboard
          </button>
          <button className={navClass(view, "audit")} onClick={() => setView("audit")} type="button">
            Plan Audit Dashboard
          </button>
          <button className={navClass(view, "live")} onClick={() => setView("live")} type="button">
            Live Monitor
          </button>
        </nav>

        <section className="side-section">
          <div className="section-head">
            <h2>Sessions</h2>
            <span>{sessions.length}</span>
          </div>

          <label className="filter">
            <span>State Filter</span>
            <select
              aria-label="State Filter"
              value={sessionFilter}
              onChange={(event) => {
                startTransition(() => {
                  setSessionFilter(normalizeSessionFilter(event.target.value));
                });
              }}
            >
              {SESSION_FILTERS.map((filter) => (
                <option key={filter.value} value={filter.value}>
                  {filter.label}
                </option>
              ))}
            </select>
          </label>

          {sessionsError ? <p className="error-text">Unable to load sessions: {sessionsError}</p> : null}
          {!sessionsError && sessions.length === 0 ? (
            <p className="muted-copy">No sessions match the current filter.</p>
          ) : null}

          <div className="session-list">
            {sessions.map((session) => (
              <button
                key={session.id}
                className={session.id === selectedSessionId ? "session-card is-active" : "session-card"}
                onClick={() => setSelectedSessionId(session.id)}
                type="button"
              >
                <div className="session-card-head">
                  <span>{session.project}</span>
                  <strong>{session.state}</strong>
                </div>
                <small>{shortID(session.id)}</small>
                <div className="session-card-meta">
                  <span>{session.plan_runs_count} plan</span>
                  <span>{session.pipeline_runs_count} run</span>
                  <span>{session.latest_workflow_state || "no workflow"}</span>
                </div>
              </button>
            ))}
          </div>
        </section>
      </aside>

      <main className="canvas">
        <header className="topbar">
          <div>
            <p className="eyebrow">Current View</p>
            <h2>{viewTitle(view)}</h2>
          </div>
          <div className="status-pill">{statusLabel(view, sessionFilter, auditFilter, streamStatus)}</div>
        </header>

        <section className="hero-grid">
          <article className="hero-card">
            <p className="hero-label">Selected Session</p>
            <strong>{selectedDetail?.session.project || "No session selected"}</strong>
            <span>
              {selectedDetail
                ? shortID(selectedDetail.session.id)
                : "Choose a session to inspect plan runs, pipeline runs, and artifacts."}
            </span>
          </article>
          <article className="hero-card">
            <p className="hero-label">Plan Audit Runs</p>
            <strong>{planAudit?.summary.total_runs ?? 0}</strong>
            <span>
              {planAudit
                ? `${planAudit.summary.changed_runs} changed / ${planAudit.summary.unchanged_runs} unchanged`
                : "Awaiting summary"}
            </span>
          </article>
          <article className="hero-card">
            <p className="hero-label">Live Event Buffer</p>
            <strong>{liveEvents.length}</strong>
            <span>{view === "live" ? "Timeline is connected to SSE." : "Switch to Live Monitor to connect."}</span>
          </article>
        </section>

        {view === "sessions" ? (
          <section className="dashboard-grid">
            <article className="panel panel-span-2">
              <div className="panel-head">
                <h3>Session Overview</h3>
              </div>
              {detailError ? <p className="error-text">Unable to load session detail: {detailError}</p> : null}
              {!selectedDetail && !detailError ? (
                <p className="muted-copy">Select a session to render plan runs, pipeline runs, and artifacts.</p>
              ) : null}
              {selectedDetail ? (
                <>
                  <dl className="meta-grid">
                    <div>
                      <dt>Session ID</dt>
                      <dd className="path-value">{selectedDetail.session.id}</dd>
                    </div>
                    <div>
                      <dt>State</dt>
                      <dd>{selectedDetail.session.state}</dd>
                    </div>
                    <div>
                      <dt>Kind</dt>
                      <dd>{selectedDetail.session.kind}</dd>
                    </div>
                    <div>
                      <dt>Created</dt>
                      <dd>{formatTimestamp(selectedDetail.session.created_at)}</dd>
                    </div>
                    <div>
                      <dt>Started</dt>
                      <dd>{formatTimestamp(selectedDetail.session.started_at)}</dd>
                    </div>
                    <div>
                      <dt>Completed</dt>
                      <dd>{formatTimestamp(selectedDetail.session.completed_at)}</dd>
                    </div>
                  </dl>
                  <div className="detail-note-grid">
                    <div className="note-card">
                      <span>Source Spec</span>
                      <strong className="path-value">{selectedDetail.session.source_spec_path || "n/a"}</strong>
                    </div>
                    <div className="note-card">
                      <span>Error</span>
                      <strong className="path-value">{selectedDetail.session.error_message || "none"}</strong>
                    </div>
                  </div>
                </>
              ) : null}
            </article>

            <article className="panel">
              <div className="panel-head">
                <h3>Plan Runs</h3>
                <span>{selectedDetail?.plan_runs.length ?? 0}</span>
              </div>
              {renderPlanRuns(selectedDetail?.plan_runs)}
            </article>

            <article className="panel">
              <div className="panel-head">
                <h3>Pipeline Runs</h3>
                <span>{selectedDetail?.pipeline_runs.length ?? 0}</span>
              </div>
              {renderPipelineRuns(selectedDetail?.pipeline_runs)}
            </article>

            <article className="panel panel-span-2">
              <div className="panel-head">
                <h3>Artifacts</h3>
                <span>{selectedDetail?.artifacts.length ?? 0}</span>
              </div>
              {renderArtifacts(selectedDetail?.artifacts)}
            </article>
          </section>
        ) : null}

        {view === "audit" ? (
          <section className="dashboard-grid">
            <article className="panel panel-span-2">
              <div className="panel-head">
                <h3>Audit Summary</h3>
                <label className="filter filter-inline">
                  <span>Audit Run Filter</span>
                  <select
                    aria-label="Audit Run Filter"
                    value={auditFilter}
                    onChange={(event) => {
                      startTransition(() => {
                        setAuditFilter(normalizeAuditFilter(event.target.value));
                      });
                    }}
                  >
                    {AUDIT_FILTERS.map((filter) => (
                      <option key={filter.value} value={filter.value}>
                        {filter.label}
                      </option>
                    ))}
                  </select>
                </label>
              </div>

              {planAuditError ? <p className="error-text">Unable to load plan audit stats: {planAuditError}</p> : null}

              {planAudit ? (
                <>
                  <dl className="meta-grid">
                    <div>
                      <dt>Total Runs</dt>
                      <dd>{planAudit.summary.total_runs}</dd>
                    </div>
                    <div>
                      <dt>Changed Runs</dt>
                      <dd>{planAudit.summary.changed_runs}</dd>
                    </div>
                    <div>
                      <dt>Unchanged Runs</dt>
                      <dd>{planAudit.summary.unchanged_runs}</dd>
                    </div>
                    <div>
                      <dt>Visible Runs</dt>
                      <dd>{filteredAuditRuns.length}</dd>
                    </div>
                  </dl>
                  <div className="detail-note-grid">
                    <div className="note-card">
                      <span>Current Filter</span>
                      <strong>{auditFilterLabel(auditFilter)}</strong>
                    </div>
                    <div className="note-card">
                      <span>Selected Run</span>
                      <strong className="path-value">
                        {selectedAuditRun ? selectedAuditRun.spec_file : "No run selected"}
                      </strong>
                    </div>
                  </div>
                </>
              ) : null}

              {!planAudit && !planAuditError ? (
                <p className="muted-copy">Audit data has not loaded yet.</p>
              ) : null}
            </article>

            <article className="panel">
              <div className="panel-head">
                <h3>Recent Audit Runs</h3>
                <span>{filteredAuditRuns.length}</span>
              </div>

              {!planAuditError ? (
                renderAuditRunList(filteredAuditRuns, selectedAuditRunId, setSelectedAuditRunId)
              ) : null}
            </article>

            <article className="panel">
              <div className="panel-head">
                <h3>Audit Run Detail</h3>
              </div>
              {renderSelectedAuditRun(selectedAuditRun, planAuditError)}
            </article>
          </section>
        ) : null}

        {view === "live" ? (
          <section className="dashboard-grid">
            <article className="panel panel-span-2">
              <div className="panel-head">
                <h3>Workflow Monitor</h3>
                <label className="filter filter-inline">
                  <span>Workflow Selection</span>
                  <select
                    aria-label="Workflow Selection"
                    disabled={!workflowRuns.length}
                    value={selectedWorkflowId ?? ""}
                    onChange={(event) => {
                      startTransition(() => {
                        setSelectedWorkflowId(event.target.value || null);
                      });
                    }}
                  >
                    {!workflowRuns.length ? <option value="">No workflows</option> : null}
                    {workflowRuns.map((run) => (
                      <option key={run.workflow_id} value={run.workflow_id}>
                        {workflowOptionLabel(run)}
                      </option>
                    ))}
                  </select>
                </label>
              </div>

              {detailError ? <p className="error-text">Unable to load session detail: {detailError}</p> : null}
              {!selectedDetail && !detailError ? (
                <p className="muted-copy">Loading the selected session before attaching the live event stream.</p>
              ) : null}
              {selectedDetail && workflowRuns.length === 0 ? (
                <p className="muted-copy">No workflows available for this session.</p>
              ) : null}
              {selectedWorkflowRun ? (
                <div className="detail-note-grid">
                  <div className="note-card">
                    <span>Workflow</span>
                    <strong className="path-value">{selectedWorkflowRun.workflow_id}</strong>
                  </div>
                  <div className="note-card">
                    <span>Work Order Type</span>
                    <strong>{selectedWorkflowRun.work_order_type || "execution"}</strong>
                  </div>
                </div>
              ) : null}
            </article>

            <article className="panel">
              <div className="panel-head">
                <h3>Current State</h3>
              </div>
              {selectedWorkflowRun ? (
                <>
                  <dl className="meta-grid">
                    <div>
                      <dt>Phase</dt>
                      <dd>{liveSummary.phase}</dd>
                    </div>
                    <div>
                      <dt>Terminal State</dt>
                      <dd>{liveSummary.terminalState}</dd>
                    </div>
                    <div>
                      <dt>Latest Event</dt>
                      <dd>{liveSummary.latestEvent}</dd>
                    </div>
                    <div>
                      <dt>Last Updated</dt>
                      <dd>{liveSummary.lastUpdated}</dd>
                    </div>
                  </dl>
                  <div className="detail-note-grid">
                    <div className="note-card">
                      <span>Verify Result</span>
                      <strong>{selectedWorkflowRun.verify_result || "pending"}</strong>
                    </div>
                    <div className="note-card">
                      <span>Human Result</span>
                      <strong>{selectedWorkflowRun.human_result || "pending"}</strong>
                    </div>
                  </div>
                </>
              ) : (
                <p className="muted-copy">Select a workflow to inspect its current phase and terminal state.</p>
              )}
            </article>

            <article className="panel">
              <div className="panel-head">
                <h3>Connection</h3>
              </div>
              <div className="detail-note-grid">
                <div className="note-card">
                  <span>Status</span>
                  <strong>{streamStatus}</strong>
                </div>
                <div className="note-card">
                  <span>Buffer</span>
                  <strong>{liveEvents.length} buffered events</strong>
                </div>
              </div>
              <p className="muted-copy">
                The monitor reuses the DB-backed SSE stream. Replay is deduped by event ID and the
                client retains the most recent {LIVE_EVENT_LIMIT} events.
              </p>
            </article>

            <article className="panel panel-span-2">
              <div className="panel-head">
                <h3>Event Timeline</h3>
                <span>{selectedWorkflowId ? shortID(selectedWorkflowId) : "no workflow"}</span>
              </div>
              {renderLiveTimeline(liveEvents, streamStatus, selectedWorkflowRun)}
            </article>
          </section>
        ) : null}
      </main>
    </div>
  );
}

function navClass(current: ShellView, target: ShellView) {
  return current === target ? "nav-button is-active" : "nav-button";
}

function viewTitle(view: ShellView) {
  switch (view) {
    case "audit":
      return "Plan Audit Dashboard";
    case "live":
      return "Live Monitor";
    default:
      return "Session Dashboard";
  }
}

function statusLabel(
  view: ShellView,
  sessionFilter: SessionFilter,
  auditFilter: AuditFilter,
  streamStatus: string,
) {
  if (view === "live") {
    return streamStatus;
  }
  if (view === "audit") {
    return auditFilterLabel(auditFilter);
  }
  return sessionFilterLabel(sessionFilter);
}

function sessionFilterLabel(value: SessionFilter) {
  return SESSION_FILTERS.find((filter) => filter.value === value)?.label || "All";
}

function auditFilterLabel(value: AuditFilter) {
  return AUDIT_FILTERS.find((filter) => filter.value === value)?.label || "All Runs";
}

function shortID(value: string) {
  return value.length > 8 ? value.slice(0, 8) : value;
}

function formatTimestamp(value?: string) {
  if (!value) {
    return "n/a";
  }

  const parsed = new Date(value);
  if (Number.isNaN(parsed.getTime())) {
    return value;
  }
  return parsed.toLocaleString();
}

function renderPlanRuns(planRuns?: PlanRun[]) {
  if (!planRuns?.length) {
    return <p className="muted-copy">No plan runs linked to this session.</p>;
  }

  return (
    <ul className="stack-list">
      {planRuns.map((run) => (
        <li key={run.id} className="data-card">
          <div className="data-card-head">
            <strong>{run.spec_file}</strong>
            <span>{formatCount(run.post_audit_work_order_count ?? run.work_orders_generated)} orders</span>
          </div>
          <p className="path-value">{run.spec_fingerprint || "No fingerprint recorded"}</p>
          <div className="card-meta">
            <span>Delta {signedCount(run.work_order_delta)}</span>
            <span>{formatTimestamp(run.created_at)}</span>
          </div>
          {run.audit_changes?.length ? (
            <ul className="tag-list">
              {run.audit_changes.map((change) => (
                <li key={change}>{change}</li>
              ))}
            </ul>
          ) : null}
        </li>
      ))}
    </ul>
  );
}

function renderPipelineRuns(pipelineRuns?: PipelineRun[]) {
  if (!pipelineRuns?.length) {
    return <p className="muted-copy">No pipeline runs linked to this session.</p>;
  }

  return (
    <ul className="stack-list">
      {pipelineRuns.map((run) => (
        <li key={run.id} className="data-card">
          <div className="data-card-head">
            <strong>{run.work_order_type || "execution"}</strong>
            <span>{shortID(run.workflow_id)}</span>
          </div>
          <p className="path-value">{run.workflow_id}</p>
          <div className="card-meta">
            <span>Verify {run.verify_result || "pending"}</span>
            <span>Human {run.human_result || "pending"}</span>
          </div>
        </li>
      ))}
    </ul>
  );
}

function renderArtifacts(artifacts?: Artifact[]) {
  if (!artifacts?.length) {
    return <p className="muted-copy">No artifacts linked to this session.</p>;
  }

  return (
    <ul className="stack-list">
      {artifacts.map((artifact) => (
        <li key={artifact.id} className="data-card">
          <div className="data-card-head">
            <strong>{artifact.artifact_type}</strong>
            <span>{artifact.size_bytes ? `${artifact.size_bytes} bytes` : "size n/a"}</span>
          </div>
          <p className="path-value">{artifact.path}</p>
          <div className="card-meta">
            <span>{artifact.workflow_id ? `Workflow ${shortID(artifact.workflow_id)}` : "Session artifact"}</span>
            <span>{artifact.task_id ? `Task ${shortID(artifact.task_id)}` : "No task"}</span>
          </div>
          {artifact.metadata ? <pre className="metadata-preview">{formatMetadata(artifact.metadata)}</pre> : null}
        </li>
      ))}
    </ul>
  );
}

function renderAuditRunList(
  auditRuns: PlanAuditRun[],
  selectedAuditRunId: string | null,
  onSelect: (id: string) => void,
) {
  if (!auditRuns.length) {
    return <p className="muted-copy">No audit runs match the current filter.</p>;
  }

  return (
    <ul className="stack-list">
      {auditRuns.map((run) => (
        <li key={run.id}>
          <button
            className={run.id === selectedAuditRunId ? "audit-run-button is-active" : "audit-run-button"}
            onClick={() => onSelect(run.id)}
            type="button"
          >
            <div className="data-card-head">
              <strong>{run.spec_file}</strong>
              <span>{auditRunState(run)}</span>
            </div>
            <p className="path-value">{run.project || "No project recorded"}</p>
            <div className="card-meta">
              <span>Delta {signedCount(run.work_order_delta)}</span>
              <span>{formatTimestamp(run.created_at)}</span>
            </div>
          </button>
        </li>
      ))}
    </ul>
  );
}

function renderSelectedAuditRun(run: PlanAuditRun | null, error: string) {
  if (error) {
    return <p className="muted-copy">Audit detail is unavailable until the stats endpoint recovers.</p>;
  }

  if (!run) {
    return <p className="muted-copy">Select an audit run to inspect its counts, delta, and audit changes.</p>;
  }

  return (
    <>
      <dl className="meta-grid">
        <div>
          <dt>Run ID</dt>
          <dd className="path-value">{run.id}</dd>
        </div>
        <div>
          <dt>Classification</dt>
          <dd>{auditRunState(run)}</dd>
        </div>
        <div>
          <dt>Spec File</dt>
          <dd className="path-value">{run.spec_file}</dd>
        </div>
        <div>
          <dt>Created</dt>
          <dd>{formatTimestamp(run.created_at)}</dd>
        </div>
        <div>
          <dt>Pre-Audit Count</dt>
          <dd>{formatCount(run.pre_audit_work_order_count)}</dd>
        </div>
        <div>
          <dt>Post-Audit Count</dt>
          <dd>{formatCount(run.post_audit_work_order_count ?? run.work_orders_generated)}</dd>
        </div>
        <div>
          <dt>Delta</dt>
          <dd>{signedCount(run.work_order_delta)}</dd>
        </div>
        <div>
          <dt>Generated</dt>
          <dd>{formatCount(run.work_orders_generated)}</dd>
        </div>
      </dl>
      <div className="detail-note-grid">
        <div className="note-card">
          <span>Project</span>
          <strong className="path-value">{run.project || "n/a"}</strong>
        </div>
        <div className="note-card">
          <span>Session Link</span>
          <strong className="path-value">{run.session_id || "n/a"}</strong>
        </div>
      </div>
      {run.audit_changes?.length ? (
        <ul className="tag-list audit-tag-list">
          {run.audit_changes.map((change) => (
            <li key={change}>{change}</li>
          ))}
        </ul>
      ) : (
        <p className="muted-copy">No audit changes were recorded for this run.</p>
      )}
    </>
  );
}

function renderLiveTimeline(
  liveEvents: EventStreamEnvelope[],
  streamStatus: string,
  selectedWorkflowRun: PipelineRun | null,
) {
  if (!selectedWorkflowRun) {
    return <p className="muted-copy">Select a workflow to attach the event timeline.</p>;
  }

  if (!liveEvents.length) {
    return (
      <p className="muted-copy">
        {streamStatus === "streaming"
          ? "Connected and waiting for events."
          : "No live events received yet."}
      </p>
    );
  }

  return (
    <ol className="timeline-list">
      {liveEvents.map((event) => (
        <li key={event.id} className="timeline-item">
          <div className="timeline-marker">{event.id}</div>
          <div className="timeline-content">
            <div className="timeline-head">
              <strong>{eventLabel(event)}</strong>
              <span>{formatTimestamp(event.created_at)}</span>
            </div>
            <p className="timeline-copy">
              Workflow {shortID(event.workflow_id || selectedWorkflowRun.workflow_id)}
              {event.task_id ? ` / Task ${shortID(event.task_id)}` : ""}
            </p>
            <ul className="timeline-detail-list">
              {eventDetailLines(event).map((line) => (
                <li key={`${event.id}-${line}`}>{line}</li>
              ))}
            </ul>
          </div>
        </li>
      ))}
    </ol>
  );
}

function filterAuditRuns(runs: PlanAuditRun[] | undefined, filter: AuditFilter) {
  if (!runs) {
    return [];
  }

  if (filter === "changed") {
    return runs.filter((run) => run.audit_changed);
  }

  if (filter === "unchanged") {
    return runs.filter((run) => !run.audit_changed);
  }

  return runs;
}

function uniqueWorkflowRuns(runs?: PipelineRun[]) {
  if (!runs?.length) {
    return [];
  }

  const seen = new Set<string>();
  const result: PipelineRun[] = [];
  for (const run of runs) {
    if (seen.has(run.workflow_id)) {
      continue;
    }
    seen.add(run.workflow_id);
    result.push(run);
  }
  return result;
}

function workflowOptionLabel(run: PipelineRun) {
  return `${run.work_order_type || "execution"} / ${shortID(run.workflow_id)}`;
}

function summarizeLiveWorkflow(
  run: PipelineRun | null,
  events: EventStreamEnvelope[],
): LiveSummary {
  if (!run) {
    return {
      phase: "n/a",
      terminalState: "n/a",
      latestEvent: "n/a",
      lastUpdated: "n/a",
    };
  }

  const latestEvent = events.length ? events[events.length - 1] : null;
  const latestPhaseEvent = [...events].reverse().find((event) => extractEventPhase(event));
  const phase = latestPhaseEvent
    ? extractEventPhase(latestPhaseEvent) || run.work_order_type || "execution"
    : run.work_order_type || "execution";

  return {
    phase,
    terminalState: latestEvent ? terminalStateLabel(latestEvent) : run.human_result || run.verify_result || "active",
    latestEvent: latestEvent ? eventLabel(latestEvent) : "No events yet",
    lastUpdated: latestEvent ? formatTimestamp(latestEvent.created_at) : formatTimestamp(run.created_at),
  };
}

function upsertLiveEvent(current: EventStreamEnvelope[], next: EventStreamEnvelope) {
  const filtered = current.filter((event) => event.id !== next.id);
  filtered.push(next);
  filtered.sort((left, right) => left.id - right.id);
  if (filtered.length > LIVE_EVENT_LIMIT) {
    return filtered.slice(filtered.length - LIVE_EVENT_LIMIT);
  }
  return filtered;
}

function eventLabel(event: EventStreamEnvelope) {
  switch (event.event_type) {
    case "phase_start":
      return "Phase Start";
    case "phase_complete":
      return "Phase Complete";
    case "phase_error":
      return "Phase Error";
    case "build_stdout":
      return "Build Stdout";
    case "scope_step":
      return "Scope Step";
    case "verify_precheck":
      return "Verify Precheck";
    case "verify_result":
      return "Verify Result";
    case "run_complete":
      return "Run Complete";
    case "run_awaiting_review":
      return "Awaiting Review";
    default:
      return event.event_type;
  }
}

function eventDetailLines(event: EventStreamEnvelope) {
  const record = payloadRecord(event.event_data);
  if (!record) {
    if (typeof event.event_data === "string" && event.event_data) {
      return [event.event_data];
    }
    return ["No event payload recorded."];
  }

  const lines: string[] = [];
  const phase = fieldText(record, ["phase"]);
  const step = fieldText(record, ["step"]);
  const result = fieldText(record, ["result", "status", "decision"]);
  const output = fieldText(record, ["output", "chunk", "line", "message"]);
  const check = fieldText(record, ["check", "name"]);
  const summary = fieldText(record, ["summary", "reason"]);

  if (phase) {
    lines.push(`Phase ${phase}`);
  }
  if (step) {
    lines.push(`Step ${step}`);
  }
  if (check && event.event_type === "verify_precheck") {
    lines.push(`Check ${check}`);
  }
  if (result) {
    lines.push(`Result ${result}`);
  }
  if (output && event.event_type === "build_stdout") {
    lines.push(`Output ${output}`);
  } else if (output) {
    lines.push(output);
  }
  if (summary) {
    lines.push(summary);
  }

  if (lines.length > 0) {
    return lines;
  }

  return Object.entries(record)
    .slice(0, 4)
    .map(([key, value]) => `${key}: ${formatPayloadValue(value)}`);
}

function terminalStateLabel(event: EventStreamEnvelope) {
  if (event.event_type === "run_complete") {
    return fieldText(payloadRecord(event.event_data), ["result", "status", "decision"]) || "complete";
  }
  if (event.event_type === "run_awaiting_review") {
    return "awaiting review";
  }
  if (event.event_type === "phase_error") {
    return "error";
  }
  return "active";
}

function extractEventPhase(event: EventStreamEnvelope) {
  return fieldText(payloadRecord(event.event_data), ["phase"]);
}

function payloadRecord(payload: EventPayload) {
  return isRecord(payload) ? payload : null;
}

function fieldText(record: Record<string, unknown> | null, keys: string[]) {
  if (!record) {
    return "";
  }

  for (const key of keys) {
    const value = record[key];
    if (typeof value === "string" && value) {
      return value;
    }
  }

  return "";
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

function auditRunState(run: PlanAuditRun) {
  return run.audit_changed ? "changed" : "unchanged";
}

function formatMetadata(value: unknown) {
  if (typeof value === "string") {
    return value;
  }
  return JSON.stringify(value, null, 2);
}

function formatPayloadValue(value: unknown) {
  if (typeof value === "string") {
    return value;
  }
  return JSON.stringify(value);
}

function formatCount(value?: number) {
  return typeof value === "number" ? value : 0;
}

function signedCount(value?: number) {
  if (typeof value !== "number") {
    return "0";
  }
  return value > 0 ? `+${value}` : String(value);
}
