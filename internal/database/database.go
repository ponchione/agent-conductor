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

	// Migration: add work_order_content column for existing databases.
	_, migErr := conn.Exec("ALTER TABLE pipeline_runs ADD COLUMN work_order_content TEXT")
	if migErr != nil && !strings.Contains(migErr.Error(), "duplicate column") {
		return nil, fmt.Errorf("migration failed: %w", migErr)
	}

	return &DB{
		Queries: New(conn),
		conn:    conn,
	}, nil
}

func (db *DB) Close() error {
	return db.conn.Close()
}
