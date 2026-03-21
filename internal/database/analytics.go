package database

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// ParseTimeRange converts a range string (7d, 30d, 90d, all) to a time boundary.
// Returns zero time for "all". Defaults to 30d for unrecognized values.
func ParseTimeRange(rangeStr string) time.Time {
	switch rangeStr {
	case "7d":
		return time.Now().UTC().AddDate(0, 0, -7)
	case "90d":
		return time.Now().UTC().AddDate(0, 0, -90)
	case "all":
		return time.Time{}
	default:
		return time.Now().UTC().AddDate(0, 0, -30)
	}
}

// PipelineSummary holds aggregate pipeline statistics.
type PipelineSummary struct {
	TotalRuns                int64
	PassCount                int64
	FailCount                int64
	ErrorCount               int64
	PassRate                 float64
	AvgCostUSD               float64
	AvgDurationSeconds       float64
	TotalCostUSD             float64
	VerifyHumanAgreementRate float64
}

// PlanSummary holds aggregate plan run statistics.
type PlanSummary struct {
	TotalRuns            int64
	AvgGenerationCostUSD float64
	AvgAuditCostUSD      float64
	TotalCostUSD         float64
	AvgWorkOrdersPerPlan float64
	AvgAuditDelta        float64
}

// GetPipelineSummary returns aggregate pipeline stats for the given time range and optional project filter.
func (db *DB) GetPipelineSummary(ctx context.Context, since time.Time, project string) (PipelineSummary, error) {
	whereClause, args := buildWhereClause(since, project, "pr")

	query := fmt.Sprintf(`
		SELECT
			COUNT(*) AS total_runs,
			COALESCE(SUM(CASE WHEN pr.verify_result = 'PASS' THEN 1 ELSE 0 END), 0) AS pass_count,
			COALESCE(SUM(CASE WHEN pr.verify_result = 'FAIL' THEN 1 ELSE 0 END), 0) AS fail_count,
			COALESCE(SUM(CASE WHEN pr.verify_result NOT IN ('PASS', 'FAIL') AND pr.verify_result IS NOT NULL THEN 1 ELSE 0 END), 0) AS error_count,
			COALESCE(AVG(COALESCE(pr.build_cost_usd, 0) + COALESCE(sc_cost.sub_cost, 0)), 0) AS avg_cost_usd,
			COALESCE(AVG(
				CASE WHEN pr.scope_started_at IS NOT NULL AND pr.verify_completed_at IS NOT NULL
				THEN CAST(strftime('%%s', pr.verify_completed_at) - strftime('%%s', pr.scope_started_at) AS REAL)
				ELSE NULL END
			), 0) AS avg_duration_seconds,
			COALESCE(SUM(COALESCE(pr.build_cost_usd, 0) + COALESCE(sc_cost.sub_cost, 0)), 0) AS total_cost_usd
		FROM pipeline_runs pr
		LEFT JOIN (
			SELECT pipeline_run_id, SUM(estimated_cost_usd) AS sub_cost
			FROM sub_calls
			GROUP BY pipeline_run_id
		) sc_cost ON sc_cost.pipeline_run_id = pr.id
		%s
	`, whereClause)

	var s PipelineSummary
	var avgCost, avgDur, totalCost sql.NullFloat64
	err := db.conn.QueryRowContext(ctx, query, args...).Scan(
		&s.TotalRuns, &s.PassCount, &s.FailCount, &s.ErrorCount,
		&avgCost, &avgDur, &totalCost,
	)
	if err != nil {
		return PipelineSummary{}, err
	}
	s.AvgCostUSD = avgCost.Float64
	s.AvgDurationSeconds = avgDur.Float64
	s.TotalCostUSD = totalCost.Float64
	if s.TotalRuns > 0 {
		s.PassRate = float64(s.PassCount) / float64(s.TotalRuns)
	}

	// Verify-human agreement rate
	agreeQuery := fmt.Sprintf(`
		SELECT
			COUNT(*) AS total_comparable,
			COALESCE(SUM(CASE
				WHEN (pr.verify_result = 'PASS' AND pr.human_result = 'approved')
				  OR (pr.verify_result = 'FAIL' AND pr.human_result = 'rejected')
				THEN 1 ELSE 0 END
			), 0) AS agreed
		FROM pipeline_runs pr
		%s AND pr.verify_result IS NOT NULL AND pr.human_result IS NOT NULL
	`, whereClause)

	var totalComparable, agreed int64
	err = db.conn.QueryRowContext(ctx, agreeQuery, args...).Scan(&totalComparable, &agreed)
	if err != nil {
		return PipelineSummary{}, err
	}
	if totalComparable > 0 {
		s.VerifyHumanAgreementRate = float64(agreed) / float64(totalComparable)
	}

	return s, nil
}

// GetPlanSummary returns aggregate plan run stats for the given time range and optional project filter.
func (db *DB) GetPlanSummary(ctx context.Context, since time.Time, project string) (PlanSummary, error) {
	whereClause, args := buildWhereClause(since, project, "pr")

	query := fmt.Sprintf(`
		SELECT
			COUNT(*) AS total_runs,
			COALESCE(AVG(COALESCE(pr.generation_cost_usd, 0)), 0) AS avg_generation_cost_usd,
			COALESCE(AVG(COALESCE(pr.audit_cost_usd, 0)), 0) AS avg_audit_cost_usd,
			COALESCE(SUM(COALESCE(pr.generation_cost_usd, 0) + COALESCE(pr.audit_cost_usd, 0)), 0) AS total_cost_usd,
			COALESCE(AVG(COALESCE(pr.work_orders_generated, 0)), 0) AS avg_work_orders_per_plan,
			COALESCE(AVG(
				CASE WHEN pr.pre_audit_work_order_count IS NOT NULL AND pr.post_audit_work_order_count IS NOT NULL
				THEN CAST(pr.post_audit_work_order_count - pr.pre_audit_work_order_count AS REAL)
				ELSE NULL END
			), 0) AS avg_audit_delta
		FROM plan_runs pr
		%s
	`, whereClause)

	var s PlanSummary
	var avgGenCost, avgAuditCost, totalCost, avgWO, avgDelta sql.NullFloat64
	err := db.conn.QueryRowContext(ctx, query, args...).Scan(
		&s.TotalRuns, &avgGenCost, &avgAuditCost, &totalCost, &avgWO, &avgDelta,
	)
	if err != nil {
		return PlanSummary{}, err
	}
	s.AvgGenerationCostUSD = avgGenCost.Float64
	s.AvgAuditCostUSD = avgAuditCost.Float64
	s.TotalCostUSD = totalCost.Float64
	s.AvgWorkOrdersPerPlan = avgWO.Float64
	s.AvgAuditDelta = avgDelta.Float64
	return s, nil
}

// PipelineRunTrend holds per-run data for time series charts.
type PipelineRunTrend struct {
	ID                     string
	Date                   string
	PipelineCostUSD        float64
	ScopeDurationSeconds   float64
	BuildDurationSeconds   float64
	VerifyDurationSeconds  float64
	TotalDurationSeconds   float64
	VerifyResult           string
	HumanResult            sql.NullString
	HumanAgreed            sql.NullBool
	ScopeModel             sql.NullString
	BuildModel             sql.NullString
	VerifyModel            sql.NullString
	ScopeFilesSuggested    sql.NullInt64
	ScopePathsStripped     sql.NullInt64
	ScopePathsReclassified sql.NullInt64
	BuildFilesChanged      sql.NullInt64
	BuildScopeDrift        sql.NullInt64
}

// GetPipelineRunTrends returns per-pipeline-run data points for charting.
func (db *DB) GetPipelineRunTrends(ctx context.Context, since time.Time, project string) ([]PipelineRunTrend, error) {
	whereClause, args := buildWhereClause(since, project, "pr")

	query := fmt.Sprintf(`
		SELECT
			pr.id,
			DATE(pr.created_at) AS date,
			COALESCE(pr.build_cost_usd, 0) + COALESCE(sc_cost.sub_cost, 0) AS pipeline_cost_usd,
			CASE WHEN pr.scope_started_at IS NOT NULL AND pr.scope_completed_at IS NOT NULL
				THEN CAST(strftime('%%s', pr.scope_completed_at) - strftime('%%s', pr.scope_started_at) AS REAL)
				ELSE 0 END AS scope_duration_seconds,
			CASE WHEN pr.build_started_at IS NOT NULL AND pr.build_completed_at IS NOT NULL
				THEN CAST(strftime('%%s', pr.build_completed_at) - strftime('%%s', pr.build_started_at) AS REAL)
				ELSE 0 END AS build_duration_seconds,
			CASE WHEN pr.verify_started_at IS NOT NULL AND pr.verify_completed_at IS NOT NULL
				THEN CAST(strftime('%%s', pr.verify_completed_at) - strftime('%%s', pr.verify_started_at) AS REAL)
				ELSE 0 END AS verify_duration_seconds,
			CASE WHEN pr.scope_started_at IS NOT NULL AND pr.verify_completed_at IS NOT NULL
				THEN CAST(strftime('%%s', pr.verify_completed_at) - strftime('%%s', pr.scope_started_at) AS REAL)
				ELSE 0 END AS total_duration_seconds,
			COALESCE(pr.verify_result, '') AS verify_result,
			pr.human_result,
			CASE
				WHEN pr.verify_result IS NULL OR pr.human_result IS NULL THEN NULL
				WHEN (pr.verify_result = 'PASS' AND pr.human_result = 'approved')
				  OR (pr.verify_result = 'FAIL' AND pr.human_result = 'rejected') THEN 1
				ELSE 0
			END AS human_agreed,
			pr.scope_model,
			pr.build_model,
			pr.verify_model,
			pr.scope_files_suggested,
			pr.scope_paths_stripped,
			pr.scope_paths_reclassified,
			pr.build_files_changed,
			pr.build_scope_drift
		FROM pipeline_runs pr
		LEFT JOIN (
			SELECT pipeline_run_id, SUM(estimated_cost_usd) AS sub_cost
			FROM sub_calls
			GROUP BY pipeline_run_id
		) sc_cost ON sc_cost.pipeline_run_id = pr.id
		%s
		ORDER BY pr.created_at ASC
	`, whereClause)

	rows, err := db.conn.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []PipelineRunTrend
	for rows.Next() {
		var t PipelineRunTrend
		var humanAgreed sql.NullInt64
		if err := rows.Scan(
			&t.ID, &t.Date, &t.PipelineCostUSD,
			&t.ScopeDurationSeconds, &t.BuildDurationSeconds, &t.VerifyDurationSeconds, &t.TotalDurationSeconds,
			&t.VerifyResult, &t.HumanResult, &humanAgreed,
			&t.ScopeModel, &t.BuildModel, &t.VerifyModel,
			&t.ScopeFilesSuggested, &t.ScopePathsStripped, &t.ScopePathsReclassified,
			&t.BuildFilesChanged, &t.BuildScopeDrift,
		); err != nil {
			return nil, err
		}
		if humanAgreed.Valid {
			agreed := humanAgreed.Int64 == 1
			t.HumanAgreed = sql.NullBool{Bool: agreed, Valid: true}
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// PlanRunTrend holds per-plan-run data for charting.
type PlanRunTrend struct {
	ID                  string
	Date                string
	GenerationCostUSD   float64
	AuditCostUSD        float64
	TotalCostUSD        float64
	WorkOrdersGenerated int64
	PreAuditCount       int64
	PostAuditCount      int64
	AuditDelta          int64
	GenerationModel     sql.NullString
	AuditModel          sql.NullString
}

// GetPlanRunTrends returns per-plan-run data points for charting.
func (db *DB) GetPlanRunTrends(ctx context.Context, since time.Time, project string) ([]PlanRunTrend, error) {
	whereClause, args := buildWhereClause(since, project, "pr")

	query := fmt.Sprintf(`
		SELECT
			pr.id,
			DATE(pr.created_at) AS date,
			COALESCE(pr.generation_cost_usd, 0) AS generation_cost_usd,
			COALESCE(pr.audit_cost_usd, 0) AS audit_cost_usd,
			COALESCE(pr.generation_cost_usd, 0) + COALESCE(pr.audit_cost_usd, 0) AS total_cost_usd,
			COALESCE(pr.work_orders_generated, 0) AS work_orders_generated,
			COALESCE(pr.pre_audit_work_order_count, 0) AS pre_audit_count,
			COALESCE(pr.post_audit_work_order_count, 0) AS post_audit_count,
			COALESCE(pr.post_audit_work_order_count, 0) - COALESCE(pr.pre_audit_work_order_count, 0) AS audit_delta,
			pr.generation_model,
			pr.audit_model
		FROM plan_runs pr
		%s
		ORDER BY pr.created_at ASC
	`, whereClause)

	rows, err := db.conn.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []PlanRunTrend
	for rows.Next() {
		var t PlanRunTrend
		if err := rows.Scan(
			&t.ID, &t.Date, &t.GenerationCostUSD, &t.AuditCostUSD, &t.TotalCostUSD,
			&t.WorkOrdersGenerated, &t.PreAuditCount, &t.PostAuditCount, &t.AuditDelta,
			&t.GenerationModel, &t.AuditModel,
		); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// ModelStat holds per-model aggregate statistics.
type ModelStat struct {
	Model              string
	Provider           string
	Role               string
	RunCount           int64
	AvgCostUSD         float64
	AvgDurationSeconds float64
	AvgTokensIn        int64
	AvgTokensOut       int64
	PassRate           sql.NullFloat64
}

// GetModelStats returns per-model aggregate stats from sub_calls.
func (db *DB) GetModelStats(ctx context.Context, since time.Time) ([]ModelStat, error) {
	sinceStr := ""
	var args []any
	if !since.IsZero() {
		sinceStr = "WHERE sc.created_at >= ?"
		args = append(args, since.Format(time.RFC3339))
	}

	query := fmt.Sprintf(`
		SELECT
			sc.model,
			sc.provider,
			sc.phase AS role,
			COUNT(*) AS run_count,
			AVG(sc.estimated_cost_usd) AS avg_cost_usd,
			AVG(sc.latency_ms) / 1000.0 AS avg_duration_seconds,
			AVG(sc.tokens_in) AS avg_tokens_in,
			AVG(sc.tokens_out) AS avg_tokens_out
		FROM sub_calls sc
		%s
		GROUP BY sc.model, sc.provider, sc.phase
		ORDER BY run_count DESC
	`, sinceStr)

	rows, err := db.conn.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []ModelStat
	for rows.Next() {
		var m ModelStat
		if err := rows.Scan(
			&m.Model, &m.Provider, &m.Role,
			&m.RunCount, &m.AvgCostUSD, &m.AvgDurationSeconds,
			&m.AvgTokensIn, &m.AvgTokensOut,
		); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Derive pass_rate for build models
	for i, m := range out {
		if m.Role != "build" {
			continue
		}
		var passRate sql.NullFloat64
		prQuery := `
			SELECT
				CAST(SUM(CASE WHEN verify_result = 'PASS' THEN 1 ELSE 0 END) AS REAL) / NULLIF(COUNT(*), 0)
			FROM pipeline_runs
			WHERE build_model = ?
		`
		prArgs := []any{m.Model}
		if !since.IsZero() {
			prQuery += " AND created_at >= ?"
			prArgs = append(prArgs, since.Format(time.RFC3339))
		}
		if err := db.conn.QueryRowContext(ctx, prQuery, prArgs...).Scan(&passRate); err != nil && err != sql.ErrNoRows {
			return nil, err
		}
		out[i].PassRate = passRate
	}

	return out, nil
}

// buildWhereClause constructs a WHERE clause for date and project filtering.
// alias is the table alias (e.g., "pr").
func buildWhereClause(since time.Time, project string, alias string) (string, []any) {
	clauses := []string{"1=1"}
	var args []any

	if !since.IsZero() {
		clauses = append(clauses, fmt.Sprintf("%s.created_at >= ?", alias))
		args = append(args, since.Format(time.RFC3339))
	}
	if project != "" {
		clauses = append(clauses, fmt.Sprintf("%s.project = ?", alias))
		args = append(args, project)
	}

	return "WHERE " + strings.Join(clauses, " AND "), args
}
