# Project Summary

**Agent Conductor** is an orchestration system designed to automate and coordinate AI coding agents (specifically via the `opencode` CLI) across software repositories. It solves the problem of manual intervention when using AI agents by automating repository setup, context gathering, task execution, and verification, while purposefully keeping humans in the loop for critical approvals.

The primary technologies used are:
- **Go (1.21+)**: The core language for the orchestration engine.
- **SQLite (`modernc.org/sqlite`)**: Used as an embedded, pure-Go database for tracking workflow states, task queues, and execution events.
- **LanceDB (`lancedb-go`)**: Embedded vector database (CGO-backed) used by the RAG subsystem to store and search code chunk embeddings.
- **tree-sitter**: Used by the RAG parser to extract structured chunks (functions, methods, types) from Go source files.
- **Git Integration**: Relies on shelling out to Git (and using `go-git`) for branch management and commit tracking.
- **External AI Integrations**: Relies on a local LLM HTTP endpoint (for scoping and verification) and the `opencode` CLI (for the core build phase). A separate local embedding server (e.g., llama.cpp) powers the RAG indexer.

# Architecture Overview

- **Architectural Style**: The project follows a purely CLI-driven modular monolith architecture. It is invoked ad-hoc via CLI commands; there is no background daemon, file watcher, or polling loop.
- **Core Design Patterns**:
  - **State Machine / Workflow Pattern**: Workflows transition through strict, predictable phases (`scope` → `build` → `verify` → `human_review` → `completed`/`failed`).
  - **Lightweight Execution Tracker**: Tasks are stored in a SQLite-backed queue and processed sequentially by the `run` command. The queue provides state tracking and audit history, not async dispatch.
  - **Event Sourcing (Audit Trail)**: State changes and task progress are appended to an `events` table for observability and debugging.
- **Data Flow Overview**:
  1. A **Work Order** (YAML file) is submitted via `conductor run <work-order.yaml>`.
  2. The system provisions a dedicated git branch and initializes a new **Workflow** and **Task** in the database.
  3. The `run` command invokes phases sequentially — Scope, Build, then Verify — each blocking until complete.
  4. Artifacts (JSON payloads, logs) are generated and stored on the filesystem at each phase.
  5. Upon completing verification, the system halts and enters a `human_review` gate, awaiting CLI approval/rejection to finalize the workflow.

# Directory Structure

```
.
├── cmd/
│   └── conductor/         # Application entry points and CLI commands (main, run, approve, status, index)
├── internal/              # Core application domains and isolated business logic
│   ├── config/            # YAML configuration parsing (project.yaml), including embed_model section
│   ├── context/           # Assembles repo context (files, git history) for the LLM prompt
│   ├── database/          # SQLite database connection, migrations, and queries (sqlc generated)
│   ├── executor/          # Subprocess wrappers (e.g., executing the `opencode` CLI)
│   ├── gate/              # Human-in-the-loop approval/rejection logic
│   ├── git/               # Git repository management and diffing tools
│   ├── llm/               # HTTP client for interacting with Local LLM endpoints
│   ├── models/            # Core data structures (WorkOrder, ContextPackage, VerificationReport)
│   ├── queue/             # Database-backed atomic task queue management
│   ├── rag/               # RAG subsystem: parser, embedder, describer, store, indexer
│   ├── templates/         # Prompt templates used for the LLM
│   └── worker/            # Phase execution logic (Scope, Build, Verify)
├── sql/                   # Database schema (schema.sql) and queries (queries.sql) for sqlc
├── include/               # C headers for the lancedb-go native library (CGO)
├── lib/                   # Precompiled lancedb-go native libraries per platform
└── ...                    # Build scripts, Makefiles, and configuration
```

**Responsibility of top-level directories:**
- `cmd/`: Holds the entry points for the Go application. Connects CLI arguments to internal system logic.
- `internal/`: Houses all the private application code, organized by domain. It separates infrastructure concerns (DB, Git, LLM, Exec) from business logic (Worker, Queue, Gate).
- `sql/`: Contains raw SQL files used by `sqlc` to auto-generate the type-safe Go code found in `internal/database`.

# Key Components

- **Main Entry Points**:
  - `cmd/conductor/main.go`: Sets up the Cobra root command and calls `Execute()` to dispatch subcommands (`run`, `approve`, `reject`, `status`, `stats`, `index`).
  - `cmd/conductor/run.go`: The synchronous execution loop that seeds the database and drives each phase to completion before entering the human review gate.
  - `cmd/conductor/gate.go`: Cobra commands for `approve` and `reject`.
  - `cmd/conductor/index.go`: The `conductor index` subcommand — wires config, LanceDB store, embedder, and describer into `rag.IndexRepo`.
- **Core Services/Modules**:
  - `worker.Worker`: Routes tasks to the appropriate phase handler (`runScope`, `runBuild`, `runVerify`) and updates workflow state on completion or failure.
  - `context.Assembler`: Gathers project intelligence (file trees, line counts, git history) to inject into LLM prompts.
  - `executor.OpenCodeRunner`: Executes the `opencode` CLI securely, with timeouts and log redirection.
  - `rag.IndexRepo`: Drives the per-file indexing pipeline: glob filtering → change detection → parse → describe → embed → upsert.
  - `rag.Store`: LanceDB-backed vector store for chunk persistence and similarity search.
  - `rag.Embedder`: HTTP client for the llama.cpp `/v1/embeddings` endpoint; supports batch embedding with automatic chunking.
  - `rag.Describer`: Calls the local LLM to generate 1–2 sentence semantic descriptions per chunk, one call per file.
  - `rag.ParseFile`: Dispatches to a tree-sitter Go parser, a markdown section splitter, or a sliding-window fallback based on file extension.
- **Important Configuration Files**:
  - `project.yaml`: Defines project path, index include/exclude globs, git conventions, local LLM settings, embed model settings, safety thresholds, and prompts.
- **Database/Integrations**:
  - **SQLite DB**: Manages tables for `workflows`, `tasks`, `events`, and `pipeline_runs`.
  - **LanceDB**: Stores `chunks` table with embeddings; queried via vector search during scope context assembly.
  - **LLM Client**: Interfaces with a generic HTTP endpoint for intelligence (completion and embeddings).

# Execution Flow

1. **Application Start**: When `conductor run <work-order.yaml>` is executed, the app loads configurations, creates a SQLite database if missing, generates an isolated git branch, and seeds a new `Workflow` and `Scope` task into the queue.
2. **Phase 1: Scope (`internal/worker/scope.go`)**:
   - The worker reads the Work Order and uses the `Assembler` to pack repository context.
   - It prompts the local LLM to generate a `ContextPackage` (identifying files to modify, new files, and dependencies).
   - Once saved to disk, it queues a `Build` task.
3. **Phase 2: Build (`internal/worker/build.go`)**:
   - The worker invokes the `opencode` CLI via the `OpenCodeRunner`, passing the Work Order and `ContextPackage`.
   - The agent writes the code. Upon successful exit, the worker checks for safety violations (forbidden paths) and commits the changes to the active branch. Enqueues a `Verify` task.
4. **Phase 3: Verify (`internal/worker/verify.go`)**:
   - The worker runs deterministic pre-checks (`go test`, `go build`, etc.) based on the acceptance criteria.
   - It gathers the Git diff and remaining criteria, then prompts the LLM to generate a `VerificationReport` grading the implementation.
   - The workflow state is transitioned to `human_review`.
5. **Phase 4: Gate (`internal/gate/gate.go`)**:
   - Execution suspends. A user reviews the diff and runs `conductor approve <id>` or `conductor reject <id>`.
   - The state transitions to `completed` or `failed`.

# Dependencies

**Key Third-Party Libraries:**
- `modernc.org/sqlite`: A pure-Go SQLite driver, eliminating the need for CGO.
- `github.com/lancedb/lancedb-go`: CGO-backed embedded vector database used by the RAG store. Requires the precompiled `liblancedb_go` native library in `lib/`.
- `github.com/apache/arrow/go/v17`: Arrow columnar format used to build LanceDB record batches.
- `github.com/tree-sitter/go-tree-sitter` + `tree-sitter-go`: Incremental parsing of Go source files for structured chunk extraction.
- `gopkg.in/yaml.v3`: For parsing work orders and system configurations.
- `github.com/google/uuid`: For tracking unique IDs across workflows and events.
- `github.com/go-git/go-git/v5`: For programmatic, native Git operations alongside shell executions.
- `github.com/spf13/cobra`: Standard Go CLI framework providing subcommand routing, flag parsing, and auto-generated help text for all `conductor` commands.

**External Systems Integrated:**
- **Git**: Local installation is required for branching, committing, and diffing.
- **opencode CLI**: The external agent engine relied upon during the Build phase to actually execute code mutations.
- **Local LLM** (completion): (e.g., vLLM, Ollama, LM Studio) Provides the reasoning engine for the Scope and Verify phases, and generates per-chunk descriptions during indexing. Default endpoint: `http://localhost:8080/v1`.
- **Local embedding server** (e.g., llama.cpp with `nomic-embed-text-v1.5`): Produces vector embeddings for code chunks and queries via the `/v1/embeddings` endpoint. Default endpoint: `http://localhost:8081/v1`.

**Build Notes:**
- The project requires CGO to link the LanceDB native library. Use `make build` rather than plain `go build`. The `Makefile` sets `CGO_CFLAGS` and `CGO_LDFLAGS` to point at `include/` and `lib/linux_amd64/` respectively.
- At runtime, the dynamic library must be on `LD_LIBRARY_PATH` (or use the `-rpath` baked in by `make build`).
