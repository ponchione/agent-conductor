package database

import (
	"database/sql"
	_ "embed"
	"fmt"
	"strings"

	_ "modernc.org/sqlite"
)

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
		conn.Close()
		return nil, err
	}

	success := false
	defer func() {
		if !success {
			conn.Close()
		}
	}()

	if _, err := conn.Exec("PRAGMA foreign_keys = ON;"); err != nil {
		return nil, err
	}

	if _, err := conn.Exec(ddl); err != nil {
		return nil, fmt.Errorf("migration failed: %w", err)
	}

	// Migrations: add columns for existing databases.
	migrations := []string{
		"ALTER TABLE pipeline_runs ADD COLUMN work_order_content TEXT",
		"ALTER TABLE pipeline_runs ADD COLUMN build_cost_usd REAL",
		"ALTER TABLE pipeline_runs ADD COLUMN build_session_id TEXT",
		"ALTER TABLE pipeline_runs ADD COLUMN build_tool_calls TEXT",
		"CREATE TABLE IF NOT EXISTS sessions (id TEXT PRIMARY KEY, kind TEXT NOT NULL, project TEXT NOT NULL, source_spec_path TEXT, state TEXT NOT NULL, error_message TEXT, started_at TEXT, completed_at TEXT, created_at TEXT NOT NULL DEFAULT (datetime('now')), updated_at TEXT NOT NULL DEFAULT (datetime('now')))",
		"CREATE INDEX IF NOT EXISTS idx_sessions_state_created_at ON sessions(state, created_at)",
		"CREATE INDEX IF NOT EXISTS idx_sessions_project_created_at ON sessions(project, created_at)",
		"ALTER TABLE pipeline_runs ADD COLUMN session_id TEXT REFERENCES sessions(id)",
		"ALTER TABLE plan_runs ADD COLUMN session_id TEXT REFERENCES sessions(id)",
		"CREATE INDEX IF NOT EXISTS idx_pipeline_runs_session_id ON pipeline_runs(session_id)",
		"CREATE INDEX IF NOT EXISTS idx_plan_runs_session_id ON plan_runs(session_id)",
		"ALTER TABLE plan_runs ADD COLUMN generation_session_id TEXT",
		"ALTER TABLE plan_runs ADD COLUMN audit_session_id TEXT",
		"ALTER TABLE plan_runs ADD COLUMN generation_cost_usd REAL",
		"ALTER TABLE plan_runs ADD COLUMN audit_cost_usd REAL",
		"ALTER TABLE plan_runs ADD COLUMN generation_duration_ms INTEGER",
		"ALTER TABLE plan_runs ADD COLUMN audit_duration_ms INTEGER",
		"ALTER TABLE plan_runs ADD COLUMN generation_retry_count INTEGER NOT NULL DEFAULT 0",
		"CREATE TABLE IF NOT EXISTS artifacts (id TEXT PRIMARY KEY, session_id TEXT REFERENCES sessions(id), workflow_id TEXT REFERENCES workflows(id), task_id TEXT REFERENCES tasks(id), artifact_type TEXT NOT NULL, path TEXT NOT NULL, size_bytes INTEGER, metadata_json TEXT, created_at TEXT NOT NULL DEFAULT (datetime('now')))",
		"CREATE INDEX IF NOT EXISTS idx_artifacts_session_id ON artifacts(session_id)",
		"CREATE INDEX IF NOT EXISTS idx_artifacts_workflow_id ON artifacts(workflow_id)",
		"CREATE INDEX IF NOT EXISTS idx_artifacts_type_created ON artifacts(artifact_type, created_at)",
		"ALTER TABLE plan_runs ADD COLUMN project TEXT",
		"ALTER TABLE plan_runs ADD COLUMN spec_fingerprint TEXT",
		"ALTER TABLE plan_runs ADD COLUMN pre_audit_work_order_count INTEGER",
		"ALTER TABLE plan_runs ADD COLUMN post_audit_work_order_count INTEGER",
		"ALTER TABLE plan_runs ADD COLUMN audit_change_text TEXT",
	}
	for _, m := range migrations {
		_, migErr := conn.Exec(m)
		if migErr != nil && !strings.Contains(migErr.Error(), "duplicate column") {
			return nil, fmt.Errorf("migration failed: %w", migErr)
		}
	}

	success = true
	return &DB{
		Queries: New(conn),
		conn:    conn,
	}, nil
}

func (db *DB) Close() error {
	return db.conn.Close()
}
