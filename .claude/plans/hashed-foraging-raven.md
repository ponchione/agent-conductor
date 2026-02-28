# WO-1: TypeScript/TSX Tree-Sitter Parser for RAG Indexing

## Context

The RAG indexer currently only produces structural chunks (named functions, methods, types) for Go files. TypeScript/TSX files fall through to the sliding-window fallback, producing anonymous 40-line blobs with names like `path/file.ts:1-40`. This WO adds tree-sitter parsing for `.ts` and `.tsx` files so the index contains properly named, typed chunks — a prerequisite for using the conductor on any TS/React project.

## Files to Modify

| File | Change |
|------|--------|
| `go.mod` / `go.sum` | Add `github.com/tree-sitter/tree-sitter-typescript` dependency |
| `internal/rag/indexer.go` | Add `.ts` → `"typescript"` and `.tsx` → `"tsx"` to `langFromExt` |
| `internal/rag/parser.go` | Add `"typescript"` and `"tsx"` cases to `ParseFile` dispatch, add TS grammar import |
| `internal/rag/tsparser.go` | **NEW** — all TS/TSX parsing logic |
| `internal/rag/tsparser_test.go` | **NEW** — unit tests |

**Not touched:** `goparser.go`, `types.go`, `store.go`, LanceDB schema, describer, embedder.

## Step 1: Add dependency — `go.mod`

```bash
go get github.com/tree-sitter/tree-sitter-typescript@latest
go mod tidy
```

## Step 2: Extension mapping — `internal/rag/indexer.go`

Add two cases to `langFromExt` (line ~337):

```go
case ".ts":
    return "typescript"
case ".tsx":
    return "tsx"
```

## Step 3: Dispatch — `internal/rag/parser.go`

Add import:
```go
ts "github.com/tree-sitter/tree-sitter-typescript/bindings/go"
```

Add cases to `ParseFile` switch (line ~28):
```go
case "typescript":
    return parseTypeScript(content, false)
case "tsx":
    return parseTypeScript(content, true)
```

Note: the `ts` import is used in `tsparser.go` (same package), but adding it to `parser.go` keeps the grammar imports co-located. Actually — since `tsparser.go` is the same package, the import only needs to be in the file that references it. Place the import in `tsparser.go` where `ts.LanguageTypescript()` and `ts.LanguageTSX()` are called.

## Step 4: TypeScript parser — `internal/rag/tsparser.go` (NEW)

Follows the exact pattern of `parseGo` in `parser.go`. Key functions:

### `parseTypeScript(content []byte, isTSX bool) ([]RawChunk, error)`
- Create `sitter.NewParser()`, set language based on `isTSX`
- Parse content, iterate root node children
- Dispatch each child to `extractTSNode`

### `extractTSNode(node *sitter.Node, content []byte) []RawChunk`
Switch on `node.Kind()`:
- `"function_declaration"` → extract name from `"name"` field, sig up to body, chunkType=`"function"`
- `"class_declaration"` → name field, sig up to `{`, chunkType=`"class"`
- `"interface_declaration"` → name field, sig up to `{`, chunkType=`"interface"`
- `"type_alias_declaration"` → name field, full text as both sig and body, chunkType=`"type"`
- `"enum_declaration"` → name field, sig up to `{`, chunkType=`"enum"`
- `"export_statement"` → unwrap: iterate children, recurse on declaration children, skip bare identifier/expression exports
- `"lexical_declaration"` → iterate `variable_declarator` children, check if `value` is `arrow_function` or `function`, extract name from declarator's `name` field, chunkType=`"function"`

### Signature extraction helpers
Reuse the same byte-offset pattern from `extractGoSignature`:
- For declarations with a body: `content[node.StartByte():bodyNode.StartByte()]`
- For arrow functions: reconstruct `const name = (params): returnType =>`
- For type aliases: entire declaration text (no body block)

### Arrow function detail
Tree-sitter AST for `const foo = (x: number) => { ... }`:
```
lexical_declaration
  variable_declarator
    name: identifier ("foo")
    value: arrow_function
      parameters: formal_parameters
      body: statement_block
```

Logic:
1. Iterate `variable_declarator` children of `lexical_declaration`
2. Get `name` field from `variable_declarator` → chunk name
3. Get `value` field → check if Kind is `"arrow_function"` or `"function"`
4. If yes, build RawChunk with name from declarator, body/sig from the whole declaration
5. If no (plain const, object, etc.), skip

### Export unwrapping detail
```
export_statement
  declaration: function_declaration / class_declaration / lexical_declaration / ...
```

Logic:
1. Iterate children of `export_statement`
2. Skip `"export"` keyword nodes and `"default"` keyword nodes
3. If child is a known declaration kind, recurse via `extractTSNode`
4. If child is just an identifier or expression (e.g. `export default Foo`), skip

## Step 5: Tests — `internal/rag/tsparser_test.go` (NEW)

### Test fixtures (inline string constants)

**TS fixture** — covers function, class, interface, type alias, enum, exported arrow function:
```typescript
function greet(name: string): string { return `Hello ${name}` }
class UserService { constructor(private repo: UserRepo) {} }
interface UserRepo { findById(id: string): Promise<User> }
type Status = "active" | "inactive"
enum Role { Admin, User, Guest }
export const handler = async (req: Request): Promise<Response> => { return new Response() }
const internalHelper = (x: number): number => { return x * 2 }
```

**TSX fixture** — React component as arrow function:
```tsx
import React from 'react'
interface Props { title: string }
export const MyComponent = ({ title }: Props) => { return <div>{title}</div> }
function helper(): string { return "hello" }
```

### Test cases

1. **TestParseTypeScript_AllConstructs** — parse TS fixture, assert:
   - 7 chunks produced (greet, UserService, UserRepo, Status, Role, handler, internalHelper)
   - Each has correct chunkType
   - Each has non-empty Name and Signature

2. **TestParseTypeScript_TSX** — parse TSX fixture with `isTSX=true`, assert:
   - Chunks for Props (interface), MyComponent (function), helper (function)
   - MyComponent chunk exists with correct name

3. **TestParseTypeScript_ArrowFunctionName** — verify arrow function gets variable name:
   - Parse `export const handler = async (req: Request) => { ... }`
   - Assert chunk name is `"handler"`, not empty

4. **TestParseTypeScript_ExportUnwrap** — verify export doesn't duplicate or lose declarations:
   - Parse `export function foo() {}` and `export default class Bar {}`
   - Assert chunks named "foo" and "Bar"

5. **TestParseTypeScript_EmptyFile** — empty content returns no chunks, no error

## Verification

1. `go vet ./internal/rag/...` — passes
2. `CGO_CFLAGS=... go test ./internal/rag/... -run TestParseTypeScript -v` — all pass
3. `make build` — passes
