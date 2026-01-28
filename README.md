# Agent Orchestration System (WORK IN PROGRESS)

A Go-based orchestration layer for coordinating AI coding agents across multiple repositories with built-in safety controls, human oversight gates, and automatic workflow management.

## Overview

Modern AI coding agents (Claude, GPT, etc.) can implement features, fix bugs, and write tests — but coordinating them across repositories, enforcing safety limits, and maintaining quality requires careful orchestration. This system automates the "human in the middle" work while preserving critical oversight checkpoints.

```
┌─────────────────────────────────────────────────────────────────┐
│                         CONDUCTOR                               │
│                                                                 │
│   ┌─────────┐     ┌─────────┐     ┌─────────┐     ┌─────────┐  │
│   │ Scanner │────▶│  Queue  │────▶│ Worker  │────▶│   Git   │  │
│   │         │     │ Manager │     │  Pool   │     │ Manager │  │
│   └─────────┘     └─────────┘     └─────────┘     └─────────┘  │
│        │                               │               │        │
│        ▼                               ▼               ▼        │
│   ┌─────────┐                    ┌─────────┐     ┌─────────┐   │
│   │ SQLite  │                    │OpenCode │     │ GitHub  │   │
│   │   DB    │                    │   CLI   │     │   API   │   │
│   └─────────┘                    └─────────┘     └─────────┘   │
└─────────────────────────────────────────────────────────────────┘
```

## Key Features

### Safety-First Design
- **Depth limits** — Prevent infinite agent ping-pong loops
- **File count limits** — Contain blast radius of changes
- **Time budgets** — Kill runaway agent sessions
- **Forbidden paths** — Protect critical files (go.mod, package.json, etc.)
- **Git branch isolation** — All work happens on feature branches, never main

### Human-in-the-Loop Gates
- **Planning approval** — Review agent's plan before execution
- **Scope expansion requests** — Agents ask permission for out-of-scope files
- **Cross-repo tickets** — Human approves before work spans repositories
- **PR-based merging** — All changes require explicit merge approval

### Multi-Repository Coordination
- **Ticket protocol** — Agents emit tickets to request work in other repos
- **Centralized inbox** — Cross-repo routing without agents touching other codebases
- **Contract synchronization** — Backend maintains API contracts, frontend consumes them

### Specialized Agent Roles

| Agent | Purpose |
|-------|---------|
| **Executor** | Implements features following work orders |
| **Work Order Agent** | Transforms tickets into detailed work orders |
| **Scope Advisor** | Recommends file scope using architecture maps |
| **Pre-flight Validator** | Validates work orders before execution |
| **Test Companion** | Writes tests for completed work |
| **Contract Reconciler** | Verifies frontend/backend alignment |

## Architecture

### State Machine

Workflows progress through well-defined states with explicit transitions:

```
PENDING → RUNNING → REVIEW_NEEDED → RUNNING → COMPLETED
                         │
                         └──────────→ FAILED
```

Gates can pause workflows at any point, requiring human approval to continue.

### Database Schema

SQLite-backed persistence for workflows, tasks, and audit events:

```sql
workflows    — Tracks complete chains of work
tasks        — Individual agent executions within a workflow  
events       — Full audit log of all state transitions
```

### Directory Structure

```
~/agent-workspace/
├── conductor.db          # SQLite database
├── config.yaml              # conductor configuration
├── inbox/                   # Centralized work intake
│   ├── frontend/
│   │   ├── orders/          # Work orders for frontend
│   │   └── tickets/         # Tickets targeting frontend
│   └── backend/
│       ├── orders/          # Work orders for backend
│       └── tickets/         # Tickets targeting backend
├── repos/                   # Symlinks to actual repositories
└── archive/                 # Completed workflows and artifacts
```

## Installation

```bash
# Clone the repository
git clone https://github.com/yourusername/conductor.git
cd conductor

# Build
go build -o bin/conductor ./cmd/conductor

# Initialize workspace
./bin/conductor init --path ~/agent-workspace

# Copy and edit configuration
cp config.example.yaml ~/agent-workspace/config.yaml
```

## Configuration

```yaml
# config.yaml

repositories:
  frontend:
    path: /home/user/code/frontend
    opencode_agent_executor: executor
    opencode_agent_workorder: work-order
    forbidden_paths:
      - package.json
      - pnpm-lock.yaml

  backend:
    path: /home/user/code/backend
    opencode_agent_executor: executor
    opencode_agent_workorder: work-order
    forbidden_paths:
      - go.mod
      - go.sum
      - main.go

safety:
  max_depth: 5
  max_files_changed: 50
  max_task_duration_minutes: 30

gates:
  planning_complete: true
  cross_repo_ticket: true
```

## Usage

### Start the conductor

```bash
# Start in foreground
conductor start

# Or run as daemon
conductor start --daemon
```

### Create a Work Order

Drop a work order into the inbox:

```bash
cp my-work-order.md ~/agent-workspace/inbox/backend/orders/
```

The conductor will automatically:
1. Detect the new file
2. Create a workflow with isolated git branch
3. Dispatch to an available worker
4. Run the appropriate agent via OpenCode

### Monitor Workflows

```bash
# List active workflows
conductor workflows list

# Show workflow details
conductor workflows show wf-abc123

# View logs
conductor logs --workflow wf-abc123 --follow
```

### Handle Gates

When a workflow pauses for human review:

```bash
# View what's waiting
conductor status

# Approve and continue
conductor approve wf-abc123

# Reject and cancel
conductor reject wf-abc123 --message "Scope too broad"
```

### Manual Operations

```bash
# Pause a running workflow
conductor workflows pause wf-abc123

# Resume a paused workflow
conductor workflows resume wf-abc123

# Trigger a single agent run (for testing)
conductor run --repo backend --agent executor --file /path/to/workorder.md
```

## Agent Protocol

### Work Order Format

```markdown
# Work Order

## Metadata
- **WO-ID:** WO-20260121-143000-API-matches-create
- **Target Repo:** backend
- **Feature Tag:** matches
- **Base-Commit:** abc123

## Allowed Scope
- internal/features/match/**
- docs/features/match/**

## Forbidden Scope  
- internal/platform/**
- go.mod

## Acceptance Criteria
- [ ] POST /v1/matches returns 201 with match ID
- [ ] Invalid payload returns 400 with error details
```

### Ticket Format

```markdown
### TKT-20260121-150000-API-to-WEB-matches-wire-ui

- **From-Repo:** backend
- **To-Repo:** frontend
- **Feature Tag:** matches

#### Request
Wire the match creation form to call POST /v1/matches

#### Acceptance Criteria
- [ ] Form submits to backend endpoint
- [ ] Success redirects to match detail page
- [ ] Errors display inline

#### Scope Hints
- apps/core/actions/match-actions.ts
- apps/core/components/matches/**
```

## Architecture Maps

Each repository should include an `ARCHITECTURE.md` that documents:

- Directory → responsibility mapping
- Feature module file groupings
- Cross-cutting files and their impact
- Naming conventions

Agents consult this map to determine correct scope for work orders.

Example:
```markdown
## Feature Modules

### matches
- internal/features/match/**
- sql/queries/match/**
- docs/features/match/**
```

## Safety Mechanisms

### Scope Enforcement

Agents may only modify files within `Allowed Scope`. Attempting to modify out-of-scope files triggers:

1. **SCOPE_EXPANSION_REQUEST** — Agent asks for permission
2. **Workflow pauses** — Human reviews the request
3. **Approve/Deny** — Human decides, workflow continues or agent adjusts

### Cycle Detection

The system tracks content hashes of tickets. If a similar ticket appears twice in the same workflow, execution pauses to prevent infinite loops.

### Budget Tracking

```yaml
Workflow wf-abc123:
  Depth:    2/5      # Agent round-trips
  Files:    12/50    # Modified files
  Duration: 8/30 min # Wall clock time
```

Exceeding any limit pauses the workflow for review.

## Development

### Running Tests

```bash
go test ./...
```

### Project Structure

```
conductor/
├── cmd/conductor/     # CLI entry point
├── internal/
│   ├── config/           # Configuration loading
│   ├── database/         # SQLite operations
│   ├── scanner/          # File watching
│   ├── queue/            # Task queue management
│   ├── worker/           # Agent execution
│   ├── executor/         # OpenCode integration
│   ├── git/              # Git operations
│   ├── github/           # PR creation
│   ├── safety/           # Validation & limits
│   └── cli/              # Command implementations
└── docs/
    ├── SPEC.md           # Technical specification
    └── IMPLEMENTATION.md # Implementation plan
```

## Roadmap

### v1.0 (MVP)
- [x] Scanner and task queue
- [x] Single worker execution
- [x] Git branch isolation
- [x] Basic safety limits
- [x] CLI interface
- [ ] GitHub PR creation

### v1.1
- [ ] Cross-repo ticket routing
- [ ] Planning phase gates
- [ ] Scope expansion protocol

### v2.0
- [ ] Multiple workers
- [ ] Web dashboard
- [ ] Discord notifications
- [ ] Token usage tracking

## Related Documentation

- [Technical Specification](docs/SPEC.md) — Detailed system design
- [Implementation Plan](docs/IMPLEMENTATION.md) — Phased build plan
- [Agent Prompts](docs/agents/) — System prompts for each agent role

## License

MIT

## Acknowledgments

Built for coordinating [OpenCode](https://opencode.ai) agents. Designed through extensive conversation about what could go wrong and how to prevent it.