package main

import (
	"bytes"
	"context"
	"database/sql"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ponchione/agent-conductor/internal/config"
	"github.com/ponchione/agent-conductor/internal/database"
	"github.com/ponchione/agent-conductor/internal/models"
	"gopkg.in/yaml.v3"
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
	sessionID, workflowID, err := initializeRunSession(ctx, db, cfg, sourcePath, sourceContent, wo, nil)
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

func TestLoadRunInputStandaloneWorkOrder(t *testing.T) {
	t.Parallel()

	sourceContent := []byte("schema_version: 2\ntitle: Add logout endpoint\ntype: new_feature\ntarget_module: auth\nrequirements:\n  - id: R1\n    text: Add logout endpoint\nacceptance_criteria:\n  - id: AC1\n    description: Logout endpoint exists\n    requirement_ids: [R1]\n    required: true\n    verification:\n      kind: precheck\n      check: curl /logout\nconstraints:\n  - keep auth middleware intact\n")

	input, err := loadRunInput("/tmp/work-order.yaml", sourceContent, "")
	if err != nil {
		t.Fatalf("loadRunInput() error = %v", err)
	}
	if input.PlanOrigin != nil {
		t.Fatalf("input.PlanOrigin = %#v, want nil", input.PlanOrigin)
	}
	if string(input.SourceContent) != string(sourceContent) {
		t.Fatalf("input.SourceContent mismatch:\n got %q\nwant %q", string(input.SourceContent), string(sourceContent))
	}
	if input.WorkOrder.Title != "Add logout endpoint" {
		t.Fatalf("input.WorkOrder.Title = %q, want %q", input.WorkOrder.Title, "Add logout endpoint")
	}
}

func TestLoadRunInputPlanManifestAndInitializeRunSession(t *testing.T) {
	t.Parallel()

	planDoc := &planDocument{
		Version:  1,
		SpecFile: "spec.md",
		Requirements: []planRequirement{
			{ID: "REQ-1", Text: "Implement endpoint"},
		},
		Epics: []planEpic{
			{
				ID:          "epic-001",
				EpicRef:     "auth",
				Title:       "Auth work",
				Description: "Auth work description",
				Covers:      []string{"REQ-1"},
				Tasks: []planTask{
					{
						ID:            "task-001",
						TaskRef:       "create-session",
						SchemaVersion: models.WorkOrderSchemaVersion,
						EpicID:        "epic-001",
						Title:         "Create session",
						Type:          "new_feature",
						TargetModule:  "auth",
						Requirements: []models.WorkOrderRequirement{
							{ID: "REQ-1", Text: "Implement endpoint"},
						},
						AcceptanceCriteria: []models.TypedAcceptanceCriterion{
							{
								ID:             "AC-1",
								Description:    "Session is created",
								RequirementIDs: []string{"REQ-1"},
								Required:       boolPtr(true),
								Verification: models.AcceptanceVerification{
									Kind:  "precheck",
									Check: "curl /session",
								},
							},
						},
						Constraints: []string{"keep auth middleware intact"},
						Size:        "S",
					},
					{
						ID:            "task-002",
						TaskRef:       "add-logout",
						SchemaVersion: models.WorkOrderSchemaVersion,
						EpicID:        "epic-001",
						Title:         "Add logout endpoint",
						Type:          "new_feature",
						TargetModule:  "auth",
						Requirements: []models.WorkOrderRequirement{
							{ID: "REQ-1", Text: "Implement endpoint"},
						},
						AcceptanceCriteria: []models.TypedAcceptanceCriterion{
							{
								ID:             "AC-2",
								Description:    "Logout endpoint exists",
								RequirementIDs: []string{"REQ-1"},
								Required:       boolPtr(true),
								Verification: models.AcceptanceVerification{
									Kind:  "precheck",
									Check: "curl /logout",
								},
							},
						},
						Constraints: []string{"keep auth middleware intact"},
						DependsOn:   []string{"task-001"},
						Size:        "S",
					},
				},
			},
		},
	}
	manifestContent, err := yaml.Marshal(planDoc)
	if err != nil {
		t.Fatalf("yaml.Marshal(planDoc) error: %v", err)
	}

	input, err := loadRunInput("/tmp/plan.yaml", manifestContent, "task-002")
	if err != nil {
		t.Fatalf("loadRunInput() error = %v", err)
	}
	if input.PlanOrigin == nil {
		t.Fatal("input.PlanOrigin = nil, want manifest origin metadata")
	}
	if input.PlanOrigin.PlanFile != "/tmp/plan.yaml" {
		t.Fatalf("PlanFile = %q, want %q", input.PlanOrigin.PlanFile, "/tmp/plan.yaml")
	}
	if input.PlanOrigin.PlanTaskID != "task-002" || input.PlanOrigin.PlanEpicID != "epic-001" {
		t.Fatalf("plan origin = %#v, want task-002/epic-001", input.PlanOrigin)
	}
	if got := input.PlanOrigin.DependencyTaskIDs; len(got) != 1 || got[0] != "task-001" {
		t.Fatalf("DependencyTaskIDs = %v, want [task-001]", got)
	}
	if strings.Contains(string(input.SourceContent), "version: 1") {
		t.Fatalf("input.SourceContent contains plan manifest fields, want strict work order YAML:\n%s", string(input.SourceContent))
	}
	if !strings.Contains(string(input.SourceContent), "title: Add logout endpoint") {
		t.Fatalf("input.SourceContent missing selected task work order YAML:\n%s", string(input.SourceContent))
	}

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
	_, workflowID, err := initializeRunSession(ctx, db, cfg, "/tmp/plan.yaml", input.SourceContent, input.WorkOrder, input.PlanOrigin)
	if err != nil {
		t.Fatalf("initializeRunSession() error: %v", err)
	}

	rawDB, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("sql.Open() error: %v", err)
	}
	t.Cleanup(func() { _ = rawDB.Close() })

	var (
		planFile         sql.NullString
		planTaskID       sql.NullString
		planEpicID       sql.NullString
		workOrderContent sql.NullString
	)
	if err := rawDB.QueryRowContext(ctx, `SELECT plan_file, plan_task_id, plan_epic_id, work_order_content FROM pipeline_runs WHERE workflow_id = ?`, workflowID).Scan(
		&planFile,
		&planTaskID,
		&planEpicID,
		&workOrderContent,
	); err != nil {
		t.Fatalf("query pipeline_runs metadata error: %v", err)
	}
	if planFile.String != "/tmp/plan.yaml" {
		t.Fatalf("plan_file = %q, want %q", planFile.String, "/tmp/plan.yaml")
	}
	if planTaskID.String != "task-002" {
		t.Fatalf("plan_task_id = %q, want %q", planTaskID.String, "task-002")
	}
	if planEpicID.String != "epic-001" {
		t.Fatalf("plan_epic_id = %q, want %q", planEpicID.String, "epic-001")
	}
	if workOrderContent.String != string(input.SourceContent) {
		t.Fatalf("work_order_content mismatch:\n got %q\nwant %q", workOrderContent.String, string(input.SourceContent))
	}
}

func TestWarnOnUnmetPlanDependencies(t *testing.T) {
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

	ctx := context.Background()
	origin := &planRunOrigin{
		PlanFile:          "/tmp/plan.yaml",
		PlanTaskID:        "task-002",
		DependencyTaskIDs: []string{"task-001"},
	}

	output, err := captureStdout(func() error {
		return warnOnUnmetPlanDependencies(ctx, db, origin)
	})
	if err != nil {
		t.Fatalf("warnOnUnmetPlanDependencies() error = %v", err)
	}
	if !strings.Contains(output, "dependency task-001 for task task-002 has no prior PASS + approved pipeline run") {
		t.Fatalf("warning output = %q, want dependency warning", output)
	}

	if err := db.CreateWorkflow(ctx, database.CreateWorkflowParams{
		ID:                     "wf-success",
		OriginalIntent:         "Work Order: satisfied dependency",
		OriginalFile:           "/tmp/input.yaml",
		CurrentState:           "completed",
		TargetRepo:             "repo",
		GitBranch:              "feature/conducted-success",
		ContextPackagePath:     sql.NullString{},
		VerificationReportPath: sql.NullString{},
		MaxDepth:               5,
		MaxFilesChanged:        25,
		MaxDurationMins:        45,
	}); err != nil {
		t.Fatalf("CreateWorkflow() error: %v", err)
	}
	if err := db.CreatePipelineRun(ctx, database.CreatePipelineRunParams{
		ID:            "pr-success",
		WorkflowID:    "wf-success",
		Project:       "repo",
		WorkOrderType: sql.NullString{String: "new_feature", Valid: true},
		PlanFile:      sql.NullString{String: "/tmp/plan.yaml", Valid: true},
		PlanTaskID:    sql.NullString{String: "task-001", Valid: true},
		PlanEpicID:    sql.NullString{String: "epic-001", Valid: true},
	}); err != nil {
		t.Fatalf("CreatePipelineRun() error: %v", err)
	}
	if err := db.UpdatePipelineRunVerify(ctx, database.UpdatePipelineRunVerifyParams{
		VerifyStartedAt:   sql.NullString{},
		VerifyCompletedAt: sql.NullString{},
		VerifyTokensIn:    sql.NullInt64{},
		VerifyTokensOut:   sql.NullInt64{},
		VerifyModel:       sql.NullString{},
		VerifyResult:      sql.NullString{String: "PASS", Valid: true},
		BuildScopeDrift:   0,
		WorkflowID:        "wf-success",
	}); err != nil {
		t.Fatalf("UpdatePipelineRunVerify() error: %v", err)
	}
	if err := db.UpdatePipelineRunHumanResult(ctx, database.UpdatePipelineRunHumanResultParams{
		HumanResult: sql.NullString{String: "approved", Valid: true},
		WorkflowID:  "wf-success",
	}); err != nil {
		t.Fatalf("UpdatePipelineRunHumanResult() error: %v", err)
	}

	output, err = captureStdout(func() error {
		return warnOnUnmetPlanDependencies(ctx, db, origin)
	})
	if err != nil {
		t.Fatalf("warnOnUnmetPlanDependencies() with satisfied dependency error = %v", err)
	}
	if output != "" {
		t.Fatalf("warning output = %q, want empty output once dependency has prior successful approved run", output)
	}
}

func TestWarnOnUnmetPlanDependenciesIgnoresSuccessfulRunsFromOtherManifest(t *testing.T) {
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

	ctx := context.Background()
	origin := &planRunOrigin{
		PlanFile:          "/tmp/current-plan.yaml",
		PlanTaskID:        "task-002",
		DependencyTaskIDs: []string{"task-001"},
	}

	if err := db.CreateWorkflow(ctx, database.CreateWorkflowParams{
		ID:                     "wf-other-plan-success",
		OriginalIntent:         "Work Order: satisfied elsewhere",
		OriginalFile:           "/tmp/other-plan.yaml",
		CurrentState:           "completed",
		TargetRepo:             "repo",
		GitBranch:              "feature/conducted-other",
		ContextPackagePath:     sql.NullString{},
		VerificationReportPath: sql.NullString{},
		MaxDepth:               5,
		MaxFilesChanged:        25,
		MaxDurationMins:        45,
	}); err != nil {
		t.Fatalf("CreateWorkflow() error: %v", err)
	}
	if err := db.CreatePipelineRun(ctx, database.CreatePipelineRunParams{
		ID:            "pr-other-plan-success",
		WorkflowID:    "wf-other-plan-success",
		Project:       "repo",
		WorkOrderType: sql.NullString{String: "new_feature", Valid: true},
		PlanFile:      sql.NullString{String: "/tmp/other-plan.yaml", Valid: true},
		PlanTaskID:    sql.NullString{String: "task-001", Valid: true},
		PlanEpicID:    sql.NullString{String: "epic-001", Valid: true},
	}); err != nil {
		t.Fatalf("CreatePipelineRun() error: %v", err)
	}
	if err := db.UpdatePipelineRunVerify(ctx, database.UpdatePipelineRunVerifyParams{
		VerifyStartedAt:   sql.NullString{},
		VerifyCompletedAt: sql.NullString{},
		VerifyTokensIn:    sql.NullInt64{},
		VerifyTokensOut:   sql.NullInt64{},
		VerifyModel:       sql.NullString{},
		VerifyResult:      sql.NullString{String: "PASS", Valid: true},
		BuildScopeDrift:   0,
		WorkflowID:        "wf-other-plan-success",
	}); err != nil {
		t.Fatalf("UpdatePipelineRunVerify() error: %v", err)
	}
	if err := db.UpdatePipelineRunHumanResult(ctx, database.UpdatePipelineRunHumanResultParams{
		HumanResult: sql.NullString{String: "approved", Valid: true},
		WorkflowID:  "wf-other-plan-success",
	}); err != nil {
		t.Fatalf("UpdatePipelineRunHumanResult() error: %v", err)
	}

	output, err := captureStdout(func() error {
		return warnOnUnmetPlanDependencies(ctx, db, origin)
	})
	if err != nil {
		t.Fatalf("warnOnUnmetPlanDependencies() error = %v", err)
	}
	if !strings.Contains(output, "dependency task-001 for task task-002 has no prior PASS + approved pipeline run") {
		t.Fatalf("warning output = %q, want dependency warning for current plan", output)
	}
}

func captureStdout(fn func() error) (string, error) {
	originalStdout := os.Stdout
	reader, writer, err := os.Pipe()
	if err != nil {
		return "", err
	}
	defer reader.Close()

	os.Stdout = writer
	runErr := fn()
	_ = writer.Close()
	os.Stdout = originalStdout

	var buf bytes.Buffer
	if _, err := io.Copy(&buf, reader); err != nil {
		return "", err
	}
	return buf.String(), runErr
}
