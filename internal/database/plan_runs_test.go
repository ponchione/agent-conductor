package database

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
)

func TestPlanRunUsefulnessQueries(t *testing.T) {
	t.Parallel()

	dbPath := filepath.Join(t.TempDir(), "conductor.db")
	db, err := NewDB(dbPath)
	if err != nil {
		t.Fatalf("NewDB() error: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	ctx := context.Background()

	if err := db.InsertPlanRun(ctx, InsertPlanRunParams{
		ID:                      "plan-changed",
		SpecFile:                "/tmp/spec-a.md",
		Project:                 String("repo"),
		WorkOrdersGenerated:     Int64(3),
		PreAuditWorkOrderCount:  Int64(2),
		PostAuditWorkOrderCount: Int64(3),
		AuditChangeText:         String(`["added missing test work order"]`),
		AuditWorkOrdersAdded:    Int64(1),
	}); err != nil {
		t.Fatalf("InsertPlanRun(changed) error: %v", err)
	}

	if err := db.InsertPlanRun(ctx, InsertPlanRunParams{
		ID:                      "plan-unchanged",
		SpecFile:                "/tmp/spec-b.md",
		Project:                 String("repo"),
		WorkOrdersGenerated:     Int64(2),
		PreAuditWorkOrderCount:  Int64(2),
		PostAuditWorkOrderCount: Int64(2),
	}); err != nil {
		t.Fatalf("InsertPlanRun(unchanged) error: %v", err)
	}

	rows, err := db.ListPlanRunUsefulness(ctx, 10)
	if err != nil {
		t.Fatalf("ListPlanRunUsefulness() error: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("len(ListPlanRunUsefulness()) = %d, want 2", len(rows))
	}

	foundChanged := false
	foundUnchanged := false
	for _, row := range rows {
		switch row.ID {
		case "plan-changed":
			foundChanged = true
			if !row.AuditChanged {
				t.Fatal("changed row should have AuditChanged=true")
			}
			if row.WorkOrderDelta.Int64 != 1 {
				t.Fatalf("changed row delta = %d, want 1", row.WorkOrderDelta.Int64)
			}
		case "plan-unchanged":
			foundUnchanged = true
			if row.AuditChanged {
				t.Fatal("unchanged row should have AuditChanged=false")
			}
			if row.WorkOrderDelta.Int64 != 0 {
				t.Fatalf("unchanged row delta = %d, want 0", row.WorkOrderDelta.Int64)
			}
		}
	}
	if !foundChanged || !foundUnchanged {
		t.Fatalf("expected both changed and unchanged rows, got %#v", rows)
	}

	stats, err := db.GetPlanAuditChangeStats(ctx)
	if err != nil {
		t.Fatalf("GetPlanAuditChangeStats() error: %v", err)
	}
	if stats.ChangedRuns != 1 || stats.UnchangedRuns != 1 || stats.TotalRuns != 2 {
		t.Fatalf("stats = %#v, want changed=1 unchanged=1 total=2", stats)
	}
}

func TestInsertPlanRunPersistsHierarchicalMetrics(t *testing.T) {
	t.Parallel()

	dbPath := filepath.Join(t.TempDir(), "conductor.db")
	db, err := NewDB(dbPath)
	if err != nil {
		t.Fatalf("NewDB() error: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	ctx := context.Background()

	if err := db.InsertPlanRun(ctx, InsertPlanRunParams{
		ID:                       "plan-metrics",
		SpecFile:                 "/tmp/spec.md",
		Project:                  String("repo"),
		WorkOrdersGenerated:      Int64(5),
		EpicCount:                Int64(2),
		TaskCount:                Int64(5),
		EpicGenerationModel:      String("claude-sonnet"),
		TaskGenerationModel:      String("claude-sonnet"),
		EpicGenerationCostUsd:    Float64(0.12),
		TaskGenerationCostUsd:    Float64(0.34),
		EpicGenerationDurationMs: Int64Value(1200),
		TaskGenerationDurationMs: Int64Value(3400),
		EpicGenerationTokensIn:   Int64(100),
		EpicGenerationTokensOut:  Int64(40),
		TaskGenerationCallCount:  Int64(2),
		TaskGenerationTokensIn:   Int64(220),
		TaskGenerationTokensOut:  Int64(110),
		PreAuditWorkOrderCount:   Int64(4),
		PostAuditWorkOrderCount:  Int64(5),
		AuditWorkOrdersAdded:     Int64(1),
		AuditWorkOrdersModified:  Int64(1),
		AuditWorkOrdersUnchanged: Int64(3),
		GenerationRetryCount:     0,
	}); err != nil {
		t.Fatalf("InsertPlanRun() error: %v", err)
	}

	var (
		epicCount                int64
		taskCount                int64
		epicGenerationModel      string
		taskGenerationModel      string
		epicGenerationCostUSD    float64
		taskGenerationCostUSD    float64
		epicGenerationDurationMs int64
		taskGenerationDurationMs int64
		epicGenerationTokensIn   int64
		epicGenerationTokensOut  int64
		taskGenerationCallCount  int64
		taskGenerationTokensIn   int64
		taskGenerationTokensOut  int64
	)
	if err := db.conn.QueryRowContext(ctx, `
		SELECT
			epic_count,
			task_count,
			epic_generation_model,
			task_generation_model,
			epic_generation_cost_usd,
			task_generation_cost_usd,
			epic_generation_duration_ms,
			task_generation_duration_ms,
			epic_generation_tokens_in,
			epic_generation_tokens_out,
			task_generation_call_count,
			task_generation_tokens_in,
			task_generation_tokens_out
		FROM plan_runs
		WHERE id = ?
	`, "plan-metrics").Scan(
		&epicCount,
		&taskCount,
		&epicGenerationModel,
		&taskGenerationModel,
		&epicGenerationCostUSD,
		&taskGenerationCostUSD,
		&epicGenerationDurationMs,
		&taskGenerationDurationMs,
		&epicGenerationTokensIn,
		&epicGenerationTokensOut,
		&taskGenerationCallCount,
		&taskGenerationTokensIn,
		&taskGenerationTokensOut,
	); err != nil {
		t.Fatalf("QueryRowContext() error: %v", err)
	}

	if epicCount != 2 || taskCount != 5 {
		t.Fatalf("counts = (%d,%d), want (2,5)", epicCount, taskCount)
	}
	if epicGenerationModel != "claude-sonnet" || taskGenerationModel != "claude-sonnet" {
		t.Fatalf("models = (%q,%q), want (claude-sonnet, claude-sonnet)", epicGenerationModel, taskGenerationModel)
	}
	if epicGenerationCostUSD != 0.12 || taskGenerationCostUSD != 0.34 {
		t.Fatalf("costs = (%f,%f), want (0.12,0.34)", epicGenerationCostUSD, taskGenerationCostUSD)
	}
	if epicGenerationDurationMs != 1200 || taskGenerationDurationMs != 3400 {
		t.Fatalf("durations = (%d,%d), want (1200,3400)", epicGenerationDurationMs, taskGenerationDurationMs)
	}
	if epicGenerationTokensIn != 100 || epicGenerationTokensOut != 40 {
		t.Fatalf("epic tokens = (%d,%d), want (100,40)", epicGenerationTokensIn, epicGenerationTokensOut)
	}
	if taskGenerationCallCount != 2 || taskGenerationTokensIn != 220 || taskGenerationTokensOut != 110 {
		t.Fatalf("task metrics = (%d,%d,%d), want (2,220,110)", taskGenerationCallCount, taskGenerationTokensIn, taskGenerationTokensOut)
	}
}

func TestCreateStubPlanRunSetsStatePending(t *testing.T) {
	t.Parallel()

	dbPath := filepath.Join(t.TempDir(), "conductor.db")
	db, err := NewDB(dbPath)
	if err != nil {
		t.Fatalf("NewDB() error: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	ctx := context.Background()

	id, err := db.CreateStubPlanRun(ctx, "", "project", "spec.md")
	if err != nil {
		t.Fatalf("CreateStubPlanRun() error: %v", err)
	}

	var state string
	if err := db.conn.QueryRowContext(ctx, `SELECT state FROM plan_runs WHERE id = ?`, id).Scan(&state); err != nil {
		t.Fatalf("query state: %v", err)
	}
	if state != PlanRunStatePending {
		t.Fatalf("state = %q, want %q", state, PlanRunStatePending)
	}
}

func TestUpdatePlanRunState(t *testing.T) {
	t.Parallel()

	dbPath := filepath.Join(t.TempDir(), "conductor.db")
	db, err := NewDB(dbPath)
	if err != nil {
		t.Fatalf("NewDB() error: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	ctx := context.Background()

	id, err := db.CreateStubPlanRun(ctx, "", "project", "spec.md")
	if err != nil {
		t.Fatalf("CreateStubPlanRun() error: %v", err)
	}

	// Transition to generating_epics
	if err := db.UpdatePlanRunState(ctx, id, PlanRunStateGeneratingEpics, "Starting epic decomposition"); err != nil {
		t.Fatalf("UpdatePlanRunState(generating_epics) error: %v", err)
	}

	run, err := db.GetPlanRun(ctx, id)
	if err != nil {
		t.Fatalf("GetPlanRun() error: %v", err)
	}
	if run.State != PlanRunStateGeneratingEpics {
		t.Fatalf("state = %q, want %q", run.State, PlanRunStateGeneratingEpics)
	}
	if !run.CurrentPhase.Valid || run.CurrentPhase.String != "Starting epic decomposition" {
		t.Fatalf("current_phase = %v, want %q", run.CurrentPhase, "Starting epic decomposition")
	}
	if run.ErrorMessage.Valid {
		t.Fatalf("error_message should be NULL, got %q", run.ErrorMessage.String)
	}

	// Transition to generating_tasks
	if err := db.UpdatePlanRunState(ctx, id, PlanRunStateGeneratingTasks, "Generating tasks for epic 2 of 3"); err != nil {
		t.Fatalf("UpdatePlanRunState(generating_tasks) error: %v", err)
	}

	run, err = db.GetPlanRun(ctx, id)
	if err != nil {
		t.Fatalf("GetPlanRun() error: %v", err)
	}
	if run.State != PlanRunStateGeneratingTasks {
		t.Fatalf("state = %q, want %q", run.State, PlanRunStateGeneratingTasks)
	}
	if !run.CurrentPhase.Valid || run.CurrentPhase.String != "Generating tasks for epic 2 of 3" {
		t.Fatalf("current_phase = %v, want %q", run.CurrentPhase, "Generating tasks for epic 2 of 3")
	}
}

func TestUpdatePlanRunFailed(t *testing.T) {
	t.Parallel()

	dbPath := filepath.Join(t.TempDir(), "conductor.db")
	db, err := NewDB(dbPath)
	if err != nil {
		t.Fatalf("NewDB() error: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	ctx := context.Background()

	id, err := db.CreateStubPlanRun(ctx, "", "project", "spec.md")
	if err != nil {
		t.Fatalf("CreateStubPlanRun() error: %v", err)
	}

	if err := db.UpdatePlanRunFailed(ctx, id, "claude CLI not found"); err != nil {
		t.Fatalf("UpdatePlanRunFailed() error: %v", err)
	}

	run, err := db.GetPlanRun(ctx, id)
	if err != nil {
		t.Fatalf("GetPlanRun() error: %v", err)
	}
	if run.State != PlanRunStateFailed {
		t.Fatalf("state = %q, want %q", run.State, PlanRunStateFailed)
	}
	if !run.ErrorMessage.Valid || run.ErrorMessage.String != "claude CLI not found" {
		t.Fatalf("error_message = %v, want %q", run.ErrorMessage, "claude CLI not found")
	}
	if run.CurrentPhase.Valid {
		t.Fatalf("current_phase should be NULL after failure, got %q", run.CurrentPhase.String)
	}
}

func TestGetPlanRun(t *testing.T) {
	t.Parallel()

	dbPath := filepath.Join(t.TempDir(), "conductor.db")
	db, err := NewDB(dbPath)
	if err != nil {
		t.Fatalf("NewDB() error: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	ctx := context.Background()

	if err := db.InsertPlanRun(ctx, InsertPlanRunParams{
		ID:                  "plan-detail",
		SpecFile:            "/tmp/spec.md",
		Project:             String("repo"),
		SpecFingerprint:     String("abc123"),
		WorkOrdersGenerated: Int64(5),
		EpicCount:           Int64(2),
		TaskCount:           Int64(5),
		GenerationCostUsd:   Float64(0.42),
	}); err != nil {
		t.Fatalf("InsertPlanRun() error: %v", err)
	}

	run, err := db.GetPlanRun(ctx, "plan-detail")
	if err != nil {
		t.Fatalf("GetPlanRun() error: %v", err)
	}

	if run.ID != "plan-detail" {
		t.Fatalf("ID = %q, want %q", run.ID, "plan-detail")
	}
	if run.SpecFile != "/tmp/spec.md" {
		t.Fatalf("SpecFile = %q, want %q", run.SpecFile, "/tmp/spec.md")
	}
	if !run.Project.Valid || run.Project.String != "repo" {
		t.Fatalf("Project = %v, want %q", run.Project, "repo")
	}
	if !run.SpecFingerprint.Valid || run.SpecFingerprint.String != "abc123" {
		t.Fatalf("SpecFingerprint = %v, want %q", run.SpecFingerprint, "abc123")
	}
	// InsertPlanRun does not set state explicitly, so the DEFAULT 'pending' applies.
	// The backfill runs at DB creation time (before any rows exist), so rows
	// inserted after DB creation retain state='pending'.
	if run.State != PlanRunStatePending {
		t.Fatalf("State = %q, want %q", run.State, PlanRunStatePending)
	}
	if !run.WorkOrdersGenerated.Valid || run.WorkOrdersGenerated.Int64 != 5 {
		t.Fatalf("WorkOrdersGenerated = %v, want 5", run.WorkOrdersGenerated)
	}
	if !run.EpicCount.Valid || run.EpicCount.Int64 != 2 {
		t.Fatalf("EpicCount = %v, want 2", run.EpicCount)
	}
	if !run.GenerationCostUsd.Valid || run.GenerationCostUsd.Float64 != 0.42 {
		t.Fatalf("GenerationCostUsd = %v, want 0.42", run.GenerationCostUsd)
	}
	if run.CreatedAt == "" {
		t.Fatal("CreatedAt should not be empty")
	}
}

func TestGetPlanRunNotFound(t *testing.T) {
	t.Parallel()

	dbPath := filepath.Join(t.TempDir(), "conductor.db")
	db, err := NewDB(dbPath)
	if err != nil {
		t.Fatalf("NewDB() error: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	ctx := context.Background()

	_, err = db.GetPlanRun(ctx, "nonexistent-id")
	if err == nil {
		t.Fatal("GetPlanRun() should return error for nonexistent ID")
	}
	if !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("GetPlanRun() error = %v, want sql.ErrNoRows", err)
	}
}

func TestListPlanRunUsefulnessIncludesState(t *testing.T) {
	t.Parallel()

	dbPath := filepath.Join(t.TempDir(), "conductor.db")
	db, err := NewDB(dbPath)
	if err != nil {
		t.Fatalf("NewDB() error: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	ctx := context.Background()

	// Create a stub plan run (state = pending, no metrics)
	stubID, err := db.CreateStubPlanRun(ctx, "", "project", "spec-stub.md")
	if err != nil {
		t.Fatalf("CreateStubPlanRun() error: %v", err)
	}

	// Create a plan run and explicitly update it to complete state.
	completeID, err := db.CreateStubPlanRun(ctx, "", "project", "spec-complete.md")
	if err != nil {
		t.Fatalf("CreateStubPlanRun(complete) error: %v", err)
	}
	if err := db.UpdatePlanRunState(ctx, completeID, PlanRunStateComplete, ""); err != nil {
		t.Fatalf("UpdatePlanRunState(complete) error: %v", err)
	}

	rows, err := db.ListPlanRunUsefulness(ctx, 10)
	if err != nil {
		t.Fatalf("ListPlanRunUsefulness() error: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("len(rows) = %d, want 2", len(rows))
	}

	foundPending := false
	foundComplete := false
	for _, row := range rows {
		switch row.ID {
		case stubID:
			foundPending = true
			if row.State != PlanRunStatePending {
				t.Fatalf("stub state = %q, want %q", row.State, PlanRunStatePending)
			}
		case completeID:
			foundComplete = true
			if row.State != PlanRunStateComplete {
				t.Fatalf("complete state = %q, want %q", row.State, PlanRunStateComplete)
			}
		}
	}
	if !foundPending || !foundComplete {
		t.Fatal("expected both pending and complete rows in ListPlanRunUsefulness")
	}
}
