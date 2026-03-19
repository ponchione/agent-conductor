package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"github.com/ponchione/agent-conductor/internal/config"
	"github.com/ponchione/agent-conductor/internal/database"
	"github.com/ponchione/agent-conductor/internal/models"
	"github.com/ponchione/agent-conductor/internal/templates"
)

func TestMergeInvokeClaudeResults(t *testing.T) {
	base := &invokeClaudeResult{
		Content:   "first",
		TokensIn:  10,
		TokensOut: 5,
		Model:     "model-a",
		CostUSD:   0.10,
		Duration:  2 * time.Second,
		SessionID: "sess-a",
		ToolCalls: map[string]int{"Read": 1},
	}
	retry := &invokeClaudeResult{
		Content:   "second",
		TokensIn:  20,
		TokensOut: 8,
		Model:     "model-b",
		CostUSD:   0.15,
		Duration:  3 * time.Second,
		SessionID: "sess-b",
		ToolCalls: map[string]int{"Read": 2, "Bash": 1},
	}

	mergeInvokeClaudeResults(base, retry)

	if base.Content != "second" {
		t.Fatalf("Content = %q, want %q", base.Content, "second")
	}
	if base.TokensIn != 30 {
		t.Fatalf("TokensIn = %d, want 30", base.TokensIn)
	}
	if base.TokensOut != 13 {
		t.Fatalf("TokensOut = %d, want 13", base.TokensOut)
	}
	if base.CostUSD != 0.25 {
		t.Fatalf("CostUSD = %f, want 0.25", base.CostUSD)
	}
	if base.Duration != 5*time.Second {
		t.Fatalf("Duration = %s, want 5s", base.Duration)
	}
	if base.Model != "model-b" {
		t.Fatalf("Model = %q, want %q", base.Model, "model-b")
	}
	if base.SessionID != "sess-b" {
		t.Fatalf("SessionID = %q, want %q", base.SessionID, "sess-b")
	}
	if base.ToolCalls["Read"] != 3 {
		t.Fatalf("ToolCalls[Read] = %d, want 3", base.ToolCalls["Read"])
	}
	if base.ToolCalls["Bash"] != 1 {
		t.Fatalf("ToolCalls[Bash] = %d, want 1", base.ToolCalls["Bash"])
	}
}

func TestGeneratePlanDocumentAssemblesHierarchicalManifest(t *testing.T) {
	projectDir := t.TempDir()
	for _, relPath := range []string{
		"cmd/conductor/plan.go",
		"internal/templates/prompts.go",
	} {
		absPath := filepath.Join(projectDir, relPath)
		if err := os.MkdirAll(filepath.Dir(absPath), 0755); err != nil {
			t.Fatalf("MkdirAll(%q) error: %v", absPath, err)
		}
		if err := os.WriteFile(absPath, []byte("package stub\n"), 0644); err != nil {
			t.Fatalf("WriteFile(%q) error: %v", absPath, err)
		}
	}

	cfg := &config.ProjectConfig{
		Project: config.Project{Path: projectDir},
		Index: config.Index{
			Include: []string{"**/*.go", "**/*.md", "**/*.yaml"},
		},
	}
	prompts := &templates.LoadedPrompts{
		PlanEpic: "epic prompt",
		PlanTask: "task prompt",
	}

	var phases []string
	invoker := func(phase, systemPrompt, userMsg string) (*invokeClaudeResult, error) {
		phases = append(phases, phase)
		switch phase {
		case "planning_epic":
			return &invokeClaudeResult{
				Content: `{
					"requirements": [
						{"id": "REQ-1", "text": "Configurable planner prompts"},
						{"id": "REQ-2", "text": "Persist plan metadata"}
					],
					"epics": [
						{
							"epic_ref": "planner-prompts",
							"title": "Planner prompt plumbing",
							"description": "Load and route prompt templates through the planner.",
							"covers": ["REQ-1"],
							"depends_on_epics": []
						},
						{
							"epic_ref": "plan-metadata",
							"title": "Plan metadata persistence",
							"description": "Persist plan metadata and related artifacts.",
							"covers": ["REQ-2"],
							"depends_on_epics": ["planner-prompts"]
						}
					]
				}`,
				TokensIn:  100,
				TokensOut: 25,
				Model:     "claude-sonnet",
				CostUSD:   0.25,
				Duration:  2 * time.Second,
			}, nil
		case "planning_task_epic-001":
			if !strings.Contains(userMsg, `"epic_ref": "planner-prompts"`) {
				t.Fatalf("epic-001 user message missing target epic context:\n%s", userMsg)
			}
			return &invokeClaudeResult{
				Content: `{
					"tasks": [
						{
							"task_ref": "wire-planner-prompts",
							"schema_version": 2,
							"title": "Wire planner prompts",
							"type": "refactor",
							"target_module": "cmd/conductor",
							"reference_module": "internal/templates",
							"known_files": ["cmd/conductor/plan.go", "internal/templates/prompts.go"],
							"requirements": [
								{"id": "REQ-1", "text": "Configurable planner prompts"}
							],
							"acceptance_criteria": [
								{
									"id": "AC-1",
									"description": "Configured planner prompts load successfully",
									"requirement_ids": ["REQ-1"],
									"required": true,
									"verification": {"kind": "diff_review", "focus": ["cmd/conductor/plan.go"]}
								}
							],
							"constraints": ["Keep current YAML output"],
							"depends_on": [],
							"size": "S"
						}
					]
				}`,
				TokensIn:  50,
				TokensOut: 20,
				Model:     "claude-sonnet",
				CostUSD:   0.10,
				Duration:  time.Second,
			}, nil
		case "planning_task_epic-002":
			if !strings.Contains(userMsg, `"task_ref": "wire-planner-prompts"`) {
				t.Fatalf("epic-002 user message missing prior task context:\n%s", userMsg)
			}
			if !strings.Contains(userMsg, `"id": "task-001"`) {
				t.Fatalf("epic-002 user message missing canonical prior task id:\n%s", userMsg)
			}
			return &invokeClaudeResult{
				Content: `{
					"tasks": [
						{
							"task_ref": "persist-plan-metadata",
							"schema_version": 2,
							"title": "Persist plan metadata artifacts",
							"type": "new_feature",
							"target_module": "cmd/conductor",
							"reference_module": "cmd/conductor/plan.go",
							"known_files": ["cmd/conductor/plan.go"],
							"requirements": [
								{"id": "REQ-2", "text": "Persist plan metadata"}
							],
							"acceptance_criteria": [
								{
									"id": "AC-2",
									"description": "Structured plan artifacts are written",
									"requirement_ids": ["REQ-2"],
									"required": true,
									"verification": {"kind": "diff_review", "focus": ["cmd/conductor/plan.go"]}
								}
							],
							"constraints": ["Keep generated YAML compatible with current pipeline"],
							"depends_on": ["wire-planner-prompts"],
							"size": "M"
						}
					]
				}`,
				TokensIn:  60,
				TokensOut: 30,
				Model:     "claude-sonnet",
				CostUSD:   0.12,
				Duration:  1500 * time.Millisecond,
			}, nil
		default:
			t.Fatalf("unexpected phase %q", phase)
			return nil, nil
		}
	}

	doc, trace, retryCount, err := generatePlanDocument("spec.md", []byte("spec body"), "session-123", cfg, prompts, invoker)
	if err != nil {
		t.Fatalf("generatePlanDocument() error = %v", err)
	}
	if retryCount != 0 {
		t.Fatalf("retryCount = %d, want 0", retryCount)
	}
	if got, want := phases, []string{"planning_epic", "planning_task_epic-001", "planning_task_epic-002"}; len(got) != len(want) || strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("phases = %v, want %v", got, want)
	}
	if trace == nil {
		t.Fatal("trace = nil, want planning trace")
	}
	if !strings.Contains(trace.AggregateRaw, "=== EPIC GENERATION ===") || !strings.Contains(trace.AggregateRaw, "=== TASK GENERATION epic-002 ===") {
		t.Fatalf("trace.AggregateRaw missing expected sections:\n%s", trace.AggregateRaw)
	}
	if trace.AggregateResult == nil {
		t.Fatal("trace.AggregateResult = nil, want aggregate result")
	}
	if trace.AggregateResult.TokensIn != 210 || trace.AggregateResult.TokensOut != 75 {
		t.Fatalf("aggregate tokens = (%d,%d), want (210,75)", trace.AggregateResult.TokensIn, trace.AggregateResult.TokensOut)
	}
	if len(trace.TaskTraces) != 2 {
		t.Fatalf("len(trace.TaskTraces) = %d, want 2", len(trace.TaskTraces))
	}
	if doc.Version != 1 || doc.SpecFile != "spec.md" || doc.SessionID != "session-123" {
		t.Fatalf("doc metadata = %#v, want version/spec/session set", doc)
	}
	if len(doc.Epics) != 2 {
		t.Fatalf("len(Epics) = %d, want 2", len(doc.Epics))
	}
	if doc.Epics[0].ID != "epic-001" || doc.Epics[1].ID != "epic-002" {
		t.Fatalf("epic IDs = (%q,%q), want (epic-001, epic-002)", doc.Epics[0].ID, doc.Epics[1].ID)
	}
	if got := doc.Epics[1].DependsOnEpics; len(got) != 1 || got[0] != "epic-001" {
		t.Fatalf("DependsOnEpics = %v, want [epic-001]", got)
	}
	if doc.Epics[0].Tasks[0].ID != "task-001" || doc.Epics[1].Tasks[0].ID != "task-002" {
		t.Fatalf("task IDs = (%q,%q), want (task-001, task-002)", doc.Epics[0].Tasks[0].ID, doc.Epics[1].Tasks[0].ID)
	}
	if got := doc.Epics[1].Tasks[0].DependsOn; len(got) != 1 || got[0] != "task-001" {
		t.Fatalf("task DependsOn = %v, want [task-001]", got)
	}
}

func TestGeneratePlanDocumentFailsBeforeAuditOnInvalidAssembledManifest(t *testing.T) {
	projectDir := t.TempDir()
	absPath := filepath.Join(projectDir, "cmd", "conductor", "plan.go")
	if err := os.MkdirAll(filepath.Dir(absPath), 0755); err != nil {
		t.Fatalf("MkdirAll(%q) error: %v", absPath, err)
	}
	if err := os.WriteFile(absPath, []byte("package stub\n"), 0644); err != nil {
		t.Fatalf("WriteFile(%q) error: %v", absPath, err)
	}

	cfg := &config.ProjectConfig{
		Project: config.Project{Path: projectDir},
		Index: config.Index{
			Include: []string{"**/*.go", "**/*.md", "**/*.yaml"},
		},
	}
	prompts := &templates.LoadedPrompts{
		PlanEpic: "epic prompt",
		PlanTask: "task prompt",
	}

	invoker := func(phase, systemPrompt, userMsg string) (*invokeClaudeResult, error) {
		switch phase {
		case "planning_epic":
			return &invokeClaudeResult{
				Content: `{
					"requirements": [{"id": "REQ-1", "text": "Configurable planner prompts"}],
					"epics": [
						{
							"epic_ref": "planner-prompts",
							"title": "Planner prompt plumbing",
							"description": "Load and route prompt templates through the planner.",
							"covers": ["REQ-1"],
							"depends_on_epics": []
						}
					]
				}`,
			}, nil
		case "planning_task_epic-001":
			return &invokeClaudeResult{
				Content: `{
					"tasks": [
						{
							"task_ref": "wire-planner-prompts",
							"schema_version": 2,
							"title": "Wire planner prompts",
							"type": "refactor",
							"target_module": "cmd/conductor",
							"reference_module": "internal/templates",
							"known_files": ["does/not/exist.go"],
							"requirements": [{"id": "REQ-1", "text": "Configurable planner prompts"}],
							"acceptance_criteria": [
								{
									"id": "AC-1",
									"description": "Configured planner prompts load successfully",
									"requirement_ids": ["REQ-1"],
									"required": true,
									"verification": {"kind": "diff_review", "focus": ["cmd/conductor/plan.go"]}
								}
							],
							"constraints": ["Keep current YAML output"],
							"depends_on": [],
							"size": "S"
						}
					]
				}`,
			}, nil
		default:
			t.Fatalf("unexpected phase %q", phase)
			return nil, nil
		}
	}

	_, _, _, err := generatePlanDocument("spec.md", []byte("spec body"), "session-123", cfg, prompts, invoker)
	if err == nil || !strings.Contains(err.Error(), "planner output validation failed") {
		t.Fatalf("generatePlanDocument() error = %v, want pre-audit validation failure", err)
	}
}

func TestRecordPlanRunPersistsObservabilityFields(t *testing.T) {
	tmpDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(tmpDir, "db"), 0755); err != nil {
		t.Fatalf("mkdir db: %v", err)
	}

	cfg := &config.ProjectConfig{}
	cfg.Project.DataDir = tmpDir

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

	generationTrace := &planGenerationTrace{
		EpicResult: &invokeClaudeResult{
			Model:     "claude-sonnet-4-6",
			TokensIn:  100,
			TokensOut: 25,
			CostUSD:   0.42,
			Duration:  1500 * time.Millisecond,
		},
		TaskTraces: []planTaskGenerationTrace{
			{
				EpicID: "epic-001",
				Result: &invokeClaudeResult{
					Model:     "claude-sonnet-4-6",
					TokensIn:  30,
					TokensOut: 10,
					CostUSD:   0.11,
					Duration:  600 * time.Millisecond,
				},
			},
			{
				EpicID: "epic-002",
				Result: &invokeClaudeResult{
					Model:     "claude-sonnet-4-6",
					TokensIn:  20,
					TokensOut: 5,
					CostUSD:   0.07,
					Duration:  400 * time.Millisecond,
				},
			},
		},
		AggregateResult: &invokeClaudeResult{
			Model:     "claude-sonnet-4-6",
			TokensIn:  150,
			TokensOut: 40,
			CostUSD:   0.60,
			Duration:  2500 * time.Millisecond,
			SessionID: "sess-plan",
		},
	}
	auditResult := &invokeClaudeResult{
		Model:     "claude-sonnet-4-6",
		TokensIn:  80,
		TokensOut: 15,
		CostUSD:   0.18,
		Duration:  900 * time.Millisecond,
		SessionID: "sess-audit",
	}
	summary := &auditSummary{
		Added:     1,
		Modified:  2,
		Unchanged: 3,
		Changes: []string{
			"Added missing migration work order",
			"Clarified test expectations",
		},
	}

	specData := []byte("spec body")
	if err := recordPlanRun(conductorDB, sessionID, "repo", "spec.md", specData, generationTrace, auditResult, 2, 4, 6, summary, 1); err != nil {
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

func TestPersistPlanningArtifactsWritesHierarchicalOutputs(t *testing.T) {
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

	sessionID, err := conductorDB.StartSession(t.Context(), database.SessionKindPlanOnly, "repo", "spec.md")
	if err != nil {
		t.Fatalf("StartSession() error: %v", err)
	}

	planDoc := &planDocument{
		Version:   1,
		SpecFile:  "spec.md",
		SessionID: sessionID,
		Requirements: []planRequirement{
			{ID: "REQ-1", Text: "Keep audit manifest-based"},
		},
		Epics: []planEpic{
			{
				ID:          "epic-001",
				EpicRef:     "planner-observability",
				Title:       "Planner observability",
				Description: "Persist artifacts and metrics for hierarchical planning",
				Covers:      []string{"REQ-1"},
				Tasks: []planTask{
					{
						ID:            "task-001",
						TaskRef:       "persist-artifacts",
						SchemaVersion: 2,
						Title:         "Persist hierarchical artifacts",
						Type:          "new_feature",
						TargetModule:  "cmd/conductor",
						Requirements: []models.WorkOrderRequirement{
							{ID: "REQ-1", Text: "Keep audit manifest-based"},
						},
						AcceptanceCriteria: []models.TypedAcceptanceCriterion{
							{
								ID:             "AC-1",
								Description:    "Artifacts are persisted per phase",
								RequirementIDs: []string{"REQ-1"},
								Required:       testBoolPtr(true),
								Verification: models.AcceptanceVerification{
									Kind:  "diff_review",
									Focus: []string{"cmd/conductor/plan.go"},
								},
							},
						},
						Size: "S",
					},
				},
			},
		},
	}

	manifestDir := filepath.Join(tmpDir, "output")
	manifestPath, err := writePlanManifest(planDoc, manifestDir)
	if err != nil {
		t.Fatalf("writePlanManifest() error: %v", err)
	}

	trace := &planGenerationTrace{
		EpicRaw: `{"requirements":[{"id":"REQ-1","text":"Keep audit manifest-based"}],"epics":[{"epic_ref":"planner-observability","title":"Planner observability","description":"Persist artifacts and metrics for hierarchical planning","covers":["REQ-1"],"depends_on_epics":[]}]}`,
		EpicResponse: &rawEpicPlanResponse{
			Requirements: []planRequirement{
				{ID: "REQ-1", Text: "Keep audit manifest-based"},
			},
			Epics: []rawPlanEpic{
				{
					EpicRef:        "planner-observability",
					Title:          "Planner observability",
					Description:    "Persist artifacts and metrics for hierarchical planning",
					Covers:         []string{"REQ-1"},
					DependsOnEpics: []string{},
				},
			},
		},
		TaskTraces: []planTaskGenerationTrace{
			{
				EpicID:  "epic-001",
				EpicRef: "planner-observability",
				Raw:     `{"tasks":[{"task_ref":"persist-artifacts","schema_version":2,"title":"Persist hierarchical artifacts","type":"new_feature","target_module":"cmd/conductor","requirements":[{"id":"REQ-1","text":"Keep audit manifest-based"}],"acceptance_criteria":[],"depends_on":[],"size":"S"}]}`,
				Response: &rawTaskPlanResponse{
					Tasks: []rawPlanTask{
						{
							TaskRef:       "persist-artifacts",
							SchemaVersion: 2,
							Title:         "Persist hierarchical artifacts",
							Type:          "new_feature",
							TargetModule:  "cmd/conductor",
							Requirements: []models.WorkOrderRequirement{
								{ID: "REQ-1", Text: "Keep audit manifest-based"},
							},
							AcceptanceCriteria: []models.TypedAcceptanceCriterion{
								{
									ID:             "AC-1",
									Description:    "Artifacts are persisted per phase",
									RequirementIDs: []string{"REQ-1"},
									Required:       testBoolPtr(true),
									Verification: models.AcceptanceVerification{
										Kind:  "diff_review",
										Focus: []string{"cmd/conductor/plan.go"},
									},
								},
							},
							DependsOn: []string{},
							Size:      "S",
						},
					},
				},
			},
		},
	}
	auditResult := &invokeClaudeResult{Content: `{"audit_summary":{"added":0,"modified":1,"unchanged":0},"epics":[]}`}
	summary := &auditSummary{Modified: 1}

	if err := persistPlanningArtifacts(t.Context(), conductorDB, sessionID, tmpDir, trace, auditResult, summary, planDoc, manifestPath); err != nil {
		t.Fatalf("persistPlanningArtifacts() error = %v", err)
	}

	artifacts, err := conductorDB.ListArtifactsBySession(t.Context(), sessionID)
	if err != nil {
		t.Fatalf("ListArtifactsBySession() error = %v", err)
	}

	counts := make(map[string]int)
	var manifestArtifact database.Artifact
	for _, artifact := range artifacts {
		counts[artifact.ArtifactType]++
		if artifact.ArtifactType == database.ArtifactTypePlanManifest {
			manifestArtifact = artifact
		}
	}

	if counts[database.ArtifactTypePlanRawEpicGeneration] != 1 {
		t.Fatalf("raw epic artifacts = %d, want 1", counts[database.ArtifactTypePlanRawEpicGeneration])
	}
	if counts[database.ArtifactTypePlanStructuredEpicGeneration] != 1 {
		t.Fatalf("structured epic artifacts = %d, want 1", counts[database.ArtifactTypePlanStructuredEpicGeneration])
	}
	if counts[database.ArtifactTypePlanRawTaskGeneration] != 1 {
		t.Fatalf("raw task artifacts = %d, want 1", counts[database.ArtifactTypePlanRawTaskGeneration])
	}
	if counts[database.ArtifactTypePlanStructuredTaskGeneration] != 1 {
		t.Fatalf("structured task artifacts = %d, want 1", counts[database.ArtifactTypePlanStructuredTaskGeneration])
	}
	if counts[database.ArtifactTypePlanRawAudit] != 1 {
		t.Fatalf("raw audit artifacts = %d, want 1", counts[database.ArtifactTypePlanRawAudit])
	}
	if counts[database.ArtifactTypePlanStructuredAudit] != 1 {
		t.Fatalf("structured audit artifacts = %d, want 1", counts[database.ArtifactTypePlanStructuredAudit])
	}
	if counts[database.ArtifactTypePlanManifest] != 1 {
		t.Fatalf("manifest artifacts = %d, want 1", counts[database.ArtifactTypePlanManifest])
	}
	if manifestArtifact.Path != manifestPath {
		t.Fatalf("manifest artifact path = %q, want %q", manifestArtifact.Path, manifestPath)
	}

	var metadata map[string]any
	if err := json.Unmarshal([]byte(manifestArtifact.MetadataJSON.String), &metadata); err != nil {
		t.Fatalf("json.Unmarshal(manifest metadata) error = %v", err)
	}
	if metadata["phase"] != "manifest" {
		t.Fatalf("manifest metadata phase = %v, want %q", metadata["phase"], "manifest")
	}
	if metadata["epic_count"] != float64(1) || metadata["task_count"] != float64(1) {
		t.Fatalf("manifest metadata counts = %#v, want epic/task count of 1", metadata)
	}
}

func TestBuildAuditUserMessageUsesGeneratedManifest(t *testing.T) {
	projectDir := t.TempDir()
	for _, relPath := range []string{
		"cmd/conductor/plan.go",
		"internal/templates/prompts.go",
	} {
		absPath := filepath.Join(projectDir, relPath)
		if err := os.MkdirAll(filepath.Dir(absPath), 0755); err != nil {
			t.Fatalf("MkdirAll(%q) error: %v", absPath, err)
		}
		if err := os.WriteFile(absPath, []byte("package stub\n"), 0644); err != nil {
			t.Fatalf("WriteFile(%q) error: %v", absPath, err)
		}
	}

	cfg := &config.ProjectConfig{
		Project: config.Project{Path: projectDir},
		Index: config.Index{
			Include: []string{"**/*.go", "**/*.md", "**/*.yaml"},
		},
	}
	planDoc := &planDocument{
		Version:  1,
		SpecFile: "spec.md",
		Epics: []planEpic{
			{
				ID:          "epic-001",
				EpicRef:     "planner-observability",
				Title:       "Planner observability",
				Description: "Persist artifacts and metrics for hierarchical planning",
				Tasks: []planTask{
					{
						ID:            "task-001",
						TaskRef:       "persist-artifacts",
						SchemaVersion: 2,
						Title:         "Persist hierarchical artifacts",
						Type:          "new_feature",
						TargetModule:  "cmd/conductor",
						AcceptanceCriteria: []models.TypedAcceptanceCriterion{
							{
								ID:             "AC-1",
								Description:    "Audit uses the manifest",
								RequirementIDs: []string{"REQ-1"},
								Required:       testBoolPtr(true),
								Verification: models.AcceptanceVerification{
									Kind:  "diff_review",
									Focus: []string{"cmd/conductor/plan.go"},
								},
							},
						},
						Requirements: []models.WorkOrderRequirement{
							{ID: "REQ-1", Text: "Keep audit manifest-based"},
						},
						Size: "S",
					},
				},
			},
		},
	}

	userMsg, err := buildAuditUserMessage("spec body", planDoc, cfg)
	if err != nil {
		t.Fatalf("buildAuditUserMessage() error = %v", err)
	}

	if !strings.Contains(userMsg, "=== GENERATED PLAN ===") {
		t.Fatalf("audit user message missing generated plan section:\n%s", userMsg)
	}
	if !strings.Contains(userMsg, `"id": "epic-001"`) {
		t.Fatalf("audit user message missing canonical epic id:\n%s", userMsg)
	}
	if !strings.Contains(userMsg, `"id": "task-001"`) {
		t.Fatalf("audit user message missing canonical task id:\n%s", userMsg)
	}
	if !strings.Contains(userMsg, "spec body") {
		t.Fatalf("audit user message missing spec body:\n%s", userMsg)
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
	planDoc := &planDocument{
		Version:  1,
		SpecFile: "spec.md",
		Requirements: []planRequirement{
			{ID: "REQ-1", Text: "Status should be derived from latest runs and dependencies"},
		},
		Epics: []planEpic{
			{
				ID:          "epic-001",
				EpicRef:     "first-epic",
				Title:       "First epic",
				Description: "First group of tasks",
				Covers:      []string{"REQ-1"},
				Tasks: []planTask{
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
				Tasks: []planTask{
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

func newStatusTestTask(taskID, taskRef, epicID string, dependsOn []string) planTask {
	return planTask{
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
