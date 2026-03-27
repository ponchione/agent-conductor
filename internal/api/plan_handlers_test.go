package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/ponchione/agent-conductor/internal/database"
	"github.com/ponchione/agent-conductor/internal/planner"
)

func TestHandleGetPlanRun_OK(t *testing.T) {
	t.Parallel()

	db := newTestDB(t)
	ctx := context.Background()

	// Insert a plan run with metrics so it gets backfilled to "complete".
	if err := db.InsertPlanRun(ctx, database.InsertPlanRunParams{
		ID:                  "plan-get-ok",
		SpecFile:            "spec.md",
		Project:             sql.NullString{String: "repo", Valid: true},
		SpecFingerprint:     sql.NullString{String: "fp-123", Valid: true},
		WorkOrdersGenerated: sql.NullInt64{Int64: 5, Valid: true},
		EpicCount:           sql.NullInt64{Int64: 2, Valid: true},
		TaskCount:           sql.NullInt64{Int64: 5, Valid: true},
		GenerationCostUsd:   sql.NullFloat64{Float64: 0.42, Valid: true},
	}); err != nil {
		t.Fatalf("InsertPlanRun() error: %v", err)
	}

	// Update state to generating_tasks with a phase message.
	if err := db.UpdatePlanRunState(ctx, "plan-get-ok", database.PlanRunStateGeneratingTasks, "Generating tasks for epic 2 of 2"); err != nil {
		t.Fatalf("UpdatePlanRunState() error: %v", err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/plan-runs/plan-get-ok", nil)

	NewServer(db, nil, "main", nil, "", nil).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var payload planRunDetailResponse
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if payload.ID != "plan-get-ok" {
		t.Fatalf("ID = %q, want %q", payload.ID, "plan-get-ok")
	}
	if payload.State != database.PlanRunStateGeneratingTasks {
		t.Fatalf("State = %q, want %q", payload.State, database.PlanRunStateGeneratingTasks)
	}
	if payload.CurrentPhase == nil || *payload.CurrentPhase != "Generating tasks for epic 2 of 2" {
		t.Fatalf("CurrentPhase = %v, want %q", payload.CurrentPhase, "Generating tasks for epic 2 of 2")
	}
	if payload.ErrorMessage != nil {
		t.Fatalf("ErrorMessage = %v, want nil", payload.ErrorMessage)
	}
	if payload.SpecFile != "spec.md" {
		t.Fatalf("SpecFile = %q, want %q", payload.SpecFile, "spec.md")
	}
	if payload.WorkOrdersGenerated == nil || *payload.WorkOrdersGenerated != 5 {
		t.Fatalf("WorkOrdersGenerated = %v, want 5", payload.WorkOrdersGenerated)
	}
	if payload.EpicCount == nil || *payload.EpicCount != 2 {
		t.Fatalf("EpicCount = %v, want 2", payload.EpicCount)
	}
	if payload.GenerationCostUSD == nil || *payload.GenerationCostUSD != 0.42 {
		t.Fatalf("GenerationCostUSD = %v, want 0.42", payload.GenerationCostUSD)
	}
	if payload.CreatedAt == "" {
		t.Fatal("CreatedAt should not be empty")
	}
	// Nullable fields that were not set should be nil (JSON null).
	if payload.AuditCostUSD != nil {
		t.Fatalf("AuditCostUSD = %v, want nil", payload.AuditCostUSD)
	}
}

func TestHandleGetPlanRun_NotFound(t *testing.T) {
	t.Parallel()

	db := newTestDB(t)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/plan-runs/nonexistent-id", nil)

	NewServer(db, nil, "main", nil, "", nil).ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusNotFound, rec.Body.String())
	}

	var payload map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload["error"] != "plan run not found" {
		t.Fatalf("error = %q, want %q", payload["error"], "plan run not found")
	}
}

// --- Background execution tests (E3-T7) ---

// newTestServer creates a Server with a test database and a mock generate function.
// It returns the server and a helper to read plan run state from the DB.
func newTestServer(t *testing.T, genFunc GenerateFunc) *Server {
	t.Helper()
	db := newTestDB(t)
	return &Server{
		db:            db,
		workOrderDir:  t.TempDir(),
		overrideStore: NewOverrideStore(),
		generateFunc:  genFunc,
	}
}

// createTestPlanRun creates a stub plan_run with a known ID for testing.
func createTestPlanRun(t *testing.T, db *database.DB, planRunID string) {
	t.Helper()
	ctx := context.Background()

	// InsertPlanRun allows setting a specific ID (unlike CreateStubPlanRun which generates one).
	if err := db.InsertPlanRun(ctx, database.InsertPlanRunParams{
		ID:       planRunID,
		SpecFile: "spec.md",
		Project:  sql.NullString{String: "test", Valid: true},
	}); err != nil {
		t.Fatalf("InsertPlanRun(%q) error: %v", planRunID, err)
	}
}

// getPlanRunState reads the plan run state and error message from the DB.
func getPlanRunState(t *testing.T, db *database.DB, planRunID string) (state string, errorMsg string) {
	t.Helper()
	row, err := db.GetPlanRun(context.Background(), planRunID)
	if err != nil {
		t.Fatalf("GetPlanRun(%q) error: %v", planRunID, err)
	}
	if row.ErrorMessage.Valid {
		return row.State, row.ErrorMessage.String
	}
	return row.State, ""
}

func TestRunPlanGeneration_SuccessPath(t *testing.T) {
	t.Parallel()

	done := make(chan struct{})
	genFunc := func(ctx context.Context, input planner.GenerateInput) (*planner.GenerateResult, error) {
		defer close(done)
		return &planner.GenerateResult{
			Metrics: planner.PlanRunMetrics{
				EpicCount:               3,
				TaskCount:               10,
				WorkOrdersGenerated:     10,
				PreAuditWorkOrderCount:  8,
				PostAuditWorkOrderCount: 10,
			},
		}, nil
	}

	s := newTestServer(t, genFunc)
	createTestPlanRun(t, s.db, "success-run")

	input := planner.GenerateInput{Timeout: 30 * time.Second}
	go s.runPlanGeneration("success-run", "session-1", "", []byte("spec data"), input)

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for goroutine")
	}

	// Give a moment for the DB writes after Generate returns.
	time.Sleep(50 * time.Millisecond)

	state, errMsg := getPlanRunState(t, s.db, "success-run")
	if state != database.PlanRunStateComplete {
		t.Fatalf("state = %q, want %q", state, database.PlanRunStateComplete)
	}
	if errMsg != "" {
		t.Fatalf("error_message = %q, want empty", errMsg)
	}

	// Verify metrics were persisted.
	row, err := s.db.GetPlanRun(context.Background(), "success-run")
	if err != nil {
		t.Fatalf("GetPlanRun() error: %v", err)
	}
	if !row.EpicCount.Valid || row.EpicCount.Int64 != 3 {
		t.Fatalf("epic_count = %v, want 3", row.EpicCount)
	}
	if !row.TaskCount.Valid || row.TaskCount.Int64 != 10 {
		t.Fatalf("task_count = %v, want 10", row.TaskCount)
	}
}

func TestRunPlanGeneration_ErrorPath(t *testing.T) {
	t.Parallel()

	done := make(chan struct{})
	genFunc := func(ctx context.Context, input planner.GenerateInput) (*planner.GenerateResult, error) {
		defer close(done)
		return nil, fmt.Errorf("claude invocation failed: connection refused")
	}

	s := newTestServer(t, genFunc)
	createTestPlanRun(t, s.db, "error-run")

	input := planner.GenerateInput{Timeout: 30 * time.Second}
	go s.runPlanGeneration("error-run", "session-1", "", []byte("spec data"), input)

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for goroutine")
	}

	time.Sleep(50 * time.Millisecond)

	state, errMsg := getPlanRunState(t, s.db, "error-run")
	if state != database.PlanRunStateFailed {
		t.Fatalf("state = %q, want %q", state, database.PlanRunStateFailed)
	}
	if errMsg == "" {
		t.Fatal("error_message should not be empty")
	}
	if errMsg != "claude invocation failed: connection refused" {
		t.Fatalf("error_message = %q, want %q", errMsg, "claude invocation failed: connection refused")
	}
}

func TestRunPlanGeneration_TimeoutPath(t *testing.T) {
	t.Parallel()

	done := make(chan struct{})
	genFunc := func(ctx context.Context, input planner.GenerateInput) (*planner.GenerateResult, error) {
		defer close(done)
		// Block until context is canceled (timeout).
		<-ctx.Done()
		return nil, ctx.Err()
	}

	s := newTestServer(t, genFunc)
	createTestPlanRun(t, s.db, "timeout-run")

	// Use a very short timeout to trigger deadline.
	input := planner.GenerateInput{Timeout: 100 * time.Millisecond}
	go s.runPlanGeneration("timeout-run", "session-1", "", []byte("spec data"), input)

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for goroutine")
	}

	time.Sleep(50 * time.Millisecond)

	state, errMsg := getPlanRunState(t, s.db, "timeout-run")
	if state != database.PlanRunStateFailed {
		t.Fatalf("state = %q, want %q", state, database.PlanRunStateFailed)
	}
	// The error message should indicate timeout, not raw Go error.
	if errMsg == "" {
		t.Fatal("error_message should not be empty")
	}
	expected := "plan generation timed out after 0 minutes"
	if errMsg != expected {
		t.Fatalf("error_message = %q, want %q", errMsg, expected)
	}
}

func TestRunPlanGeneration_PanicRecovery(t *testing.T) {
	t.Parallel()

	done := make(chan struct{})
	genFunc := func(ctx context.Context, input planner.GenerateInput) (*planner.GenerateResult, error) {
		defer close(done)
		panic("nil pointer dereference in test")
	}

	s := newTestServer(t, genFunc)
	createTestPlanRun(t, s.db, "panic-run")

	input := planner.GenerateInput{Timeout: 30 * time.Second}

	// Run in goroutine and verify the server doesn't crash.
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		s.runPlanGeneration("panic-run", "session-1", "", []byte("spec data"), input)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for goroutine")
	}

	wg.Wait() // Ensure the goroutine has fully completed (including defer).

	state, errMsg := getPlanRunState(t, s.db, "panic-run")
	if state != database.PlanRunStateFailed {
		t.Fatalf("state = %q, want %q", state, database.PlanRunStateFailed)
	}
	if errMsg == "" {
		t.Fatal("error_message should not be empty")
	}
	// The error message should contain "internal error" prefix (sanitized).
	expected := "internal error: nil pointer dereference in test"
	if errMsg != expected {
		t.Fatalf("error_message = %q, want %q", errMsg, expected)
	}
}

func TestRunPlanGeneration_ProgressCallback(t *testing.T) {
	t.Parallel()

	done := make(chan struct{})
	genFunc := func(ctx context.Context, input planner.GenerateInput) (*planner.GenerateResult, error) {
		defer close(done)
		// Simulate progress callbacks.
		if input.OnProgress != nil {
			input.OnProgress(database.PlanRunStateGeneratingEpics, "Starting epic decomposition")
			input.OnProgress(database.PlanRunStateGeneratingTasks, "Generating tasks for epic 1 of 2")
			input.OnProgress(database.PlanRunStateGeneratingTasks, "Generating tasks for epic 2 of 2")
			input.OnProgress(database.PlanRunStateAuditing, "Running audit pass")
		}
		return &planner.GenerateResult{
			Metrics: planner.PlanRunMetrics{
				EpicCount: 2,
				TaskCount: 6,
			},
		}, nil
	}

	s := newTestServer(t, genFunc)
	createTestPlanRun(t, s.db, "progress-run")

	input := planner.GenerateInput{Timeout: 30 * time.Second}
	go s.runPlanGeneration("progress-run", "session-1", "", []byte("spec data"), input)

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for goroutine")
	}

	time.Sleep(50 * time.Millisecond)

	// After completion, the final state should be "complete".
	state, _ := getPlanRunState(t, s.db, "progress-run")
	if state != database.PlanRunStateComplete {
		t.Fatalf("state = %q, want %q", state, database.PlanRunStateComplete)
	}
}

func TestRunPlanGeneration_DoesNotUseRequestContext(t *testing.T) {
	t.Parallel()

	// Create a context that is already canceled to simulate client disconnect.
	canceledCtx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately.

	done := make(chan struct{})
	genFunc := func(ctx context.Context, input planner.GenerateInput) (*planner.GenerateResult, error) {
		defer close(done)
		// Verify that the context passed to Generate is NOT canceled.
		// runPlanGeneration creates its own context.Background() with timeout.
		if ctx.Err() != nil {
			return nil, fmt.Errorf("context was already canceled: %w", ctx.Err())
		}
		return &planner.GenerateResult{
			Metrics: planner.PlanRunMetrics{EpicCount: 1, TaskCount: 3},
		}, nil
	}

	s := newTestServer(t, genFunc)
	createTestPlanRun(t, s.db, "no-req-ctx-run")

	input := planner.GenerateInput{Timeout: 30 * time.Second}

	// Even though canceledCtx is done, runPlanGeneration creates its own.
	_ = canceledCtx // Just to show the canceled context is not passed.
	go s.runPlanGeneration("no-req-ctx-run", "session-1", "", []byte("spec data"), input)

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for goroutine")
	}

	time.Sleep(50 * time.Millisecond)

	state, errMsg := getPlanRunState(t, s.db, "no-req-ctx-run")
	if state != database.PlanRunStateComplete {
		t.Fatalf("state = %q, want %q; error_message = %q", state, database.PlanRunStateComplete, errMsg)
	}
}
