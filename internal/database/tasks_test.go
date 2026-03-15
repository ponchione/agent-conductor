package database

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
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

	var wg sync.WaitGroup
	results := make(chan *Task, 2)
	errs := make(chan error, 2)

	claim := func(workerID string) {
		defer wg.Done()
		for range 5 {
			task, claimErr := db.AtomicClaimTask(ctx, workerID)
			if claimErr != nil {
				if strings.Contains(claimErr.Error(), "SQLITE_BUSY") || strings.Contains(claimErr.Error(), "database is locked") {
					time.Sleep(10 * time.Millisecond)
					continue
				}
				errs <- claimErr
				return
			}
			results <- task
			return
		}
		errs <- context.DeadlineExceeded
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
