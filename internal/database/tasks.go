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
