package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/ponchione/agent-conductor/internal/config"
	"github.com/ponchione/agent-conductor/internal/database"
)

func runStatus(args []string) {
	if len(args) == 0 {
		fmt.Fprintf(os.Stderr, "Usage: conductor status <workflow-id>\n")
		os.Exit(1)
	}
	workflowID := args[0]

	cfg, err := config.Load("project.yaml")
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to load config: %v\n", err)
		os.Exit(1)
	}

	dbPath := filepath.Join(cfg.Project.DataDir, "conductor.db")
	db, err := database.NewDB(dbPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to open database: %v\n", err)
		os.Exit(1)
	}
	defer db.Close()

	ctx := context.Background()
	wf, err := db.GetWorkflow(ctx, workflowID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to fetch workflow: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("=== WORKFLOW STATUS ===\n")
	fmt.Printf("ID:             %s\n", wf.ID)
	fmt.Printf("Intent:         %s\n", wf.OriginalIntent)
	fmt.Printf("State/Phase:    %s\n", wf.CurrentState)
	fmt.Printf("Repository:     %s\n", wf.TargetRepo)
	fmt.Printf("Branch:         %s\n\n", wf.GitBranch)

	fmt.Printf("--- TIMINGS ---\n")
	fmt.Printf("Created At:     %s\n", wf.CreatedAt)
	fmt.Printf("Updated At:     %s\n", wf.UpdatedAt)
	if wf.StartedAt.Valid {
		fmt.Printf("Started At:     %s\n", wf.StartedAt.String)
	}
	if wf.CompletedAt.Valid {
		fmt.Printf("Completed At:   %s\n", wf.CompletedAt.String)
	}
	fmt.Printf("\n--- ARTIFACTS ---\n")
	fmt.Printf("Work Order:     %s\n", wf.OriginalFile)

	if wf.ContextPackagePath.Valid {
		fmt.Printf("Context Pkg:    %s\n", wf.ContextPackagePath.String)
	} else {
		fmt.Printf("Context Pkg:    (Pending)\n")
	}

	if wf.VerificationReportPath.Valid {
		fmt.Printf("Verify Report:  %s\n", wf.VerificationReportPath.String)
	} else {
		fmt.Printf("Verify Report:  (Pending)\n")
	}

	if wf.ErrorMessage.Valid {
		fmt.Printf("\n--- ERRORS ---\n")
		fmt.Printf("%s\n", wf.ErrorMessage.String)
	}
}
