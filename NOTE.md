# Where We Left Off — 2026-03-21

## Completed (All 8 UI Specs)

- **Spec 01: Foundation & App Shell** — `01d6e14`
- **Spec 02: Pipeline Space (Read-Only)** — `5e1398a`
- **Spec 03: Pipeline Interactive** — `baa2dd5`
- **Spec 04: Queue System** — `c7e0b50`
- **Spec 05: Plan Space** — `e447a8d`
- **Spec 06: Config & Overrides** — `b41ef00`
- **Spec 07: Analytics Space** — `5f24572`
- **Spec 08: CLI Lock Mechanism** — `e194f0e`
- **Audit Fixes** — `3fda9c3`

## Pre-Push Audit Results

Full audit of Specs 04-08 was performed. All blocking issues were fixed in `3fda9c3`. Below are the remaining known issues organized by spec.

### Spec 04: Queue System

**Fixed in audit:**
- addQueueItems client was sending raw array instead of `{ items: [...] }` — every add-to-queue call from the frontend would 400
- QueueState.current was typed as QueueItem in TypeScript but backend sends a string ID — QueueStrip never showed the executing item title
- Data race on RunQueue.subscribers map — broadcast iterated the map without holding a lock while Subscribe/Unsubscribe mutated it concurrently. Fixed by adding a separate RWMutex for subscriber management

**Known issues (not fixed):**
- Reorder endpoint uses POST instead of spec-required PUT (functional, spec deviation)
- GetState shallow-copies items but shares Overrides map references (potential corruption if snapshot mutated)
- Clear Queue fires N serial DELETE requests without await (UI flicker, potential race)
- Per-item override dropdowns in queue drawer are read-only (disabled) — spec implies interactive editing
- SSE events lack `event:` field in protocol (all arrive as generic `message` events)
- Frontend polls every 3s instead of using the queue SSE endpoint (acceptable per spec)

### Spec 05: Plan Space

**Fixed in audit:**
- PlanSpace Outlet conditional prevented /plan/new from rendering — NewPlan page was unreachable
- Work order response JSON tag was `mod_time` but frontend expected `modified_at`

**Known issues (not fixed):**
- POST /api/plan creates a session but background plan execution is a TODO stub — no work orders are actually generated
- No SSE integration for plan generation progress in NewPlan — progress display is static
- CodeViewer placeholder prop is accepted but non-functional (no @codemirror/view placeholder extension used)
- PlanSpace does not show StatusBadge per run (only shows "audited" badge)
- PlanRunDetail missing spec-required metrics: Generation Cost, Audit Cost, Total Duration, model labels
- CodeViewer only supports YAML highlighting — json and markdown language extensions not loaded
- Work order count in list shows work_orders_generated instead of post_audit_count

### Spec 06: Config & Overrides

**Known issues (not fixed):**
- **Override system is hollow.** ValidateOverrides only accepts the single model already configured on each provider. The ProviderConfig struct has one Model field, not a list. The spec example shows multiple models per provider (e.g., claude-cli offering both opus and sonnet), but the data model can't represent this. Overrides work for switching a role to a different *provider*, but not for switching models within a provider. Fix requires adding a Models []string or AvailableModels []string field to ProviderConfig, or hardcoding known models per provider type.
- **ResolveModel is not wired into the pipeline execution path.** The function exists, is correct, and is tested, but nothing in internal/worker/ calls it. Setting overrides via the UI has no effect on actual pipeline runs. The override UI (set, get, clear, validate, display) works correctly — it's a working control panel connected to nothing. Fix requires wiring ResolveModel (or the OverrideStore) into the worker's role resolution where LLM clients are selected.
- Connection status hardcoded to "Connected" / green — no SSE state monitoring
- Per-item queue drawer overrides are display-only (disabled dropdowns)
- QueueItem.Overrides uses Record<string, string> while config uses Record<string, ModelOverride> — fragile "provider::model" encoding

### Spec 07: Analytics Space

**Known issues (not fixed):**
- Project filter dropdown is static — only "All Projects" option, no dynamic project list populated
- GetStatsModels endpoint does not accept a project filter parameter (spec only documents range, but creates UI inconsistency when project filter is selected)
- Audit chart clamps negative deltas to zero in the stacked visual (delta label still correct, but bar doesn't shrink below pre-audit height)
- Verify distribution pie counts runs with NULL verify_result as "ERROR" — should filter them out
- Test coverage only covers empty-database scenarios — no tests with seeded data verifying SQL aggregation correctness
- Loading state uses plain text "Loading..." instead of shadcn/ui Skeleton components

### Spec 08: CLI Lock Mechanism

**Known issues (not fixed):**
- When serve is invoked with --db flag only (no --data-dir or project config), dataDir is empty and lock protection is silently skipped — two serve instances can run simultaneously
- net.SplitHostPort and strconv.Atoi errors silently ignored when parsing serveAddr — malformed addr results in port 0 in lock file
- Corrupt lock file handling (JSON parse failure → cleanup) exists but is not tested

## Build Verification

All passing as of end of session:
- `make build` — pass
- `make test` — pass (all 23 packages, 0 failures)
- `cd web && npm run build` — pass
