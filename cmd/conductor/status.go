package main

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/ponchione/agent-conductor/internal/database"
	"github.com/spf13/cobra"
)

var statusCmd = &cobra.Command{
	Use:   "status <workflow-id>",
	Short: "Show the current status of a workflow",
	Long: `Show detailed status for a workflow.

The workflow ID can be a full UUID or a unique prefix (e.g. the 8-char prefix
shown by 'conductor list').`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		dbPath := filepath.Join(cfg.Project.DataDir, "db", "conductor.db")
		db, err := database.NewDB(dbPath)
		if err != nil {
			return fmt.Errorf("failed to open database: %w", err)
		}
		defer db.Close()

		ctx := context.Background()
		resolvedID, err := resolveWorkflowID(ctx, db, args[0])
		if err != nil {
			return err
		}
		wf, err := db.GetWorkflow(ctx, resolvedID)
		if err != nil {
			return fmt.Errorf("failed to fetch workflow: %w", err)
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

		return nil
	},
}
