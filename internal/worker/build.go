package worker

import (
	"context"
	"database/sql"
	"log/slog"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/ponchione/agent-conductor/internal/database"
	pipelineerrors "github.com/ponchione/agent-conductor/internal/errors"
	"github.com/ponchione/agent-conductor/internal/executor"
	"github.com/ponchione/agent-conductor/internal/queue"
)

func (w *Worker) runBuild(ctx context.Context, task *database.Task) error {
	slog.Info("Starting Build Phase", "task", task.ID)
	w.db.LogEvent(task.WorkflowID, task.ID, "build_started", nil)

	buildStartedAt := time.Now()

	wf, err := w.db.GetWorkflow(ctx, task.WorkflowID)
	if err != nil {
		return pipelineerrors.Fatalf("build", task.WorkflowID, task.ID, "workflow missing: %w", err)
	}

	contextPath := wf.ContextPackagePath.String
	if !wf.ContextPackagePath.Valid {
		return pipelineerrors.Fatalf("build", task.WorkflowID, task.ID, "context package path is missing")
	}

	workOrderPath := wf.OriginalFile

	runCfg := executor.RunConfig{
		RepoPath:   w.cfg.Project.Path,
		InputFiles: []string{workOrderPath, contextPath},
		Prompt:     w.prompts.Build,
		Timeout:    time.Duration(wf.MaxDurationMins) * time.Minute,
		LogDir:     filepath.Join(w.cfg.Project.DataDir, "logs", task.ID),
	}

	result, err := w.runner.Run(ctx, runCfg)
	if err != nil {
		return pipelineerrors.Fatalf("build", task.WorkflowID, task.ID, "build execution failed: %w", err)
	}

	if !result.Success {
		return pipelineerrors.NeedsHumanf("build", task.WorkflowID, task.ID,
			"build agent exited with code %d", result.ExitCode)
	}

	changedFiles, changedFilesErr := w.git.GetChangedFilesBetween(w.cfg.Project.Path, "main", wf.GitBranch)

	if len(w.cfg.Safety.ForbiddenPaths) > 0 {
		if changedFilesErr != nil {
			slog.Warn("Could not enumerate changed files for forbidden path check", "error", changedFilesErr)
		} else {
			for _, changed := range changedFiles {
				for _, forbidden := range w.cfg.Safety.ForbiddenPaths {
					if strings.EqualFold(changed, forbidden) || strings.HasSuffix(changed, "/"+forbidden) {
						return pipelineerrors.Fatalf("build", task.WorkflowID, task.ID, "forbidden path violation: build agent modified %q which is listed in safety.forbidden_paths", changed)
					}
				}
			}
		}
	}

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

	changedFilesCount := 0
	if changedFilesErr == nil {
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

	slog.Info("Build phase complete",
		"duration", result.Duration.String(),
		"files_changed", changedFilesCount,
	)

	w.q.CompleteTask(task.ID, &queue.TaskResult{
		ExitCode:  result.ExitCode,
		StdoutLog: result.StdoutPath,
		StderrLog: result.StderrPath,
	})

	verifyTaskID := uuid.New().String()
	if err := w.db.CreateTask(ctx, database.CreateTaskParams{
		ID:            verifyTaskID,
		WorkflowID:    task.WorkflowID,
		SequenceNum:   task.SequenceNum + 1,
		TaskType:      "verification",
		AgentType:     "claude-code",
		TargetRepo:    task.TargetRepo,
		Phase:         "verify",
		InputArtifact: contextPath,
		State:         "pending",
		MaxAttempts:   2,
	}); err != nil {
		return pipelineerrors.Fatalf("build", task.WorkflowID, task.ID, "failed to create verify task: %w", err)
	}

	return nil
}
