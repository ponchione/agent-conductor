package gate

import (
	"context"
	"database/sql"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/ponchione/agent-conductor/internal/database"
	"github.com/ponchione/agent-conductor/internal/pipeline"
)

func TestRejectEmitsRunCompleteEvent(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "conductor.db")
	db, err := database.NewDB(dbPath)
	if err != nil {
		t.Fatalf("NewDB() error: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	ctx := context.Background()
	sessionID, err := db.StartSession(ctx, database.SessionKindRunOnly, "repo", "/tmp/spec.md")
	if err != nil {
		t.Fatalf("StartSession() error: %v", err)
	}

	if err := db.CreateWorkflow(ctx, database.CreateWorkflowParams{
		ID:                     "wf-gate-reject",
		OriginalIntent:         "test",
		OriginalFile:           "/tmp/work-order.yaml",
		CurrentState:           "human_review",
		TargetRepo:             "repo",
		GitBranch:              "feature/test",
		ContextPackagePath:     sql.NullString{},
		VerificationReportPath: sql.NullString{},
		MaxDepth:               3,
		MaxFilesChanged:        10,
		MaxDurationMins:        30,
	}); err != nil {
		t.Fatalf("CreateWorkflow() error: %v", err)
	}
	if err := db.CreatePipelineRun(ctx, database.CreatePipelineRunParams{
		ID:            "pr-gate-reject",
		WorkflowID:    "wf-gate-reject",
		Project:       "repo",
		WorkOrderType: sql.NullString{String: "new_feature", Valid: true},
	}); err != nil {
		t.Fatalf("CreatePipelineRun() error: %v", err)
	}
	if err := db.LinkPipelineRunToSession(ctx, "pr-gate-reject", sessionID); err != nil {
		t.Fatalf("LinkPipelineRunToSession() error: %v", err)
	}

	if err := Reject(ctx, db, "wf-gate-reject", "not acceptable"); err != nil {
		t.Fatalf("Reject() error: %v", err)
	}

	events, err := db.ListEvents(ctx, sql.NullString{String: "wf-gate-reject", Valid: true})
	if err != nil {
		t.Fatalf("ListEvents() error: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("len(events) = %d, want 1", len(events))
	}
	if events[0].EventType != pipeline.EventRunComplete {
		t.Fatalf("events[0].EventType = %q, want %q", events[0].EventType, pipeline.EventRunComplete)
	}

	var payload map[string]any
	if err := json.Unmarshal([]byte(events[0].EventData.String), &payload); err != nil {
		t.Fatalf("Unmarshal() error: %v", err)
	}
	if payload["human_result"] != "rejected" {
		t.Fatalf("payload[human_result] = %#v, want rejected", payload["human_result"])
	}
	if payload["final_state"] != "failed" {
		t.Fatalf("payload[final_state] = %#v, want failed", payload["final_state"])
	}
}
