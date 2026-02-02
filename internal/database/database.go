package database

import (
	"context"
	"database/sql"
	_ "embed"
	"encoding/json"
	"fmt"

	_ "modernc.org/sqlite"
)

const ddl = `
CREATE TABLE IF NOT EXISTS workflows (
    id                  TEXT PRIMARY KEY,
    original_intent     TEXT NOT NULL,
    original_file       TEXT NOT NULL,
    current_state       TEXT NOT NULL,
    target_repo         TEXT NOT NULL,
    git_branch          TEXT NOT NULL,
    max_depth           INTEGER NOT NULL DEFAULT 5,
    max_files_changed   INTEGER NOT NULL DEFAULT 50,
    max_duration_mins   INTEGER NOT NULL DEFAULT 60,
    current_depth       INTEGER NOT NULL DEFAULT 0,
    files_changed       INTEGER NOT NULL DEFAULT 0,
    started_at          TEXT,
    created_at          TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at          TEXT NOT NULL DEFAULT (datetime('now')),
    completed_at        TEXT,
    error_message       TEXT
);

CREATE TABLE IF NOT EXISTS tasks (
    id                  TEXT PRIMARY KEY,
    workflow_id         TEXT NOT NULL REFERENCES workflows(id),
    sequence_num        INTEGER NOT NULL,
    task_type           TEXT NOT NULL,
    agent_type          TEXT NOT NULL,
    target_repo         TEXT NOT NULL,
    input_artifact      TEXT NOT NULL,
    output_artifact     TEXT,
    state               TEXT NOT NULL DEFAULT 'pending',
    claimed_by          TEXT,
    claimed_at          TEXT,
    attempts            INTEGER NOT NULL DEFAULT 0,
    max_attempts        INTEGER NOT NULL DEFAULT 2,
    exit_code           INTEGER,
    stdout_log          TEXT,
    stderr_log          TEXT,
    files_changed       TEXT,
    created_at          TEXT NOT NULL DEFAULT (datetime('now')),
    started_at          TEXT,
    completed_at        TEXT,
    error_message       TEXT
);

CREATE INDEX IF NOT EXISTS idx_tasks_workflow ON tasks(workflow_id);
CREATE INDEX IF NOT EXISTS idx_tasks_state ON tasks(state);
CREATE INDEX IF NOT EXISTS idx_tasks_claimed ON tasks(state, claimed_at);

CREATE TABLE IF NOT EXISTS events (
    id                  INTEGER PRIMARY KEY AUTOINCREMENT,
    workflow_id         TEXT REFERENCES workflows(id),
    task_id             TEXT REFERENCES tasks(id),
    event_type          TEXT NOT NULL,
    event_data          TEXT,
    created_at          TEXT NOT NULL DEFAULT (datetime('now'))
);
CREATE INDEX IF NOT EXISTS idx_events_workflow ON events(workflow_id);
`

// DB wraps the sqlc generated queries and the raw connection
type DB struct {
	*Queries
	conn *sql.DB
}

func NewDB(dsn string) (*DB, error) {
	conn, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}

	if err := conn.Ping(); err != nil {
		return nil, err
	}

	// Enable foreign keys
	if _, err := conn.Exec("PRAGMA foreign_keys = ON;"); err != nil {
		return nil, err
	}

	// Run migration
	if _, err := conn.Exec(ddl); err != nil {
		return nil, fmt.Errorf("migration failed: %w", err)
	}

	return &DB{
		Queries: New(conn),
		conn:    conn,
	}, nil
}

func (db *DB) Close() error {
	return db.conn.Close()
}

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
