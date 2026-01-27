package database

import (
	"database/sql"
	"encoding/json"

	"github.com/google/uuid"
	"github.com/ponchione/agent-conductor/internal/util"
)

func (db *DB) CreateWorkflow(w *Workflow) error {
	if w.ID == "" {
		w.ID = uuid.New().String()
	}
	query := `INSERT INTO workflows (
		id, original_intent, original_file, current_state, target_repo, git_branch,
		max_depth, max_files_changed, max_duration_mins
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`

	_, err := db.conn.Exec(query,
		w.ID, w.OriginalIntent, w.OriginalFile, w.CurrentState, w.TargetRepo, w.GitBranch,
		w.MaxDepth, w.MaxFilesChanged, w.MaxDurationMins,
	)
	return err
}

func (db *DB) GetWorkflow(id string) (*Workflow, error) {
	query := `SELECT id, original_intent, original_file, current_state, target_repo, git_branch,
		max_depth, max_files_changed, max_duration_mins, current_depth, files_changed,
		started_at, created_at, updated_at, completed_at, error_message
		FROM workflows WHERE id = ?`

	w := &Workflow{}
	var startedAt, createdAt, updatedAt, completedAt sql.NullString
	var errMsg sql.NullString

	err := db.conn.QueryRow(query, id).Scan(
		&w.ID, &w.OriginalIntent, &w.OriginalFile, &w.CurrentState, &w.TargetRepo, &w.GitBranch,
		&w.MaxDepth, &w.MaxFilesChanged, &w.MaxDurationMins, &w.CurrentDepth, &w.FilesChanged,
		&startedAt, &createdAt, &updatedAt, &completedAt, &errMsg,
	)
	if err != nil {
		return nil, err
	}

	w.StartedAt = util.ParseSQLiteTime(startedAt)
	w.CompletedAt = util.ParseSQLiteTime(completedAt)
	if t := util.ParseSQLiteTime(createdAt); t != nil {
		w.CreatedAt = *t
	}
	if t := util.ParseSQLiteTime(updatedAt); t != nil {
		w.UpdatedAt = *t
	}
	w.ErrorMessage = errMsg.String
	return w, nil
}

func (db *DB) UpdateWorkflowState(id, state string) error {
	_, err := db.conn.Exec("UPDATE workflows SET current_state = ?, updated_at = datetime('now') WHERE id = ?", state, id)
	return err
}

// Event Operations

func (db *DB) LogEvent(workflowID, taskID, eventType string, data map[string]any) error {
	var dataJson []byte
	if data != nil {
		dataJson, _ = json.Marshal(data)
	}

	query := `INSERT INTO events (workflow_id, task_id, event_type, event_data) VALUES (?, ?, ?, ?)`
	_, err := db.conn.Exec(query, workflowID, taskID, eventType, string(dataJson))
	return err
}

func (db *DB) GetEventsForWorkflow(workflowID string) ([]*Event, error) {
	query := `SELECT id, workflow_id, task_id, event_type, event_data, created_at FROM events WHERE workflow_id = ? ORDER BY created_at ASC`
	rows, err := db.conn.Query(query, workflowID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var events []*Event
	for rows.Next() {
		e := &Event{}
		var taskID sql.NullString
		var dataStr sql.NullString
		var createdAt sql.NullString

		if err := rows.Scan(&e.ID, &e.WorkflowID, &taskID, &e.EventType, &dataStr, &createdAt); err != nil {
			return nil, err
		}
		e.TaskID = taskID.String
		if dataStr.Valid && dataStr.String != "" {
			_ = json.Unmarshal([]byte(dataStr.String), &e.EventData)
		}
		if t := util.ParseSQLiteTime(createdAt); t != nil {
			e.CreatedAt = *t
		}
		events = append(events, e)
	}
	return events, nil
}
