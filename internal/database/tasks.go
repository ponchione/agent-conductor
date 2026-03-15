package database

import (
	"context"
	"database/sql"
	"encoding/json"
	"log/slog"
)

// TaskSummary holds the fields needed for task list operations.
type TaskSummary struct {
	ID          string
	SequenceNum int64
	Phase       string
	State       string
	Attempts    int64
	MaxAttempts int64
	StartedAt   sql.NullString
	CompletedAt sql.NullString
}

// ListTasksByWorkflow returns tasks for a workflow ordered by sequence_num ascending.
func (db *DB) ListTasksByWorkflow(ctx context.Context, workflowID string) ([]TaskSummary, error) {
	rows, err := db.conn.QueryContext(ctx,
		`SELECT id, sequence_num, phase, state, attempts, max_attempts, started_at, completed_at
		 FROM tasks WHERE workflow_id = ? ORDER BY sequence_num ASC`, workflowID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []TaskSummary
	for rows.Next() {
		var s TaskSummary
		if err := rows.Scan(&s.ID, &s.SequenceNum, &s.Phase, &s.State, &s.Attempts, &s.MaxAttempts, &s.StartedAt, &s.CompletedAt); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// AtomicClaimTask handles the transaction for finding and claiming a task
func (db *DB) AtomicClaimTask(ctx context.Context, workerID string) (*Task, error) {
	for range 5 {
		tx, err := db.conn.Begin()
		if err != nil {
			return nil, err
		}
		qtx := db.WithTx(tx)

		taskID, err := qtx.GetPendingTask(ctx)
		if err == sql.ErrNoRows {
			_ = tx.Rollback()
			return nil, nil
		}
		if err != nil {
			_ = tx.Rollback()
			return nil, err
		}

		rows, err := qtx.ClaimTask(ctx, ClaimTaskParams{
			ClaimedBy: sql.NullString{String: workerID, Valid: true},
			ID:        taskID,
		})
		if err != nil {
			_ = tx.Rollback()
			return nil, err
		}
		if rows == 0 {
			_ = tx.Rollback()
			continue
		}

		task, err := qtx.GetTask(ctx, taskID)
		if err != nil {
			_ = tx.Rollback()
			return nil, err
		}

		if err := markTaskStartedIfUnset(ctx, tx, task.ID); err != nil {
			_ = tx.Rollback()
			return nil, err
		}
		if err := markWorkflowStartedIfUnset(ctx, tx, task.WorkflowID); err != nil {
			_ = tx.Rollback()
			return nil, err
		}

		task, err = qtx.GetTask(ctx, taskID)
		if err != nil {
			_ = tx.Rollback()
			return nil, err
		}

		if err := tx.Commit(); err != nil {
			_ = tx.Rollback()
			return nil, err
		}

		return &task, nil
	}

	return nil, nil
}

func markTaskStartedIfUnset(ctx context.Context, q DBTX, taskID string) error {
	_, err := q.ExecContext(ctx, `
		UPDATE tasks
		SET started_at = COALESCE(started_at, datetime('now'))
		WHERE id = ? AND started_at IS NULL
	`, taskID)
	return err
}

func (db *DB) LogEvent(workflowID, taskID, eventType string, data map[string]any) error {
	var dataJson []byte
	if data != nil {
		var err error
		dataJson, err = json.Marshal(data)
		if err != nil {
			slog.Warn("failed to marshal event data", "event_type", eventType, "error", err)
		}
	}

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
