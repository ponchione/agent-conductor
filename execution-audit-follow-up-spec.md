# Agent Conductor Execution Audit Follow-Up Spec

## Purpose

This spec defines an execution-time `audit` phase that runs after `verify`.

The goal is not to create an in-place retry loop. Instead, the goal is to let
the pipeline produce a structured post-verify audit result and, when
appropriate, generate a narrow follow-up work order that can be run as a fresh
pipeline execution with its own metrics.

This spec is intended to be directly consumable by `conductor plan`.

## Product Context

Agent Conductor currently plans work orders from specs and executes work orders
through a scope/build/verify pipeline before human review.

That execution model works well for a single narrow work order, but it has a
gap:

- `verify` already performs a substantial review
- the workflow then stops in `human_review`
- there is no structured execution-time audit that can convert findings into
  the next bounded unit of work

The planner already has a second-pass audit model for generated work orders.
Execution should gain a similar concept, but adapted for remediation rather
than decomposition.

## What Already Exists

### Current execution pipeline

The current execution path is:

- `scope`
- `build`
- `verify`
- `human_review`

`build` creates a `verify` task directly.

### Current verify behavior

The current `verify` phase is already more than a simple diff check. It:

- checks out the workflow branch
- computes the git diff against the base branch
- runs deterministic pre-checks for criteria mentioning `go build`, `go test`,
  or `go vet`
- segments the diff into logical chunks
- runs per-segment LLM analysis
- synthesizes a structured `VerificationReport`
- writes a verify report artifact
- stores verify metrics on `pipeline_runs`
- transitions the workflow into `human_review`

### Current planner audit precedent

The `plan` command already runs a second-pass audit over generated work orders.
That audit can:

- confirm work orders unchanged
- modify weak work orders
- add missing work orders

This is useful precedent for an execution audit phase, but execution audit must
produce bounded remediation work, not re-plan the whole feature.

### Current work order model

Work orders currently support these types:

- `new_feature`
- `bug_fix`
- `refactor`
- `schema_change`
- `docs`
- `bootstrap`

There is already an optional `audit_source` field, but it is used by the plan
audit pass and should not be overloaded for execution audit provenance.

## Problem Statement

The current system has two missing capabilities:

1. It cannot distinguish between:
   - "the current workflow is acceptable as-is"
   - "the current workflow is close, but needs a narrow follow-up run"
   - "the current workflow needs human judgment because the issues are too
     broad or ambiguous for automated remediation"

2. It cannot turn verification findings into a fresh, machine-generated work
   order with preserved lineage and independent metrics.

The result is that human reviewers must manually translate verify findings into
the next work order, and the system loses structured information about that
transition.

## Settled Product Decisions

These decisions should be treated as fixed for this slice.

### 1. Add a first-class `audit` phase after `verify`

The execution pipeline becomes:

- `scope`
- `build`
- `verify`
- `audit`
- `human_review`

### 2. Do not add an automatic retry loop in this slice

The audit phase must not automatically kick the same workflow back to `scope`
or `build`.

### 3. Audit may generate a fresh follow-up work order

When audit determines that the implementation is not complete or contains
bounded defects, it may generate a new work order artifact intended for a
separate future run.

### 4. Human review remains the final gate

The workflow still ends in `human_review`. This spec does not remove or replace
manual approve/reject.

### 5. Do not auto-run audit-generated work orders

Generating a follow-up work order is required. Automatically executing it is
out of scope for this slice.

### 6. Keep remediation work narrow

Execution audit must generate at most one follow-up work order in this slice.

If the issues are too broad, too numerous, or too ambiguous for one narrow
follow-up work order, audit must return `NEEDS_HUMAN` and emit no follow-up
work order.

### 7. Do not introduce a new work order type for this slice

Execution audit should continue using existing work order types. It must not
introduce a new `remediation` type.

In most cases, audit-generated follow-up work orders will be `bug_fix`, but
other existing types may be used if they are clearly more accurate.

### 8. Add explicit lineage for audit-generated follow-up work

Audit-generated follow-up work orders must be traceable back to the originating
workflow.

This must use a dedicated provenance field, not the existing planner-only
`audit_source` field.

### 9. Audit failures should degrade to human review, not hard-fail the workflow

If execution audit cannot complete after its own retries, the workflow should
still reach `human_review` with a clear audit failure summary rather than being
hard-failed outright.

### 10. `source_workflow_id` should validate as a UUID string only

Work order validation should treat `source_workflow_id` as:

- optional for normal work orders
- required for audit-generated follow-up work orders
- syntactically valid only if it parses as a UUID

Validation must not require the referenced workflow to exist in the current DB
at YAML parse time. A follow-up work order may be moved across machines or
project DBs before it is executed.

### 11. Settle the audit decision matrix now

The audit phase must use exactly these meanings:

- `PASS`
  - no follow-up work order
  - `recommended_human_action = approve`
- `FOLLOW_UP_REQUIRED`
  - exactly one follow-up work order is generated
  - `recommended_human_action = reject_and_run_follow_up`
- `NEEDS_HUMAN`
  - no follow-up work order is generated
  - `recommended_human_action = reject`

Audit model failures after retries must be represented as:

- `status = NEEDS_HUMAN`
- one issue with category `audit_failure`
- no follow-up work order

### 12. Settle exact workflow state naming for this slice

Use these workflow state transitions for normal execution:

- `pending`
- `scope_complete`
- `build_complete`
- `verify_complete`
- `human_review`
- `completed`
- `failed`

`audit` does not need a durable intermediate workflow state for this slice.
The audit task itself is sufficient to represent that in-progress phase.

### 13. Settle the audit task input artifact now

The `audit` task should use the verify report path as its `input_artifact`.

The audit worker can resolve the workflow and load any additional artifacts it
needs, including:

- original work order
- context package
- build stdout/stderr
- git diff

## Desired End State

After this slice:

- every execution run that reaches the end of `verify` also passes through
  `audit`
- audit produces a structured audit report artifact
- audit records its own timings, model, token usage, and result on
  `pipeline_runs`
- audit may generate a follow-up work order artifact
- generated follow-up work orders carry provenance linking them to the original
  workflow
- a follow-up work order can be run later as a fresh pipeline execution with
  separate metrics
- CLI/session/status/stats surfaces expose the audit result, follow-up artifact
  path, and rich audit observability metrics

## Functional Requirements

### Execution phase sequencing

The execution pipeline must change as follows:

- `build` still creates `verify`
- `verify` no longer transitions directly to `human_review`
- `verify` writes its report and transitions to the non-terminal workflow state
  `verify_complete`
- `verify` then creates an `audit` task
- `audit` is responsible for transitioning the workflow into `human_review`

The worker must recognize `audit` as a valid task phase.

The new audit task should be created with:

- `phase = "audit"`
- a dedicated `task_type` such as `audit`
- `input_artifact = <verify report path>`
- a retry budget appropriate for transient model/parse failures

### Verify behavior changes

`verify` should remain the phase that produces structured verification
evidence. It should continue to:

- run deterministic pre-checks
- run segmented LLM review
- produce the existing `VerificationReport`
- store verify metrics

Important change:

- if `verify` successfully produces a report, it must enqueue `audit` even when
  the verify result is `FAIL`
- the current "all pre-checks failed" path must no longer short-circuit past
  audit if a verify report was successfully written

Task completion semantics for `verify` in this slice:

- if `verify` successfully writes a verify report, the verify task should be
  marked completed regardless of whether the report status is `PASS`, `WARN`,
  or `FAIL`
- `verify` should use retryable task failure only for transient execution
  failures that prevent a verify report from being produced at all
- `verify` should no longer use `NeedsHuman` merely because the verify result is
  negative

### Audit inputs

The new `audit` phase must receive enough context to decide whether:

- the implementation is acceptable as-is
- a narrow follow-up work order should be created
- human judgment is required

At minimum, audit should consume:

- the original work order
- the scope context package
- the git diff against the base branch
- the structured verification report
- verify pre-check results, either via the verify report or equivalent
- build stdout/stderr artifact paths, and optionally truncated content if
  needed for model input
- workflow metadata such as workflow ID, branch name, and project path

### Audit output contract

Audit must produce a structured JSON report with a stable schema.

The report should include:

- `status`: `PASS | FOLLOW_UP_REQUIRED | NEEDS_HUMAN`
- `summary`: short, objective summary
- `recommended_human_action`: `approve | reject | reject_and_run_follow_up`
- `issues`: array of structured findings
- `follow_up_work_order`: optional, present only when `status` is
  `FOLLOW_UP_REQUIRED`

Each issue entry should include at least:

- severity
- category
- evidence
- affected files when known

Suggested issue categories:

- `missing_acceptance_criterion`
- `failed_precheck`
- `scope_drift`
- `bug`
- `test_gap`
- `pattern_violation`
- `audit_failure`

The report schema should be concrete enough that implementation does not invent
its own shape. The desired baseline contract is:

```json
{
  "status": "PASS",
  "summary": "Implementation satisfies the work order with no follow-up required.",
  "recommended_human_action": "approve",
  "issues": [
    {
      "severity": "medium",
      "category": "test_gap",
      "evidence": "Acceptance criterion for regression coverage is only partially satisfied.",
      "affected_files": ["internal/foo/bar_test.go"]
    }
  ],
  "follow_up_work_order": null
}
```

Schema rules:

- `severity` should be one of `low | medium | high`
- `affected_files` must be an array and may be empty
- `follow_up_work_order` must be `null` unless `status` is
  `FOLLOW_UP_REQUIRED`
- if `status` is `FOLLOW_UP_REQUIRED`, `follow_up_work_order` must be a full,
  valid work order object including `source_workflow_id`
- audit must output JSON only, with no markdown

### Follow-up work order generation

When audit emits a follow-up work order:

- it must be a valid work order according to the normal work order schema
- it must be narrow and bounded
- it must address only unmet criteria or concrete defects found in the current
  workflow
- it must not restate or re-scope the full original feature unless that full
  re-scope is truly required, in which case audit should return `NEEDS_HUMAN`
  instead
- it must include explicit constraints that keep the work focused on audit
  findings
- it must include provenance linking it to the source workflow

The follow-up work order should prefer:

- exact known files implicated by verify/audit findings
- acceptance criteria that are directly testable
- constraints such as "Do not broaden the implementation beyond the listed
  audit findings"

The follow-up work order should also follow these conventions:

- title format should start with `Follow-up:` or `Fix audit findings:`
- it should generally use `type: bug_fix` unless another existing type is
  clearly more accurate
- `known_files` should be limited to files directly implicated by the audit
  findings, not copied wholesale from the original work order
- acceptance criteria should restate only unmet criteria or concrete defects
- constraints should explicitly forbid re-expanding the scope to the full
  original feature

The follow-up work order should be concrete enough that the planner does not
need to infer its shape. A baseline YAML example is:

```yaml
title: "Fix audit findings: add missing regression coverage for health endpoint"
type: bug_fix
target_module: internal/health
reference_module: internal/status
source_workflow_id: "123e4567-e89b-12d3-a456-426614174000"
known_files:
  - internal/health/handler.go
  - internal/health/handler_test.go
acceptance_criteria:
  - "Regression test covers the previously missed error-path behavior"
  - "GET /health continues to return 200 with the expected JSON response"
  - "go test ./... passes"
constraints:
  - "Address only the audit findings from source workflow 123e4567-e89b-12d3-a456-426614174000"
  - "Do not broaden the implementation beyond the listed audit findings"
  - "Follow the existing test patterns already used in internal/health"
```

### Provenance and lineage

This slice must add a dedicated optional work order field:

- `source_workflow_id`

Requirements:

- `source_workflow_id` is optional for normal manually-authored work orders
- `source_workflow_id` is required on audit-generated follow-up work orders
- `source_workflow_id` must reference the workflow that produced the audit
- `audit_source` remains planner-only and must not be reused for execution
  lineage

Validation requirements:

- `source_workflow_id` must be ignored when empty
- if present, it must parse as a UUID
- validation must not query the DB to confirm existence
- the field must round-trip through YAML and JSON forms of the work order model

When `conductor run` starts from a work order containing `source_workflow_id`,
the new workflow and pipeline run should preserve that lineage in persistence.

### Persistence and schema

The following persistence additions are required.

#### Workflows

Add fields needed to persist execution-audit state, at minimum:

- `audit_report_path`
- `source_workflow_id`

`audit_report_path` should behave similarly to `verification_report_path`:

- nullable until audit writes a report
- visible in workflow status output
- stored on the workflow row for easy lookup

#### Pipeline runs

Add execution-audit metrics and lineage fields, at minimum:

- `audit_started_at`
- `audit_completed_at`
- `audit_model`
- `audit_tokens_in`
- `audit_tokens_out`
- `audit_result`
- `audit_follow_up_generated`
- `source_workflow_id`

If implementation discovers a better exact column split, it is acceptable as
long as audit timing, model, token usage, result, follow-up generation, and
lineage are all queryable.

#### Artifacts

Add new artifact types for at least:

- `audit_report`
- `audit_generated_work_order`

If raw audit model output is persisted for debugging, use a distinct artifact
type rather than overloading the parsed report artifact.

Artifact path conventions should be settled for this slice:

- audit reports:
  - `<dataDir>/artifacts/audit-reports/<workflow-id>-audit-report.json`
- audit-generated follow-up work orders:
  - `<dataDir>/artifacts/work-orders/<workflow-id>-audit-follow-up.yaml`
- optional raw audit output:
  - `<dataDir>/artifacts/audit-raw/<workflow-id>-audit-raw.txt`

### Audit prompt and model routing

Add a dedicated execution-audit prompt surface.

At minimum:

- add `prompts.audit`
- add a default embedded audit prompt
- add a model role for audit such as `audit`

The default project template and documentation must be updated accordingly.

The audit prompt should be explicitly instructed to:

- prefer no follow-up work order over a broad one
- emit `NEEDS_HUMAN` when the necessary remediation exceeds one narrow work
  order
- preserve exact file paths and acceptance criteria wording where possible
- generate YAML-compatible work order content with `source_workflow_id`
- avoid re-planning the full feature

### Task creation and retry behavior

`verify` should create the `audit` task after its report is successfully
written.

`audit` should have its own retry budget for transient failures such as:

- model invocation failures
- parse failures
- temporary IO issues

After audit retries are exhausted:

- the workflow should still transition to `human_review`
- an audit result representing failure-to-audit should be persisted
- no follow-up work order should be generated

Task completion semantics for `audit` in this slice:

- if audit successfully produces a parsed audit report, the audit task should be
  marked completed regardless of whether the report status is `PASS`,
  `FOLLOW_UP_REQUIRED`, or `NEEDS_HUMAN`
- if audit exhausts retries but the implementation can still persist a fallback
  audit report, the audit task should still be marked completed and the
  workflow should transition to `human_review`
- hard task failure should be reserved for situations where audit cannot even
  persist its fallback outcome

### Human review behavior

This slice does not remove manual human decision-making.

The expected human behavior is:

- if audit result is `PASS`, the workflow is a normal candidate for approval
- if audit result is `FOLLOW_UP_REQUIRED`, the workflow should usually be
  rejected and the generated follow-up work order should be considered for a
  separate fresh run
- if audit result is `NEEDS_HUMAN`, the reviewer decides the next step manually

Approve/reject command semantics do not need to be redesigned in this slice
unless a minimal guardrail is easy to add without broad CLI churn.

For planning purposes, assume:

- `approve` remains allowed even when audit recommends follow-up
- no hard approval block is required in this slice
- surfacing the audit recommendation clearly in CLI/status output is sufficient

### Minimal CLI and status surfacing

This slice should expose audit outputs in existing local surfaces without
requiring a new frontend effort.

At minimum, the user should be able to inspect:

- audit result
- audit summary
- whether a follow-up work order was generated
- the artifact path for the generated follow-up work order

Good targets for this are:

- `conductor session <id>`
- `conductor status`
- artifact listings tied to a session or workflow

The minimum expected output additions are:

- `conductor status <workflow>` should show:
  - audit report path
  - audit result
  - whether a follow-up work order exists
- `conductor session <session>` should show for execution runs:
  - audit result
  - source workflow linkage when present
  - audit-generated follow-up work order artifact paths
- `conductor stats` should gain at least a lightweight audit summary section
  rather than remaining verify-only

### `conductor stats` observability requirements

`conductor stats` should be treated as a first-class observability surface for
this slice, not a minimal afterthought.

The goal is to let an operator understand not just whether execution audit
exists, but how often it triggers, how expensive it is, how often it generates
follow-up work, and how those follow-ups relate to later outcomes.

At minimum, `conductor stats` should gain audit-aware sections covering all of
the following where data is available.

#### Audit outcome counts

Include aggregate counts for:

- total runs with audit executed
- audit `PASS`
- audit `FOLLOW_UP_REQUIRED`
- audit `NEEDS_HUMAN`
- audit failures represented through fallback audit reports

#### Audit recommendation counts

Include aggregate counts for:

- recommended human action `approve`
- recommended human action `reject`
- recommended human action `reject_and_run_follow_up`

#### Audit follow-up generation metrics

Include aggregate counts for:

- runs that generated a follow-up work order
- runs that did not generate a follow-up work order
- follow-up generation rate among audited runs

#### Audit timing metrics

Include timing metrics similar in spirit to existing phase timing metrics:

- average audit duration
- median audit duration if practical
- slowest recent audit duration if practical

If median/percentile style metrics are not practical in this slice, average
plus recent-run visibility is sufficient.

#### Audit token and cost metrics

Include aggregate metrics for:

- total audit tokens in
- total audit tokens out
- total audit estimated cost or direct cost, depending on how audit usage is
  recorded
- average audit tokens per run if practical

If audit uses `sub_calls`, stats should incorporate those values rather than
silently omitting them.

#### Cross-phase phase metrics

The phase-level metrics view should expand beyond scope/build/verify so an
operator can compare all execution phases:

- average scope duration
- average build duration
- average verify duration
- average audit duration
- token totals for scope/build/verify/audit
- cost totals for build and audit, plus other phases if available

#### Audit-to-human agreement metrics

Just as current stats compare verify and human outcomes, this slice should
additionally compare audit recommendations against eventual human actions.

At minimum, surface counts for:

- audit `PASS` later approved
- audit `PASS` later rejected
- audit `FOLLOW_UP_REQUIRED` later approved
- audit `FOLLOW_UP_REQUIRED` later rejected
- audit `NEEDS_HUMAN` later approved
- audit `NEEDS_HUMAN` later rejected

If possible, include an aggregate audit/human agreement percentage.

#### Follow-up lineage metrics

Because this slice introduces audit-generated follow-up work orders, stats
should surface lineage-aware counts where practical.

At minimum, include:

- number of runs started from a work order with `source_workflow_id`
- number of original runs that produced a follow-up work order
- recent pairs of `source_workflow_id -> new workflow_id` when a follow-up was
  later executed, if practical

If full lineage rollups are too much for this slice, aggregate counts are still
required.

#### Recent run visibility

The recent runs section should become audit-aware. For recent execution runs,
include where practical:

- workflow ID
- work order type
- verify result
- audit result
- human result
- whether a follow-up work order was generated
- whether the run itself originated from `source_workflow_id`
- total tokens across all available phases

The display does not need to become perfect in this slice, but audit data must
be visible without opening another command.

#### Preserve existing stats surfaces

Existing `conductor stats` sections should not be removed without replacement.

Current surfaces such as:

- verify result summaries
- human result summaries
- verify/human agreement
- work-order-type breakdown
- scope quality metrics
- plan audit effectiveness
- sub-call summaries

should remain available unless there is a strong reason to evolve their exact
presentation.

#### Query strategy for stats

The implementation should prefer additive, dedicated audit/lineage query
surfaces over turning the existing summary queries into overly broad
all-purpose queries.

Specifically:

- keep existing summary-oriented queries such as the current equivalents of
  `GetPipelineStats` and `GetRecentPipelineRuns` conceptually lean
- only extend those existing queries where phase-parity additions are natural,
  such as:
  - average audit duration
  - aggregate audit token totals
  - aggregate audit cost totals
  - audit result field visibility in recent runs
- add dedicated audit-focused queries for richer observability needs such as:
  - audit outcome counts
  - audit recommendation counts
  - follow-up generation counts
  - audit-to-human agreement
  - follow-up lineage and re-run counts
  - recent audit-aware run listings if that becomes too wide for the existing
    recent-runs query

Rationale:

- narrower queries are easier to test
- audit observability can evolve without destabilizing core run summaries
- future API/UI work can reuse dedicated audit readers cleanly
- this avoids creating fragile "god queries" in `sql/queries.sql`

### Metrics and observability

Audit should be visible as a first-class execution phase in telemetry.

At minimum:

- `pipeline_runs` must store audit timing and token metrics
- `sub_calls` should store audit LLM usage if audit uses the shared sub-call
  recording model
- workflow/session artifact listings should include audit artifacts
- the documented pipeline phase order should be updated everywhere that still
  says `scope -> build -> verify -> human_review`
- `conductor stats` should surface audit metrics as comprehensively as the
  available persistence allows, rather than treating audit as a side note

If the existing event vocabulary is maintained, it should be extended or reused
for audit rather than creating a second overlapping event model.

The minimum event additions should be:

- `audit_result`
- `audit_follow_up_generated`

Existing generic events such as `phase_start`, `phase_complete`, and
`phase_error` should also cover the new `audit` phase.

## Non-Goals

The following are explicitly out of scope for this spec:

- automatic re-entry of the same workflow into `scope` or `build`
- automatic execution of audit-generated follow-up work orders
- generation of multiple follow-up work orders from a single audit pass
- redesign of the entire human approval model
- major observability UI work
- planner behavior changes beyond keeping planner audit and execution audit
  clearly separate
- replacing the existing verify report schema

## Technical Constraints

- preserve the existing scope and build behavior unless required for audit
  integration
- preserve the existing `VerificationReport` contract
- do not overload planner-only `audit_source` for execution lineage
- keep audit-generated follow-up work orders compatible with normal
  `conductor run`
- keep the work order format human-editable YAML
- avoid introducing a new work order type unless a real blocker is discovered
- keep the current SQLite/sqlc model coherent; DB migrations must upgrade older
  project DBs cleanly
- keep audit additive to the existing pipeline rather than replacing verify

## Validation Expectations

The implementation derived from this spec should be validated with both
automated and manual checks.

### Automated

At minimum:

- tests for work order validation with optional `source_workflow_id`
- tests for DB migrations and sqlc-backed queries covering new audit fields
- tests proving `verify` now enqueues `audit`
- tests proving the worker handles `audit` phase correctly
- tests proving successful audit writes an audit report artifact
- tests proving follow-up work order generation writes a valid YAML artifact
- tests proving audit failure degrades to `human_review` rather than hard
  failing the workflow
- tests proving a fresh run initialized from a follow-up work order preserves
  `source_workflow_id`
- tests for template/config loading of the new `prompts.audit` field and
  `models.roles.audit`
- tests for status/session/stats readers that now surface audit information
- tests for `conductor stats` sections or backing queries covering audit
  outcomes, audit timing, audit token usage, and follow-up generation counts
- tests for any newly introduced dedicated audit/lineage DB queries
- existing verify, session, artifact, and stats tests remain green or are
  updated intentionally

### Manual

At minimum:

1. run a normal work order that should pass verify cleanly
2. confirm the workflow executes `scope -> build -> verify -> audit ->
   human_review`
3. inspect the audit report artifact and confirm `PASS`
4. run a work order that intentionally fails a criterion or misses required
   work
5. confirm audit generates a narrow follow-up work order artifact
6. run the generated follow-up work order as a fresh execution
7. confirm the new run has its own metrics and preserves lineage back to the
   original workflow
8. inspect `conductor status` and `conductor session` output to confirm audit
   details are visible
9. inspect `conductor stats` to confirm audit outcome, timing, token, cost,
   follow-up generation, and lineage-related metrics are surfaced

## Planning Guidance

When using `conductor plan` against this spec:

- do generate work around:
  - work order provenance field support
  - DB/schema/sqlc updates for execution audit and lineage
  - audit prompt/config/model-role support
  - worker/execution phase sequencing changes
  - audit report generation
  - audit-generated follow-up work order artifact generation
  - dedicated audit/lineage stats queries where richer observability is needed
  - minimal CLI/status/session surfacing
  - README and template updates
  - focused tests for the new phase

- do not generate work around:
  - automatic workflow loops
  - auto-running follow-up work orders
  - broad new frontend/dashboard work
  - multi-work-order audit decomposition
  - replacing verify with audit

Prefer a decomposition where the foundational persistence and schema changes
land before the worker/audit execution logic, and where prompt/config updates
land before integration tests that depend on them.

## Known Risks

- Audit may be tempted to generate follow-up work orders that are too broad.
  The prompt and tests must constrain this aggressively.
- Adding `source_workflow_id` to work orders introduces provenance value but
  touches validation, documentation, templates, and run initialization.
- There is a design temptation to overload planner audit structures for
  execution audit. That should be resisted where the semantics differ.
- If audit failures are not degraded carefully, the system could become less
  usable than the current verify-only flow.
- Existing metrics and stats readers may need targeted updates so audit fields
  are visible and not silently ignored.

## Reference Files

Useful files for planning and implementation context:

- `README.md`
- `work-order.template.yaml`
- `project.yaml`
- `project.template.yaml`
- `cmd/conductor/main.go`
- `cmd/conductor/run.go`
- `cmd/conductor/gate.go`
- `cmd/conductor/status.go`
- `cmd/conductor/session.go`
- `cmd/conductor/plan.go`
- `internal/models/workorder.go`
- `internal/worker/worker.go`
- `internal/worker/scope.go`
- `internal/worker/build.go`
- `internal/worker/verify.go`
- `internal/worker/artifacts.go`
- `internal/verify/orchestrator.go`
- `internal/templates/prompts.go`
- `internal/config/config.go`
- `internal/database/schema.sql`
- `sql/queries.sql`
- `internal/database/workflows.go`
- `internal/database/artifacts.go`
- `internal/database/sessions.go`
- `internal/database/plan_runs.go`
- `internal/pipeline/events.go`
