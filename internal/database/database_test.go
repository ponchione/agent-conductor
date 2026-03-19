package database

import (
	"database/sql"
	"path/filepath"
	"strings"
	"testing"

	_ "modernc.org/sqlite"
)

func TestNewDB_FailsForNonCanonicalLegacySchema(t *testing.T) {
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

	_, err = NewDB(dbPath)
	if err == nil {
		t.Fatal("NewDB() error = nil, want canonical schema failure")
	}
	if !strings.Contains(err.Error(), "database schema validation failed") {
		t.Fatalf("NewDB() error = %v, want schema validation failure", err)
	}
}

func TestNewDB_CreatesCanonicalHierarchicalPlanSchema(t *testing.T) {
	t.Parallel()

	dbPath := filepath.Join(t.TempDir(), "conductor.db")
	db, err := NewDB(dbPath)
	if err != nil {
		t.Fatalf("NewDB() error: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	assertColumnExists(t, db.conn, "pipeline_runs", "plan_file")
	assertColumnExists(t, db.conn, "pipeline_runs", "plan_task_id")
	assertColumnExists(t, db.conn, "pipeline_runs", "plan_epic_id")
	assertColumnExists(t, db.conn, "pipeline_runs", "verify_result")
	assertColumnExists(t, db.conn, "pipeline_runs", "human_result")
	assertColumnExists(t, db.conn, "plan_runs", "epic_count")
	assertColumnExists(t, db.conn, "plan_runs", "task_count")
	assertColumnExists(t, db.conn, "plan_runs", "epic_generation_model")
	assertColumnExists(t, db.conn, "plan_runs", "task_generation_model")
	assertColumnExists(t, db.conn, "plan_runs", "task_generation_call_count")
	assertIndexExists(t, db.conn, "pipeline_runs", "idx_pipeline_runs_plan_task")
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

func assertIndexExists(t *testing.T, db *sql.DB, tableName, indexName string) {
	t.Helper()

	rows, err := db.Query(`PRAGMA index_list(` + tableName + `)`)
	if err != nil {
		t.Fatalf("PRAGMA index_list(%s) error: %v", tableName, err)
	}
	defer rows.Close()

	for rows.Next() {
		var (
			seq     int
			name    string
			unique  int
			origin  string
			partial int
		)
		if err := rows.Scan(&seq, &name, &unique, &origin, &partial); err != nil {
			t.Fatalf("Scan PRAGMA index_list(%s) error: %v", tableName, err)
		}
		if name == indexName {
			return
		}
	}

	if err := rows.Err(); err != nil {
		t.Fatalf("rows.Err() for %s: %v", tableName, err)
	}
	t.Fatalf("index %q not found on %s", indexName, tableName)
}
