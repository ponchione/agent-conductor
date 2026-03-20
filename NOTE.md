# Where We Left Off — 2026-03-20

## Completed

- **Spec 01: Foundation & App Shell** — committed as `01d6e14`
  - Tailwind CSS v4, shadcn/ui, React Router v7, dark theme
  - SPA fallback routing in Go backend
  - Sidebar, shared components, placeholder pages

- **Spec 02: Pipeline Space (Read-Only)** — implemented, ready to commit
  - Go backend: `GET /api/workflows` (list with filtering/pagination/joins) and `GET /api/workflows/:id` (detail with pipeline_run + sub_calls)
  - TypeScript types and API client extensions
  - Pipeline list panel with filterable workflow cards, pagination, visual states
  - Workflow detail shell with header, PhaseProgressStrip, 5-tab URL-routed interface
  - Overview tab: phase timing table, cost/token metrics, scope quality metrics
  - Events tab: SSE connection, reverse-chronological event cards, connection status
  - Code reviewed — all important fixes applied

## Pick Up Tomorrow

- **Spec 03: Pipeline Interactive** — next in line
  - Approve/reject actions, build terminal viewer, diff modal, scope viewer, verify tab
  - Builds directly on top of Spec 02's detail shell and disabled buttons
  - Spec at: `docs/specs/UI/03-pipeline-interactive/`

- Remaining specs after that: 04 (Queue System), 05 (Plan Space), 06 (Config Overrides), 07 (Analytics Space), 08 (CLI Lock Mechanism)

## Build Verification

All passing as of end of session:
- `cd web && npm run build` — pass
- `make build` — pass
- `make test` — pass (all 22 packages)
