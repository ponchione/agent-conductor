package main

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"github.com/ponchione/agent-conductor/internal/config"
	"github.com/ponchione/agent-conductor/internal/database"
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

	genResult := &invokeClaudeResult{
		Model:     "claude-sonnet-4-6",
		TokensIn:  100,
		TokensOut: 25,
		CostUSD:   0.42,
		Duration:  1500 * time.Millisecond,
		SessionID: "sess-plan",
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
	if err := recordPlanRun(conductorDB, sessionID, "repo", "spec.md", specData, genResult, auditResult, 4, 6, summary, 1); err != nil {
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
			audit_model,
			generation_session_id,
			audit_session_id,
			work_orders_generated,
			pre_audit_work_order_count,
			post_audit_work_order_count,
			audit_change_text,
			audit_work_orders_added,
			audit_work_orders_modified,
			audit_work_orders_unchanged,
			generation_cost_usd,
			audit_cost_usd,
			generation_duration_ms,
			audit_duration_ms,
			generation_retry_count,
			generation_tokens_in,
			generation_tokens_out,
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
		auditModel               sql.NullString
		generationSessionID      sql.NullString
		auditSessionID           sql.NullString
		workOrdersGenerated      sql.NullInt64
		preAuditWorkOrderCount   sql.NullInt64
		postAuditWorkOrderCount  sql.NullInt64
		auditChangeText          sql.NullString
		auditWorkOrdersAdded     sql.NullInt64
		auditWorkOrdersModified  sql.NullInt64
		auditWorkOrdersUnchanged sql.NullInt64
		generationCostUSD        sql.NullFloat64
		auditCostUSD             sql.NullFloat64
		generationDurationMs     sql.NullInt64
		auditDurationMs          sql.NullInt64
		generationRetryCount     int64
		generationTokensIn       sql.NullInt64
		generationTokensOut      sql.NullInt64
		auditTokensIn            sql.NullInt64
		auditTokensOut           sql.NullInt64
	)
	if err := row.Scan(
		&recordedSessionID,
		&specFile,
		&project,
		&specFingerprint,
		&generationModel,
		&auditModel,
		&generationSessionID,
		&auditSessionID,
		&workOrdersGenerated,
		&preAuditWorkOrderCount,
		&postAuditWorkOrderCount,
		&auditChangeText,
		&auditWorkOrdersAdded,
		&auditWorkOrdersModified,
		&auditWorkOrdersUnchanged,
		&generationCostUSD,
		&auditCostUSD,
		&generationDurationMs,
		&auditDurationMs,
		&generationRetryCount,
		&generationTokensIn,
		&generationTokensOut,
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
	if auditModel.String != "claude-sonnet-4-6" {
		t.Fatalf("audit_model = %q, want %q", auditModel.String, "claude-sonnet-4-6")
	}
	if generationSessionID.String != "sess-plan" {
		t.Fatalf("generation_session_id = %q, want %q", generationSessionID.String, "sess-plan")
	}
	if auditSessionID.String != "sess-audit" {
		t.Fatalf("audit_session_id = %q, want %q", auditSessionID.String, "sess-audit")
	}
	if workOrdersGenerated.Int64 != 6 {
		t.Fatalf("work_orders_generated = %d, want 6", workOrdersGenerated.Int64)
	}
	if preAuditWorkOrderCount.Int64 != 4 {
		t.Fatalf("pre_audit_work_order_count = %d, want 4", preAuditWorkOrderCount.Int64)
	}
	if postAuditWorkOrderCount.Int64 != 6 {
		t.Fatalf("post_audit_work_order_count = %d, want 6", postAuditWorkOrderCount.Int64)
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
	if generationCostUSD.Float64 != 0.42 {
		t.Fatalf("generation_cost_usd = %f, want 0.42", generationCostUSD.Float64)
	}
	if auditCostUSD.Float64 != 0.18 {
		t.Fatalf("audit_cost_usd = %f, want 0.18", auditCostUSD.Float64)
	}
	if generationDurationMs.Int64 != 1500 {
		t.Fatalf("generation_duration_ms = %d, want 1500", generationDurationMs.Int64)
	}
	if auditDurationMs.Int64 != 900 {
		t.Fatalf("audit_duration_ms = %d, want 900", auditDurationMs.Int64)
	}
	if generationRetryCount != 1 {
		t.Fatalf("generation_retry_count = %d, want 1", generationRetryCount)
	}
	if generationTokensIn.Int64 != 100 || generationTokensOut.Int64 != 25 {
		t.Fatalf("generation tokens = (%d,%d), want (100,25)", generationTokensIn.Int64, generationTokensOut.Int64)
	}
	if auditTokensIn.Int64 != 80 || auditTokensOut.Int64 != 15 {
		t.Fatalf("audit tokens = (%d,%d), want (80,15)", auditTokensIn.Int64, auditTokensOut.Int64)
	}
}
