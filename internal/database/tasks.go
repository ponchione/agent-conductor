package database

import (
	"context"
	"database/sql"
	"encoding/json"
	"log/slog"
)

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

		if err := tx.Commit(); err != nil {
			_ = tx.Rollback()
			return nil, err
		}

		return &task, nil
	}

	return nil, nil
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
