# UI Observability Handoff

## Purpose

This is the single handoff document for the next session.

It replaces the earlier observability planning documents and reflects the
current repository state as of 2026-03-15, including what was actually
implemented, what was validated, and what should happen next.

## Current Status

The observability/backend work is no longer in planning-only state.

Current state:

- Phase 1 session backbone is implemented
- Phase 1 lifecycle behavior has been smoke-validated against a real run path
- Phase 2 backend groundwork is implemented
- Phase 2 artifact registration is implemented for planning and execution
- Phase 2 read/query surfaces have started
- there is still no HTTP/server polling API layer
- there is still no frontend implementation in this repo

## What Is Implemented

### 1. Session backbone and lifecycle

Implemented:

- `sessions` table
- `session_id` linkage on `plan_runs`
- `session_id` linkage on `pipeline_runs`
- `StartSession`, `TransitionSessionState`, session list/detail helpers
- centralized workflow transition helper
- task claim now stamps `tasks.started_at`
- workflow start/terminal timestamps are maintained
- run-only sessions mirror workflow state transitions

Primary files:

- [`internal/database/sessions.go`](/home/gernsback/source/agent-conductor/internal/database/sessions.go)
- [`internal/database/tasks.go`](/home/gernsback/source/agent-conductor/internal/database/tasks.go)
- [`internal/database/workflows.go`](/home/gernsback/source/agent-conductor/internal/database/workflows.go)
- [`cmd/conductor/plan.go`](/home/gernsback/source/agent-conductor/cmd/conductor/plan.go)
- [`cmd/conductor/run.go`](/home/gernsback/source/agent-conductor/cmd/conductor/run.go)

### 2. Artifact registry and stronger plan telemetry

Implemented:

- `artifacts` table
- artifact DB helper layer
- successful plan raw generation output persistence
- successful plan raw audit output persistence
- generated work-order artifact registration
- context package artifact registration
- verify report artifact registration
- build stdout/stderr artifact registration
- expanded `plan_runs` fields:
  - `project`
  - `spec_fingerprint`
  - `pre_audit_work_order_count`
  - `post_audit_work_order_count`
  - `audit_change_text`

Primary files:

- [`internal/database/schema.sql`](/home/gernsback/source/agent-conductor/internal/database/schema.sql)
- [`internal/database/artifacts.go`](/home/gernsback/source/agent-conductor/internal/database/artifacts.go)
- [`internal/database/database.go`](/home/gernsback/source/agent-conductor/internal/database/database.go)
- [`sql/queries.sql`](/home/gernsback/source/agent-conductor/sql/queries.sql)
- [`cmd/conductor/plan.go`](/home/gernsback/source/agent-conductor/cmd/conductor/plan.go)
- [`internal/worker/scope.go`](/home/gernsback/source/agent-conductor/internal/worker/scope.go)
- [`internal/worker/build.go`](/home/gernsback/source/agent-conductor/internal/worker/build.go)
- [`internal/worker/verify.go`](/home/gernsback/source/agent-conductor/internal/worker/verify.go)
- [`internal/worker/artifacts.go`](/home/gernsback/source/agent-conductor/internal/worker/artifacts.go)

### 3. Read/query surfaces for polling consumers

Implemented:

- session list query
- session detail query returning:
  - session row
  - linked `plan_runs`
  - linked `pipeline_runs`
  - linked artifacts
- plan usefulness queries:
  - recent plan usefulness rows
  - aggregate changed vs unchanged audit counts

Primary files:

- [`internal/database/sessions.go`](/home/gernsback/source/agent-conductor/internal/database/sessions.go)
- [`internal/database/plan_runs.go`](/home/gernsback/source/agent-conductor/internal/database/plan_runs.go)

### 4. CLI consumers of the new model

Implemented:

- `conductor sessions`
- `conductor session <id-or-prefix>`
- `conductor stats` now includes a plan audit effectiveness section
- `conductor status` already exposes workflow task progress

Primary files:

- [`cmd/conductor/sessions.go`](/home/gernsback/source/agent-conductor/cmd/conductor/sessions.go)
- [`cmd/conductor/session.go`](/home/gernsback/source/agent-conductor/cmd/conductor/session.go)
- [`cmd/conductor/stats.go`](/home/gernsback/source/agent-conductor/cmd/conductor/stats.go)
- [`cmd/conductor/status.go`](/home/gernsback/source/agent-conductor/cmd/conductor/status.go)

## Validation Completed

### Smoke validation

Real smoke work was executed with a freshly built current binary, not the stale
checked-in one.

Observed and validated:

1. `plan` created a `plan_only` session and linked `plan_runs`
2. `run` created a `run_only` session and linked `pipeline_runs`
3. `tasks.started_at` was stamped on claim
4. `workflows.started_at` was stamped when work began
5. `run_only` session transitioned to `awaiting_review` when workflow entered
   `human_review`
6. rejecting the smoke workflow transitioned both workflow and session to
   `failed` and stamped terminal `completed_at`
7. later smoke runs confirmed:
   - planning artifacts were registered
   - execution artifacts were registered
   - the new `sessions` and `session` commands read the stored data correctly

Smoke environment used:

- worktree: `/tmp/agent-conductor-smoke`
- auxiliary files: `/tmp/agent-conductor-smoke-artifacts`
- smoke project DB:
  `/home/gernsback/source/.conductor/projects/agent-conductor-smoke/db/conductor.db`

### Focused automated validation

The following focused validations passed during this session:

```bash
go test ./internal/database -run 'TestRegisterArtifactAndQueries|TestStartSessionAndTransitionState|TestAtomicClaimTask_SetsStartedTimestamps|TestTransitionWorkflowState_MirrorsRunOnlySessionState|TestLinkPlanRunToSession|TestGetSessionDetailIncludesRunsAndArtifacts|TestPlanRunUsefulnessQueries|TestListTasksByWorkflow|TestFindSessionsByPrefix'

go test ./cmd/conductor -run 'TestRecordPlanRunPersistsObservabilityFields|TestMergeInvokeClaudeResults'

go test ./internal/worker/...
```

`sqlc generate` was also rerun successfully after the `plan_runs` insert shape
changed.

## Known Constraints and Caveats

### 1. Broad package test flake still exists

There is a pre-existing flaky database test:

- `TestAtomicClaimTask_ConcurrentClaimSingleWinner`

It can fail with timeout / SQLite locking behavior when running a broader
unfiltered `./internal/database` package test pass.

This was not introduced by the observability work.

### 2. Build/test environment requires the repo's CGO settings

Current builds/tests rely on the vendored LanceDB libraries.

Use the repo's CGO settings, for example:

```bash
CGO_CFLAGS="-I$(pwd)/include" \
CGO_LDFLAGS="-L$(pwd)/lib/linux_amd64 -llancedb_go -lm -ldl -lpthread" \
LD_LIBRARY_PATH="$(pwd)/lib/linux_amd64" \
go test ./...
```

### 3. Optional embedding endpoint may be absent

Smoke runs produced warnings when `localhost:8081` embeddings were unavailable.
Those warnings did not block workflow/session lifecycle validation.

## Recommended Next Work

The backend foundation is now sufficient to stop doing schema-first work and
start doing delivery-surface work.

Recommended next steps, in order:

1. add HTTP/server read endpoints for:
   - session list
   - session detail
   - plan usefulness summary/trends
2. keep the API shapes thin and map directly onto the existing DB helpers
3. decide whether to expose workflow detail through the new session model or
   keep both workflow-first and session-first read paths
4. optionally extend CLI output so `list` or `status` can surface session IDs
   directly
5. only after the read API exists, begin the actual polling UI

## Suggested API Surfaces

If the next session starts the server layer, the minimum useful endpoints are:

- `GET /api/sessions`
- `GET /api/sessions/:id`
- `GET /api/stats/plan-audit`

These can map directly to:

- `ListSessions(...)`
- `GetSessionDetail(...)`
- `GetPlanAuditChangeStats(...)` plus `ListPlanRunUsefulness(...)`

## Important Repo State Notes

- The working tree is still dirty with unrelated code changes outside this
  observability slice.
- Do not revert unrelated user changes.
- The observability docs that preceded this file were planning artifacts, not
  the source of truth for current state.

## Definition of Good Next-Session Start

A good next session should:

1. read this file first
2. inspect the current dirty worktree before editing
3. avoid reopening the old planning questions
4. build the read API on top of the DB/query surfaces that now exist

## Superseded Documents

This file supersedes and replaces:

- `PHASE1_SESSION_BACKBONE_PLAN.md`
- `UI_OBSERVABILITY_IMPLEMENTATION_PLAN.md`
- `UI_OBSERVABILITY_NEXT_WORK_PLAN.md`
- `UI_OBSERVABILITY_ROADMAP.md`
