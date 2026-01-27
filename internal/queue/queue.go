package queue

import (
	"errors"
	"fmt"
	"log/slog"

	"github.com/ponchione/agent-conductor/internal/config"
	"github.com/ponchione/agent-conductor/internal/database"
)

var (
	ErrNoTasks        = errors.New("no tasks available")
	ErrWorkflowPaused = errors.New("workflow is paused")
)

type Queue struct {
	db  *database.DB
	cfg *config.Config
}

func New(cfg *config.Config, db *database.DB) *Queue {
	return &Queue{
		db:  db,
		cfg: cfg,
	}
}

// ClaimNextTask attempts to claim a task for a worker.
func (q *Queue) ClaimNextTask(workerID string) (*database.Task, error) {
	//Claim from DB
	task, err := q.db.ClaimTask(workerID)
	if err != nil {
		return nil, err
	}
	if task == nil {
		return nil, ErrNoTasks
	}

	//Load Workflow
	wf, err := q.db.GetWorkflow(task.WorkflowID)
	if err != nil {
		// Should not happen, but if so, fail task
		q.FailTask(task.ID, fmt.Errorf("workflow missing: %w", err))
		return nil, err
	}

	//Check Workflow State
	if wf.CurrentState == "paused" || wf.CurrentState == "review_needed" || wf.CurrentState == "completed" || wf.CurrentState == "failed" {
		q.ReleaseTask(task.ID)
		return nil, ErrWorkflowPaused
	}

	//Check Safety Limits (Budget)
	if q.checkBudgetExceeded(wf) {
		// Trigger Gate
		q.triggerGate(wf.ID, "budget_exceeded", "Workflow limits exceeded")
		q.ReleaseTask(task.ID)
		return nil, ErrWorkflowPaused
	}

	return task, nil
}

func (q *Queue) checkBudgetExceeded(wf *database.Workflow) bool {
	// Depth limit
	if wf.CurrentDepth >= wf.MaxDepth {
		slog.Warn("Workflow depth exceeded", "wf", wf.ID, "depth", wf.CurrentDepth)
		return true
	}

	// File count limit
	if wf.FilesChanged >= wf.MaxFilesChanged {
		slog.Warn("Workflow file limit exceeded", "wf", wf.ID, "files", wf.FilesChanged)
		return true
	}

	return false
}

func (q *Queue) triggerGate(workflowID, gateType, details string) {
	slog.Info("Triggering gate", "wf", workflowID, "type", gateType)
	q.db.UpdateWorkflowState(workflowID, "review_needed")
	q.db.LogEvent(workflowID, "", "gate_triggered", map[string]any{
		"gate_type": gateType,
		"details":   details,
	})
}

func (q *Queue) ReleaseTask(taskID string) error {
	return nil
}

func (q *Queue) CompleteTask(taskID string, result *TaskResult) error {
	//implement fully in Worker phase
	return nil
}

func (q *Queue) FailTask(taskID string, err error) error {
	//implement fully in Worker phase
	return nil
}

type TaskResult struct {
	ExitCode     int
	StdoutLog    string
	StderrLog    string
	FilesChanged []string
}
