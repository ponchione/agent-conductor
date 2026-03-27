package database

import (
	"context"
	"database/sql"
	"path/filepath"
	"sync"
	"testing"
)

func TestListTasksByWorkflow(t *testing.T) {
	t.Parallel()

	dbPath := filepath.Join(t.TempDir(), "conductor.db")
	db, err := NewDB(dbPath)
	if err != nil {
		t.Fatalf("NewDB() error: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	ctx := context.Background()
	workflowID := "wf-list"

	if err := db.CreateWorkflow(ctx, CreateWorkflowParams{
		ID:                     workflowID,
		OriginalIntent:         "test list tasks",
		OriginalFile:           "/tmp/work-order.yaml",
		CurrentState:           "running",
		TargetRepo:             "repo",
		GitBranch:              "feature/list-test",
		ContextPackagePath:     sql.NullString{},
		VerificationReportPath: sql.NullString{},
		MaxDepth:               5,
		MaxFilesChanged:        50,
		MaxDurationMins:        60,
	}); err != nil {
		t.Fatalf("CreateWorkflow() error: %v", err)
	}

	// Insert task with sequence_num 2 first to verify ordering.
	if err := db.CreateTask(ctx, CreateTaskParams{
		ID:            "task-b",
		WorkflowID:    workflowID,
		SequenceNum:   2,
		TaskType:      "execution",
		AgentType:     "claude-code",
		TargetRepo:    "repo",
		Phase:         "build",
		InputArtifact: "/tmp/ctx.json",
		State:         "pending",
		MaxAttempts:   3,
	}); err != nil {
		t.Fatalf("CreateTask(task-b) error: %v", err)
	}

	if err := db.CreateTask(ctx, CreateTaskParams{
		ID:            "task-a",
		WorkflowID:    workflowID,
		SequenceNum:   1,
		TaskType:      "execution",
		AgentType:     "claude-code",
		TargetRepo:    "repo",
		Phase:         "scope",
		InputArtifact: "/tmp/work-order.yaml",
		State:         "running",
		MaxAttempts:   2,
	}); err != nil {
		t.Fatalf("CreateTask(task-a) error: %v", err)
	}

	tasks, err := db.ListTasksByWorkflow(ctx, workflowID)
	if err != nil {
		t.Fatalf("ListTasksByWorkflow() error: %v", err)
	}
	if len(tasks) != 2 {
		t.Fatalf("expected 2 tasks, got %d", len(tasks))
	}

	// First task should be sequence_num 1.
	if tasks[0].ID != "task-a" {
		t.Errorf("tasks[0].ID = %q, want %q", tasks[0].ID, "task-a")
	}
	if tasks[0].SequenceNum != 1 {
		t.Errorf("tasks[0].SequenceNum = %d, want 1", tasks[0].SequenceNum)
	}
	if tasks[0].Phase != "scope" {
		t.Errorf("tasks[0].Phase = %q, want %q", tasks[0].Phase, "scope")
	}
	if tasks[0].State != "running" {
		t.Errorf("tasks[0].State = %q, want %q", tasks[0].State, "running")
	}

	// Second task should be sequence_num 2.
	if tasks[1].ID != "task-b" {
		t.Errorf("tasks[1].ID = %q, want %q", tasks[1].ID, "task-b")
	}
	if tasks[1].SequenceNum != 2 {
		t.Errorf("tasks[1].SequenceNum = %d, want 2", tasks[1].SequenceNum)
	}
	if tasks[1].Phase != "build" {
		t.Errorf("tasks[1].Phase = %q, want %q", tasks[1].Phase, "build")
	}
	if tasks[1].State != "pending" {
		t.Errorf("tasks[1].State = %q, want %q", tasks[1].State, "pending")
	}
}

func TestAtomicClaimTask_ConcurrentClaimSingleWinner(t *testing.T) {
	t.Parallel()

	dbPath := filepath.Join(t.TempDir(), "conductor.db")
	db, err := NewDB(dbPath)
	if err != nil {
		t.Fatalf("NewDB() error: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	ctx := context.Background()
	workflowID := "wf-1"
	taskID := "task-1"

	createWorkflowForTaskTests(t, db, workflowID, "pending")
	createTaskForWorkflowTests(t, db, taskID, workflowID)

	var wg sync.WaitGroup
	results := make(chan *Task, 2)
	errs := make(chan error, 2)

	claim := func(workerID string) {
		defer wg.Done()
		task, claimErr := db.AtomicClaimTask(ctx, workerID)
		if claimErr != nil {
			errs <- claimErr
			return
		}
		results <- task
	}

	wg.Add(2)
	go claim("worker-a")
	go claim("worker-b")
	wg.Wait()
	close(results)
	close(errs)

	for e := range errs {
		t.Fatalf("AtomicClaimTask() unexpected error: %v", e)
	}

	var claimed []*Task
	for r := range results {
		if r != nil {
			claimed = append(claimed, r)
		}
	}

	if len(claimed) != 1 {
		t.Fatalf("expected exactly one claimed task, got %d", len(claimed))
	}
	if claimed[0].ID != taskID {
		t.Fatalf("claimed task ID = %q, want %q", claimed[0].ID, taskID)
	}
}

func TestAtomicClaimTask_SkipsBlockedWorkflowTasks(t *testing.T) {
	t.Parallel()

	dbPath := filepath.Join(t.TempDir(), "conductor.db")
	db, err := NewDB(dbPath)
	if err != nil {
		t.Fatalf("NewDB() error: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	ctx := context.Background()
	createWorkflowForTaskTests(t, db, "wf-blocked", "human_review")
	createTaskForWorkflowTests(t, db, "task-blocked", "wf-blocked")

	task, err := db.AtomicClaimTask(ctx, "worker-1")
	if err != nil {
		t.Fatalf("AtomicClaimTask() error: %v", err)
	}
	if task != nil {
		t.Fatalf("AtomicClaimTask() = %#v, want nil", task)
	}
}

func TestAtomicClaimTask_ClaimsRunnableTaskWhenBlockedTaskExists(t *testing.T) {
	t.Parallel()

	dbPath := filepath.Join(t.TempDir(), "conductor.db")
	db, err := NewDB(dbPath)
	if err != nil {
		t.Fatalf("NewDB() error: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	ctx := context.Background()
	createWorkflowForTaskTests(t, db, "wf-blocked", "human_review")
	createTaskForWorkflowTests(t, db, "task-blocked", "wf-blocked")
	createWorkflowForTaskTests(t, db, "wf-runnable", "pending")
	createTaskForWorkflowTests(t, db, "task-runnable", "wf-runnable")

	task, err := db.AtomicClaimTask(ctx, "worker-1")
	if err != nil {
		t.Fatalf("AtomicClaimTask() error: %v", err)
	}
	if task == nil {
		t.Fatal("expected claimed task, got nil")
	}
	if task.ID != "task-runnable" {
		t.Fatalf("claimed task ID = %q, want %q", task.ID, "task-runnable")
	}
}

func createWorkflowForTaskTests(t *testing.T, db *DB, workflowID, state string) {
	t.Helper()

	if err := db.CreateWorkflow(context.Background(), CreateWorkflowParams{
		ID:                     workflowID,
		OriginalIntent:         "test",
		OriginalFile:           "/tmp/work-order.yaml",
		CurrentState:           state,
		TargetRepo:             "repo",
		GitBranch:              "feature/" + workflowID,
		ContextPackagePath:     sql.NullString{},
		VerificationReportPath: sql.NullString{},
		MaxDepth:               5,
		MaxFilesChanged:        50,
		MaxDurationMins:        60,
	}); err != nil {
		t.Fatalf("CreateWorkflow(%s) error: %v", workflowID, err)
	}
}

func createTaskForWorkflowTests(t *testing.T, db *DB, taskID, workflowID string) {
	t.Helper()

	if err := db.CreateTask(context.Background(), CreateTaskParams{
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
