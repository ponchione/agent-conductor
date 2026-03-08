package main

import (
	stdctx "context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/google/uuid"
	"github.com/ponchione/agent-conductor/internal/database"
	"github.com/ponchione/agent-conductor/internal/gate"
	"github.com/ponchione/agent-conductor/internal/git"
	"github.com/ponchione/agent-conductor/internal/rag"
	"github.com/spf13/cobra"
)

func openDB() (*database.DB, error) {
	dbPath := filepath.Join(cfg.Project.DataDir, "db", "conductor.db")
	db, err := database.NewDB(dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}
	return db, nil
}

var approveCmd = &cobra.Command{
	Use:   "approve <workflow-id>",
	Short: "Approve a workflow in human_review and merge its branch",
	Long: `Approve a workflow that is waiting in human_review state.

The workflow ID must be a full UUID (e.g. from 'conductor list').`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		workflowID := args[0]
		if _, err := uuid.Parse(workflowID); err != nil {
			return fmt.Errorf("invalid workflow ID %q: expected a UUID (e.g. from 'conductor list')", workflowID)
		}

		db, err := openDB()
		if err != nil {
			return err
		}
		defer db.Close()

		ctx := stdctx.Background()
		gitMgr := git.New(cfg)
		if err := gate.Approve(ctx, db, gitMgr, workflowID, cfg.Project.Path, cfg.Git.BaseBranch); err != nil {
			return fmt.Errorf("approve failed: %w", err)
		}
		fmt.Printf("Workflow %s approved and merged into %s.\n", workflowID, cfg.Git.BaseBranch)

		archiveWorkOrder(ctx, db, workflowID, cfg.Project.DataDir)

		if cfg.Index.AutoReindex {
			fmt.Println("Re-indexing RAG store...")
			ctx2 := stdctx.Background()
			store, embedder, describer, err := buildRAGStack(ctx2, cfg)
			if err != nil {
				slog.Warn("Auto re-index failed: could not build RAG stack", "error", err)
				fmt.Printf("Warning: auto re-index failed: %v. Run 'conductor index' manually.\n", err)
				return nil
			}
			defer store.Close()

			if err := rag.IndexRepo(ctx2, cfg, store, embedder, describer, rag.IndexOpts{}); err != nil {
				slog.Warn("Auto re-index failed", "error", err)
				fmt.Printf("Warning: auto re-index failed: %v. Run 'conductor index' manually.\n", err)
				return nil
			}
			slog.Info("RAG index updated after approve")
			fmt.Println("RAG index updated.")
		}

		return nil
	},
}

var rejectCmd = &cobra.Command{
	Use:   "reject <workflow-id> [reason]",
	Short: "Reject a workflow in human_review",
	Long: `Reject a workflow that is waiting in human_review state.

The workflow ID must be a full UUID (e.g. from 'conductor list').
An optional reason can be provided as the second argument.`,
	Args: cobra.RangeArgs(1, 2),
	RunE: func(cmd *cobra.Command, args []string) error {
		workflowID := args[0]
		if _, err := uuid.Parse(workflowID); err != nil {
			return fmt.Errorf("invalid workflow ID %q: expected a UUID (e.g. from 'conductor list')", workflowID)
		}

		reason := ""
		if len(args) > 1 {
			reason = args[1]
		}

		db, err := openDB()
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

// archiveWorkOrder is fire-and-forget; errors are logged, not returned.
func archiveWorkOrder(ctx stdctx.Context, db *database.DB, workflowID, dataDir string) {
	wf, err := db.GetWorkflow(ctx, workflowID)
	if err != nil {
		slog.Warn("archive: could not fetch workflow", "id", workflowID, "error", err)
		return
	}

	content, err := os.ReadFile(wf.OriginalFile)
	if err != nil {
		slog.Warn("archive: could not read work order file", "path", wf.OriginalFile, "error", err)
		return
	}

	// File copy to artifacts directory.
	destPath := filepath.Join(dataDir, "artifacts", "work-orders", workflowID+".yaml")
	if err := os.WriteFile(destPath, content, 0644); err != nil {
		slog.Warn("archive: could not copy work order file", "dest", destPath, "error", err)
	}

	// Store content in DB.
	if err := db.UpdatePipelineRunWorkOrderContent(ctx, database.UpdatePipelineRunWorkOrderContentParams{
		WorkOrderContent: database.String(string(content)),
		WorkflowID:       workflowID,
	}); err != nil {
		slog.Warn("archive: could not store work order content in DB", "error", err)
	}
}
