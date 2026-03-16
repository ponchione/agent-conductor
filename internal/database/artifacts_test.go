package database

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"
)

func TestRegisterArtifactAndQueries(t *testing.T) {
	t.Parallel()

	dbPath := filepath.Join(t.TempDir(), "conductor.db")
	db, err := NewDB(dbPath)
	if err != nil {
		t.Fatalf("NewDB() error: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	ctx := context.Background()
	sessionID, err := db.StartSession(ctx, SessionKindPlanOnly, "repo", "/tmp/spec.md")
	if err != nil {
		t.Fatalf("StartSession() error: %v", err)
	}

	workflowID := "wf-artifacts"
	if err := db.CreateWorkflow(ctx, CreateWorkflowParams{
		ID:                     workflowID,
		OriginalIntent:         "artifact test",
		OriginalFile:           "/tmp/work-order.yaml",
		CurrentState:           "pending",
		TargetRepo:             "repo",
		GitBranch:              "feature/artifacts",
		ContextPackagePath:     sql.NullString{},
		VerificationReportPath: sql.NullString{},
		MaxDepth:               5,
		MaxFilesChanged:        50,
		MaxDurationMins:        60,
	}); err != nil {
		t.Fatalf("CreateWorkflow() error: %v", err)
	}

	taskID := "task-artifacts"
	if err := db.CreateTask(ctx, CreateTaskParams{
		ID:            taskID,
		WorkflowID:    workflowID,
		SequenceNum:   1,
		TaskType:      "execution",
		AgentType:     "claude-code",
		TargetRepo:    "repo",
		Phase:         "scope",
		InputArtifact: "/tmp/work-order.yaml",
		State:         "pending",
		MaxAttempts:   2,
	}); err != nil {
		t.Fatalf("CreateTask() error: %v", err)
	}

	artifactDir := t.TempDir()
	contextPath := filepath.Join(artifactDir, "context.json")
	if err := os.WriteFile(contextPath, []byte("hello"), 0644); err != nil {
		t.Fatalf("WriteFile(contextPath) error: %v", err)
	}

	first, err := db.RegisterArtifact(ctx, RegisterArtifactParams{
		SessionID:    sessionID,
		WorkflowID:   workflowID,
		TaskID:       taskID,
		ArtifactType: "context_package",
		Path:         contextPath,
		Metadata: map[string]any{
			"phase": "scope",
		},
	})
	if err != nil {
		t.Fatalf("RegisterArtifact(first) error: %v", err)
	}

	if !first.SizeBytes.Valid || first.SizeBytes.Int64 != 5 {
		t.Fatalf("SizeBytes = %#v, want valid size 5", first.SizeBytes)
	}
	if !first.MetadataJSON.Valid || first.MetadataJSON.String != `{"phase":"scope"}` {
		t.Fatalf("MetadataJSON = %#v, want scope metadata", first.MetadataJSON)
	}

	reportPath := filepath.Join(artifactDir, "verify.txt")
	if err := os.WriteFile(reportPath, []byte("verification"), 0644); err != nil {
		t.Fatalf("WriteFile(reportPath) error: %v", err)
	}

	second, err := db.RegisterArtifact(ctx, RegisterArtifactParams{
		SessionID:    sessionID,
		WorkflowID:   workflowID,
		ArtifactType: "verify_report",
		Path:         reportPath,
	})
	if err != nil {
		t.Fatalf("RegisterArtifact(second) error: %v", err)
	}

	sessionArtifacts, err := db.ListArtifactsBySession(ctx, sessionID)
	if err != nil {
		t.Fatalf("ListArtifactsBySession() error: %v", err)
	}
	if len(sessionArtifacts) != 2 {
		t.Fatalf("len(ListArtifactsBySession()) = %d, want 2", len(sessionArtifacts))
	}

	workflowArtifacts, err := db.ListArtifactsByWorkflow(ctx, workflowID)
	if err != nil {
		t.Fatalf("ListArtifactsByWorkflow() error: %v", err)
	}
	if len(workflowArtifacts) != 2 {
		t.Fatalf("len(ListArtifactsByWorkflow()) = %d, want 2", len(workflowArtifacts))
	}

	latest, err := db.GetLatestArtifactByType(ctx, "verify_report", sessionID, workflowID)
	if err != nil {
		t.Fatalf("GetLatestArtifactByType() error: %v", err)
	}
	if latest.ID != second.ID {
		t.Fatalf("latest.ID = %q, want %q", latest.ID, second.ID)
	}
	if latest.Path != reportPath {
		t.Fatalf("latest.Path = %q, want %q", latest.Path, reportPath)
	}
}
