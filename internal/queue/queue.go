package queue

import (
	"context"
	"database/sql"
	"encoding/json"
	goerrors "errors"
	"fmt"
	"log/slog"

	"github.com/ponchione/agent-conductor/internal/config"
	"github.com/ponchione/agent-conductor/internal/database"
	pipelineerrors "github.com/ponchione/agent-conductor/internal/errors"
)

var (
	ErrNoTasks        = goerrors.New("no tasks available")
	ErrWorkflowPaused = goerrors.New("workflow is paused")
)

type Queue struct {
	db  *database.DB
	cfg *config.ProjectConfig
}

type TaskResult struct {
	ExitCode     int
	StdoutLog    string
	StderrLog    string
	FilesChanged []string
}

func New(cfg *config.ProjectConfig, db *database.DB) *Queue {
	return &Queue{
		db:  db,
		cfg: cfg,
	}
}

// ClaimNextTask attempts to claim a task for a worker.
func (q *Queue) ClaimNextTask(workerID string) (*database.Task, error) {
	ctx := context.Background()

	// Use the atomic helper we added to database.go
	task, err := q.db.AtomicClaimTask(ctx, workerID)
	if err != nil {
		return nil, err
	}
	if task == nil {
		return nil, ErrNoTasks
	}

	wf, err := q.db.GetWorkflow(ctx, task.WorkflowID)
	if err != nil {
		q.FailTask(task.ID, fmt.Errorf("workflow missing: %w", err))
		return nil, err
	}

	if wf.CurrentState == "paused" || wf.CurrentState == "review_needed" ||
		wf.CurrentState == "completed" || wf.CurrentState == "failed" {
		q.ReleaseTask(task.ID)
		return nil, ErrWorkflowPaused
	}

	if q.checkBudgetExceeded(&wf) {
		q.triggerGate(wf.ID, "budget_exceeded", "Workflow limits exceeded")
		q.ReleaseTask(task.ID)
		return nil, ErrWorkflowPaused
	}

	return task, nil
}

func (q *Queue) ReleaseTask(taskID string) error {
	return q.db.ReleaseTask(context.Background(), taskID)
}

func (q *Queue) CompleteTask(taskID string, result *TaskResult) error {
	filesJson, _ := json.Marshal(result.FilesChanged)

	params := database.CompleteTaskParams{
		ID:           taskID,
		ExitCode:     sql.NullInt64{Int64: int64(result.ExitCode), Valid: true},
		StdoutLog:    sql.NullString{String: result.StdoutLog, Valid: result.StdoutLog != ""},
		StderrLog:    sql.NullString{String: result.StderrLog, Valid: result.StderrLog != ""},
		FilesChanged: sql.NullString{String: string(filesJson), Valid: true},
	}
	return q.db.CompleteTask(context.Background(), params)
}

func (q *Queue) FailTask(taskID string, err error) error {
	params := database.FailTaskParams{
		ID:           taskID,
		ErrorMessage: sql.NullString{String: err.Error(), Valid: true},
	}
	return q.db.FailTask(context.Background(), params)
}

func (q *Queue) RetryTask(taskID string) error {
	return q.db.RetryTask(context.Background(), taskID)
}

func (q *Queue) ShouldRetry(task *database.Task, err error) bool {
	var pe *pipelineerrors.PipelineError
	if goerrors.As(err, &pe) && pe.Class != pipelineerrors.Retryable {
		return false
	}
	return task.Attempts < task.MaxAttempts
}

func (q *Queue) checkBudgetExceeded(wf *database.Workflow) bool {
	if wf.CurrentDepth >= wf.MaxDepth {
		return true
	}
	if wf.FilesChanged >= wf.MaxFilesChanged {
		return true
	}
	return false
}

func (q *Queue) triggerGate(workflowID, gateType, details string) {
	slog.Info("Triggering gate", "wf", workflowID, "type", gateType)

	ctx := context.Background()
	q.db.UpdateWorkflowState(ctx, database.UpdateWorkflowStateParams{
		ID:           workflowID,
		CurrentState: "review_needed",
	})

	q.db.LogEvent(workflowID, "", "gate_triggered", map[string]any{
		"gate_type": gateType,
		"details":   details,
	})
}
