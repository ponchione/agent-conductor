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
	ID                     string  `json:"id"`
	Date                   string  `json:"date"`
	PipelineCostUSD        float64 `json:"pipeline_cost_usd"`
	ScopeDurationSeconds   float64 `json:"scope_duration_seconds"`
	BuildDurationSeconds   float64 `json:"build_duration_seconds"`
	VerifyDurationSeconds  float64 `json:"verify_duration_seconds"`
	TotalDurationSeconds   float64 `json:"total_duration_seconds"`
	VerifyResult           string  `json:"verify_result"`
	HumanResult            *string `json:"human_result,omitempty"`
	HumanAgreed            *bool   `json:"human_agreed,omitempty"`
	ScopeModel             *string `json:"scope_model,omitempty"`
	BuildModel             *string `json:"build_model,omitempty"`
	VerifyModel            *string `json:"verify_model,omitempty"`
	ScopeFilesSuggested    *int64  `json:"scope_files_suggested,omitempty"`
	ScopePathsStripped     *int64  `json:"scope_paths_stripped,omitempty"`
	ScopePathsReclassified *int64  `json:"scope_paths_reclassified,omitempty"`
	BuildFilesChanged      *int64  `json:"build_files_changed,omitempty"`
	BuildScopeDrift        *int64  `json:"build_scope_drift,omitempty"`
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
	AvgTokensIn        float64  `json:"avg_tokens_in"`
	AvgTokensOut       float64  `json:"avg_tokens_out"`
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
			ID:                     pr.ID,
			Date:                   pr.Date,
			PipelineCostUSD:        pr.PipelineCostUSD,
			ScopeDurationSeconds:   pr.ScopeDurationSeconds,
			BuildDurationSeconds:   pr.BuildDurationSeconds,
			VerifyDurationSeconds:  pr.VerifyDurationSeconds,
			TotalDurationSeconds:   pr.TotalDurationSeconds,
			VerifyResult:           pr.VerifyResult,
			HumanResult:            stringPtr(pr.HumanResult),
			ScopeModel:             stringPtr(pr.ScopeModel),
			BuildModel:             stringPtr(pr.BuildModel),
			VerifyModel:            stringPtr(pr.VerifyModel),
			ScopeFilesSuggested:    int64Ptr(pr.ScopeFilesSuggested),
			ScopePathsStripped:     int64Ptr(pr.ScopePathsStripped),
			ScopePathsReclassified: int64Ptr(pr.ScopePathsReclassified),
			BuildFilesChanged:      int64Ptr(pr.BuildFilesChanged),
			BuildScopeDrift:        int64Ptr(pr.BuildScopeDrift),
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
