package gate

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/ponchione/agent-conductor/internal/database"
	"github.com/ponchione/agent-conductor/internal/git"
)

// Approve merges the workflow's branch into the configured base branch and transitions the workflow
// from human_review to completed. Returns an error if the workflow is not in
// the human_review state or if the merge fails.
func Approve(ctx context.Context, db *database.DB, gitMgr *git.GitManager, workflowID, repoPath, baseBranch string) error {
	wf, err := db.GetWorkflow(ctx, workflowID)
	if err != nil {
		return fmt.Errorf("workflow not found: %w", err)
	}

	if wf.CurrentState != "human_review" {
		return fmt.Errorf("workflow %s is in state %q, expected human_review", workflowID, wf.CurrentState)
	}

	if err := gitMgr.MergeBranch(repoPath, baseBranch, wf.GitBranch); err != nil {
		return fmt.Errorf("merge branch %s into %s: %w", wf.GitBranch, baseBranch, err)
	}

	if err := gitMgr.DeleteBranch(repoPath, wf.GitBranch); err != nil {
		slog.Warn("Failed to delete conducted branch", "branch", wf.GitBranch, "error", err)
	}

	if err := db.TransitionWorkflowState(ctx, workflowID, "completed"); err != nil {
		return fmt.Errorf("failed to approve workflow: %w", err)
	}

	if err := db.UpdatePipelineRunHumanResult(ctx, database.UpdatePipelineRunHumanResultParams{
		WorkflowID:  workflowID,
		HumanResult: database.String("approved"),
	}); err != nil {
		slog.Warn("Failed to update pipeline run human result", "workflow", workflowID, "error", err)
	}

	db.LogEvent(workflowID, "", "human_approved", map[string]any{
		"workflow_id": workflowID,
		"merged_into": baseBranch,
		"branch":      wf.GitBranch,
	})

	slog.Info("Workflow approved and merged", "workflow", workflowID, "branch", wf.GitBranch)
	return nil
}

// Reject transitions a workflow in human_review to failed, storing the reason.
// Returns an error if the workflow is not in the human_review state.
func Reject(ctx context.Context, db *database.DB, workflowID, reason string) error {
	wf, err := db.GetWorkflow(ctx, workflowID)
	if err != nil {
		return fmt.Errorf("workflow not found: %w", err)
	}

	if wf.CurrentState != "human_review" {
		return fmt.Errorf("workflow %s is in state %q, expected human_review", workflowID, wf.CurrentState)
	}

	if reason == "" {
		reason = "rejected by human reviewer"
	}

	if err := db.TransitionWorkflowState(ctx, workflowID, "failed"); err != nil {
		return fmt.Errorf("failed to reject workflow: %w", err)
	}

	if err := db.UpdatePipelineRunHumanResult(ctx, database.UpdatePipelineRunHumanResultParams{
		WorkflowID:  workflowID,
		HumanResult: database.String("rejected"),
	}); err != nil {
		slog.Warn("Failed to update pipeline run human result", "workflow", workflowID, "error", err)
	}

	db.LogEvent(workflowID, "", "human_rejected", map[string]any{
		"workflow_id": workflowID,
		"reason":      reason,
	})

	slog.Info("Workflow rejected", "workflow", workflowID, "reason", reason)
	return nil
}
