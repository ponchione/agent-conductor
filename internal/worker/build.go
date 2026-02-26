package worker

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/ponchione/agent-conductor/internal/database"
	"github.com/ponchione/agent-conductor/internal/executor"
	"github.com/ponchione/agent-conductor/internal/queue"
	"github.com/ponchione/agent-conductor/internal/templates"
)

func (w *Worker) runBuild(ctx context.Context, task *database.Task) error {
	slog.Info("Starting Build Phase", "task", task.ID)
	w.db.LogEvent(task.WorkflowID, task.ID, "build_started", nil)

	buildStartedAt := time.Now()

	// 1. Get Workflow
	wf, err := w.db.GetWorkflow(ctx, task.WorkflowID)
	if err != nil {
		return fmt.Errorf("workflow missing: %w", err)
	}

	// 2. Validate context package path
	contextPath := wf.ContextPackagePath.String
	if !wf.ContextPackagePath.Valid {
		return fmt.Errorf("context package path is missing")
	}

	workOrderPath := wf.OriginalFile

	// 3. Execute OpenCode
	runCfg := executor.RunConfig{
		RepoPath:   w.cfg.Project.Path,
		Agent:      "build",
		InputFiles: []string{workOrderPath, contextPath},
		Prompt:     templates.BuildPrompt,
		Title:      fmt.Sprintf("Build Task %s", task.ID),
		Timeout:    time.Duration(wf.MaxDurationMins) * time.Minute,
		LogDir:     filepath.Join(w.cfg.Project.DataDir, "logs", task.ID),
	}

	result, err := w.runner.Run(ctx, runCfg)
	if err != nil {
		return fmt.Errorf("build execution failed: %w", err)
	}

	if !result.Success {
		return fmt.Errorf("build failed with exit code %d", result.ExitCode)
	}

	// 4a. Commit changes made by the build agent
	hasChanges, err := w.git.HasUncommittedChanges(w.cfg.Project.Path)
	if err != nil {
		slog.Warn("Could not check for uncommitted changes", "error", err)
	} else if hasChanges {
		commitMsg := fmt.Sprintf("Build phase implementation [%s]", task.ID)
		if err := w.git.Commit(w.cfg.Project.Path, commitMsg); err != nil {
			return fmt.Errorf("failed to commit build changes: %w", err)
		}
	} else {
		slog.Warn("No changes to commit after build phase", "task", task.ID)
	}

	// 4b. Forbidden path check — fail if build agent touched a protected file
	if len(w.cfg.Safety.ForbiddenPaths) > 0 {
		changedFiles, err := w.git.GetChangedFilesBetween(w.cfg.Project.Path, "main", wf.GitBranch)
		if err != nil {
			slog.Warn("Could not enumerate changed files for forbidden path check", "error", err)
		} else {
			for _, changed := range changedFiles {
				for _, forbidden := range w.cfg.Safety.ForbiddenPaths {
					if strings.EqualFold(changed, forbidden) || strings.HasSuffix(changed, "/"+forbidden) {
						return fmt.Errorf("forbidden path violation: build agent modified %q which is listed in safety.forbidden_paths", changed)
					}
				}
			}
		}
	}

	// 5. Update Workflow
	if err := w.db.UpdateWorkflowState(ctx, database.UpdateWorkflowStateParams{
		ID:           task.WorkflowID,
		CurrentState: "build_complete",
	}); err != nil {
		slog.Error("Failed to update workflow state to build_complete", "workflow", task.WorkflowID, "error", err)
	}

	w.db.LogEvent(task.WorkflowID, task.ID, "build_completed", map[string]any{
		"exit_code": result.ExitCode,
		"duration":  result.Duration.String(),
	})

	// Add Observability Printf
	// We need to count actually changed files to print, we already fetched it potentially above
	// Let's re-fetch or use if we have it
	changedFilesCount := 0
	changedFiles, err := w.git.GetChangedFilesBetween(w.cfg.Project.Path, "main", wf.GitBranch)
	if err == nil {
		changedFilesCount = len(changedFiles)
	}

	if err := w.db.UpdatePipelineRunBuild(ctx, database.UpdatePipelineRunBuildParams{
		WorkflowID:        task.WorkflowID,
		BuildStartedAt:    sql.NullString{String: buildStartedAt.UTC().Format(time.RFC3339), Valid: true},
		BuildCompletedAt:  sql.NullString{String: time.Now().UTC().Format(time.RFC3339), Valid: true},
		BuildFilesChanged: sql.NullInt64{Int64: int64(changedFilesCount), Valid: true},
	}); err != nil {
		slog.Warn("Failed to update pipeline run build metrics", "error", err)
	}

	fmt.Printf("\n--- BUILD PHASE COMPLETE ---\n")
	fmt.Printf("Duration: %s\n", result.Duration.String())
	fmt.Printf("Files Changed: %d\n", changedFilesCount)
	fmt.Printf("----------------------------\n\n")

	// 6. Complete Task
	w.q.CompleteTask(task.ID, &queue.TaskResult{
		ExitCode:  result.ExitCode,
		StdoutLog: result.StdoutPath,
		StderrLog: result.StderrPath,
	})

	// 7. Queue Verify Task
	verifyTaskID := uuid.New().String()
	if err := w.db.CreateTask(ctx, database.CreateTaskParams{
		ID:            verifyTaskID,
		WorkflowID:    task.WorkflowID,
		SequenceNum:   task.SequenceNum + 1,
		TaskType:      "verification",
		AgentType:     "opencode",
		TargetRepo:    task.TargetRepo,
		Phase:         "verify",
		InputArtifact: contextPath,
		State:         "pending",
		MaxAttempts:   2,
	}); err != nil {
		return fmt.Errorf("failed to create verify task: %w", err)
	}

	return nil
}
