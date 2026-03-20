package api

import (
	"database/sql"
	"encoding/json"

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

func computeDelta(before, after sql.NullInt64) *int64 {
	if !before.Valid || !after.Valid {
		return nil
	}
	value := after.Int64 - before.Int64
	return &value
}
