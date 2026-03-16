package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"text/tabwriter"
	"time"

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
		if wf.StartedAt.Valid && !wf.CompletedAt.Valid {
			started, err := time.Parse("2006-01-02 15:04:05", wf.StartedAt.String)
			if err == nil {
				elapsed := time.Since(started)
				mins := int(elapsed.Minutes())
				secs := int(elapsed.Seconds()) % 60
				fmt.Printf("Elapsed:        %dm %ds\n", mins, secs)
			}
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

		pr, prErr := db.GetPipelineRunByWorkflowID(ctx, resolvedID)
		if prErr == nil {
			if pr.BuildClaudeMdContent.Valid {
				fmt.Printf("CLAUDE.md:      present (%s bytes)\n", fmtInt(int64(len(pr.BuildClaudeMdContent.String))))
			} else {
				fmt.Printf("CLAUDE.md:      (none)\n")
			}
		}

		tasks, err := db.ListTasksByWorkflow(ctx, resolvedID)
		if err != nil {
			return fmt.Errorf("failed to list tasks: %w", err)
		}

		fmt.Printf("\n--- TASKS ---\n")
		if len(tasks) == 0 {
			fmt.Printf("(no tasks)\n")
		} else {
			w := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', 0)
			fmt.Fprintln(w, "SEQ\tPHASE\tSTATE\tATTEMPTS\tSTARTED\tCOMPLETED")
			for _, t := range tasks {
				started := "-"
				if t.StartedAt.Valid {
					started = t.StartedAt.String
				}
				completed := "-"
				if t.CompletedAt.Valid {
					completed = t.CompletedAt.String
				}
				fmt.Fprintf(w, "%d\t%s\t%s\t%d/%d\t%s\t%s\n",
					t.SequenceNum, t.Phase, t.State,
					t.Attempts, t.MaxAttempts,
					started, completed)
			}
			if err := w.Flush(); err != nil {
				return fmt.Errorf("flush output: %w", err)
			}
		}

		if wf.ErrorMessage.Valid {
			fmt.Printf("\n--- ERRORS ---\n")
			fmt.Printf("%s\n", wf.ErrorMessage.String)
		}

		return nil
	},
}
