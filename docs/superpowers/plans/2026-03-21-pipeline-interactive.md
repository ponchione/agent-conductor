# Pipeline Interactive (Spec 03) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add interactive capabilities to the Pipeline space: approve/reject workflows, view build output, diffs, verification results, and scope context packages.

**Architecture:** Two parallel tracks — Backend (Go API endpoints) and Frontend (React components). Backend adds 4 new endpoints to the existing chi router, wiring into existing `gate.Approve/Reject` logic and `exec.Command` git operations. Frontend installs 3 npm packages, adds TypeScript types/client methods, then builds 5 new page/component files and enhances 2 existing ones.

**Tech Stack:** Go (chi router, database/sql, os/exec), React 18, TypeScript, Tailwind CSS v4, shadcn/ui, react-diff-viewer-continued, react-window

**Spec source:** `docs/specs/UI/03-pipeline-interactive/`

---

## File Structure

### Backend (Track A) — New Files
- `internal/api/diff.go` — Git diff shell helper + HTTP handler
- `internal/api/scope.go` — Context package reader + HTTP handler
- `internal/api/approve_reject.go` — Approve/reject HTTP handlers wrapping gate logic

### Backend (Track A) — Modified Files
- `internal/api/server.go` — Add `*git.GitManager` + `baseBranch` to Server struct, register 4 new routes, update `NewServer` signature
- `internal/api/response.go` — Add diff/scope/approve/reject response types
- `internal/api/server_test.go` — Add tests for new endpoints
- `cmd/conductor/serve.go` — Pass GitManager and baseBranch to `NewServer`

### Frontend (Track B) — New Files
- `web/src/components/TerminalViewer.tsx` — Virtualized terminal with auto-scroll and scroll-lock
- `web/src/components/DiffModal.tsx` — Near-full-screen diff viewer with split/unified toggle
- `web/src/pages/WorkflowBuild.tsx` — Build tab with live SSE streaming + historical log
- `web/src/pages/WorkflowVerify.tsx` — Verdict, precheck cards, LLM analysis
- `web/src/pages/WorkflowScope.tsx` — Collapsible JSON tree viewer

### Frontend (Track B) — Modified Files
- `web/src/types/api.ts` — Add 4 new response interfaces
- `web/src/api/client.ts` — Add 4 new API client functions (2 GET, 2 POST)
- `web/src/pages/WorkflowDetail.tsx` — Enable approve/reject buttons, replace tab placeholders with real components
- `web/src/pages/WorkflowOverview.tsx` — Add review summary section for human_review workflows

---

## Track A: Backend

### Task A1: Expand Server struct and update NewServer signature

**Files:**
- Modify: `internal/api/server.go:30-48`
- Modify: `internal/api/server_test.go` (all `NewServer(db)` calls)
- Modify: `cmd/conductor/serve.go:43`

The Server needs a `*git.GitManager` and `baseBranch` for approve and diff endpoints. The GitManager's methods never reference `g.cfg`, so it can be instantiated with `nil` config where full config isn't available.

- [ ] **Step 1: Update Server struct and NewServer**

In `internal/api/server.go`, change:

```go
type Server struct {
	db         *database.DB
	gitMgr     *git.GitManager
	baseBranch string
}

func NewServer(db *database.DB, gitMgr *git.GitManager, baseBranch string) http.Handler {
	s := &Server{db: db, gitMgr: gitMgr, baseBranch: baseBranch}
	// ... existing routes unchanged ...
}
```

Add imports: `"github.com/ponchione/agent-conductor/internal/git"`

- [ ] **Step 2: Update all NewServer call sites**

In `internal/api/server_test.go`, update every `NewServer(db)` to `NewServer(db, nil, "main")`. There are ~14 call sites — use find-and-replace.

In `cmd/conductor/serve.go`, update to:
```go
import "github.com/ponchione/agent-conductor/internal/git"

// Inside RunE, after db open:
gitMgr := git.New(nil)
baseBranch := "main"
// If project config is loaded, use its base branch:
if cfg != nil && cfg.Git.BaseBranch != "" {
    baseBranch = cfg.Git.BaseBranch
}
if err := http.ListenAndServe(serveAddr, api.NewServer(db, gitMgr, baseBranch)); err != nil {
```

Note: `cfg` is a package-level var in cmd/conductor. Check if it's populated during serve. If `loadProjectConfig()` was called (lines 64-75), cfg is available. If not (bare --db mode), default to "main". The GitManager works with nil config since its methods don't use it.

- [ ] **Step 3: Verify build**

Run: `make build`
Expected: PASS

- [ ] **Step 4: Verify tests**

Run: `make test`
Expected: PASS — all existing tests should pass with updated NewServer signature

---

### Task A2: Add diff response types and git diff helper

**Files:**
- Modify: `internal/api/response.go` (append response types)
- Create: `internal/api/diff.go`

- [ ] **Step 1: Add diff response types to response.go**

Append to `internal/api/response.go`:

```go
// --- Diff response types ---

type diffStatsResponse struct {
	Additions int `json:"additions"`
	Deletions int `json:"deletions"`
	Files     int `json:"files"`
}

type workflowDiffResponse struct {
	Diff           string            `json:"diff"`
	BaseBranch     string            `json:"base_branch"`
	WorkflowBranch string            `json:"workflow_branch"`
	FilesChanged   []string          `json:"files_changed"`
	Stats          diffStatsResponse `json:"stats"`
}
```

- [ ] **Step 2: Create diff.go with git diff helper and handler**

Create `internal/api/diff.go`:

```go
package api

import (
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"os/exec"
	"regexp"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
)

// gitDiffResult holds the output of a three-dot git diff.
type gitDiffResult struct {
	Diff         string
	FilesChanged []string
	Additions    int
	Deletions    int
}

// errBranchNotFound is returned when the workflow branch no longer exists.
var errBranchNotFound = errors.New("branch not found")

// runGitDiff runs git diff between base and workflow branch using three-dot notation.
func runGitDiff(repoPath, baseBranch, workflowBranch string) (gitDiffResult, error) {
	// Check branch exists first.
	if err := runGit(repoPath, "rev-parse", "--verify", "refs/heads/"+workflowBranch); err != nil {
		return gitDiffResult{}, errBranchNotFound
	}

	// Full diff.
	diff, err := runGitOutput(repoPath, "diff", baseBranch+"..."+workflowBranch)
	if err != nil {
		return gitDiffResult{}, fmt.Errorf("git diff: %w", err)
	}

	// File list.
	nameOnly, err := runGitOutput(repoPath, "diff", "--name-only", baseBranch+"..."+workflowBranch)
	if err != nil {
		return gitDiffResult{}, fmt.Errorf("git diff --name-only: %w", err)
	}
	var files []string
	for _, f := range strings.Split(strings.TrimSpace(nameOnly), "\n") {
		if f != "" {
			files = append(files, f)
		}
	}

	// Stats.
	additions, deletions := parseDiffStats(repoPath, baseBranch, workflowBranch)

	return gitDiffResult{
		Diff:         diff,
		FilesChanged: files,
		Additions:    additions,
		Deletions:    deletions,
	}, nil
}

var diffStatLineRe = regexp.MustCompile(`(\d+) files? changed(?:, (\d+) insertions?)?(?:, (\d+) deletions?)?`)

func parseDiffStats(repoPath, baseBranch, workflowBranch string) (additions, deletions int) {
	stat, err := runGitOutput(repoPath, "diff", "--stat", baseBranch+"..."+workflowBranch)
	if err != nil {
		return 0, 0
	}
	lines := strings.Split(stat, "\n")
	if len(lines) == 0 {
		return 0, 0
	}
	summary := lines[len(lines)-1]
	if summary == "" && len(lines) > 1 {
		summary = lines[len(lines)-2]
	}
	m := diffStatLineRe.FindStringSubmatch(summary)
	if m == nil {
		return 0, 0
	}
	if m[2] != "" {
		additions, _ = strconv.Atoi(m[2])
	}
	if m[3] != "" {
		deletions, _ = strconv.Atoi(m[3])
	}
	return additions, deletions
}

func runGit(repoPath string, args ...string) error {
	cmd := exec.Command("git", args...)
	cmd.Dir = repoPath
	_, err := cmd.Output()
	return err
}

func runGitOutput(repoPath string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = repoPath
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
}

func (s *Server) handleGetWorkflowDiff(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	wf, _, _, err := s.db.GetWorkflowDetailForUI(r.Context(), id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "workflow not found")
			return
		}
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("get workflow: %v", err))
		return
	}

	result, err := runGitDiff(wf.TargetRepo, s.baseBranch, wf.GitBranch)
	if err != nil {
		if errors.Is(err, errBranchNotFound) {
			writeError(w, http.StatusNotFound, "branch no longer exists")
			return
		}
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("git diff: %v", err))
		return
	}

	writeJSON(w, http.StatusOK, workflowDiffResponse{
		Diff:           result.Diff,
		BaseBranch:     s.baseBranch,
		WorkflowBranch: wf.GitBranch,
		FilesChanged:   result.FilesChanged,
		Stats: diffStatsResponse{
			Additions: result.Additions,
			Deletions: result.Deletions,
			Files:     len(result.FilesChanged),
		},
	})
}
```

- [ ] **Step 3: Register route in server.go**

In `NewServer`, add before the SPA fallback route:
```go
r.Get("/api/workflows/{id}/diff", s.handleGetWorkflowDiff)
```

- [ ] **Step 4: Verify build**

Run: `make build`
Expected: PASS

---

### Task A3: Add scope endpoint

**Files:**
- Modify: `internal/api/response.go` (append scope response type)
- Create: `internal/api/scope.go`

- [ ] **Step 1: Add scope response type to response.go**

Append to `internal/api/response.go`:

```go
// --- Scope response types ---

type workflowScopeResponse struct {
	ContextPackage json.RawMessage `json:"context_package"`
	Source         string          `json:"source"`
}
```

Add `"encoding/json"` to imports if not already present (it is).

- [ ] **Step 2: Create scope.go with handler**

Create `internal/api/scope.go`:

```go
package api

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"

	"github.com/go-chi/chi/v5"
	"github.com/ponchione/agent-conductor/internal/database"
)

func (s *Server) handleGetWorkflowScope(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	wf, _, _, err := s.db.GetWorkflowDetailForUI(r.Context(), id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "workflow not found")
			return
		}
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("get workflow: %v", err))
		return
	}

	// Try loading context package from file path on the workflow.
	contextPackagePath := s.getWorkflowContextPackagePath(r.Context(), id)
	if contextPackagePath != "" {
		data, err := os.ReadFile(contextPackagePath)
		if err == nil {
			// Validate it's JSON.
			if json.Valid(data) {
				writeJSON(w, http.StatusOK, workflowScopeResponse{
					ContextPackage: json.RawMessage(data),
					Source:         "file",
				})
				return
			}
		}
	}

	// Fallback: check artifacts table for context_package type.
	artifact, err := s.db.GetLatestArtifactByType(r.Context(), database.ArtifactTypeContextPackage, "", id)
	if err == nil && artifact.Path != "" {
		data, err := os.ReadFile(artifact.Path)
		if err == nil && json.Valid(data) {
			writeJSON(w, http.StatusOK, workflowScopeResponse{
				ContextPackage: json.RawMessage(data),
				Source:         "artifact",
			})
			return
		}
	}

	writeError(w, http.StatusNotFound, "no scope context package available")
}

// getWorkflowContextPackagePath fetches the context_package_path for a workflow.
func (s *Server) getWorkflowContextPackagePath(ctx context.Context, workflowID string) string {
	wf, err := s.db.GetWorkflow(ctx, workflowID)
	if err != nil {
		return ""
	}
	if wf.ContextPackagePath.Valid {
		return wf.ContextPackagePath.String
	}
	return ""
}
```

Note: `s.db.GetWorkflow` is the sqlc-generated method returning the full workflow record including `context_package_path`. Import `"context"` in scope.go.

- [ ] **Step 3: Register route in server.go**

In `NewServer`, add:
```go
r.Get("/api/workflows/{id}/scope", s.handleGetWorkflowScope)
```

- [ ] **Step 4: Verify build**

Run: `make build`
Expected: PASS

---

### Task A4: Add approve and reject endpoints

**Files:**
- Modify: `internal/api/response.go` (append approve/reject response types)
- Create: `internal/api/approve_reject.go`

- [ ] **Step 1: Add approve/reject response types to response.go**

Append to `internal/api/response.go`:

```go
// --- Approve/Reject response types ---

type approveRequest struct {
	Reindex bool `json:"reindex"`
}

type approveResponse struct {
	Status     string `json:"status"`
	WorkflowID string `json:"workflow_id"`
	Merged     bool   `json:"merged"`
	Reindexed  bool   `json:"reindexed"`
}

type rejectRequest struct {
	Reason string `json:"reason"`
}

type rejectResponse struct {
	Status     string `json:"status"`
	WorkflowID string `json:"workflow_id"`
}
```

- [ ] **Step 2: Create approve_reject.go with handlers**

Create `internal/api/approve_reject.go`:

```go
package api

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/ponchione/agent-conductor/internal/gate"
)

func (s *Server) handleApproveWorkflow(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	if s.gitMgr == nil {
		writeError(w, http.StatusServiceUnavailable, "approve not available: server started without git configuration")
		return
	}

	var req approveRequest
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&req)
	}

	// Look up workflow to get target_repo and validate state.
	wf, _, _, err := s.db.GetWorkflowDetailForUI(r.Context(), id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "workflow not found")
			return
		}
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("get workflow: %v", err))
		return
	}

	if wf.CurrentState != "human_review" {
		writeError(w, http.StatusConflict, fmt.Sprintf("workflow is in state %q, expected human_review", wf.CurrentState))
		return
	}

	if err := gate.Approve(r.Context(), s.db, s.gitMgr, id, wf.TargetRepo, s.baseBranch); err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("approve failed: %v", err))
		return
	}

	writeJSON(w, http.StatusOK, approveResponse{
		Status:     "approved",
		WorkflowID: id,
		Merged:     true,
		Reindexed:  false, // Reindex is not wired in the API server context.
	})
}

func (s *Server) handleRejectWorkflow(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	var req rejectRequest
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&req)
	}

	// Look up workflow to validate state.
	wf, _, _, err := s.db.GetWorkflowDetailForUI(r.Context(), id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "workflow not found")
			return
		}
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("get workflow: %v", err))
		return
	}

	if wf.CurrentState != "human_review" {
		writeError(w, http.StatusConflict, fmt.Sprintf("workflow is in state %q, expected human_review", wf.CurrentState))
		return
	}

	reason := strings.TrimSpace(req.Reason)
	if err := gate.Reject(r.Context(), s.db, id, reason); err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("reject failed: %v", err))
		return
	}

	writeJSON(w, http.StatusOK, rejectResponse{
		Status:     "rejected",
		WorkflowID: id,
	})
}
```

- [ ] **Step 3: Register routes in server.go**

In `NewServer`, add:
```go
r.Post("/api/workflows/{id}/approve", s.handleApproveWorkflow)
r.Post("/api/workflows/{id}/reject", s.handleRejectWorkflow)
```

- [ ] **Step 4: Verify build**

Run: `make build`
Expected: PASS

---

### Task A5: Backend tests for new endpoints

**Files:**
- Modify: `internal/api/server_test.go`

- [ ] **Step 1: Add test for diff endpoint (branch not found returns 404)**

```go
func TestGetWorkflowDiffEndpointNotFound(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/workflows/nonexistent/diff", nil)
	NewServer(db, nil, "main").ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}
```

- [ ] **Step 2: Add test for scope endpoint (no context package returns 404)**

```go
func TestGetWorkflowScopeEndpointNoPackage(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	ctx := context.Background()
	if err := db.CreateWorkflow(ctx, database.CreateWorkflowParams{
		ID: "wf-scope-none", OriginalIntent: "test", OriginalFile: "/tmp/wo.yaml",
		CurrentState: "completed", TargetRepo: "/tmp", GitBranch: "feat/test",
		MaxDepth: 3, MaxFilesChanged: 10, MaxDurationMins: 30,
	}); err != nil {
		t.Fatalf("CreateWorkflow() error: %v", err)
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/workflows/wf-scope-none/scope", nil)
	NewServer(db, nil, "main").ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}
```

- [ ] **Step 3: Add test for scope endpoint (file source)**

```go
func TestGetWorkflowScopeEndpointFromFile(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	ctx := context.Background()
	tmpDir := t.TempDir()
	cpPath := filepath.Join(tmpDir, "context_package.json")
	if err := os.WriteFile(cpPath, []byte(`{"work_order":{"title":"test"}}`), 0644); err != nil {
		t.Fatalf("WriteFile() error: %v", err)
	}
	if err := db.CreateWorkflow(ctx, database.CreateWorkflowParams{
		ID: "wf-scope-file", OriginalIntent: "test", OriginalFile: "/tmp/wo.yaml",
		CurrentState: "completed", TargetRepo: tmpDir, GitBranch: "feat/test",
		ContextPackagePath: sql.NullString{String: cpPath, Valid: true},
		MaxDepth: 3, MaxFilesChanged: 10, MaxDurationMins: 30,
	}); err != nil {
		t.Fatalf("CreateWorkflow() error: %v", err)
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/workflows/wf-scope-file/scope", nil)
	NewServer(db, nil, "main").ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	var payload workflowScopeResponse
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload.Source != "file" {
		t.Fatalf("source = %q, want file", payload.Source)
	}
}
```

- [ ] **Step 4: Add test for reject endpoint (wrong state returns 409)**

```go
func TestRejectWorkflowEndpointWrongState(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	ctx := context.Background()
	if err := db.CreateWorkflow(ctx, database.CreateWorkflowParams{
		ID: "wf-reject-wrong", OriginalIntent: "test", OriginalFile: "/tmp/wo.yaml",
		CurrentState: "completed", TargetRepo: "/tmp", GitBranch: "feat/test",
		MaxDepth: 3, MaxFilesChanged: 10, MaxDurationMins: 30,
	}); err != nil {
		t.Fatalf("CreateWorkflow() error: %v", err)
	}
	body := strings.NewReader(`{"reason":"bad code"}`)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/workflows/wf-reject-wrong/reject", body)
	NewServer(db, nil, "main").ServeHTTP(rec, req)
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusConflict)
	}
}
```

- [ ] **Step 5: Add test for approve endpoint without gitMgr returns 503**

```go
func TestApproveWorkflowEndpointNoGitMgr(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	ctx := context.Background()
	if err := db.CreateWorkflow(ctx, database.CreateWorkflowParams{
		ID: "wf-approve-nogit", OriginalIntent: "test", OriginalFile: "/tmp/wo.yaml",
		CurrentState: "human_review", TargetRepo: "/tmp", GitBranch: "feat/test",
		MaxDepth: 3, MaxFilesChanged: 10, MaxDurationMins: 30,
	}); err != nil {
		t.Fatalf("CreateWorkflow() error: %v", err)
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/workflows/wf-approve-nogit/approve", nil)
	NewServer(db, nil, "main").ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusServiceUnavailable)
	}
}
```

- [ ] **Step 6: Run tests**

Run: `make test`
Expected: PASS

- [ ] **Step 7: Commit backend work**

```bash
git add internal/api/diff.go internal/api/scope.go internal/api/approve_reject.go \
       internal/api/server.go internal/api/response.go internal/api/server_test.go \
       cmd/conductor/serve.go
git commit -m "feat: add diff, scope, approve, reject API endpoints (Spec 03 backend)"
```

---

## Track B: Frontend

### Task B1: Install npm dependencies and add TypeScript types

**Files:**
- Modify: `web/package.json` (via npm install)
- Modify: `web/src/types/api.ts`
- Modify: `web/src/api/client.ts`

- [ ] **Step 1: Install npm packages**

```bash
cd web && npm install react-diff-viewer-continued@^4 react-window@^1 && npm install -D @types/react-window@^1
```

- [ ] **Step 2: Add TypeScript interfaces to api.ts**

Append to `web/src/types/api.ts`:

```typescript
// --- Spec 03: Interactive Pipeline types ---

export interface DiffStats {
  additions: number;
  deletions: number;
  files: number;
}

export interface WorkflowDiffResponse {
  diff: string;
  base_branch: string;
  workflow_branch: string;
  files_changed: string[];
  stats: DiffStats;
}

export interface WorkflowScopeResponse {
  context_package: Record<string, unknown>;
  source: string;
}

export interface ApproveResponse {
  status: string;
  workflow_id: string;
  merged: boolean;
  reindexed: boolean;
}

export interface RejectResponse {
  status: string;
  workflow_id: string;
}
```

- [ ] **Step 3: Add API client functions to client.ts**

Add imports for new types, then append functions:

```typescript
import type {
  // ... existing imports ...
  WorkflowDiffResponse,
  WorkflowScopeResponse,
  ApproveResponse,
  RejectResponse,
} from "../types/api";

export async function getWorkflowDiff(id: string): Promise<WorkflowDiffResponse> {
  return fetchJSON<WorkflowDiffResponse>(`/api/workflows/${encodeURIComponent(id)}/diff`);
}

export async function getWorkflowScope(id: string): Promise<WorkflowScopeResponse> {
  return fetchJSON<WorkflowScopeResponse>(`/api/workflows/${encodeURIComponent(id)}/scope`);
}

async function postJSON<T>(path: string, body: unknown): Promise<T> {
  const response = await fetch(path, {
    method: "POST",
    headers: {
      "Content-Type": "application/json",
      Accept: "application/json",
    },
    body: JSON.stringify(body),
  });
  const payload = (await response.json().catch(() => ({}))) as { error?: string };
  if (!response.ok) {
    throw new Error(payload.error || `Request failed: ${response.status}`);
  }
  return payload as T;
}

export async function approveWorkflow(
  id: string,
  options: { reindex: boolean }
): Promise<ApproveResponse> {
  return postJSON<ApproveResponse>(
    `/api/workflows/${encodeURIComponent(id)}/approve`,
    options
  );
}

export async function rejectWorkflow(
  id: string,
  options: { reason?: string }
): Promise<RejectResponse> {
  return postJSON<RejectResponse>(
    `/api/workflows/${encodeURIComponent(id)}/reject`,
    options
  );
}
```

- [ ] **Step 4: Verify frontend build**

Run: `cd web && npm run build`
Expected: PASS

- [ ] **Step 5: Commit foundation**

```bash
git add web/package.json web/package-lock.json web/src/types/api.ts web/src/api/client.ts
git commit -m "feat: add Spec 03 npm deps, TypeScript types, and API client functions"
```

---

### Task B2: TerminalViewer component

**Files:**
- Create: `web/src/components/TerminalViewer.tsx`

- [ ] **Step 1: Create TerminalViewer.tsx**

```tsx
import { useCallback, useEffect, useRef, useState } from "react";
import { FixedSizeList } from "react-window";
import { ArrowDown } from "lucide-react";
import { Button } from "@/components/ui/button";

interface TerminalViewerProps {
  lines: string[];
  streaming: boolean;
  maxLines?: number;
}

const LINE_HEIGHT = 20;

export function TerminalViewer({ lines, streaming, maxLines = 5000 }: TerminalViewerProps) {
  const listRef = useRef<FixedSizeList>(null);
  const outerRef = useRef<HTMLDivElement>(null);
  const [scrollLocked, setScrollLocked] = useState(false);
  const userScrolledRef = useRef(false);

  // Truncate lines from front if over maxLines.
  const offset = lines.length > maxLines ? lines.length - maxLines : 0;
  const visibleLines = lines.length > maxLines ? lines.slice(-maxLines) : lines;

  // Auto-scroll to bottom when new lines arrive (unless scroll-locked).
  useEffect(() => {
    if (streaming && !userScrolledRef.current && listRef.current) {
      listRef.current.scrollToItem(visibleLines.length - 1, "end");
    }
  }, [visibleLines.length, streaming]);

  const handleScroll = useCallback(
    ({ scrollOffset }: { scrollOffset: number }) => {
      if (!outerRef.current) return;
      const maxScroll = visibleLines.length * LINE_HEIGHT - outerRef.current.clientHeight;
      const atBottom = scrollOffset >= maxScroll - LINE_HEIGHT;
      if (atBottom) {
        userScrolledRef.current = false;
        setScrollLocked(false);
      } else if (streaming) {
        userScrolledRef.current = true;
        setScrollLocked(true);
      }
    },
    [visibleLines.length, streaming]
  );

  const jumpToBottom = () => {
    userScrolledRef.current = false;
    setScrollLocked(false);
    listRef.current?.scrollToItem(visibleLines.length - 1, "end");
  };

  const Row = ({ index, style }: { index: number; style: React.CSSProperties }) => (
    <div style={style} className="flex font-mono text-xs leading-5">
      <span className="w-12 shrink-0 select-none pr-2 text-right text-zinc-600">
        {index + offset + 1}
      </span>
      <span className="whitespace-pre text-zinc-300">{visibleLines[index]}</span>
    </div>
  );

  return (
    <div className="relative rounded-lg border border-border bg-[#1a1a2e] overflow-hidden">
      <FixedSizeList
        ref={listRef}
        outerRef={outerRef}
        height={400}
        width="100%"
        itemCount={visibleLines.length}
        itemSize={LINE_HEIGHT}
        onScroll={handleScroll}
        overscanCount={20}
      >
        {Row}
      </FixedSizeList>

      {scrollLocked && (
        <Button
          variant="secondary"
          size="sm"
          className="absolute bottom-3 right-3 shadow-lg"
          onClick={jumpToBottom}
        >
          <ArrowDown className="mr-1 size-3" />
          Jump to bottom
        </Button>
      )}
    </div>
  );
}
```

- [ ] **Step 2: Verify build**

Run: `cd web && npm run build`
Expected: PASS

---

### Task B3: Build tab page (WorkflowBuild.tsx)

**Files:**
- Create: `web/src/pages/WorkflowBuild.tsx`

- [ ] **Step 1: Create WorkflowBuild.tsx**

This component:
- For running workflows: subscribes to SSE events, filters for `build_stdout`, appends lines
- For completed workflows: fetches historical events to populate log
- Shows files changed count and a "View Diff" button below the terminal

```tsx
import { useCallback, useEffect, useState } from "react";
import { openEventStream, getWorkflow } from "@/api/client";
import { TerminalViewer } from "@/components/TerminalViewer";
import { DiffModal } from "@/components/DiffModal";
import { Button } from "@/components/ui/button";
import { FileCode, Loader2 } from "lucide-react";
import type { EventStreamEnvelope, PipelineRunDetail } from "@/types/api";

interface WorkflowBuildProps {
  workflowId: string;
  workflowState: string;
  pipelineRun: PipelineRunDetail | null;
}

const terminalStates = new Set(["completed", "failed"]);

export default function WorkflowBuild({ workflowId, workflowState, pipelineRun }: WorkflowBuildProps) {
  const [lines, setLines] = useState<string[]>([]);
  const [loading, setLoading] = useState(true);
  const [diffOpen, setDiffOpen] = useState(false);

  const isTerminal = terminalStates.has(workflowState);
  const streaming = !isTerminal;

  const handleEvent = useCallback((event: EventStreamEnvelope) => {
    if (event.event_type !== "build_stdout") return;
    const data = event.event_data;
    if (typeof data === "string") {
      setLines((prev) => [...prev, ...data.split("\n")]);
    } else if (data && typeof data === "object" && "content" in data) {
      const content = String((data as Record<string, unknown>).content);
      setLines((prev) => [...prev, ...content.split("\n")]);
    }
  }, []);

  useEffect(() => {
    setLines([]);
    setLoading(true);

    // Subscribe to SSE for all events (replay for historical, live for running).
    const source = openEventStream({
      workflowId,
      cursor: 0,
      onEvent: (event) => {
        setLoading(false);
        handleEvent(event);
      },
      onOpen: () => setLoading(false),
      onError: () => setLoading(false),
    });

    // For terminal workflows, allow time for replay then disconnect.
    let timer: ReturnType<typeof setTimeout> | undefined;
    if (isTerminal) {
      timer = setTimeout(() => source.close(), 5000);
    }

    return () => {
      source.close();
      if (timer) clearTimeout(timer);
    };
  }, [workflowId, isTerminal, handleEvent]);

  const filesChanged = pipelineRun?.build_files_changed ?? 0;

  return (
    <div className="space-y-4 p-4">
      {loading && lines.length === 0 ? (
        <div className="flex items-center justify-center p-6">
          <Loader2 className="size-5 animate-spin text-muted-foreground" />
        </div>
      ) : lines.length === 0 ? (
        <div className="flex items-center justify-center p-6 text-sm text-muted-foreground">
          No build output available
        </div>
      ) : (
        <TerminalViewer lines={lines} streaming={streaming} />
      )}

      {/* Footer: files changed + view diff */}
      <div className="flex items-center justify-between rounded-lg border border-border bg-card px-4 py-3">
        <div className="flex items-center gap-2 text-sm text-muted-foreground">
          <FileCode className="size-4" />
          <span>{filesChanged} file{filesChanged !== 1 ? "s" : ""} changed</span>
        </div>
        <Button variant="outline" size="sm" onClick={() => setDiffOpen(true)}>
          View Diff
        </Button>
      </div>

      <DiffModal open={diffOpen} onClose={() => setDiffOpen(false)} workflowId={workflowId} />
    </div>
  );
}
```

- [ ] **Step 2: Verify build**

Run: `cd web && npm run build`
Expected: PASS (DiffModal doesn't exist yet — create a stub or build B3 after B4. Better: create B4 first or build them together.)

Note: This task depends on Task B4 (DiffModal). If building sequentially, do B4 first or create a temporary stub.

---

### Task B4: DiffModal component

**Files:**
- Create: `web/src/components/DiffModal.tsx`

- [ ] **Step 1: Create DiffModal.tsx**

```tsx
import { useEffect, useState } from "react";
import { getWorkflowDiff } from "@/api/client";
import type { WorkflowDiffResponse } from "@/types/api";
import { Button } from "@/components/ui/button";
import { Loader2, X } from "lucide-react";

interface DiffModalProps {
  open: boolean;
  onClose: () => void;
  workflowId: string;
}

export function DiffModal({ open, onClose, workflowId }: DiffModalProps) {
  const [data, setData] = useState<WorkflowDiffResponse | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [splitView, setSplitView] = useState(true);

  useEffect(() => {
    if (!open) return;
    setLoading(true);
    setError(null);
    setData(null);

    getWorkflowDiff(workflowId)
      .then(setData)
      .catch((err: unknown) => {
        const message = err instanceof Error ? err.message : "Failed to load diff";
        // 404 means branch is gone.
        if (message.includes("404") || message.includes("not found") || message.includes("no longer exists")) {
          setError("Branch no longer exists. Diff is unavailable.");
        } else {
          setError(message);
        }
      })
      .finally(() => setLoading(false));
  }, [open, workflowId]);

  if (!open) return null;

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/80">
      <div className="flex h-[95vh] w-[95vw] flex-col rounded-lg border border-border bg-background">
        {/* Header */}
        <div className="flex items-center justify-between border-b border-border px-4 py-3">
          <div className="flex items-center gap-4 text-sm">
            {data && (
              <>
                <span>{data.stats.files} file{data.stats.files !== 1 ? "s" : ""} changed</span>
                <span className="text-green-400">+{data.stats.additions}</span>
                <span className="text-red-400">-{data.stats.deletions}</span>
              </>
            )}
          </div>
          <div className="flex items-center gap-2">
            <Button
              variant="outline"
              size="sm"
              onClick={() => setSplitView(!splitView)}
            >
              {splitView ? "Unified" : "Split"}
            </Button>
            <Button variant="ghost" size="icon" onClick={onClose}>
              <X className="size-4" />
            </Button>
          </div>
        </div>

        {/* Content */}
        <div className="min-h-0 flex-1 overflow-auto p-4">
          {loading && (
            <div className="flex h-full items-center justify-center">
              <Loader2 className="size-6 animate-spin text-muted-foreground" />
            </div>
          )}
          {error && (
            <div className="flex h-full items-center justify-center">
              <p className="text-sm text-muted-foreground">{error}</p>
            </div>
          )}
          {data && !loading && !error && (
            <pre className="whitespace-pre-wrap break-all font-mono text-xs text-zinc-300 bg-[#1a1a2e] rounded-lg p-4 overflow-auto">
              {data.diff}
            </pre>
          )}
        </div>

        {/* File list */}
        {data && data.files_changed.length > 0 && (
          <div className="border-t border-border px-4 py-2 text-xs text-muted-foreground">
            <details>
              <summary className="cursor-pointer">Changed files ({data.files_changed.length})</summary>
              <ul className="mt-1 space-y-0.5 font-mono">
                {data.files_changed.map((f) => (
                  <li key={f}>{f}</li>
                ))}
              </ul>
            </details>
          </div>
        )}
      </div>
    </div>
  );
}
```

Note: The API returns a raw unified diff string. react-diff-viewer-continued is installed for future per-file diff viewing (splitting the unified diff into old/new pairs per file). For v1, we render the raw diff in a styled `<pre>` block.

- [ ] **Step 2: Verify build**

Run: `cd web && npm run build`
Expected: PASS

---

### Task B5: Verify tab page (WorkflowVerify.tsx)

**Files:**
- Create: `web/src/pages/WorkflowVerify.tsx`

- [ ] **Step 1: Create WorkflowVerify.tsx**

```tsx
import { useCallback, useEffect, useState } from "react";
import { openEventStream } from "@/api/client";
import { StatusBadge } from "@/components/StatusBadge";
import { Loader2 } from "lucide-react";
import type { EventStreamEnvelope, PipelineRunDetail } from "@/types/api";

interface WorkflowVerifyProps {
  workflowId: string;
  workflowState: string;
  pipelineRun: PipelineRunDetail | null;
}

interface PrecheckResult {
  name: string;
  passed: boolean;
  output: string;
}

export default function WorkflowVerify({ workflowId, workflowState, pipelineRun }: WorkflowVerifyProps) {
  const [prechecks, setPrechecks] = useState<PrecheckResult[]>([]);
  const [llmAnalysis, setLlmAnalysis] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);
  const [expandedChecks, setExpandedChecks] = useState<Set<string>>(new Set());

  const handleEvent = useCallback((event: EventStreamEnvelope) => {
    if (event.event_type === "verify_precheck" && event.event_data && typeof event.event_data === "object") {
      const data = event.event_data as Record<string, unknown>;
      setPrechecks((prev) => {
        const name = String(data.check ?? data.name ?? "unknown");
        // Deduplicate by name.
        if (prev.some((p) => p.name === name)) return prev;
        return [
          ...prev,
          {
            name,
            passed: data.passed === true || data.status === "pass" || data.result === "PASS",
            output: String(data.output ?? data.stdout ?? ""),
          },
        ];
      });
    }
    if (event.event_type === "verify_result" && event.event_data && typeof event.event_data === "object") {
      const data = event.event_data as Record<string, unknown>;
      if (data.analysis) {
        setLlmAnalysis(String(data.analysis));
      } else if (data.reasoning) {
        setLlmAnalysis(String(data.reasoning));
      }
    }
  }, []);

  useEffect(() => {
    setPrechecks([]);
    setLlmAnalysis(null);
    setLoading(true);

    const source = openEventStream({
      workflowId,
      cursor: 0,
      onEvent: (event) => {
        setLoading(false);
        handleEvent(event);
      },
      onOpen: () => setLoading(false),
      onError: () => setLoading(false),
    });

    const isTerminal = ["completed", "failed"].includes(workflowState);
    let timer: ReturnType<typeof setTimeout> | undefined;
    if (isTerminal) {
      timer = setTimeout(() => source.close(), 5000);
    }

    return () => {
      source.close();
      if (timer) clearTimeout(timer);
    };
  }, [workflowId, workflowState, handleEvent]);

  const toggleCheck = (name: string) => {
    setExpandedChecks((prev) => {
      const next = new Set(prev);
      if (next.has(name)) next.delete(name);
      else next.add(name);
      return next;
    });
  };

  const verifyResult = pipelineRun?.verify_result;
  const verifyModel = pipelineRun?.verify_model;

  return (
    <div className="space-y-6 p-4">
      {/* Verdict */}
      <section className="flex flex-col items-center gap-2 rounded-lg border border-border bg-card p-6">
        {verifyResult ? (
          <>
            <StatusBadge status={verifyResult === "PASS" ? "completed" : "failed"} size="md" />
            <span className="text-lg font-semibold">
              Verification: {verifyResult}
            </span>
            {verifyModel && (
              <span className="text-xs text-muted-foreground">Model: {verifyModel}</span>
            )}
          </>
        ) : (
          <span className="text-sm text-muted-foreground">Verification pending</span>
        )}
      </section>

      {/* Loading */}
      {loading && prechecks.length === 0 && (
        <div className="flex items-center justify-center p-6">
          <Loader2 className="size-5 animate-spin text-muted-foreground" />
        </div>
      )}

      {/* Precheck cards */}
      {prechecks.length > 0 && (
        <section>
          <h3 className="mb-3 text-sm font-medium uppercase tracking-wider text-muted-foreground">
            Prechecks
          </h3>
          <div className="space-y-2">
            {prechecks.map((check) => (
              <div key={check.name} className="rounded-lg border border-border bg-card">
                <button
                  type="button"
                  className="flex w-full items-center justify-between px-4 py-3 text-left text-sm"
                  onClick={() => toggleCheck(check.name)}
                >
                  <span className="font-medium">{check.name}</span>
                  <StatusBadge status={check.passed ? "completed" : "failed"} size="sm" />
                </button>
                {expandedChecks.has(check.name) && check.output && (
                  <div className="border-t border-border px-4 py-3">
                    <pre className="max-h-60 overflow-auto whitespace-pre-wrap text-xs font-mono text-muted-foreground">
                      {check.output}
                    </pre>
                  </div>
                )}
              </div>
            ))}
          </div>
        </section>
      )}

      {/* LLM Analysis */}
      <section>
        <h3 className="mb-3 text-sm font-medium uppercase tracking-wider text-muted-foreground">
          LLM Analysis
        </h3>
        {llmAnalysis ? (
          <div className="rounded-lg border border-border bg-card p-4 text-sm leading-relaxed text-foreground">
            {llmAnalysis}
          </div>
        ) : (
          <div className="text-sm text-muted-foreground">
            {loading ? "Loading..." : "No LLM analysis recorded"}
          </div>
        )}
      </section>
    </div>
  );
}
```

- [ ] **Step 2: Verify build**

Run: `cd web && npm run build`
Expected: PASS

---

### Task B6: Scope tab page (WorkflowScope.tsx)

**Files:**
- Create: `web/src/pages/WorkflowScope.tsx`

- [ ] **Step 1: Create WorkflowScope.tsx with collapsible tree viewer**

```tsx
import { useEffect, useState } from "react";
import { getWorkflowScope } from "@/api/client";
import { ChevronDown, ChevronRight, Loader2 } from "lucide-react";

interface WorkflowScopeProps {
  workflowId: string;
}

export default function WorkflowScope({ workflowId }: WorkflowScopeProps) {
  const [data, setData] = useState<Record<string, unknown> | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    setLoading(true);
    setError(null);

    getWorkflowScope(workflowId)
      .then((resp) => setData(resp.context_package))
      .catch((err: unknown) => {
        const message = err instanceof Error ? err.message : "Failed to load scope";
        if (message.includes("404") || message.includes("not found")) {
          setError("No scope context package available for this workflow");
        } else {
          setError(message);
        }
      })
      .finally(() => setLoading(false));
  }, [workflowId]);

  if (loading) {
    return (
      <div className="flex items-center justify-center p-6">
        <Loader2 className="size-5 animate-spin text-muted-foreground" />
      </div>
    );
  }

  if (error) {
    return (
      <div className="flex items-center justify-center p-6 text-sm text-muted-foreground">
        {error}
      </div>
    );
  }

  if (!data) return null;

  const topLevelOrder = [
    "work_order", "file_tree", "conventions", "rag_context",
    "build_instructions", "constraints", "files_to_modify", "new_files",
  ];
  const sortedKeys = [
    ...topLevelOrder.filter((k) => k in data),
    ...Object.keys(data).filter((k) => !topLevelOrder.includes(k)),
  ];

  return (
    <div className="space-y-1 p-4">
      {sortedKeys.map((key) => (
        <CollapsibleTreeNode
          key={key}
          label={key}
          value={data[key]}
          defaultOpen={key === "work_order"}
          depth={0}
        />
      ))}
    </div>
  );
}

interface CollapsibleTreeNodeProps {
  label: string;
  value: unknown;
  defaultOpen?: boolean;
  depth: number;
}

function CollapsibleTreeNode({ label, value, defaultOpen = false, depth }: CollapsibleTreeNodeProps) {
  const [open, setOpen] = useState(defaultOpen);

  if (value === null || value === undefined) {
    return (
      <div style={{ paddingLeft: depth * 16 }} className="flex gap-2 py-0.5 text-sm">
        <span className="font-medium text-muted-foreground">{label}:</span>
        <span className="text-zinc-500 italic">null</span>
      </div>
    );
  }

  if (typeof value === "string") {
    const isMultiline = value.includes("\n");
    return (
      <div style={{ paddingLeft: depth * 16 }} className="py-0.5 text-sm">
        <span className="font-medium text-muted-foreground">{label}:</span>{" "}
        {isMultiline ? (
          <pre className="mt-1 whitespace-pre-wrap rounded bg-muted/50 p-2 font-mono text-xs text-foreground">
            {value}
          </pre>
        ) : (
          <span className="font-mono text-foreground">{value}</span>
        )}
      </div>
    );
  }

  if (typeof value === "number" || typeof value === "boolean") {
    return (
      <div style={{ paddingLeft: depth * 16 }} className="flex gap-2 py-0.5 text-sm">
        <span className="font-medium text-muted-foreground">{label}:</span>
        <span className="text-foreground">{String(value)}</span>
      </div>
    );
  }

  if (Array.isArray(value)) {
    return (
      <div style={{ paddingLeft: depth * 16 }} className="py-0.5">
        <button
          type="button"
          className="flex items-center gap-1 text-sm font-medium text-muted-foreground hover:text-foreground"
          onClick={() => setOpen(!open)}
        >
          {open ? <ChevronDown className="size-3" /> : <ChevronRight className="size-3" />}
          {label}
          <span className="ml-1 rounded bg-muted px-1.5 py-0.5 text-xs">{value.length}</span>
        </button>
        {open && (
          <div className="mt-0.5">
            {value.map((item, i) => (
              <CollapsibleTreeNode key={i} label={`[${i}]`} value={item} depth={depth + 1} />
            ))}
          </div>
        )}
      </div>
    );
  }

  if (typeof value === "object") {
    const entries = Object.entries(value as Record<string, unknown>);
    return (
      <div style={{ paddingLeft: depth * 16 }} className="py-0.5">
        <button
          type="button"
          className="flex items-center gap-1 text-sm font-medium text-muted-foreground hover:text-foreground"
          onClick={() => setOpen(!open)}
        >
          {open ? <ChevronDown className="size-3" /> : <ChevronRight className="size-3" />}
          {label}
          <span className="ml-1 rounded bg-muted px-1.5 py-0.5 text-xs">{entries.length} keys</span>
        </button>
        {open && (
          <div className="mt-0.5">
            {entries.map(([k, v]) => (
              <CollapsibleTreeNode key={k} label={k} value={v} depth={depth + 1} />
            ))}
          </div>
        )}
      </div>
    );
  }

  return null;
}
```

- [ ] **Step 2: Verify build**

Run: `cd web && npm run build`
Expected: PASS

---

### Task B7: Wire tabs and approve/reject into WorkflowDetail.tsx

**Files:**
- Modify: `web/src/pages/WorkflowDetail.tsx`

This is the integration task — replace placeholder tabs with real components and enable approve/reject buttons.

- [ ] **Step 1: Add imports for new components**

At the top of WorkflowDetail.tsx, add:

```typescript
import WorkflowBuild from "@/pages/WorkflowBuild";
import WorkflowVerify from "@/pages/WorkflowVerify";
import WorkflowScope from "@/pages/WorkflowScope";
import { ConfirmDialog } from "@/components/ConfirmDialog";
import { approveWorkflow, rejectWorkflow, getWorkflow as refetchWorkflow } from "@/api/client";
import { Loader2 as ButtonLoader } from "lucide-react";
```

- [ ] **Step 2: Add approve/reject state and handlers**

Inside the `WorkflowDetail` component, after the existing state declarations, add:

```typescript
const [approveOpen, setApproveOpen] = useState(false);
const [rejectOpen, setRejectOpen] = useState(false);
const [rejectReason, setRejectReason] = useState("");
const [actionLoading, setActionLoading] = useState(false);
const [actionError, setActionError] = useState<string | null>(null);
const [reindexAfterMerge, setReindexAfterMerge] = useState(true);

const isAwaitingReview = workflow.current_state === "human_review";

const handleApprove = async () => {
  setActionLoading(true);
  setActionError(null);
  try {
    await approveWorkflow(workflow.id, { reindex: reindexAfterMerge });
    setApproveOpen(false);
    // Re-fetch workflow to update state.
    const updated = await refetchWorkflow(workflow.id);
    setData(updated);
  } catch (err: unknown) {
    setActionError(err instanceof Error ? err.message : "Approve failed");
  } finally {
    setActionLoading(false);
  }
};

const handleReject = async () => {
  setActionLoading(true);
  setActionError(null);
  try {
    await rejectWorkflow(workflow.id, { reason: rejectReason });
    setRejectOpen(false);
    setRejectReason("");
    const updated = await refetchWorkflow(workflow.id);
    setData(updated);
  } catch (err: unknown) {
    setActionError(err instanceof Error ? err.message : "Reject failed");
  } finally {
    setActionLoading(false);
  }
};
```

- [ ] **Step 3: Replace disabled buttons with conditional approve/reject**

Replace the approve/reject button block (lines 114-121) with:

```tsx
{isAwaitingReview && (
  <div className="flex items-center gap-2">
    <Button
      variant="outline"
      size="sm"
      className="border-green-600 text-green-400 hover:bg-green-600/20"
      onClick={() => setApproveOpen(true)}
    >
      Approve
    </Button>
    <Button
      variant="outline"
      size="sm"
      className="border-red-600 text-red-400 hover:bg-red-600/20"
      onClick={() => setRejectOpen(true)}
    >
      Reject
    </Button>
  </div>
)}
```

- [ ] **Step 4: Replace tab placeholders with real components**

Replace the scope TabsContent (lines 166-169):
```tsx
<TabsContent value="scope">
  <WorkflowScope workflowId={workflow.id} />
</TabsContent>
```

Replace the build TabsContent (lines 171-174):
```tsx
<TabsContent value="build">
  <WorkflowBuild
    workflowId={workflow.id}
    workflowState={workflow.current_state}
    pipelineRun={pipeline_run}
  />
</TabsContent>
```

Replace the verify TabsContent (lines 176-179):
```tsx
<TabsContent value="verify">
  <WorkflowVerify
    workflowId={workflow.id}
    workflowState={workflow.current_state}
    pipelineRun={pipeline_run}
  />
</TabsContent>
```

- [ ] **Step 5: Add confirmation dialogs before the closing tag**

Before the final `</div>` of the component, add:

```tsx
{/* Approve dialog */}
<ConfirmDialog
  open={approveOpen}
  title="Approve Workflow"
  description={`This will merge branch \`${workflow.git_branch}\` into \`main\`. This action cannot be undone.`}
  confirmLabel={actionLoading ? "Approving..." : "Approve & Merge"}
  onConfirm={handleApprove}
  onCancel={() => { setApproveOpen(false); setActionError(null); }}
>
  <div className="space-y-3 px-1">
    <label className="flex items-center gap-2 text-sm">
      <input
        type="checkbox"
        checked={reindexAfterMerge}
        onChange={(e) => setReindexAfterMerge(e.target.checked)}
        className="rounded"
      />
      Auto-reindex after merge
    </label>
    {actionError && (
      <p className="text-sm text-destructive">{actionError}</p>
    )}
  </div>
</ConfirmDialog>

{/* Reject dialog */}
<ConfirmDialog
  open={rejectOpen}
  title="Reject Workflow"
  description="This workflow will be marked as rejected."
  confirmLabel={actionLoading ? "Rejecting..." : "Reject"}
  onConfirm={handleReject}
  onCancel={() => { setRejectOpen(false); setActionError(null); setRejectReason(""); }}
>
  <div className="space-y-3 px-1">
    <textarea
      className="w-full rounded-md border border-border bg-background px-3 py-2 text-sm"
      placeholder="Optional: reason for rejection"
      rows={3}
      value={rejectReason}
      onChange={(e) => setRejectReason(e.target.value)}
    />
    {actionError && (
      <p className="text-sm text-destructive">{actionError}</p>
    )}
  </div>
</ConfirmDialog>
```

- [ ] **Step 6: Verify build**

Run: `cd web && npm run build`
Expected: PASS

---

### Task B8: Overview tab review summary (WorkflowOverview.tsx)

**Files:**
- Modify: `web/src/pages/WorkflowOverview.tsx`

- [ ] **Step 1: Add review summary section props**

Update the `WorkflowOverviewProps` interface:

```typescript
interface WorkflowOverviewProps {
  pipelineRun: PipelineRunDetail | null;
  subCalls: SubCall[];
  workflowState: string;
  workflowId: string;
  onApprove?: () => void;
  onReject?: () => void;
}
```

Update the function signature to destructure new props.

- [ ] **Step 2: Add review summary section**

At the start of the return JSX (before the Phase Timing section), add:

```tsx
{/* Review Summary — shown for human_review workflows */}
{workflowState === "human_review" && pipelineRun && (
  <section className="rounded-lg border-2 border-amber-500/30 bg-amber-500/5 p-4 space-y-3">
    <h3 className="text-sm font-medium uppercase tracking-wider text-amber-400">
      Review Required
    </h3>
    <div className="flex items-center gap-3">
      {pipelineRun.verify_result && (
        <StatusBadge
          status={pipelineRun.verify_result === "PASS" ? "completed" : "failed"}
          size="md"
        />
      )}
      <span className="text-sm font-medium">
        Verification: {pipelineRun.verify_result ?? "pending"}
      </span>
    </div>
    <div className="text-xs text-muted-foreground">
      {pipelineRun.build_files_changed ?? 0} files changed
      {" · "}
      {formatCost(totalCost)}
      {" · "}
      {formatDuration(
        pipelineRun.scope_started_at,
        pipelineRun.verify_completed_at ?? pipelineRun.build_completed_at
      )}
    </div>
    {onApprove && onReject && (
      <div className="flex gap-2 pt-1">
        <Button
          size="sm"
          className="border-green-600 text-green-400 hover:bg-green-600/20"
          variant="outline"
          onClick={onApprove}
        >
          Approve
        </Button>
        <Button
          size="sm"
          className="border-red-600 text-red-400 hover:bg-red-600/20"
          variant="outline"
          onClick={onReject}
        >
          Reject
        </Button>
      </div>
    )}
  </section>
)}
```

Import `Button` from `@/components/ui/button` at the top.

- [ ] **Step 3: Update WorkflowDetail.tsx to pass new props**

In WorkflowDetail.tsx, update the WorkflowOverview usage:

```tsx
<TabsContent value="overview">
  <WorkflowOverview
    pipelineRun={pipeline_run}
    subCalls={sub_calls}
    workflowState={workflow.current_state}
    workflowId={workflow.id}
    onApprove={() => setApproveOpen(true)}
    onReject={() => setRejectOpen(true)}
  />
</TabsContent>
```

- [ ] **Step 4: Verify build**

Run: `cd web && npm run build`
Expected: PASS

- [ ] **Step 5: Commit all frontend work**

```bash
cd web
git add -A
git commit -m "feat: implement interactive pipeline UI — build/verify/scope tabs, diff modal, approve/reject UX (Spec 03 frontend)"
```

---

## Final Verification

### Task C1: Full build and test

- [ ] **Step 1: Run full backend build**

Run: `make build`
Expected: PASS

- [ ] **Step 2: Run full test suite**

Run: `make test`
Expected: PASS (all packages)

- [ ] **Step 3: Run frontend build**

Run: `cd web && npm run build`
Expected: PASS

- [ ] **Step 4: Final commit if needed**

If any fixes were needed during verification, commit them.

---

## Parallel Execution Strategy

Track A (Backend: Tasks A1-A5) and Track B (Frontend: Tasks B1-B8) are **fully independent** — they never touch the same files. They can be executed in parallel by two agents in separate worktrees, then merged cleanly.

Within Track B, tasks must be sequential: B1 (foundation) → B2-B6 (components, any order) → B7-B8 (integration).
