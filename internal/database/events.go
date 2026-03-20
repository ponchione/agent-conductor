package database

import (
	"context"
	"database/sql"
	"errors"
)

// LogWorkflowEvent enriches event payloads with workflow/task/session
// identifiers before persisting them into the events table.
func (db *DB) LogWorkflowEvent(ctx context.Context, workflowID, taskID, eventType string, data map[string]any) error {
	payload := make(map[string]any, len(data)+3)
	for key, value := range data {
		payload[key] = value
	}

	if workflowID != "" {
		payload["workflow_id"] = workflowID
	}
	if taskID != "" {
		payload["task_id"] = taskID
	}
	if workflowID != "" {
		if _, ok := payload["session_id"]; !ok {
			sessionID, err := db.GetSessionIDForWorkflow(ctx, workflowID)
			switch {
			case err == nil:
				payload["session_id"] = sessionID
			case errors.Is(err, sql.ErrNoRows):
			default:
				return err
			}
		}
	}

	return db.LogEvent(workflowID, taskID, eventType, payload)
}
