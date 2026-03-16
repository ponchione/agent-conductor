package database

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/google/uuid"
)

const (
	SessionKindPlanOnly = "plan_only"
	SessionKindRunOnly  = "run_only"

	SessionStateRunning        = "running"
	SessionStateAwaitingReview = "awaiting_review"
	SessionStateCompleted      = "completed"
	SessionStateFailed         = "failed"
	SessionStatePartial        = "partial"
)

// Session is the top-level parent entity for a user-initiated plan or run.
type Session struct {
	ID             string
	Kind           string
	Project        string
	SourceSpecPath sql.NullString
	State          string
	ErrorMessage   sql.NullString
	StartedAt      sql.NullString
	CompletedAt    sql.NullString
	CreatedAt      string
	UpdatedAt      string
}

// SessionSummary is the list/detail-friendly shape for polling UI screens.
type SessionSummary struct {
	Session
	PlanRunsCount       int64
	PipelineRunsCount   int64
	LatestWorkflowState sql.NullString
}

// SessionPlanRun is the session-scoped view of a plan run.
type SessionPlanRun struct {
	ID                      string
	SpecFile                string
	Project                 sql.NullString
	SpecFingerprint         sql.NullString
	WorkOrdersGenerated     sql.NullInt64
	PreAuditWorkOrderCount  sql.NullInt64
	PostAuditWorkOrderCount sql.NullInt64
	AuditChangeText         sql.NullString
	CreatedAt               string
}

// SessionPipelineRun is the session-scoped view of a pipeline run.
type SessionPipelineRun struct {
	ID            string
	WorkflowID    string
	Project       string
	WorkOrderType sql.NullString
	CreatedAt     string
}

// SessionDetail is the polling-friendly detail shape for one session.
type SessionDetail struct {
	Session      Session
	PlanRuns     []SessionPlanRun
	PipelineRuns []SessionPipelineRun
	Artifacts    []Artifact
}

// StartSession creates a session in running state and returns its ID.
func (db *DB) StartSession(ctx context.Context, kind, project, sourceSpecPath string) (string, error) {
	id := uuid.New().String()
	if _, err := db.conn.ExecContext(ctx, `
		INSERT INTO sessions (
			id, kind, project, source_spec_path, state, started_at
		) VALUES (?, ?, ?, ?, ?, datetime('now'))
	`, id, kind, project, nullableString(sourceSpecPath), SessionStateRunning); err != nil {
		return "", fmt.Errorf("insert session: %w", err)
	}
	return id, nil
}

// TransitionSessionState updates session state and stamps completion on terminal states.
func (db *DB) TransitionSessionState(ctx context.Context, sessionID, newState, errorMessage string) error {
	return transitionSessionState(ctx, db.conn, sessionID, newState, errorMessage)
}

// GetSession returns a session by ID.
func (db *DB) GetSession(ctx context.Context, sessionID string) (Session, error) {
	row := db.conn.QueryRowContext(ctx, `
		SELECT
			id, kind, project, source_spec_path, state, error_message,
			started_at, completed_at, created_at, updated_at
		FROM sessions
		WHERE id = ?
		LIMIT 1
	`, sessionID)

	var s Session
	err := row.Scan(
		&s.ID,
		&s.Kind,
		&s.Project,
		&s.SourceSpecPath,
		&s.State,
		&s.ErrorMessage,
		&s.StartedAt,
		&s.CompletedAt,
		&s.CreatedAt,
		&s.UpdatedAt,
	)
	return s, err
}

// FindSessionsByPrefix returns session IDs whose id begins with prefix.
func (db *DB) FindSessionsByPrefix(ctx context.Context, prefix string) ([]string, error) {
	rows, err := db.conn.QueryContext(ctx,
		`SELECT id FROM sessions WHERE id LIKE ? ORDER BY created_at DESC`,
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

// ListSessions returns sessions ordered newest first.
func (db *DB) ListSessions(ctx context.Context, state string, limit int) ([]SessionSummary, error) {
	q := `
		SELECT
			s.id,
			s.kind,
			s.project,
			s.source_spec_path,
			s.state,
			s.error_message,
			s.started_at,
			s.completed_at,
			s.created_at,
			s.updated_at,
			(SELECT COUNT(*) FROM plan_runs pr WHERE pr.session_id = s.id) AS plan_runs_count,
			(SELECT COUNT(*) FROM pipeline_runs pr WHERE pr.session_id = s.id) AS pipeline_runs_count,
			(
				SELECT w.current_state
				FROM pipeline_runs pr
				JOIN workflows w ON w.id = pr.workflow_id
				WHERE pr.session_id = s.id
				ORDER BY w.created_at DESC
				LIMIT 1
			) AS latest_workflow_state
		FROM sessions s`

	var args []any
	if state != "" {
		q += ` WHERE s.state = ?`
		args = append(args, state)
	}
	q += ` ORDER BY s.created_at DESC`
	if limit > 0 {
		q += ` LIMIT ?`
		args = append(args, limit)
	}

	rows, err := db.conn.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []SessionSummary
	for rows.Next() {
		var s SessionSummary
		if err := rows.Scan(
			&s.ID,
			&s.Kind,
			&s.Project,
			&s.SourceSpecPath,
			&s.State,
			&s.ErrorMessage,
			&s.StartedAt,
			&s.CompletedAt,
			&s.CreatedAt,
			&s.UpdatedAt,
			&s.PlanRunsCount,
			&s.PipelineRunsCount,
			&s.LatestWorkflowState,
		); err != nil {
			return nil, err
		}
		out = append(out, s)
	}

	return out, rows.Err()
}

// ListPlanRunsBySession returns plan runs linked to a session, newest first.
func (db *DB) ListPlanRunsBySession(ctx context.Context, sessionID string) ([]SessionPlanRun, error) {
	rows, err := db.conn.QueryContext(ctx, `
		SELECT
			id,
			spec_file,
			project,
			spec_fingerprint,
			work_orders_generated,
			pre_audit_work_order_count,
			post_audit_work_order_count,
			audit_change_text,
			created_at
		FROM plan_runs
		WHERE session_id = ?
		ORDER BY created_at DESC
	`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []SessionPlanRun
	for rows.Next() {
		var run SessionPlanRun
		if err := rows.Scan(
			&run.ID,
			&run.SpecFile,
			&run.Project,
			&run.SpecFingerprint,
			&run.WorkOrdersGenerated,
			&run.PreAuditWorkOrderCount,
			&run.PostAuditWorkOrderCount,
			&run.AuditChangeText,
			&run.CreatedAt,
		); err != nil {
			return nil, err
		}
		out = append(out, run)
	}
	return out, rows.Err()
}

// ListPipelineRunsBySession returns pipeline runs linked to a session, newest first.
func (db *DB) ListPipelineRunsBySession(ctx context.Context, sessionID string) ([]SessionPipelineRun, error) {
	rows, err := db.conn.QueryContext(ctx, `
		SELECT id, workflow_id, project, work_order_type, created_at
		FROM pipeline_runs
		WHERE session_id = ?
		ORDER BY created_at DESC
	`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []SessionPipelineRun
	for rows.Next() {
		var run SessionPipelineRun
		if err := rows.Scan(&run.ID, &run.WorkflowID, &run.Project, &run.WorkOrderType, &run.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, run)
	}
	return out, rows.Err()
}

// GetSessionIDForWorkflow returns the session linked to a workflow through pipeline_runs.
func (db *DB) GetSessionIDForWorkflow(ctx context.Context, workflowID string) (string, error) {
	row := db.conn.QueryRowContext(ctx, `
		SELECT pr.session_id
		FROM pipeline_runs pr
		WHERE pr.workflow_id = ? AND pr.session_id IS NOT NULL
		LIMIT 1
	`, workflowID)

	var sessionID string
	err := row.Scan(&sessionID)
	return sessionID, err
}

// GetSessionDetail returns one session plus linked runs and artifacts.
func (db *DB) GetSessionDetail(ctx context.Context, sessionID string) (SessionDetail, error) {
	session, err := db.GetSession(ctx, sessionID)
	if err != nil {
		return SessionDetail{}, err
	}

	planRuns, err := db.ListPlanRunsBySession(ctx, sessionID)
	if err != nil {
		return SessionDetail{}, err
	}

	pipelineRuns, err := db.ListPipelineRunsBySession(ctx, sessionID)
	if err != nil {
		return SessionDetail{}, err
	}

	artifacts, err := db.ListArtifactsBySession(ctx, sessionID)
	if err != nil {
		return SessionDetail{}, err
	}

	return SessionDetail{
		Session:      session,
		PlanRuns:     planRuns,
		PipelineRuns: pipelineRuns,
		Artifacts:    artifacts,
	}, nil
}

// LinkPlanRunToSession associates an existing plan_run row with a session.
func (db *DB) LinkPlanRunToSession(ctx context.Context, planRunID, sessionID string) error {
	return linkRunToSession(ctx, db.conn, "plan_runs", planRunID, sessionID)
}

// LinkPipelineRunToSession associates an existing pipeline_run row with a session.
func (db *DB) LinkPipelineRunToSession(ctx context.Context, pipelineRunID, sessionID string) error {
	return linkRunToSession(ctx, db.conn, "pipeline_runs", pipelineRunID, sessionID)
}

func linkRunToSession(ctx context.Context, q DBTX, tableName, rowID, sessionID string) error {
	res, err := q.ExecContext(ctx,
		fmt.Sprintf("UPDATE %s SET session_id = ? WHERE id = ?", tableName),
		sessionID, rowID,
	)
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
	return nil
}

func transitionSessionState(ctx context.Context, q DBTX, sessionID, newState, errorMessage string) error {
	res, err := q.ExecContext(ctx, `
		UPDATE sessions
		SET
			state = ?,
			error_message = ?,
			completed_at = CASE
				WHEN ? IN (?, ?, ?) THEN COALESCE(completed_at, datetime('now'))
				ELSE completed_at
			END,
			updated_at = datetime('now')
		WHERE id = ?
	`, newState, nullableString(errorMessage), newState, SessionStateCompleted, SessionStateFailed, SessionStatePartial, sessionID)
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
	return nil
}

func nullableString(s string) sql.NullString {
	return sql.NullString{String: s, Valid: s != ""}
}
