# Plan Space (Spec 05) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement the Plan space — where specs are submitted for planning, generated work orders are reviewed/edited, and selected work orders are queued for pipeline execution.

**Architecture:** Backend adds 4 new endpoints for work order file CRUD and plan submission. Frontend replaces the PlanSpace placeholder with a full master-detail layout: plan run list on the left, plan run detail / new plan form on the right. A shared CodeViewer component wraps CodeMirror 6 for YAML/markdown editing. Work order cards are expandable with inline editing and queue selection.

**Tech Stack:** Go (chi router, os, yaml.v3), React 18, TypeScript, CodeMirror 6, Tailwind CSS v4, shadcn/ui

**Spec source:** `docs/specs/UI/05-plan-space/`

---

## Important Codebase Context

**Current `NewServer` signature:** `NewServer(db *database.DB, gitMgr *git.GitManager, baseBranch string, runQueue *RunQueue, workOrderDir string) http.Handler`

**Current `Server` struct fields:** `db, gitMgr, baseBranch, runQueue, workOrderDir`

**All existing `NewServer` test calls use:** `NewServer(db, nil, "main", nil, "")`

**Serve.go** creates `rq := api.NewRunQueue()` and calls `api.NewServer(db, gitMgr, baseBranch, rq, "")`.

The `workOrderDir` field already exists on Server but is empty — Spec 05 will populate it from project config.

---

## File Structure

### Backend — New Files
- `internal/api/workorder_handlers.go` — GET list, GET single, PUT update handlers + path traversal validation + YAML parsing helper
- `internal/api/plan_handlers.go` — POST /api/plan handler with session creation

### Backend — Modified Files
- `internal/api/server.go` — Register 4 new routes
- `internal/api/response.go` — Add work order + plan response types
- `internal/api/server_test.go` — Add work order endpoint tests
- `internal/pipeline/events.go` — Add plan SSE event constants
- `cmd/conductor/serve.go` — Populate workOrderDir from config

### Frontend — New Files
- `web/src/components/CodeViewer.tsx` — CodeMirror 6 wrapper component
- `web/src/pages/NewPlan.tsx` — Spec editor with generate button
- `web/src/pages/PlanRunDetail.tsx` — Plan run detail with work order cards

### Frontend — Modified Files
- `web/src/types/api.ts` — Add WorkOrderFile, WorkOrderListResponse, etc.
- `web/src/api/client.ts` — Add work order + plan client functions
- `web/src/pages/PlanSpace.tsx` — Replace placeholder with master-detail layout
- `web/src/App.tsx` — Add nested plan routes

---

## Track A: Backend

### Task A1: Work order file handlers

**Files:**
- Create: `internal/api/workorder_handlers.go`
- Modify: `internal/api/response.go`
- Modify: `internal/api/server.go`

- [ ] **Step 1: Add work order response types to response.go**

Append to `internal/api/response.go`:

```go
// --- Work order response types ---

type workOrderFileResponse struct {
	Filename    string `json:"filename"`
	Title       string `json:"title"`
	Type        string `json:"type"`
	TargetModule string `json:"target_module"`
	SizeBytes   int64  `json:"size_bytes"`
	ModifiedAt  string `json:"modified_at"`
}

type workOrderListResponse struct {
	WorkOrders []workOrderFileResponse `json:"work_orders"`
	Directory  string                  `json:"directory"`
}

type workOrderContentResponse struct {
	Filename string `json:"filename"`
	Content  string `json:"content"`
}

type workOrderUpdateRequest struct {
	Content string `json:"content"`
}

type workOrderUpdateResponse struct {
	Filename string `json:"filename"`
	Content  string `json:"content"`
	Valid    bool   `json:"valid"`
}

type planSubmitRequest struct {
	SpecContent  string `json:"spec_content"`
	SpecFilePath string `json:"spec_file_path"`
}

type planSubmitResponse struct {
	SessionID string `json:"session_id"`
	Status    string `json:"status"`
}
```

- [ ] **Step 2: Create workorder_handlers.go**

```go
package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-chi/chi/v5"
	"gopkg.in/yaml.v3"
)

// validateWorkOrderFilename rejects path traversal and non-YAML filenames.
func validateWorkOrderFilename(name string) error {
	if name == "" {
		return fmt.Errorf("filename is required")
	}
	if strings.Contains(name, "..") || strings.Contains(name, "/") || strings.Contains(name, "\\") {
		return fmt.Errorf("invalid filename: path traversal not allowed")
	}
	if !strings.HasSuffix(name, ".yaml") && !strings.HasSuffix(name, ".yml") {
		return fmt.Errorf("invalid filename: must end with .yaml or .yml")
	}
	return nil
}

// parseWorkOrderMeta extracts title, type, and target_module from YAML.
type workOrderMeta struct {
	Title        string `yaml:"title"`
	Type         string `yaml:"type"`
	TargetModule string `yaml:"target_module"`
}

func parseWorkOrderYAML(data []byte) (workOrderMeta, error) {
	var meta workOrderMeta
	if err := yaml.Unmarshal(data, &meta); err != nil {
		return meta, err
	}
	return meta, nil
}

func (s *Server) handleListWorkOrders(w http.ResponseWriter, r *http.Request) {
	if s.workOrderDir == "" {
		writeError(w, http.StatusNotFound, "work orders directory not configured")
		return
	}

	entries, err := os.ReadDir(s.workOrderDir)
	if err != nil {
		if os.IsNotExist(err) {
			writeError(w, http.StatusNotFound, "work orders directory does not exist")
			return
		}
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("read work orders directory: %v", err))
		return
	}

	var files []workOrderFileResponse
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasSuffix(name, ".yaml") && !strings.HasSuffix(name, ".yml") {
			continue
		}

		path := filepath.Join(s.workOrderDir, name)
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}

		info, _ := entry.Info()
		var sizeBytes int64
		var modifiedAt string
		if info != nil {
			sizeBytes = info.Size()
			modifiedAt = info.ModTime().UTC().Format("2006-01-02T15:04:05Z")
		}

		meta, _ := parseWorkOrderYAML(data)

		files = append(files, workOrderFileResponse{
			Filename:     name,
			Title:        meta.Title,
			Type:         meta.Type,
			TargetModule: meta.TargetModule,
			SizeBytes:    sizeBytes,
			ModifiedAt:   modifiedAt,
		})
	}

	if files == nil {
		files = []workOrderFileResponse{}
	}

	writeJSON(w, http.StatusOK, workOrderListResponse{
		WorkOrders: files,
		Directory:  s.workOrderDir,
	})
}

func (s *Server) handleGetWorkOrder(w http.ResponseWriter, r *http.Request) {
	filename := chi.URLParam(r, "filename")
	if err := validateWorkOrderFilename(filename); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	if s.workOrderDir == "" {
		writeError(w, http.StatusNotFound, "work orders directory not configured")
		return
	}

	path := filepath.Join(s.workOrderDir, filename)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			writeError(w, http.StatusNotFound, "work order not found")
			return
		}
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("read work order: %v", err))
		return
	}

	writeJSON(w, http.StatusOK, workOrderContentResponse{
		Filename: filename,
		Content:  string(data),
	})
}

func (s *Server) handleUpdateWorkOrder(w http.ResponseWriter, r *http.Request) {
	filename := chi.URLParam(r, "filename")
	if err := validateWorkOrderFilename(filename); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	if s.workOrderDir == "" {
		writeError(w, http.StatusNotFound, "work orders directory not configured")
		return
	}

	path := filepath.Join(s.workOrderDir, filename)
	if _, err := os.Stat(path); os.IsNotExist(err) {
		writeError(w, http.StatusNotFound, "work order not found")
		return
	}

	var req workOrderUpdateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	// Validate YAML is parseable.
	var parsed any
	if err := yaml.Unmarshal([]byte(req.Content), &parsed); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid YAML: %v", err))
		return
	}

	if err := os.WriteFile(path, []byte(req.Content), 0644); err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("write work order: %v", err))
		return
	}

	writeJSON(w, http.StatusOK, workOrderUpdateResponse{
		Filename: filename,
		Content:  req.Content,
		Valid:    true,
	})
}
```

- [ ] **Step 3: Register routes in server.go**

Add before the SPA fallback route:
```go
r.Get("/api/work-orders", s.handleListWorkOrders)
r.Get("/api/work-orders/{filename}", s.handleGetWorkOrder)
r.Put("/api/work-orders/{filename}", s.handleUpdateWorkOrder)
```

- [ ] **Step 4: Verify build**

Run: `make build`
Expected: PASS

---

### Task A2: Plan submission handler

**Files:**
- Create: `internal/api/plan_handlers.go`
- Modify: `internal/api/server.go`
- Modify: `internal/pipeline/events.go`

- [ ] **Step 1: Add plan event constants to pipeline/events.go**

Append to the constants block:

```go
	// Plan events.
	EventPlanStarted   = "plan_started"
	EventPlanStep      = "plan_step"
	EventPlanCompleted = "plan_completed"
	EventPlanFailed    = "plan_failed"
```

- [ ] **Step 2: Create plan_handlers.go**

The POST /api/plan handler creates a session and returns immediately. Background plan execution requires the plan logic to be extractable from `cmd/conductor/plan.go` (package main). For v1, the handler creates the session and logs a placeholder event. Full background execution integration is a follow-up task when the plan logic is extracted into an importable package.

```go
package api

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"

	"github.com/ponchione/agent-conductor/internal/database"
)

func (s *Server) handleSubmitPlan(w http.ResponseWriter, r *http.Request) {
	var req planSubmitRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.SpecContent == "" && req.SpecFilePath == "" {
		writeError(w, http.StatusBadRequest, "spec_content or spec_file_path is required")
		return
	}

	// Validate spec_file_path exists if provided.
	specPath := req.SpecFilePath
	if specPath != "" {
		if _, err := os.Stat(specPath); os.IsNotExist(err) {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("spec file not found: %s", specPath))
			return
		}
	} else {
		// Write spec_content to a temp file.
		tmpDir := os.TempDir()
		tmpFile, err := os.CreateTemp(tmpDir, "spec-*.md")
		if err != nil {
			writeError(w, http.StatusInternalServerError, fmt.Sprintf("create temp file: %v", err))
			return
		}
		if _, err := tmpFile.WriteString(req.SpecContent); err != nil {
			tmpFile.Close()
			writeError(w, http.StatusInternalServerError, fmt.Sprintf("write temp file: %v", err))
			return
		}
		tmpFile.Close()
		specPath = tmpFile.Name()
	}

	// Create a plan session.
	ctx := r.Context()
	project := ""
	if s.workOrderDir != "" {
		project = filepath.Base(filepath.Dir(s.workOrderDir))
	}

	sessionID, err := s.db.StartSession(ctx, database.SessionKindPlanOnly, project, specPath)
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("create session: %v", err))
		return
	}

	// Emit plan_started event.
	if err := s.db.LogWorkflowEvent(ctx, "", "", "plan_started", map[string]any{
		"session_id": sessionID,
		"spec_file":  filepath.Base(specPath),
	}); err != nil {
		slog.Warn("failed to log plan_started event", "error", err)
	}

	// TODO: Launch background plan execution goroutine.
	// This requires extracting plan generation logic from cmd/conductor/plan.go
	// into an importable internal package. For now, the session is created and
	// the frontend can poll for status.
	slog.Info("plan session created", "session_id", sessionID, "spec_file", specPath)

	writeJSON(w, http.StatusOK, planSubmitResponse{
		SessionID: sessionID,
		Status:    "started",
	})
}
```

- [ ] **Step 3: Register route in server.go**

Add before the SPA fallback:
```go
r.Post("/api/plan", s.handleSubmitPlan)
```

- [ ] **Step 4: Verify build**

Run: `make build`
Expected: PASS

---

### Task A3: Update serve.go and add tests

**Files:**
- Modify: `cmd/conductor/serve.go`
- Modify: `internal/api/server_test.go`

- [ ] **Step 1: Populate workOrderDir from project config in serve.go**

In `cmd/conductor/serve.go`, after the baseBranch logic, add:

```go
workOrderDir := ""
if cfg != nil {
    workOrderDir = filepath.Join(cfg.Project.DataDir, "work-orders")
}
```

Update the NewServer call:
```go
if err := http.ListenAndServe(serveAddr, api.NewServer(db, gitMgr, baseBranch, rq, workOrderDir)); err != nil {
```

Add `"path/filepath"` to imports if not already present.

- [ ] **Step 2: Add work order endpoint tests**

```go
func TestListWorkOrdersEndpointEmpty(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	dir := t.TempDir()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/work-orders", nil)
	NewServer(db, nil, "main", nil, dir).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestListWorkOrdersWithFiles(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "test.yaml"), []byte("title: Test\ntype: new_feature\ntarget_module: core\n"), 0644)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/work-orders", nil)
	NewServer(db, nil, "main", nil, dir).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	var payload struct {
		WorkOrders []workOrderFileResponse `json:"work_orders"`
	}
	json.NewDecoder(rec.Body).Decode(&payload)
	if len(payload.WorkOrders) != 1 {
		t.Fatalf("work_orders = %d, want 1", len(payload.WorkOrders))
	}
	if payload.WorkOrders[0].Title != "Test" {
		t.Fatalf("title = %q, want Test", payload.WorkOrders[0].Title)
	}
}

func TestGetWorkOrderPathTraversal(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	dir := t.TempDir()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/work-orders/..%2Fetc%2Fpasswd", nil)
	NewServer(db, nil, "main", nil, dir).ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestGetWorkOrderNotFound(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	dir := t.TempDir()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/work-orders/nonexistent.yaml", nil)
	NewServer(db, nil, "main", nil, dir).ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestUpdateWorkOrderValidYAML(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "test.yaml"), []byte("title: Original\n"), 0644)
	body := strings.NewReader(`{"content":"title: Updated\ntype: bug_fix\n"}`)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/api/work-orders/test.yaml", body)
	NewServer(db, nil, "main", nil, dir).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var payload workOrderUpdateResponse
	json.NewDecoder(rec.Body).Decode(&payload)
	if !payload.Valid {
		t.Fatal("valid = false, want true")
	}
}

func TestUpdateWorkOrderInvalidYAML(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "test.yaml"), []byte("title: Original\n"), 0644)
	body := strings.NewReader(`{"content":"invalid: yaml: [unclosed"}`)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/api/work-orders/test.yaml", body)
	NewServer(db, nil, "main", nil, dir).ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}
```

- [ ] **Step 3: Run tests**

Run: `make test`
Expected: PASS

- [ ] **Step 4: Commit backend**

```bash
git add internal/api/workorder_handlers.go internal/api/plan_handlers.go \
       internal/api/server.go internal/api/response.go internal/api/server_test.go \
       internal/pipeline/events.go cmd/conductor/serve.go
git commit -m "feat: add work order file endpoints and plan submission handler (Spec 05 backend)"
```

---

## Track B: Frontend

### Task B1: Install CodeMirror 6 and add types/client

**Files:**
- Modify: `web/package.json` (via npm install)
- Modify: `web/src/types/api.ts`
- Modify: `web/src/api/client.ts`

- [ ] **Step 1: Install CodeMirror packages**

```bash
cd web && npm install codemirror@^6 @codemirror/view@^6 @codemirror/state@^6 @codemirror/lang-yaml@^6 @codemirror/theme-one-dark@^6
```

- [ ] **Step 2: Add TypeScript interfaces to api.ts**

Append:

```typescript
// --- Spec 05: Plan Space types ---

export interface WorkOrderFile {
  filename: string;
  title: string;
  type: string;
  target_module: string;
  size_bytes: number;
  modified_at: string;
}

export interface WorkOrderListResponse {
  work_orders: WorkOrderFile[];
  directory: string;
}

export interface WorkOrderContentResponse {
  filename: string;
  content: string;
}

export interface WorkOrderUpdateResponse {
  filename: string;
  content: string;
  valid: boolean;
}

export interface PlanSubmitResponse {
  session_id: string;
  status: string;
}
```

- [ ] **Step 3: Add API client functions**

Add type imports, then append:

```typescript
export async function listWorkOrders(): Promise<WorkOrderListResponse> {
  return fetchJSON<WorkOrderListResponse>("/api/work-orders");
}

export async function getWorkOrder(filename: string): Promise<WorkOrderContentResponse> {
  return fetchJSON<WorkOrderContentResponse>(`/api/work-orders/${encodeURIComponent(filename)}`);
}

export async function updateWorkOrder(filename: string, content: string): Promise<WorkOrderUpdateResponse> {
  return postJSON<WorkOrderUpdateResponse>(`/api/work-orders/${encodeURIComponent(filename)}`, { content });
}

export async function submitPlan(options: { spec_content?: string; spec_file_path?: string }): Promise<PlanSubmitResponse> {
  return postJSON<PlanSubmitResponse>("/api/plan", options);
}
```

Note: `updateWorkOrder` uses `postJSON` but the backend expects PUT. Either change the backend to accept POST, or create a `putJSON` helper. Simplest: the `postJSON` helper can be refactored to accept a method parameter, or create the request with PUT inline:

```typescript
export async function updateWorkOrder(filename: string, content: string): Promise<WorkOrderUpdateResponse> {
  const response = await fetch(`/api/work-orders/${encodeURIComponent(filename)}`, {
    method: "PUT",
    headers: { "Content-Type": "application/json", Accept: "application/json" },
    body: JSON.stringify({ content }),
  });
  const payload = (await response.json().catch(() => ({}))) as { error?: string };
  if (!response.ok) throw new Error(payload.error || `Request failed: ${response.status}`);
  return payload as WorkOrderUpdateResponse;
}
```

- [ ] **Step 4: Verify build**

Run: `cd web && npm run build`
Expected: PASS

---

### Task B2: CodeViewer component

**Files:**
- Create: `web/src/components/CodeViewer.tsx`

- [ ] **Step 1: Create CodeViewer.tsx**

```tsx
import { useEffect, useRef } from "react";
import { EditorView, basicSetup } from "codemirror";
import { EditorState } from "@codemirror/state";
import { oneDark } from "@codemirror/theme-one-dark";
import { yaml } from "@codemirror/lang-yaml";
import { ViewUpdate } from "@codemirror/view";

interface CodeViewerProps {
  value: string;
  onChange?: (value: string) => void;
  language?: "yaml" | "json" | "markdown";
  readOnly?: boolean;
  height?: string;
  placeholder?: string;
}

function getLanguageExtension(lang?: string) {
  switch (lang) {
    case "yaml":
      return yaml();
    default:
      return [];
  }
}

export function CodeViewer({
  value,
  onChange,
  language,
  readOnly = false,
  height = "300px",
  placeholder: _placeholder,
}: CodeViewerProps) {
  const containerRef = useRef<HTMLDivElement>(null);
  const viewRef = useRef<EditorView | null>(null);
  const onChangeRef = useRef(onChange);
  onChangeRef.current = onChange;

  const isEditable = !readOnly && !!onChange;

  useEffect(() => {
    if (!containerRef.current) return;

    const extensions = [
      basicSetup,
      oneDark,
      getLanguageExtension(language),
      EditorView.theme({
        "&": { height, backgroundColor: "#1a1a2e" },
        ".cm-scroller": { overflow: "auto" },
        ".cm-content": { fontFamily: "monospace" },
      }),
    ];

    if (!isEditable) {
      extensions.push(EditorState.readOnly.of(true));
      extensions.push(EditorView.editable.of(false));
    } else {
      extensions.push(
        EditorView.updateListener.of((update: ViewUpdate) => {
          if (update.docChanged && onChangeRef.current) {
            onChangeRef.current(update.state.doc.toString());
          }
        })
      );
    }

    const state = EditorState.create({
      doc: value,
      extensions,
    });

    const view = new EditorView({
      state,
      parent: containerRef.current,
    });

    viewRef.current = view;

    return () => {
      view.destroy();
      viewRef.current = null;
    };
  }, [language, isEditable, height]); // Recreate on mode/language change

  // Sync external value changes.
  useEffect(() => {
    const view = viewRef.current;
    if (!view) return;
    const currentContent = view.state.doc.toString();
    if (currentContent !== value) {
      view.dispatch({
        changes: { from: 0, to: currentContent.length, insert: value },
      });
    }
  }, [value]);

  return (
    <div
      ref={containerRef}
      className="overflow-hidden rounded-lg border border-border"
    />
  );
}
```

- [ ] **Step 2: Verify build**

Run: `cd web && npm run build`
Expected: PASS

---

### Task B3: PlanSpace master-detail layout and routes

**Files:**
- Modify: `web/src/pages/PlanSpace.tsx` (full rewrite)
- Modify: `web/src/App.tsx`
- Create: `web/src/pages/NewPlan.tsx`
- Create: `web/src/pages/PlanRunDetail.tsx`

- [ ] **Step 1: Update App.tsx routes**

Add nested routes under /plan:

```tsx
import NewPlan from "@/pages/NewPlan";
import PlanRunDetail from "@/pages/PlanRunDetail";

// Inside Routes:
<Route path="plan" element={<PlanSpace />}>
  <Route path="new" element={<NewPlan />} />
  <Route path=":planRunId" element={<PlanRunDetail />} />
</Route>
```

- [ ] **Step 2: Rewrite PlanSpace.tsx with master-detail layout**

```tsx
import { useEffect, useState } from "react";
import { Outlet, useNavigate, useParams } from "react-router-dom";
import { Plus, Loader2 } from "lucide-react";
import { getPlanAuditStats } from "@/api/client";
import type { PlanAuditRun } from "@/types/api";
import { StatusBadge } from "@/components/StatusBadge";
import { TimeAgo } from "@/components/TimeAgo";
import { Button } from "@/components/ui/button";
import { cn } from "@/lib/utils";

export default function PlanSpace() {
  const navigate = useNavigate();
  const { planRunId } = useParams();
  const [runs, setRuns] = useState<PlanAuditRun[]>([]);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    getPlanAuditStats(50)
      .then((data) => setRuns(data.recent_runs))
      .catch(() => {})
      .finally(() => setLoading(false));
  }, []);

  return (
    <div className="flex h-full">
      {/* Left panel — plan list */}
      <div className="flex w-80 shrink-0 flex-col border-r border-border">
        <div className="flex items-center justify-between border-b border-border px-4 py-3">
          <h2 className="text-sm font-semibold">Plan Runs</h2>
          <Button size="sm" onClick={() => navigate("/plan/new")}>
            <Plus className="mr-1 size-3" /> New Plan
          </Button>
        </div>
        <div className="flex-1 overflow-auto">
          {loading && (
            <div className="flex items-center justify-center p-6">
              <Loader2 className="size-5 animate-spin text-muted-foreground" />
            </div>
          )}
          {!loading && runs.length === 0 && (
            <p className="p-4 text-sm text-muted-foreground">No plan runs yet</p>
          )}
          {runs.map((run) => {
            const delta = run.work_order_delta ?? 0;
            return (
              <button
                key={run.id}
                type="button"
                className={cn(
                  "w-full border-b border-border px-4 py-3 text-left hover:bg-accent/50",
                  planRunId === run.id && "bg-accent"
                )}
                onClick={() => navigate(`/plan/${run.id}`)}
              >
                <div className="flex items-center justify-between">
                  <span className="truncate text-sm font-medium">{run.spec_file}</span>
                  <StatusBadge status={run.audit_changed ? "completed" : "pending"} size="sm" />
                </div>
                <div className="mt-1 flex items-center gap-2 text-xs text-muted-foreground">
                  <span>{run.post_audit_work_order_count ?? run.work_orders_generated ?? 0} WOs</span>
                  {delta !== 0 && (
                    <span className={delta > 0 ? "text-green-400" : "text-red-400"}>
                      {delta > 0 ? `+${delta}` : delta}
                    </span>
                  )}
                  {delta === 0 && <span className="text-zinc-500">0</span>}
                  <span className="ml-auto"><TimeAgo timestamp={run.created_at} /></span>
                </div>
              </button>
            );
          })}
        </div>
      </div>

      {/* Right panel — detail */}
      <div className="flex-1 overflow-auto">
        <Outlet />
        {!planRunId && !window.location.pathname.includes("/new") && (
          <div className="flex h-full items-center justify-center text-sm text-muted-foreground">
            Select a plan run to view details
          </div>
        )}
      </div>
    </div>
  );
}
```

- [ ] **Step 3: Create NewPlan.tsx stub**

```tsx
import { useState } from "react";
import { useNavigate } from "react-router-dom";
import { Loader2, Upload } from "lucide-react";
import { submitPlan } from "@/api/client";
import { CodeViewer } from "@/components/CodeViewer";
import { Button } from "@/components/ui/button";

export default function NewPlan() {
  const navigate = useNavigate();
  const [specContent, setSpecContent] = useState("");
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [progress, setProgress] = useState<string[]>([]);

  const handleFileLoad = () => {
    const input = document.createElement("input");
    input.type = "file";
    input.accept = ".md";
    input.onchange = (e) => {
      const file = (e.target as HTMLInputElement).files?.[0];
      if (!file) return;
      const reader = new FileReader();
      reader.onload = () => setSpecContent(reader.result as string);
      reader.readAsText(file);
    };
    input.click();
  };

  const handleGenerate = async () => {
    setSubmitting(true);
    setError(null);
    setProgress(["Starting plan generation..."]);
    try {
      const result = await submitPlan({ spec_content: specContent });
      setProgress((p) => [...p, `Session created: ${result.session_id}`]);
      // Navigate to the session detail (plan run will appear there).
      setTimeout(() => navigate(`/plan/${result.session_id}`), 1500);
    } catch (err: unknown) {
      setError(err instanceof Error ? err.message : "Plan generation failed");
      setSubmitting(false);
    }
  };

  return (
    <div className="flex flex-col gap-4 p-6">
      <div className="flex items-center justify-between">
        <h2 className="text-lg font-semibold">New Plan</h2>
        <Button variant="outline" size="sm" onClick={handleFileLoad}>
          <Upload className="mr-1 size-3" /> Load File
        </Button>
      </div>

      <CodeViewer
        value={specContent}
        onChange={setSpecContent}
        language="markdown"
        height="calc(60vh - 100px)"
        placeholder="Paste your feature spec here..."
      />

      <div className="flex items-center gap-3">
        <Button
          disabled={!specContent.trim() || submitting}
          onClick={handleGenerate}
        >
          {submitting ? (
            <>
              <Loader2 className="mr-1 size-3 animate-spin" /> Generating...
            </>
          ) : (
            "Generate Plan"
          )}
        </Button>
        {error && <p className="text-sm text-destructive">{error}</p>}
      </div>

      {progress.length > 0 && (
        <div className="rounded-lg border border-border bg-card p-3 space-y-1">
          {progress.map((msg, i) => (
            <p key={i} className="text-xs text-muted-foreground">{msg}</p>
          ))}
        </div>
      )}
    </div>
  );
}
```

- [ ] **Step 4: Create PlanRunDetail.tsx**

```tsx
import { useEffect, useState } from "react";
import { useParams, useNavigate } from "react-router-dom";
import { ChevronDown, ChevronRight, Loader2, Edit3, Save, X, Check } from "lucide-react";
import { getPlanAuditStats, listWorkOrders, getWorkOrder, updateWorkOrder } from "@/api/client";
import { addQueueItems } from "@/api/client";
import type { PlanAuditRun, WorkOrderFile, QueueAddItem } from "@/types/api";
import { MetricCard } from "@/components/MetricCard";
import { CopyableID } from "@/components/CopyableID";
import { TimeAgo } from "@/components/TimeAgo";
import { StatusBadge } from "@/components/StatusBadge";
import { CodeViewer } from "@/components/CodeViewer";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";

export default function PlanRunDetail() {
  const { planRunId } = useParams();
  const navigate = useNavigate();
  const [run, setRun] = useState<PlanAuditRun | null>(null);
  const [workOrders, setWorkOrders] = useState<WorkOrderFile[]>([]);
  const [loading, setLoading] = useState(true);
  const [expanded, setExpanded] = useState<Set<string>>(new Set());
  const [contentCache, setContentCache] = useState<Record<string, string>>({});
  const [editing, setEditing] = useState<string | null>(null);
  const [editContent, setEditContent] = useState("");
  const [editError, setEditError] = useState<string | null>(null);
  const [saving, setSaving] = useState(false);
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const [queueing, setQueueing] = useState(false);
  const [queueSuccess, setQueueSuccess] = useState(false);

  useEffect(() => {
    if (!planRunId) return;
    setLoading(true);

    Promise.all([
      getPlanAuditStats(100).then((stats) => {
        const found = stats.recent_runs.find((r) => r.id === planRunId || r.session_id === planRunId);
        setRun(found ?? null);
      }),
      listWorkOrders().then((resp) => setWorkOrders(resp.work_orders)),
    ])
      .catch(() => {})
      .finally(() => setLoading(false));
  }, [planRunId]);

  const toggleExpand = async (filename: string) => {
    const next = new Set(expanded);
    if (next.has(filename)) {
      next.delete(filename);
    } else {
      next.add(filename);
      // Fetch content if not cached.
      if (!contentCache[filename]) {
        try {
          const resp = await getWorkOrder(filename);
          setContentCache((prev) => ({ ...prev, [filename]: resp.content }));
        } catch {
          // Silently fail.
        }
      }
    }
    setExpanded(next);
  };

  const startEdit = (filename: string) => {
    setEditing(filename);
    setEditContent(contentCache[filename] ?? "");
    setEditError(null);
  };

  const cancelEdit = () => {
    setEditing(null);
    setEditContent("");
    setEditError(null);
  };

  const saveEdit = async (filename: string) => {
    setSaving(true);
    setEditError(null);
    try {
      const resp = await updateWorkOrder(filename, editContent);
      setContentCache((prev) => ({ ...prev, [filename]: resp.content }));
      setEditing(null);
    } catch (err: unknown) {
      setEditError(err instanceof Error ? err.message : "Save failed");
    } finally {
      setSaving(false);
    }
  };

  const toggleSelect = (filename: string) => {
    setSelected((prev) => {
      const next = new Set(prev);
      if (next.has(filename)) next.delete(filename); else next.add(filename);
      return next;
    });
  };

  const selectAll = () => {
    if (selected.size === workOrders.length) {
      setSelected(new Set());
    } else {
      setSelected(new Set(workOrders.map((wo) => wo.filename)));
    }
  };

  const handleQueueSelected = async () => {
    setQueueing(true);
    try {
      const items: QueueAddItem[] = [...selected].map((filename) => ({
        work_order_file: filename,
      }));
      await addQueueItems(items);
      setQueueSuccess(true);
      setSelected(new Set());
      setTimeout(() => setQueueSuccess(false), 5000);
    } catch {
      // Error handled by queue hook.
    } finally {
      setQueueing(false);
    }
  };

  const getAuditAnnotation = (filename: string): { label: string; color: string } | null => {
    if (!run?.audit_changes) return null;
    for (const change of run.audit_changes) {
      const lower = change.toLowerCase();
      if (lower.includes(filename.replace(".yaml", "")) || lower.includes(filename)) {
        if (lower.includes("add")) return { label: "added by audit", color: "text-green-400 bg-green-500/20" };
        if (lower.includes("modif")) return { label: "modified by audit", color: "text-amber-400 bg-amber-500/20" };
        if (lower.includes("remov")) return { label: "removed by audit", color: "text-red-400 bg-red-500/20" };
      }
    }
    return null;
  };

  if (loading) {
    return (
      <div className="flex h-full items-center justify-center">
        <Loader2 className="size-6 animate-spin text-muted-foreground" />
      </div>
    );
  }

  return (
    <div className="space-y-6 p-6">
      {/* Header */}
      {run && (
        <div className="space-y-2">
          <div className="flex items-center gap-3">
            <h2 className="text-lg font-semibold">{run.spec_file}</h2>
            <StatusBadge status={run.audit_changed ? "completed" : "pending"} size="sm" />
          </div>
          <div className="flex items-center gap-4 text-xs text-muted-foreground">
            <CopyableID id={run.id} truncate={run.id.length} />
            <TimeAgo timestamp={run.created_at} />
          </div>
        </div>
      )}

      {/* Audit summary */}
      {run && (
        <section>
          <h3 className="mb-3 text-sm font-medium uppercase tracking-wider text-muted-foreground">
            Audit Summary
          </h3>
          <div className="grid grid-cols-2 gap-4 md:grid-cols-3">
            <MetricCard label="Pre-Audit Count" value={run.pre_audit_work_order_count ?? 0} />
            <MetricCard label="Post-Audit Count" value={run.post_audit_work_order_count ?? 0} />
            <MetricCard
              label="Delta"
              value={
                run.work_order_delta != null
                  ? run.work_order_delta > 0
                    ? `+${run.work_order_delta}`
                    : String(run.work_order_delta)
                  : "0"
              }
            />
            <MetricCard label="WOs Generated" value={run.work_orders_generated ?? 0} />
          </div>
          {run.audit_changes && run.audit_changes.length > 0 && (
            <div className="mt-3 flex flex-wrap gap-1">
              {run.audit_changes.map((change, i) => (
                <Badge key={i} variant="outline" className="text-xs">
                  {change}
                </Badge>
              ))}
            </div>
          )}
        </section>
      )}

      {/* Work orders */}
      <section>
        <div className="mb-3 flex items-center justify-between">
          <h3 className="text-sm font-medium uppercase tracking-wider text-muted-foreground">
            Work Orders ({workOrders.length})
          </h3>
          {workOrders.length > 0 && (
            <Button variant="ghost" size="sm" onClick={selectAll}>
              {selected.size === workOrders.length ? "Deselect All" : "Select All"}
            </Button>
          )}
        </div>

        {workOrders.length === 0 && (
          <p className="text-sm text-muted-foreground">No work orders found</p>
        )}

        <div className="space-y-2">
          {workOrders.map((wo) => {
            const isExpanded = expanded.has(wo.filename);
            const isEditing = editing === wo.filename;
            const annotation = getAuditAnnotation(wo.filename);

            return (
              <div key={wo.filename} className="rounded-lg border border-border bg-card">
                <div className="flex items-center gap-3 px-4 py-3">
                  <input
                    type="checkbox"
                    checked={selected.has(wo.filename)}
                    onChange={() => toggleSelect(wo.filename)}
                    className="rounded"
                  />
                  <button
                    type="button"
                    className="flex flex-1 items-center gap-2 text-left"
                    onClick={() => toggleExpand(wo.filename)}
                  >
                    {isExpanded ? <ChevronDown className="size-3" /> : <ChevronRight className="size-3" />}
                    <span className="text-sm font-medium">{wo.title || wo.filename}</span>
                    <Badge variant="outline" className="text-xs">{wo.type}</Badge>
                    {wo.target_module && (
                      <span className="text-xs text-muted-foreground">{wo.target_module}</span>
                    )}
                    {annotation && (
                      <span className={`rounded px-1.5 py-0.5 text-xs ${annotation.color}`}>
                        {annotation.label}
                      </span>
                    )}
                  </button>
                </div>

                {isExpanded && (
                  <div className="border-t border-border px-4 py-3">
                    <div className="mb-2 flex items-center justify-end gap-2">
                      {!isEditing ? (
                        <Button variant="ghost" size="sm" onClick={() => startEdit(wo.filename)}>
                          <Edit3 className="mr-1 size-3" /> Edit
                        </Button>
                      ) : (
                        <>
                          <Button
                            variant="ghost"
                            size="sm"
                            disabled={saving}
                            onClick={() => saveEdit(wo.filename)}
                          >
                            <Save className="mr-1 size-3" /> {saving ? "Saving..." : "Save"}
                          </Button>
                          <Button variant="ghost" size="sm" onClick={cancelEdit}>
                            <X className="mr-1 size-3" /> Cancel
                          </Button>
                        </>
                      )}
                    </div>
                    {editError && (
                      <p className="mb-2 text-sm text-destructive">{editError}</p>
                    )}
                    <CodeViewer
                      value={isEditing ? editContent : (contentCache[wo.filename] ?? "Loading...")}
                      onChange={isEditing ? setEditContent : undefined}
                      language="yaml"
                      readOnly={!isEditing}
                      height="300px"
                    />
                  </div>
                )}
              </div>
            );
          })}
        </div>
      </section>

      {/* Queue actions */}
      {workOrders.length > 0 && (
        <div className="sticky bottom-0 flex items-center gap-3 border-t border-border bg-background pt-4">
          <Button
            disabled={selected.size === 0 || queueing}
            onClick={handleQueueSelected}
          >
            {queueing ? (
              <><Loader2 className="mr-1 size-3 animate-spin" /> Queueing...</>
            ) : (
              `Queue Selected (${selected.size})`
            )}
          </Button>
          {queueSuccess && (
            <span className="flex items-center gap-1 text-sm text-green-400">
              <Check className="size-3" /> Queued!{" "}
              <button
                type="button"
                className="underline"
                onClick={() => navigate("/pipeline")}
              >
                Open Queue
              </button>
            </span>
          )}
        </div>
      )}
    </div>
  );
}
```

- [ ] **Step 5: Verify build**

Run: `cd web && npm run build`
Expected: PASS

- [ ] **Step 6: Commit frontend**

```bash
cd web
git add -A
git commit -m "feat: implement Plan space UI — CodeViewer, plan list, new plan form, work order cards, queue selection (Spec 05 frontend)"
```

---

## Final Verification

### Task C1: Full build and test

- [ ] **Step 1:** `make build` — PASS
- [ ] **Step 2:** `make test` — PASS (all packages)
- [ ] **Step 3:** `cd web && npm run build` — PASS

---

## Parallel Execution Strategy

Track A (Backend: A1-A3) and Track B (Frontend: B1-B3) are **fully independent** — no shared files. Execute in parallel via worktrees.

**CRITICAL:** Worktrees must branch from the CURRENT main commit (`c7e0b50`), which includes Specs 03 and 04. The `NewServer` signature is `NewServer(db, gitMgr, baseBranch, runQueue, workOrderDir)` — agents must use this exact signature.

## Spec Compliance Notes

- **Epic 2 (Plan Submission):** The POST /api/plan handler creates a session and returns immediately. Full background plan execution integration requires extracting plan logic from `cmd/conductor/plan.go` (package main) into a shared internal package. This is noted as a TODO in the handler. The frontend gracefully handles this — it shows session creation progress and can navigate to plan results when they appear.
- **Epic 9 (Queue Selection):** Fully implemented — checkboxes, select all, "Queue Selected" button calls POST /api/queue, confirmation with "Open Queue" link.
- **All other epics:** Fully implemented per spec.
