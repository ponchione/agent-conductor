package database

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
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

// UIWorkflowSummary holds the fields needed for workflow list UI.
type UIWorkflowSummary struct {
	ID                   string
	CurrentState         string
	WorkOrderTitle       string
	WorkOrderType        sql.NullString
	Project              string
	SessionID            sql.NullString
	GitBranch            string
	VerifyResult         sql.NullString
	HumanResult          sql.NullString
	TotalCostUSD         sql.NullFloat64
	TotalDurationSeconds sql.NullInt64
	CreatedAt            string
	UpdatedAt            string
}

// ListWorkflowsForUI returns workflows with their latest pipeline run data for
// the observability UI. Results are sorted by state priority (running first,
// then awaiting review, then everything else) and recency.
func (db *DB) ListWorkflowsForUI(ctx context.Context, status, project, sessionID string, limit, offset int) ([]UIWorkflowSummary, int, error) {
	// Build WHERE clause dynamically.
	var conditions []string
	var args []any
	if status != "" {
		conditions = append(conditions, `w.current_state = ?`)
		args = append(args, status)
	}
	if project != "" {
		conditions = append(conditions, `pr.project = ?`)
		args = append(args, project)
	}
	if sessionID != "" {
		conditions = append(conditions, `pr.session_id = ?`)
		args = append(args, sessionID)
	}

	whereClause := ""
	if len(conditions) > 0 {
		whereClause = " WHERE " + strings.Join(conditions, " AND ")
	}

	// Count total matching rows.
	countQuery := `
		SELECT COUNT(*)
		FROM workflows w
		LEFT JOIN (
			SELECT pr1.*
			FROM pipeline_runs pr1
			INNER JOIN (
				SELECT workflow_id, MAX(created_at) AS max_created
				FROM pipeline_runs
				GROUP BY workflow_id
			) pr2 ON pr1.workflow_id = pr2.workflow_id AND pr1.created_at = pr2.max_created
		) pr ON pr.workflow_id = w.id` + whereClause

	var total int
	if err := db.conn.QueryRowContext(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count workflows: %w", err)
	}

	// Main query.
	q := `
		SELECT
			w.id,
			w.current_state,
			pr.work_order_content,
			pr.work_order_type,
			COALESCE(pr.project, ''),
			pr.session_id,
			w.git_branch,
			pr.verify_result,
			pr.human_result,
			COALESCE(pr.build_cost_usd, 0) + COALESCE(
				(SELECT SUM(sc.estimated_cost_usd) FROM sub_calls sc WHERE sc.pipeline_run_id = pr.id), 0
			),
			CAST((julianday(COALESCE(pr.verify_completed_at, pr.build_completed_at, pr.scope_completed_at)) - julianday(pr.scope_started_at)) * 86400 AS INTEGER),
			w.created_at,
			w.updated_at
		FROM workflows w
		LEFT JOIN (
			SELECT pr1.*
			FROM pipeline_runs pr1
			INNER JOIN (
				SELECT workflow_id, MAX(created_at) AS max_created
				FROM pipeline_runs
				GROUP BY workflow_id
			) pr2 ON pr1.workflow_id = pr2.workflow_id AND pr1.created_at = pr2.max_created
		) pr ON pr.workflow_id = w.id` + whereClause + `
		ORDER BY
			CASE
				WHEN w.current_state = 'running' THEN 0
				WHEN w.current_state IN ('human_review', 'awaiting_review') THEN 1
				ELSE 2
			END,
			w.updated_at DESC
		LIMIT ? OFFSET ?`

	if limit <= 0 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}
	args = append(args, limit, offset)

	rows, err := db.conn.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("list workflows: %w", err)
	}
	defer rows.Close()

	var out []UIWorkflowSummary
	for rows.Next() {
		var s UIWorkflowSummary
		var workOrderContent sql.NullString
		if err := rows.Scan(
			&s.ID,
			&s.CurrentState,
			&workOrderContent,
			&s.WorkOrderType,
			&s.Project,
			&s.SessionID,
			&s.GitBranch,
			&s.VerifyResult,
			&s.HumanResult,
			&s.TotalCostUSD,
			&s.TotalDurationSeconds,
			&s.CreatedAt,
			&s.UpdatedAt,
		); err != nil {
			return nil, 0, fmt.Errorf("scan workflow: %w", err)
		}
		s.WorkOrderTitle = parseWorkOrderTitle(workOrderContent, s.WorkOrderType)
		out = append(out, s)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}

	return out, total, nil
}

// parseWorkOrderTitle extracts the title from work_order_content YAML or falls
// back to work_order_type.
func parseWorkOrderTitle(content sql.NullString, woType sql.NullString) string {
	if content.Valid && content.String != "" {
		for _, line := range strings.Split(content.String, "\n") {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "title:") {
				title := strings.TrimSpace(strings.TrimPrefix(trimmed, "title:"))
				title = strings.Trim(title, `"'`)
				if title != "" {
					return title
				}
			}
		}
	}
	if woType.Valid && woType.String != "" {
		return woType.String
	}
	return ""
}

// UIWorkflowDetail holds workflow-level fields for the detail view.
type UIWorkflowDetail struct {
	ID             string
	CurrentState   string
	OriginalIntent string
	OriginalFile   string
	GitBranch      string
	TargetRepo     string
	CreatedAt      string
	UpdatedAt      string
	ErrorMessage   sql.NullString
}

// UIPipelineRunDetail holds pipeline run fields for the detail view.
type UIPipelineRunDetail struct {
	ID                       string
	WorkOrderType            sql.NullString
	WorkOrderContent         sql.NullString
	VerifyResult             sql.NullString
	HumanResult              sql.NullString
	ScopeStartedAt           sql.NullString
	ScopeCompletedAt         sql.NullString
	BuildStartedAt           sql.NullString
	BuildCompletedAt         sql.NullString
	VerifyStartedAt          sql.NullString
	VerifyCompletedAt        sql.NullString
	ScopeModel               sql.NullString
	BuildModel               sql.NullString
	VerifyModel              sql.NullString
	ScopeFilesSuggested      sql.NullInt64
	BuildFilesChanged        sql.NullInt64
	BuildScopeDrift          int
	ScopeEstimatedComplexity sql.NullString
	ScopeRagDirect           sql.NullInt64
	ScopeRagHops             sql.NullInt64
	ScopePathsStripped       sql.NullInt64
	ScopePathsReclassified   sql.NullInt64
	BuildCostUSD             sql.NullFloat64
	BuildToolCalls           sql.NullString
}

// UISubCall holds one sub_call row for the detail view.
type UISubCall struct {
	Phase            string
	Step             string
	Provider         string
	Model            string
	TokensIn         int
	TokensOut        int
	LatencyMs        int
	EstimatedCostUSD float64
	Success          bool
	ErrorMessage     sql.NullString
}

// GetWorkflowDetailForUI returns a workflow, its most recent pipeline run, and
// the sub_calls for that pipeline run.
func (db *DB) GetWorkflowDetailForUI(ctx context.Context, workflowID string) (UIWorkflowDetail, *UIPipelineRunDetail, []UISubCall, error) {
	// Fetch workflow.
	row := db.conn.QueryRowContext(ctx, `
		SELECT
			id, current_state, original_intent, original_file,
			git_branch, target_repo, created_at, updated_at, error_message
		FROM workflows
		WHERE id = ?
		LIMIT 1
	`, workflowID)

	var w UIWorkflowDetail
	if err := row.Scan(
		&w.ID,
		&w.CurrentState,
		&w.OriginalIntent,
		&w.OriginalFile,
		&w.GitBranch,
		&w.TargetRepo,
		&w.CreatedAt,
		&w.UpdatedAt,
		&w.ErrorMessage,
	); err != nil {
		return UIWorkflowDetail{}, nil, nil, err
	}

	// Fetch most recent pipeline run.
	prRow := db.conn.QueryRowContext(ctx, `
		SELECT
			id, work_order_type, work_order_content,
			verify_result, human_result,
			scope_started_at, scope_completed_at,
			build_started_at, build_completed_at,
			verify_started_at, verify_completed_at,
			scope_model, build_model, verify_model,
			scope_files_suggested, build_files_changed,
			COALESCE(build_scope_drift, 0),
			scope_estimated_complexity,
			scope_rag_direct, scope_rag_hops,
			scope_paths_stripped, scope_paths_reclassified,
			build_cost_usd, build_tool_calls
		FROM pipeline_runs
		WHERE workflow_id = ?
		ORDER BY created_at DESC
		LIMIT 1
	`, workflowID)

	var pr UIPipelineRunDetail
	err := prRow.Scan(
		&pr.ID,
		&pr.WorkOrderType,
		&pr.WorkOrderContent,
		&pr.VerifyResult,
		&pr.HumanResult,
		&pr.ScopeStartedAt,
		&pr.ScopeCompletedAt,
		&pr.BuildStartedAt,
		&pr.BuildCompletedAt,
		&pr.VerifyStartedAt,
		&pr.VerifyCompletedAt,
		&pr.ScopeModel,
		&pr.BuildModel,
		&pr.VerifyModel,
		&pr.ScopeFilesSuggested,
		&pr.BuildFilesChanged,
		&pr.BuildScopeDrift,
		&pr.ScopeEstimatedComplexity,
		&pr.ScopeRagDirect,
		&pr.ScopeRagHops,
		&pr.ScopePathsStripped,
		&pr.ScopePathsReclassified,
		&pr.BuildCostUSD,
		&pr.BuildToolCalls,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			// Workflow exists but has no pipeline runs.
			return w, nil, nil, nil
		}
		return UIWorkflowDetail{}, nil, nil, fmt.Errorf("get pipeline run: %w", err)
	}

	// Fetch sub_calls for the pipeline run.
	scRows, err := db.conn.QueryContext(ctx, `
		SELECT
			phase, step, provider, model,
			tokens_in, tokens_out, latency_ms,
			estimated_cost_usd, success, error_message
		FROM sub_calls
		WHERE pipeline_run_id = ?
		ORDER BY created_at
	`, pr.ID)
	if err != nil {
		return UIWorkflowDetail{}, nil, nil, fmt.Errorf("list sub_calls: %w", err)
	}
	defer scRows.Close()

	var subCalls []UISubCall
	for scRows.Next() {
		var sc UISubCall
		var success int
		if err := scRows.Scan(
			&sc.Phase,
			&sc.Step,
			&sc.Provider,
			&sc.Model,
			&sc.TokensIn,
			&sc.TokensOut,
			&sc.LatencyMs,
			&sc.EstimatedCostUSD,
			&success,
			&sc.ErrorMessage,
		); err != nil {
			return UIWorkflowDetail{}, nil, nil, fmt.Errorf("scan sub_call: %w", err)
		}
		sc.Success = success == 1
		subCalls = append(subCalls, sc)
	}
	if err := scRows.Err(); err != nil {
		return UIWorkflowDetail{}, nil, nil, err
	}

	return w, &pr, subCalls, nil
}
