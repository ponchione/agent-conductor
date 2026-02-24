package main

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/google/uuid"
	"github.com/ponchione/agent-conductor/internal/config"
	"github.com/ponchione/agent-conductor/internal/database"
	"github.com/ponchione/agent-conductor/internal/git"
	"github.com/ponchione/agent-conductor/internal/worker"
)

// runSync executes a work order synchronously
func runSync(args []string, w *worker.Worker, db *database.DB, cfg *config.ProjectConfig, gitMgr *git.GitManager) {
	if len(args) == 0 {
		fmt.Fprintf(os.Stderr, "Usage: conductor run <work-order-path>\n")
		os.Exit(1)
	}

	woPath := args[0]
	absPath, err := filepath.Abs(woPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "invalid path: %v\n", err)
		os.Exit(1)
	}

	if _, err := os.Stat(absPath); os.IsNotExist(err) {
		fmt.Fprintf(os.Stderr, "file not found: %s\n", absPath)
		os.Exit(1)
	}

	fmt.Printf("Starting synchronous execution for: %s\n\n", absPath)

	ctx := context.Background()

	// 1. Create Workflow and Task manually
	wfID := uuid.New().String()
	taskID := uuid.New().String()
	branchName := fmt.Sprintf("%s-%s", cfg.Git.BranchPrefix, wfID[:8])

	fmt.Printf("Creating git branch: %s\n", branchName)
	if err := gitMgr.CreateBranch(cfg.Project.Path, branchName, "main"); err != nil {
		fmt.Fprintf(os.Stderr, "git create branch failed: %v\n", err)
		os.Exit(1)
	}

	err = db.CreateWorkflow(ctx, database.CreateWorkflowParams{
		ID:                     wfID,
		OriginalIntent:         "Work Order: " + filepath.Base(absPath),
		OriginalFile:           absPath,
		CurrentState:           "pending",
		TargetRepo:             cfg.Project.Name,
		GitBranch:              branchName,
		ContextPackagePath:     sql.NullString{},
		VerificationReportPath: sql.NullString{},
		MaxDepth:               5,
		MaxFilesChanged:        50,
		MaxDurationMins:        60,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to create workflow: %v\n", err)
		os.Exit(1)
	}

	err = db.CreateTask(ctx, database.CreateTaskParams{
		ID:            taskID,
		WorkflowID:    wfID,
		SequenceNum:   1,
		TaskType:      "execution",
		AgentType:     "opencode",
		TargetRepo:    cfg.Project.Name,
		Phase:         "scope",
		InputArtifact: absPath,
		State:         "pending",
		MaxAttempts:   2,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to create task: %v\n", err)
		os.Exit(1)
	}

	// 2. Poll until workflow is complete
	fmt.Printf("Workflow %s created. Processing phases...\n", wfID)

	for {
		// Run worker tick
		w.ProcessNextTask(ctx) // Need to export processNextTask to ProcessNextTask or handle differently

		// Check workflow state
		wf, err := db.GetWorkflow(ctx, wfID)
		if err != nil {
			fmt.Fprintf(os.Stderr, "failed to query workflow: %v\n", err)
			os.Exit(1)
		}

		if wf.CurrentState == "human_review" || wf.CurrentState == "completed" || wf.CurrentState == "failed" {
			fmt.Printf("\nExecution finished with state: %s\n", wf.CurrentState)
			break
		}

		time.Sleep(1 * time.Second)
	}
}
