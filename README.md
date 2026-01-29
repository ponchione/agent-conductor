# Agent Orchestration System

A personal experiment in coordinating AI coding agents across repositories.

## What This Is

I use AI agents (via OpenCode) to help build my product. The problem: coordinating agents across multiple repos requires a lot of manual babysitting — running agents, copying tickets between repos, creating branches, watching for completion, etc.

This system automates that coordination layer while keeping humans in the loop for the decisions that matter.

## Current State

**Phase 4 of 9 complete.** This is a work in progress.

What works:
- Project scaffolding and configuration
- SQLite database with workflow/task/event tables
- File scanner that detects new work orders
- Task queue with atomic claiming
- Git branch creation and management
- OpenCode CLI execution with timeout handling

What doesn't exist yet:
- Full worker loop
- Safety limit enforcement
- Cross-repo ticket routing
- GitHub PR creation
- CLI interface beyond basics

## The Idea

```
Work Order dropped in inbox
    ↓
Orchestrator detects it
    ↓
Creates isolated git branch
    ↓
Runs agent via OpenCode
    ↓
Agent does Planning phase → pauses for human approval
    ↓
Human approves → Agent does Build phase
    ↓
If agent creates ticket for other repo → route it there, repeat
    ↓
PR created when done
```

Safety mechanisms prevent runaway loops: depth limits, file count limits, time budgets, forbidden paths.

## Why

Two reasons:

1. **Practical** — I got tired of being a human message bus between my frontend and backend agents
2. **Learning** — Wanted to build something non-trivial in Go that solves a real problem I have

## Tech Stack

- Go 1.21+
- SQLite (via modernc.org/sqlite, pure Go)
- fsnotify for file watching
- Cobra for CLI
- Shells out to `git` and `opencode`

## Related Docs

- `docs/SPEC.md` — Full technical specification
- `docs/IMPLEMENTATION.md` — Phased build plan

## Status

Experimental. I use the manual version of this workflow daily for my actual product. The automation is catching up.