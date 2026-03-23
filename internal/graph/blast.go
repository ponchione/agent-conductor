package graph

import (
	"database/sql"
	"fmt"
	"strings"
)

// BlastRadius performs a blast radius query from the target symbol.
func (s *GraphStore) BlastRadius(req BlastRadiusRequest) (*BlastRadiusResult, error) {
	if req.MaxDepth <= 0 {
		req.MaxDepth = 3
	}
	if req.Budget <= 0 {
		req.Budget = 30
	}
	if req.MinConfidence <= 0 {
		req.MinConfidence = 0.5
	}

	target, err := s.resolveTarget(req.TargetSymbol)
	if err != nil {
		return nil, fmt.Errorf("resolve target %q: %w", req.TargetSymbol, err)
	}

	result := &BlastRadiusResult{Target: *target}

	if req.Direction == Upstream || req.Direction == Both {
		nodes, err := s.blastUpstream(target.ID, req)
		if err != nil {
			return nil, fmt.Errorf("upstream blast: %w", err)
		}
		result.Upstream = nodes
	}

	if req.Direction == Downstream || req.Direction == Both {
		nodes, err := s.blastDownstream(target.ID, req)
		if err != nil {
			return nil, fmt.Errorf("downstream blast: %w", err)
		}
		result.Downstream = nodes
	}

	if req.Direction == Both || req.Direction == Upstream {
		ifaces, err := s.getInterfaces(target.ID)
		if err != nil {
			return nil, fmt.Errorf("interfaces: %w", err)
		}
		result.Interfaces = ifaces
	}

	return result, nil
}

// resolveTarget finds a symbol by exact ID or by name.
func (s *GraphStore) resolveTarget(target string) (*Symbol, error) {
	sym, err := s.GetSymbol(target)
	if err == nil {
		return sym, nil
	}
	if err != sql.ErrNoRows {
		return nil, err
	}

	// Fuzzy match by name
	syms, err := s.GetSymbolsByName(target)
	if err != nil {
		return nil, err
	}
	if len(syms) == 0 {
		return nil, fmt.Errorf("symbol not found: %s", target)
	}
	return &syms[0], nil
}

// blastUpstream returns symbols that depend on the target (callers, importers).
func (s *GraphStore) blastUpstream(targetID string, req BlastRadiusRequest) ([]BlastRadiusNode, error) {
	edgeFilter := buildEdgeFilter(req.EdgeTypes)
	testFilter := ""
	if !req.IncludeTests {
		testFilter = "AND s.file_path NOT LIKE '%_test.go' AND s.file_path NOT LIKE '%.test.ts' AND s.file_path NOT LIKE '%test_%'"
	}

	query := fmt.Sprintf(`
		WITH RECURSIVE blast(symbol_id, depth, confidence, path, edge_type) AS (
			SELECT e.source_id, 1, e.confidence, e.source_id, e.edge_type
			FROM edges e
			WHERE e.target_id = ?
			  AND e.confidence >= ?
			  %s

			UNION ALL

			SELECT e.source_id, b.depth + 1, e.confidence,
			       b.path || '>' || e.source_id, e.edge_type
			FROM edges e
			JOIN blast b ON e.target_id = b.symbol_id
			WHERE b.depth < ?
			  AND e.confidence >= ?
			  AND e.source_id NOT IN (SELECT id FROM boundary_symbols)
			  AND instr(b.path, e.source_id) = 0
			  AND e.source_id != ?
			  %s
		)
		SELECT s.id, s.name, s.kind, s.language, s.package, s.file_path,
		       s.line_start, s.line_end, s.signature, s.exported, s.receiver,
		       b.depth, b.confidence, b.path, b.edge_type
		FROM blast b
		JOIN symbols s ON b.symbol_id = s.id
		WHERE 1=1 %s
		GROUP BY s.id
		HAVING b.depth = MIN(b.depth)
		ORDER BY b.depth ASC, b.confidence DESC
		LIMIT ?`,
		edgeFilter, edgeFilter, testFilter)

	rows, err := s.db.Query(query, targetID, req.MinConfidence,
		req.MaxDepth, req.MinConfidence, targetID, req.Budget)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return scanBlastNodes(rows)
}

// blastDownstream returns symbols the target depends on (callees, dependencies).
func (s *GraphStore) blastDownstream(targetID string, req BlastRadiusRequest) ([]BlastRadiusNode, error) {
	edgeFilter := buildEdgeFilter(req.EdgeTypes)
	testFilter := ""
	if !req.IncludeTests {
		testFilter = "AND s.file_path NOT LIKE '%_test.go' AND s.file_path NOT LIKE '%.test.ts' AND s.file_path NOT LIKE '%test_%'"
	}

	query := fmt.Sprintf(`
		WITH RECURSIVE blast(symbol_id, depth, confidence, path, edge_type) AS (
			SELECT e.target_id, 1, e.confidence, e.target_id, e.edge_type
			FROM edges e
			WHERE e.source_id = ?
			  AND e.confidence >= ?
			  AND e.target_id NOT IN (SELECT id FROM boundary_symbols)
			  AND e.edge_type != 'IMPLEMENTS'
			  %s

			UNION ALL

			SELECT e.target_id, b.depth + 1, e.confidence,
			       b.path || '>' || e.target_id, e.edge_type
			FROM edges e
			JOIN blast b ON e.source_id = b.symbol_id
			WHERE b.depth < ?
			  AND e.confidence >= ?
			  AND e.target_id NOT IN (SELECT id FROM boundary_symbols)
			  AND instr(b.path, e.target_id) = 0
			  AND e.target_id != ?
			  AND e.edge_type != 'IMPLEMENTS'
			  %s
		)
		SELECT s.id, s.name, s.kind, s.language, s.package, s.file_path,
		       s.line_start, s.line_end, s.signature, s.exported, s.receiver,
		       b.depth, b.confidence, b.path, b.edge_type
		FROM blast b
		JOIN symbols s ON b.symbol_id = s.id
		WHERE 1=1 %s
		GROUP BY s.id
		HAVING b.depth = MIN(b.depth)
		ORDER BY b.depth ASC, b.confidence DESC
		LIMIT ?`,
		edgeFilter, edgeFilter, testFilter)

	rows, err := s.db.Query(query, targetID, req.MinConfidence,
		req.MaxDepth, req.MinConfidence, targetID, req.Budget)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return scanBlastNodes(rows)
}

func buildEdgeFilter(edgeTypes []string) string {
	if len(edgeTypes) == 0 {
		return ""
	}
	quoted := make([]string, len(edgeTypes))
	for i, t := range edgeTypes {
		quoted[i] = fmt.Sprintf("'%s'", t)
	}
	return fmt.Sprintf("AND e.edge_type IN (%s)", strings.Join(quoted, ","))
}

func scanBlastNodes(rows *sql.Rows) ([]BlastRadiusNode, error) {
	var nodes []BlastRadiusNode
	for rows.Next() {
		var sym Symbol
		var node BlastRadiusNode
		var exported int
		var pkg, sig, recv, pathStr sql.NullString

		if err := rows.Scan(&sym.ID, &sym.Name, &sym.Kind, &sym.Language, &pkg,
			&sym.FilePath, &sym.LineStart, &sym.LineEnd, &sig, &exported, &recv,
			&node.Depth, &node.Confidence, &pathStr, &node.EdgeType); err != nil {
			return nil, err
		}

		sym.Package = pkg.String
		sym.Signature = sig.String
		sym.Receiver = recv.String
		sym.Exported = exported == 1
		node.Symbol = sym

		if pathStr.Valid {
			node.Path = strings.Split(pathStr.String, ">")
		}

		nodes = append(nodes, node)
	}
	return nodes, rows.Err()
}

// getInterfaces returns interfaces the target symbol implements.
func (s *GraphStore) getInterfaces(symbolID string) ([]Symbol, error) {
	rows, err := s.db.Query(`
		SELECT s.id, s.name, s.kind, s.language, s.package, s.file_path,
		       s.line_start, s.line_end, s.signature, s.exported, s.receiver
		FROM edges e
		JOIN symbols s ON e.target_id = s.id
		WHERE e.source_id = ? AND e.edge_type = 'IMPLEMENTS'`, symbolID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanSymbols(rows)
}
