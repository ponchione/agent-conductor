package database

import "context"

// WorkflowSummary holds the fields needed for list/resolve operations.
type WorkflowSummary struct {
	ID           string
	CurrentState string
	GitBranch    string
	CreatedAt    string
}

// ListWorkflows returns workflows ordered newest-first.
// If state is non-empty, filters by current_state. If limit is 0, returns all.
func (db *DB) ListWorkflows(ctx context.Context, state string, limit int) ([]WorkflowSummary, error) {
	q := `SELECT id, current_state, git_branch, created_at FROM workflows`
	var args []any
	if state != "" {
		q += ` WHERE current_state = ?`
		args = append(args, state)
	}
	q += ` ORDER BY created_at DESC`
	if limit > 0 {
		q += ` LIMIT ?`
		args = append(args, limit)
	}
	rows, err := db.conn.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []WorkflowSummary
	for rows.Next() {
		var s WorkflowSummary
		if err := rows.Scan(&s.ID, &s.CurrentState, &s.GitBranch, &s.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// FindWorkflowsByPrefix returns workflow IDs whose id begins with prefix.
func (db *DB) FindWorkflowsByPrefix(ctx context.Context, prefix string) ([]string, error) {
	rows, err := db.conn.QueryContext(ctx,
		`SELECT id FROM workflows WHERE id LIKE ? ORDER BY created_at DESC`,
		prefix+"%")
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
