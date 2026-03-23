package graph

import (
	"database/sql"
	"fmt"

	_ "modernc.org/sqlite"
)

const graphDDL = `
CREATE TABLE IF NOT EXISTS graph_meta (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS symbols (
    id         TEXT PRIMARY KEY,
    name       TEXT NOT NULL,
    kind       TEXT NOT NULL,
    language   TEXT NOT NULL,
    package    TEXT,
    file_path  TEXT NOT NULL,
    line_start INTEGER NOT NULL,
    line_end   INTEGER NOT NULL,
    signature  TEXT,
    exported   INTEGER NOT NULL DEFAULT 0,
    receiver   TEXT
);

CREATE INDEX IF NOT EXISTS idx_symbols_file ON symbols(file_path);
CREATE INDEX IF NOT EXISTS idx_symbols_name ON symbols(name);
CREATE INDEX IF NOT EXISTS idx_symbols_kind ON symbols(kind);
CREATE INDEX IF NOT EXISTS idx_symbols_package ON symbols(package);

CREATE TABLE IF NOT EXISTS edges (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    source_id   TEXT NOT NULL REFERENCES symbols(id) ON DELETE CASCADE,
    target_id   TEXT NOT NULL,
    edge_type   TEXT NOT NULL,
    confidence  REAL NOT NULL DEFAULT 1.0,
    source_line INTEGER,
    metadata    TEXT
);

CREATE INDEX IF NOT EXISTS idx_edges_source ON edges(source_id);
CREATE INDEX IF NOT EXISTS idx_edges_target ON edges(target_id);
CREATE INDEX IF NOT EXISTS idx_edges_type ON edges(edge_type);
CREATE INDEX IF NOT EXISTS idx_edges_confidence ON edges(confidence);

CREATE TABLE IF NOT EXISTS boundary_symbols (
    id         TEXT PRIMARY KEY,
    name       TEXT NOT NULL,
    kind       TEXT NOT NULL,
    language   TEXT NOT NULL,
    package    TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS chunk_mapping (
    symbol_id TEXT NOT NULL REFERENCES symbols(id) ON DELETE CASCADE,
    chunk_id  TEXT NOT NULL,
    UNIQUE(symbol_id, chunk_id)
);
`

// GraphStore manages the graph.db SQLite database.
type GraphStore struct {
	db *sql.DB
}

// NewGraphStore opens or creates a graph.db at the given DSN.
func NewGraphStore(dsn string) (*GraphStore, error) {
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open graph db: %w", err)
	}

	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping graph db: %w", err)
	}

	for _, pragma := range []string{
		"PRAGMA foreign_keys = ON",
		"PRAGMA busy_timeout = 5000",
		"PRAGMA journal_mode = WAL",
	} {
		if _, err := db.Exec(pragma); err != nil {
			db.Close()
			return nil, fmt.Errorf("pragma %q: %w", pragma, err)
		}
	}

	if _, err := db.Exec(graphDDL); err != nil {
		db.Close()
		return nil, fmt.Errorf("apply graph schema: %w", err)
	}

	return &GraphStore{db: db}, nil
}

// Close closes the underlying database connection.
func (s *GraphStore) Close() error {
	return s.db.Close()
}
