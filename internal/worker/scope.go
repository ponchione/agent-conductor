package worker

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/ponchione/agent-conductor/internal/database"
	pipelineerrors "github.com/ponchione/agent-conductor/internal/errors"
	"github.com/ponchione/agent-conductor/internal/llm"
	"github.com/ponchione/agent-conductor/internal/models"
	"github.com/ponchione/agent-conductor/internal/queue"
	"gopkg.in/yaml.v3"
)

const stripThreshold = 0.50

type scopeValidationResult struct {
	pkg               *models.ContextPackage
	strippedPaths     []string
	reclassifiedPaths []string
	pathsChecked      int
	thresholdExceeded bool
}

// validateScopeOutput checks every path in the ContextPackage against the
// filesystem, strips non-existent files from existing-file arrays, reclassifies
// NewFiles that already exist into FilesToModify, and reports whether the
// fraction of invalid paths exceeds stripThreshold.
func validateScopeOutput(repoRoot string, pkg *models.ContextPackage) *scopeValidationResult {
	result := &scopeValidationResult{pkg: pkg}

	// Filter existing-file arrays: FilesToModify, FilesToReference, SQLFiles.
	filterFileRefs := func(refs []models.FileRef, source string) []models.FileRef {
		result.pathsChecked += len(refs)
		filtered := make([]models.FileRef, 0, len(refs))
		for _, ref := range refs {
			if _, err := os.Stat(filepath.Join(repoRoot, ref.Path)); err != nil {
				result.strippedPaths = append(result.strippedPaths, ref.Path+" ("+source+")")
				slog.Warn("Scope validation: path does not exist", "path", ref.Path, "source", source)
				continue
			}
			filtered = append(filtered, ref)
		}
		return filtered
	}

	pkg.FilesToModify = filterFileRefs(pkg.FilesToModify, "files_to_modify")
	pkg.FilesToReference = filterFileRefs(pkg.FilesToReference, "files_to_reference")
	pkg.SQLFiles = filterFileRefs(pkg.SQLFiles, "sql_files")

	// Reclassify NewFiles that already exist on disk into FilesToModify.
	keptNew := make([]models.NewFile, 0, len(pkg.NewFiles))
	for _, nf := range pkg.NewFiles {
		if _, err := os.Stat(filepath.Join(repoRoot, nf.Path)); err == nil {
			pkg.FilesToModify = append(pkg.FilesToModify, models.FileRef{
				Path:   nf.Path,
				Reason: nf.Purpose,
			})
			result.reclassifiedPaths = append(result.reclassifiedPaths, nf.Path)
			slog.Warn("Scope validation: new_file already exists, reclassified to files_to_modify", "path", nf.Path)
		} else {
			keptNew = append(keptNew, nf)
		}
	}
	pkg.NewFiles = keptNew

	// Threshold check.
	if result.pathsChecked > 0 {
		result.thresholdExceeded = float64(len(result.strippedPaths))/float64(result.pathsChecked) > stripThreshold
	}

	return result
}

// buildCorrectionSection returns a prompt section listing paths the LLM
// hallucinated so a retry can avoid them.
func buildCorrectionSection(strippedPaths []string) string {
	var b strings.Builder
	b.WriteString("\n=== CORRECTION ===\nThe following paths do not exist in the repository. Do not include them:")
	for _, p := range strippedPaths {
		b.WriteString("\n  - ")
		b.WriteString(p)
	}
	return b.String()
}

func (w *Worker) runScope(ctx context.Context, task *database.Task) error {
	slog.Info("Starting Scope Phase", "task", task.ID)
	w.db.LogEvent(task.WorkflowID, task.ID, "scope_started", nil)

	scopeStartedAt := time.Now()

	woContent, err := os.ReadFile(task.InputArtifact)
	if err != nil {
		return pipelineerrors.Fatalf("scope", task.WorkflowID, task.ID, "failed to read work order: %w", err)
	}

	var wo models.WorkOrder
	if err := yaml.Unmarshal(woContent, &wo); err != nil {
		return pipelineerrors.Fatalf("scope", task.WorkflowID, task.ID, "failed to parse work order: %w", err)
	}

	scopePrompt, err := w.assembler.AssembleScopePrompt(ctx, &wo)
	if err != nil {
		return pipelineerrors.Fatalf("scope", task.WorkflowID, task.ID, "context assembly failed: %w", err)
	}

	maxAttempts := max(int(task.MaxAttempts), 1)

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

		jsonStr, usage, err := w.llm.Complete(ctx, w.prompts.Scope, scopePrompt)
		if err != nil {
			lastErr = fmt.Errorf("llm completion failed (attempt %d): %w", attempt, err)
			continue
		}

		cleanedJSON := llm.CleanLLMResponse(jsonStr)

		if err := json.Unmarshal([]byte(cleanedJSON), &pkg); err != nil {
			lastErr = fmt.Errorf("invalid json from llm (attempt %d): %w", attempt, err)
			continue
		}

		// Path validation
		result := validateScopeOutput(w.cfg.Project.Path, &pkg)

		if result.thresholdExceeded {
			lastErr = fmt.Errorf("scope validation: %d of %d paths do not exist (exceeds %.0f%% threshold)",
				len(result.strippedPaths), result.pathsChecked, stripThreshold*100)
			scopePrompt = scopePrompt + "\n\n" + buildCorrectionSection(result.strippedPaths)
			w.db.LogEvent(task.WorkflowID, task.ID, "scope_validation_retry", map[string]any{
				"attempt":        attempt,
				"stripped_paths": result.strippedPaths,
			})
			continue
		}

		// Validation passed
		w.db.LogEvent(task.WorkflowID, task.ID, "scope_validated", map[string]any{
			"paths_checked":      result.pathsChecked,
			"paths_stripped":     len(result.strippedPaths),
			"paths_reclassified": len(result.reclassifiedPaths),
		})
		pkg = *result.pkg
		lastUsage = usage
		lastErr = nil
		break
	}

	if lastErr != nil {
		return pipelineerrors.Retryablef("scope", task.WorkflowID, task.ID,
			"llm failed after %d attempts: %w", maxAttempts, lastErr)
	}

	wf, err := w.db.GetWorkflow(ctx, task.WorkflowID)
	if err != nil {
		return pipelineerrors.Fatalf("scope", task.WorkflowID, task.ID, "failed to get workflow: %w", err)
	}

	fullPkg, err := w.assembler.Assemble(ctx, &wo, &pkg, wf.GitBranch)
	if err != nil {
		return pipelineerrors.Fatalf("scope", task.WorkflowID, task.ID, "full context assembly failed: %w", err)
	}

	pkgDir := filepath.Join(w.cfg.Project.DataDir, "artifacts", "context-packages")
	if err := os.MkdirAll(pkgDir, 0755); err != nil {
		return pipelineerrors.Fatalf("scope", task.WorkflowID, task.ID, "failed to create context-packages dir: %w", err)
	}
	pkgPath := filepath.Join(pkgDir, task.WorkflowID+"-context-package.json")
	pkgData, _ := json.MarshalIndent(fullPkg, "", "  ")
	if err := os.WriteFile(pkgPath, pkgData, 0644); err != nil {
		return pipelineerrors.Fatalf("scope", task.WorkflowID, task.ID, "failed to write context package: %w", err)
	}

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

	if err := w.db.UpdateWorkflowContext(ctx, database.UpdateWorkflowContextParams{
		ID:                 task.WorkflowID,
		ContextPackagePath: sql.NullString{String: pkgPath, Valid: true},
	}); err != nil {
		return pipelineerrors.Fatalf("scope", task.WorkflowID, task.ID, "failed to update workflow context path: %w", err)
	}

	if err := w.db.UpdateWorkflowState(ctx, database.UpdateWorkflowStateParams{
		ID:           task.WorkflowID,
		CurrentState: "scope_complete",
	}); err != nil {
		slog.Error("Failed to update workflow state to scope_complete", "workflow", task.WorkflowID, "error", err)
	}

	w.db.LogEvent(task.WorkflowID, task.ID, "scope_completed", map[string]any{
		"context_package_path": pkgPath,
		"files_to_modify":      len(fullPkg.Scope.FilesToModify),
	})

	slog.Info("Scope phase complete",
		"summary", fullPkg.Scope.Summary,
		"files_to_modify", len(fullPkg.Scope.FilesToModify),
		"files_to_reference", len(fullPkg.Scope.FilesToReference),
	)

	w.q.CompleteTask(task.ID, &queue.TaskResult{
		ExitCode:  0,
		StdoutLog: "Scope completed successfully",
	})

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
		MaxAttempts:   1,
	}); err != nil {
		return pipelineerrors.Fatalf("scope", task.WorkflowID, task.ID, "failed to create build task: %w", err)
	}

	return nil
}
