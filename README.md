# Agent Conductor

A pipeline orchestration system for AI-assisted code generation. Uses local models for analysis and Claude Code for implementation, with human review as the final gate.

## How It Works

```
conductor run --project project.yaml work-order.yaml
```

The conductor runs a three-phase pipeline:

**Scope** — Reads the work order, queries the RAG index for semantically relevant code, and sends everything to a local LLM (DeepSeek-R1). The LLM produces a context package: which files to modify, which to reference, what to create, and step-by-step build instructions. No tokens leave your machine during this phase.

**Build** — Passes the context package to Claude Code, which creates a git branch, implements the changes, and commits. Claude Code runs with `--dangerously-skip-permissions` in a non-interactive mode. The conductor stays out of its way — it prepares and directs, it doesn't micromanage.

**Verify** — Diffs the conducted branch against main, runs deterministic checks (build, test, vet if specified in acceptance criteria), then sends the diff to the local LLM for a qualitative review. Produces a PASS/WARN/FAIL verdict with findings.

The workflow then sits in `human_review` until you approve or reject it.

```
conductor list --project project.yaml
conductor status --project project.yaml <id>
conductor approve --project project.yaml <full-uuid>
conductor reject --project project.yaml <full-uuid> "reason"
```

## Why

Frontier AI tokens are expensive. The core insight: most of the work in an AI coding pipeline is analysis, not generation. File discovery, context assembly, and verification don't need a frontier model — a local 32B model handles them fine. Reserve the expensive model for the part that actually writes code.

This follows the RLM (Reasoning and Language Model) pattern: local models reason about what to do, cloud models execute.

## Prerequisites

- **Go 1.21+** with CGo support
- **DeepSeek-R1** (or compatible local LLM) serving on `http://localhost:8080/v1`
- **nomic-embed-code** serving on `http://localhost:8081/v1` with `--embeddings --pooling last`
- **Claude Code** installed at `/home/<user>/.local/bin/claude`
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

executor:
  tool: claude-code
  timeout_minutes: 30

safety:
  forbidden_paths: []
  max_files_changed: 50
  max_duration_mins: 60
```

Optional fields for Go projects or projects with conventions:

```yaml
conventions:
  module_path: "internal/features/"
  shared_path: "internal/shared/"
  sql_path: "sql/"
  docs_path: "docs/"

prompts:
  scope: ""    # blank = compiled defaults
  verify: ""
  build: ""
```

### Index the Repository

```bash
conductor index --project /path/to/project.yaml
```

This walks the repo, parses files into structural chunks (functions, classes, interfaces, types), generates semantic descriptions via the local LLM, embeds them with nomic-embed-code, and stores everything in LanceDB. Change detection skips unchanged files on subsequent runs.

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

## Review Workflow

After the pipeline completes, the workflow enters `human_review`:

```bash
# See all workflows
conductor list --project project.yaml

# Inspect a specific workflow
conductor status --project project.yaml <id-prefix>

# Read the verification report
cat ~/.conductor/projects/<name>/artifacts/verify-reports/<id>-verify-report.json | jq .

# Diff the changes
cd /path/to/repo && git diff main..feature/conducted-<prefix>

# Decide
conductor approve --project project.yaml <full-uuid>
conductor reject --project project.yaml <full-uuid> "reason"
```

After approving, manually merge the branch:

```bash
cd /path/to/repo
git checkout main
git merge feature/conducted-<prefix>
```

## Data Directory

All project data lives under `~/.conductor/projects/<project-name>/`:

```
db/conductor.db                              # SQLite — workflows, tasks, events, pipeline runs
rag/chunks.lance/                            # LanceDB — vector embeddings for RAG search
rag_file_hashes.json                         # Change detection for incremental re-indexing
artifacts/context-packages/<wf-id>-*.json    # Scope phase output
artifacts/verify-reports/<wf-id>-*.json      # Verify phase verdict
logs/<task-id>/stdout.log                    # Claude Code output
logs/<task-id>/stderr.log
```

## Architecture

```
cmd/conductor/          CLI entry points (run, index, list, status, approve, reject, stats)
internal/
  config/               Two-tier config: global defaults + project overrides
  context/              Context assembly — builds scope prompts and structured context packages
  database/             SQLite via sqlc — workflows, tasks, events, pipeline runs
  errors/               Phase-aware error handling
  executor/             Build executor interface — ClaudeCodeExecutor and OpenCodeExecutor
  gate/                 Human review — approve/reject with DB state management
  git/                  Git operations via go-git — diff, changed files
  llm/                  OpenAI-compatible client for local LLM calls
  logging/              Structured JSON logging
  models/               Work order and context package models
  queue/                Task queue with atomic claiming
  rag/                  RAG pipeline — indexing, parsing, embedding, vector search
  templates/            Prompt templates for scope, build, and verify phases
  util/                 Shared helpers
  worker/               Phase workers — scope, build, verify orchestration
templates/              External prompt template files
sql/                    SQL queries (sqlc source)
```

## Tech Stack

- **Go** with CGo for LanceDB bindings
- **LanceDB** for vector storage and search
- **tree-sitter** for structural code parsing (Go, TypeScript, TSX)
- **nomic-embed-code** (7B) for code-specialized embeddings
- **DeepSeek-R1-Distill-Qwen-32B** for scope and verify phases
- **Claude Code** for build execution
- **SQLite** (via modernc.org/sqlite) for workflow state
- **go-git** for git operations
- **Cobra** for CLI

## Known Limitations

- Approve does not auto-merge — manual `git merge` required after approval
- Claude Code output does not stream in real-time to the console
- No cross-repo coordination (single repo per work order)
- No GitHub PR creation
- go-git merge support is limited; complex merges may need manual intervention
- LanceDB uses L2 distance — similarity scores appear low but ranking is correct
- Verify phase assumes `main` as the default branch