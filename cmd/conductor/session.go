package main

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/ponchione/agent-conductor/internal/database"
	"github.com/spf13/cobra"
)

var sessionCmd = &cobra.Command{
	Use:   "session <session-id>",
	Short: "Show detailed status for a session",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		dbPath := filepath.Join(cfg.Project.DataDir, "db", "conductor.db")
		db, err := database.NewDB(dbPath)
		if err != nil {
			return fmt.Errorf("failed to open database: %w", err)
		}
		defer db.Close()

		ctx := context.Background()
		resolvedID, err := resolveSessionID(ctx, db, args[0])
		if err != nil {
			return err
		}

		detail, err := db.GetSessionDetail(ctx, resolvedID)
		if err != nil {
			return fmt.Errorf("failed to fetch session detail: %w", err)
		}

		fmt.Printf("=== SESSION ===\n")
		fmt.Printf("ID:             %s\n", detail.Session.ID)
		fmt.Printf("Kind:           %s\n", detail.Session.Kind)
		fmt.Printf("Project:        %s\n", detail.Session.Project)
		fmt.Printf("State:          %s\n", detail.Session.State)
		if detail.Session.SourceSpecPath.Valid {
			fmt.Printf("Source:         %s\n", detail.Session.SourceSpecPath.String)
		}
		fmt.Printf("Created At:     %s\n", detail.Session.CreatedAt)
		fmt.Printf("Updated At:     %s\n", detail.Session.UpdatedAt)
		if detail.Session.StartedAt.Valid {
			fmt.Printf("Started At:     %s\n", detail.Session.StartedAt.String)
		}
		if detail.Session.CompletedAt.Valid {
			fmt.Printf("Completed At:   %s\n", detail.Session.CompletedAt.String)
		}
		if detail.Session.ErrorMessage.Valid {
			fmt.Printf("Error:          %s\n", detail.Session.ErrorMessage.String)
		}

		fmt.Printf("\n--- PLAN RUNS ---\n")
		if len(detail.PlanRuns) == 0 {
			fmt.Println("(none)")
		} else {
			for _, run := range detail.PlanRuns {
				fmt.Printf("%s  %s", shortID(run.ID), run.SpecFile)
				if run.PostAuditWorkOrderCount.Valid {
					fmt.Printf("  orders=%d", run.PostAuditWorkOrderCount.Int64)
				} else if run.WorkOrdersGenerated.Valid {
					fmt.Printf("  orders=%d", run.WorkOrdersGenerated.Int64)
				}
				if run.PreAuditWorkOrderCount.Valid && run.PostAuditWorkOrderCount.Valid {
					fmt.Printf("  delta=%+d", run.PostAuditWorkOrderCount.Int64-run.PreAuditWorkOrderCount.Int64)
				}
				fmt.Printf("  created=%s\n", run.CreatedAt)
			}
		}

		fmt.Printf("\n--- EXECUTION RUNS ---\n")
		if len(detail.PipelineRuns) == 0 {
			fmt.Println("(none)")
		} else {
			for _, run := range detail.PipelineRuns {
				workOrderType := "-"
				if run.WorkOrderType.Valid {
					workOrderType = run.WorkOrderType.String
				}
				fmt.Printf("%s  workflow=%s  type=%s  created=%s\n",
					shortID(run.ID), shortID(run.WorkflowID), workOrderType, run.CreatedAt)
			}
		}

		fmt.Printf("\n--- ARTIFACTS ---\n")
		if len(detail.Artifacts) == 0 {
			fmt.Println("(none)")
		} else {
			for _, artifact := range detail.Artifacts {
				fmt.Printf("%s  %s\n", artifact.ArtifactType, artifact.Path)
			}
		}

		return nil
	},
}

func resolveSessionID(ctx context.Context, db *database.DB, input string) (string, error) {
	if len(input) >= 32 {
		return input, nil
	}

	ids, err := db.FindSessionsByPrefix(ctx, input)
	if err != nil {
		return "", fmt.Errorf("failed to resolve session id: %w", err)
	}
	if len(ids) == 0 {
		return "", fmt.Errorf("no session found with prefix %q", input)
	}
	if len(ids) > 1 {
		return "", fmt.Errorf("session prefix %q is ambiguous (%d matches)", input, len(ids))
	}
	return ids[0], nil
}

func shortID(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	return id
}
