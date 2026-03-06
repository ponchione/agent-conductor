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
		return nil, err
	}

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
	}
	for _, m := range migrations {
		_, migErr := conn.Exec(m)
		if migErr != nil && !strings.Contains(migErr.Error(), "duplicate column") {
			return nil, fmt.Errorf("migration failed: %w", migErr)
		}
	}

	return &DB{
		Queries: New(conn),
		conn:    conn,
	}, nil
}

func (db *DB) Close() error {
	return db.conn.Close()
}
