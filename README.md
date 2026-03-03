# Agent Conductor

A pipeline orchestration system for AI-assisted code generation. Uses local models for analysis and Claude Code for implementation, with human review as the final gate.

## How It Works

```
conductor run --project project.yaml work-order.yaml
```

The conductor runs a three-phase pipeline:

**Scope** — Reads the work order, gathers project context (file tree, conventions, RAG results), and sends everything to a local LLM (DeepSeek-R1). The LLM produces a context package: which files to modify, which to reference, what to create, and step-by-step build instructions. Post-scope validation then checks every path against the filesystem, strips hallucinated paths, reclassifies misplaced entries, and retries the LLM with correction feedback if too many paths were invalid. No tokens leave your machine during this phase.

**Build** — Passes the context package to Claude Code, which creates a git branch, implements the changes, and commits. Claude Code runs with `--dangerously-skip-permissions` in a non-interactive mode. The conductor stays out of its way — it prepares and directs, it doesn't micromanage.

**Verify** — Diffs the conducted branch against main, runs deterministic checks (build, test, vet if specified in acceptance criteria), then sends the diff to the local LLM for a qualitative review. Produces a PASS/WARN/FAIL verdict with findings.

The workflow then sits in `human_review` until you approve or reject it.

```
conductor list --project project.yaml
conductor status --project project.yaml <id>
conductor approve --project project.yaml <full-uuid>
conductor reject --project project.yaml <full-uuid> "reason"
```

Approving merges the conducted branch into main, archives the work order, and optionally re-indexes the RAG store to include the new code.

## Planning

Got a spec from a conversation with Claude? Turn it into executable work orders:

```
conductor plan spec.md --output ./work-orders/
```

The plan command reads any text format (markdown, bullets, freeform spec, implementation plan) and calls Claude Code to decompose it into a dependency-ordered sequence of properly-scoped work orders. Each work order is sized for a single conductor run.

For existing projects, pass `--project` to ground the plan in the actual codebase:

```
conductor plan spec.md --project project.yaml --output ./work-orders/
```

The planner receives the project's file tree and conventions, producing work orders that reference real paths and follow existing patterns.

For greenfield projects, omit `--project`. The planner operates from the spec alone and generates an initial work order to set up the project structure.

Output is a set of numbered YAML files ready for `conductor run`:

```
work-orders/
  001-initialize-project-structure.yaml
  002-add-database-schema.yaml
  003-implement-api-endpoints.yaml
```

Review and adjust before running. The planner encodes sizing heuristics, dependency ordering, and acceptance criteria standards, but your judgment is the final gate.

## Prerequisites

- **Go 1.21+** with CGo support
- **An existing git repository** with at least one commit on main (the conductor does not initialize repos)
- **DeepSeek-R1** (or compatible local LLM) serving on `http://localhost:8080/v1`
- **nomic-embed-code** serving on `http://localhost:8081/v1` with `--embeddings --pooling last`
- **Claude Code** installed and available on PATH
- **NVIDIA GPU** with sufficient VRAM for local models (RTX 4090 or equivalent)

### Docker Compose for Local Models

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

The binary requires CGo for LanceDB. The Makefile handles the flags:

```bash
make build
```

This produces `bin/conductor` with the LanceDB shared library path baked in via rpath. You can copy the binary elsewhere — it will still find `liblancedb_go.so` at the absolute path in the repo's `lib/linux_amd64/` directory.

```bash
cp bin/conductor ~/bin/conductor
alias conductor='~/bin/conductor'
```

## Setup

### Global Configuration

```bash
conductor init-global
```

Creates `~/.conductor/config.yaml` with defaults. Edit to match your infrastructure:

```yaml
local_model:
  endpoint: "http://localhost:8080/v1"
  model_name: "deepseek-r1"
  temperature: 0.0
  timeout_seconds: 120
embed_model:
  endpoint: "http://localhost:8081/v1"
  model_name: "nomic-embed-code"
  timeout_seconds: 30
git:
  branch_prefix: "feature/conducted"
  commit_author_name: "Agent Conductor"
  commit_author_email: "conductor@local"
```

### Project Configuration

Create a `project.yaml` in or near your repository:

```yaml
project:
  name: my-project
  path: /absolute/path/to/repo
  language: typescript
  framework: react

index:
  include:
    - "**/*.ts"
    - "**/*.tsx"
    - "**/*.md"
  exclude:
    - "**/node_modules/**"
    - "**/.git/**"
    - "**/dist/**"
    - "**/build/**"
    - "**/.next/**"
  max_tree_lines: 200
  max_rag_results: 30
  auto_reindex: false

executor:
  tool: claude-code
  timeout_minutes: 30

safety:
  forbidden_paths: []
  max_files_changed: 50
  max_duration_mins: 60
```

Optional fields for projects with conventions:

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

When conventions are configured, the scope phase injects them into the LLM prompt so it understands the project's structural patterns.

### Prompt Overrides

The conductor uses compiled default prompts for scope, build, and verify phases. You can override these per-project by placing files in a `.prompts/` directory at the repo root:

```
<repo>/.prompts/
  scope-prompt.md
  build-prompt.md
  verify-prompt.md
```

The resolution order for each phase is: (1) repo `.prompts/` file if it exists, (2) path from `prompts` section of project.yaml if configured (hard error if file missing), (3) compiled default.

### Index the Repository

```bash
conductor index --project /path/to/project.yaml
```

This walks the repo, parses files into structural chunks (functions, classes, interfaces, types), generates semantic descriptions via the local LLM, embeds them with nomic-embed-code, and stores everything in LanceDB. Change detection skips unchanged files on subsequent runs.

When `index.auto_reindex` is set to `true`, the RAG index is automatically updated after each `conductor approve`, so new code is immediately available for future scope phases. If re-indexing fails, the approve still succeeds — it's best-effort.

Supported languages for structural parsing: Go (full AST with call graphs), TypeScript, TSX. All other file types fall back to a sliding-window chunker.

## Work Orders

A work order is a YAML file describing what you want built:

```yaml
title: "Add health check endpoint"
type: new_feature
target_module: health
reference_module: ""
acceptance_criteria:
  - "GET /health returns 200"
  - "go build ./... passes"
known_files: []
constraints: []
```

The `type` field accepts: `new_feature`, `bug_fix`, `refactor`, `schema_change`, `docs`.

Run it:

```bash
conductor run --project /path/to/project.yaml work-order.yaml
```

## Scope Phase Detail

The scope phase assembles rich context for the LLM through a pre-scope, scope, and post-scope pipeline:

**Pre-scope** gathers deterministic context: a glob-filtered, indented file tree of the project (capped at `max_tree_lines`), project conventions (if configured), and multi-query RAG search results with dependency hop expansion. This gives the scope LLM a map of what exists, how the project is structured, and which functions are semantically relevant.

**Scope** sends the assembled context to the local LLM, which returns a structured JSON context package identifying files to modify, files to reference, new files to create, and build instructions.

**Post-scope** validates the LLM's output against reality. Every path in `files_to_modify` and `files_to_reference` is checked with `os.Stat`. Paths that don't exist are stripped. Entries in `new_files` that already exist on disk are automatically reclassified to `files_to_modify`. If more than 50% of paths are stripped, the LLM is retried with correction feedback listing the invalid paths.

## Review Workflow

After the pipeline completes, the workflow enters `human_review`:

```bash
# See all workflows
conductor list --project project.yaml

# Inspect a specific workflow
conductor status --project project.yaml <id-prefix>

# Read the verification report
cat ~/.conductor/projects/<n>/artifacts/verify-reports/<id>-verify-report.json | jq .

# Diff the changes
cd /path/to/repo && git diff main..feature/conducted-<prefix>

# Decide
conductor approve --project project.yaml <full-uuid>
conductor reject --project project.yaml <full-uuid> "reason"
```

On approve, the conducted branch is merged into main and deleted. The original work order is archived (both as a file copy and in the database) for historical reference. If `auto_reindex` is enabled, the RAG index is updated to include the new code.

## Metrics and Stats

The conductor tracks per-phase timing, token usage, scope quality, and outcomes across all pipeline runs:

```bash
conductor stats --project project.yaml
```

This displays aggregate statistics including: total runs, verify result distribution (PASS/WARN/FAIL), human decision counts, token usage by phase, average phase duration, verify-to-human agreement rate, breakdown by work order type, and scope quality metrics (paths stripped, paths reclassified, complexity distribution).

Tracked metrics per run include: scope estimated complexity, RAG direct hits vs dependency hops, paths stripped and reclassified during post-scope validation, build files changed, build scope drift, verify result, and human decision. These enable closed-loop prompt tuning — if paths_stripped trends high, the scope prompt needs adjustment; if verify-human agreement is low, the verify prompt needs calibration.

## Data Directory

All project data lives under `~/.conductor/projects/<project-name>/`:

```
db/conductor.db                              # SQLite — workflows, tasks, events, pipeline runs
rag/chunks.lance/                            # LanceDB — vector embeddings for RAG search
rag_file_hashes.json                         # Change detection for incremental re-indexing
artifacts/context-packages/<wf-id>-*.json    # Scope phase output
artifacts/verify-reports/<wf-id>-*.json      # Verify phase verdict
artifacts/work-orders/<wf-id>.yaml           # Archived work orders (on approve)
logs/<task-id>/stdout.log                    # Claude Code output
logs/<task-id>/stderr.log
```

## Architecture

```
cmd/conductor/          CLI entry points (run, plan, index, list, status, approve, reject, stats)
internal/
  config/               Two-tier config: global defaults + project overrides
  context/              Context assembly — builds scope prompts and structured context packages
  database/             SQLite via sqlc — workflows, tasks, events, pipeline runs
  errors/               Phase-aware error handling (retryable, needs-human, fatal)
  executor/             Build executor interface — ClaudeCodeExecutor and OpenCodeExecutor
  gate/                 Human review — approve/reject with branch merge and archival
  git/                  Git operations — diff, changed files, merge, branch management
  llm/                  OpenAI-compatible client for local LLM calls
  logging/              Structured JSON logging
  models/               Work order, context package, and verification report models
  queue/                Task queue with atomic claiming
  rag/                  RAG pipeline — indexing, parsing, embedding, vector search
  templates/            Prompt templates with .prompts/ override support
  util/                 Shared helpers (glob matching, SQL utilities)
  worker/               Phase workers — scope (pre/post validation), build, verify
sql/                    SQL queries (sqlc source)
```

## Tech Stack

- **Go** with CGo for LanceDB bindings
- **LanceDB** for vector storage and search
- **tree-sitter** for structural code parsing (Go, TypeScript, TSX)
- **Go AST parser** for rich Go analysis (call graphs, interface implementations, type relationships)
- **nomic-embed-code** (7B) for code-specialized embeddings
- **DeepSeek-R1-Distill-Qwen-32B** for scope and verify phases
- **Claude Code** for build execution and work order planning
- **SQLite** (via modernc.org/sqlite) for workflow state and metrics
- **Cobra** for CLI

## Known Limitations

- Claude Code output does not stream in real-time to the console
- No cross-repo coordination (single repo per work order)
- No GitHub PR creation
- go-git merge support is limited; complex merges may need manual intervention
- LanceDB uses L2 distance — similarity scores appear low but ranking is correct
- Verify phase assumes `main` as the default branch
- The plan command requires Claude Code to be available on PATH