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
