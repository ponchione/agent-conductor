package worker

import (
	stdctx "context"
	"fmt"
	"log/slog"
	"time"

	"github.com/ponchione/agent-conductor/internal/config"
	"github.com/ponchione/agent-conductor/internal/context"
	"github.com/ponchione/agent-conductor/internal/database"
	"github.com/ponchione/agent-conductor/internal/executor"
	"github.com/ponchione/agent-conductor/internal/git"
	"github.com/ponchione/agent-conductor/internal/llm"
	"github.com/ponchione/agent-conductor/internal/queue"
)

// Worker processes tasks from the queue
type Worker struct {
	ID        string
	q         *queue.Queue
	db        *database.DB
	cfg       *config.ProjectConfig
	assembler *context.Assembler
	llm       *llm.Client
	runner    *executor.OpenCodeRunner
	git       *git.GitManager
}

func New(
	id string,
	q *queue.Queue,
	db *database.DB,
	cfg *config.ProjectConfig,
	assembler *context.Assembler,
	llmClient *llm.Client,
	runner *executor.OpenCodeRunner,
	gitMgr *git.GitManager,
) *Worker {
	return &Worker{
		ID:        id,
		q:         q,
		db:        db,
		cfg:       cfg,
		assembler: assembler,
		llm:       llmClient,
		runner:    runner,
		git:       gitMgr,
	}
}

// Start begins the worker loop
func (w *Worker) Start(ctx stdctx.Context) {
	slog.Info("Worker starting", "id", w.ID)
	w.ProcessNextTask(ctx)

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			slog.Info("Worker stopping", "id", w.ID)
			return
		case <-ticker.C:
			w.ProcessNextTask(ctx)
		}
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
		result = w.runScope(ctx, task)
	case "build":
		result = w.runBuild(ctx, task)
	case "verify":
		result = w.runVerify(ctx, task)
	default:
		result = fmt.Errorf("unknown phase: %s", task.Phase)
	}

	if result != nil {
		slog.Error("Task failed", "id", task.ID, "phase", task.Phase, "error", result)

		w.db.LogEvent(task.WorkflowID, task.ID, task.Phase+"_failed", map[string]any{
			"error": result.Error(),
		})

		w.q.FailTask(task.ID, result)

		if err := w.db.UpdateWorkflowState(ctx, database.UpdateWorkflowStateParams{
			ID:           task.WorkflowID,
			CurrentState: "failed",
		}); err != nil {
			slog.Error("Failed to update workflow state to failed", "workflow", task.WorkflowID, "error", err)
		}
	}
}
