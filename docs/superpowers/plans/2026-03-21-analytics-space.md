# Analytics Space Implementation Plan (Spec 07)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the Analytics dashboard with aggregate metrics, trend charts, and model comparisons across all pipeline and plan runs.

**Architecture:** Three new backend endpoints (`/api/stats/summary`, `/api/stats/trends`, `/api/stats/models`) serve aggregated data from pipeline_runs, sub_calls, and plan_runs tables using hand-written SQL queries in the database wrapper layer. Frontend replaces the AnalyticsSpace placeholder with a full-width Recharts dashboard featuring summary cards, time-series charts, distribution charts, and a sortable model table.

**Tech Stack:** Go (chi router, SQLite via modernc.org/sqlite), React 19, TypeScript, Recharts, Tailwind CSS, shadcn/ui

---

## File Structure

**New Go files:**
- `internal/database/analytics.go` — Hand-written SQL queries for summary, trends, and model stats with time range filtering
- `internal/api/analytics_handlers.go` — HTTP handlers + response types for the three stats endpoints
- `internal/api/analytics_handlers_test.go` — Handler tests

**Modified Go files:**
- `internal/api/server.go` — Register three new routes

**New frontend files:**
- `web/src/pages/AnalyticsSpace.tsx` — Full rewrite: dashboard layout, controls, summary cards, all 7 charts

**Modified frontend files:**
- `web/src/types/api.ts` — Add stats types
- `web/src/api/client.ts` — Add stats API functions
- `web/package.json` — Add recharts dependency

---

## Task 1: Install Recharts

**Files:**
- Modify: `web/package.json`

- [ ] **Step 1: Install recharts**

Run: `cd /home/gernsback/source/agent-conductor/web && npm install recharts`

- [ ] **Step 2: Verify frontend build**

Run: `cd /home/gernsback/source/agent-conductor/web && npm run build`
Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git add web/package.json web/package-lock.json
git commit -m "chore(spec07): add recharts dependency"
```

---

## Task 2: Database Analytics Queries

**Files:**
- Create: `internal/database/analytics.go`

This file contains hand-written SQL queries (same pattern as `plan_runs.go` and `workflows.go`) that query pipeline_runs, sub_calls, and plan_runs with time range filtering.

- [ ] **Step 1: Create analytics.go with time range helper and all query methods**

Create `internal/database/analytics.go`:

```go
package database

import (
	"context"
	"database/sql"
	"fmt"
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
	TotalRuns                  int64
	PassCount                  int64
	FailCount                  int64
	ErrorCount                 int64
	PassRate                   float64
	AvgCostUSD                 float64
	AvgDurationSeconds         float64
	TotalCostUSD               float64
	VerifyHumanAgreementRate   float64
}

// PlanSummary holds aggregate plan run statistics.
type PlanSummary struct {
	TotalRuns              int64
	AvgGenerationCostUSD   float64
	AvgAuditCostUSD        float64
	TotalCostUSD           float64
	AvgWorkOrdersPerPlan   float64
	AvgAuditDelta          float64
}

// GetPipelineSummary returns aggregate pipeline stats for the given time range and optional project filter.
func (db *DB) GetPipelineSummary(ctx context.Context, since time.Time, project string) (PipelineSummary, error) {
	whereClause, args := buildWhereClause(since, project, "pr")

	query := fmt.Sprintf(`
		SELECT
			COUNT(*) AS total_runs,
			SUM(CASE WHEN pr.verify_result = 'PASS' THEN 1 ELSE 0 END) AS pass_count,
			SUM(CASE WHEN pr.verify_result = 'FAIL' THEN 1 ELSE 0 END) AS fail_count,
			SUM(CASE WHEN pr.verify_result NOT IN ('PASS', 'FAIL') AND pr.verify_result IS NOT NULL THEN 1 ELSE 0 END) AS error_count,
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
			SUM(CASE
				WHEN (pr.verify_result = 'PASS' AND pr.human_result = 'approved')
				  OR (pr.verify_result = 'FAIL' AND pr.human_result = 'rejected')
				THEN 1 ELSE 0 END
			) AS agreed
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

	where := "WHERE "
	for i, c := range clauses {
		if i > 0 {
			where += " AND "
		}
		where += c
	}
	return where, args
}
```

- [ ] **Step 2: Verify build**

Run: `cd /home/gernsback/source/agent-conductor && go vet ./internal/database/`
Expected: PASS (no errors).

- [ ] **Step 3: Commit**

```bash
git add internal/database/analytics.go
git commit -m "feat(spec07): add analytics database queries with time range filtering"
```

---

## Task 3: Analytics HTTP Handlers

**Files:**
- Create: `internal/api/analytics_handlers.go`
- Modify: `internal/api/server.go` (add routes)

- [ ] **Step 1: Create analytics_handlers.go with response types and handlers**

Create `internal/api/analytics_handlers.go`:

```go
package api

import (
	"net/http"

	"github.com/ponchione/agent-conductor/internal/database"
)

// --- Response types ---

type pipelineSummaryResponse struct {
	TotalRuns                int64   `json:"total_runs"`
	PassCount                int64   `json:"pass_count"`
	FailCount                int64   `json:"fail_count"`
	ErrorCount               int64   `json:"error_count"`
	PassRate                 float64 `json:"pass_rate"`
	AvgCostUSD               float64 `json:"avg_cost_usd"`
	AvgDurationSeconds       float64 `json:"avg_duration_seconds"`
	TotalCostUSD             float64 `json:"total_cost_usd"`
	VerifyHumanAgreementRate float64 `json:"verify_human_agreement_rate"`
}

type planSummaryResponse struct {
	TotalRuns            int64   `json:"total_runs"`
	AvgGenerationCostUSD float64 `json:"avg_generation_cost_usd"`
	AvgAuditCostUSD      float64 `json:"avg_audit_cost_usd"`
	TotalCostUSD         float64 `json:"total_cost_usd"`
	AvgWorkOrdersPerPlan float64 `json:"avg_work_orders_per_plan"`
	AvgAuditDelta        float64 `json:"avg_audit_delta"`
}

type statsSummaryResponse struct {
	Range                string                  `json:"range"`
	Pipeline             pipelineSummaryResponse `json:"pipeline"`
	Plan                 planSummaryResponse     `json:"plan"`
	CombinedTotalCostUSD float64                 `json:"combined_total_cost_usd"`
}

type pipelineRunTrendResponse struct {
	ID                     string   `json:"id"`
	Date                   string   `json:"date"`
	PipelineCostUSD        float64  `json:"pipeline_cost_usd"`
	ScopeDurationSeconds   float64  `json:"scope_duration_seconds"`
	BuildDurationSeconds   float64  `json:"build_duration_seconds"`
	VerifyDurationSeconds  float64  `json:"verify_duration_seconds"`
	TotalDurationSeconds   float64  `json:"total_duration_seconds"`
	VerifyResult           string   `json:"verify_result"`
	HumanResult            *string  `json:"human_result,omitempty"`
	HumanAgreed            *bool    `json:"human_agreed,omitempty"`
	ScopeModel             *string  `json:"scope_model,omitempty"`
	BuildModel             *string  `json:"build_model,omitempty"`
	VerifyModel            *string  `json:"verify_model,omitempty"`
	ScopeFilesSuggested    *int64   `json:"scope_files_suggested,omitempty"`
	ScopePathsStripped     *int64   `json:"scope_paths_stripped,omitempty"`
	ScopePathsReclassified *int64   `json:"scope_paths_reclassified,omitempty"`
	BuildFilesChanged      *int64   `json:"build_files_changed,omitempty"`
	BuildScopeDrift        *int64   `json:"build_scope_drift,omitempty"`
}

type planRunTrendResponse struct {
	ID                  string  `json:"id"`
	Date                string  `json:"date"`
	GenerationCostUSD   float64 `json:"generation_cost_usd"`
	AuditCostUSD        float64 `json:"audit_cost_usd"`
	TotalCostUSD        float64 `json:"total_cost_usd"`
	WorkOrdersGenerated int64   `json:"work_orders_generated"`
	PreAuditCount       int64   `json:"pre_audit_count"`
	PostAuditCount      int64   `json:"post_audit_count"`
	AuditDelta          int64   `json:"audit_delta"`
	GenerationModel     *string `json:"generation_model,omitempty"`
	AuditModel          *string `json:"audit_model,omitempty"`
}

type statsTrendsResponse struct {
	PipelineRuns []pipelineRunTrendResponse `json:"pipeline_runs"`
	PlanRuns     []planRunTrendResponse     `json:"plan_runs"`
}

type modelStatResponse struct {
	Model              string   `json:"model"`
	Provider           string   `json:"provider"`
	Role               string   `json:"role"`
	RunCount           int64    `json:"run_count"`
	AvgCostUSD         float64  `json:"avg_cost_usd"`
	AvgDurationSeconds float64  `json:"avg_duration_seconds"`
	AvgTokensIn        int64    `json:"avg_tokens_in"`
	AvgTokensOut       int64    `json:"avg_tokens_out"`
	PassRate           *float64 `json:"pass_rate"`
}

type statsModelsResponse struct {
	Models []modelStatResponse `json:"models"`
}

// --- Handlers ---

func (s *Server) handleGetStatsSummary(w http.ResponseWriter, r *http.Request) {
	rangeStr := r.URL.Query().Get("range")
	if rangeStr == "" {
		rangeStr = "30d"
	}
	project := r.URL.Query().Get("project")
	since := database.ParseTimeRange(rangeStr)

	pipeline, err := s.db.GetPipelineSummary(r.Context(), since, project)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "pipeline summary: "+err.Error())
		return
	}

	plan, err := s.db.GetPlanSummary(r.Context(), since, project)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "plan summary: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, statsSummaryResponse{
		Range: rangeStr,
		Pipeline: pipelineSummaryResponse{
			TotalRuns:                pipeline.TotalRuns,
			PassCount:                pipeline.PassCount,
			FailCount:                pipeline.FailCount,
			ErrorCount:               pipeline.ErrorCount,
			PassRate:                 pipeline.PassRate,
			AvgCostUSD:               pipeline.AvgCostUSD,
			AvgDurationSeconds:       pipeline.AvgDurationSeconds,
			TotalCostUSD:             pipeline.TotalCostUSD,
			VerifyHumanAgreementRate: pipeline.VerifyHumanAgreementRate,
		},
		Plan: planSummaryResponse{
			TotalRuns:            plan.TotalRuns,
			AvgGenerationCostUSD: plan.AvgGenerationCostUSD,
			AvgAuditCostUSD:      plan.AvgAuditCostUSD,
			TotalCostUSD:         plan.TotalCostUSD,
			AvgWorkOrdersPerPlan: plan.AvgWorkOrdersPerPlan,
			AvgAuditDelta:        plan.AvgAuditDelta,
		},
		CombinedTotalCostUSD: pipeline.TotalCostUSD + plan.TotalCostUSD,
	})
}

func (s *Server) handleGetStatsTrends(w http.ResponseWriter, r *http.Request) {
	rangeStr := r.URL.Query().Get("range")
	if rangeStr == "" {
		rangeStr = "30d"
	}
	project := r.URL.Query().Get("project")
	since := database.ParseTimeRange(rangeStr)

	pipelineRuns, err := s.db.GetPipelineRunTrends(r.Context(), since, project)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "pipeline trends: "+err.Error())
		return
	}

	planRuns, err := s.db.GetPlanRunTrends(r.Context(), since, project)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "plan trends: "+err.Error())
		return
	}

	resp := statsTrendsResponse{
		PipelineRuns: make([]pipelineRunTrendResponse, 0, len(pipelineRuns)),
		PlanRuns:     make([]planRunTrendResponse, 0, len(planRuns)),
	}

	for _, pr := range pipelineRuns {
		item := pipelineRunTrendResponse{
			ID:                    pr.ID,
			Date:                  pr.Date,
			PipelineCostUSD:       pr.PipelineCostUSD,
			ScopeDurationSeconds:  pr.ScopeDurationSeconds,
			BuildDurationSeconds:  pr.BuildDurationSeconds,
			VerifyDurationSeconds: pr.VerifyDurationSeconds,
			TotalDurationSeconds:  pr.TotalDurationSeconds,
			VerifyResult:          pr.VerifyResult,
			HumanResult:           stringPtr(pr.HumanResult),
			ScopeModel:            stringPtr(pr.ScopeModel),
			BuildModel:            stringPtr(pr.BuildModel),
			VerifyModel:           stringPtr(pr.VerifyModel),
			ScopeFilesSuggested:   int64Ptr(pr.ScopeFilesSuggested),
			ScopePathsStripped:    int64Ptr(pr.ScopePathsStripped),
			ScopePathsReclassified: int64Ptr(pr.ScopePathsReclassified),
			BuildFilesChanged:     int64Ptr(pr.BuildFilesChanged),
			BuildScopeDrift:       int64Ptr(pr.BuildScopeDrift),
		}
		if pr.HumanAgreed.Valid {
			agreed := pr.HumanAgreed.Bool
			item.HumanAgreed = &agreed
		}
		resp.PipelineRuns = append(resp.PipelineRuns, item)
	}

	for _, pr := range planRuns {
		resp.PlanRuns = append(resp.PlanRuns, planRunTrendResponse{
			ID:                  pr.ID,
			Date:                pr.Date,
			GenerationCostUSD:   pr.GenerationCostUSD,
			AuditCostUSD:        pr.AuditCostUSD,
			TotalCostUSD:        pr.TotalCostUSD,
			WorkOrdersGenerated: pr.WorkOrdersGenerated,
			PreAuditCount:       pr.PreAuditCount,
			PostAuditCount:      pr.PostAuditCount,
			AuditDelta:          pr.AuditDelta,
			GenerationModel:     stringPtr(pr.GenerationModel),
			AuditModel:          stringPtr(pr.AuditModel),
		})
	}

	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleGetStatsModels(w http.ResponseWriter, r *http.Request) {
	rangeStr := r.URL.Query().Get("range")
	if rangeStr == "" {
		rangeStr = "30d"
	}
	since := database.ParseTimeRange(rangeStr)

	models, err := s.db.GetModelStats(r.Context(), since)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "model stats: "+err.Error())
		return
	}

	resp := statsModelsResponse{
		Models: make([]modelStatResponse, 0, len(models)),
	}
	for _, m := range models {
		item := modelStatResponse{
			Model:              m.Model,
			Provider:           m.Provider,
			Role:               m.Role,
			RunCount:           m.RunCount,
			AvgCostUSD:         m.AvgCostUSD,
			AvgDurationSeconds: m.AvgDurationSeconds,
			AvgTokensIn:        m.AvgTokensIn,
			AvgTokensOut:       m.AvgTokensOut,
		}
		if m.PassRate.Valid {
			pr := m.PassRate.Float64
			item.PassRate = &pr
		}
		resp.Models = append(resp.Models, item)
	}

	writeJSON(w, http.StatusOK, resp)
}
```

- [ ] **Step 2: Register routes in server.go**

Add before the SPA fallback:

```go
	r.Get("/api/stats/summary", s.handleGetStatsSummary)
	r.Get("/api/stats/trends", s.handleGetStatsTrends)
	r.Get("/api/stats/models", s.handleGetStatsModels)
```

- [ ] **Step 3: Verify build**

Run: `cd /home/gernsback/source/agent-conductor && go vet ./internal/api/`
Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add internal/api/analytics_handlers.go internal/api/server.go
git commit -m "feat(spec07): add stats summary, trends, and models HTTP handlers"
```

---

## Task 4: Analytics Handler Tests

**Files:**
- Create: `internal/api/analytics_handlers_test.go`

- [ ] **Step 1: Write handler tests**

Create `internal/api/analytics_handlers_test.go`:

```go
package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGetStatsSummary_Empty(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/stats/summary", nil)
	NewServer(db, nil, "main", nil, "", nil).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var resp statsSummaryResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if resp.Range != "30d" {
		t.Fatalf("range = %q, want 30d", resp.Range)
	}
	if resp.Pipeline.TotalRuns != 0 {
		t.Fatalf("pipeline.total_runs = %d, want 0", resp.Pipeline.TotalRuns)
	}
	if resp.Plan.TotalRuns != 0 {
		t.Fatalf("plan.total_runs = %d, want 0", resp.Plan.TotalRuns)
	}
}

func TestGetStatsSummary_WithRange(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/stats/summary?range=7d", nil)
	NewServer(db, nil, "main", nil, "", nil).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var resp statsSummaryResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if resp.Range != "7d" {
		t.Fatalf("range = %q, want 7d", resp.Range)
	}
}

func TestGetStatsTrends_Empty(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/stats/trends", nil)
	NewServer(db, nil, "main", nil, "", nil).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var resp statsTrendsResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if len(resp.PipelineRuns) != 0 {
		t.Fatalf("pipeline_runs len = %d, want 0", len(resp.PipelineRuns))
	}
	if len(resp.PlanRuns) != 0 {
		t.Fatalf("plan_runs len = %d, want 0", len(resp.PlanRuns))
	}
}

func TestGetStatsModels_Empty(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/stats/models", nil)
	NewServer(db, nil, "main", nil, "", nil).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var resp statsModelsResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if len(resp.Models) != 0 {
		t.Fatalf("models len = %d, want 0", len(resp.Models))
	}
}
```

- [ ] **Step 2: Run tests**

Run: `cd /home/gernsback/source/agent-conductor && go test ./internal/api/ -run "TestGetStats" -v -count=1`
Expected: all PASS.

- [ ] **Step 3: Run full test suite**

Run: `make test`
Expected: all packages pass.

- [ ] **Step 4: Commit**

```bash
git add internal/api/analytics_handlers_test.go
git commit -m "test(spec07): add analytics handler tests"
```

---

## Task 5: Frontend Types & API Client

**Files:**
- Modify: `web/src/types/api.ts`
- Modify: `web/src/api/client.ts`

- [ ] **Step 1: Add stats types to api.ts**

Append to `web/src/types/api.ts`:

```typescript
// --- Spec 07: Analytics types ---

export interface StatsSummaryResponse {
  range: string;
  pipeline: {
    total_runs: number;
    pass_count: number;
    fail_count: number;
    error_count: number;
    pass_rate: number;
    avg_cost_usd: number;
    avg_duration_seconds: number;
    total_cost_usd: number;
    verify_human_agreement_rate: number;
  };
  plan: {
    total_runs: number;
    avg_generation_cost_usd: number;
    avg_audit_cost_usd: number;
    total_cost_usd: number;
    avg_work_orders_per_plan: number;
    avg_audit_delta: number;
  };
  combined_total_cost_usd: number;
}

export interface PipelineRunTrend {
  id: string;
  date: string;
  pipeline_cost_usd: number;
  scope_duration_seconds: number;
  build_duration_seconds: number;
  verify_duration_seconds: number;
  total_duration_seconds: number;
  verify_result: string;
  human_result?: string;
  human_agreed?: boolean;
  scope_model?: string;
  build_model?: string;
  verify_model?: string;
  scope_files_suggested?: number;
  scope_paths_stripped?: number;
  scope_paths_reclassified?: number;
  build_files_changed?: number;
  build_scope_drift?: number;
}

export interface PlanRunTrend {
  id: string;
  date: string;
  generation_cost_usd: number;
  audit_cost_usd: number;
  total_cost_usd: number;
  work_orders_generated: number;
  pre_audit_count: number;
  post_audit_count: number;
  audit_delta: number;
  generation_model?: string;
  audit_model?: string;
}

export interface StatsTrendsResponse {
  pipeline_runs: PipelineRunTrend[];
  plan_runs: PlanRunTrend[];
}

export interface ModelStat {
  model: string;
  provider: string;
  role: string;
  run_count: number;
  avg_cost_usd: number;
  avg_duration_seconds: number;
  avg_tokens_in: number;
  avg_tokens_out: number;
  pass_rate: number | null;
}

export interface StatsModelsResponse {
  models: ModelStat[];
}
```

- [ ] **Step 2: Add stats API functions to client.ts**

Append to `web/src/api/client.ts`. Add `StatsSummaryResponse`, `StatsTrendsResponse`, `StatsModelsResponse` to the import block.

```typescript
// --- Stats API ---

export async function getStatsSummary(range_?: string, project?: string): Promise<StatsSummaryResponse> {
  const params = new URLSearchParams();
  if (range_) params.set("range", range_);
  if (project) params.set("project", project);
  const query = params.toString();
  return fetchJSON<StatsSummaryResponse>(query ? `/api/stats/summary?${query}` : "/api/stats/summary");
}

export async function getStatsTrends(range_?: string, project?: string): Promise<StatsTrendsResponse> {
  const params = new URLSearchParams();
  if (range_) params.set("range", range_);
  if (project) params.set("project", project);
  const query = params.toString();
  return fetchJSON<StatsTrendsResponse>(query ? `/api/stats/trends?${query}` : "/api/stats/trends");
}

export async function getStatsModels(range_?: string): Promise<StatsModelsResponse> {
  const params = new URLSearchParams();
  if (range_) params.set("range", range_);
  const query = params.toString();
  return fetchJSON<StatsModelsResponse>(query ? `/api/stats/models?${query}` : "/api/stats/models");
}
```

- [ ] **Step 3: Verify frontend build**

Run: `cd /home/gernsback/source/agent-conductor/web && npm run build`
Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add web/src/types/api.ts web/src/api/client.ts
git commit -m "feat(spec07): add analytics TypeScript types and API client functions"
```

---

## Task 6: AnalyticsSpace Dashboard (Full Rewrite)

**Files:**
- Rewrite: `web/src/pages/AnalyticsSpace.tsx`

This is the largest task — it replaces the placeholder with the full dashboard including controls, summary cards, and all 7 charts. Since it's a single page component, it's implemented as one cohesive file.

- [ ] **Step 1: Rewrite AnalyticsSpace.tsx**

Replace `web/src/pages/AnalyticsSpace.tsx` with the full dashboard implementation. The component should:

1. **State:** `range` (default "30d"), `project` (default ""), `summary`, `trends`, `models`, `loading`, `error`
2. **Data fetching:** `useEffect` that calls `getStatsSummary`, `getStatsTrends`, `getStatsModels` via `Promise.all` whenever `range` or `project` changes
3. **Controls bar:** Time range segmented buttons (7d/30d/90d/All) + project filter (hardcode "All Projects" for v1)
4. **Summary cards:** Two rows using `MetricCard` — 5 pipeline cards + 3 plan cards
5. **Charts (all using Recharts):**
   - Cost Over Time (LineChart — pipeline blue + plan purple)
   - Duration by Phase (StackedBarChart — scope/build/verify in blues)
   - Verify Result Distribution (PieChart — PASS green, FAIL red, ERROR amber) — left column
   - Scope Quality Scatter (ScatterChart — files vs paths, color by result) — right column
   - Model Comparison Table (shadcn Table — sortable)
   - Verify-Human Agreement Trend (LineChart — rolling 10-run window)
   - Plan Audit Effectiveness (BarChart — pre/post stacked with delta labels)

**Color constants:**
```typescript
const COLORS = {
  pipeline: "#3b82f6",
  plan: "#8b5cf6",
  success: "#22c55e",
  failure: "#ef4444",
  warning: "#f59e0b",
  neutral: "#6b7280",
  scopeBlue: "#93c5fd",
  buildBlue: "#3b82f6",
  verifyBlue: "#1d4ed8",
} as const;
```

**Formatting helpers:**
```typescript
function formatCurrency(v: number): string {
  return `$${v.toFixed(2)}`;
}

function formatDuration(seconds: number): string {
  const m = Math.floor(seconds / 60);
  const s = Math.round(seconds % 60);
  return m > 0 ? `${m}m ${s}s` : `${s}s`;
}

function formatPercent(v: number): string {
  return `${(v * 100).toFixed(1)}%`;
}
```

**Chart wrapper pattern:**
```tsx
function ChartCard({ title, children, empty, loading }: {
  title: string;
  children: React.ReactNode;
  empty?: boolean;
  loading?: boolean;
}) {
  return (
    <div className="rounded-lg border border-border p-4">
      <h3 className="mb-4 text-sm font-medium">{title}</h3>
      {loading ? (
        <div className="flex h-64 items-center justify-center">
          <p className="text-sm text-muted-foreground">Loading...</p>
        </div>
      ) : empty ? (
        <div className="flex h-64 items-center justify-center">
          <p className="text-sm text-muted-foreground">No data available for the selected time range</p>
        </div>
      ) : (
        children
      )}
    </div>
  );
}
```

**Model table sort:** Track `sortCol` and `sortDir` in state. Sort models array before rendering. Clicking a header toggles direction.

**Agreement trend rolling window:** Compute client-side from pipeline trends:
```typescript
function computeAgreementTrend(runs: PipelineRunTrend[]) {
  const withBoth = runs.filter(r => r.human_agreed !== undefined);
  return withBoth.map((_, idx) => {
    const windowStart = Math.max(0, idx - 9);
    const window = withBoth.slice(windowStart, idx + 1);
    const agreed = window.filter(r => r.human_agreed === true).length;
    return {
      date: withBoth[idx].date,
      rate: (agreed / window.length) * 100,
    };
  });
}
```

- [ ] **Step 2: Verify frontend build**

Run: `cd /home/gernsback/source/agent-conductor/web && npm run build`
Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git add web/src/pages/AnalyticsSpace.tsx
git commit -m "feat(spec07): implement full analytics dashboard with charts"
```

---

## Task 7: Embed Updated Frontend Assets & Final Verification

- [ ] **Step 1: Rebuild frontend for embedding**

Run: `cd /home/gernsback/source/agent-conductor && make web-build`
(This copies built assets into `internal/api/static/` for the Go embed)

- [ ] **Step 2: Run Go build**

Run: `make build`
Expected: PASS.

- [ ] **Step 3: Run Go tests**

Run: `make test`
Expected: all packages pass.

- [ ] **Step 4: Verify frontend build**

Run: `cd /home/gernsback/source/agent-conductor/web && npm run build`
Expected: PASS.

- [ ] **Step 5: Commit embedded assets if changed**

```bash
git add internal/api/static/
git commit -m "chore(spec07): update embedded frontend assets"
```
