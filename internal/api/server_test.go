package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ponchione/agent-conductor/internal/database"
)

func TestListSessionsEndpoint(t *testing.T) {
	t.Parallel()

	db := newTestDB(t)
	ctx := context.Background()

	sessionID, err := db.StartSession(ctx, database.SessionKindRunOnly, "repo", "/tmp/spec-a.md")
	if err != nil {
		t.Fatalf("StartSession() error: %v", err)
	}

	if err := db.CreateWorkflow(ctx, database.CreateWorkflowParams{
		ID:                     "wf-api-list",
		OriginalIntent:         "test",
		OriginalFile:           "/tmp/work-order.yaml",
		CurrentState:           "human_review",
		TargetRepo:             "repo",
		GitBranch:              "feature/test",
		ContextPackagePath:     sql.NullString{},
		VerificationReportPath: sql.NullString{},
		MaxDepth:               3,
		MaxFilesChanged:        10,
		MaxDurationMins:        30,
	}); err != nil {
		t.Fatalf("CreateWorkflow() error: %v", err)
	}
	if err := db.CreatePipelineRun(ctx, database.CreatePipelineRunParams{
		ID:            "pr-api-list",
		WorkflowID:    "wf-api-list",
		Project:       "repo",
		WorkOrderType: sql.NullString{String: "new_feature", Valid: true},
	}); err != nil {
		t.Fatalf("CreatePipelineRun() error: %v", err)
	}
	if err := db.LinkPipelineRunToSession(ctx, "pr-api-list", sessionID); err != nil {
		t.Fatalf("LinkPipelineRunToSession() error: %v", err)
	}

	otherSessionID, err := db.StartSession(ctx, database.SessionKindPlanOnly, "repo", "/tmp/spec-b.md")
	if err != nil {
		t.Fatalf("StartSession() error: %v", err)
	}
	if err := db.TransitionSessionState(ctx, otherSessionID, database.SessionStateCompleted, ""); err != nil {
		t.Fatalf("TransitionSessionState() error: %v", err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/sessions?state=running&limit=1", nil)

	NewServer(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var payload sessionListResponse
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if len(payload.Sessions) != 1 {
		t.Fatalf("len(payload.Sessions) = %d, want 1", len(payload.Sessions))
	}
	if payload.Sessions[0].ID != sessionID {
		t.Fatalf("payload.Sessions[0].ID = %q, want %q", payload.Sessions[0].ID, sessionID)
	}
	if payload.Sessions[0].LatestWorkflowState == nil || *payload.Sessions[0].LatestWorkflowState != "human_review" {
		t.Fatalf("LatestWorkflowState = %#v, want human_review", payload.Sessions[0].LatestWorkflowState)
	}
	if payload.Sessions[0].PipelineRunsCount != 1 {
		t.Fatalf("PipelineRunsCount = %d, want 1", payload.Sessions[0].PipelineRunsCount)
	}
}

func TestGetSessionDetailEndpoint(t *testing.T) {
	t.Parallel()

	db := newTestDB(t)
	ctx := context.Background()

	sessionID, err := db.StartSession(ctx, database.SessionKindPlanOnly, "repo", "/tmp/spec.md")
	if err != nil {
		t.Fatalf("StartSession() error: %v", err)
	}
	if err := db.InsertPlanRun(ctx, database.InsertPlanRunParams{
		ID:                      "plan-api-detail",
		SpecFile:                "spec.md",
		Project:                 sql.NullString{String: "repo", Valid: true},
		SpecFingerprint:         sql.NullString{String: "abc123", Valid: true},
		WorkOrdersGenerated:     sql.NullInt64{Int64: 2, Valid: true},
		PreAuditWorkOrderCount:  sql.NullInt64{Int64: 1, Valid: true},
		PostAuditWorkOrderCount: sql.NullInt64{Int64: 2, Valid: true},
		AuditChangeText:         sql.NullString{String: `["Add tests"]`, Valid: true},
	}); err != nil {
		t.Fatalf("InsertPlanRun() error: %v", err)
	}
	if err := db.LinkPlanRunToSession(ctx, "plan-api-detail", sessionID); err != nil {
		t.Fatalf("LinkPlanRunToSession() error: %v", err)
	}

	if err := db.CreateWorkflow(ctx, database.CreateWorkflowParams{
		ID:                     "wf-api-detail",
		OriginalIntent:         "detail test",
		OriginalFile:           "/tmp/work-order.yaml",
		CurrentState:           "pending",
		TargetRepo:             "repo",
		GitBranch:              "feature/test",
		ContextPackagePath:     sql.NullString{},
		VerificationReportPath: sql.NullString{},
		MaxDepth:               3,
		MaxFilesChanged:        10,
		MaxDurationMins:        30,
	}); err != nil {
		t.Fatalf("CreateWorkflow() error: %v", err)
	}
	if err := db.CreatePipelineRun(ctx, database.CreatePipelineRunParams{
		ID:            "pr-api-detail",
		WorkflowID:    "wf-api-detail",
		Project:       "repo",
		WorkOrderType: sql.NullString{String: "bugfix", Valid: true},
	}); err != nil {
		t.Fatalf("CreatePipelineRun() error: %v", err)
	}
	if err := db.LinkPipelineRunToSession(ctx, "pr-api-detail", sessionID); err != nil {
		t.Fatalf("LinkPipelineRunToSession() error: %v", err)
	}

	artifactPath := filepath.Join(t.TempDir(), "verify.txt")
	if err := os.WriteFile(artifactPath, []byte("report"), 0644); err != nil {
		t.Fatalf("WriteFile() error: %v", err)
	}
	artifact, err := db.RegisterArtifact(ctx, database.RegisterArtifactParams{
		SessionID:    sessionID,
		WorkflowID:   "wf-api-detail",
		ArtifactType: database.ArtifactTypeVerifyReport,
		Path:         artifactPath,
		Metadata: map[string]any{
			"kind": "report",
		},
	})
	if err != nil {
		t.Fatalf("RegisterArtifact() error: %v", err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/sessions/"+sessionID, nil)

	NewServer(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var payload sessionDetailResponse
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if payload.Session.ID != sessionID {
		t.Fatalf("payload.Session.ID = %q, want %q", payload.Session.ID, sessionID)
	}
	if len(payload.PlanRuns) != 1 {
		t.Fatalf("len(payload.PlanRuns) = %d, want 1", len(payload.PlanRuns))
	}
	if payload.PlanRuns[0].WorkOrderDelta == nil || *payload.PlanRuns[0].WorkOrderDelta != 1 {
		t.Fatalf("WorkOrderDelta = %#v, want 1", payload.PlanRuns[0].WorkOrderDelta)
	}
	if len(payload.PlanRuns[0].AuditChanges) != 1 || payload.PlanRuns[0].AuditChanges[0] != "Add tests" {
		t.Fatalf("AuditChanges = %#v, want [\"Add tests\"]", payload.PlanRuns[0].AuditChanges)
	}
	if len(payload.PipelineRuns) != 1 || payload.PipelineRuns[0].WorkflowID != "wf-api-detail" {
		t.Fatalf("PipelineRuns = %#v, want wf-api-detail", payload.PipelineRuns)
	}
	if len(payload.Artifacts) != 1 {
		t.Fatalf("len(payload.Artifacts) = %d, want 1", len(payload.Artifacts))
	}
	if payload.Artifacts[0].ID != artifact.ID {
		t.Fatalf("artifact ID = %q, want %q", payload.Artifacts[0].ID, artifact.ID)
	}
	metadata, ok := payload.Artifacts[0].Metadata.(map[string]any)
	if !ok || metadata["kind"] != "report" {
		t.Fatalf("artifact metadata = %#v, want kind=report", payload.Artifacts[0].Metadata)
	}
}

func TestGetSessionDetailEndpointNotFound(t *testing.T) {
	t.Parallel()

	db := newTestDB(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/sessions/does-not-exist", nil)

	NewServer(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestGetSessionDetailEndpointEmptyLinkedData(t *testing.T) {
	t.Parallel()

	db := newTestDB(t)
	ctx := context.Background()

	sessionID, err := db.StartSession(ctx, database.SessionKindPlanOnly, "repo", "/tmp/spec.md")
	if err != nil {
		t.Fatalf("StartSession() error: %v", err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/sessions/"+sessionID, nil)

	NewServer(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var payload sessionDetailResponse
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(payload.PlanRuns) != 0 || len(payload.PipelineRuns) != 0 || len(payload.Artifacts) != 0 {
		t.Fatalf("expected empty linked data, got plan=%d pipeline=%d artifacts=%d", len(payload.PlanRuns), len(payload.PipelineRuns), len(payload.Artifacts))
	}
}

func TestGetPlanAuditStatsEndpoint(t *testing.T) {
	t.Parallel()

	db := newTestDB(t)
	ctx := context.Background()

	if err := db.InsertPlanRun(ctx, database.InsertPlanRunParams{
		ID:                      "plan-changed",
		SpecFile:                "changed.md",
		Project:                 sql.NullString{String: "repo", Valid: true},
		WorkOrdersGenerated:     sql.NullInt64{Int64: 2, Valid: true},
		PreAuditWorkOrderCount:  sql.NullInt64{Int64: 1, Valid: true},
		PostAuditWorkOrderCount: sql.NullInt64{Int64: 2, Valid: true},
		AuditChangeText:         sql.NullString{String: `["Add migration"]`, Valid: true},
	}); err != nil {
		t.Fatalf("InsertPlanRun(changed) error: %v", err)
	}
	if err := db.InsertPlanRun(ctx, database.InsertPlanRunParams{
		ID:                      "plan-unchanged",
		SpecFile:                "unchanged.md",
		Project:                 sql.NullString{String: "repo", Valid: true},
		WorkOrdersGenerated:     sql.NullInt64{Int64: 1, Valid: true},
		PreAuditWorkOrderCount:  sql.NullInt64{Int64: 1, Valid: true},
		PostAuditWorkOrderCount: sql.NullInt64{Int64: 1, Valid: true},
		AuditWorkOrdersAdded:    sql.NullInt64{Int64: 0, Valid: true},
		AuditWorkOrdersModified: sql.NullInt64{Int64: 0, Valid: true},
		AuditChangeText:         sql.NullString{},
	}); err != nil {
		t.Fatalf("InsertPlanRun(unchanged) error: %v", err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/stats/plan-audit?limit=0", nil)

	NewServer(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var payload planAuditStatsResponse
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if payload.Summary.ChangedRuns != 1 || payload.Summary.UnchangedRuns != 1 || payload.Summary.TotalRuns != 2 {
		t.Fatalf("summary = %#v, want changed=1 unchanged=1 total=2", payload.Summary)
	}
	if len(payload.RecentRuns) != 2 {
		t.Fatalf("len(payload.RecentRuns) = %d, want 2", len(payload.RecentRuns))
	}

	foundChanged := false
	foundUnchanged := false
	for _, row := range payload.RecentRuns {
		switch row.ID {
		case "plan-changed":
			foundChanged = true
			if !row.AuditChanged {
				t.Fatal("plan-changed should have AuditChanged=true")
			}
			if len(row.AuditChanges) != 1 || row.AuditChanges[0] != "Add migration" {
				t.Fatalf("plan-changed AuditChanges = %#v", row.AuditChanges)
			}
		case "plan-unchanged":
			foundUnchanged = true
			if row.AuditChanged {
				t.Fatal("plan-unchanged should have AuditChanged=false")
			}
		}
	}
	if !foundChanged || !foundUnchanged {
		t.Fatalf("recent run ids missing, payload = %#v", payload.RecentRuns)
	}
}

func TestGetPlanAuditStatsEndpointEmpty(t *testing.T) {
	t.Parallel()

	db := newTestDB(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/stats/plan-audit", nil)

	NewServer(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var payload planAuditStatsResponse
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload.Summary.TotalRuns != 0 || payload.Summary.ChangedRuns != 0 || payload.Summary.UnchangedRuns != 0 {
		t.Fatalf("summary = %#v, want all zero", payload.Summary)
	}
	if len(payload.RecentRuns) != 0 {
		t.Fatalf("len(payload.RecentRuns) = %d, want 0", len(payload.RecentRuns))
	}
}

func TestListSessionsEndpointRejectsInvalidLimit(t *testing.T) {
	t.Parallel()

	db := newTestDB(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/sessions?limit=-1", nil)

	NewServer(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestGetPlanAuditStatsEndpointRejectsInvalidLimit(t *testing.T) {
	t.Parallel()

	db := newTestDB(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/stats/plan-audit?limit=abc", nil)

	NewServer(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestObservabilityPageEndpoint(t *testing.T) {
	t.Parallel()

	db := newTestDB(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/observability", nil)

	NewServer(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if contentType := rec.Header().Get("Content-Type"); contentType != "text/html; charset=utf-8" {
		t.Fatalf("Content-Type = %q, want text/html; charset=utf-8", contentType)
	}
	if body := rec.Body.String(); !strings.Contains(body, "Observability Console") {
		t.Fatalf("body missing page title, body = %q", body)
	}
}

func newTestDB(t *testing.T) *database.DB {
	t.Helper()

	dbPath := filepath.Join(t.TempDir(), "conductor.db")
	db, err := database.NewDB(dbPath)
	if err != nil {
		t.Fatalf("NewDB() error: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}
