package database

import (
	"context"
	"database/sql"
)

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

// MarkWorkflowStartedIfUnset stamps started_at once and preserves the first start time.
func (db *DB) MarkWorkflowStartedIfUnset(ctx context.Context, workflowID string) error {
	return markWorkflowStartedIfUnset(ctx, db.conn, workflowID)
}

// TransitionWorkflowState updates workflow state and lifecycle timestamps.
// For run_only sessions, it also mirrors the session state when appropriate.
func (db *DB) TransitionWorkflowState(ctx context.Context, workflowID, newState string) error {
	tx, err := db.conn.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	if err := markWorkflowStartedIfUnset(ctx, tx, workflowID); err != nil {
		return err
	}

	res, err := tx.ExecContext(ctx, `
		UPDATE workflows
		SET
			current_state = ?,
			completed_at = CASE
				WHEN ? IN ('completed', 'failed') THEN COALESCE(completed_at, datetime('now'))
				ELSE completed_at
			END,
			updated_at = datetime('now')
		WHERE id = ?
	`, newState, newState, workflowID)
	if err != nil {
		return err
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return sql.ErrNoRows
	}

	if err := mirrorRunOnlySessionStateForWorkflow(ctx, tx, workflowID, newState); err != nil {
		return err
	}

	return tx.Commit()
}

func markWorkflowStartedIfUnset(ctx context.Context, q DBTX, workflowID string) error {
	_, err := q.ExecContext(ctx, `
		UPDATE workflows
		SET started_at = COALESCE(started_at, datetime('now')),
		    updated_at = datetime('now')
		WHERE id = ? AND started_at IS NULL
	`, workflowID)
	return err
}

func mirrorRunOnlySessionStateForWorkflow(ctx context.Context, q DBTX, workflowID, workflowState string) error {
	row := q.QueryRowContext(ctx, `
		SELECT s.id, s.kind
		FROM pipeline_runs pr
		JOIN sessions s ON s.id = pr.session_id
		WHERE pr.workflow_id = ?
		LIMIT 1
	`, workflowID)

	var sessionID, kind string
	if err := row.Scan(&sessionID, &kind); err != nil {
		if err == sql.ErrNoRows {
			return nil
		}
		return err
	}
	if kind != SessionKindRunOnly {
		return nil
	}

	var sessionState string
	switch workflowState {
	case "human_review":
		sessionState = SessionStateAwaitingReview
	case "completed":
		sessionState = SessionStateCompleted
	case "failed":
		sessionState = SessionStateFailed
	default:
		sessionState = SessionStateRunning
	}

	return transitionSessionState(ctx, q, sessionID, sessionState, "")
}

// SetWorkflowVerificationAndTransition writes the verification report path and
// transitions the workflow/session into human review in one DB transaction.
func (db *DB) SetWorkflowVerificationAndTransition(ctx context.Context, workflowID string, reportPath sql.NullString) error {
	tx, err := db.conn.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	res, err := tx.ExecContext(ctx, `
		UPDATE workflows
		SET verification_report_path = ?,
		    current_state = 'human_review',
		    updated_at = datetime('now')
		WHERE id = ?
	`, reportPath, workflowID)
	if err != nil {
		return err
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return sql.ErrNoRows
	}

	if err := markWorkflowStartedIfUnset(ctx, tx, workflowID); err != nil {
		return err
	}
	if err := mirrorRunOnlySessionStateForWorkflow(ctx, tx, workflowID, "human_review"); err != nil {
		return err
	}

	return tx.Commit()
}
