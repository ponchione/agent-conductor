# Technical Debt

This document tracks issues discovered while executing prompt-related work
orders. Update it as soon as issues are identified.

## Open Issues

### Test instability: database concurrency test

- Area: `internal/database`
- Symptom: `make test` fails on `TestAtomicClaimTask_ConcurrentClaimSingleWinner`
  with `context deadline exceeded`
- Observed during: WO-002, WO-003, WO-004
- Latest status: not reproduced during WO-006; treat as intermittent until the
  test is proven stable across repeated full-suite runs
- Impact: full repository test runs are not currently green even when the
  prompt/planning work itself is passing

### Sandbox-sensitive test: llm client test server bind

- Area: `internal/llm`
- Symptom: `make test` fails on `TestClient_Complete` because `httptest`
  cannot bind a local port in the current sandbox
- Observed during: WO-002, WO-003, WO-004, WO-006, WO-007, WO-008, WO-009, WO-010
- Impact: full repository test runs are environment-sensitive; focused tests
  are required to verify prompt/planning changes in restricted environments

### Sandbox-sensitive Go module stat-cache warning

- Area: Go toolchain / module cache behavior in restricted environments
- Symptom: `make build` succeeds but emits a non-fatal `go: writing stat cache`
  permission warning against `/home/gernsback/go/pkg/mod/cache/download/...`
- Observed during: WO-003, WO-004, WO-005, WO-006, WO-007, WO-008, WO-009, WO-010
- Impact: repository builds are still successful, but logs are noisy and can
  obscure real build failures in sandboxed runs
