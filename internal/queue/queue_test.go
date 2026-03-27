package queue

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/ponchione/agent-conductor/internal/database"
)

func TestClaimNextTask_BlockedWorkflowReturnsNoTasks(t *testing.T) {
	t.Parallel()

	dbPath := filepath.Join(t.TempDir(), "conductor.db")
	db, err := database.NewDB(dbPath)
	if err != nil {
		t.Fatalf("NewDB() error: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	createQueueWorkflow(t, db, "wf-blocked", StateReviewNeeded, 5, 50, 0, 0)
	createQueueTask(t, db, "task-blocked", "wf-blocked")

	q := New(nil, db)
	task, err := q.ClaimNextTask("worker-1")
	if err != ErrNoTasks {
		t.Fatalf("ClaimNextTask() error = %v, want ErrNoTasks", err)
	}
	if task != nil {
		t.Fatalf("ClaimNextTask() task = %#v, want nil", task)
	}
}

func TestClaimNextTask_BudgetGatedWorkflowReturnsNoTasksAndTransitionsToReview(t *testing.T) {
	t.Parallel()

	dbPath := filepath.Join(t.TempDir(), "conductor.db")
	db, err := database.NewDB(dbPath)
	if err != nil {
		t.Fatalf("NewDB() error: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	ctx := context.Background()
	createQueueWorkflow(t, db, "wf-budget", "running", 1, 50, 1, 0)
	createQueueTask(t, db, "task-budget", "wf-budget")

	q := New(nil, db)
	task, err := q.ClaimNextTask("worker-1")
	if err != ErrNoTasks {
		t.Fatalf("ClaimNextTask() error = %v, want ErrNoTasks", err)
	}
	if task != nil {
		t.Fatalf("ClaimNextTask() task = %#v, want nil", task)
	}

	workflow, err := db.GetWorkflow(ctx, "wf-budget")
	if err != nil {
		t.Fatalf("GetWorkflow() error: %v", err)
	}
	if workflow.CurrentState != StateReviewNeeded {
		t.Fatalf("workflow.CurrentState = %q, want %q", workflow.CurrentState, StateReviewNeeded)
	}

	storedTask, err := db.GetTask(ctx, "task-budget")
	if err != nil {
		t.Fatalf("GetTask() error: %v", err)
	}
	if storedTask.State != "pending" {
		t.Fatalf("storedTask.State = %q, want pending", storedTask.State)
	}
}

func createQueueWorkflow(t *testing.T, db *database.DB, workflowID, state string, maxDepth, maxFilesChanged, currentDepth, filesChanged int64) {
	t.Helper()

	if err := db.CreateWorkflow(context.Background(), database.CreateWorkflowParams{
		ID:                     workflowID,
		OriginalIntent:         "test",
		OriginalFile:           "/tmp/work-order.yaml",
		CurrentState:           state,
		TargetRepo:             "repo",
		GitBranch:              "feature/" + workflowID,
		ContextPackagePath:     sql.NullString{},
		VerificationReportPath: sql.NullString{},
		MaxDepth:               maxDepth,
		MaxFilesChanged:        maxFilesChanged,
		MaxDurationMins:        60,
	}); err != nil {
		t.Fatalf("CreateWorkflow(%s) error: %v", workflowID, err)
	}
	if err := db.UpdateWorkflowBudget(context.Background(), database.UpdateWorkflowBudgetParams{
		CurrentDepth: currentDepth,
		FilesChanged: filesChanged,
		ID:           workflowID,
	}); err != nil {
		t.Fatalf("UpdateWorkflowBudget(%s) error: %v", workflowID, err)
	}
}

func createQueueTask(t *testing.T, db *database.DB, taskID, workflowID string) {
	t.Helper()

	if err := db.CreateTask(context.Background(), database.CreateTaskParams{
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
		t.Fatalf("CreateTask(%s) error: %v", taskID, err)
	}
}
