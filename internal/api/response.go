package api

import (
	"database/sql"
	"encoding/json"
	"time"

	"github.com/ponchione/agent-conductor/internal/database"
)

type sessionListResponse struct {
	Sessions []sessionSummaryResponse `json:"sessions"`
}

type sessionSummaryResponse struct {
	ID                  string  `json:"id"`
	Kind                string  `json:"kind"`
	Project             string  `json:"project"`
	SourceSpecPath      *string `json:"source_spec_path,omitempty"`
	State               string  `json:"state"`
	ErrorMessage        *string `json:"error_message,omitempty"`
	StartedAt           *string `json:"started_at,omitempty"`
	CompletedAt         *string `json:"completed_at,omitempty"`
	CreatedAt           string  `json:"created_at"`
	UpdatedAt           string  `json:"updated_at"`
	PlanRunsCount       int64   `json:"plan_runs_count"`
	PipelineRunsCount   int64   `json:"pipeline_runs_count"`
	LatestWorkflowState *string `json:"latest_workflow_state,omitempty"`
}

type sessionDetailResponse struct {
	Session      sessionResponse          `json:"session"`
	PlanRuns     []sessionPlanRunResponse `json:"plan_runs"`
	PipelineRuns []sessionPipelineRunDTO  `json:"pipeline_runs"`
	Artifacts    []artifactResponse       `json:"artifacts"`
}

type sessionResponse struct {
	ID             string  `json:"id"`
	Kind           string  `json:"kind"`
	Project        string  `json:"project"`
	SourceSpecPath *string `json:"source_spec_path,omitempty"`
	State          string  `json:"state"`
	ErrorMessage   *string `json:"error_message,omitempty"`
	StartedAt      *string `json:"started_at,omitempty"`
	CompletedAt    *string `json:"completed_at,omitempty"`
	CreatedAt      string  `json:"created_at"`
	UpdatedAt      string  `json:"updated_at"`
}

type sessionPlanRunResponse struct {
	ID                      string   `json:"id"`
	SpecFile                string   `json:"spec_file"`
	Project                 *string  `json:"project,omitempty"`
	State                   string   `json:"state"`
	SpecFingerprint         *string  `json:"spec_fingerprint,omitempty"`
	WorkOrdersGenerated     *int64   `json:"work_orders_generated,omitempty"`
	PreAuditWorkOrderCount  *int64   `json:"pre_audit_work_order_count,omitempty"`
	PostAuditWorkOrderCount *int64   `json:"post_audit_work_order_count,omitempty"`
	WorkOrderDelta          *int64   `json:"work_order_delta,omitempty"`
	AuditChanges            []string `json:"audit_changes,omitempty"`
	CreatedAt               string   `json:"created_at"`
}

type sessionPipelineRunDTO struct {
	ID            string  `json:"id"`
	WorkflowID    string  `json:"workflow_id"`
	Project       string  `json:"project"`
	WorkOrderType *string `json:"work_order_type,omitempty"`
	VerifyResult  *string `json:"verify_result,omitempty"`
	HumanResult   *string `json:"human_result,omitempty"`
	CreatedAt     string  `json:"created_at"`
}

type artifactResponse struct {
	ID           string  `json:"id"`
	WorkflowID   *string `json:"workflow_id,omitempty"`
	TaskID       *string `json:"task_id,omitempty"`
	ArtifactType string  `json:"artifact_type"`
	Path         string  `json:"path"`
	SizeBytes    *int64  `json:"size_bytes,omitempty"`
	Metadata     any     `json:"metadata,omitempty"`
	CreatedAt    string  `json:"created_at"`
}

type planAuditStatsResponse struct {
	Summary    planAuditSummaryResponse    `json:"summary"`
	RecentRuns []planRunUsefulnessResponse `json:"recent_runs"`
}

type planAuditSummaryResponse struct {
	ChangedRuns   int64 `json:"changed_runs"`
	UnchangedRuns int64 `json:"unchanged_runs"`
	TotalRuns     int64 `json:"total_runs"`
}

type planRunUsefulnessResponse struct {
	ID                      string   `json:"id"`
	SessionID               *string  `json:"session_id,omitempty"`
	Project                 *string  `json:"project,omitempty"`
	SpecFile                string   `json:"spec_file"`
	State                   string   `json:"state"`
	AuditChanged            bool     `json:"audit_changed"`
	WorkOrdersGenerated     *int64   `json:"work_orders_generated,omitempty"`
	PreAuditWorkOrderCount  *int64   `json:"pre_audit_work_order_count,omitempty"`
	PostAuditWorkOrderCount *int64   `json:"post_audit_work_order_count,omitempty"`
	WorkOrderDelta          *int64   `json:"work_order_delta,omitempty"`
	AuditChanges            []string `json:"audit_changes,omitempty"`
	CreatedAt               string   `json:"created_at"`
}

type eventStreamResponse struct {
	ID         int64   `json:"id"`
	WorkflowID *string `json:"workflow_id,omitempty"`
	TaskID     *string `json:"task_id,omitempty"`
	EventType  string  `json:"event_type"`
	EventData  any     `json:"event_data"`
	CreatedAt  string  `json:"created_at"`
}

func mapSessionSummaries(rows []database.SessionSummary) []sessionSummaryResponse {
	out := make([]sessionSummaryResponse, 0, len(rows))
	for _, row := range rows {
		out = append(out, sessionSummaryResponse{
			ID:                  row.ID,
			Kind:                row.Kind,
			Project:             row.Project,
			SourceSpecPath:      stringPtr(row.SourceSpecPath),
			State:               row.State,
			ErrorMessage:        stringPtr(row.ErrorMessage),
			StartedAt:           stringPtr(row.StartedAt),
			CompletedAt:         stringPtr(row.CompletedAt),
			CreatedAt:           row.CreatedAt,
			UpdatedAt:           row.UpdatedAt,
			PlanRunsCount:       row.PlanRunsCount,
			PipelineRunsCount:   row.PipelineRunsCount,
			LatestWorkflowState: stringPtr(row.LatestWorkflowState),
		})
	}
	return out
}

func mapSessionDetail(detail database.SessionDetail) sessionDetailResponse {
	return sessionDetailResponse{
		Session: sessionResponse{
			ID:             detail.Session.ID,
			Kind:           detail.Session.Kind,
			Project:        detail.Session.Project,
			SourceSpecPath: stringPtr(detail.Session.SourceSpecPath),
			State:          detail.Session.State,
			ErrorMessage:   stringPtr(detail.Session.ErrorMessage),
			StartedAt:      stringPtr(detail.Session.StartedAt),
			CompletedAt:    stringPtr(detail.Session.CompletedAt),
			CreatedAt:      detail.Session.CreatedAt,
			UpdatedAt:      detail.Session.UpdatedAt,
		},
		PlanRuns:     mapSessionPlanRuns(detail.PlanRuns),
		PipelineRuns: mapSessionPipelineRuns(detail.PipelineRuns),
		Artifacts:    mapArtifacts(detail.Artifacts),
	}
}

func mapSessionPlanRuns(rows []database.SessionPlanRun) []sessionPlanRunResponse {
	out := make([]sessionPlanRunResponse, 0, len(rows))
	for _, row := range rows {
		out = append(out, sessionPlanRunResponse{
			ID:                      row.ID,
			SpecFile:                row.SpecFile,
			Project:                 stringPtr(row.Project),
			State:                   row.State,
			SpecFingerprint:         stringPtr(row.SpecFingerprint),
			WorkOrdersGenerated:     int64Ptr(row.WorkOrdersGenerated),
			PreAuditWorkOrderCount:  int64Ptr(row.PreAuditWorkOrderCount),
			PostAuditWorkOrderCount: int64Ptr(row.PostAuditWorkOrderCount),
			WorkOrderDelta:          computeDelta(row.PreAuditWorkOrderCount, row.PostAuditWorkOrderCount),
			AuditChanges:            parseAuditChanges(row.AuditChangeText),
			CreatedAt:               row.CreatedAt,
		})
	}
	return out
}

func mapSessionPipelineRuns(rows []database.SessionPipelineRun) []sessionPipelineRunDTO {
	out := make([]sessionPipelineRunDTO, 0, len(rows))
	for _, row := range rows {
		out = append(out, sessionPipelineRunDTO{
			ID:            row.ID,
			WorkflowID:    row.WorkflowID,
			Project:       row.Project,
			WorkOrderType: stringPtr(row.WorkOrderType),
			VerifyResult:  stringPtr(row.VerifyResult),
			HumanResult:   stringPtr(row.HumanResult),
			CreatedAt:     row.CreatedAt,
		})
	}
	return out
}

func mapArtifacts(rows []database.Artifact) []artifactResponse {
	out := make([]artifactResponse, 0, len(rows))
	for _, row := range rows {
		out = append(out, artifactResponse{
			ID:           row.ID,
			WorkflowID:   stringPtr(row.WorkflowID),
			TaskID:       stringPtr(row.TaskID),
			ArtifactType: row.ArtifactType,
			Path:         row.Path,
			SizeBytes:    int64Ptr(row.SizeBytes),
			Metadata:     parseMetadata(row.MetadataJSON),
			CreatedAt:    row.CreatedAt,
		})
	}
	return out
}

func mapPlanAuditSummary(stats database.PlanAuditChangeStats) planAuditSummaryResponse {
	return planAuditSummaryResponse{
		ChangedRuns:   stats.ChangedRuns,
		UnchangedRuns: stats.UnchangedRuns,
		TotalRuns:     stats.TotalRuns,
	}
}

func mapPlanRunUsefulnessRows(rows []database.PlanRunUsefulness) []planRunUsefulnessResponse {
	out := make([]planRunUsefulnessResponse, 0, len(rows))
	for _, row := range rows {
		out = append(out, planRunUsefulnessResponse{
			ID:                      row.ID,
			SessionID:               stringPtr(row.SessionID),
			Project:                 stringPtr(row.Project),
			SpecFile:                row.SpecFile,
			State:                   row.State,
			AuditChanged:            row.AuditChanged,
			WorkOrdersGenerated:     int64Ptr(row.WorkOrdersGenerated),
			PreAuditWorkOrderCount:  int64Ptr(row.PreAuditWorkOrderCount),
			PostAuditWorkOrderCount: int64Ptr(row.PostAuditWorkOrderCount),
			WorkOrderDelta:          int64Ptr(row.WorkOrderDelta),
			AuditChanges:            parseAuditChanges(row.AuditChangeText),
			CreatedAt:               row.CreatedAt,
		})
	}
	return out
}

func mapEventStreamRow(row database.Event) eventStreamResponse {
	return eventStreamResponse{
		ID:         row.ID,
		WorkflowID: stringPtr(row.WorkflowID),
		TaskID:     stringPtr(row.TaskID),
		EventType:  row.EventType,
		EventData:  parseJSONValue(row.EventData),
		CreatedAt:  row.CreatedAt,
	}
}

func parseAuditChanges(raw sql.NullString) []string {
	if !raw.Valid || raw.String == "" {
		return nil
	}

	var changes []string
	if err := json.Unmarshal([]byte(raw.String), &changes); err == nil {
		return changes
	}

	return []string{raw.String}
}

func parseMetadata(raw sql.NullString) any {
	return parseJSONValue(raw)
}

func parseJSONValue(raw sql.NullString) any {
	if !raw.Valid || raw.String == "" {
		return nil
	}

	var payload any
	if err := json.Unmarshal([]byte(raw.String), &payload); err != nil {
		return raw.String
	}
	return payload
}

func stringPtr(v sql.NullString) *string {
	if !v.Valid {
		return nil
	}
	value := v.String
	return &value
}

func int64Ptr(v sql.NullInt64) *int64 {
	if !v.Valid {
		return nil
	}
	value := v.Int64
	return &value
}

func float64Ptr(v sql.NullFloat64) *float64 {
	if !v.Valid {
		return nil
	}
	value := v.Float64
	return &value
}

func computeDelta(before, after sql.NullInt64) *int64 {
	if !before.Valid || !after.Valid {
		return nil
	}
	value := after.Int64 - before.Int64
	return &value
}

// --- Workflow response types ---

type workflowSummaryResponse struct {
	ID                   string   `json:"id"`
	CurrentState         string   `json:"current_state"`
	WorkOrderTitle       string   `json:"work_order_title"`
	WorkOrderType        *string  `json:"work_order_type,omitempty"`
	Project              string   `json:"project"`
	SessionID            *string  `json:"session_id,omitempty"`
	GitBranch            string   `json:"git_branch"`
	VerifyResult         *string  `json:"verify_result,omitempty"`
	HumanResult          *string  `json:"human_result,omitempty"`
	TotalCostUSD         *float64 `json:"total_cost_usd,omitempty"`
	TotalDurationSeconds *int64   `json:"total_duration_seconds,omitempty"`
	CreatedAt            string   `json:"created_at"`
	UpdatedAt            string   `json:"updated_at"`
}

type workflowListResponse struct {
	Workflows []workflowSummaryResponse `json:"workflows"`
	Total     int                       `json:"total"`
}

type workflowRecordResponse struct {
	ID             string  `json:"id"`
	CurrentState   string  `json:"current_state"`
	OriginalIntent string  `json:"original_intent"`
	OriginalFile   string  `json:"original_file"`
	GitBranch      string  `json:"git_branch"`
	TargetRepo     string  `json:"target_repo"`
	CreatedAt      string  `json:"created_at"`
	UpdatedAt      string  `json:"updated_at"`
	ErrorMessage   *string `json:"error_message,omitempty"`
}

type pipelineRunDetailResponse struct {
	ID                       string   `json:"id"`
	WorkOrderType            *string  `json:"work_order_type,omitempty"`
	WorkOrderContent         *string  `json:"work_order_content,omitempty"`
	VerifyResult             *string  `json:"verify_result,omitempty"`
	HumanResult              *string  `json:"human_result,omitempty"`
	ScopeStartedAt           *string  `json:"scope_started_at,omitempty"`
	ScopeCompletedAt         *string  `json:"scope_completed_at,omitempty"`
	BuildStartedAt           *string  `json:"build_started_at,omitempty"`
	BuildCompletedAt         *string  `json:"build_completed_at,omitempty"`
	VerifyStartedAt          *string  `json:"verify_started_at,omitempty"`
	VerifyCompletedAt        *string  `json:"verify_completed_at,omitempty"`
	ScopeModel               *string  `json:"scope_model,omitempty"`
	BuildModel               *string  `json:"build_model,omitempty"`
	VerifyModel              *string  `json:"verify_model,omitempty"`
	ScopeFilesSuggested      *int64   `json:"scope_files_suggested,omitempty"`
	BuildFilesChanged        *int64   `json:"build_files_changed,omitempty"`
	BuildScopeDrift          int      `json:"build_scope_drift"`
	ScopeEstimatedComplexity *string  `json:"scope_estimated_complexity,omitempty"`
	ScopeRagDirect           *int64   `json:"scope_rag_direct,omitempty"`
	ScopeRagHops             *int64   `json:"scope_rag_hops,omitempty"`
	ScopePathsStripped       *int64   `json:"scope_paths_stripped,omitempty"`
	ScopePathsReclassified   *int64   `json:"scope_paths_reclassified,omitempty"`
	BuildCostUSD             *float64 `json:"build_cost_usd,omitempty"`
	BuildToolCalls           *string  `json:"build_tool_calls,omitempty"`
}

type subCallResponse struct {
	Phase            string  `json:"phase"`
	Step             string  `json:"step"`
	Provider         string  `json:"provider"`
	Model            string  `json:"model"`
	TokensIn         int     `json:"tokens_in"`
	TokensOut        int     `json:"tokens_out"`
	LatencyMs        int     `json:"latency_ms"`
	EstimatedCostUSD float64 `json:"estimated_cost_usd"`
	Success          bool    `json:"success"`
	ErrorMessage     *string `json:"error_message,omitempty"`
}

type workflowDetailResponse struct {
	Workflow    workflowRecordResponse     `json:"workflow"`
	PipelineRun *pipelineRunDetailResponse `json:"pipeline_run"`
	SubCalls    []subCallResponse          `json:"sub_calls"`
}

func mapWorkflowSummaries(rows []database.UIWorkflowSummary) []workflowSummaryResponse {
	out := make([]workflowSummaryResponse, 0, len(rows))
	for _, row := range rows {
		out = append(out, workflowSummaryResponse{
			ID:                   row.ID,
			CurrentState:         row.CurrentState,
			WorkOrderTitle:       row.WorkOrderTitle,
			WorkOrderType:        stringPtr(row.WorkOrderType),
			Project:              row.Project,
			SessionID:            stringPtr(row.SessionID),
			GitBranch:            row.GitBranch,
			VerifyResult:         stringPtr(row.VerifyResult),
			HumanResult:          stringPtr(row.HumanResult),
			TotalCostUSD:         float64Ptr(row.TotalCostUSD),
			TotalDurationSeconds: int64Ptr(row.TotalDurationSeconds),
			CreatedAt:            row.CreatedAt,
			UpdatedAt:            row.UpdatedAt,
		})
	}
	return out
}

func mapWorkflowDetail(w database.UIWorkflowDetail, pr *database.UIPipelineRunDetail, subCalls []database.UISubCall) workflowDetailResponse {
	resp := workflowDetailResponse{
		Workflow: workflowRecordResponse{
			ID:             w.ID,
			CurrentState:   w.CurrentState,
			OriginalIntent: w.OriginalIntent,
			OriginalFile:   w.OriginalFile,
			GitBranch:      w.GitBranch,
			TargetRepo:     w.TargetRepo,
			CreatedAt:      w.CreatedAt,
			UpdatedAt:      w.UpdatedAt,
			ErrorMessage:   stringPtr(w.ErrorMessage),
		},
		SubCalls: mapSubCalls(subCalls),
	}
	if pr != nil {
		detail := pipelineRunDetailResponse{
			ID:                       pr.ID,
			WorkOrderType:            stringPtr(pr.WorkOrderType),
			WorkOrderContent:         stringPtr(pr.WorkOrderContent),
			VerifyResult:             stringPtr(pr.VerifyResult),
			HumanResult:              stringPtr(pr.HumanResult),
			ScopeStartedAt:           stringPtr(pr.ScopeStartedAt),
			ScopeCompletedAt:         stringPtr(pr.ScopeCompletedAt),
			BuildStartedAt:           stringPtr(pr.BuildStartedAt),
			BuildCompletedAt:         stringPtr(pr.BuildCompletedAt),
			VerifyStartedAt:          stringPtr(pr.VerifyStartedAt),
			VerifyCompletedAt:        stringPtr(pr.VerifyCompletedAt),
			ScopeModel:               stringPtr(pr.ScopeModel),
			BuildModel:               stringPtr(pr.BuildModel),
			VerifyModel:              stringPtr(pr.VerifyModel),
			ScopeFilesSuggested:      int64Ptr(pr.ScopeFilesSuggested),
			BuildFilesChanged:        int64Ptr(pr.BuildFilesChanged),
			BuildScopeDrift:          pr.BuildScopeDrift,
			ScopeEstimatedComplexity: stringPtr(pr.ScopeEstimatedComplexity),
			ScopeRagDirect:           int64Ptr(pr.ScopeRagDirect),
			ScopeRagHops:             int64Ptr(pr.ScopeRagHops),
			ScopePathsStripped:       int64Ptr(pr.ScopePathsStripped),
			ScopePathsReclassified:   int64Ptr(pr.ScopePathsReclassified),
			BuildCostUSD:             float64Ptr(pr.BuildCostUSD),
			BuildToolCalls:           stringPtr(pr.BuildToolCalls),
		}
		resp.PipelineRun = &detail
	}
	return resp
}

func mapSubCalls(rows []database.UISubCall) []subCallResponse {
	out := make([]subCallResponse, 0, len(rows))
	for _, row := range rows {
		out = append(out, subCallResponse{
			Phase:            row.Phase,
			Step:             row.Step,
			Provider:         row.Provider,
			Model:            row.Model,
			TokensIn:         row.TokensIn,
			TokensOut:        row.TokensOut,
			LatencyMs:        row.LatencyMs,
			EstimatedCostUSD: row.EstimatedCostUSD,
			Success:          row.Success,
			ErrorMessage:     stringPtr(row.ErrorMessage),
		})
	}
	return out
}

// --- Diff / Scope / Approve / Reject response types ---

type diffStatsResponse struct {
	Additions int `json:"additions"`
	Deletions int `json:"deletions"`
	Files     int `json:"files"`
}

type workflowDiffResponse struct {
	Diff           string            `json:"diff"`
	BaseBranch     string            `json:"base_branch"`
	WorkflowBranch string            `json:"workflow_branch"`
	FilesChanged   []string          `json:"files_changed"`
	Stats          diffStatsResponse `json:"stats"`
}

type workflowScopeResponse struct {
	ContextPackage json.RawMessage `json:"context_package"`
	Source         string          `json:"source"`
}

type approveRequest struct {
	Reindex bool `json:"reindex"`
}

type approveResponse struct {
	Status     string `json:"status"`
	WorkflowID string `json:"workflow_id"`
	Merged     bool   `json:"merged"`
	Reindexed  bool   `json:"reindexed"`
}

type rejectRequest struct {
	Reason string `json:"reason"`
}

type rejectResponse struct {
	Status     string `json:"status"`
	WorkflowID string `json:"workflow_id"`
}

// --- Queue response/request types ---

type queueItemResponse struct {
	ID            string            `json:"id"`
	WorkOrderFile string            `json:"work_order_file"`
	Title         string            `json:"title"`
	Type          string            `json:"type"`
	TargetModule  string            `json:"target_module"`
	Overrides     map[string]string `json:"overrides,omitempty"`
	Status        string            `json:"status"`
	WorkflowID    string            `json:"workflow_id,omitempty"`
	Error         string            `json:"error,omitempty"`
	AddedAt       string            `json:"added_at"`
}

type queueStateResponse struct {
	State       string              `json:"state"`
	Items       []queueItemResponse `json:"items"`
	Current     string              `json:"current,omitempty"`
	PauseReason string              `json:"pause_reason,omitempty"`
}

type queueAddItemRequest struct {
	WorkOrderFile string            `json:"work_order_file"`
	Overrides     map[string]string `json:"overrides,omitempty"`
}

type queueAddRequest struct {
	Items []queueAddItemRequest `json:"items"`
}

type queueReorderRequest struct {
	Order []string `json:"order"`
}

func mapQueueState(snap QueueSnapshot) queueStateResponse {
	items := make([]queueItemResponse, 0, len(snap.Items))
	for _, item := range snap.Items {
		items = append(items, mapQueueItem(item))
	}
	return queueStateResponse{
		State:       snap.State,
		Items:       items,
		Current:     snap.Current,
		PauseReason: snap.PauseReason,
	}
}

func mapQueueItem(item QueueItem) queueItemResponse {
	return queueItemResponse{
		ID:            item.ID,
		WorkOrderFile: item.WorkOrderFile,
		Title:         item.Title,
		Type:          item.Type,
		TargetModule:  item.TargetModule,
		Overrides:     item.Overrides,
		Status:        item.Status,
		WorkflowID:    item.WorkflowID,
		Error:         item.Error,
		AddedAt:       item.AddedAt.Format(time.RFC3339),
	}
}

// --- Work order response types ---

type workOrderFileResponse struct {
	Filename     string `json:"filename"`
	Title        string `json:"title,omitempty"`
	Type         string `json:"type,omitempty"`
	TargetModule string `json:"target_module,omitempty"`
	SizeBytes    int64  `json:"size_bytes"`
	ModTime      string `json:"modified_at,omitempty"`
}

type workOrderListResponse struct {
	WorkOrders []workOrderFileResponse `json:"work_orders"`
	Directory  string                  `json:"directory,omitempty"`
}

type workOrderContentResponse struct {
	Filename string `json:"filename"`
	Content  string `json:"content"`
}

type workOrderUpdateRequest struct {
	Content string `json:"content"`
}

type workOrderUpdateResponse struct {
	Filename string `json:"filename"`
	Content  string `json:"content"`
	Valid    bool   `json:"valid"`
}

// --- Plan submission types ---

type planSubmitRequest struct {
	SpecContent  string `json:"spec_content"`
	SpecFilePath string `json:"spec_file_path"`
}

type planSubmitResponse struct {
	PlanRunID string `json:"plan_run_id"`
	State     string `json:"state"`
	SessionID string `json:"session_id"` // backward compat for frontend
}
