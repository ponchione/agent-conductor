package database

import (
	"context"
	"database/sql"
)

// PlanRunUsefulness summarizes one plan run for audit-effectiveness views.
type PlanRunUsefulness struct {
	ID                      string
	SessionID               sql.NullString
	Project                 sql.NullString
	SpecFile                string
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
