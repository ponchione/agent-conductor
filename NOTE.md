# Where We Left Off — 2026-03-21

## Completed

- **Spec 01: Foundation & App Shell** — committed as `01d6e14`
  - Tailwind CSS v4, shadcn/ui, React Router v7, dark theme
  - SPA fallback routing in Go backend
  - Sidebar, shared components, placeholder pages

- **Spec 02: Pipeline Space (Read-Only)** — committed as `5e1398a`
  - Go backend: `GET /api/workflows` (list with filtering/pagination/joins) and `GET /api/workflows/:id` (detail with pipeline_run + sub_calls)
  - TypeScript types and API client extensions
  - Pipeline list panel with filterable workflow cards, pagination, visual states
  - Workflow detail shell with header, PhaseProgressStrip, 5-tab URL-routed interface
  - Overview tab: phase timing table, cost/token metrics, scope quality metrics
  - Events tab: SSE connection, reverse-chronological event cards, connection status

- **Spec 03: Pipeline Interactive** — committed as `baa2dd5`
  - Backend: `GET /api/workflows/:id/diff`, `GET /api/workflows/:id/scope`, `POST .../approve`, `POST .../reject`
  - TerminalViewer (virtualized, auto-scroll, scroll-lock), DiffModal, WorkflowBuild (SSE streaming), WorkflowVerify (precheck cards, LLM analysis), WorkflowScope (collapsible JSON tree)
  - Approve/reject buttons with confirmation dialogs, review summary on Overview tab

- **Spec 04: Queue System** — committed as `c7e0b50`
  - In-memory RunQueue with mutex-protected state machine (idle/ready/running/paused/completed)
  - 8 API endpoints: GET/POST/DELETE queue, reorder, start/pause/continue, SSE events
  - useQueue hook with 3s polling, QueueStrip sidebar widget, QueueDrawer slide-over panel
  - Drag-and-drop reorder, per-item override expansion, workflow navigation links

- **Spec 05: Plan Space** — committed as `e447a8d`
  - Backend: `GET /api/work-orders` (list), `GET /api/work-orders/:filename` (read), `PUT /api/work-orders/:filename` (update with YAML validation), `POST /api/plan` (session creation)
  - CodeViewer component (CodeMirror 6 wrapper with dark theme, YAML highlighting)
  - PlanSpace master-detail layout with plan run list from audit stats
  - NewPlan form with spec editor, file picker, generate button
  - PlanRunDetail with audit summary MetricCards, expandable work order cards with inline editing, queue selection
  - Note: POST /api/plan creates session but background plan execution requires extracting plan logic from cmd/conductor/plan.go into an importable package (TODO)

- **Spec 06: Config & Overrides** — uncommitted, ready to commit
  - Backend: OverrideStore (mutex-protected, concurrent-safe), ValidateOverrides (role + provider + model), ResolveModel (per-workflow > session > project.yaml)
  - 3 API endpoints: `GET /api/config/roles`, `GET /api/config/overrides`, `PUT /api/config/overrides`
  - Server struct extended with `cfg *config.ProjectConfig` and `overrideStore *OverrideStore`
  - Frontend: useConfig hook, ConfigPanel with RoleOverrideDropdown, Sidebar integration
  - QueueDrawer per-item override display (read-only dropdowns showing effective override chain)
  - Known v1 limitations: connection status hardcoded to "Connected" (no SSE state monitoring), per-item queue overrides are read-only (no PATCH endpoint), available_models returns one model per provider (ProviderConfig stores single Model field)

## Pick Up Next

- **Spec 07: Analytics Space** — next in line
  - Spec at: `docs/specs/UI/07-analytics-space/` (if exists)

- Remaining after that: 08 (CLI Lock Mechanism)

## Build Verification

All passing as of end of session:
- `make build` — pass
- `make test` — pass (all 22 packages, 0 failures)
- `cd web && npm run build` — pass (chunk size warning from CodeMirror is expected)
