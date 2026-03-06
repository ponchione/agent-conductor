package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"text/tabwriter"

	"github.com/ponchione/agent-conductor/internal/database"
	"github.com/spf13/cobra"
)

var listCmd = &cobra.Command{
	Use:   "list",
	Short: "List workflows and their current state",
	Long: `Display recent workflows with ID, state, branch, and creation date.

Default shows the last 10. Use --all to show all.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		all, _ := cmd.Flags().GetBool("all")
		state, _ := cmd.Flags().GetString("state")

		dbPath := filepath.Join(cfg.Project.DataDir, "db", "conductor.db")
		db, err := database.NewDB(dbPath)
		if err != nil {
			return fmt.Errorf("failed to open database: %w", err)
		}
		defer db.Close()

		limit := 10
		if all {
			limit = 0
		}

		ctx := context.Background()
		workflows, err := db.ListWorkflows(ctx, state, limit)
		if err != nil {
			return fmt.Errorf("failed to list workflows: %w", err)
		}

		if len(workflows) == 0 {
			fmt.Println("No workflows found.")
			return nil
		}

		w := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', 0)
		fmt.Fprintln(w, "ID\tSTATE\tBRANCH\tCREATED")
		for _, wf := range workflows {
			id := wf.ID
			if len(id) > 8 {
				id = id[:8]
			}
			created := wf.CreatedAt
			if len(created) > 10 {
				created = created[:10]
			}
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", id, wf.CurrentState, wf.GitBranch, created)
		}
		if err := w.Flush(); err != nil {
			return fmt.Errorf("flush output: %w", err)
		}
		return nil
	},
}

func init() {
	listCmd.Flags().Bool("all", false, "Show all workflows instead of the last 10")
	listCmd.Flags().String("state", "", "Filter by state (pending, human_review, completed, failed)")
}
