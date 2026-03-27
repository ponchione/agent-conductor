# Structural Intelligence Audit

Date: 2026-03-23

Scope:
- Compared `structural-intelligence-spec-v1.md`
- Compared `docs/superpowers/plans/*.md`
- Audited the implemented code in `internal/graph`, `internal/context`, `internal/config`, and `cmd/conductor`
- Verified current test status with `make test`

Result:
- `make test` passes
- The implementation has several material gaps against the spec and generated plans

## Findings

### 1. TypeScript and Python IMPORTS edges cannot be stored in graph.db

Severity: High

Why it matters:
- `edges.source_id` is constrained to reference `symbols(id)`.
- The TypeScript and Python analyzers emit `IMPORTS` edges whose source is a synthetic module symbol.
- Those module symbols are not inserted into `symbols`.
- On a real TS or Python project with imports, graph persistence can fail with a foreign key error.

Code:
- `internal/graph/store.go`: `edges.source_id` references `symbols(id)`
- `internal/graph/ts-analyzer/analyze.ts`: import edges use module-shaped `source_id`
- `internal/graph/python_analyzer.go`: import edges use module-shaped `source_id`
- Neither analyzer emits module symbols into `symbols`

References:
- `internal/graph/store.go:35`
- `internal/graph/store.go:37`
- `internal/graph/ts-analyzer/analyze.ts:332`
- `internal/graph/python_analyzer.go:380`
- `internal/graph/ts-analyzer/analyze.ts:90`
- `internal/graph/python_analyzer.go:102`

Spec mismatch:
- The spec allows module-like relationships, but the implemented schema and emitted symbol set do not line up cleanly.

### 2. Scope blast-radius lookup is ambiguous because it uses symbol names instead of symbol IDs

Severity: High

Why it matters:
- Scope assembly fetches symbols for a file, then calls blast radius using `sym.Name`.
- The graph layer resolves names by returning the first same-name symbol it finds.
- Repeated names like `Run`, `Create`, `New`, `Handle`, or `Close` are common.
- Structural context can silently describe the wrong symbol and therefore the wrong blast radius.

Code:
- `internal/context/assembly.go`: passes `sym.Name` to blast radius
- `internal/graph/blast.go`: `resolveTarget` falls back to `GetSymbolsByName` and picks the first match

References:
- `internal/context/assembly.go:523`
- `internal/context/assembly.go:681`
- `internal/graph/blast.go:55`
- `internal/graph/blast.go:65`

Spec mismatch:
- The spec allows fuzzy matching for direct query inputs, but scope integration should already have exact symbol identity after `GetSymbolsForFile`.
- The current wiring throws away that exact identity.

### 3. The TypeScript analyzer emits method call edges to nonexistent function IDs

Severity: Medium-High

Why it matters:
- Method symbols are stored as `method` with a qualified name like `Class.method`.
- Call resolution classifies most non-class call targets as `function`.
- Calls to methods can therefore point to `ts:...:function:methodName` instead of `ts:...:method:Class.method`.
- That breaks downstream traversal and undercuts class-heavy TypeScript projects.

Code:
- `internal/graph/ts-analyzer/analyze.ts`: method symbols are emitted as `method`
- `internal/graph/ts-analyzer/analyze.ts`: call-edge target kind is `class` or `function`, not `method`

References:
- `internal/graph/ts-analyzer/analyze.ts:150`
- `internal/graph/ts-analyzer/analyze.ts:303`

Spec mismatch:
- The spec explicitly calls for `MethodDeclaration -> kind=method` and type-checker-resolved `CALLS` edges.
- The implemented edge IDs are not consistent with the implemented symbol IDs.

### 4. Boundary symbols are excluded from blast-radius results instead of being surfaced as terminal nodes

Severity: Medium

Why it matters:
- The spec says boundary symbols should appear in blast radius results but not be expanded further.
- The current downstream query filters them out before selection.
- The final result only joins `symbols`, never `boundary_symbols`.
- Context like stdlib and third-party dependencies is lost from structural output.

Code:
- `internal/graph/blast.go`: downstream query excludes `boundary_symbols`
- `internal/graph/blast.go`: final select joins only `symbols`

References:
- `internal/graph/blast.go:141`
- `internal/graph/blast.go:153`
- `internal/graph/blast.go:163`

Spec mismatch:
- `structural-intelligence-spec-v1.md` says boundary nodes should be included as terminal results.

Spec reference:
- `structural-intelligence-spec-v1.md:183`
- `structural-intelligence-spec-v1.md:185`
- `structural-intelligence-spec-v1.md:400`

### 5. Phase 3 chunk mapping and RAG cross-referencing are still unimplemented

Severity: Medium

Why it matters:
- The spec and plans describe a three-phase index flow, with symbol-to-chunk linking after graph and RAG indexing.
- The implementation logs that chunk mapping is skipped.
- Scope prompt assembly also does not annotate RAG results with any graph-derived structural importance.
- This leaves a planned major part of the feature incomplete.

Code:
- `cmd/conductor/index.go`: link phase is explicitly skipped
- `internal/context/assembly.go`: RAG results are rendered without graph-derived annotations

References:
- `cmd/conductor/index.go:80`
- `cmd/conductor/index.go:83`
- `internal/context/assembly.go:219`

Spec mismatch:
- The spec requires `chunk_mapping` population during index and structural enrichment of RAG results.

Spec reference:
- `structural-intelligence-spec-v1.md:169`
- `structural-intelligence-spec-v1.md:179`
- `structural-intelligence-spec-v1.md:503`
- `structural-intelligence-spec-v1.md:552`

### 6. User-facing config/docs are not aligned with the new graph config contract

Severity: Medium

Why it matters:
- The implementation adds `graph` config support in code.
- The checked-in `project.yaml` has no `graph:` section.
- `project.template.yaml`, `README.md`, and `work-order.template.yaml` also do not document the new contract.
- In this repo, the feature is effectively off by default unless users discover the hidden config surface.

Code and docs:
- `internal/config/config.go`: graph config types and defaults exist
- `project.yaml`: no `graph:` section
- `project.template.yaml`: no `graph:` section

References:
- `internal/config/config.go:28`
- `project.yaml:1`
- `project.template.yaml:1`

Spec mismatch:
- The repo instructions say user-visible config changes should keep docs/templates aligned.
- The spec explicitly defines the `graph:` config shape.

Spec reference:
- `structural-intelligence-spec-v1.md:509`

## Coverage Gaps

The current tests did not catch the most important issues:

- TS/Python analyzer tests validate extraction in memory, but do not store analyzer output through `GraphStore`, so the foreign key failure path is untested.
- Scope assembly tests use a mock graph querier and do not exercise duplicate symbol-name resolution.
- Blast-radius tests validate current behavior, but they encode boundary-symbol exclusion as expected behavior rather than the spec behavior.

## Verification

Command run:

```bash
make test
```

Observed result:
- Passed

That passing result does not invalidate the findings above; the main gaps are contract mismatches and insufficient integration coverage rather than currently failing unit tests.
