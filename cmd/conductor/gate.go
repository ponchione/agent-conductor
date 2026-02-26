package main

import (
	stdctx "context"
	"fmt"
	"path/filepath"

	"github.com/ponchione/agent-conductor/internal/config"
	"github.com/ponchione/agent-conductor/internal/database"
	"github.com/ponchione/agent-conductor/internal/gate"
	"github.com/spf13/cobra"
)

func openDB(projectPath string) (*database.DB, *config.ProjectConfig, error) {
	cfg, err := config.Load(projectPath)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to load config: %w", err)
	}
	dbPath := filepath.Join(cfg.Project.DataDir, "db", "conductor.db")
	db, err := database.NewDB(dbPath)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to open database: %w", err)
	}
	return db, cfg, nil
}

var approveCmd = &cobra.Command{
	Use:   "approve <workflow-id>",
	Short: "Approve a workflow in human_review and merge its branch",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		projectPath, _ := cmd.Flags().GetString("project")
		workflowID := args[0]

		db, _, err := openDB(projectPath)
		if err != nil {
			return err
		}
		defer db.Close()

		ctx := stdctx.Background()
		if err := gate.Approve(ctx, db, workflowID); err != nil {
			return fmt.Errorf("approve failed: %w", err)
		}
		fmt.Printf("Workflow %s approved. Merging branch...\n", workflowID)
		return nil
	},
}

var rejectCmd = &cobra.Command{
	Use:   "reject <workflow-id> [reason]",
	Short: "Reject a workflow in human_review",
	Args:  cobra.RangeArgs(1, 2),
	RunE: func(cmd *cobra.Command, args []string) error {
		projectPath, _ := cmd.Flags().GetString("project")
		workflowID := args[0]
		reason := ""
		if len(args) > 1 {
			reason = args[1]
		}

		db, _, err := openDB(projectPath)
		if err != nil {
			return err
		}
		defer db.Close()

		ctx := stdctx.Background()
		if err := gate.Reject(ctx, db, workflowID, reason); err != nil {
			return fmt.Errorf("reject failed: %w", err)
		}
		fmt.Printf("Workflow %s rejected: %s\n", workflowID, reason)
		return nil
	},
}

func init() {
	approveCmd.Flags().String("project", "project.yaml", "Path to project config file")
	rejectCmd.Flags().String("project", "project.yaml", "Path to project config file")
}
