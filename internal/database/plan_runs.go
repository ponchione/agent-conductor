package database

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/google/uuid"
)

// PlanRunState constants define the lifecycle states for a plan run.
const (
	PlanRunStatePending         = "pending"
	PlanRunStateGeneratingEpics = "generating_epics"
	PlanRunStateGeneratingTasks = "generating_tasks"
	PlanRunStateAuditing        = "auditing"
	PlanRunStatePersisting      = "persisting"
	PlanRunStateComplete        = "complete"
	PlanRunStateFailed          = "failed"
)

// CreateStubPlanRun inserts a minimal plan_run record and returns its ID.
// This is used by the dashboard submit flow to create a trackable record
// before background plan generation completes.
func (db *DB) CreateStubPlanRun(ctx context.Context, sessionID, project, specFile string) (string, error) {
	id := uuid.New().String()
	if _, err := db.conn.ExecContext(ctx, `
		INSERT INTO plan_runs (id, session_id, project, spec_file, state)
		VALUES (?, ?, ?, ?, ?)
	`, id, nullableString(sessionID), nullableString(project), specFile, PlanRunStatePending); err != nil {
		return "", fmt.Errorf("insert stub plan_run: %w", err)
	}
	return id, nil
}

// UpdatePlanRunState atomically updates the state and current_phase of a plan run.
// The error_message is cleared (set to NULL) on non-failed state transitions.
func (db *DB) UpdatePlanRunState(ctx context.Context, id, state, phase string) error {
	return db.Queries.UpdatePlanRunState(ctx, UpdatePlanRunStateParams{
		ID:           id,
		State:        state,
		CurrentPhase: nullableString(phase),
		ErrorMessage: sql.NullString{},
	})
}

// UpdatePlanRunFailed sets the plan run state to "failed" with the given error message.
// The current_phase is cleared.
func (db *DB) UpdatePlanRunFailed(ctx context.Context, id, errorMessage string) error {
	return db.Queries.UpdatePlanRunState(ctx, UpdatePlanRunStateParams{
		ID:           id,
		State:        PlanRunStateFailed,
		CurrentPhase: sql.NullString{},
		ErrorMessage: nullableString(errorMessage),
	})
}

// GetPlanRun returns the full plan run record by ID, including state tracking fields.
func (db *DB) GetPlanRun(ctx context.Context, id string) (GetPlanRunByIDRow, error) {
	return db.Queries.GetPlanRunByID(ctx, id)
}

// PlanRunUsefulness summarizes one plan run for audit-effectiveness views.
type PlanRunUsefulness struct {
	ID                      string
	SessionID               sql.NullString
	Project                 sql.NullString
	SpecFile                string
	State                   string
	AuditChanged            bool
	WorkOrdersGenerated     sql.NullInt64
	PreAuditWorkOrderCount  sql.NullInt64
	PostAuditWorkOrderCount sql.NullInt64
	WorkOrderDelta          sql.NullInt64
	AuditChangeText         sql.NullString
	CreatedAt               string
}

// PlanAuditChangeStats aggregates how often audit changed plan output.
type PlanAuditChangeStats struct {
	ChangedRuns   int64
	UnchangedRuns int64
	TotalRuns     int64
}

// ListPlanRunUsefulness returns recent plan runs with derived audit-change fields.
func (db *DB) ListPlanRunUsefulness(ctx context.Context, limit int) ([]PlanRunUsefulness, error) {
	query := `
		SELECT
			id,
			session_id,
			project,
			spec_file,
			state,
			CASE
				WHEN COALESCE(audit_work_orders_added, 0) > 0
				  OR COALESCE(audit_work_orders_modified, 0) > 0
				  OR COALESCE(post_audit_work_order_count, 0) != COALESCE(pre_audit_work_order_count, 0)
				  OR COALESCE(audit_change_text, '') != ''
				THEN 1 ELSE 0
			END AS audit_changed,
			work_orders_generated,
			pre_audit_work_order_count,
			post_audit_work_order_count,
			CASE
				WHEN pre_audit_work_order_count IS NOT NULL AND post_audit_work_order_count IS NOT NULL
				THEN post_audit_work_order_count - pre_audit_work_order_count
				ELSE NULL
			END AS work_order_delta,
			audit_change_text,
			created_at
		FROM plan_runs
		ORDER BY created_at DESC
	`
	var args []any
	if limit > 0 {
		query += ` LIMIT ?`
		args = append(args, limit)
	}

	rows, err := db.conn.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []PlanRunUsefulness
	for rows.Next() {
		var row PlanRunUsefulness
		var auditChanged int64
		if err := rows.Scan(
			&row.ID,
			&row.SessionID,
			&row.Project,
			&row.SpecFile,
			&row.State,
			&auditChanged,
			&row.WorkOrdersGenerated,
			&row.PreAuditWorkOrderCount,
			&row.PostAuditWorkOrderCount,
			&row.WorkOrderDelta,
			&row.AuditChangeText,
			&row.CreatedAt,
		); err != nil {
			return nil, err
		}
		row.AuditChanged = auditChanged == 1
		out = append(out, row)
	}

	return out, rows.Err()
}

// PlanRunMetricsUpdate contains all metrics fields that can be updated on an
// existing plan_run row after generation completes. The caller maps planner
// result types to this struct.
type PlanRunMetricsUpdate struct {
	SpecFingerprint          sql.NullString
	GenerationModel          sql.NullString
	EpicGenerationModel      sql.NullString
	TaskGenerationModel      sql.NullString
	AuditModel               sql.NullString
	GenerationSessionID      sql.NullString
	AuditSessionID           sql.NullString
	WorkOrdersGenerated      sql.NullInt64
	EpicCount                sql.NullInt64
	TaskCount                sql.NullInt64
	PreAuditWorkOrderCount   sql.NullInt64
	PostAuditWorkOrderCount  sql.NullInt64
	AuditChangeText          sql.NullString
	AuditWorkOrdersAdded     sql.NullInt64
	AuditWorkOrdersModified  sql.NullInt64
	AuditWorkOrdersUnchanged sql.NullInt64
	GenerationCostUsd        sql.NullFloat64
	EpicGenerationCostUsd    sql.NullFloat64
	TaskGenerationCostUsd    sql.NullFloat64
	AuditCostUsd             sql.NullFloat64
	GenerationDurationMs     sql.NullInt64
	EpicGenerationDurationMs sql.NullInt64
	TaskGenerationDurationMs sql.NullInt64
	AuditDurationMs          sql.NullInt64
	GenerationRetryCount     int64
	GenerationTokensIn       sql.NullInt64
	GenerationTokensOut      sql.NullInt64
	EpicGenerationTokensIn   sql.NullInt64
	EpicGenerationTokensOut  sql.NullInt64
	TaskGenerationCallCount  sql.NullInt64
	TaskGenerationTokensIn   sql.NullInt64
	TaskGenerationTokensOut  sql.NullInt64
	AuditTokensIn            sql.NullInt64
	AuditTokensOut           sql.NullInt64
}

// UpdatePlanRunMetrics updates the metrics columns on an existing plan_run row.
// This is used by the background execution path where a stub row is created
// first (via CreateStubPlanRun) and metrics are filled in after generation.
func (db *DB) UpdatePlanRunMetrics(ctx context.Context, id string, m PlanRunMetricsUpdate) error {
	_, err := db.conn.ExecContext(ctx, `
		UPDATE plan_runs SET
			spec_fingerprint = ?,
			generation_model = ?, epic_generation_model = ?, task_generation_model = ?, audit_model = ?,
			generation_session_id = ?, audit_session_id = ?,
			work_orders_generated = ?, epic_count = ?, task_count = ?,
			pre_audit_work_order_count = ?, post_audit_work_order_count = ?,
			audit_change_text = ?,
			audit_work_orders_added = ?, audit_work_orders_modified = ?, audit_work_orders_unchanged = ?,
			generation_cost_usd = ?, epic_generation_cost_usd = ?, task_generation_cost_usd = ?, audit_cost_usd = ?,
			generation_duration_ms = ?, epic_generation_duration_ms = ?, task_generation_duration_ms = ?, audit_duration_ms = ?,
			generation_retry_count = ?,
			generation_tokens_in = ?, generation_tokens_out = ?,
			epic_generation_tokens_in = ?, epic_generation_tokens_out = ?,
			task_generation_call_count = ?, task_generation_tokens_in = ?, task_generation_tokens_out = ?,
			audit_tokens_in = ?, audit_tokens_out = ?
		WHERE id = ?
	`,
		m.SpecFingerprint,
		m.GenerationModel, m.EpicGenerationModel, m.TaskGenerationModel, m.AuditModel,
		m.GenerationSessionID, m.AuditSessionID,
		m.WorkOrdersGenerated, m.EpicCount, m.TaskCount,
		m.PreAuditWorkOrderCount, m.PostAuditWorkOrderCount,
		m.AuditChangeText,
		m.AuditWorkOrdersAdded, m.AuditWorkOrdersModified, m.AuditWorkOrdersUnchanged,
		m.GenerationCostUsd, m.EpicGenerationCostUsd, m.TaskGenerationCostUsd, m.AuditCostUsd,
		m.GenerationDurationMs, m.EpicGenerationDurationMs, m.TaskGenerationDurationMs, m.AuditDurationMs,
		m.GenerationRetryCount,
		m.GenerationTokensIn, m.GenerationTokensOut,
		m.EpicGenerationTokensIn, m.EpicGenerationTokensOut,
		m.TaskGenerationCallCount, m.TaskGenerationTokensIn, m.TaskGenerationTokensOut,
		m.AuditTokensIn, m.AuditTokensOut,
		id,
	)
	if err != nil {
		return fmt.Errorf("update plan_run metrics: %w", err)
	}
	return nil
}

// GetPlanAuditChangeStats returns changed vs unchanged counts across plan runs.
func (db *DB) GetPlanAuditChangeStats(ctx context.Context) (PlanAuditChangeStats, error) {
	row := db.conn.QueryRowContext(ctx, `
		SELECT
			SUM(CASE
				WHEN COALESCE(audit_work_orders_added, 0) > 0
				  OR COALESCE(audit_work_orders_modified, 0) > 0
				  OR COALESCE(post_audit_work_order_count, 0) != COALESCE(pre_audit_work_order_count, 0)
				  OR COALESCE(audit_change_text, '') != ''
				THEN 1 ELSE 0
			END) AS changed_runs,
			SUM(CASE
				WHEN COALESCE(audit_work_orders_added, 0) = 0
				  AND COALESCE(audit_work_orders_modified, 0) = 0
				  AND (
					pre_audit_work_order_count IS NULL
					OR post_audit_work_order_count IS NULL
					OR COALESCE(post_audit_work_order_count, 0) = COALESCE(pre_audit_work_order_count, 0)
				  )
				  AND COALESCE(audit_change_text, '') = ''
				THEN 1 ELSE 0
			END) AS unchanged_runs,
			COUNT(*) AS total_runs
		FROM plan_runs
	`)

	var stats PlanAuditChangeStats
	var changedRuns, unchangedRuns, totalRuns sql.NullInt64
	if err := row.Scan(&changedRuns, &unchangedRuns, &totalRuns); err != nil {
		return PlanAuditChangeStats{}, err
	}
	stats.ChangedRuns = changedRuns.Int64
	stats.UnchangedRuns = unchangedRuns.Int64
	stats.TotalRuns = totalRuns.Int64
	return stats, nil
}
