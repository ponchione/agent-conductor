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

var sessionsCmd = &cobra.Command{
	Use:   "sessions",
	Short: "List sessions and their current state",
	Args:  cobra.NoArgs,
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

		rows, err := db.ListSessions(context.Background(), state, limit)
		if err != nil {
			return fmt.Errorf("failed to list sessions: %w", err)
		}
		if len(rows) == 0 {
			fmt.Println("No sessions found.")
			return nil
		}

		w := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', 0)
		fmt.Fprintln(w, "ID\tKIND\tSTATE\tPLAN\tRUN\tLATEST\tCREATED")
		for _, row := range rows {
			id := row.ID
			if len(id) > 8 {
				id = id[:8]
			}
			created := row.CreatedAt
			if len(created) > 16 {
				created = created[:16]
			}
			latest := "-"
			if row.LatestWorkflowState.Valid {
				latest = row.LatestWorkflowState.String
			}
			fmt.Fprintf(w, "%s\t%s\t%s\t%d\t%d\t%s\t%s\n",
				id, row.Kind, row.State, row.PlanRunsCount, row.PipelineRunsCount, latest, created)
		}
		if err := w.Flush(); err != nil {
			return fmt.Errorf("flush output: %w", err)
		}
		return nil
	},
}

func init() {
	sessionsCmd.Flags().Bool("all", false, "Show all sessions instead of the last 10")
	sessionsCmd.Flags().String("state", "", "Filter by state (running, awaiting_review, completed, failed, partial)")
}
