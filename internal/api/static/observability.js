const POLL_MS = 5000;
const SESSION_LIMIT = 25;
const AUDIT_LIMIT = 6;

const state = {
  sessions: [],
  selectedSessionId: null,
  selectedSessionDetail: null,
};

const elements = {
  stateFilter: document.querySelector("#state-filter"),
  sessionListStatus: document.querySelector("#session-list-status"),
  sessionList: document.querySelector("#session-list"),
  detailStatus: document.querySelector("#detail-status"),
  sessionOverview: document.querySelector("#session-overview"),
  sessionSummary: document.querySelector("#session-summary"),
  planRuns: document.querySelector("#plan-runs"),
  pipelineRuns: document.querySelector("#pipeline-runs"),
  artifacts: document.querySelector("#artifacts"),
  auditSummary: document.querySelector("#audit-summary"),
  auditRuns: document.querySelector("#audit-runs"),
};

document.addEventListener("DOMContentLoaded", () => {
  const url = new URL(window.location.href);
  state.selectedSessionId = url.searchParams.get("session");

  elements.stateFilter.addEventListener("change", () => {
    loadSessions({ preserveSelection: false });
  });

  loadDashboard();
  window.setInterval(loadDashboard, POLL_MS);
});

async function loadDashboard() {
  await Promise.all([loadSessions({ preserveSelection: true }), loadPlanAudit()]);
}

async function loadSessions({ preserveSelection }) {
  const params = new URLSearchParams({ limit: String(SESSION_LIMIT) });
  if (elements.stateFilter.value) {
    params.set("state", elements.stateFilter.value);
  }

  elements.sessionListStatus.textContent = "Refreshing sessions...";

  try {
    const payload = await fetchJSON(`/api/sessions?${params.toString()}`);
    state.sessions = payload.sessions;
    renderSessionList();

    if (!preserveSelection || !state.selectedSessionId) {
      state.selectedSessionId = state.sessions[0]?.id ?? null;
    }

    if (state.selectedSessionId) {
      const hasSelected = state.sessions.some((session) => session.id === state.selectedSessionId);
      if (!hasSelected && !preserveSelection) {
        state.selectedSessionId = state.sessions[0]?.id ?? null;
      }
      syncURL();
      await loadSessionDetail(state.selectedSessionId);
    } else {
      state.selectedSessionDetail = null;
      renderSessionDetail();
    }

    elements.sessionListStatus.textContent = `${state.sessions.length} session${state.sessions.length === 1 ? "" : "s"} shown`;
  } catch (error) {
    elements.sessionListStatus.textContent = error.message;
    elements.sessionList.innerHTML = "";
  }
}

async function loadSessionDetail(sessionID) {
  elements.detailStatus.textContent = "Loading detail...";
  elements.detailStatus.className = "status-chip";
  try {
    state.selectedSessionDetail = await fetchJSON(`/api/sessions/${sessionID}`);
    renderSessionDetail();
    elements.detailStatus.textContent = state.selectedSessionDetail.session.state;
    elements.detailStatus.className = `status-chip is-${state.selectedSessionDetail.session.state}`;
  } catch (error) {
    state.selectedSessionDetail = null;
    renderSessionDetail(error.message);
    elements.detailStatus.textContent = "Unavailable";
    elements.detailStatus.className = "status-chip is-failed";
  }
}

async function loadPlanAudit() {
  try {
    const payload = await fetchJSON(`/api/stats/plan-audit?limit=${AUDIT_LIMIT}`);
    renderPlanAudit(payload);
  } catch (error) {
    elements.auditSummary.innerHTML = `<div class="empty-state">${escapeHTML(error.message)}</div>`;
    elements.auditRuns.innerHTML = "";
  }
}

function renderSessionList() {
  if (!state.sessions.length) {
    elements.sessionList.innerHTML = '<div class="empty-state">No sessions match the current filter.</div>';
    return;
  }

  elements.sessionList.innerHTML = state.sessions.map((session) => {
    const activeClass = session.id === state.selectedSessionId ? "is-active" : "";
    const latest = session.latest_workflow_state ? `Latest workflow: ${escapeHTML(session.latest_workflow_state)}` : "No workflow yet";
    return `
      <article class="session-card ${activeClass}" data-session-id="${session.id}" tabindex="0">
        <div class="session-card-head">
          <div>
            <div class="session-id">${escapeHTML(shortID(session.id))}</div>
            <strong>${escapeHTML(session.project)}</strong>
          </div>
          <span class="badge is-${escapeHTML(session.state)}">${escapeHTML(session.state)}</span>
        </div>
        <div class="badge-row">
          <span class="badge">${escapeHTML(session.kind)}</span>
          <span class="badge">${session.plan_runs_count} plan</span>
          <span class="badge">${session.pipeline_runs_count} run</span>
        </div>
        ${session.source_spec_path ? `<div class="session-card-copy">${escapeHTML(session.source_spec_path)}</div>` : ""}
        <div class="card-meta">
          <span>${escapeHTML(latest)}</span>
          <span>${escapeHTML(formatTimestamp(session.created_at))}</span>
        </div>
      </article>
    `;
  }).join("");

  elements.sessionList.querySelectorAll(".session-card").forEach((card) => {
    const activate = () => {
      state.selectedSessionId = card.dataset.sessionId;
      syncURL();
      renderSessionList();
      loadSessionDetail(state.selectedSessionId);
    };
    card.addEventListener("click", activate);
    card.addEventListener("keydown", (event) => {
      if (event.key === "Enter" || event.key === " ") {
        event.preventDefault();
        activate();
      }
    });
  });
}

function renderSessionDetail(errorMessage = "") {
  if (errorMessage) {
    elements.sessionOverview.innerHTML = `<div class="empty-state">${escapeHTML(errorMessage)}</div>`;
    elements.sessionSummary.innerHTML = `<div class="empty-state">${escapeHTML(errorMessage)}</div>`;
    elements.planRuns.innerHTML = '<div class="empty-state">No plan run data available.</div>';
    elements.pipelineRuns.innerHTML = '<div class="empty-state">No execution run data available.</div>';
    elements.artifacts.innerHTML = '<div class="empty-state">No artifact data available.</div>';
    return;
  }

  if (!state.selectedSessionDetail) {
    elements.sessionOverview.innerHTML = '<div class="empty-state">Pick a session to inspect plan runs, pipeline runs, and artifacts.</div>';
    elements.sessionSummary.innerHTML = '<div class="empty-state">Pick a session to inspect plan runs, pipeline runs, and artifacts.</div>';
    elements.planRuns.innerHTML = '<div class="empty-state">No plan run data available.</div>';
    elements.pipelineRuns.innerHTML = '<div class="empty-state">No execution run data available.</div>';
    elements.artifacts.innerHTML = '<div class="empty-state">No artifact data available.</div>';
    return;
  }

  const { session, plan_runs: planRuns, pipeline_runs: pipelineRuns, artifacts } = state.selectedSessionDetail;

  elements.sessionOverview.innerHTML = `
    <div class="summary-block detail-title">
      <div>
        <div class="summary-key">Project</div>
        <h3>${escapeHTML(session.project)}</h3>
      </div>
      <div class="meta-row">
        <span class="badge is-${escapeHTML(session.state)}">${escapeHTML(session.state)}</span>
        <span class="badge">${escapeHTML(session.kind)}</span>
        <span class="badge">${planRuns.length} plan</span>
        <span class="badge">${pipelineRuns.length} run</span>
        <span class="badge">${artifacts.length} artifact</span>
      </div>
      <div class="path-block">${escapeHTML(session.id)}</div>
    </div>
    <div class="detail-support">
      <div class="detail-note">Use the session cards on the left to move between runs while this page keeps polling in place.</div>
      ${session.source_spec_path ? `<div class="path-block">${escapeHTML(session.source_spec_path)}</div>` : '<div class="detail-note">No source spec path recorded for this session.</div>'}
      ${session.error_message ? `<div class="error-block">${escapeHTML(session.error_message)}</div>` : ""}
    </div>
  `;

  elements.sessionSummary.innerHTML = [
    summaryBlock("Session ID", shortID(session.id)),
    summaryBlock("Kind", session.kind),
    summaryBlock("State", session.state),
    summaryBlock("Created", formatTimestamp(session.created_at)),
    summaryBlock("Started", formatTimestamp(session.started_at)),
    summaryBlock("Updated", formatTimestamp(session.updated_at)),
    summaryBlock("Completed", formatTimestamp(session.completed_at)),
    summaryBlock("Source", session.source_spec_path || "n/a", true),
  ].join("");

  elements.planRuns.innerHTML = renderCollection(planRuns, (run) => `
    <article class="data-card">
      <div class="data-card-head">
        <strong>${escapeHTML(run.spec_file)}</strong>
        <span class="mono">${escapeHTML(shortID(run.id))}</span>
      </div>
      <div class="meta-row">
        <span class="badge">Orders ${numberOrDash(run.post_audit_work_order_count ?? run.work_orders_generated)}</span>
        <span class="badge">Delta ${signedNumber(run.work_order_delta)}</span>
        ${run.project ? `<span class="badge">${escapeHTML(run.project)}</span>` : ""}
      </div>
      ${run.spec_fingerprint ? `<div class="path-block">${escapeHTML(run.spec_fingerprint)}</div>` : ""}
      ${renderChanges(run.audit_changes)}
      <div class="card-meta">
        <span>${escapeHTML(formatTimestamp(run.created_at))}</span>
      </div>
    </article>
  `, "No plan runs linked to this session.");

  elements.pipelineRuns.innerHTML = renderCollection(pipelineRuns, (run) => `
    <article class="data-card">
      <div class="data-card-head">
        <strong>${escapeHTML(run.work_order_type || "execution")}</strong>
        <span class="mono">${escapeHTML(shortID(run.workflow_id))}</span>
      </div>
      <div class="path-block">${escapeHTML(run.workflow_id)}</div>
      <div class="card-meta">
        <span>Pipeline run: ${escapeHTML(shortID(run.id))}</span>
        <span>${escapeHTML(formatTimestamp(run.created_at))}</span>
      </div>
    </article>
  `, "No execution runs linked to this session.");

  elements.artifacts.innerHTML = renderCollection(artifacts, (artifact) => `
    <article class="data-card">
      <div class="data-card-head">
        <strong>${escapeHTML(artifact.artifact_type)}</strong>
        <span>${escapeHTML(bytesLabel(artifact.size_bytes))}</span>
      </div>
      <div class="path-block">${escapeHTML(artifact.path)}</div>
      ${artifact.workflow_id ? `<div class="card-copy"><p>Workflow ${escapeHTML(shortID(artifact.workflow_id))}</p></div>` : ""}
      <div class="card-meta">
        <span>${escapeHTML(formatTimestamp(artifact.created_at))}</span>
      </div>
    </article>
  `, "No artifacts linked to this session.");
}

function renderPlanAudit(payload) {
  const { summary, recent_runs: recentRuns } = payload;
  elements.auditSummary.innerHTML = [
    auditStat("Changed", summary.changed_runs),
    auditStat("Unchanged", summary.unchanged_runs),
    auditStat("Total", summary.total_runs),
  ].join("");

  elements.auditRuns.innerHTML = renderCollection(recentRuns, (run) => `
    <article class="data-card">
      <div class="data-card-head">
        <strong>${escapeHTML(run.spec_file)}</strong>
        <span class="badge ${run.audit_changed ? "is-running" : "is-completed"}">${run.audit_changed ? "changed" : "unchanged"}</span>
      </div>
      <div class="card-meta">
        <span>Delta: ${signedNumber(run.work_order_delta)}</span>
        <span>${escapeHTML(formatTimestamp(run.created_at))}</span>
      </div>
      ${renderChanges(run.audit_changes)}
    </article>
  `, "No plan audit data yet.");
}

function renderCollection(items, renderItem, emptyText) {
  if (!items || !items.length) {
    return `<div class="empty-state">${escapeHTML(emptyText)}</div>`;
  }
  return items.map(renderItem).join("");
}

function renderChanges(changes) {
  if (!changes || !changes.length) {
    return "";
  }
  return `<ul class="changes">${changes.map((change) => `<li>${escapeHTML(change)}</li>`).join("")}</ul>`;
}

function summaryBlock(label, value, wrap = false) {
  const contentClass = wrap ? "summary-value path-block" : "summary-value";
  return `
    <div class="summary-block">
      <div class="summary-key">${escapeHTML(label)}</div>
      <div class="${contentClass}">${escapeHTML(String(value))}</div>
    </div>
  `;
}

function auditStat(label, value) {
  return `
    <div class="audit-stat">
      <span class="muted">${escapeHTML(label)}</span>
      <strong>${escapeHTML(String(value))}</strong>
    </div>
  `;
}

async function fetchJSON(path) {
  const response = await fetch(path, { headers: { Accept: "application/json" } });
  const payload = await response.json().catch(() => ({}));
  if (!response.ok) {
    throw new Error(payload.error || `Request failed: ${response.status}`);
  }
  return payload;
}

function syncURL() {
  const url = new URL(window.location.href);
  if (state.selectedSessionId) {
    url.searchParams.set("session", state.selectedSessionId);
  } else {
    url.searchParams.delete("session");
  }
  window.history.replaceState({}, "", url);
}

function shortID(id) {
  return id && id.length > 8 ? id.slice(0, 8) : id || "";
}

function formatTimestamp(value) {
  if (!value) {
    return "n/a";
  }
  const normalized = value.includes("T") ? value : value.replace(" ", "T");
  const date = new Date(normalized.endsWith("Z") ? normalized : `${normalized}Z`);
  if (Number.isNaN(date.getTime())) {
    return value;
  }
  return date.toLocaleString();
}

function numberOrDash(value) {
  return value === null || value === undefined ? "-" : value;
}

function signedNumber(value) {
  if (value === null || value === undefined) {
    return "-";
  }
  return value > 0 ? `+${value}` : String(value);
}

function bytesLabel(size) {
  if (size === null || size === undefined) {
    return "size n/a";
  }
  if (size < 1024) {
    return `${size} B`;
  }
  if (size < 1024 * 1024) {
    return `${(size / 1024).toFixed(1)} KiB`;
  }
  return `${(size / (1024 * 1024)).toFixed(1)} MiB`;
}

function escapeHTML(value) {
  return String(value)
    .replaceAll("&", "&amp;")
    .replaceAll("<", "&lt;")
    .replaceAll(">", "&gt;")
    .replaceAll('"', "&quot;")
    .replaceAll("'", "&#39;");
}
