# Queue System (Spec 04) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement a server-side run queue that manages sequential execution of work orders with manual start, auto-pause on review/error, and a full management UI.

**Architecture:** The queue is an in-memory service (`RunQueue`) living in the API layer. It manages an ordered list of work order items, executing them one at a time by reusing the same pipeline execution patterns as the CLI. The API server holds the RunQueue and exposes 7 new endpoints (CRUD + control + SSE). Frontend adds a `useQueue` hook with polling, a QueueStrip sidebar widget, and a QueueDrawer management panel.

**Tech Stack:** Go (sync.Mutex, goroutines, chi router), React 18, TypeScript, Tailwind CSS v4, shadcn/ui, HTML5 Drag API

**Spec source:** `docs/specs/UI/04-queue-system/`

---

## File Structure

### Backend — New Files
- `internal/api/runqueue.go` — In-memory RunQueue: data structures, state machine, Add/Remove/Reorder/Start/Pause/Continue/GetState, execution loop, SSE fan-out
- `internal/api/runqueue_test.go` — Unit tests for RunQueue state machine
- `internal/api/queue_handlers.go` — HTTP handlers for all 7 queue endpoints

### Backend — Modified Files
- `internal/api/server.go` — Add `*RunQueue` to Server struct, register 8 new routes, update NewServer
- `internal/api/response.go` — Add queue response types
- `internal/api/server_test.go` — Update NewServer call sites, add queue endpoint tests
- `cmd/conductor/serve.go` — Create RunQueue and pass to NewServer
- `internal/pipeline/events.go` — Add queue event type constants

### Frontend — New Files
- `web/src/hooks/useQueue.ts` — Polling hook with state + action methods
- `web/src/components/QueueStrip.tsx` — Sidebar queue status widget
- `web/src/components/QueueDrawer.tsx` — Full queue management drawer

### Frontend — Modified Files
- `web/src/types/api.ts` — Add QueueItem, QueueState, QueueAddItem interfaces
- `web/src/api/client.ts` — Add 7 queue API client functions
- `web/src/components/Sidebar.tsx` — Replace placeholder with QueueStrip + QueueDrawer

---

## Track A: Backend

### Task A1: RunQueue data structures and state machine

**Files:**
- Create: `internal/api/runqueue.go`
- Create: `internal/api/runqueue_test.go`

- [ ] **Step 1: Create runqueue.go with types and constructor**

```go
package api

import (
	"errors"
	"sync"
	"time"

	"github.com/google/uuid"
)

// Queue-level state constants.
const (
	QueueStateIdle      = "idle"
	QueueStateReady     = "ready"
	QueueStateRunning   = "running"
	QueueStatePaused    = "paused"
	QueueStateCompleted = "completed"
)

// Item-level status constants.
const (
	QueueItemPending   = "pending"
	QueueItemExecuting = "executing"
	QueueItemCompleted = "completed"
	QueueItemFailed    = "failed"
	QueueItemSkipped   = "skipped"
)

var (
	ErrQueueInvalidState = errors.New("invalid queue state for this operation")
	ErrQueueItemNotFound = errors.New("queue item not found")
	ErrQueueItemBusy     = errors.New("cannot remove an executing item")
	ErrQueueNoPending    = errors.New("no pending items")
	ErrQueueInvalidIDs   = errors.New("provided IDs do not match pending items")
)

// QueueItem represents a work order in the queue.
type QueueItem struct {
	ID            string            `json:"id"`
	WorkOrderFile string            `json:"work_order_file"`
	Title         string            `json:"title"`
	Type          string            `json:"type"`
	TargetModule  string            `json:"target_module"`
	Overrides     map[string]string `json:"overrides"`
	Status        string            `json:"status"`
	WorkflowID    string            `json:"workflow_id,omitempty"`
	Error         string            `json:"error,omitempty"`
	AddedAt       time.Time         `json:"added_at"`
}

// QueueSnapshot is a point-in-time copy of the queue state.
type QueueSnapshot struct {
	State       string      `json:"state"`
	Items       []QueueItem `json:"items"`
	Current     *QueueItem  `json:"current"`
	PauseReason string      `json:"pause_reason,omitempty"`
}

// RunQueue manages an ordered list of work orders for sequential execution.
type RunQueue struct {
	mu          sync.Mutex
	state       string
	items       []QueueItem
	current     *QueueItem
	pauseReason string

	// pauseRequested is set when Pause() is called during execution;
	// the execution loop checks this after each item completes.
	pauseRequested bool

	// onEvent is called (under lock) for SSE fan-out. May be nil.
	onEvent func(eventType string, payload map[string]any)

	// executeFn is called to run a work order. Set by the server at init.
	// Signature: (workOrderFile string, overrides map[string]string) -> (workflowID string, err error)
	// The function should create the workflow and return immediately.
	// A nil executeFn means execution is not available (queue is display-only).
	executeFn func(workOrderFile string, overrides map[string]string) (string, error)

	// monitorFn polls a workflow and returns its final state.
	// Signature: (workflowID string) -> (finalState string, err error)
	// Returns when workflow reaches human_review, completed, or failed.
	// A nil monitorFn means execution is not available.
	monitorFn func(workflowID string) (string, error)
}

// NewRunQueue creates an empty queue in idle state.
func NewRunQueue() *RunQueue {
	return &RunQueue{
		state: QueueStateIdle,
	}
}
```

- [ ] **Step 2: Implement Add method**

```go
// Add appends items to the queue. Each item gets a UUID and starts as pending.
func (rq *RunQueue) Add(items []QueueItem) {
	rq.mu.Lock()
	defer rq.mu.Unlock()

	for i := range items {
		items[i].ID = uuid.New().String()
		items[i].Status = QueueItemPending
		items[i].AddedAt = time.Now().UTC()
	}
	rq.items = append(rq.items, items...)

	if rq.state == QueueStateIdle || rq.state == QueueStateCompleted {
		rq.setState(QueueStateReady)
	}
}

func (rq *RunQueue) setState(newState string) {
	prev := rq.state
	rq.state = newState
	if rq.onEvent != nil && prev != newState {
		rq.onEvent("queue_state_changed", map[string]any{
			"state":          newState,
			"previous_state": prev,
		})
	}
}
```

- [ ] **Step 3: Implement Remove method**

```go
// Remove removes an item by ID. Returns error if executing or not found.
func (rq *RunQueue) Remove(id string) error {
	rq.mu.Lock()
	defer rq.mu.Unlock()

	for i, item := range rq.items {
		if item.ID == id {
			if item.Status == QueueItemExecuting {
				return ErrQueueItemBusy
			}
			rq.items = append(rq.items[:i], rq.items[i+1:]...)

			// If no pending items remain and queue is ready, go idle.
			if rq.state == QueueStateReady && !rq.hasPending() {
				rq.setState(QueueStateIdle)
			}
			return nil
		}
	}
	return ErrQueueItemNotFound
}

func (rq *RunQueue) hasPending() bool {
	for _, item := range rq.items {
		if item.Status == QueueItemPending {
			return true
		}
	}
	return false
}
```

- [ ] **Step 4: Implement Reorder method**

```go
// Reorder sets a new order for pending items. All pending item IDs must be provided.
func (rq *RunQueue) Reorder(ids []string) error {
	rq.mu.Lock()
	defer rq.mu.Unlock()

	// Collect pending items into a map.
	pending := make(map[string]*QueueItem)
	for i := range rq.items {
		if rq.items[i].Status == QueueItemPending {
			pending[rq.items[i].ID] = &rq.items[i]
		}
	}

	if len(ids) != len(pending) {
		return ErrQueueInvalidIDs
	}
	for _, id := range ids {
		if _, ok := pending[id]; !ok {
			return ErrQueueInvalidIDs
		}
	}

	// Rebuild items: non-pending first (in original order), then pending in new order.
	var rebuilt []QueueItem
	for _, item := range rq.items {
		if item.Status != QueueItemPending {
			rebuilt = append(rebuilt, item)
		}
	}
	for _, id := range ids {
		rebuilt = append(rebuilt, *pending[id])
	}
	rq.items = rebuilt
	return nil
}
```

- [ ] **Step 5: Implement GetState method**

```go
// GetState returns a snapshot of the current queue state.
func (rq *RunQueue) GetState() QueueSnapshot {
	rq.mu.Lock()
	defer rq.mu.Unlock()

	items := make([]QueueItem, len(rq.items))
	copy(items, rq.items)

	var current *QueueItem
	if rq.current != nil {
		c := *rq.current
		current = &c
	}

	return QueueSnapshot{
		State:       rq.state,
		Items:       items,
		Current:     current,
		PauseReason: rq.pauseReason,
	}
}
```

- [ ] **Step 6: Write tests for state machine**

Create `internal/api/runqueue_test.go`:

```go
package api

import "testing"

func TestRunQueueAddTransitionsToReady(t *testing.T) {
	rq := NewRunQueue()
	if rq.GetState().State != QueueStateIdle {
		t.Fatalf("initial state = %q, want idle", rq.GetState().State)
	}
	rq.Add([]QueueItem{{WorkOrderFile: "wo.yaml", Title: "Test"}})
	snap := rq.GetState()
	if snap.State != QueueStateReady {
		t.Fatalf("state = %q, want ready", snap.State)
	}
	if len(snap.Items) != 1 {
		t.Fatalf("items = %d, want 1", len(snap.Items))
	}
	if snap.Items[0].Status != QueueItemPending {
		t.Fatalf("item status = %q, want pending", snap.Items[0].Status)
	}
}

func TestRunQueueRemovePendingItem(t *testing.T) {
	rq := NewRunQueue()
	rq.Add([]QueueItem{{WorkOrderFile: "a.yaml"}, {WorkOrderFile: "b.yaml"}})
	id := rq.GetState().Items[0].ID
	if err := rq.Remove(id); err != nil {
		t.Fatalf("Remove() error: %v", err)
	}
	if len(rq.GetState().Items) != 1 {
		t.Fatalf("items = %d, want 1", len(rq.GetState().Items))
	}
}

func TestRunQueueRemoveLastPendingGoesIdle(t *testing.T) {
	rq := NewRunQueue()
	rq.Add([]QueueItem{{WorkOrderFile: "a.yaml"}})
	id := rq.GetState().Items[0].ID
	_ = rq.Remove(id)
	if rq.GetState().State != QueueStateIdle {
		t.Fatalf("state = %q, want idle", rq.GetState().State)
	}
}

func TestRunQueueRemoveExecutingReturnsError(t *testing.T) {
	rq := NewRunQueue()
	rq.Add([]QueueItem{{WorkOrderFile: "a.yaml"}})
	// Manually set to executing for test.
	rq.mu.Lock()
	rq.items[0].Status = QueueItemExecuting
	rq.state = QueueStateRunning
	rq.mu.Unlock()
	err := rq.Remove(rq.GetState().Items[0].ID)
	if err != ErrQueueItemBusy {
		t.Fatalf("Remove() error = %v, want ErrQueueItemBusy", err)
	}
}

func TestRunQueueReorder(t *testing.T) {
	rq := NewRunQueue()
	rq.Add([]QueueItem{
		{WorkOrderFile: "a.yaml", Title: "A"},
		{WorkOrderFile: "b.yaml", Title: "B"},
		{WorkOrderFile: "c.yaml", Title: "C"},
	})
	snap := rq.GetState()
	// Reverse order.
	err := rq.Reorder([]string{snap.Items[2].ID, snap.Items[1].ID, snap.Items[0].ID})
	if err != nil {
		t.Fatalf("Reorder() error: %v", err)
	}
	after := rq.GetState()
	if after.Items[0].Title != "C" || after.Items[1].Title != "B" || after.Items[2].Title != "A" {
		t.Fatalf("reorder failed: %v", after.Items)
	}
}

func TestRunQueueReorderInvalidIDs(t *testing.T) {
	rq := NewRunQueue()
	rq.Add([]QueueItem{{WorkOrderFile: "a.yaml"}})
	err := rq.Reorder([]string{"nonexistent"})
	if err != ErrQueueInvalidIDs {
		t.Fatalf("Reorder() error = %v, want ErrQueueInvalidIDs", err)
	}
}

func TestRunQueueGetStateReturnsCopy(t *testing.T) {
	rq := NewRunQueue()
	rq.Add([]QueueItem{{WorkOrderFile: "a.yaml"}})
	snap := rq.GetState()
	snap.Items[0].Title = "mutated"
	if rq.GetState().Items[0].Title == "mutated" {
		t.Fatal("GetState returned a reference, not a copy")
	}
}
```

- [ ] **Step 7: Run tests**

Run: `make test`
Expected: PASS

---

### Task A2: RunQueue execution loop (Start, Pause, Continue)

**Files:**
- Modify: `internal/api/runqueue.go`
- Modify: `internal/api/runqueue_test.go`

- [ ] **Step 1: Implement Start method**

```go
// Start begins executing from the first pending item.
func (rq *RunQueue) Start() error {
	rq.mu.Lock()
	defer rq.mu.Unlock()

	if rq.state != QueueStateReady {
		return ErrQueueInvalidState
	}
	if !rq.hasPending() {
		return ErrQueueNoPending
	}

	rq.pauseRequested = false
	rq.pauseReason = ""
	rq.setState(QueueStateRunning)
	rq.advanceLocked()
	return nil
}

// advanceLocked picks the next pending item and begins execution.
// Must be called with rq.mu held.
func (rq *RunQueue) advanceLocked() {
	for i := range rq.items {
		if rq.items[i].Status == QueueItemPending {
			rq.items[i].Status = QueueItemExecuting
			item := rq.items[i]
			rq.current = &item
			if rq.onEvent != nil {
				rq.onEvent("queue_item_started", map[string]any{
					"item_id":         item.ID,
					"work_order_file": item.WorkOrderFile,
				})
			}
			go rq.executeItem(item)
			return
		}
	}
	// No pending items — mark completed.
	rq.current = nil
	rq.setState(QueueStateCompleted)
}
```

- [ ] **Step 2: Implement executeItem goroutine**

```go
// executeItem runs a single queue item and handles the result.
func (rq *RunQueue) executeItem(item QueueItem) {
	if rq.executeFn == nil {
		rq.completeItem(item.ID, QueueItemFailed, "", "execution not available: server started without pipeline config")
		return
	}

	workflowID, err := rq.executeFn(item.WorkOrderFile, item.Overrides)
	if err != nil {
		rq.completeItem(item.ID, QueueItemFailed, "", err.Error())
		return
	}

	// Store workflow ID.
	rq.mu.Lock()
	for i := range rq.items {
		if rq.items[i].ID == item.ID {
			rq.items[i].WorkflowID = workflowID
			break
		}
	}
	if rq.current != nil && rq.current.ID == item.ID {
		rq.current.WorkflowID = workflowID
	}
	if rq.onEvent != nil {
		rq.onEvent("queue_item_started", map[string]any{
			"item_id":         item.ID,
			"work_order_file": item.WorkOrderFile,
			"workflow_id":     workflowID,
		})
	}
	rq.mu.Unlock()

	// Monitor workflow until terminal state.
	if rq.monitorFn == nil {
		rq.completeItem(item.ID, QueueItemFailed, workflowID, "monitoring not available")
		return
	}

	finalState, err := rq.monitorFn(workflowID)
	if err != nil {
		rq.completeItem(item.ID, QueueItemFailed, workflowID, err.Error())
		return
	}

	switch finalState {
	case "completed":
		rq.completeItem(item.ID, QueueItemCompleted, workflowID, "")
	case "human_review":
		rq.pauseForReview(item.ID, workflowID)
	default: // failed or unknown
		rq.completeItem(item.ID, QueueItemFailed, workflowID, "workflow ended in state: "+finalState)
	}
}

func (rq *RunQueue) completeItem(itemID, status, workflowID, errMsg string) {
	rq.mu.Lock()
	defer rq.mu.Unlock()

	for i := range rq.items {
		if rq.items[i].ID == itemID {
			rq.items[i].Status = status
			rq.items[i].Error = errMsg
			if workflowID != "" {
				rq.items[i].WorkflowID = workflowID
			}
			break
		}
	}
	rq.current = nil

	if rq.onEvent != nil {
		rq.onEvent("queue_item_completed", map[string]any{
			"item_id":     itemID,
			"status":      status,
			"workflow_id": workflowID,
		})
	}

	// Auto-pause on failure.
	if status == QueueItemFailed {
		rq.pauseReason = "error"
		rq.setState(QueueStatePaused)
		return
	}

	// Check for manual pause request.
	if rq.pauseRequested {
		rq.pauseRequested = false
		rq.pauseReason = "manual"
		rq.setState(QueueStatePaused)
		return
	}

	// Advance to next item.
	rq.advanceLocked()
}

func (rq *RunQueue) pauseForReview(itemID, workflowID string) {
	rq.mu.Lock()
	defer rq.mu.Unlock()

	// Keep item as executing (still in review).
	rq.pauseReason = "awaiting_review"
	rq.setState(QueueStatePaused)
}
```

- [ ] **Step 3: Implement Pause and Continue methods**

```go
// Pause requests the queue to pause. If between items, pauses immediately.
func (rq *RunQueue) Pause() error {
	rq.mu.Lock()
	defer rq.mu.Unlock()

	if rq.state != QueueStateRunning {
		return ErrQueueInvalidState
	}

	if rq.current != nil {
		// Workflow executing — pause after it completes.
		rq.pauseRequested = true
	} else {
		// Between items — pause now.
		rq.pauseReason = "manual"
		rq.setState(QueueStatePaused)
	}
	return nil
}

// Continue resumes execution after a pause.
func (rq *RunQueue) Continue() error {
	rq.mu.Lock()
	defer rq.mu.Unlock()

	if rq.state != QueueStatePaused {
		return ErrQueueInvalidState
	}

	// If current item is still executing (awaiting_review pause),
	// mark it completed (the workflow was approved/rejected externally).
	if rq.current != nil {
		for i := range rq.items {
			if rq.items[i].ID == rq.current.ID && rq.items[i].Status == QueueItemExecuting {
				rq.items[i].Status = QueueItemCompleted
				break
			}
		}
		rq.current = nil
	}

	if !rq.hasPending() {
		rq.setState(QueueStateCompleted)
		return nil
	}

	rq.pauseReason = ""
	rq.pauseRequested = false
	rq.setState(QueueStateRunning)
	rq.advanceLocked()
	return nil
}
```

- [ ] **Step 4: Add tests for Start/Pause/Continue**

Append to `runqueue_test.go`:

```go
func TestRunQueueStartRequiresReady(t *testing.T) {
	rq := NewRunQueue()
	if err := rq.Start(); err != ErrQueueInvalidState {
		t.Fatalf("Start() on idle = %v, want ErrQueueInvalidState", err)
	}
}

func TestRunQueueStartTransitionsToRunning(t *testing.T) {
	rq := NewRunQueue()
	rq.Add([]QueueItem{{WorkOrderFile: "a.yaml"}})
	// Set a no-op executeFn so Start doesn't fail.
	rq.executeFn = func(string, map[string]string) (string, error) {
		return "wf-1", nil
	}
	rq.monitorFn = func(string) (string, error) {
		return "completed", nil
	}
	if err := rq.Start(); err != nil {
		t.Fatalf("Start() error: %v", err)
	}
	// State should be running (or completed if execution was instant).
	s := rq.GetState().State
	if s != QueueStateRunning && s != QueueStateCompleted {
		t.Fatalf("state = %q, want running or completed", s)
	}
}

func TestRunQueuePauseRequiresRunning(t *testing.T) {
	rq := NewRunQueue()
	if err := rq.Pause(); err != ErrQueueInvalidState {
		t.Fatalf("Pause() on idle = %v, want ErrQueueInvalidState", err)
	}
}

func TestRunQueueContinueRequiresPaused(t *testing.T) {
	rq := NewRunQueue()
	if err := rq.Continue(); err != ErrQueueInvalidState {
		t.Fatalf("Continue() on idle = %v, want ErrQueueInvalidState", err)
	}
}
```

- [ ] **Step 5: Run tests**

Run: `make test`
Expected: PASS

---

### Task A3: Queue response types and event constants

**Files:**
- Modify: `internal/api/response.go`
- Modify: `internal/pipeline/events.go`

- [ ] **Step 1: Add queue response types to response.go**

Append to `internal/api/response.go`:

```go
// --- Queue response types ---

type queueItemResponse struct {
	ID            string            `json:"id"`
	WorkOrderFile string            `json:"work_order_file"`
	Title         string            `json:"title"`
	Type          string            `json:"type"`
	TargetModule  string            `json:"target_module"`
	Overrides     map[string]string `json:"overrides"`
	Status        string            `json:"status"`
	WorkflowID    string            `json:"workflow_id,omitempty"`
	Error         string            `json:"error,omitempty"`
	AddedAt       string            `json:"added_at"`
}

type queueStateResponse struct {
	State       string              `json:"state"`
	Items       []queueItemResponse `json:"items"`
	Current     *queueItemResponse  `json:"current"`
	PauseReason string              `json:"pause_reason,omitempty"`
}

type queueAddItemRequest struct {
	WorkOrderFile string            `json:"work_order_file"`
	Overrides     map[string]string `json:"overrides"`
}

type queueAddRequest struct {
	Items []queueAddItemRequest `json:"items"`
}

type queueReorderRequest struct {
	Order []string `json:"order"`
}

func mapQueueState(snap QueueSnapshot) queueStateResponse {
	items := make([]queueItemResponse, 0, len(snap.Items))
	for _, item := range snap.Items {
		items = append(items, mapQueueItem(item))
	}
	var current *queueItemResponse
	if snap.Current != nil {
		c := mapQueueItem(*snap.Current)
		current = &c
	}
	return queueStateResponse{
		State:       snap.State,
		Items:       items,
		Current:     current,
		PauseReason: snap.PauseReason,
	}
}

func mapQueueItem(item QueueItem) queueItemResponse {
	overrides := item.Overrides
	if overrides == nil {
		overrides = make(map[string]string)
	}
	return queueItemResponse{
		ID:            item.ID,
		WorkOrderFile: item.WorkOrderFile,
		Title:         item.Title,
		Type:          item.Type,
		TargetModule:  item.TargetModule,
		Overrides:     overrides,
		Status:        item.Status,
		WorkflowID:    item.WorkflowID,
		Error:         item.Error,
		AddedAt:       item.AddedAt.Format(time.RFC3339),
	}
}
```

Add `"time"` to imports in response.go.

- [ ] **Step 2: Add queue event constants to pipeline/events.go**

Append to `internal/pipeline/events.go`:

```go
// Queue-level event types.
const (
	EventQueueStateChanged  = "queue_state_changed"
	EventQueueItemStarted   = "queue_item_started"
	EventQueueItemCompleted = "queue_item_completed"
)
```

- [ ] **Step 3: Verify build**

Run: `make build`
Expected: PASS

---

### Task A4: Queue HTTP handlers

**Files:**
- Create: `internal/api/queue_handlers.go`

- [ ] **Step 1: Create queue_handlers.go with all 7 handlers + SSE**

```go
package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-chi/chi/v5"
	"gopkg.in/yaml.v3"
)

// handleGetQueue returns the current queue state.
func (s *Server) handleGetQueue(w http.ResponseWriter, r *http.Request) {
	if s.runQueue == nil {
		writeError(w, http.StatusServiceUnavailable, "queue not available")
		return
	}
	writeJSON(w, http.StatusOK, mapQueueState(s.runQueue.GetState()))
}

// handleAddQueueItems adds work order items to the queue.
func (s *Server) handleAddQueueItems(w http.ResponseWriter, r *http.Request) {
	if s.runQueue == nil {
		writeError(w, http.StatusServiceUnavailable, "queue not available")
		return
	}

	var req queueAddRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if len(req.Items) == 0 {
		writeError(w, http.StatusBadRequest, "items array is required")
		return
	}

	var items []QueueItem
	for _, addItem := range req.Items {
		if addItem.WorkOrderFile == "" {
			writeError(w, http.StatusBadRequest, "work_order_file is required")
			return
		}

		// Resolve work order file path.
		woPath := addItem.WorkOrderFile
		if !filepath.IsAbs(woPath) && s.workOrderDir != "" {
			woPath = filepath.Join(s.workOrderDir, woPath)
		}

		data, err := os.ReadFile(woPath)
		if err != nil {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("work order file not found: %s", addItem.WorkOrderFile))
			return
		}

		// Parse YAML to extract metadata.
		var wo struct {
			Title        string `yaml:"title"`
			Type         string `yaml:"type"`
			TargetModule string `yaml:"target_module"`
		}
		if err := yaml.Unmarshal(data, &wo); err != nil {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid work order YAML: %s: %v", addItem.WorkOrderFile, err))
			return
		}

		overrides := addItem.Overrides
		if overrides == nil {
			overrides = make(map[string]string)
		}

		items = append(items, QueueItem{
			WorkOrderFile: addItem.WorkOrderFile,
			Title:         wo.Title,
			Type:          wo.Type,
			TargetModule:  wo.TargetModule,
			Overrides:     overrides,
		})
	}

	s.runQueue.Add(items)
	writeJSON(w, http.StatusOK, mapQueueState(s.runQueue.GetState()))
}

// handleRemoveQueueItem removes a pending item from the queue.
func (s *Server) handleRemoveQueueItem(w http.ResponseWriter, r *http.Request) {
	if s.runQueue == nil {
		writeError(w, http.StatusServiceUnavailable, "queue not available")
		return
	}

	id := chi.URLParam(r, "id")
	err := s.runQueue.Remove(id)
	if err != nil {
		switch {
		case errors.Is(err, ErrQueueItemNotFound):
			writeError(w, http.StatusNotFound, "queue item not found")
		case errors.Is(err, ErrQueueItemBusy):
			writeError(w, http.StatusConflict, "cannot remove an executing item")
		default:
			writeError(w, http.StatusInternalServerError, err.Error())
		}
		return
	}
	writeJSON(w, http.StatusOK, mapQueueState(s.runQueue.GetState()))
}

// handleReorderQueue reorders pending items.
func (s *Server) handleReorderQueue(w http.ResponseWriter, r *http.Request) {
	if s.runQueue == nil {
		writeError(w, http.StatusServiceUnavailable, "queue not available")
		return
	}

	var req queueReorderRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if err := s.runQueue.Reorder(req.Order); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, mapQueueState(s.runQueue.GetState()))
}

// handleStartQueue begins queue execution.
func (s *Server) handleStartQueue(w http.ResponseWriter, r *http.Request) {
	if s.runQueue == nil {
		writeError(w, http.StatusServiceUnavailable, "queue not available")
		return
	}

	if err := s.runQueue.Start(); err != nil {
		if errors.Is(err, ErrQueueInvalidState) || errors.Is(err, ErrQueueNoPending) {
			writeError(w, http.StatusConflict, err.Error())
		} else {
			writeError(w, http.StatusInternalServerError, err.Error())
		}
		return
	}
	writeJSON(w, http.StatusOK, mapQueueState(s.runQueue.GetState()))
}

// handlePauseQueue pauses queue execution.
func (s *Server) handlePauseQueue(w http.ResponseWriter, r *http.Request) {
	if s.runQueue == nil {
		writeError(w, http.StatusServiceUnavailable, "queue not available")
		return
	}

	if err := s.runQueue.Pause(); err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, mapQueueState(s.runQueue.GetState()))
}

// handleContinueQueue resumes queue execution after pause.
func (s *Server) handleContinueQueue(w http.ResponseWriter, r *http.Request) {
	if s.runQueue == nil {
		writeError(w, http.StatusServiceUnavailable, "queue not available")
		return
	}

	if err := s.runQueue.Continue(); err != nil {
		if errors.Is(err, ErrQueueInvalidState) {
			writeError(w, http.StatusConflict, err.Error())
		} else {
			writeError(w, http.StatusInternalServerError, err.Error())
		}
		return
	}
	writeJSON(w, http.StatusOK, mapQueueState(s.runQueue.GetState()))
}

// handleQueueEvents streams queue events via SSE.
func (s *Server) handleQueueEvents(w http.ResponseWriter, r *http.Request) {
	if s.runQueue == nil {
		writeError(w, http.StatusServiceUnavailable, "queue not available")
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming not supported")
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	// Register listener channel.
	ch := s.runQueue.Subscribe()
	defer s.runQueue.Unsubscribe(ch)

	eventID := 0
	for {
		select {
		case <-r.Context().Done():
			return
		case event, ok := <-ch:
			if !ok {
				return
			}
			eventID++
			payload, _ := json.Marshal(event)
			fmt.Fprintf(w, "id: %d\ndata: %s\n\n", eventID, payload)
			flusher.Flush()
		}
	}
}
```

- [ ] **Step 2: Add Subscribe/Unsubscribe to RunQueue for SSE fan-out**

Add to `internal/api/runqueue.go`:

```go
// subscribers holds SSE listener channels.
// Add field to RunQueue struct:
//   subscribers []chan map[string]any

// Subscribe registers a channel to receive queue events.
func (rq *RunQueue) Subscribe() chan map[string]any {
	rq.mu.Lock()
	defer rq.mu.Unlock()
	ch := make(chan map[string]any, 32)
	rq.subscribers = append(rq.subscribers, ch)
	return ch
}

// Unsubscribe removes a channel from the subscriber list.
func (rq *RunQueue) Unsubscribe(ch chan map[string]any) {
	rq.mu.Lock()
	defer rq.mu.Unlock()
	for i, sub := range rq.subscribers {
		if sub == ch {
			rq.subscribers = append(rq.subscribers[:i], rq.subscribers[i+1:]...)
			close(ch)
			return
		}
	}
}
```

Update `NewRunQueue` to initialize `subscribers: nil`.

Update `setState` and add to the RunQueue struct a `subscribers []chan map[string]any` field. Wire the `onEvent` callback to broadcast to subscribers:

```go
// In NewRunQueue, after creation:
func NewRunQueue() *RunQueue {
	rq := &RunQueue{state: QueueStateIdle}
	rq.onEvent = func(eventType string, payload map[string]any) {
		event := map[string]any{
			"event_type": eventType,
		}
		for k, v := range payload {
			event[k] = v
		}
		for _, ch := range rq.subscribers {
			select {
			case ch <- event:
			default: // drop if slow consumer
			}
		}
	}
	return rq
}
```

- [ ] **Step 3: Verify build**

Run: `make build`
Expected: PASS

---

### Task A5: Wire queue into Server and serve.go

**Files:**
- Modify: `internal/api/server.go`
- Modify: `internal/api/server_test.go`
- Modify: `cmd/conductor/serve.go`

- [ ] **Step 1: Add RunQueue and workOrderDir to Server struct, update NewServer**

In `internal/api/server.go`:

```go
type Server struct {
	db           *database.DB
	gitMgr       *git.GitManager
	baseBranch   string
	runQueue     *RunQueue
	workOrderDir string
}

func NewServer(db *database.DB, gitMgr *git.GitManager, baseBranch string, runQueue *RunQueue, workOrderDir string) http.Handler {
	s := &Server{db: db, gitMgr: gitMgr, baseBranch: baseBranch, runQueue: runQueue, workOrderDir: workOrderDir}
	r := chi.NewRouter()
	// ... existing routes ...

	// Queue routes — before SPA fallback:
	r.Get("/api/queue", s.handleGetQueue)
	r.Post("/api/queue", s.handleAddQueueItems)
	r.Delete("/api/queue/{id}", s.handleRemoveQueueItem)
	r.Put("/api/queue/reorder", s.handleReorderQueue)
	r.Post("/api/queue/start", s.handleStartQueue)
	r.Post("/api/queue/pause", s.handlePauseQueue)
	r.Post("/api/queue/continue", s.handleContinueQueue)
	r.Get("/api/queue/events", s.handleQueueEvents)

	r.Get("/*", s.handleSPAFallback)
	return r
}
```

- [ ] **Step 2: Update all NewServer call sites**

In `internal/api/server_test.go`, update every `NewServer(db, nil, "main")` to `NewServer(db, nil, "main", nil, "")`.

In `cmd/conductor/serve.go`, update the call:
```go
rq := api.NewRunQueue()
if err := http.ListenAndServe(serveAddr, api.NewServer(db, gitMgr, baseBranch, rq, "")); err != nil {
```

Note: `workOrderDir` is empty for now — the POST /api/queue handler will treat WorkOrderFile paths as absolute if no dir is set. Future: resolve from config.

- [ ] **Step 3: Verify build and tests**

Run: `make build && make test`
Expected: PASS

---

### Task A6: Queue endpoint tests

**Files:**
- Modify: `internal/api/server_test.go`

- [ ] **Step 1: Add test for GET /api/queue (empty)**

```go
func TestGetQueueEndpointEmpty(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	rq := NewRunQueue()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/queue", nil)
	NewServer(db, nil, "main", rq, "").ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	var payload queueStateResponse
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if payload.State != "idle" {
		t.Fatalf("state = %q, want idle", payload.State)
	}
}
```

- [ ] **Step 2: Add test for POST /api/queue (add items)**

```go
func TestAddQueueItemsEndpoint(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	rq := NewRunQueue()

	// Create a temp work order file.
	dir := t.TempDir()
	woPath := filepath.Join(dir, "test-wo.yaml")
	os.WriteFile(woPath, []byte("title: Test WO\ntype: new_feature\ntarget_module: core\nrequirements:\n  - do something\n"), 0644)

	body := strings.NewReader(fmt.Sprintf(`{"items":[{"work_order_file":"%s"}]}`, woPath))
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/queue", body)
	NewServer(db, nil, "main", rq, "").ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var payload queueStateResponse
	json.NewDecoder(rec.Body).Decode(&payload)
	if payload.State != "ready" {
		t.Fatalf("state = %q, want ready", payload.State)
	}
	if len(payload.Items) != 1 {
		t.Fatalf("items = %d, want 1", len(payload.Items))
	}
	if payload.Items[0].Title != "Test WO" {
		t.Fatalf("title = %q, want Test WO", payload.Items[0].Title)
	}
}
```

- [ ] **Step 3: Add test for DELETE /api/queue/:id**

```go
func TestRemoveQueueItemEndpoint(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	rq := NewRunQueue()
	rq.Add([]QueueItem{{WorkOrderFile: "a.yaml", Title: "A"}})
	id := rq.GetState().Items[0].ID

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodDelete, "/api/queue/"+id, nil)
	NewServer(db, nil, "main", rq, "").ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
}
```

- [ ] **Step 4: Add test for start with invalid state returns 409**

```go
func TestStartQueueEndpointInvalidState(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	rq := NewRunQueue() // idle, no items
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/queue/start", nil)
	NewServer(db, nil, "main", rq, "").ServeHTTP(rec, req)
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusConflict)
	}
}
```

- [ ] **Step 5: Run tests**

Run: `make test`
Expected: PASS

- [ ] **Step 6: Commit all backend work**

```bash
git add internal/api/runqueue.go internal/api/runqueue_test.go \
       internal/api/queue_handlers.go internal/api/server.go \
       internal/api/response.go internal/api/server_test.go \
       internal/pipeline/events.go cmd/conductor/serve.go
git commit -m "feat: implement queue service with API endpoints (Spec 04 backend)"
```

---

## Track B: Frontend

### Task B1: TypeScript types and API client

**Files:**
- Modify: `web/src/types/api.ts`
- Modify: `web/src/api/client.ts`

- [ ] **Step 1: Add queue types to api.ts**

Append to `web/src/types/api.ts`:

```typescript
// --- Spec 04: Queue System types ---

export type QueueItemStatus = 'pending' | 'executing' | 'completed' | 'failed' | 'skipped';
export type QueueStateValue = 'idle' | 'ready' | 'running' | 'paused' | 'completed';

export interface QueueItem {
  id: string;
  work_order_file: string;
  title: string;
  type: string;
  target_module: string;
  overrides: Record<string, string>;
  status: QueueItemStatus;
  workflow_id?: string;
  error?: string;
  added_at: string;
}

export interface QueueState {
  state: QueueStateValue;
  items: QueueItem[];
  current?: QueueItem;
  pause_reason?: string;
}

export interface QueueAddItem {
  work_order_file: string;
  overrides?: Record<string, string>;
}
```

- [ ] **Step 2: Add queue API client functions to client.ts**

Add imports for new types, then append:

```typescript
import type {
  // ... existing ...
  QueueState,
  QueueAddItem,
} from "../types/api";

export async function getQueue(): Promise<QueueState> {
  return fetchJSON<QueueState>("/api/queue");
}

export async function addQueueItems(items: QueueAddItem[]): Promise<QueueState> {
  return postJSON<QueueState>("/api/queue", { items });
}

export async function removeQueueItem(id: string): Promise<QueueState> {
  const response = await fetch(`/api/queue/${encodeURIComponent(id)}`, {
    method: "DELETE",
    headers: { Accept: "application/json" },
  });
  const payload = (await response.json().catch(() => ({}))) as { error?: string };
  if (!response.ok) throw new Error(payload.error || `Request failed: ${response.status}`);
  return payload as QueueState;
}

export async function reorderQueue(order: string[]): Promise<QueueState> {
  return postJSON<QueueState>("/api/queue/reorder", { order });
}

export async function startQueue(): Promise<QueueState> {
  return postJSON<QueueState>("/api/queue/start", {});
}

export async function pauseQueue(): Promise<QueueState> {
  return postJSON<QueueState>("/api/queue/pause", {});
}

export async function continueQueue(): Promise<QueueState> {
  return postJSON<QueueState>("/api/queue/continue", {});
}
```

Note: `reorderQueue` uses `postJSON` (which sends POST) but the backend expects PUT. Fix: either change the backend to accept POST, or add a `putJSON` helper. Simplest: use the existing `postJSON` pattern but with method: "PUT":

```typescript
async function fetchWithMethod<T>(path: string, method: string, body?: unknown): Promise<T> {
  const options: RequestInit = {
    method,
    headers: { "Content-Type": "application/json", Accept: "application/json" },
  };
  if (body !== undefined) options.body = JSON.stringify(body);
  const response = await fetch(path, options);
  const payload = (await response.json().catch(() => ({}))) as { error?: string };
  if (!response.ok) throw new Error(payload.error || `Request failed: ${response.status}`);
  return payload as T;
}

export async function reorderQueue(order: string[]): Promise<QueueState> {
  return fetchWithMethod<QueueState>("/api/queue/reorder", "PUT", { order });
}
```

Actually, simplest fix: use `postJSON` and change the backend endpoint from PUT to POST. Both are semantically valid for reorder. Update in queue_handlers.go route registration to `r.Post("/api/queue/reorder", ...)` instead of `r.Put`. This keeps the client simple.

- [ ] **Step 3: Verify build**

Run: `cd web && npm run build`
Expected: PASS

---

### Task B2: useQueue hook

**Files:**
- Create: `web/src/hooks/useQueue.ts`

- [ ] **Step 1: Create useQueue.ts**

```typescript
import { useCallback, useEffect, useRef, useState } from "react";
import {
  getQueue,
  addQueueItems,
  removeQueueItem,
  reorderQueue,
  startQueue,
  pauseQueue,
  continueQueue,
} from "@/api/client";
import type { QueueState, QueueAddItem } from "@/types/api";

const POLL_INTERVAL = 3000;

const emptyState: QueueState = { state: "idle", items: [], current: undefined };

export function useQueue() {
  const [state, setState] = useState<QueueState>(emptyState);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const intervalRef = useRef<ReturnType<typeof setInterval> | null>(null);

  const refresh = useCallback(() => {
    getQueue()
      .then((data) => {
        setState(data);
        setError(null);
      })
      .catch((err: unknown) => {
        setError(err instanceof Error ? err.message : "Failed to fetch queue");
      })
      .finally(() => setLoading(false));
  }, []);

  useEffect(() => {
    refresh();
    intervalRef.current = setInterval(refresh, POLL_INTERVAL);
    return () => {
      if (intervalRef.current) clearInterval(intervalRef.current);
    };
  }, [refresh]);

  const wrapAction = useCallback(
    async (action: () => Promise<QueueState>) => {
      try {
        const updated = await action();
        setState(updated);
        setError(null);
      } catch (err: unknown) {
        setError(err instanceof Error ? err.message : "Queue action failed");
        throw err;
      }
    },
    []
  );

  return {
    state,
    loading,
    error,
    refresh,
    addItems: (items: QueueAddItem[]) => wrapAction(() => addQueueItems(items)),
    removeItem: (id: string) => wrapAction(() => removeQueueItem(id)),
    reorder: (ids: string[]) => wrapAction(() => reorderQueue(ids)),
    start: () => wrapAction(() => startQueue()),
    pause: () => wrapAction(() => pauseQueue()),
    continue_: () => wrapAction(() => continueQueue()),
  };
}
```

- [ ] **Step 2: Verify build**

Run: `cd web && npm run build`
Expected: PASS

---

### Task B3: QueueStrip sidebar component

**Files:**
- Create: `web/src/components/QueueStrip.tsx`
- Modify: `web/src/components/Sidebar.tsx`

- [ ] **Step 1: Create QueueStrip.tsx**

```tsx
import { Button } from "@/components/ui/button";
import type { QueueState } from "@/types/api";

interface QueueStripProps {
  state: QueueState;
  onManageClick: () => void;
}

function getStateLabel(state: QueueState): string {
  switch (state.state) {
    case "idle":
      return "Idle";
    case "ready": {
      const pending = state.items.filter((i) => i.status === "pending").length;
      return `${pending} Queued`;
    }
    case "running": {
      const completed = state.items.filter(
        (i) => i.status === "completed" || i.status === "failed"
      ).length;
      return `Running (${completed + 1}/${state.items.length})`;
    }
    case "paused":
      return "Paused";
    case "completed":
      return "Complete";
    default:
      return state.state;
  }
}

function getPauseDetail(state: QueueState): string | null {
  if (state.state !== "paused") return null;
  switch (state.pause_reason) {
    case "awaiting_review":
      return "Paused — awaiting review";
    case "manual":
      return "Paused — manual";
    case "error":
      return "Paused — error";
    default:
      return "Paused";
  }
}

export function QueueStrip({ state, onManageClick }: QueueStripProps) {
  const label = getStateLabel(state);
  const pauseDetail = getPauseDetail(state);

  return (
    <div>
      <p className="mb-2 text-xs font-medium uppercase text-muted-foreground">Queue</p>

      <p className="text-sm font-medium">{label}</p>

      {state.state === "running" && state.current && (
        <div className="mt-1 flex items-center gap-1.5 text-xs text-muted-foreground">
          <span className="h-1.5 w-1.5 rounded-full bg-blue-500 animate-pulse" />
          <span className="truncate">{state.current.title}</span>
        </div>
      )}

      {pauseDetail && (
        <p className="mt-1 text-xs italic text-muted-foreground">{pauseDetail}</p>
      )}

      <Button variant="outline" size="sm" className="mt-3" onClick={onManageClick}>
        Manage Queue
      </Button>
    </div>
  );
}
```

- [ ] **Step 2: Update Sidebar.tsx to use QueueStrip**

Replace the queue placeholder block (lines 39-49 of Sidebar.tsx) with:

```tsx
import { useState } from "react";
import { QueueStrip } from "@/components/QueueStrip";
import { QueueDrawer } from "@/components/QueueDrawer";
import { useQueue } from "@/hooks/useQueue";

// Inside Sidebar component:
const queue = useQueue();
const [drawerOpen, setDrawerOpen] = useState(false);

// Replace the queue div:
<div className="mt-6">
  <Separator className="mb-4" />
  <QueueStrip state={queue.state} onManageClick={() => setDrawerOpen(true)} />
</div>

<QueueDrawer open={drawerOpen} onClose={() => setDrawerOpen(false)} queue={queue} />
```

Note: QueueDrawer doesn't exist yet — create a stub first or build B3 and B4 together.

- [ ] **Step 3: Verify build**

Run: `cd web && npm run build`
Expected: PASS (may need QueueDrawer stub first)

---

### Task B4: QueueDrawer component

**Files:**
- Create: `web/src/components/QueueDrawer.tsx`

- [ ] **Step 1: Create QueueDrawer.tsx**

```tsx
import { useState, useCallback } from "react";
import { useNavigate } from "react-router-dom";
import { X, GripVertical, Trash2, ChevronDown, ChevronRight, Play, Pause, SkipForward } from "lucide-react";
import { Button } from "@/components/ui/button";
import { StatusBadge } from "@/components/StatusBadge";
import { ConfirmDialog } from "@/components/ConfirmDialog";
import type { QueueItem, QueueAddItem } from "@/types/api";

interface QueueDrawerProps {
  open: boolean;
  onClose: () => void;
  queue: {
    state: { state: string; items: QueueItem[]; current?: QueueItem; pause_reason?: string };
    start: () => Promise<void>;
    pause: () => Promise<void>;
    continue_: () => Promise<void>;
    removeItem: (id: string) => Promise<void>;
    reorder: (ids: string[]) => Promise<void>;
  };
}

export function QueueDrawer({ open, onClose, queue }: QueueDrawerProps) {
  const navigate = useNavigate();
  const [clearOpen, setClearOpen] = useState(false);
  const [actionLoading, setActionLoading] = useState(false);
  const [expandedItems, setExpandedItems] = useState<Set<string>>(new Set());
  const [dragOverId, setDragOverId] = useState<string | null>(null);

  const { state } = queue;
  const pendingItems = state.items.filter((i) => i.status === "pending");
  const completedItems = state.items.filter((i) => i.status === "completed" || i.status === "failed");
  const executingItem = state.items.find((i) => i.status === "executing");

  const wrapAction = async (fn: () => Promise<void>) => {
    setActionLoading(true);
    try { await fn(); } finally { setActionLoading(false); }
  };

  const handleClearAll = async () => {
    for (const item of pendingItems) {
      await queue.removeItem(item.id);
    }
    setClearOpen(false);
  };

  const toggleExpand = (id: string) => {
    setExpandedItems((prev) => {
      const next = new Set(prev);
      if (next.has(id)) next.delete(id); else next.add(id);
      return next;
    });
  };

  // Drag-and-drop reorder for pending items.
  const handleDragStart = (e: React.DragEvent, id: string) => {
    e.dataTransfer.setData("text/plain", id);
    e.dataTransfer.effectAllowed = "move";
  };

  const handleDragOver = (e: React.DragEvent, id: string) => {
    e.preventDefault();
    e.dataTransfer.dropEffect = "move";
    setDragOverId(id);
  };

  const handleDrop = async (e: React.DragEvent, targetId: string) => {
    e.preventDefault();
    setDragOverId(null);
    const draggedId = e.dataTransfer.getData("text/plain");
    if (draggedId === targetId) return;

    const ids = pendingItems.map((i) => i.id);
    const fromIdx = ids.indexOf(draggedId);
    const toIdx = ids.indexOf(targetId);
    if (fromIdx === -1 || toIdx === -1) return;

    ids.splice(fromIdx, 1);
    ids.splice(toIdx, 0, draggedId);
    await queue.reorder(ids);
  };

  const goToWorkflow = (workflowId: string) => {
    onClose();
    navigate(`/pipeline/${workflowId}`);
  };

  if (!open) return null;

  return (
    <div className="fixed inset-0 z-50 flex justify-end bg-black/60">
      <div className="flex h-full w-full max-w-lg flex-col border-l border-border bg-background">
        {/* Header */}
        <div className="flex items-center justify-between border-b border-border px-4 py-3">
          <div className="flex items-center gap-3">
            <h2 className="text-lg font-semibold">Run Queue</h2>
            <StatusBadge status={state.state === "running" ? "running" : state.state === "paused" ? "awaiting_review" : state.state} size="sm" />
          </div>
          <div className="flex items-center gap-2">
            {state.state === "ready" && (
              <Button size="sm" disabled={actionLoading} onClick={() => wrapAction(queue.start)}>
                <Play className="mr-1 size-3" /> Start
              </Button>
            )}
            {state.state === "paused" && (
              <Button size="sm" disabled={actionLoading} onClick={() => wrapAction(queue.continue_)}>
                <SkipForward className="mr-1 size-3" /> Continue
              </Button>
            )}
            {state.state === "running" && (
              <Button size="sm" variant="outline" disabled={actionLoading} onClick={() => wrapAction(queue.pause)}>
                <Pause className="mr-1 size-3" /> Pause
              </Button>
            )}
            {pendingItems.length > 0 && (
              <Button size="sm" variant="ghost" onClick={() => setClearOpen(true)}>
                Clear
              </Button>
            )}
            <Button variant="ghost" size="icon" onClick={onClose}>
              <X className="size-4" />
            </Button>
          </div>
        </div>

        {/* Item list */}
        <div className="flex-1 overflow-auto p-4 space-y-2">
          {state.items.length === 0 && (
            <p className="text-sm text-muted-foreground text-center py-8">Queue is empty</p>
          )}

          {/* Completed items */}
          {completedItems.map((item) => (
            <div key={item.id} className="rounded-lg border border-border bg-card p-3 opacity-60">
              <div className="flex items-center justify-between">
                <div className="min-w-0">
                  <p className="truncate text-sm font-medium">{item.title || item.work_order_file}</p>
                  <p className="text-xs text-muted-foreground">{item.type}</p>
                  {item.error && <p className="text-xs text-red-400 mt-1">{item.error}</p>}
                </div>
                <div className="flex items-center gap-2">
                  <StatusBadge status={item.status} size="sm" />
                  {item.workflow_id && (
                    <button
                      type="button"
                      className="text-xs text-blue-400 hover:underline"
                      onClick={() => goToWorkflow(item.workflow_id!)}
                    >
                      View
                    </button>
                  )}
                </div>
              </div>
            </div>
          ))}

          {/* Executing item */}
          {executingItem && (
            <div className="rounded-lg border-2 border-blue-500/50 bg-card p-3">
              <div className="flex items-center justify-between">
                <div className="flex items-center gap-2 min-w-0">
                  <span className="h-2 w-2 rounded-full bg-blue-500 animate-pulse shrink-0" />
                  <div className="min-w-0">
                    <p className="truncate text-sm font-medium">{executingItem.title}</p>
                    <p className="text-xs text-muted-foreground">{executingItem.type}</p>
                  </div>
                </div>
                <div className="flex items-center gap-2">
                  <StatusBadge status="running" size="sm" />
                  {executingItem.workflow_id && (
                    <button
                      type="button"
                      className="text-xs text-blue-400 hover:underline"
                      onClick={() => goToWorkflow(executingItem.workflow_id!)}
                    >
                      View
                    </button>
                  )}
                </div>
              </div>
            </div>
          )}

          {/* Pending items */}
          {pendingItems.map((item) => (
            <div
              key={item.id}
              draggable
              onDragStart={(e) => handleDragStart(e, item.id)}
              onDragOver={(e) => handleDragOver(e, item.id)}
              onDrop={(e) => handleDrop(e, item.id)}
              onDragLeave={() => setDragOverId(null)}
              className={`rounded-lg border border-border bg-card p-3 cursor-grab active:cursor-grabbing ${
                dragOverId === item.id ? "border-blue-500" : ""
              }`}
            >
              <div className="flex items-center gap-2">
                <GripVertical className="size-4 shrink-0 text-muted-foreground" />
                <div className="flex-1 min-w-0">
                  <div className="flex items-center gap-2">
                    <p className="truncate text-sm font-medium">{item.title || item.work_order_file}</p>
                    {Object.keys(item.overrides).length > 0 && (
                      <span className="rounded bg-amber-500/20 px-1.5 py-0.5 text-xs text-amber-400">
                        Custom overrides
                      </span>
                    )}
                  </div>
                  <p className="text-xs text-muted-foreground">
                    {item.type} {item.target_module ? `· ${item.target_module}` : ""}
                  </p>
                </div>
                <div className="flex items-center gap-1">
                  <button
                    type="button"
                    className="p-1 text-muted-foreground hover:text-foreground"
                    onClick={() => toggleExpand(item.id)}
                  >
                    {expandedItems.has(item.id) ? <ChevronDown className="size-3" /> : <ChevronRight className="size-3" />}
                  </button>
                  <button
                    type="button"
                    className="p-1 text-muted-foreground hover:text-red-400"
                    onClick={() => queue.removeItem(item.id)}
                  >
                    <Trash2 className="size-3" />
                  </button>
                </div>
              </div>

              {/* Override expansion */}
              {expandedItems.has(item.id) && (
                <div className="mt-2 border-t border-border pt-2 pl-6 text-xs">
                  {Object.keys(item.overrides).length > 0 ? (
                    Object.entries(item.overrides).map(([role, model]) => (
                      <div key={role} className="flex gap-2 text-muted-foreground">
                        <span className="font-medium">{role}:</span>
                        <span>{model}</span>
                      </div>
                    ))
                  ) : (
                    <p className="text-muted-foreground">Using default models</p>
                  )}
                </div>
              )}
            </div>
          ))}
        </div>
      </div>

      {/* Clear confirmation */}
      <ConfirmDialog
        open={clearOpen}
        title="Clear Queue"
        description={`Remove all ${pendingItems.length} pending item${pendingItems.length !== 1 ? "s" : ""} from the queue?`}
        confirmLabel="Clear All"
        onConfirm={handleClearAll}
        onCancel={() => setClearOpen(false)}
      />
    </div>
  );
}
```

- [ ] **Step 2: Verify build**

Run: `cd web && npm run build`
Expected: PASS

- [ ] **Step 3: Commit all frontend work**

```bash
cd web
git add -A
git commit -m "feat: implement queue UI — useQueue hook, QueueStrip, QueueDrawer (Spec 04 frontend)"
```

---

## Final Verification

### Task C1: Full build and test

- [ ] **Step 1: Run Go build**

Run: `make build`
Expected: PASS

- [ ] **Step 2: Run Go tests**

Run: `make test`
Expected: PASS

- [ ] **Step 3: Run frontend build**

Run: `cd web && npm run build`
Expected: PASS

---

## Parallel Execution Strategy

Track A (Backend: Tasks A1-A6) and Track B (Frontend: Tasks B1-B4) are **fully independent** — they never touch the same files. They can be executed in parallel by two agents in separate worktrees, then merged cleanly.

Within Track A, tasks are sequential (A1→A2→A3→A4→A5→A6).
Within Track B, tasks are sequential (B1→B2→B3→B4).

**Note on reorder endpoint:** The plan uses `PUT /api/queue/reorder` in the backend but the frontend client uses `postJSON`. Either change the backend route to `r.Post` or add a `putJSON` helper in the client. Simplest: change the backend to POST since both are semantically valid. The implementing agent should pick one approach and be consistent.
