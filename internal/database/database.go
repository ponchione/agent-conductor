package database

import (
	"database/sql"
	//"encoding/json"
	"fmt"
	//"time"

	//"github.com/google/uuid"
	_ "modernc.org/sqlite" // Pure Go SQLite driver
)

type DB struct {
	conn *sql.DB
}

func New(dsn string) (*DB, error) {
	conn, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}

	if err := conn.Ping(); err != nil {
		return nil, err
	}

	db := &DB{conn: conn}
	if err := db.migrate(); err != nil {
		return nil, fmt.Errorf("migration failed: %w", err)
	}

	return db, nil
}

func (db *DB) Close() error {
	return db.conn.Close()
}

func (db *DB) migrate() error {
	queries := []string{
		`CREATE TABLE IF NOT EXISTS workflows (
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
		);`,
		`CREATE TABLE IF NOT EXISTS tasks (
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
		);`,
		`CREATE TABLE IF NOT EXISTS events (
			id                  INTEGER PRIMARY KEY AUTOINCREMENT,
			workflow_id         TEXT REFERENCES workflows(id),
			task_id             TEXT REFERENCES tasks(id),
			event_type          TEXT NOT NULL,
			event_data          TEXT,
			created_at          TEXT NOT NULL DEFAULT (datetime('now'))
		);`,
		`CREATE INDEX IF NOT EXISTS idx_tasks_workflow ON tasks(workflow_id);`,
		`CREATE INDEX IF NOT EXISTS idx_tasks_state ON tasks(state);`,
		`CREATE INDEX IF NOT EXISTS idx_tasks_claimed ON tasks(state, claimed_at);`,
		`CREATE INDEX IF NOT EXISTS idx_events_workflow ON events(workflow_id);`,
	}

	for _, q := range queries {
		if _, err := db.conn.Exec(q); err != nil {
			return err
		}
	}
	return nil
}
