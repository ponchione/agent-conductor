# Planning and Verification Consistency Spec

## Goal

Make `conductor plan` and the verify pipeline coordinate through an explicit,
machine-verifiable contract instead of freeform English. The result should be:

- better work-order decomposition at plan time
- fewer unverifiable or misleading acceptance criteria
- deterministic verify behavior across projects
- fail-closed handling when planning or audit output is weak

## Problem Statement

Today the pipeline has a contract mismatch:

- planning emits prose `acceptance_criteria`
- build tries to satisfy that prose
- verify infers what it can run by string-matching the prose

That causes repeated failures and false negatives:

- project-specific commands like `make test` are not recognized unless they look
  like hardcoded `go test` strings
- criteria about unchanged behavior or compatibility are often not assessable
  from a git diff alone
- `known_files` shape planning context but do not reliably participate in verify
- plan audit can still allow weak work orders to be written out

## Core Decisions

### 1. Planning and verify will share a typed verification contract

Work orders should stop using string-only acceptance criteria as the system of
record. The end state is structured acceptance criteria with:

- a stable ID
- a human-readable description
- linked requirement IDs
- a typed verification method
- a required/advisory flag

Supported verification kinds in the first implementation:

- `precheck`
- `diff_review`
- `file_compatibility`
- `http_smoke`

### 2. Project-specific verification commands belong in config

Build, test, and vet commands must not be encoded in work-order prose.

Verify should execute configured commands based on typed criteria, not by
searching for substrings in English text.

### 3. `known_files` becomes reference context, not proof

`known_files` should remain useful, but its meaning must be explicit:

- at plan time, it identifies files the agent should read or is likely to edit
- at verify time, it provides reference files that may be loaded even if they
  are unchanged in the diff

`known_files` alone does not satisfy a criterion. It only expands the evidence
available to verify, especially for compatibility checks.

### 4. Verify needs tri-state criterion outcomes

Each criterion result should be:

- `met`
- `unmet`
- `unassessable`

Pipeline status stays `PASS | WARN | FAIL`, but criterion-level semantics are
separate from workflow-level release semantics.

### 5. Plan audit must fail closed

If audit is enabled and returns unusable output, planning should error by
default instead of silently falling back to weak unaudited work orders. Any
unsafe fallback must be explicit.

## Contract Surfaces

### Work-order schema versions

The work-order file format must become explicitly versioned.

- `schema_version: 1` means the current repository format with string-only
  `acceptance_criteria`
- `schema_version: 2` means the new typed verification contract

`schema_version` is required for new files once version 2 lands. Until then:

- planner internals may produce version-2-shaped data
- on-disk work orders may still be written as version 1 during Phase 1
- verify must continue to accept version 1 during the transition window

### Canonical version 2 work-order shape

Version 2 extends the current work-order structure rather than replacing the
human-readable acceptance criteria field outright.

Canonical YAML shape:

```yaml
schema_version: 2
title: "Preserve observability asset routing during chi migration"
type: refactor
target_module: internal/api
reference_module: internal/http
known_files:
  - internal/api/server.go
  - internal/api/server_test.go
requirements:
  - id: REQ-1
    text: Static assets remain reachable after the router migration
    source: spec
acceptance_criteria:
  - id: AC-1
    description: GET /assets/observability.css returns 200
    requirement_ids: ["REQ-1"]
    required: true
    verification:
      kind: http_smoke
      route: observability_assets
constraints:
  - "Do not change unrelated API behavior"
audit_source: modified
```

Rules:

- `title`, `type`, `target_module`, and `constraints` retain their current
  meaning
- `requirements` is work-order-local traceability metadata written by planner
  and audit
- `acceptance_criteria[].description` remains the human-readable reviewer-facing
  sentence
- `acceptance_criteria[].verification` is the machine-verifiable contract
- `requirement_ids` must reference IDs present in `requirements`
- `audit_source` remains optional metadata from plan audit

### Canonical acceptance-criterion schema

Each `acceptance_criteria[]` item in version 2 must include:

- `id`: unique within the work order
- `description`: human-readable statement of what must be true
- `requirement_ids`: one or more requirement IDs covered by the criterion
- `required`: boolean
- `verification`: typed execution or assessment contract

Optional fields:

- `notes`: planner or auditor guidance for reviewers
- `criticality`: `normal | release_blocking`

Validation rules:

- at least one acceptance criterion is required
- criterion IDs must be unique within the work order
- every criterion must reference at least one requirement ID
- every requirement must be covered by at least one criterion
- a criterion may cover multiple requirements only if the same verification
  evidence is sufficient for all of them

### Verification-kind schemas

#### `precheck`

Used for deterministic project commands such as build, test, and vet.

```yaml
verification:
  kind: precheck
  check: test
```

Fields:

- `check`: logical command key, for example `build`, `test`, `vet`, `lint`

#### `diff_review`

Used when a criterion can be assessed from changed files plus structured review
context.

```yaml
verification:
  kind: diff_review
  focus:
    - internal/templates/prompts.go
    - cmd/conductor/plan.go
```

Fields:

- `focus`: optional files or directories that should be emphasized in review

#### `file_compatibility`

Used for unchanged-behavior or compatibility claims that require loading stable
reference files outside the diff.

```yaml
verification:
  kind: file_compatibility
  subject: cmd/conductor/serve.go
  expectation: public_flags_unchanged
```

Fields:

- `subject`: primary file or module whose compatibility is being asserted
- `expectation`: stable compatibility contract name

#### `http_smoke`

Used for deterministic HTTP checks run against a temporary app instance or test
harness.

```yaml
verification:
  kind: http_smoke
  route: observability_assets
```

Fields:

- `route`: logical smoke-check ID defined in project config

The criterion should not inline raw port numbers, base URLs, or setup logic.
Those belong in verify config.

### Version 1 compatibility rules

During the migration window, version 1 work orders remain valid.

When verify receives version 1:

- it may still run legacy string-matched prechecks for compatibility
- it should record that the criterion contract is legacy
- it should not infer new verification kinds from prose beyond the existing
  compatibility shim

Planner behavior by phase:

- Phase 1: internal planner output may use version-2-shaped data, but emitted
  YAML may remain version 1 if the rest of the pipeline cannot yet consume
  version 2
- Phase 2: planner and audit emit version 2 by default
- after Phase 2 stabilization, version 1 generation should be removed

## Config Schema

### Planner prompt config

Project config must add:

```yaml
prompts:
  plan: templates/plan-prompt.md
  plan_audit: templates/plan-audit.md
```

These fields should load the same way existing prompt paths load today.

### Verify command config

Project config must add:

```yaml
verify:
  commands:
    build:
      argv: ["make", "build"]
      workdir: .
      timeout_seconds: 120
    test:
      argv: ["make", "test"]
      workdir: .
      timeout_seconds: 300
  smoke:
    observability_assets:
      command:
        argv: ["go", "test", "./internal/api", "-run", "TestSmokeAssets"]
        timeout_seconds: 180
```

Rules:

- `commands` maps logical precheck names to executable command definitions
- `argv` is required and must be an exact argv array, not a shell string
- `workdir` is optional and defaults to the project root
- `timeout_seconds` is optional and defaults to a conservative global timeout
- environment-variable injection may be added later, but is not required for the
  first typed-contract implementation

### HTTP smoke execution model

`http_smoke` does not directly encode how to boot the app. Instead it references
project-defined smoke checks.

First implementation rules:

- smoke checks are config-addressable named routines under `verify.smoke`
- each smoke routine is executed by a deterministic command, not by ad hoc LLM
  reasoning
- the command may be a focused test target or a dedicated smoke binary
- if the smoke routine is missing from config, the criterion is invalid

This keeps HTTP verification project-specific without baking app-start logic
into the work order itself.

## Requirement and Traceability Model

### Requirement IDs

Planner extracts requirements from the input spec and assigns stable IDs in the
form `REQ-1`, `REQ-2`, and so on within that planning run.

Rules:

- planner audit may rewrite work-order decomposition, but it must preserve
  requirement IDs once they are assigned
- new requirements discovered by audit may be added with new IDs
- planner artifacts should persist the extracted requirement list even if the
  on-disk work order format is still version 1

### Planning metadata

Each internal planner work order should include:

- `covers`: requirement IDs addressed by the work order
- `depends_on`: prior work-order IDs or titles
- `why_now`: ordering rationale
- `size`: `S | M | L`

This metadata may remain internal during Phase 1 if the on-disk YAML does not
yet carry it.

## Verify Result Semantics

### Criterion result schema

Version-2 verify results should no longer collapse criteria to a boolean.

Canonical shape:

```json
{
  "criterion_id": "AC-1",
  "description": "GET /assets/observability.css returns 200",
  "required": true,
  "result": "met",
  "verification_kind": "http_smoke",
  "notes": "Smoke routine observability_assets exited 0"
}
```

`result` must be one of:

- `met`
- `unmet`
- `unassessable`

### Workflow status derivation

Required criteria:

- any required criterion with result `unmet` yields overall `FAIL`
- if no required criteria are `unmet` and at least one required criterion is
  `unassessable`, overall status is `WARN`
- if all required criteria are `met`, overall status is at least `PASS`

Advisory criteria:

- advisory `unmet` yields at least `WARN`
- advisory `unassessable` may remain `WARN` if the requirement is genuinely
  non-blocking
- advisory criteria can never independently produce `FAIL`

If both required and advisory issues exist, the more severe workflow status
wins.

## `known_files` Loading Rules

Verify may load unchanged files as reference context, but the loading boundary
must be deterministic.

Rules:

- `diff_review` should prioritize changed files and may optionally load files
  listed in `focus`
- `file_compatibility` may load the declared `subject` plus up to a bounded
  subset of relevant `known_files`
- `known_files` should be capped by count and total bytes using repository
  config limits similar to existing context-packaging limits
- if a criterion needs a reference file not in the diff, it should prefer to
  name that file directly in the verification contract

## End-State Pipeline Behavior

### Planning

`conductor plan` should:

- load planner prompts from config via `prompts.plan` and `prompts.plan_audit`
- build a richer planning context that includes:
  - the original spec
  - a canonical work-order template
  - project facts and conventions
  - curated existing-system context from key files
  - optional relevant docs or excerpts
- ask the planner to reason in this order:
  1. extract requirements
  2. extract constraints and non-goals
  3. identify the existing system
  4. identify gaps
  5. sequence work orders
  6. map work orders to requirement IDs
- emit a richer internal planner response with:
  - `requirements`
  - `non_goals`
  - `existing_system`
  - `work_orders`
  - `planning_warnings`

### Audit

The audit prompt should output a complete corrected plan, not a conservative
patch. Audit may:

- delete
- split
- merge
- replace
- reorder

Audit must verify:

- requirement coverage
- verifiability of every acceptance criterion
- realistic scope and file references
- dependency ordering
- overlap between work orders
- preservation of spec constraints

### Build

The build phase should continue to consume the work order, but it should also
persist structured evidence about what it actually ran:

- commands executed
- exit codes
- targeted packages or paths
- smoke checks performed

Verify can ingest that evidence as supporting context, while deterministic reruns
remain the source of truth when available.

### Verify

Verify should stop relying on diff-only reasoning for every criterion.

Instead, per criterion it should choose the declared verification method:

- `precheck`: run configured project commands
- `diff_review`: assess against changed files and structured review context
- `file_compatibility`: load unchanged reference files named in the criterion or
  `known_files`
- `http_smoke`: run deterministic project-defined smoke routines

## Validation Rules

Plan-time validation should reject work orders with:

- empty acceptance criteria
- criteria with no verification method in version 2
- missing requirement coverage
- duplicate or heavily overlapping work orders
- impossible `known_files`
- invalid dependency ordering
- oversized work orders
- unverifiable compatibility claims
- invalid `schema_version`

Verify-time validation should reject or warn on:

- unknown verification kinds
- missing configured commands for a referenced precheck
- missing configured smoke routines for a referenced `http_smoke`
- criteria that cite files outside the available diff plus reference context
- invalid result values in verifier output

## Delivery Phases

### Phase 1: planner hardening on the current pipeline

- add `prompts.plan` and `prompts.plan_audit`
- move plan prompt text out of hardcoded constants
- build a dedicated planning context builder
- expand the internal planner and audit response schema
- persist requirement extraction and planning metadata as artifacts
- harden validation for current generation quality
- make audit fail closed

Phase 1 uses version 1 on-disk work orders unless the downstream pipeline is
updated first.

### Phase 2: typed verification contract

- add `schema_version` support to work-order parsing
- add `verify.commands` and `verify.smoke` to project config
- migrate work orders from string-only criteria to structured criteria
- update build and verify to consume the structured schema
- support tri-state criterion results
- make `known_files` available as verify reference context

### Phase 3: deterministic verification expansion

- add project-defined HTTP smoke routines
- persist structured build evidence
- extend verifier coverage beyond diff-only review for compatibility claims
- remove version-1 planner output once version 2 is stable

## Definition of Done

The work is complete when:

- planner prompts are configurable and no longer hardcoded in `plan.go`
- plan output traces work orders back to stable requirement IDs
- audit can fully rewrite bad decompositions and fails closed on unusable output
- project-specific verify commands come from config
- work orders carry typed, machine-verifiable acceptance criteria
- verify reports `met | unmet | unassessable` per criterion and derives
  `PASS | WARN | FAIL` consistently
- advisory criteria cannot independently fail a workflow
- unchanged reference files can participate in compatibility verification
- weak or unverifiable work orders are rejected before they are written to
  `/work-orders`
