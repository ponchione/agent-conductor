package main

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	"github.com/ponchione/agent-conductor/internal/config"
	"github.com/ponchione/agent-conductor/internal/database"
	"github.com/ponchione/agent-conductor/internal/models"
	_ "modernc.org/sqlite"
)

func TestInitializeRunSessionPersistsManagedWorkOrderHistory(t *testing.T) {
	t.Parallel()

	dataDir := t.TempDir()
	dbPath := filepath.Join(dataDir, "db", "conductor.db")
	if err := os.MkdirAll(filepath.Dir(dbPath), 0755); err != nil {
		t.Fatalf("MkdirAll(db dir) error: %v", err)
	}

	db, err := database.NewDB(dbPath)
	if err != nil {
		t.Fatalf("NewDB() error: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	sourcePath := filepath.Join(t.TempDir(), "input.yaml")
	sourceContent := []byte("schema_version: 2\ntitle: Add logout endpoint\ntype: new_feature\ntarget_module: auth\nrequirements:\n  - id: R1\n    text: Add logout endpoint\nacceptance_criteria:\n  - id: AC1\n    description: Logout endpoint exists\n    requirement_ids: [R1]\n    required: true\n    verification:\n      kind: precheck\n      check: curl /logout\nconstraints:\n  - keep auth middleware intact\n")
	if err := os.WriteFile(sourcePath, sourceContent, 0644); err != nil {
		t.Fatalf("WriteFile(sourcePath) error: %v", err)
	}

	wo := models.WorkOrder{
		SchemaVersion: models.WorkOrderSchemaVersion,
		Title:         "Add logout endpoint",
		Type:          "new_feature",
		TargetModule:  "auth",
		Requirements: []models.WorkOrderRequirement{
			{ID: "R1", Text: "Add logout endpoint"},
		},
		TypedAcceptanceCriteria: []models.TypedAcceptanceCriterion{
			{
				ID:             "AC1",
				Description:    "Logout endpoint exists",
				RequirementIDs: []string{"R1"},
				Required:       boolPtr(true),
				Verification: models.AcceptanceVerification{
					Kind:  "precheck",
					Check: "curl /logout",
				},
			},
		},
		Constraints: []string{"keep auth middleware intact"},
	}

	cfg := &config.ProjectConfig{
		Project: config.Project{
			Name:    "repo",
			DataDir: dataDir,
		},
		Git: config.Git{
			BranchPrefix: "feature/conducted",
		},
		Safety: config.Safety{
			MaxFilesChanged: 25,
			MaxDurationMins: 45,
		},
	}

	ctx := context.Background()
	sessionID, workflowID, err := initializeRunSession(ctx, db, cfg, sourcePath, sourceContent, wo)
	if err != nil {
		t.Fatalf("initializeRunSession() error: %v", err)
	}
	if sessionID == "" || workflowID == "" {
		t.Fatalf("initializeRunSession() returned empty IDs: session=%q workflow=%q", sessionID, workflowID)
	}

	managedPath := runInputWorkOrderPath(dataDir, workflowID)

	workflow, err := db.GetWorkflow(ctx, workflowID)
	if err != nil {
		t.Fatalf("GetWorkflow() error: %v", err)
	}
	if workflow.OriginalFile != managedPath {
		t.Fatalf("workflow.OriginalFile = %q, want %q", workflow.OriginalFile, managedPath)
	}

	tasks, err := db.ListTasksByWorkflow(ctx, workflowID)
	if err != nil {
		t.Fatalf("ListTasksByWorkflow() error: %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("len(ListTasksByWorkflow()) = %d, want 1", len(tasks))
	}
	task, err := db.GetTask(ctx, tasks[0].ID)
	if err != nil {
		t.Fatalf("GetTask() error: %v", err)
	}
	if task.InputArtifact != managedPath {
		t.Fatalf("task.InputArtifact = %q, want %q", task.InputArtifact, managedPath)
	}

	storedContent, err := os.ReadFile(managedPath)
	if err != nil {
		t.Fatalf("ReadFile(managedPath) error: %v", err)
	}
	if string(storedContent) != string(sourceContent) {
		t.Fatalf("managed work order content mismatch:\n got %q\nwant %q", string(storedContent), string(sourceContent))
	}

	var workOrderContent sql.NullString
	rawDB, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("sql.Open() error: %v", err)
	}
	t.Cleanup(func() { _ = rawDB.Close() })
	if err := rawDB.QueryRowContext(ctx, `SELECT work_order_content FROM pipeline_runs WHERE workflow_id = ?`, workflowID).Scan(&workOrderContent); err != nil {
		t.Fatalf("query work_order_content error: %v", err)
	}
	if !workOrderContent.Valid || workOrderContent.String != string(sourceContent) {
		t.Fatalf("work_order_content = %#v, want raw source YAML", workOrderContent)
	}

	artifacts, err := db.ListArtifactsByWorkflow(ctx, workflowID)
	if err != nil {
		t.Fatalf("ListArtifactsByWorkflow() error: %v", err)
	}
	if len(artifacts) != 1 {
		t.Fatalf("len(ListArtifactsByWorkflow()) = %d, want 1", len(artifacts))
	}
	if artifacts[0].ArtifactType != database.ArtifactTypeInputWorkOrder {
		t.Fatalf("artifacts[0].ArtifactType = %q, want %q", artifacts[0].ArtifactType, database.ArtifactTypeInputWorkOrder)
	}
	if artifacts[0].Path != managedPath {
		t.Fatalf("artifacts[0].Path = %q, want %q", artifacts[0].Path, managedPath)
	}
}

func boolPtr(v bool) *bool {
	return &v
}
