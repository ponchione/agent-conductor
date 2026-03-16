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
