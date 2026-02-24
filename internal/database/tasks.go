package database

import (
	"context"
	"database/sql"
	"encoding/json"
)

// AtomicClaimTask handles the transaction for finding and claiming a task
func (db *DB) AtomicClaimTask(ctx context.Context, workerID string) (*Task, error) {
	tx, err := db.conn.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	qtx := db.WithTx(tx)

	// 1. Find candidate
	taskID, err := qtx.GetPendingTask(ctx)
	if err == sql.ErrNoRows {
		return nil, nil // No tasks
	}
	if err != nil {
		return nil, err
	}

	// 2. Claim it
	err = qtx.ClaimTask(ctx, ClaimTaskParams{
		ClaimedBy: sql.NullString{String: workerID, Valid: true},
		ID:        taskID,
	})
	if err != nil {
		return nil, err
	}

	// 3. Return full task
	task, err := qtx.GetTask(ctx, taskID)
	if err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}

	return &task, nil
}

// CreatePipelineRun inserts a new pipeline_run row when a workflow starts.
func (db *DB) CreatePipelineRun(ctx context.Context, id, workflowID, project, workOrderType string) error {
	_, err := db.conn.ExecContext(ctx,
		`INSERT INTO pipeline_runs (id, workflow_id, project, work_order_type) VALUES (?, ?, ?, ?)`,
		id, workflowID, project, sql.NullString{String: workOrderType, Valid: workOrderType != ""},
	)
	return err
}

// MarkPipelinePhaseStart records the start time for a given phase (scope, build, verify).
func (db *DB) MarkPipelinePhaseStart(ctx context.Context, workflowID, phase string) error {
	col := phase + "_started_at"
	_, err := db.conn.ExecContext(ctx,
		`UPDATE pipeline_runs SET `+col+` = datetime('now'), updated_at = datetime('now') WHERE workflow_id = ?`,
		workflowID,
	)
	return err
}

// MarkPipelinePhaseComplete records the completion time and optional outcome for a phase.
func (db *DB) MarkPipelinePhaseComplete(ctx context.Context, workflowID, phase string) error {
	col := phase + "_completed_at"
	_, err := db.conn.ExecContext(ctx,
		`UPDATE pipeline_runs SET `+col+` = datetime('now'), updated_at = datetime('now') WHERE workflow_id = ?`,
		workflowID,
	)
	return err
}

// SetVerifyResult stores the PASS/WARN/FAIL result from the verify phase.
func (db *DB) SetVerifyResult(ctx context.Context, workflowID, result string) error {
	_, err := db.conn.ExecContext(ctx,
		`UPDATE pipeline_runs SET verify_result = ?, updated_at = datetime('now') WHERE workflow_id = ?`,
		result, workflowID,
	)
	return err
}

// SetHumanResult stores the approve/reject decision from the human gate.
func (db *DB) SetHumanResult(ctx context.Context, workflowID, result string) error {
	_, err := db.conn.ExecContext(ctx,
		`UPDATE pipeline_runs SET human_result = ?, updated_at = datetime('now') WHERE workflow_id = ?`,
		result, workflowID,
	)
	return err
}

// ApproveWorkflow transitions a workflow from human_review to completed.
func (db *DB) ApproveWorkflow(ctx context.Context, workflowID string) error {
	_, err := db.conn.ExecContext(ctx,
		`UPDATE workflows SET current_state = 'completed', completed_at = datetime('now'), updated_at = datetime('now') WHERE id = ?`,
		workflowID,
	)
	return err
}

// RejectWorkflow transitions a workflow from human_review to failed with a reason.
func (db *DB) RejectWorkflow(ctx context.Context, workflowID, reason string) error {
	_, err := db.conn.ExecContext(ctx,
		`UPDATE workflows SET current_state = 'failed', error_message = ?, completed_at = datetime('now'), updated_at = datetime('now') WHERE id = ?`,
		reason, workflowID,
	)
	return err
}

func (db *DB) LogEvent(workflowID, taskID, eventType string, data map[string]any) error {
	var dataJson []byte
	if data != nil {
		dataJson, _ = json.Marshal(data)
	}

	// CreateEvent expects sql.NullString for taskID and data
	tID := sql.NullString{String: taskID, Valid: taskID != ""}
	dStr := sql.NullString{String: string(dataJson), Valid: len(dataJson) > 0}
	wID := sql.NullString{String: workflowID, Valid: workflowID != ""}

	return db.CreateEvent(context.Background(), CreateEventParams{
		WorkflowID: wID,
		TaskID:     tID,
		EventType:  eventType,
		EventData:  dStr,
	})
}
