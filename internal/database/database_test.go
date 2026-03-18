package database

import (
	"database/sql"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

func TestNewDB_ResetsLegacyDatabaseToCanonicalSchema(t *testing.T) {
	t.Parallel()

	dbPath := filepath.Join(t.TempDir(), "conductor.db")
	legacyDB, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("sql.Open() error: %v", err)
	}
	t.Cleanup(func() { _ = legacyDB.Close() })

	legacySchema := []string{
		`CREATE TABLE workflows (
			id TEXT PRIMARY KEY,
			original_intent TEXT NOT NULL,
			original_file TEXT NOT NULL,
			current_state TEXT NOT NULL,
			target_repo TEXT NOT NULL,
			git_branch TEXT NOT NULL,
			created_at TEXT NOT NULL DEFAULT (datetime('now')),
			updated_at TEXT NOT NULL DEFAULT (datetime('now'))
		)`,
		`CREATE TABLE pipeline_runs (
			id TEXT PRIMARY KEY,
			workflow_id TEXT NOT NULL REFERENCES workflows(id),
			project TEXT NOT NULL,
			work_order_type TEXT,
			created_at TEXT NOT NULL DEFAULT (datetime('now')),
			updated_at TEXT NOT NULL DEFAULT (datetime('now'))
		)`,
		`CREATE TABLE plan_runs (
			id TEXT PRIMARY KEY,
			spec_file TEXT NOT NULL,
			work_orders_generated INTEGER,
			created_at TEXT NOT NULL DEFAULT (datetime('now'))
		)`,
	}
	for _, stmt := range legacySchema {
		if _, err := legacyDB.Exec(stmt); err != nil {
			t.Fatalf("legacy schema exec error: %v", err)
		}
	}
	if _, err := legacyDB.Exec(`INSERT INTO workflows (id, original_intent, original_file, current_state, target_repo, git_branch) VALUES ('wf-1', 'legacy', 'spec.md', 'pending', 'repo', 'feature/legacy')`); err != nil {
		t.Fatalf("legacy insert error: %v", err)
	}

	resetDB, err := NewDB(dbPath)
	if err != nil {
		t.Fatalf("NewDB() reset error: %v", err)
	}
	t.Cleanup(func() { _ = resetDB.Close() })

	assertColumnExists(t, resetDB.conn, "pipeline_runs", "session_id")
	assertColumnExists(t, resetDB.conn, "plan_runs", "session_id")
	assertColumnExists(t, resetDB.conn, "sessions", "state")
	assertColumnExists(t, resetDB.conn, "artifacts", "metadata_json")

	var workflowCount int
	if err := resetDB.conn.QueryRow(`SELECT COUNT(*) FROM workflows`).Scan(&workflowCount); err != nil {
		t.Fatalf("workflow count query error: %v", err)
	}
	if workflowCount != 0 {
		t.Fatalf("workflowCount = %d, want 0 after clean-slate reset", workflowCount)
	}
}

func assertColumnExists(t *testing.T, db *sql.DB, tableName, columnName string) {
	t.Helper()

	rows, err := db.Query(`PRAGMA table_info(` + tableName + `)`)
	if err != nil {
		t.Fatalf("PRAGMA table_info(%s) error: %v", tableName, err)
	}
	defer rows.Close()

	for rows.Next() {
		var (
			cid        int
			name       string
			columnType string
			notNull    int
			defaultVal sql.NullString
			pk         int
		)
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultVal, &pk); err != nil {
			t.Fatalf("Scan PRAGMA table_info(%s) error: %v", tableName, err)
		}
		if name == columnName {
			return
		}
	}

	if err := rows.Err(); err != nil {
		t.Fatalf("rows.Err() for %s: %v", tableName, err)
	}
	t.Fatalf("column %q not found in %s", columnName, tableName)
}
