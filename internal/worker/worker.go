package worker

import (
	stdctx "context"
	goerrors "errors"
	"fmt"
	"log/slog"
	"os"

	"github.com/ponchione/agent-conductor/internal/config"
	"github.com/ponchione/agent-conductor/internal/context"
	"github.com/ponchione/agent-conductor/internal/database"
	pipelineerrors "github.com/ponchione/agent-conductor/internal/errors"
	"github.com/ponchione/agent-conductor/internal/executor"
	"github.com/ponchione/agent-conductor/internal/git"
	"github.com/ponchione/agent-conductor/internal/llm"
	"github.com/ponchione/agent-conductor/internal/queue"
	"github.com/ponchione/agent-conductor/internal/templates"
	"gopkg.in/yaml.v3"
)

// Worker processes tasks from the queue
type Worker struct {
	ID         string
	q          *queue.Queue
	db         *database.DB
	cfg        *config.ProjectConfig
	assembler  *context.Assembler
	models     *llm.RoleResolver
	guardrails *config.Guardrails
	runner     executor.BuildExecutor
	git        *git.GitManager
	prompts    *templates.LoadedPrompts
}

func New(
	id string,
	q *queue.Queue,
	db *database.DB,
	cfg *config.ProjectConfig,
	assembler *context.Assembler,
	models *llm.RoleResolver,
	guardrails *config.Guardrails,
	runner executor.BuildExecutor,
	gitMgr *git.GitManager,
	prompts *templates.LoadedPrompts,
) *Worker {
	return &Worker{
		ID:         id,
		q:          q,
		db:         db,
		cfg:        cfg,
		assembler:  assembler,
		models:     models,
		guardrails: guardrails,
		runner:     runner,
		git:        gitMgr,
		prompts:    prompts,
	}
}

// ProcessNextTask claims and processes the next task from the queue
func (w *Worker) ProcessNextTask(ctx stdctx.Context) {
	task, err := w.q.ClaimNextTask(w.ID)
	if err != nil {
		if err != queue.ErrNoTasks {
			slog.Error("Failed to claim task", "error", err)
		}
		return
	}

	slog.Info("Processing task", "id", task.ID, "phase", task.Phase)
	w.db.LogEvent(task.WorkflowID, task.ID, "task_claimed", map[string]any{
		"phase":     task.Phase,
		"worker_id": w.ID,
	})

	var result error
	switch task.Phase {
	case "scope":
		if isBootstrapWorkOrder(task) {
			result = w.runBootstrapScope(ctx, task)
		} else {
			result = w.runScope(ctx, task)
		}
	case "build":
		result = w.runBuild(ctx, task)
	case "verify":
		result = w.runVerify(ctx, task)
	default:
		result = fmt.Errorf("unknown phase: %s", task.Phase)
	}

	if result != nil {
		var pe *pipelineerrors.PipelineError
		if !goerrors.As(result, &pe) {
			pe = pipelineerrors.Fatalf(task.Phase, task.WorkflowID, task.ID, "%v", result)
		}

		slog.Error("Task failed",
			"id", task.ID, "phase", task.Phase,
			"class", pe.Class.String(), "error", pe)
		w.db.LogEvent(task.WorkflowID, task.ID, task.Phase+"_failed", map[string]any{
			"error": pe.Error(), "class": pe.Class.String(),
		})

		switch pe.Class {
		case pipelineerrors.Retryable:
			if w.q.ShouldRetry(task, result) {
				w.q.RetryTask(task.ID)
				return
			}
		case pipelineerrors.NeedsHuman:
			w.q.FailTask(task.ID, result)
			if err := w.db.TransitionWorkflowState(ctx, task.WorkflowID, "human_review"); err != nil {
				slog.Error("Failed to transition workflow to human_review", "workflow", task.WorkflowID, "error", err)
			}
			return
		}
		w.q.FailTask(task.ID, result)
		if err := w.db.TransitionWorkflowState(ctx, task.WorkflowID, "failed"); err != nil {
			slog.Error("Failed to transition workflow to failed", "workflow", task.WorkflowID, "error", err)
		}
	}
}

// isBootstrapWorkOrder reads the task's input artifact (work order YAML) and
// returns true if the work order type is "bootstrap".
func isBootstrapWorkOrder(task *database.Task) bool {
	data, err := os.ReadFile(task.InputArtifact)
	if err != nil {
		return false
	}
	var wo struct {
		Type string `yaml:"type"`
	}
	if err := yaml.Unmarshal(data, &wo); err != nil {
		return false
	}
	return wo.Type == "bootstrap"
}
