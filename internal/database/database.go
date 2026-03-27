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
	conn, err := openSQLiteConnection(dsn)
	if err != nil {
		return nil, err
	}

	success := false
	defer func() {
		if !success && conn != nil {
			conn.Close()
		}
	}()

	conn, err = ensureCanonicalDatabase(conn, dsn)
	if err != nil {
		return nil, err
	}

	success = true
	return &DB{
		Queries: New(conn),
		conn:    conn,
	}, nil
}

func openSQLiteConnection(dsn string) (*sql.DB, error) {
	conn, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	// Keep SQLite on a single pooled connection. This avoids self-inflicted
	// lock contention inside one process while remaining compatible with
	// future multi-process access to the same DB file.
	conn.SetMaxOpenConns(1)
	conn.SetMaxIdleConns(1)

	if err := conn.Ping(); err != nil {
		conn.Close()
		return nil, err
	}
	if _, err := conn.Exec("PRAGMA foreign_keys = ON;"); err != nil {
		conn.Close()
		return nil, err
	}
	if _, err := conn.Exec("PRAGMA busy_timeout = 5000;"); err != nil {
		conn.Close()
		return nil, err
	}
	if _, err := conn.Exec("PRAGMA journal_mode = WAL;"); err != nil {
		conn.Close()
		return nil, err
	}

	return conn, nil
}

func ensureCanonicalDatabase(conn *sql.DB, _ string) (*sql.DB, error) {
	if err := applyDDL(conn); err != nil {
		return nil, fmt.Errorf("schema bootstrap failed: %w", err)
	}
	if err := validateCanonicalSchema(conn); err != nil {
		return nil, fmt.Errorf("database schema validation failed: %w", err)
	}
	return conn, nil
}

func validateCanonicalSchema(conn *sql.DB) error {
	checks := []string{
		"SELECT id, context_package_path, verification_report_path, max_depth, max_files_changed, max_duration_mins, current_depth, files_changed, error_message FROM workflows LIMIT 0",
		"SELECT id, phase, input_artifact, output_artifact, files_changed, error_message FROM tasks LIMIT 0",
		"SELECT id, workflow_id, task_id, event_type, event_data FROM events LIMIT 0",
		"SELECT id, kind, project, source_spec_path, state, error_message, started_at, completed_at FROM sessions LIMIT 0",
		"SELECT id, session_id, workflow_id, task_id, artifact_type, path, size_bytes, metadata_json FROM artifacts LIMIT 0",
		"SELECT id, session_id, workflow_id, project, work_order_type, plan_file, plan_task_id, plan_epic_id, scope_started_at, build_started_at, verify_started_at, scope_model, build_model, verify_model, scope_files_suggested, build_files_changed, build_scope_drift, scope_estimated_complexity, scope_rag_direct, scope_rag_hops, scope_paths_stripped, scope_paths_reclassified, verify_result, human_result, work_order_content, scope_tokens_in, build_tokens_in, verify_tokens_in, build_cost_usd, build_session_id, build_tool_calls, build_claude_md_content FROM pipeline_runs LIMIT 0",
		"SELECT id, pipeline_run_id, phase, step, target_path, provider, model, tokens_in, tokens_out, latency_ms, estimated_cost_usd, success, error_message FROM sub_calls LIMIT 0",
		"SELECT id, session_id, spec_file, project, spec_fingerprint, generation_model, epic_generation_model, task_generation_model, audit_model, generation_session_id, audit_session_id, work_orders_generated, epic_count, task_count, pre_audit_work_order_count, post_audit_work_order_count, audit_change_text, audit_work_orders_added, audit_work_orders_modified, audit_work_orders_unchanged, generation_cost_usd, epic_generation_cost_usd, task_generation_cost_usd, audit_cost_usd, generation_duration_ms, epic_generation_duration_ms, task_generation_duration_ms, audit_duration_ms, generation_retry_count, audit_tokens_in, audit_tokens_out, generation_tokens_in, generation_tokens_out, epic_generation_tokens_in, epic_generation_tokens_out, task_generation_call_count, task_generation_tokens_in, task_generation_tokens_out, state, error_message, current_phase FROM plan_runs LIMIT 0",
	}

	for _, query := range checks {
		rows, err := conn.Query(query)
		if err != nil {
			return fmt.Errorf("canonical schema check failed for query %q: %w", query, err)
		}
		rows.Close()
	}
	return nil
}

func (db *DB) Close() error {
	return db.conn.Close()
}

func applyDDL(conn *sql.DB) error {
	for _, block := range []string{ddl, ddlExtra} {
		statements := strings.Split(block, ";")
		for _, stmt := range statements {
			stmt = strings.TrimSpace(stmt)
			if stmt == "" {
				continue
			}
			if _, err := conn.Exec(stmt); err != nil {
				upperStmt := strings.ToUpper(stripSQLComments(stmt))
				if strings.HasPrefix(upperStmt, "CREATE INDEX IF NOT EXISTS") && strings.Contains(err.Error(), "no such column") {
					continue
				}
				if strings.HasPrefix(upperStmt, "ALTER TABLE") && strings.Contains(err.Error(), "duplicate column name") {
					continue
				}
				return err
			}
		}
	}
	return nil
}

// stripSQLComments removes leading SQL line comments (-- ...) from a statement.
func stripSQLComments(stmt string) string {
	lines := strings.Split(stmt, "\n")
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed != "" && !strings.HasPrefix(trimmed, "--") {
			return strings.TrimSpace(strings.Join(lines[i:], "\n"))
		}
	}
	return ""
}
