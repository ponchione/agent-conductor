package worker

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/google/uuid"
	"github.com/ponchione/agent-conductor/internal/database"
	"github.com/ponchione/agent-conductor/internal/llm"
	"github.com/ponchione/agent-conductor/internal/models"
	"github.com/ponchione/agent-conductor/internal/queue"
	"github.com/ponchione/agent-conductor/internal/templates"
	"gopkg.in/yaml.v3"
)

func (w *Worker) runScope(ctx context.Context, task *database.Task) error {
	slog.Info("Starting Scope Phase", "task", task.ID)
	w.db.LogEvent(task.WorkflowID, task.ID, "scope_started", nil)

	scopeStartedAt := time.Now()

	// 1. Read Work Order
	woContent, err := os.ReadFile(task.InputArtifact)
	if err != nil {
		return fmt.Errorf("failed to read work order: %w", err)
	}

	var wo models.WorkOrder
	if err := yaml.Unmarshal(woContent, &wo); err != nil {
		return fmt.Errorf("failed to parse work order: %w", err)
	}

	// 2. Assemble Context
	contextBlock, err := w.assembler.Assemble(ctx, &wo)
	if err != nil {
		return fmt.Errorf("context assembly failed: %w", err)
	}

	// 3. Call LLM with retry
	maxAttempts := int(task.MaxAttempts)
	if maxAttempts < 1 {
		maxAttempts = 1
	}

	var pkg models.ContextPackage
	var lastErr error
	var lastUsage llm.Usage
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		if attempt > 1 {
			slog.Warn("Retrying scope LLM call", "task", task.ID, "attempt", attempt)
			w.db.LogEvent(task.WorkflowID, task.ID, "scope_retry", map[string]any{
				"attempt": attempt,
				"error":   lastErr.Error(),
			})
		}

		jsonStr, usage, err := w.llm.Complete(ctx, templates.ScopePrompt, contextBlock)
		if err != nil {
			lastErr = fmt.Errorf("llm completion failed (attempt %d): %w", attempt, err)
			continue
		}

		cleanedJSON := cleanLLMResponse(jsonStr)

		if err := json.Unmarshal([]byte(cleanedJSON), &pkg); err != nil {
			lastErr = fmt.Errorf("invalid json from llm (attempt %d): %w", attempt, err)
			continue
		}

		lastUsage = usage
		lastErr = nil
		break
	}

	if lastErr != nil {
		return lastErr
	}

	// 4. Write Context Package to Disk
	pkgDir := filepath.Join(w.cfg.Project.DataDir, "artifacts", "context-packages")
	if err := os.MkdirAll(pkgDir, 0755); err != nil {
		return fmt.Errorf("failed to create context-packages dir: %w", err)
	}
	pkgPath := filepath.Join(pkgDir, task.WorkflowID+"-context-package.json")
	pkgData, _ := json.MarshalIndent(pkg, "", "  ")
	if err := os.WriteFile(pkgPath, pkgData, 0644); err != nil {
		return fmt.Errorf("failed to write context package: %w", err)
	}

	// 5. Record scope metrics in pipeline_run
	if err := w.db.UpdatePipelineRunScope(ctx, database.UpdatePipelineRunScopeParams{
		WorkflowID:       task.WorkflowID,
		ScopeStartedAt:   sql.NullString{String: scopeStartedAt.UTC().Format(time.RFC3339), Valid: true},
		ScopeCompletedAt: sql.NullString{String: time.Now().UTC().Format(time.RFC3339), Valid: true},
		ScopeTokensIn:    sql.NullInt64{Int64: int64(lastUsage.PromptTokens), Valid: true},
		ScopeTokensOut:   sql.NullInt64{Int64: int64(lastUsage.CompletionTokens), Valid: true},
		ScopeModel:       sql.NullString{String: w.cfg.LocalModel.ModelName, Valid: true},
	}); err != nil {
		slog.Warn("Failed to update pipeline run scope metrics", "error", err)
	}

	// 6. Update Workflow
	if err := w.db.UpdateWorkflowContext(ctx, database.UpdateWorkflowContextParams{
		ID:                 task.WorkflowID,
		ContextPackagePath: sql.NullString{String: pkgPath, Valid: true},
	}); err != nil {
		return fmt.Errorf("failed to update workflow context path: %w", err)
	}

	if err := w.db.UpdateWorkflowState(ctx, database.UpdateWorkflowStateParams{
		ID:           task.WorkflowID,
		CurrentState: "scope_complete",
	}); err != nil {
		slog.Error("Failed to update workflow state to scope_complete", "workflow", task.WorkflowID, "error", err)
	}

	w.db.LogEvent(task.WorkflowID, task.ID, "scope_completed", map[string]any{
		"context_package_path": pkgPath,
		"files_to_modify":      len(pkg.FilesToModify),
	})

	// Add Observability Printf
	fmt.Printf("\n--- SCOPE PHASE COMPLETE ---\n")
	fmt.Printf("Summary: %s\n", pkg.Summary)
	fmt.Printf("Files to modify: %d\n", len(pkg.FilesToModify))
	fmt.Printf("Files to reference: %d\n", len(pkg.FilesToReference))
	fmt.Printf("----------------------------\n\n")

	// 6. Complete Task
	w.q.CompleteTask(task.ID, &queue.TaskResult{
		ExitCode:  0,
		StdoutLog: "Scope completed successfully",
	})

	// 7. Queue Build Task
	buildTaskID := uuid.New().String()
	if err := w.db.CreateTask(ctx, database.CreateTaskParams{
		ID:            buildTaskID,
		WorkflowID:    task.WorkflowID,
		SequenceNum:   task.SequenceNum + 1,
		TaskType:      "execution",
		AgentType:     "opencode",
		TargetRepo:    task.TargetRepo,
		Phase:         "build",
		InputArtifact: pkgPath,
		State:         "pending",
		MaxAttempts:   1, // No retries for build
	}); err != nil {
		return fmt.Errorf("failed to create build task: %w", err)
	}

	return nil
}
