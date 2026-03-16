package database

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"
)

func TestStartSessionAndTransitionState(t *testing.T) {
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

	session, err := db.GetSession(ctx, sessionID)
	if err != nil {
		t.Fatalf("GetSession() error: %v", err)
	}
	if session.Kind != SessionKindPlanOnly {
		t.Fatalf("Kind = %q, want %q", session.Kind, SessionKindPlanOnly)
	}
	if session.Project != "repo" {
		t.Fatalf("Project = %q, want %q", session.Project, "repo")
	}
	if session.State != SessionStateRunning {
		t.Fatalf("State = %q, want %q", session.State, SessionStateRunning)
	}
	if !session.StartedAt.Valid {
		t.Fatal("StartedAt should be populated")
	}

	if err := db.TransitionSessionState(ctx, sessionID, SessionStateCompleted, ""); err != nil {
		t.Fatalf("TransitionSessionState() error: %v", err)
	}

	session, err = db.GetSession(ctx, sessionID)
	if err != nil {
		t.Fatalf("GetSession() after transition error: %v", err)
	}
	if session.State != SessionStateCompleted {
		t.Fatalf("State = %q, want %q", session.State, SessionStateCompleted)
	}
	if !session.CompletedAt.Valid {
		t.Fatal("CompletedAt should be populated")
	}
}

func TestAtomicClaimTask_SetsStartedTimestamps(t *testing.T) {
	t.Parallel()

	dbPath := filepath.Join(t.TempDir(), "conductor.db")
	db, err := NewDB(dbPath)
	if err != nil {
		t.Fatalf("NewDB() error: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	ctx := context.Background()
	workflowID := "wf-started"
	taskID := "task-started"

	if err := db.CreateWorkflow(ctx, CreateWorkflowParams{
		ID:                     workflowID,
		OriginalIntent:         "test",
		OriginalFile:           "/tmp/work-order.yaml",
		CurrentState:           "pending",
		TargetRepo:             "repo",
		GitBranch:              "feature/test",
		ContextPackagePath:     sql.NullString{},
		VerificationReportPath: sql.NullString{},
		MaxDepth:               5,
		MaxFilesChanged:        50,
		MaxDurationMins:        60,
	}); err != nil {
		t.Fatalf("CreateWorkflow() error: %v", err)
	}

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

	task, err := db.AtomicClaimTask(ctx, "worker-1")
	if err != nil {
		t.Fatalf("AtomicClaimTask() error: %v", err)
	}
	if task == nil {
		t.Fatal("expected claimed task")
	}
	if !task.StartedAt.Valid {
		t.Fatal("task.StartedAt should be populated")
	}

	workflow, err := db.GetWorkflow(ctx, workflowID)
	if err != nil {
		t.Fatalf("GetWorkflow() error: %v", err)
	}
	if !workflow.StartedAt.Valid {
		t.Fatal("workflow.StartedAt should be populated")
	}
}

func TestTransitionWorkflowState_MirrorsRunOnlySessionState(t *testing.T) {
	t.Parallel()

	dbPath := filepath.Join(t.TempDir(), "conductor.db")
	db, err := NewDB(dbPath)
	if err != nil {
		t.Fatalf("NewDB() error: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	ctx := context.Background()

	sessionID, err := db.StartSession(ctx, SessionKindRunOnly, "repo", "/tmp/work-order.yaml")
	if err != nil {
		t.Fatalf("StartSession() error: %v", err)
	}

	workflowID := "wf-mirror"
	if err := db.CreateWorkflow(ctx, CreateWorkflowParams{
		ID:                     workflowID,
		OriginalIntent:         "test",
		OriginalFile:           "/tmp/work-order.yaml",
		CurrentState:           "pending",
		TargetRepo:             "repo",
		GitBranch:              "feature/test",
		ContextPackagePath:     sql.NullString{},
		VerificationReportPath: sql.NullString{},
		MaxDepth:               5,
		MaxFilesChanged:        50,
		MaxDurationMins:        60,
	}); err != nil {
		t.Fatalf("CreateWorkflow() error: %v", err)
	}

	pipelineRunID := "pr-mirror"
	if err := db.CreatePipelineRun(ctx, CreatePipelineRunParams{
		ID:            pipelineRunID,
		WorkflowID:    workflowID,
		Project:       "repo",
		WorkOrderType: sql.NullString{String: "new_feature", Valid: true},
	}); err != nil {
		t.Fatalf("CreatePipelineRun() error: %v", err)
	}
	if err := db.LinkPipelineRunToSession(ctx, pipelineRunID, sessionID); err != nil {
		t.Fatalf("LinkPipelineRunToSession() error: %v", err)
	}
	linkedSessionID, err := db.GetSessionIDForWorkflow(ctx, workflowID)
	if err != nil {
		t.Fatalf("GetSessionIDForWorkflow() error: %v", err)
	}
	if linkedSessionID != sessionID {
		t.Fatalf("GetSessionIDForWorkflow() = %q, want %q", linkedSessionID, sessionID)
	}
	pipelineRuns, err := db.ListPipelineRunsBySession(ctx, sessionID)
	if err != nil {
		t.Fatalf("ListPipelineRunsBySession() error: %v", err)
	}
	if len(pipelineRuns) != 1 || pipelineRuns[0].ID != pipelineRunID {
		t.Fatalf("ListPipelineRunsBySession() = %#v, want pipeline_run %q", pipelineRuns, pipelineRunID)
	}

	if err := db.TransitionWorkflowState(ctx, workflowID, "human_review"); err != nil {
		t.Fatalf("TransitionWorkflowState(human_review) error: %v", err)
	}

	session, err := db.GetSession(ctx, sessionID)
	if err != nil {
		t.Fatalf("GetSession() error: %v", err)
	}
	if session.State != SessionStateAwaitingReview {
		t.Fatalf("session.State = %q, want %q", session.State, SessionStateAwaitingReview)
	}
	if session.CompletedAt.Valid {
		t.Fatal("session.CompletedAt should not be populated for awaiting_review")
	}

	if err := db.TransitionWorkflowState(ctx, workflowID, "failed"); err != nil {
		t.Fatalf("TransitionWorkflowState(failed) error: %v", err)
	}

	workflow, err := db.GetWorkflow(ctx, workflowID)
	if err != nil {
		t.Fatalf("GetWorkflow() error: %v", err)
	}
	if workflow.CurrentState != "failed" {
		t.Fatalf("workflow.CurrentState = %q, want %q", workflow.CurrentState, "failed")
	}
	if !workflow.CompletedAt.Valid {
		t.Fatal("workflow.CompletedAt should be populated")
	}

	session, err = db.GetSession(ctx, sessionID)
	if err != nil {
		t.Fatalf("GetSession() after failure error: %v", err)
	}
	if session.State != SessionStateFailed {
		t.Fatalf("session.State = %q, want %q", session.State, SessionStateFailed)
	}
	if !session.CompletedAt.Valid {
		t.Fatal("session.CompletedAt should be populated")
	}
}

func TestLinkPlanRunToSession(t *testing.T) {
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

	planRunID := "plan-run-1"
	if err := db.InsertPlanRun(ctx, InsertPlanRunParams{
		ID:                  planRunID,
		SpecFile:            "/tmp/spec.md",
		WorkOrdersGenerated: Int64(2),
	}); err != nil {
		t.Fatalf("InsertPlanRun() error: %v", err)
	}
	if err := db.LinkPlanRunToSession(ctx, planRunID, sessionID); err != nil {
		t.Fatalf("LinkPlanRunToSession() error: %v", err)
	}
	planRuns, err := db.ListPlanRunsBySession(ctx, sessionID)
	if err != nil {
		t.Fatalf("ListPlanRunsBySession() error: %v", err)
	}
	if len(planRuns) != 1 || planRuns[0].ID != planRunID {
		t.Fatalf("ListPlanRunsBySession() = %#v, want plan_run %q", planRuns, planRunID)
	}
	if planRuns[0].WorkOrdersGenerated.Int64 != 2 {
		t.Fatalf("WorkOrdersGenerated = %d, want 2", planRuns[0].WorkOrdersGenerated.Int64)
	}

	var linkedSessionID sql.NullString
	row := db.conn.QueryRowContext(ctx, `SELECT session_id FROM plan_runs WHERE id = ?`, planRunID)
	if err := row.Scan(&linkedSessionID); err != nil {
		t.Fatalf("QueryRow(scan session_id) error: %v", err)
	}
	if !linkedSessionID.Valid || linkedSessionID.String != sessionID {
		t.Fatalf("session_id = %q (valid=%t), want %q", linkedSessionID.String, linkedSessionID.Valid, sessionID)
	}
}

func TestGetSessionDetailIncludesRunsAndArtifacts(t *testing.T) {
	t.Parallel()

	dbPath := filepath.Join(t.TempDir(), "conductor.db")
	db, err := NewDB(dbPath)
	if err != nil {
		t.Fatalf("NewDB() error: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	ctx := context.Background()
	sessionID, err := db.StartSession(ctx, SessionKindRunOnly, "repo", "/tmp/work-order.yaml")
	if err != nil {
		t.Fatalf("StartSession() error: %v", err)
	}

	workflowID := "wf-detail"
	if err := db.CreateWorkflow(ctx, CreateWorkflowParams{
		ID:                     workflowID,
		OriginalIntent:         "detail test",
		OriginalFile:           "/tmp/work-order.yaml",
		CurrentState:           "pending",
		TargetRepo:             "repo",
		GitBranch:              "feature/detail",
		ContextPackagePath:     sql.NullString{},
		VerificationReportPath: sql.NullString{},
		MaxDepth:               5,
		MaxFilesChanged:        50,
		MaxDurationMins:        60,
	}); err != nil {
		t.Fatalf("CreateWorkflow() error: %v", err)
	}

	pipelineRunID := "pr-detail"
	if err := db.CreatePipelineRun(ctx, CreatePipelineRunParams{
		ID:            pipelineRunID,
		WorkflowID:    workflowID,
		Project:       "repo",
		WorkOrderType: sql.NullString{String: "new_feature", Valid: true},
	}); err != nil {
		t.Fatalf("CreatePipelineRun() error: %v", err)
	}
	if err := db.LinkPipelineRunToSession(ctx, pipelineRunID, sessionID); err != nil {
		t.Fatalf("LinkPipelineRunToSession() error: %v", err)
	}

	artifactPath := filepath.Join(t.TempDir(), "context.json")
	if err := os.WriteFile(artifactPath, []byte("{}"), 0644); err != nil {
		t.Fatalf("WriteFile() error: %v", err)
	}
	if _, err := db.RegisterArtifact(ctx, RegisterArtifactParams{
		SessionID:    sessionID,
		WorkflowID:   workflowID,
		ArtifactType: ArtifactTypeContextPackage,
		Path:         artifactPath,
	}); err != nil {
		t.Fatalf("RegisterArtifact() error: %v", err)
	}

	detail, err := db.GetSessionDetail(ctx, sessionID)
	if err != nil {
		t.Fatalf("GetSessionDetail() error: %v", err)
	}
	if detail.Session.ID != sessionID {
		t.Fatalf("Session.ID = %q, want %q", detail.Session.ID, sessionID)
	}
	if len(detail.PipelineRuns) != 1 || detail.PipelineRuns[0].ID != pipelineRunID {
		t.Fatalf("PipelineRuns = %#v, want pipeline_run %q", detail.PipelineRuns, pipelineRunID)
	}
	if len(detail.Artifacts) != 1 || detail.Artifacts[0].ArtifactType != ArtifactTypeContextPackage {
		t.Fatalf("Artifacts = %#v, want one %q artifact", detail.Artifacts, ArtifactTypeContextPackage)
	}
}

func TestFindSessionsByPrefix(t *testing.T) {
	t.Parallel()

	dbPath := filepath.Join(t.TempDir(), "conductor.db")
	db, err := NewDB(dbPath)
	if err != nil {
		t.Fatalf("NewDB() error: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	ctx := context.Background()
	idA, err := db.StartSession(ctx, SessionKindPlanOnly, "repo", "/tmp/spec-a.md")
	if err != nil {
		t.Fatalf("StartSession(a) error: %v", err)
	}
	idB, err := db.StartSession(ctx, SessionKindRunOnly, "repo", "/tmp/work-order.yaml")
	if err != nil {
		t.Fatalf("StartSession(b) error: %v", err)
	}

	ids, err := db.FindSessionsByPrefix(ctx, idA[:8])
	if err != nil {
		t.Fatalf("FindSessionsByPrefix() error: %v", err)
	}
	if len(ids) != 1 || ids[0] != idA {
		t.Fatalf("FindSessionsByPrefix(%q) = %#v, want [%q]", idA[:8], ids, idA)
	}

	ids, err = db.FindSessionsByPrefix(ctx, "missing")
	if err != nil {
		t.Fatalf("FindSessionsByPrefix(missing) error: %v", err)
	}
	if len(ids) != 0 {
		t.Fatalf("FindSessionsByPrefix(missing) = %#v, want empty", ids)
	}

	if idA == idB {
		t.Fatal("session ids should be unique")
	}
}
