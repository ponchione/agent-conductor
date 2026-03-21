# Topham Dashboard — Manual Test Plan

## Prerequisites

### Environment Setup

1. **Start the server:**
   ```bash
   conductor serve --project project.yaml --addr 127.0.0.1:8088
   ```
   If no database exists yet, run at least one pipeline first:
   ```bash
   conductor run work-orders/some-work-order.yaml
   ```

2. **Open the dashboard:** Navigate to `http://localhost:8088` in a browser.

3. **Seed data:** Many tests require existing pipeline runs, plan runs, and sub_calls in the database. If the database is empty, tests will verify empty-state behavior only. For full coverage, run several pipelines with varying outcomes (PASS, FAIL, approved, rejected) before testing.

4. **Browser:** Use a Chromium-based browser with DevTools open (Network tab for API verification, Console for JS errors).

### Conventions

- **VERIFY** = check visually in the browser
- **EXPECT** = the expected outcome
- **API CHECK** = open DevTools Network tab to inspect the raw API response

---

## 1. Foundation & App Shell (Spec 01)

### 1.1 Initial Load and Navigation

| # | Step | Expected |
|---|------|----------|
| 1 | Open `http://localhost:8088` | Redirects to `/pipeline`. Dark theme renders. No console errors. |
| 2 | VERIFY the sidebar is visible on the left (~264px wide) | Three nav items: Plan, Pipeline, Analytics. "topham" title at top. |
| 3 | Click "Plan" in sidebar | URL changes to `/plan`. Plan space loads. "Plan" nav item is highlighted. |
| 4 | Click "Pipeline" in sidebar | URL changes to `/pipeline`. Pipeline nav item highlighted. |
| 5 | Click "Analytics" in sidebar | URL changes to `/analytics`. Analytics nav item highlighted. |
| 6 | Navigate to a non-existent URL like `/nonexistent` | SPA fallback serves the app shell (no 404 page — app handles routing). |

### 1.2 Responsive Layout

| # | Step | Expected |
|---|------|----------|
| 1 | Resize browser to narrow width | Sidebar remains fixed at 264px. Main content area shrinks. |
| 2 | VERIFY dark theme colors | Background is dark, text is light, borders are subtle. No white flashes. |

---

## 2. Pipeline Space — Read-Only (Spec 02)

### 2.1 Pipeline List

| # | Step | Expected |
|---|------|----------|
| 1 | Navigate to `/pipeline` | Pipeline list loads. Each workflow shows: title, state badge, cost, duration, timestamp. |
| 2 | API CHECK: `GET /api/workflows` | Returns JSON with `workflows` array and `total` count. |
| 3 | VERIFY status badges | Color-coded: running=blue, awaiting_review=amber, completed=green, failed=red. |
| 4 | If >20 workflows: scroll to bottom | Pagination controls appear. Click "Next" loads more. |
| 5 | If filters are available, apply a status filter | List updates to show only matching workflows. URL may update with query params. |
| 6 | If no workflows exist | "No workflows" or empty state message displays. |

### 2.2 Workflow Detail — Overview Tab

| # | Step | Expected |
|---|------|----------|
| 1 | Click a workflow in the list | URL changes to `/pipeline/:workflowId`. Detail view loads with tabs. |
| 2 | VERIFY Overview tab is selected by default | Shows: phase progress strip, cost/token metrics, scope quality metrics. |
| 3 | VERIFY PhaseProgressStrip | Three phases (Scope, Build, Verify) with status icons and durations. Completed phases show checkmarks. |
| 4 | VERIFY MetricCards | Display cost, tokens, duration with correct formatting ($X.XX, Xm Ys). |
| 5 | If workflow is in `human_review` state | Approve and Reject buttons are visible. |

### 2.3 Workflow Detail — Events Tab

| # | Step | Expected |
|---|------|----------|
| 1 | Click "Events" tab | URL changes to `/pipeline/:workflowId/events`. |
| 2 | VERIFY event cards | Reverse-chronological list of events. Each shows type, timestamp, data. |
| 3 | API CHECK: SSE connection | DevTools Network shows an EventSource connection to `/api/events/stream?workflow_id=...`. |
| 4 | If workflow is still running | New events appear in real-time without page refresh. Connection status indicator shows "Connected". |

---

## 3. Pipeline Space — Interactive (Spec 03)

### 3.1 Scope Tab

| # | Step | Expected |
|---|------|----------|
| 1 | Click "Scope" tab on a completed workflow | Context package JSON tree renders. Sections are collapsible. |
| 2 | API CHECK: `GET /api/workflows/:id/scope` | Returns `context_package` JSON and `source` string. |
| 3 | Expand/collapse tree nodes | Smooth toggle. No layout jumps. |

### 3.2 Build Tab

| # | Step | Expected |
|---|------|----------|
| 1 | Click "Build" tab on a completed workflow | Terminal output renders with monospace font. |
| 2 | VERIFY TerminalViewer | Auto-scrolls if streaming. Scroll-lock behavior when user scrolls up. |
| 3 | Click "View Diff" button (if available) | DiffModal opens showing the git diff. Files changed listed. Stats (additions/deletions) shown. |
| 4 | Close the diff modal | Modal dismisses cleanly. |

### 3.3 Verify Tab

| # | Step | Expected |
|---|------|----------|
| 1 | Click "Verify" tab | Precheck cards render with pass/fail icons. |
| 2 | VERIFY color coding | Pass items green, fail items red. |

### 3.4 Approve / Reject

| # | Step | Expected |
|---|------|----------|
| 1 | On a workflow in `human_review`, click "Approve" | Confirmation dialog appears. |
| 2 | Confirm the approval | API CHECK: `POST /api/workflows/:id/approve`. Workflow state transitions. Success message or state update. |
| 3 | On another `human_review` workflow, click "Reject" | Confirmation dialog appears. |
| 4 | Confirm the rejection | API CHECK: `POST /api/workflows/:id/reject`. Workflow state transitions to failed. |
| 5 | VERIFY buttons disappear after action | Approve/Reject no longer shown for completed/failed workflows. |

---

## 4. Queue System (Spec 04)

### 4.1 Queue Strip (Sidebar)

| # | Step | Expected |
|---|------|----------|
| 1 | VERIFY queue strip in sidebar below navigation | Shows current state: "Idle", "X Queued", "Running (X/Y)", "Paused", or "Complete". |
| 2 | When queue is running | Blue pulsing dot visible. Currently executing item's title shown. |
| 3 | Click "Manage Queue" button | Queue drawer slides in from the right. |

### 4.2 Queue Drawer — Basic Operations

| # | Step | Expected |
|---|------|----------|
| 1 | VERIFY drawer layout | Header with state badge and control buttons. Three sections: Completed, Executing, Pending. |
| 2 | Click backdrop (dark overlay) | Drawer closes. |
| 3 | Click X button | Drawer closes. |
| 4 | API CHECK: `GET /api/queue` | Returns `state`, `items` array, `current` (string ID), `pause_reason`. |

### 4.3 Queue Drawer — Adding Items

This test requires the Plan Space to be working, or a direct API call:

| # | Step | Expected |
|---|------|----------|
| 1 | Add items via Plan Space "Queue Selected" or API | API CHECK: `POST /api/queue` with `{"items": [...]}` body. |
| 2 | VERIFY items appear in Pending section | Title, type, target module visible. |
| 3 | Queue state transitions from "idle" to "ready" | Queue strip updates to show "X Queued". |

### 4.4 Queue Drawer — Controls

| # | Step | Expected |
|---|------|----------|
| 1 | With items queued (state=ready), click "Start" | API CHECK: `POST /api/queue/start`. State transitions to "running". First item starts executing. |
| 2 | While running, click "Pause" | API CHECK: `POST /api/queue/pause`. State transitions to "paused" after current item completes. |
| 3 | While paused, click "Continue" | API CHECK: `POST /api/queue/continue`. Execution resumes. |
| 4 | Click "Clear" with pending items | Confirmation dialog appears: "Remove all X pending items?" Confirm removes them. |

### 4.5 Queue Drawer — Drag and Drop Reorder

| # | Step | Expected |
|---|------|----------|
| 1 | With 3+ pending items, drag an item by its grip handle | Item visually lifts. Drop zone highlights on hover. |
| 2 | Drop the item in a new position | API CHECK: `POST /api/queue/reorder` with new order. Items re-render in new order. |
| 3 | Attempt to drag a completed or executing item | Not possible (only pending items have drag handles). |

### 4.6 Queue Drawer — Item Actions

| # | Step | Expected |
|---|------|----------|
| 1 | Click trash icon on a pending item | API CHECK: `DELETE /api/queue/:id`. Item removed. |
| 2 | Click "View" on a completed item with a workflow_id | Navigates to `/pipeline/:workflowId`. |
| 3 | Click "Overrides" expand button on a pending item | Override section expands showing role dropdowns (disabled/read-only in v1). |

### 4.7 Queue — SSE Events

| # | Step | Expected |
|---|------|----------|
| 1 | API CHECK: `GET /api/queue/events` | SSE endpoint returns `text/event-stream`. |
| 2 | Start the queue while observing events | Events like `queue_state_changed`, `queue_item_started`, `queue_item_completed` arrive. |

---

## 5. Plan Space (Spec 05)

### 5.1 Plan Run List

| # | Step | Expected |
|---|------|----------|
| 1 | Navigate to `/plan` | Left panel shows plan audit runs sorted by date descending. |
| 2 | API CHECK: `GET /api/stats/plan-audit` | Returns `summary` (changed/unchanged/total) and `recent_runs` array. |
| 3 | VERIFY each run item | Shows: spec file name, timestamp (via TimeAgo), "audited" badge if audit changed output. |
| 4 | Click a plan run | Right panel loads PlanRunDetail. URL updates to `/plan/:planRunId`. |
| 5 | If no plan runs exist | Empty state message in the list. |

### 5.2 New Plan

| # | Step | Expected |
|---|------|----------|
| 1 | Click "New" button in Plan Space header | URL changes to `/plan/new`. NewPlan form renders in right panel. |
| 2 | VERIFY the form | Spec editor (CodeMirror), file picker input, "Generate Plan" button. |
| 3 | Paste or type content in the editor | CodeMirror renders with dark theme. YAML content gets basic highlighting. |
| 4 | Click "Generate Plan" | API CHECK: `POST /api/plan`. Returns `session_id` and `status`. |
| 5 | After submission | Progress message appears. Navigates to plan run detail (note: background execution is a v1 stub — no actual plan generation occurs). |

### 5.3 Plan Run Detail

| # | Step | Expected |
|---|------|----------|
| 1 | View a plan run detail | MetricCards show: Pre-Audit count, Post-Audit count, Delta, WOs Generated. |
| 2 | VERIFY work order cards | Each work order expandable. Shows title, type, target module. |
| 3 | Expand a work order card | Inline CodeViewer shows YAML content. |
| 4 | Edit work order content and save | API CHECK: `PUT /api/work-orders/:filename`. Returns updated content with `valid` boolean. |
| 5 | Submit invalid YAML | API CHECK returns 400 with error message. Error displayed in UI. |

### 5.4 Work Order Endpoints

| # | Step | Expected |
|---|------|----------|
| 1 | API CHECK: `GET /api/work-orders` | Returns `work_orders` array with filename, title, type, target_module, size_bytes, modified_at. |
| 2 | API CHECK: `GET /api/work-orders/:filename` | Returns filename and content string. |
| 3 | API CHECK: `GET /api/work-orders/..%2Fetc%2Fpasswd` | Returns 400 (path traversal protection). |
| 4 | API CHECK: `GET /api/work-orders/nonexistent.yaml` | Returns 404. |

### 5.5 Queue Integration

| # | Step | Expected |
|---|------|----------|
| 1 | In PlanRunDetail, select work orders via checkboxes | "Queue Selected" button appears. |
| 2 | Click "Queue Selected" | API CHECK: `POST /api/queue` with `{"items": [...]}` containing selected filenames. Items appear in queue drawer. |

---

## 6. Config & Overrides (Spec 06)

### 6.1 Config Panel (Sidebar)

| # | Step | Expected |
|---|------|----------|
| 1 | VERIFY config panel at bottom of sidebar | Shows: "Config" header, project name, truncated project path, connection status. |
| 2 | Hover over the truncated path | Tooltip shows the full project path. |
| 3 | VERIFY connection status | Green dot + "Connected" label (hardcoded in v1). |
| 4 | If no project config loaded | Shows "No project loaded" with gray "Disconnected" indicator. |

### 6.2 Role Override Dropdowns

| # | Step | Expected |
|---|------|----------|
| 1 | VERIFY one dropdown per configured role | Each labeled with formatted role name (e.g., "Build", "Decompose", "Verify Analyze"). |
| 2 | Each dropdown shows "Default ({model})" as first option | Remaining options grouped by provider via `<optgroup>`. |
| 3 | Select a non-default model in a dropdown | API CHECK: `PUT /api/config/overrides` fires immediately with updated overrides. |
| 4 | VERIFY blue accent dot appears next to overridden role | Visual indicator of active override. |
| 5 | Select "Default" again | Override for that role is cleared. Blue dot disappears. |
| 6 | API CHECK: `GET /api/config/overrides` | Returns current session overrides as `{"overrides": {...}}`. |

### 6.3 Reset All

| # | Step | Expected |
|---|------|----------|
| 1 | Set overrides on 2+ roles | "Reset All" link appears below the dropdowns. |
| 2 | Click "Reset All" | All dropdowns revert to "Default". All blue dots disappear. |
| 3 | API CHECK: `PUT /api/config/overrides` with `{"overrides": {}}` | Overrides cleared. |
| 4 | When no overrides are active | "Reset All" link is hidden. |

### 6.4 Config Roles Endpoint

| # | Step | Expected |
|---|------|----------|
| 1 | API CHECK: `GET /api/config/roles` | Returns `roles` (array with name, current_provider, current_model, description), `available_models` (grouped by provider), `project` (name, path, data_dir). |
| 2 | Roles are sorted alphabetically | Deterministic order across requests. |

### 6.5 Validation

| # | Step | Expected |
|---|------|----------|
| 1 | API CHECK: `PUT /api/config/overrides` with unknown role | Returns 400 with `"unknown role"` message. |
| 2 | API CHECK: `PUT /api/config/overrides` with unknown provider | Returns 400 with `"unknown provider"` message. |
| 3 | API CHECK: `PUT /api/config/overrides` with unknown model | Returns 400 with `"unknown model"` message. |

### 6.6 Queue Drawer Overrides

| # | Step | Expected |
|---|------|----------|
| 1 | Open queue drawer, expand a pending item's "Overrides" | Role override dropdowns appear for each role. |
| 2 | Dropdowns are disabled (grayed out, cursor not-allowed) | Read-only in v1 — displays effective override chain but cannot be edited. |
| 3 | If session overrides are active | Dropdowns reflect session overrides as the selected value. |

---

## 7. Analytics Space (Spec 07)

### 7.1 Dashboard Layout and Controls

| # | Step | Expected |
|---|------|----------|
| 1 | Navigate to `/analytics` | Full-width scrollable dashboard loads. "Analytics" header visible. |
| 2 | VERIFY time range selector | Segmented buttons: 7d, 30d, 90d, All. "30d" selected by default. |
| 3 | Click "7d" | All data refetches. API CHECK: three calls to `/api/stats/summary?range=7d`, `/api/stats/trends?range=7d`, `/api/stats/models?range=7d`. |
| 4 | Click "All" | Data refetches with `range=all`. |
| 5 | VERIFY project filter dropdown | Shows "All Projects" (static in v1). |

### 7.2 Summary Cards

| # | Step | Expected |
|---|------|----------|
| 1 | VERIFY pipeline summary row (5 cards) | Total Runs (number), Pass Rate (%), Avg Cost ($X.XX), Avg Duration (Xm Ys), Verify Agreement (%). |
| 2 | VERIFY plan summary row (3 cards) | Plan Runs (number), Avg Plan Cost ($X.XX), Combined Total ($X.XX). |
| 3 | VERIFY subtitles | "pipeline executions", "X / Y" for pass rate, "per pipeline run", "verify matches human", "gen + audit per plan", "all runs". |
| 4 | While loading | Animated pulse skeleton placeholders visible for card areas. |
| 5 | With empty database | All values show 0, $0.00, 0.0%, 0s as appropriate. No crashes. |

### 7.3 Cost Over Time Chart

| # | Step | Expected |
|---|------|----------|
| 1 | Scroll to "Cost Over Time" card | Line chart with two lines: pipeline (blue #3b82f6) and plan (purple #8b5cf6). |
| 2 | Hover over data points | Tooltip shows exact cost values for that date. |
| 3 | VERIFY legend | Shows "Pipeline Cost" and "Plan Cost" labels. |
| 4 | With no data | "No data available for the selected time range" message centered in chart area. |

### 7.4 Duration by Phase Chart

| # | Step | Expected |
|---|------|----------|
| 1 | Scroll to "Duration by Phase" card | Stacked bar chart with three segments per bar. |
| 2 | VERIFY colors | Scope (light blue #93c5fd), Build (blue #3b82f6), Verify (dark blue #1d4ed8). |
| 3 | Hover over a bar segment | Tooltip shows per-phase duration in seconds. |
| 4 | VERIFY stacking | Segments stack on top of each other (not side-by-side). |

### 7.5 Verify Result Distribution (Pie Chart)

| # | Step | Expected |
|---|------|----------|
| 1 | Scroll to the two-column chart grid | Pie chart on the left, scatter on the right. |
| 2 | VERIFY pie segments | PASS (green #22c55e), FAIL (red #ef4444), ERROR (amber #f59e0b). |
| 3 | VERIFY center label | Total run count displayed inside the donut. |
| 4 | VERIFY legend | Shows count for each result type. |

### 7.6 Scope Quality Scatter Chart

| # | Step | Expected |
|---|------|----------|
| 1 | VERIFY scatter chart in right column | X-axis: Files Suggested. Y-axis: Paths Stripped. |
| 2 | VERIFY dot colors | Green dots for PASS, red dots for FAIL. |
| 3 | Hover over a dot | Tooltip shows: run ID, files suggested, paths stripped, verify result. |

### 7.7 Model Comparison Table

| # | Step | Expected |
|---|------|----------|
| 1 | Scroll to "Model Comparison" card | Table with columns: Model, Provider, Role, Runs, Avg Cost, Avg Duration, Tokens In, Tokens Out, Pass Rate. |
| 2 | Click "Runs" column header | Table sorts by run count. Sort indicator (triangle) appears. |
| 3 | Click "Runs" again | Sort direction reverses. |
| 4 | Click "Model" column header | Sorts alphabetically by model name. |
| 5 | VERIFY Pass Rate column | Shows percentage for build models, "N/A" for non-build roles. |
| 6 | VERIFY formatting | Costs as $X.XX, durations as Xm Ys or Xs, tokens with comma separators. |

### 7.8 Verify-Human Agreement Trend

| # | Step | Expected |
|---|------|----------|
| 1 | Scroll to "Verify-Human Agreement Trend" card | Line chart with single line showing rolling 10-run agreement %. |
| 2 | VERIFY Y-axis range | 0-100%. |
| 3 | VERIFY reference line | Horizontal dashed line at the overall agreement rate. Labeled "Overall". |
| 4 | Hover over the line | Tooltip shows exact agreement percentage for that point. |

### 7.9 Plan Audit Effectiveness Chart

| # | Step | Expected |
|---|------|----------|
| 1 | Scroll to "Plan Audit Effectiveness" card | Bar chart with stacked bars. |
| 2 | VERIFY bar segments | Pre-audit count in gray (#6b7280), audit-added portion in purple (#8b5cf6). |
| 3 | VERIFY delta labels | "+N" or "-N" annotations above bars where delta is non-zero. |
| 4 | Hover over a bar | Custom tooltip shows: Pre-audit count, Post-audit count, Delta. |

### 7.10 Analytics Empty States

| # | Step | Expected |
|---|------|----------|
| 1 | Select a time range with no data (e.g., "7d" on an old database) | All charts show "No data available for the selected time range". Summary cards show zeros. |
| 2 | While data is loading | Charts show "Loading..." text. Cards show animated pulse skeletons. |

---

## 8. CLI Lock Mechanism (Spec 08)

### 8.1 Lock File Lifecycle

| # | Step | Expected |
|---|------|----------|
| 1 | Start server: `conductor serve --project project.yaml` | Lock file created at `{data_dir}/topham.lock`. |
| 2 | Inspect lock file: `cat {data_dir}/topham.lock` | Valid JSON with `pid` (matches server PID), `port` (8088), `started_at` (ISO 8601). |
| 3 | Stop server with Ctrl+C (SIGINT) | Lock file is removed. Verify: `ls {data_dir}/topham.lock` → "No such file". |
| 4 | Start server again, then `kill <pid>` (SIGTERM) | Lock file is removed on shutdown. |

### 8.2 Duplicate Server Prevention

| # | Step | Expected |
|---|------|----------|
| 1 | Start server in terminal 1 | Server starts, lock file written. |
| 2 | In terminal 2, run `conductor serve --project project.yaml` | Error: "another topham server is already running (PID X, port 8088). Stop it first or use that instance." Exit code 1. |

### 8.3 Stale Lock Cleanup

| # | Step | Expected |
|---|------|----------|
| 1 | Start and stop the server normally | Lock file removed. |
| 2 | Manually create a fake lock file with a dead PID: `echo '{"pid":999999999,"port":8088,"started_at":"2026-01-01T00:00:00Z"}' > {data_dir}/topham.lock` | File exists with bogus PID. |
| 3 | Start server: `conductor serve --project project.yaml` | Warning logged about cleaning up stale lock. Server starts normally. New lock file written with correct PID. |

### 8.4 Mutation Command Guards

Test each with the server running in another terminal:

| # | Step | Expected |
|---|------|----------|
| 1 | `conductor run work-orders/test.yaml` | Error: "Topham server is active on port 8088 (PID X, started ...).\nUse the dashboard at http://localhost:8088 or stop the server first." Exit code 1. |
| 2 | `conductor plan specs/test-spec.md` | Same error message. Exit code 1. |
| 3 | `conductor approve <workflow-id>` | Same error message. Exit code 1. |
| 4 | `conductor reject <workflow-id>` | Same error message. Exit code 1. |

### 8.5 Read Commands Unaffected

Test each with the server running:

| # | Step | Expected |
|---|------|----------|
| 1 | `conductor list` | Executes normally. No lock error. |
| 2 | `conductor status <workflow-id>` | Executes normally. |
| 3 | `conductor stats` | Executes normally. |
| 4 | `conductor index` | Executes normally. |

### 8.6 Lock Without Project Config

| # | Step | Expected |
|---|------|----------|
| 1 | `conductor serve --db /path/to/conductor.db` (no --project, no --data-dir) | Server starts without lock protection (known v1 limitation). No lock file created. No error. |

---

## Cross-Cutting Concerns

### API Error Handling

| # | Step | Expected |
|---|------|----------|
| 1 | Stop the server while the dashboard is open | API calls fail. UI should not crash. Error states or stale data displayed. |
| 2 | Send malformed JSON to any PUT endpoint | Returns 400 with "invalid request body" message. |

### Browser Console

| # | Step | Expected |
|---|------|----------|
| 1 | Navigate through all pages and perform all major actions | No JavaScript errors in the console. Warnings are acceptable (React dev warnings, chunk size). |

### Concurrent Access

| # | Step | Expected |
|---|------|----------|
| 1 | Open the dashboard in two browser tabs | Both tabs show consistent data. Queue state syncs via polling. |
| 2 | Modify overrides in tab 1, refresh tab 2 | Tab 2 shows the updated overrides after refresh. |

### Performance

| # | Step | Expected |
|---|------|----------|
| 1 | Load analytics with 100+ pipeline runs | Charts render within 2-3 seconds. No browser freezing. |
| 2 | Open queue drawer with 20+ items | Renders smoothly. Drag-and-drop responsive. |
