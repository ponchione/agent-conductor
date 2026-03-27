package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"github.com/ponchione/agent-conductor/internal/config"
	"github.com/ponchione/agent-conductor/internal/database"
	"github.com/ponchione/agent-conductor/internal/models"
	"github.com/ponchione/agent-conductor/internal/planner"
)

func TestRecordPlanRunPersistsObservabilityFields(t *testing.T) {
	tmpDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(tmpDir, "db"), 0755); err != nil {
		t.Fatalf("mkdir db: %v", err)
	}

	cfg := &config.ProjectConfig{}
	cfg.Project.DataDir = tmpDir
	_ = cfg // cfg is used by the test setup context

	dbPath := filepath.Join(tmpDir, "db", "conductor.db")
	conductorDB, err := database.NewDB(dbPath)
	if err != nil {
		t.Fatalf("database.NewDB() error: %v", err)
	}
	defer conductorDB.Close()

	sessionID, err := conductorDB.StartSession(t.Context(), database.SessionKindPlanOnly, "repo", "spec.md")
	if err != nil {
		t.Fatalf("StartSession() error: %v", err)
	}

	result := &planner.GenerateResult{
		PlanDoc: &planner.PlanDocument{Version: 1},
		GenerationTrace: &planner.PlanGenerationTrace{
			EpicResult: &planner.InvokeClaudeResult{
				Model:     "claude-sonnet-4-6",
				TokensIn:  100,
				TokensOut: 25,
				CostUSD:   0.42,
				Duration:  1500 * time.Millisecond,
			},
			TaskTraces: []planner.PlanTaskGenerationTrace{
				{
					EpicID: "epic-001",
					Result: &planner.InvokeClaudeResult{
						Model:     "claude-sonnet-4-6",
						TokensIn:  30,
						TokensOut: 10,
						CostUSD:   0.11,
						Duration:  600 * time.Millisecond,
					},
				},
				{
					EpicID: "epic-002",
					Result: &planner.InvokeClaudeResult{
						Model:     "claude-sonnet-4-6",
						TokensIn:  20,
						TokensOut: 5,
						CostUSD:   0.07,
						Duration:  400 * time.Millisecond,
					},
				},
			},
			AggregateResult: &planner.InvokeClaudeResult{
				Model:     "claude-sonnet-4-6",
				TokensIn:  150,
				TokensOut: 40,
				CostUSD:   0.60,
				Duration:  2500 * time.Millisecond,
				SessionID: "sess-plan",
			},
		},
		AuditResult: &planner.InvokeClaudeResult{
			Model:     "claude-sonnet-4-6",
			TokensIn:  80,
			TokensOut: 15,
			CostUSD:   0.18,
			Duration:  900 * time.Millisecond,
			SessionID: "sess-audit",
		},
		AuditSummary: &planner.AuditSummary{
			Added:     1,
			Modified:  2,
			Unchanged: 3,
			Changes: []string{
				"Added missing migration work order",
				"Clarified test expectations",
			},
		},
		Metrics: planner.PlanRunMetrics{
			EpicCount:               2,
			TaskCount:               6,
			WorkOrdersGenerated:     6,
			PreAuditWorkOrderCount:  4,
			PostAuditWorkOrderCount: 6,
			GenerationRetryCount:    1,
		},
	}

	specData := []byte("spec body")
	if err := recordPlanRun(conductorDB, sessionID, "repo", "spec.md", specData, result); err != nil {
		t.Fatalf("recordPlanRun: %v", err)
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()

	row := db.QueryRow(`
		SELECT
			session_id,
			spec_file,
			project,
			spec_fingerprint,
			generation_model,
			epic_generation_model,
			task_generation_model,
			audit_model,
			generation_session_id,
			audit_session_id,
			work_orders_generated,
			epic_count,
			task_count,
			pre_audit_work_order_count,
			post_audit_work_order_count,
			audit_change_text,
			audit_work_orders_added,
			audit_work_orders_modified,
			audit_work_orders_unchanged,
			generation_cost_usd,
			epic_generation_cost_usd,
			task_generation_cost_usd,
			audit_cost_usd,
			generation_duration_ms,
			epic_generation_duration_ms,
			task_generation_duration_ms,
			audit_duration_ms,
			generation_retry_count,
			generation_tokens_in,
			generation_tokens_out,
			epic_generation_tokens_in,
			epic_generation_tokens_out,
			task_generation_call_count,
			task_generation_tokens_in,
			task_generation_tokens_out,
			audit_tokens_in,
			audit_tokens_out
		FROM plan_runs
		LIMIT 1
	`)

	var (
		recordedSessionID        sql.NullString
		specFile                 string
		project                  sql.NullString
		specFingerprint          sql.NullString
		generationModel          sql.NullString
		epicGenerationModel      sql.NullString
		taskGenerationModel      sql.NullString
		auditModel               sql.NullString
		generationSessionID      sql.NullString
		auditSessionID           sql.NullString
		generatedTaskCount       sql.NullInt64
		epicCount                sql.NullInt64
		taskCount                sql.NullInt64
		preAuditTaskCount        sql.NullInt64
		postAuditTaskCount       sql.NullInt64
		auditChangeText          sql.NullString
		auditWorkOrdersAdded     sql.NullInt64
		auditWorkOrdersModified  sql.NullInt64
		auditWorkOrdersUnchanged sql.NullInt64
		generationCostUSD        sql.NullFloat64
		epicGenerationCostUSD    sql.NullFloat64
		taskGenerationCostUSD    sql.NullFloat64
		auditCostUSD             sql.NullFloat64
		generationDurationMs     sql.NullInt64
		epicGenerationDurationMs sql.NullInt64
		taskGenerationDurationMs sql.NullInt64
		auditDurationMs          sql.NullInt64
		generationRetryCount     int64
		generationTokensIn       sql.NullInt64
		generationTokensOut      sql.NullInt64
		epicGenerationTokensIn   sql.NullInt64
		epicGenerationTokensOut  sql.NullInt64
		taskGenerationCallCount  sql.NullInt64
		taskGenerationTokensIn   sql.NullInt64
		taskGenerationTokensOut  sql.NullInt64
		auditTokensIn            sql.NullInt64
		auditTokensOut           sql.NullInt64
	)
	if err := row.Scan(
		&recordedSessionID,
		&specFile,
		&project,
		&specFingerprint,
		&generationModel,
		&epicGenerationModel,
		&taskGenerationModel,
		&auditModel,
		&generationSessionID,
		&auditSessionID,
		&generatedTaskCount,
		&epicCount,
		&taskCount,
		&preAuditTaskCount,
		&postAuditTaskCount,
		&auditChangeText,
		&auditWorkOrdersAdded,
		&auditWorkOrdersModified,
		&auditWorkOrdersUnchanged,
		&generationCostUSD,
		&epicGenerationCostUSD,
		&taskGenerationCostUSD,
		&auditCostUSD,
		&generationDurationMs,
		&epicGenerationDurationMs,
		&taskGenerationDurationMs,
		&auditDurationMs,
		&generationRetryCount,
		&generationTokensIn,
		&generationTokensOut,
		&epicGenerationTokensIn,
		&epicGenerationTokensOut,
		&taskGenerationCallCount,
		&taskGenerationTokensIn,
		&taskGenerationTokensOut,
		&auditTokensIn,
		&auditTokensOut,
	); err != nil {
		t.Fatalf("scan row: %v", err)
	}

	if specFile != "spec.md" {
		t.Fatalf("spec_file = %q, want %q", specFile, "spec.md")
	}
	if recordedSessionID.String != sessionID {
		t.Fatalf("session_id = %q, want %q", recordedSessionID.String, sessionID)
	}
	if project.String != "repo" {
		t.Fatalf("project = %q, want %q", project.String, "repo")
	}
	expectedFingerprint := sha256.Sum256(specData)
	if specFingerprint.String != hex.EncodeToString(expectedFingerprint[:]) {
		t.Fatalf("spec_fingerprint = %q, want %q", specFingerprint.String, hex.EncodeToString(expectedFingerprint[:]))
	}
	if generationModel.String != "claude-sonnet-4-6" {
		t.Fatalf("generation_model = %q, want %q", generationModel.String, "claude-sonnet-4-6")
	}
	if epicGenerationModel.String != "claude-sonnet-4-6" {
		t.Fatalf("epic_generation_model = %q, want %q", epicGenerationModel.String, "claude-sonnet-4-6")
	}
	if taskGenerationModel.String != "claude-sonnet-4-6" {
		t.Fatalf("task_generation_model = %q, want %q", taskGenerationModel.String, "claude-sonnet-4-6")
	}
	if auditModel.String != "claude-sonnet-4-6" {
		t.Fatalf("audit_model = %q, want %q", auditModel.String, "claude-sonnet-4-6")
	}
	if generationSessionID.String != "sess-plan" {
		t.Fatalf("generation_session_id = %q, want %q", generationSessionID.String, "sess-plan")
	}
	if auditSessionID.String != "sess-audit" {
		t.Fatalf("audit_session_id = %q, want %q", auditSessionID.String, "sess-audit")
	}
	if generatedTaskCount.Int64 != 6 {
		t.Fatalf("work_orders_generated = %d, want 6", generatedTaskCount.Int64)
	}
	if epicCount.Int64 != 2 {
		t.Fatalf("epic_count = %d, want 2", epicCount.Int64)
	}
	if taskCount.Int64 != 6 {
		t.Fatalf("task_count = %d, want 6", taskCount.Int64)
	}
	if preAuditTaskCount.Int64 != 4 {
		t.Fatalf("pre_audit_work_order_count = %d, want 4", preAuditTaskCount.Int64)
	}
	if postAuditTaskCount.Int64 != 6 {
		t.Fatalf("post_audit_work_order_count = %d, want 6", postAuditTaskCount.Int64)
	}
	if auditChangeText.String != `["Added missing migration work order","Clarified test expectations"]` {
		t.Fatalf("audit_change_text = %q, want serialized changes", auditChangeText.String)
	}
	if auditWorkOrdersAdded.Int64 != 1 || auditWorkOrdersModified.Int64 != 2 || auditWorkOrdersUnchanged.Int64 != 3 {
		t.Fatalf(
			"audit summary = (%d,%d,%d), want (1,2,3)",
			auditWorkOrdersAdded.Int64,
			auditWorkOrdersModified.Int64,
			auditWorkOrdersUnchanged.Int64,
		)
	}
	if generationCostUSD.Float64 != 0.60 {
		t.Fatalf("generation_cost_usd = %f, want 0.60", generationCostUSD.Float64)
	}
	if epicGenerationCostUSD.Float64 != 0.42 {
		t.Fatalf("epic_generation_cost_usd = %f, want 0.42", epicGenerationCostUSD.Float64)
	}
	if taskGenerationCostUSD.Float64 != 0.18 {
		t.Fatalf("task_generation_cost_usd = %f, want 0.18", taskGenerationCostUSD.Float64)
	}
	if auditCostUSD.Float64 != 0.18 {
		t.Fatalf("audit_cost_usd = %f, want 0.18", auditCostUSD.Float64)
	}
	if generationDurationMs.Int64 != 2500 {
		t.Fatalf("generation_duration_ms = %d, want 2500", generationDurationMs.Int64)
	}
	if epicGenerationDurationMs.Int64 != 1500 {
		t.Fatalf("epic_generation_duration_ms = %d, want 1500", epicGenerationDurationMs.Int64)
	}
	if taskGenerationDurationMs.Int64 != 1000 {
		t.Fatalf("task_generation_duration_ms = %d, want 1000", taskGenerationDurationMs.Int64)
	}
	if auditDurationMs.Int64 != 900 {
		t.Fatalf("audit_duration_ms = %d, want 900", auditDurationMs.Int64)
	}
	if generationRetryCount != 1 {
		t.Fatalf("generation_retry_count = %d, want 1", generationRetryCount)
	}
	if generationTokensIn.Int64 != 150 || generationTokensOut.Int64 != 40 {
		t.Fatalf("generation tokens = (%d,%d), want (150,40)", generationTokensIn.Int64, generationTokensOut.Int64)
	}
	if epicGenerationTokensIn.Int64 != 100 || epicGenerationTokensOut.Int64 != 25 {
		t.Fatalf("epic generation tokens = (%d,%d), want (100,25)", epicGenerationTokensIn.Int64, epicGenerationTokensOut.Int64)
	}
	if taskGenerationCallCount.Int64 != 2 {
		t.Fatalf("task_generation_call_count = %d, want 2", taskGenerationCallCount.Int64)
	}
	if taskGenerationTokensIn.Int64 != 50 || taskGenerationTokensOut.Int64 != 15 {
		t.Fatalf("task generation tokens = (%d,%d), want (50,15)", taskGenerationTokensIn.Int64, taskGenerationTokensOut.Int64)
	}
	if auditTokensIn.Int64 != 80 || auditTokensOut.Int64 != 15 {
		t.Fatalf("audit tokens = (%d,%d), want (80,15)", auditTokensIn.Int64, auditTokensOut.Int64)
	}
}

func TestBuildPlanStatusReportResolvesLatestRunsAndBlockedState(t *testing.T) {
	tmpDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(tmpDir, "db"), 0755); err != nil {
		t.Fatalf("mkdir db: %v", err)
	}

	dbPath := filepath.Join(tmpDir, "db", "conductor.db")
	conductorDB, err := database.NewDB(dbPath)
	if err != nil {
		t.Fatalf("database.NewDB() error: %v", err)
	}
	defer conductorDB.Close()

	ctx := context.Background()
	planPath := filepath.Join(tmpDir, "plan.yaml")
	planDoc := &planner.PlanDocument{
		Version:  1,
		SpecFile: "spec.md",
		Requirements: []planner.PlanRequirement{
			{ID: "REQ-1", Text: "Status should be derived from latest runs and dependencies"},
		},
		Epics: []planner.PlanEpic{
			{
				ID:          "epic-001",
				EpicRef:     "first-epic",
				Title:       "First epic",
				Description: "First group of tasks",
				Covers:      []string{"REQ-1"},
				Tasks: []planner.PlanTask{
					newStatusTestTask("task-001", "bootstrap-foundation", "epic-001", nil),
					newStatusTestTask("task-002", "failing-task", "epic-001", nil),
					newStatusTestTask("task-003", "review-task", "epic-001", nil),
				},
			},
			{
				ID:          "epic-002",
				EpicRef:     "second-epic",
				Title:       "Second epic",
				Description: "Second group of tasks",
				Covers:      []string{"REQ-1"},
				Tasks: []planner.PlanTask{
					newStatusTestTask("task-004", "running-task", "epic-002", nil),
					newStatusTestTask("task-005", "ready-task", "epic-002", []string{"task-001"}),
					newStatusTestTask("task-006", "blocked-task", "epic-002", []string{"task-005"}),
				},
			},
		},
	}

	if err := createPlanStatusRun(ctx, conductorDB, "wf-old-complete", "run-001", planPath, "task-001", "epic-001", "failed", "FAIL", "rejected"); err != nil {
		t.Fatalf("createPlanStatusRun(old complete) error: %v", err)
	}
	if err := createPlanStatusRun(ctx, conductorDB, "wf-new-complete", "run-002", planPath, "task-001", "epic-001", "completed", "PASS", "approved"); err != nil {
		t.Fatalf("createPlanStatusRun(new complete) error: %v", err)
	}
	if err := createPlanStatusRun(ctx, conductorDB, "wf-failed", "run-003", planPath, "task-002", "epic-001", "failed", "FAIL", "rejected"); err != nil {
		t.Fatalf("createPlanStatusRun(failed) error: %v", err)
	}
	if err := createPlanStatusRun(ctx, conductorDB, "wf-review", "run-004", planPath, "task-003", "epic-001", "human_review", "PASS", ""); err != nil {
		t.Fatalf("createPlanStatusRun(review) error: %v", err)
	}
	if err := createPlanStatusRun(ctx, conductorDB, "wf-running", "run-005", planPath, "task-004", "epic-002", "build", "", ""); err != nil {
		t.Fatalf("createPlanStatusRun(running) error: %v", err)
	}

	report, err := buildPlanStatusReport(ctx, conductorDB, planPath, planDoc)
	if err != nil {
		t.Fatalf("buildPlanStatusReport() error = %v", err)
	}

	if report.Summary.Complete != 1 || report.Summary.Failed != 1 || report.Summary.InProgress != 1 || report.Summary.AwaitingReview != 1 || report.Summary.Blocked != 1 || report.Summary.NotStarted != 1 {
		t.Fatalf("summary = %#v, want one task in each status bucket", report.Summary)
	}

	gotStatuses := map[string]planTaskExecutionStatus{}
	for _, epic := range report.Epics {
		for _, task := range epic.Tasks {
			gotStatuses[task.Task.ID] = task.Status
		}
	}
	wantStatuses := map[string]planTaskExecutionStatus{
		"task-001": planTaskStatusComplete,
		"task-002": planTaskStatusFailed,
		"task-003": planTaskStatusAwaitingReview,
		"task-004": planTaskStatusInProgress,
		"task-005": planTaskStatusNotStarted,
		"task-006": planTaskStatusBlocked,
	}
	for taskID, wantStatus := range wantStatuses {
		if gotStatuses[taskID] != wantStatus {
			t.Fatalf("status[%s] = %q, want %q", taskID, gotStatuses[taskID], wantStatus)
		}
	}

	var output bytes.Buffer
	if err := renderPlanStatusReport(&output, report); err != nil {
		t.Fatalf("renderPlanStatusReport() error = %v", err)
	}
	rendered := output.String()
	if !strings.Contains(rendered, "epic-001 First epic") || !strings.Contains(rendered, "epic-002 Second epic") {
		t.Fatalf("rendered output missing epic grouping:\n%s", rendered)
	}
	if !strings.Contains(rendered, "[blocked] task-006 blocked-task") {
		t.Fatalf("rendered output missing blocked task line:\n%s", rendered)
	}
	if !strings.Contains(rendered, "Summary: complete 1  failed 1  in-progress 1") {
		t.Fatalf("rendered output missing summary line:\n%s", rendered)
	}
}

func newStatusTestTask(taskID, taskRef, epicID string, dependsOn []string) planner.PlanTask {
	return planner.PlanTask{
		ID:            taskID,
		TaskRef:       taskRef,
		SchemaVersion: 2,
		EpicID:        epicID,
		Title:         taskRef,
		Type:          "new_feature",
		TargetModule:  "cmd/conductor",
		Requirements: []models.WorkOrderRequirement{
			{ID: "REQ-1", Text: "Status should be derived from latest runs and dependencies"},
		},
		AcceptanceCriteria: []models.TypedAcceptanceCriterion{
			{
				ID:             "AC-" + taskID,
				Description:    "Task status is visible",
				RequirementIDs: []string{"REQ-1"},
				Required:       testBoolPtr(true),
				Verification: models.AcceptanceVerification{
					Kind:  "diff_review",
					Focus: []string{"cmd/conductor/planstatus.go"},
				},
			},
		},
		DependsOn: append([]string(nil), dependsOn...),
		Size:      "S",
	}
}

func createPlanStatusRun(ctx context.Context, db *database.DB, workflowID, pipelineRunID, planPath, taskID, epicID, workflowState, verifyResult, humanResult string) error {
	if err := db.CreateWorkflow(ctx, database.CreateWorkflowParams{
		ID:                     workflowID,
		OriginalIntent:         "Work Order: " + taskID,
		OriginalFile:           planPath,
		CurrentState:           workflowState,
		TargetRepo:             "repo",
		GitBranch:              "feature/" + workflowID,
		ContextPackagePath:     sql.NullString{},
		VerificationReportPath: sql.NullString{},
		MaxDepth:               5,
		MaxFilesChanged:        25,
		MaxDurationMins:        45,
	}); err != nil {
		return err
	}

	if err := db.CreatePipelineRun(ctx, database.CreatePipelineRunParams{
		ID:            pipelineRunID,
		WorkflowID:    workflowID,
		Project:       "repo",
		WorkOrderType: sql.NullString{String: "new_feature", Valid: true},
		PlanFile:      database.String(planPath),
		PlanTaskID:    database.String(taskID),
		PlanEpicID:    database.String(epicID),
	}); err != nil {
		return err
	}

	if verifyResult != "" {
		if err := db.UpdatePipelineRunVerify(ctx, database.UpdatePipelineRunVerifyParams{
			VerifyStartedAt:   sql.NullString{},
			VerifyCompletedAt: sql.NullString{},
			VerifyTokensIn:    sql.NullInt64{},
			VerifyTokensOut:   sql.NullInt64{},
			VerifyModel:       sql.NullString{},
			VerifyResult:      sql.NullString{String: verifyResult, Valid: true},
			BuildScopeDrift:   0,
			WorkflowID:        workflowID,
		}); err != nil {
			return err
		}
	}

	if humanResult != "" {
		if err := db.UpdatePipelineRunHumanResult(ctx, database.UpdatePipelineRunHumanResultParams{
			HumanResult: sql.NullString{String: humanResult, Valid: true},
			WorkflowID:  workflowID,
		}); err != nil {
			return err
		}
	}

	return nil
}

func testBoolPtr(v bool) *bool {
	return &v
}
