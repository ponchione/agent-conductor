package worker

import (
	"context"
	"database/sql"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/ponchione/agent-conductor/internal/database"
	pipelineerrors "github.com/ponchione/agent-conductor/internal/errors"
	"github.com/ponchione/agent-conductor/internal/executor"
	"github.com/ponchione/agent-conductor/internal/queue"
	"github.com/ponchione/agent-conductor/internal/verify"
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

	// Create and checkout the feature branch before running the executor.
	exists, err := w.git.BranchExists(w.cfg.Project.Path, wf.GitBranch)
	if err != nil {
		return pipelineerrors.Fatalf("build", task.WorkflowID, task.ID,
			"failed to check branch %s: %w", wf.GitBranch, err)
	}
	if !exists {
		if err := w.git.CreateBranch(w.cfg.Project.Path, wf.GitBranch, w.cfg.Git.BaseBranch); err != nil {
			return pipelineerrors.Fatalf("build", task.WorkflowID, task.ID,
				"failed to create branch %s: %w", wf.GitBranch, err)
		}
	}
	if err := w.git.CheckoutBranch(w.cfg.Project.Path, wf.GitBranch); err != nil {
		return pipelineerrors.Fatalf("build", task.WorkflowID, task.ID,
			"failed to checkout branch %s: %w", wf.GitBranch, err)
	}
	slog.Info("Branch ready", "branch", wf.GitBranch, "base", w.cfg.Git.BaseBranch)

	// Capture CLAUDE.md contents for audit logging.
	var claudeMDContent sql.NullString
	claudeMDPath := filepath.Join(w.cfg.Project.Path, "CLAUDE.md")
	if data, readErr := os.ReadFile(claudeMDPath); readErr == nil {
		claudeMDContent = sql.NullString{String: string(data), Valid: true}
	} else if !os.IsNotExist(readErr) {
		slog.Warn("Failed to read CLAUDE.md for audit", "path", claudeMDPath, "error", readErr)
	}

	workOrderPath := wf.OriginalFile

	prompt := w.prompts.Build
	if w.prompts.Bootstrap != "" {
		ctxData, readErr := os.ReadFile(contextPath)
		if readErr == nil {
			var cp struct {
				WorkOrder struct {
					Type string `json:"type"`
				} `json:"work_order"`
			}
			if json.Unmarshal(ctxData, &cp) == nil && cp.WorkOrder.Type == "bootstrap" {
				prompt = w.prompts.Bootstrap
			}
		}
	}

	runCfg := executor.RunConfig{
		RepoPath:   w.cfg.Project.Path,
		InputFiles: []string{workOrderPath, contextPath},
		Prompt:     prompt,
		Timeout:    time.Duration(wf.MaxDurationMins) * time.Minute,
		LogDir:     filepath.Join(w.cfg.Project.DataDir, "logs", task.ID),
	}

	result, err := w.runner.Run(ctx, runCfg)
	if err != nil {
		return pipelineerrors.Fatalf("build", task.WorkflowID, task.ID, "build execution failed: %w", err)
	}

	w.registerWorkflowArtifact(ctx, task.WorkflowID, task.ID, database.ArtifactTypeBuildStdout, result.StdoutPath, map[string]any{
		"phase": "build",
	})
	w.registerWorkflowArtifact(ctx, task.WorkflowID, task.ID, database.ArtifactTypeBuildStderr, result.StderrPath, map[string]any{
		"phase": "build",
	})
	buildEvidencePath, evidenceErr := w.writeBuildValidationEvidence(task.WorkflowID)
	if evidenceErr != nil {
		slog.Warn("Failed to write build validation evidence", "error", evidenceErr)
	} else {
		w.registerWorkflowArtifact(ctx, task.WorkflowID, task.ID, database.ArtifactTypeBuildValidationEvidence, buildEvidencePath, map[string]any{
			"phase": "build",
		})
	}

	if !result.Success {
		return pipelineerrors.NeedsHumanf("build", task.WorkflowID, task.ID,
			"build agent exited with code %d", result.ExitCode)
	}

	if err := w.git.CheckoutBranch(w.cfg.Project.Path, wf.GitBranch); err != nil {
		return pipelineerrors.Fatalf("build", task.WorkflowID, task.ID,
			"failed to ensure branch %s after build: %w", wf.GitBranch, err)
	}

	baseBranch := w.cfg.Git.BaseBranch
	changedFiles, changedFilesErr := w.git.GetChangedFilesAgainstBase(w.cfg.Project.Path, baseBranch)

	if len(w.cfg.Safety.ForbiddenPaths) > 0 {
		if changedFilesErr != nil {
			return pipelineerrors.Fatalf("build", task.WorkflowID, task.ID,
				"cannot verify forbidden paths: git changed files failed: %w", changedFilesErr)
		}
		for _, changed := range changedFiles {
			cleanChanged := filepath.Clean(changed)
			for _, forbidden := range w.cfg.Safety.ForbiddenPaths {
				cleanForbidden := filepath.Clean(forbidden)
				if cleanChanged == cleanForbidden || strings.HasPrefix(cleanChanged, cleanForbidden+"/") {
					return pipelineerrors.Fatalf("build", task.WorkflowID, task.ID, "forbidden path violation: build agent modified %q which is listed in safety.forbidden_paths", changed)
				}
			}
		}
	}

	changedFilesCount := 0
	if changedFilesErr == nil {
		changedFilesCount = len(changedFiles)
	}

	// Persist budget consumption before advancing workflow state.
	if err := w.db.UpdateWorkflowBudget(ctx, database.UpdateWorkflowBudgetParams{
		CurrentDepth: wf.CurrentDepth + 1,
		FilesChanged: wf.FilesChanged + int64(changedFilesCount),
		ID:           task.WorkflowID,
	}); err != nil {
		return pipelineerrors.Fatalf("build", task.WorkflowID, task.ID,
			"failed to update workflow budget: %w", err)
	}

	// Intentionally non-transactional: state update and metrics are written
	// separately. If the state update fails we return fatal; metric writes
	// are best-effort and logged on failure.
	if err := w.db.TransitionWorkflowState(ctx, task.WorkflowID, "build_complete"); err != nil {
		return pipelineerrors.Fatalf("build", task.WorkflowID, task.ID,
			"failed to update workflow state to build_complete: %w", err)
	}

	w.db.LogEvent(task.WorkflowID, task.ID, "build_completed", map[string]any{
		"exit_code": result.ExitCode,
		"duration":  result.Duration.String(),
	})

	var toolCallsJSON string
	if len(result.ToolCalls) > 0 {
		if b, err := json.Marshal(result.ToolCalls); err == nil {
			toolCallsJSON = string(b)
		}
	}

	if err := w.db.UpdatePipelineRunBuild(ctx, database.UpdatePipelineRunBuildParams{
		WorkflowID:           task.WorkflowID,
		BuildStartedAt:       sql.NullString{String: buildStartedAt.UTC().Format(time.RFC3339), Valid: true},
		BuildCompletedAt:     sql.NullString{String: time.Now().UTC().Format(time.RFC3339), Valid: true},
		BuildFilesChanged:    sql.NullInt64{Int64: int64(changedFilesCount), Valid: true},
		BuildTokensIn:        sql.NullInt64{Int64: int64(result.TokensIn), Valid: result.TokensIn > 0},
		BuildTokensOut:       sql.NullInt64{Int64: int64(result.TokensOut), Valid: result.TokensOut > 0},
		BuildModel:           sql.NullString{String: result.Model, Valid: result.Model != ""},
		BuildCostUsd:         sql.NullFloat64{Float64: result.CostUSD, Valid: result.CostUSD > 0},
		BuildSessionID:       sql.NullString{String: result.SessionID, Valid: result.SessionID != ""},
		BuildToolCalls:       sql.NullString{String: toolCallsJSON, Valid: toolCallsJSON != ""},
		BuildClaudeMdContent: claudeMDContent,
	}); err != nil {
		slog.Warn("Failed to update pipeline run build metrics", "error", err)
	}

	slog.Info("Build phase complete",
		"duration", result.Duration.String(),
		"files_changed", changedFilesCount,
		"tokens_in", result.TokensIn,
		"tokens_out", result.TokensOut,
		"cost_usd", result.CostUSD,
		"model", result.Model,
		"tool_calls", len(result.ToolCalls),
	)

	if err := w.q.CompleteTask(task.ID, &queue.TaskResult{
		ExitCode:  result.ExitCode,
		StdoutLog: result.StdoutPath,
		StderrLog: result.StderrPath,
	}); err != nil {
		return pipelineerrors.Fatalf("build", task.WorkflowID, task.ID,
			"failed to complete build task: %w", err)
	}

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

func (w *Worker) writeBuildValidationEvidence(workflowID string) (string, error) {
	evidencePath := filepath.Join(w.cfg.Project.DataDir, "artifacts", "build-evidence", workflowID+"-build-evidence.json")
	evidence := &verify.ValidationEvidence{
		Phase:       "build",
		Commands:    []verify.ValidationEvidenceEntry{},
		SmokeChecks: []verify.ValidationEvidenceEntry{},
	}
	if err := verify.WriteValidationEvidence(evidencePath, evidence); err != nil {
		return "", err
	}
	return evidencePath, nil
}
