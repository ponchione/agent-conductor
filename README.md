# Agent Conductor

A pipeline orchestration system for AI-assisted code generation. Local models handle analysis and planning; Claude Code handles implementation. Human review is the final gate.

The core idea: prepare, direct, and verify a smart agent rather than micromanage a dumb one. The conductor assembles rich, task-specific context through a multi-step analysis pipeline, hands it to Claude Code as a structured JSON directive, then independently verifies the result.

## How It Works

```
conductor run --project project.yaml work-order.yaml
```

The conductor runs a three-phase pipeline:

**Scope** — A fan-out/fan-in pipeline that decomposes the work order into investigation targets, gathers context for each target (files, signatures, RAG results), analyzes each area independently, identifies cross-cutting concerns, then synthesizes everything into a single structured context package. All analysis runs on local LLMs — no tokens leave your machine.

**Build** — Passes the context package to Claude Code, which creates a git branch, implements the changes, and commits. The conductor stays out of its way.

**Verify** — A two-step pipeline that segments the git diff, analyzes each segment against the work order's acceptance criteria, runs deterministic configured checks (`precheck`, `http_smoke`, and bounded `file_compatibility` reference loading), then synthesizes a final PASS/WARN/FAIL verdict. Analysis runs on local LLMs.

The workflow then sits in `human_review` until you approve or reject it.

## The Scope Pipeline

The scope phase runs six steps in a fan-out/fan-in pattern:

```
PreScope (Go)  →  Decompose (1 LLM call)  →  Gather (Go)  →  Analyze (N LLM calls)  →  CrossCut (1 LLM call)  →  Synthesize (1 LLM call)
```

**PreScope** gathers deterministic context with no LLM involvement: a glob-filtered indented file tree (capped at `max_tree_lines`) and project conventions if configured.

**Decompose** receives the work order and pre-scope context, then identifies 2–6 investigation targets — directories or packages in the codebase that need examination. Each target is classified as `primary_modification`, `supporting_modification`, or `reference_only`.

**Gather** reads the actual files for each target, extracts function and type signatures, and runs RAG search filtered to the target's path prefix. This gives each analyze call concrete code to work with rather than just a file tree.

**Analyze** runs one LLM call per target. Each call receives the work order, the target's files and signatures, and relevant RAG chunks. It identifies which files to modify, which to reference, what to create, interfaces to preserve, and concerns specific to that area.

**CrossCut** receives all per-target analyses and identifies shared types, cross-target dependencies, integration risks, and a suggested modification order. This step is non-fatal — if it fails, the pipeline continues without it.

**Synthesize** merges all analyses, cross-cut results, and conventions into a single deduplicated context package: files to modify, files to reference, new files, SQL files, dependencies, build instructions, and an estimated complexity rating.

**Post-Scope Validation** then checks every path against the filesystem. Non-existent paths are stripped. New files that already exist are reclassified to `files_to_modify`. If more than 50% of paths are invalid, the scope phase retries.

## The Verify Pipeline

The verify phase follows the same fan-out/fan-in pattern:

```
Segment (Go)  →  Analyze (N LLM calls)  →  Synthesize (1 LLM call)
```

**Segment** splits the unified diff into logical groups, pairing source files with their test files (e.g., `foo.go` and `foo_test.go` land in the same segment).

**Pre-Checks** run deterministically before the LLM. Work orders name exact configured commands under `verify.commands` and smoke routines under `verify.smoke`. If all deterministic checks fail, the LLM evaluation is skipped entirely and the report is set to FAIL.

**Analyze** runs one LLM call per diff segment, assessing alignment with the work order, checking acceptance criteria relevant to that segment, and flagging bugs, style issues, and concerns.

**Synthesize** merges deterministic check results and per-segment verdicts into a final verification report with tri-state criterion outcomes (`met`, `unmet`, `unassessable`), centralized PASS/WARN/FAIL status derivation, scope drift detection, completeness assessment, and pattern consistency check.

## Planning

Turn a freeform spec into executable work orders:

```
conductor plan spec.md --output ./work-orders/
```

The plan command reads any text format and calls Claude Code to produce a hierarchical `plan.yaml` manifest of dependency-ordered epics and tasks. Each task is sized for a single conductor run (1–3 files changed).

For existing projects, pass `--project` to ground the plan in the actual codebase:

```
conductor plan spec.md --project project.yaml --output ./work-orders/
```

The planner receives the project's file tree and conventions, producing work orders that reference real paths and follow existing patterns.

For greenfield projects, omit `--project`. The planner operates from the spec alone. If the plan command can't parse Claude's response on the first try, it retries with the parse error as correction feedback.

### Audit Pass

After generation, the plan command runs a second LLM pass that audits the assembled manifest against the original spec. The audit checks for completeness, correctness, and proper scoping. It can add missing tasks within an existing epic, modify existing tasks, or confirm them unchanged. Each changed task is tagged with an `audit_source` field (`added`, `modified`, or omitted for unchanged).

```
Audit: 1 added, 2 modified, 3 unchanged
  - Added missing migration task in epic-002
  - Modified acceptance criteria on API endpoint task
```

To skip the audit pass (faster, fewer tokens):

```
conductor plan spec.md --skip-audit
```

### Plan Metrics

Each plan run records metrics to the `plan_runs` table: spec file, generation/audit model names, epic/task counts, pre/post-audit task counts, audit summary (added/modified/unchanged), and token usage for generation and audit. These are best-effort; a recording failure does not block the plan command.

### Output

Output is a single hierarchical `plan.yaml` manifest:

```
work-orders/
  plan.yaml
```

Run one task from the manifest by canonical task ID:

```bash
conductor run work-orders/plan.yaml --task task-001
```

## Prerequisites

- **Go 1.21+** with CGo support
- **An existing git repository** with at least one commit on main
- **Claude Code** installed and available on PATH
- **NVIDIA GPU** with sufficient VRAM for local models (RTX 4090 or equivalent)

### Local Models

The conductor needs two local model servers:

| Purpose | Default Port | Model | Role |
|---------|-------------|-------|------|
| Reasoning | 8080 | DeepSeek-R1-Distill-Qwen-32B | Scope analysis, verify analysis |
| Embeddings | 8081 | nomic-embed-code | RAG indexing and search |

Both run via llama.cpp's server mode. A Docker Compose setup:

```yaml
services:
  deepseek:
    image: ghcr.io/ggml-org/llama.cpp:server-cuda
    container_name: deepseek-server
    ports:
      - "8080:8080"
    volumes:
      - ./gguf:/models:ro
    deploy:
      resources:
        reservations:
          devices:
            - driver: nvidia
              count: all
              capabilities: [gpu]
    command: >
      --host 0.0.0.0
      --port 8080
      -m /models/DeepSeek-R1-Distill-Qwen-32B-Q4_K_M.gguf
      -c 8192
      --flash-attn on
      --parallel 1
      --gpu-layers 999

  nomic-embed:
    image: ghcr.io/ggml-org/llama.cpp:server-cuda
    container_name: nomic-embed-server
    ports:
      - "8081:8081"
    volumes:
      - ./gguf:/models:ro
    command: >
      --host 0.0.0.0
      --port 8081
      --embeddings
      --pooling last
      -m /models/nomic-embed-code.Q8_0.gguf
      -c 8192
      --flash-attn on
      --parallel 1
      --gpu-layers 999
```

## Build

The binary requires CGo for LanceDB:

```bash
make build
```

This produces `bin/conductor` with the LanceDB shared library path baked in via rpath.

## Setup

### Global Configuration

```bash
conductor init-global
```

Creates `~/source/.conductor/config.yaml` with machine-level defaults for model endpoints, git settings, and embedding config. These apply to all projects and are overridden by project-specific settings.

### Multi-Provider Configuration

The conductor supports multiple LLM providers with role-based routing. Each pipeline step is mapped to a named provider through the `models` config section:

```yaml
models:
  providers:
    local-reasoning:
      type: openai
      endpoint: "http://localhost:8080/v1"
      model: "deepseek-r1"
      timeout_seconds: 120
      temperature: 0.0
      pricing:
        input_per_million: 0.0
        output_per_million: 0.0
    cloud-reasoning:
      type: openai
      endpoint: "https://api.example.com/v1"
      model: "some-model"
      api_key: "${REASONING_API_KEY}"
      timeout_seconds: 60
      max_context_tokens: 32000
      temperature: 0.1
      pricing:
        input_per_million: 0.25
        output_per_million: 1.00

  roles:
    decompose: local-reasoning
    analyze: local-reasoning
    crosscut: local-reasoning
    synthesize: local-reasoning
    describe: local-reasoning
    verify_analyze: local-reasoning
    verify_synthesize: local-reasoning
```

This lets you mix providers — run cheap analysis steps locally while routing synthesis to a more capable cloud model, or vice versa. Environment variables in `api_key` fields are expanded at load time.

Every provider entry must set an explicit `type`, and every runtime role must map to a configured provider.

### Guardrails

Control resource usage per pipeline phase:

```yaml
guardrails:
  max_investigation_targets: 6     # Cap on decompose targets
  max_sub_calls_total: 12          # Max LLM calls across a phase
  phase_timeout_seconds: 300       # Wall-clock timeout per phase
  max_cost_per_phase_usd: 0.50    # Cost ceiling (requires pricing config)
  warn_cost_per_phase_usd: 0.10   # Cost warning threshold
```

### Project Configuration

Create a `project.yaml` in or near your repository:

```yaml
project:
  name: my-project
  path: /absolute/path/to/repo
  language: go

index:
  include:
    - "**/*.go"
    - "**/*.md"
  exclude:
    - "**/vendor/**"
    - "**/.git/**"
    - "**/bin/**"
  max_tree_lines: 200
  max_rag_results: 30
  auto_reindex: false

executor:
  tool: claude-code
  timeout_minutes: 30

prompts:
  plan_epic: templates/plan-epic.md
  plan_task: templates/plan-task.md
  plan_audit: templates/plan-audit.md
  scope_decompose: ""
  scope_analyze: ""
  scope_crosscut: ""
  scope_synthesize: ""
  verify_analyze: ""
  verify_synthesize: ""
  build: ""
  describe: ""
  bootstrap: ""

verify:
  commands:
    build:
      argv: ["make", "build"]
      timeout_seconds: 120
    test:
      argv: ["make", "test"]
      timeout_seconds: 300
  smoke:
    assets:
      command:
        argv: ["make", "smoke"]
        timeout_seconds: 180

safety:
  forbidden_paths: []
  max_files_changed: 50
  max_duration_mins: 60
```

Optional conventions section for projects with structural patterns:

```yaml
conventions:
  module_path: "internal/features/"
  module_structure:
    - "handler.go"
    - "service.go"
    - "repository.go"
  shared_path: "internal/shared/"
  sql_path: "sql/"
  docs_path: "docs/"
```

### Prompt System

The conductor uses a three-tier prompt resolution for every pipeline step:

1. **Repo override:** `<repo>/.prompts/<step>-prompt.md` — highest priority
2. **Project config:** path from `prompts` section of `project.yaml` — hard error if file missing
3. **Compiled default:** embedded in the binary — always available

The pipeline steps with independent prompts are:

| Step | Default File | Role |
|------|-------------|------|
| `scope_decompose` | `defaults/scope_decompose.md` | Identify investigation targets |
| `scope_analyze` | `defaults/scope_analyze.md` | Analyze a single target area |
| `scope_crosscut` | `defaults/scope_crosscut.md` | Find cross-cutting concerns |
| `scope_synthesize` | `defaults/scope_synthesize.md` | Produce final context package |
| `verify_analyze` | `defaults/verify_analyze.md` | Analyze a single diff segment |
| `verify_synthesize` | `defaults/verify_synthesize.md` | Produce final verify report |
| `build` | `defaults/build.md` | Instructions for Claude Code |
| `plan_epic` | `defaults/plan_epic.md` | Pass 1 epic decomposition |
| `plan_task` | `defaults/plan_task.md` | Pass 2 task decomposition |
| `plan_audit` | `defaults/plan_audit.md` | Planner audit pass |
| `describe` | `defaults/describe.md` | RAG chunk description generation |

### Index the Repository

```bash
conductor index --project /path/to/project.yaml
```

### Inspect the RAG Database

After indexing, verify what's stored:

```bash
# Aggregate stats: chunk counts by language, type, and top files
conductor rag-dump --stats

# Show all chunks for a specific file (sorted by line number)
conductor rag-dump --file internal/rag/store.go

# Find chunks matching a symbol name (includes call graph)
conductor rag-dump --name NewStore
```

The indexer runs a three-pass pipeline:

1. **Walk + Parse:** Walks the repo filtered by include/exclude globs, parses each file into structural chunks. For Go projects, uses `go/packages` and `go/ast` for rich metadata — call graphs, interface implementations, type relationships. Falls back to tree-sitter for TypeScript/TSX/Python, and a sliding-window chunker for everything else.

2. **Reverse Call Graph:** Resolves forward call references to populate `CalledBy` on target chunks, using package-suffix matching with O(1) lookups.

3. **Describe + Embed + Store:** Sends each file to the local LLM for semantic descriptions (with relationship context appended), embeds the signature + description via nomic-embed-code, and upserts into LanceDB.

Change detection via content hashing skips unchanged files on subsequent runs. Schema version tracking forces a full re-index when the storage format changes.

When `index.auto_reindex` is `true`, the index is updated after each `conductor approve`.

## Work Orders

A work order is a YAML file describing what you want built. The canonical shape
uses `schema_version: 2`, explicit requirement mapping, and typed verification
metadata.

Canonical example:

```yaml
schema_version: 2
title: "Preserve observability assets"
type: refactor
target_module: internal/api
reference_module: internal/http
known_files:
  - internal/api/server.go
  - internal/api/static/observability.css
requirements:
  - id: REQ-1
    text: Static observability assets remain reachable
acceptance_criteria:
  - id: AC-1
    description: GET /assets/observability.css returns 200
    requirement_ids: [REQ-1]
    required: true
    verification:
      kind: http_smoke
      route: assets
constraints:
  - "Do not change unrelated routes"
```

Work orders must use the version-2 shape above.

Fields:

- **title:** Short imperative description. Also used as a RAG search query.
- **type:** One of `new_feature`, `bug_fix`, `refactor`, `schema_change`, `docs`.
- **target_module:** Primary directory the changes will land in.
- **reference_module:** Existing module to use as an architectural reference (optional).
- **known_files:** Files the agent should definitely read or modify.
- **acceptance_criteria:** Typed criteria with `id`, `description`, `requirement_ids`, `required`, and `verification`.
- **requirements:** Requirement IDs that acceptance criteria must map back to.
- **constraints:** Things the agent must not do.

Version-2 verification kinds currently supported:

- **`precheck`** — runs an exact argv entry from `verify.commands`
- **`diff_review`** — assessed from changed files plus structured review context
- **`file_compatibility`** — loads the declared subject file plus a bounded subset of `known_files`
- **`http_smoke`** — runs a named deterministic command from `verify.smoke`

Verify reports store per-criterion results as `met`, `unmet`, or `unassessable`.
Required `unmet` criteria fail the workflow, required `unassessable` criteria warn,
and advisory criteria can warn but cannot independently fail a workflow.

## Review Workflow

After the pipeline completes, the workflow enters `human_review`:

```bash
conductor list
conductor status <id-prefix>
conductor approve <full-uuid>
conductor reject <full-uuid> "reason"
```

Inspect the artifacts:

```bash
# Context package (scope output)
cat ~/source/.conductor/projects/<name>/artifacts/context-packages/<id>-context-package.json | jq .

# Verification report
cat ~/source/.conductor/projects/<name>/artifacts/verify-reports/<id>-verify-report.json | jq .

# Build logs
cat ~/source/.conductor/projects/<name>/logs/<task-id>/stdout.log

# Git diff
cd /path/to/repo && git diff main..feature/conducted-<prefix>
```

On approve, the conducted branch is fast-forward merged into main and deleted. The work order is archived both as a file and in the database. If `auto_reindex` is enabled, the RAG index is updated.

## Metrics and Stats

The conductor tracks granular metrics across all pipeline runs:

```bash
conductor stats
```

This displays:

- **Pipeline overview:** total runs, verify result distribution (PASS/WARN/FAIL), human decisions (approved/rejected/pending)
- **Token usage:** scope and verify tokens in/out
- **Phase timing:** average seconds for scope and verify
- **Recent runs:** per-run detail with type, verify result, human result, complexity, agreement, and token count
- **Verify-human agreement:** confusion matrix of verify verdicts vs human decisions, with overall agreement percentage
- **Work order type breakdown:** pass/warn/fail rates and average scope time by type
- **Scope quality:** average paths stripped, average paths reclassified, complexity distribution (low/medium/high)
- **Sub-call summary:** per-provider call counts, token totals, estimated costs, average sub-calls per phase, and provider distribution

### Sub-Call Tracking

Every LLM call within the scope and verify pipelines is individually tracked in the `sub_calls` table: phase, step, target path, provider, model, tokens in/out, latency, estimated cost, and success/failure. This enables fine-grained analysis of which steps consume the most tokens and where failures occur.

## Context Package Structure

The build agent receives a structured JSON directive, not a text blob:

```json
{
  "work_order": {
    "title": "...",
    "type": "...",
    "target_module": "...",
    "reference_module": "...",
    "acceptance_criteria": ["..."],
    "constraints": ["..."],
    "known_files": ["..."]
  },
  "scope": {
    "files_to_modify": [{"path": "...", "reason": "..."}],
    "files_to_reference": [{"path": "...", "reason": "..."}],
    "file_contents": [{"path": "...", "source": "..."}],
    "relevant_code": [
      {
        "function": "...",
        "file": "...",
        "description": "...",
        "body": "...",
        "signature": "...",
        "calls": [{"name": "...", "package": "..."}],
        "called_by": [{"name": "...", "package": "..."}],
        "is_dependency_hop": false,
        "query_hit_count": 2
      }
    ],
    "summary": "...",
    "estimated_complexity": "medium",
    "build_instructions": "...",
    "new_files": [{"path": "...", "purpose": "..."}],
    "sql_files": [{"path": "...", "reason": "..."}],
    "dependencies": []
  },
  "directives": {
    "branch_name": "feature/conducted-abc12345",
    "reference_module_note": "Use internal/status as an architectural reference..."
  }
}
```

The `file_contents` array includes the full source of every file in `files_to_modify` and `files_to_reference`, pre-loaded so the build agent can begin editing without discovery reads. Files are capped at 50KB each with a 512KB total budget. The `relevant_code` array includes function bodies and signatures from the RAG index, plus both direct hits (ranked by multi-query hit count) and one-hop dependency expansions through the call graph.

## RAG Search

The searcher performs multi-query expansion from work order fields:

1. **Title** — primary search query
2. **Target module** — package-level search
3. **Acceptance criteria** — filtered for non-boilerplate criteria (skips "go build passes" etc.)
4. **Known files** — directory + filename search

Results are deduplicated across queries, ranked by hit count then score, and split into a 60/40 budget between direct hits and dependency hops. Dependency expansion walks one hop through both `Calls` and `CalledBy` edges in the stored call graph.

## Data Directory

All project data lives under `~/source/.conductor/projects/<project-name>/`:

```
db/conductor.db                              # SQLite — workflows, tasks, events, pipeline runs, sub-calls, plan runs
rag/chunks.lance/                            # LanceDB — vector embeddings
rag_file_hashes.json                         # Change detection for incremental re-indexing
artifacts/context-packages/<wf-id>-*.json    # Scope phase output
artifacts/verify-reports/<wf-id>-*.json      # Verify phase verdict
artifacts/build-evidence/<wf-id>-*.json      # Structured build validation evidence
artifacts/work-orders/<wf-id>.yaml           # Archived work orders (on approve)
logs/<task-id>/stdout.log                    # Build agent output
logs/<task-id>/stderr.log
```

The database is clean-slate only. If conductor encounters an older or
incompatible `db/conductor.db`, it deletes that DB file and recreates it from
the current canonical schema.

## Architecture

```
cmd/conductor/
  main.go              CLI root, cobra setup
  run.go               Synchronous pipeline execution
  plan.go              Spec → work order decomposition via Claude Code, with audit pass
  gate.go              Approve/reject with merge, archive, and auto-reindex
  index.go             RAG indexing entry point
  ragdump.go           RAG database inspection (stats, file, name lookup)
  ragsetup.go          Shared RAG stack initialization
  list.go              Workflow listing
  status.go            Workflow detail with prefix resolution
  stats.go             Aggregate metrics display
  resolve.go           Workflow ID prefix resolution
  initglobal.go        Global config scaffolding

internal/
  config/              Two-tier config (global + project), provider env var expansion
  context/             Context assembly — PreScope, GatherForTarget, Assemble
  database/            SQLite via sqlc — workflows, tasks, events, pipeline_runs, sub_calls, plan_runs
  errors/              Classified errors: Retryable, Fatal, NeedsHuman
  executor/            Build executors — ClaudeCodeExecutor, OpenCodeExecutor
  gate/                Human review — approve (merge + archive) / reject
  git/                 go-git operations — diff, changed files, merge, branch delete
  llm/                 OpenAI-compatible client, RoleResolver, RAGCompleterAdapter
  logging/             Structured slog handler
  models/              WorkOrder, ContextPackage, VerificationReport, FullContextPackage
  queue/               Task queue with atomic claiming, budget checks
  rag/
    indexer.go         Three-pass pipeline: walk+parse → reverse call graph → describe+embed+store
    goparser.go        go/packages AST parser with call graphs and interface detection
    parser.go          Tree-sitter parser (Go, TS, TSX, Python, Markdown, fallback)
    tsparser.go        TypeScript/TSX structural extraction
    pyparser.go        Python structural extraction
    describer.go       LLM-powered semantic descriptions with relationship context
    embedder.go        nomic-embed-code client with batch support
    searcher.go        Multi-query expansion, dedup/rerank, dependency hop expansion
    store.go           LanceDB vector storage with name index for call graph lookups
    types.go           Chunk, SearchResult, FuncRef, Filter
  scope/
    orchestrator.go    Scope pipeline: Decompose → Gather → Analyze → CrossCut → Synthesize
    types.go           InvestigationTarget, AreaAnalysis, CrossCutAnalysis, SubCallRecord
  templates/
    prompts.go         Three-tier prompt resolution, compiled defaults via go:embed
    defaults/          Embedded default prompts for all pipeline steps
  util/                Glob matching, SQL time parsing
  verify/
    orchestrator.go    Verify pipeline: Segment → Analyze → Synthesize
    types.go           DiffSegment, SegmentVerdict, PreCheckResult
  worker/
    worker.go          Task dispatch by phase
    scope.go           Scope phase runner with post-validation
    build.go           Build phase runner with forbidden path checks
    verify.go          Verify phase runner with pre-checks and merge
    metrics.go         Sub-call persistence and token aggregation

sql/queries.sql        sqlc query definitions
templates/             Project-level prompt overrides (scope, build, verify)
```

## Tech Stack

- **Go** with CGo for LanceDB bindings
- **LanceDB** for vector storage and similarity search
- **tree-sitter** for structural code parsing (Go, TypeScript, TSX, Python)
- **go/packages + go/ast** for rich Go analysis (call graphs, interface implementations, type relationships)
- **nomic-embed-code** for code-specialized embeddings via llama.cpp
- **DeepSeek-R1-Distill-Qwen-32B** (or any OpenAI-compatible model) for scope and verify analysis
- **Claude Code** for build execution and work order planning
- **SQLite** (via modernc.org/sqlite, pure Go) for workflow state and metrics
- **sqlc** for type-safe SQL query generation
- **Cobra** for CLI

## Known Limitations

- Single repo per work order — no cross-repo coordination
- No GitHub/GitLab PR creation
- Verify phase assumes `main` as the base branch
- LanceDB uses L2 distance — raw scores appear low but ranking is correct
- The plan command requires Claude Code on PATH
- Local model tool calling is not supported in the scope phase
