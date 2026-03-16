# Agent Conductor Observability V2 Spec

## Purpose

This spec defines the next major observability slice for Agent Conductor.

It is intentionally broader than a handoff note and narrower than a set of
work orders. The goal is to give `conductor plan` a complete, validated input
that reflects:

- what already exists
- what was proven by the current PoC
- what architectural decisions are now settled
- what should happen next
- what should explicitly not happen yet

This spec supersedes ad hoc dashboard direction that emerged during the first
observability implementation pass.

## Product Context

Agent Conductor is a local-first orchestration system for AI-assisted code
generation. It plans work from specs, runs a scope/build/verify pipeline, and
stops in human review before final approval.

An observability surface is needed so a user can understand:

- what planning and execution sessions exist
- what state each session is in
- what plan artifacts and execution artifacts were produced
- what changed during plan audit
- what is happening during active runs

The first observability PoC now exists and has been manually validated against
real project data. That PoC proved the data model and basic UX, but it is not
the final architecture.

## What Already Exists

### Session-backed observability model

Already implemented:

- `sessions` table
- `session_id` linkage on `plan_runs`
- `session_id` linkage on `pipeline_runs`
- session lifecycle helpers
- workflow/session state mirroring for run-only sessions
- artifact registration for planning and execution outputs
- richer `plan_runs` telemetry fields

Relevant code:

- `internal/database/sessions.go`
- `internal/database/plan_runs.go`
- `internal/database/artifacts.go`
- `internal/database/workflows.go`
- `internal/database/database.go`
- `cmd/conductor/plan.go`
- `cmd/conductor/run.go`

### Read/query layer

Already implemented:

- `ListSessions(...)`
- `GetSessionDetail(...)`
- `ListPlanRunUsefulness(...)`
- `GetPlanAuditChangeStats(...)`

### CLI readers

Already implemented:

- `conductor sessions`
- `conductor session <id-or-prefix>`
- `conductor stats`
- `conductor status`

### Current PoC server and UI

Already implemented:

- `conductor serve`
- single-project DB selection via `--db`, `--data-dir`, or `--project`
- current REST endpoints:
  - `GET /api/sessions`
  - `GET /api/sessions/:id`
  - `GET /api/stats/plan-audit`
- an interim `/observability` page served from embedded static assets
- 5-second polling for session list, session detail, and plan-audit views

Relevant code:

- `cmd/conductor/serve.go`
- `internal/api/server.go`
- `internal/api/response.go`
- `internal/api/static/observability.html`
- `internal/api/static/observability.css`
- `internal/api/static/observability.js`

### DB bootstrap fix for older databases

Already implemented:

- `NewDB` now upgrades older DBs without failing on indexes that reference
  columns introduced by later migrations

Relevant code:

- `internal/database/database.go`
- `internal/database/database_test.go`

## What The PoC Validated

The current single-project PoC successfully proved:

- current runs can write into the session-backed model
- the REST read layer works against real project DBs
- `conductor serve --db ...` is a usable local workflow
- a UI can render session-first observability correctly
- the current data is sufficient for a basic session detail and plan-audit view

Real manual validation confirmed:

- `conductor serve --db ...` starts successfully
- `/observability` loads
- new run activity appears
- the page updates every 5 seconds
- `curl` requests to the read API succeed

## Problems Identified After Using The PoC

The PoC also revealed important architectural issues:

1. The raw `net/http` server shape is not the preferred long-term foundation.
2. Polling-only updates are acceptable for the PoC but not for live monitoring.
3. The current frontend implementation is useful for proof-of-life only; the
   real target frontend should be React-based.
4. Historical project DBs that predate the session-backed model naturally show
   no session data. That is acceptable for now and should not force a backfill
   detour.

## Settled Product Decisions

These are considered decided for the next planning pass.

### 1. Historical backfill is out of scope

Do not plan historical backfill work right now.

Reasoning:

- old project DBs may be wiped anyway
- backfill is lower value than finishing the forward-looking architecture

### 2. React is the target frontend

Do not plan more investment in plain HTML/CSS/JS beyond interim maintenance.

Reasoning:

- the current static UI served its purpose as a PoC
- the real frontend should be React-based

### 3. The next slice stays single-project

Do not broaden to multi-project aggregation yet.

Reasoning:

- first fix the server/router and live-update architecture
- then land the React frontend on that corrected single-project base
- only after that should multi-project support be added

### 4. The long-term server should use chi

The next server refactor should move to `chi`.

Reasoning:

- the server will likely grow beyond a few simple routes
- grouped routes and middleware are easier to manage in `chi`
- it is cheaper to make this correction now than after the frontend grows

### 5. The long-term live-update model should be REST + SSE

Use a hybrid transport model:

- REST for passive read models and initial page load
- SSE for active/live monitoring

Reasoning:

- polling is fine for session lists, history, and plan-audit summaries
- polling is weak for active build output and fast phase transitions
- SSE fits one-way live updates well without the complexity of WebSockets

## Existing Event Model To Reuse

There is already an event vocabulary in `internal/pipeline/events.go`:

- `phase_start`
- `phase_complete`
- `phase_error`
- `build_stdout`
- `scope_step`
- `verify_precheck`
- `verify_result`
- `run_complete`
- `run_awaiting_review`

This event vocabulary should be reused or adapted rather than replaced with a
second overlapping model.

Important caveat:

- this event model is not yet wired into `conductor serve`

## Desired End State For This V2 Slice

The V2 observability slice should produce a corrected single-project
observability stack with these characteristics:

### Backend/server

- `conductor serve` remains the entrypoint
- the server uses `chi`
- the current REST endpoints remain available unless there is a strong reason
  to evolve them
- at least one SSE endpoint exists for live monitoring
- static asset serving continues to work for the frontend bundle
- the current DB-selection behavior (`--db`, `--data-dir`, `--project`) remains
  intact

### Frontend

- the `/observability` surface is implemented in React + TypeScript
- initial load uses REST
- passive surfaces use REST-backed fetches
- active/live monitoring uses SSE
- the current static PoC can be retired only after React reaches parity

### User experience

The user should be able to:

- start the server against a project DB without needing `project.yaml`
- open a React observability UI at `/observability`
- browse session summaries
- open a session detail view
- inspect plan-audit outcomes
- watch an active run update live without relying solely on periodic polling

## Functional Requirements

### REST read surfaces

The existing read surfaces must remain available:

- `GET /api/sessions`
- `GET /api/sessions/:id`
- `GET /api/stats/plan-audit`

They should continue to map to the existing DB helpers where practical.

### SSE live monitoring surface

Add a live stream endpoint for active monitoring.

The exact route may be decided during implementation, but the behavior should
support:

- connecting from the browser using standard EventSource
- receiving live session/run updates for an active execution
- receiving build output / phase changes / verification-relevant events
- clean disconnect behavior

The SSE design should be compatible with the existing pipeline event vocabulary.

### React frontend shell

The React app should provide:

- an application shell for `/observability`
- typed API client(s)
- typed frontend models matching backend responses
- routing or URL-state that preserves focused session selection
- a live-monitor-capable view design that can combine REST state with SSE events

### Session dashboard

The UI should support:

- session list
- state filtering
- selected session detail
- readable rendering for long paths and identifiers
- clear empty states and error states

### Plan audit view

The UI should support:

- changed vs unchanged summary
- recent plan-audit outcomes
- readable rendering of longer audit notes

## Non-Goals

The following are explicitly out of scope for this spec:

- historical backfill for old DB rows with no session linkage
- multi-project aggregation
- control panel / run submission UI
- approve/reject action endpoints
- analytics/trends beyond the current plan-audit summary
- replacing the planner itself
- changing the core session DB model unless a real blocker is discovered

## Technical Constraints

- preserve the current single-project behavior during this V2 slice
- preserve the current `conductor serve` entrypoint
- preserve `--db`, `--data-dir`, and `--project` support
- keep the current REST endpoints available unless there is a strong migration
  reason to evolve them
- use React + TypeScript for the real frontend implementation
- prefer reuse of the existing event vocabulary and DB helpers
- keep the build/test environment compatible with the repo’s current CGO/LanceDB
  requirements

## Validation Expectations

The next implementation derived from this spec should be validated with:

### Automated

- focused backend tests for router/server behavior
- focused SSE tests
- focused frontend tests where practical
- focused DB upgrade tests must remain green
- existing observability API tests must remain green

### Manual

At minimum:

1. start `conductor serve --db <path>`
2. open `/observability`
3. verify REST-backed session and plan-audit data load
4. run a fresh work order
5. verify live updates appear without relying solely on fixed polling intervals

## Planning Guidance

When using `conductor plan` against this spec:

- do not generate work orders for historical backfill
- do not generate work orders for multi-project support yet
- do not assume the current static PoC should be expanded much further
- do generate work around:
  - `chi` server refactor
  - SSE transport for live monitoring
  - React frontend foundation
  - React session dashboard
  - React plan-audit view
  - cutover from static PoC to React once parity exists

## Known Risks

- The current event model may not map 1:1 onto the final SSE payload shape and
  may require adaptation.
- Refactoring to `chi` while preserving the existing endpoints may touch test
  structure and static asset serving.
- Mixing REST baseline state with SSE incremental state in the React UI will
  require careful state ownership.
- Broad `./internal/database` test runs may still hit the existing flaky
  concurrent-claim SQLite test.

## Reference Files

Useful files for planning and implementation context:

- `README.md`
- `UI_OBSERVABILITY_HANDOFF.md`
- `cmd/conductor/serve.go`
- `cmd/conductor/serve_test.go`
- `internal/api/server.go`
- `internal/api/response.go`
- `internal/api/server_test.go`
- `internal/database/sessions.go`
- `internal/database/plan_runs.go`
- `internal/database/artifacts.go`
- `internal/database/database.go`
- `internal/database/database_test.go`
- `internal/pipeline/events.go`

