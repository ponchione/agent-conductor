package database

import (
	"context"
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
