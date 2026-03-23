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

// InsertSymbols batch-inserts symbols into the graph store.
func (s *GraphStore) InsertSymbols(symbols []Symbol) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(`INSERT OR REPLACE INTO symbols
		(id, name, kind, language, package, file_path, line_start, line_end, signature, exported, receiver)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, sym := range symbols {
		exported := 0
		if sym.Exported {
			exported = 1
		}
		if _, err := stmt.Exec(sym.ID, sym.Name, sym.Kind, sym.Language, sym.Package,
			sym.FilePath, sym.LineStart, sym.LineEnd, sym.Signature, exported, sym.Receiver); err != nil {
			return fmt.Errorf("insert symbol %s: %w", sym.ID, err)
		}
	}

	return tx.Commit()
}

// InsertEdges batch-inserts edges into the graph store.
func (s *GraphStore) InsertEdges(edges []Edge) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(`INSERT INTO edges
		(source_id, target_id, edge_type, confidence, source_line, metadata)
		VALUES (?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, e := range edges {
		if _, err := stmt.Exec(e.SourceID, e.TargetID, e.EdgeType, e.Confidence, e.SourceLine, e.Metadata); err != nil {
			return fmt.Errorf("insert edge %s->%s: %w", e.SourceID, e.TargetID, err)
		}
	}

	return tx.Commit()
}

// GetSymbol retrieves a single symbol by ID.
func (s *GraphStore) GetSymbol(id string) (*Symbol, error) {
	row := s.db.QueryRow(`SELECT id, name, kind, language, package, file_path,
		line_start, line_end, signature, exported, receiver
		FROM symbols WHERE id = ?`, id)
	return scanSymbol(row)
}

// GetSymbolsByFile returns all symbols in the given file.
func (s *GraphStore) GetSymbolsByFile(filePath string) ([]Symbol, error) {
	rows, err := s.db.Query(`SELECT id, name, kind, language, package, file_path,
		line_start, line_end, signature, exported, receiver
		FROM symbols WHERE file_path = ? ORDER BY line_start`, filePath)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanSymbols(rows)
}

// GetSymbolsByName returns all symbols with the given name.
func (s *GraphStore) GetSymbolsByName(name string) ([]Symbol, error) {
	rows, err := s.db.Query(`SELECT id, name, kind, language, package, file_path,
		line_start, line_end, signature, exported, receiver
		FROM symbols WHERE name = ?`, name)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanSymbols(rows)
}

// GetEdgesFrom returns all edges originating from the given symbol.
func (s *GraphStore) GetEdgesFrom(symbolID string) ([]Edge, error) {
	rows, err := s.db.Query(`SELECT source_id, target_id, edge_type, confidence, source_line, metadata
		FROM edges WHERE source_id = ?`, symbolID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanEdges(rows)
}

// GetEdgesTo returns all edges targeting the given symbol.
func (s *GraphStore) GetEdgesTo(symbolID string) ([]Edge, error) {
	rows, err := s.db.Query(`SELECT source_id, target_id, edge_type, confidence, source_line, metadata
		FROM edges WHERE target_id = ?`, symbolID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanEdges(rows)
}

// scanSymbol scans a single symbol from a row.
func scanSymbol(row *sql.Row) (*Symbol, error) {
	var sym Symbol
	var exported int
	var pkg, sig, recv sql.NullString
	err := row.Scan(&sym.ID, &sym.Name, &sym.Kind, &sym.Language, &pkg,
		&sym.FilePath, &sym.LineStart, &sym.LineEnd, &sig, &exported, &recv)
	if err != nil {
		return nil, err
	}
	sym.Package = pkg.String
	sym.Signature = sig.String
	sym.Receiver = recv.String
	sym.Exported = exported == 1
	return &sym, nil
}

// scanSymbols scans multiple symbols from rows.
func scanSymbols(rows *sql.Rows) ([]Symbol, error) {
	var syms []Symbol
	for rows.Next() {
		var sym Symbol
		var exported int
		var pkg, sig, recv sql.NullString
		if err := rows.Scan(&sym.ID, &sym.Name, &sym.Kind, &sym.Language, &pkg,
			&sym.FilePath, &sym.LineStart, &sym.LineEnd, &sig, &exported, &recv); err != nil {
			return nil, err
		}
		sym.Package = pkg.String
		sym.Signature = sig.String
		sym.Receiver = recv.String
		sym.Exported = exported == 1
		syms = append(syms, sym)
	}
	return syms, rows.Err()
}

// scanEdges scans multiple edges from rows.
func scanEdges(rows *sql.Rows) ([]Edge, error) {
	var edges []Edge
	for rows.Next() {
		var e Edge
		var sourceLine sql.NullInt64
		var metadata sql.NullString
		if err := rows.Scan(&e.SourceID, &e.TargetID, &e.EdgeType, &e.Confidence, &sourceLine, &metadata); err != nil {
			return nil, err
		}
		e.SourceLine = int(sourceLine.Int64)
		e.Metadata = metadata.String
		edges = append(edges, e)
	}
	return edges, rows.Err()
}

// InsertBoundarySymbols batch-inserts boundary symbols.
func (s *GraphStore) InsertBoundarySymbols(bounds []BoundarySymbol) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(`INSERT OR REPLACE INTO boundary_symbols
		(id, name, kind, language, package) VALUES (?, ?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, b := range bounds {
		if _, err := stmt.Exec(b.ID, b.Name, b.Kind, b.Language, b.Package); err != nil {
			return fmt.Errorf("insert boundary symbol %s: %w", b.ID, err)
		}
	}

	return tx.Commit()
}

// InsertChunkMappings links a symbol to one or more LanceDB chunk IDs.
func (s *GraphStore) InsertChunkMappings(symbolID string, chunkIDs []string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(`INSERT OR IGNORE INTO chunk_mapping (symbol_id, chunk_id) VALUES (?, ?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, cid := range chunkIDs {
		if _, err := stmt.Exec(symbolID, cid); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// GetChunkMappingsForSymbol returns LanceDB chunk IDs for a symbol.
func (s *GraphStore) GetChunkMappingsForSymbol(symbolID string) ([]string, error) {
	rows, err := s.db.Query(`SELECT chunk_id FROM chunk_mapping WHERE symbol_id = ?`, symbolID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// SetMeta sets a key-value pair in graph_meta.
func (s *GraphStore) SetMeta(key, value string) error {
	_, err := s.db.Exec(`INSERT OR REPLACE INTO graph_meta (key, value) VALUES (?, ?)`, key, value)
	return err
}

// GetMeta retrieves a value from graph_meta.
func (s *GraphStore) GetMeta(key string) (string, error) {
	var value string
	err := s.db.QueryRow(`SELECT value FROM graph_meta WHERE key = ?`, key).Scan(&value)
	return value, err
}
