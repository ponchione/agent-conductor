# Technical Debt

This document tracks issues discovered while executing prompt-related work
orders. Update it as soon as issues are identified.

Last reviewed: 2026-03-17

## Open Issues

### Work-order history persistence is not yet durable

- Area: work-order lifecycle / database artifacts
- Symptom: work-order YAML files are operational inputs, but there is not yet a
  clearly defined durable DB record for the completed/approved work-order
  content and outcome summary
- Observed during: final cleanup after WO-010
- Decision: keep `/work-orders` ignored by git and do not use repository
  tracking as the canonical history mechanism
- Next step: persist the original or normalized work-order content plus outcome
  metadata into the project DB and/or artifacts so completed input YAML can be
  deleted safely
- Impact: work-order execution history is only partially durable today unless
  the input file is preserved externally

## Resolved During Cleanup

### Work-order verification wording drift

- Area: `work-orders/prompt-wo/001-wire-planner-prompts-into-config-and-loader.yaml`
- Symptom: WO-001 still referenced a raw `go test` invocation while the rest of
  the prompt work-order sequence had been normalized to the repository
  `make test` contract
- Resolved: updated during final cleanup on 2026-03-16

### Sandbox-sensitive Go module stat-cache warning

- Area: Go toolchain / module cache behavior in restricted environments
- Symptom: `make build` succeeded but emitted a non-fatal `go: writing stat
  cache` permission warning against the default module cache path
- Observed during: WO-003, WO-004, WO-005, WO-006, WO-007, WO-008, WO-009, WO-010
- Resolved: standardized writable `GOCACHE` and `GOMODCACHE` defaults in the
  `Makefile` on 2026-03-17; `make build` now runs cleanly against `/tmp`
  cache paths

### Test instability: database concurrency test

- Area: `internal/database`
- Symptom: `TestAtomicClaimTask_ConcurrentClaimSingleWinner` intermittently
  timed out under SQLite lock contention
- Observed during: WO-002, WO-003, WO-004
- Resolved: added SQLite busy handling and single-connection pooling in
  `NewDB`, moved busy retries into `AtomicClaimTask`, and simplified the test
  to rely on the production retry path
- Verification: `go test ./internal/database -run TestAtomicClaimTask_ConcurrentClaimSingleWinner -count=20`,
  `go test ./internal/database`, and `make test` all passed on 2026-03-17

### Sandbox-sensitive test: llm client test server bind

- Area: `internal/llm`
- Symptom: `TestClient_Complete` and related tests depended on
  `httptest.NewServer`, which required local socket binds unavailable in the
  restricted sandbox
- Observed during: WO-002, WO-003, WO-004, WO-006, WO-007, WO-008, WO-009, WO-010
- Resolved: kept the public constructor behavior stable, added an internal
  `*http.Client` seam, and rewrote the tests to use transport stubs instead of
  real listeners
- Verification: `go test ./internal/llm` and `make test` both passed on
  2026-03-17
