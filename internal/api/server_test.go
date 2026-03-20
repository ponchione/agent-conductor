package api

import (
	"bufio"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ponchione/agent-conductor/internal/database"
	"github.com/ponchione/agent-conductor/internal/pipeline"
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
	if err := db.UpdatePipelineRunVerify(ctx, database.UpdatePipelineRunVerifyParams{
		VerifyResult:    sql.NullString{String: "PASS", Valid: true},
		BuildScopeDrift: 0,
		WorkflowID:      "wf-api-detail",
	}); err != nil {
		t.Fatalf("UpdatePipelineRunVerify() error: %v", err)
	}
	if err := db.UpdatePipelineRunHumanResult(ctx, database.UpdatePipelineRunHumanResultParams{
		HumanResult: sql.NullString{String: "approved", Valid: true},
		WorkflowID:  "wf-api-detail",
	}); err != nil {
		t.Fatalf("UpdatePipelineRunHumanResult() error: %v", err)
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
	if payload.PipelineRuns[0].VerifyResult == nil || *payload.PipelineRuns[0].VerifyResult != "PASS" {
		t.Fatalf("VerifyResult = %#v, want PASS", payload.PipelineRuns[0].VerifyResult)
	}
	if payload.PipelineRuns[0].HumanResult == nil || *payload.PipelineRuns[0].HumanResult != "approved" {
		t.Fatalf("HumanResult = %#v, want approved", payload.PipelineRuns[0].HumanResult)
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
	body := rec.Body.String()
	if !strings.Contains(body, "Conductor Observability") {
		t.Fatalf("body missing page title, body = %q", body)
	}
	if !strings.Contains(body, "/assets/app.js") {
		t.Fatalf("body missing React bundle path, body = %q", body)
	}
}

func TestObservabilityAssetsEndpoint(t *testing.T) {
	t.Parallel()

	db := newTestDB(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/assets/app.js", nil)

	NewServer(db).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if !strings.Contains(rec.Body.String(), "Observability Console") {
		t.Fatalf("body missing React app marker, body = %q", rec.Body.String())
	}
}

func TestEventStreamEndpointReplaysExistingEvents(t *testing.T) {
	t.Parallel()

	db := newTestDB(t)
	createEventWorkflowAndTask(t, db, "wf-stream-replay", "task-1")
	if err := db.LogEvent("wf-stream-replay", "task-1", pipeline.EventPhaseStart, map[string]any{"phase": "scope", "step": "decompose"}); err != nil {
		t.Fatalf("LogEvent(phase_start) error: %v", err)
	}
	if err := db.LogEvent("wf-stream-replay", "task-1", pipeline.EventPhaseComplete, map[string]any{"phase": "build", "duration": "1s"}); err != nil {
		t.Fatalf("LogEvent(phase_complete) error: %v", err)
	}

	server := httptest.NewServer(NewServer(db))
	defer server.Close()

	resp, reader, closeStream := openEventStream(t, server.URL+"/api/events/stream?workflow_id=wf-stream-replay")
	defer closeStream()

	if contentType := resp.Header.Get("Content-Type"); contentType != "text/event-stream" {
		t.Fatalf("Content-Type = %q, want text/event-stream", contentType)
	}
	if cacheControl := resp.Header.Get("Cache-Control"); cacheControl != "no-cache" {
		t.Fatalf("Cache-Control = %q, want no-cache", cacheControl)
	}

	first := readNextSSEEvent(t, reader)
	second := readNextSSEEvent(t, reader)

	if first.Event != pipeline.EventPhaseStart {
		t.Fatalf("first.Event = %q, want %s", first.Event, pipeline.EventPhaseStart)
	}
	if first.Payload.WorkflowID == nil || *first.Payload.WorkflowID != "wf-stream-replay" {
		t.Fatalf("first.Payload.WorkflowID = %#v, want wf-stream-replay", first.Payload.WorkflowID)
	}
	firstData, ok := first.Payload.EventData.(map[string]any)
	if !ok || firstData["step"] != "decompose" {
		t.Fatalf("first.Payload.EventData = %#v, want step=decompose", first.Payload.EventData)
	}

	if second.Event != pipeline.EventPhaseComplete {
		t.Fatalf("second.Event = %q, want %s", second.Event, pipeline.EventPhaseComplete)
	}
	secondData, ok := second.Payload.EventData.(map[string]any)
	if !ok || secondData["duration"] != "1s" {
		t.Fatalf("second.Payload.EventData = %#v, want duration=1s", second.Payload.EventData)
	}
	if second.ID <= first.ID {
		t.Fatalf("event IDs should increase, got first=%d second=%d", first.ID, second.ID)
	}
}

func TestEventStreamEndpointReplaysFromCursor(t *testing.T) {
	t.Parallel()

	db := newTestDB(t)
	createEventWorkflowAndTask(t, db, "wf-stream-cursor", "task-1")
	if err := db.LogEvent("wf-stream-cursor", "task-1", pipeline.EventPhaseStart, map[string]any{"phase": "scope"}); err != nil {
		t.Fatalf("LogEvent(phase_start) error: %v", err)
	}
	if err := db.LogEvent("wf-stream-cursor", "task-1", pipeline.EventScopeStep, map[string]any{"phase": "scope", "step": "validated"}); err != nil {
		t.Fatalf("LogEvent(scope_step) error: %v", err)
	}
	if err := db.LogEvent("wf-stream-cursor", "task-1", pipeline.EventVerifyResult, map[string]any{"phase": "verify", "status": "PASS"}); err != nil {
		t.Fatalf("LogEvent(verify_result) error: %v", err)
	}

	rows, err := db.ListEvents(context.Background(), sql.NullString{String: "wf-stream-cursor", Valid: true})
	if err != nil {
		t.Fatalf("ListEvents() error: %v", err)
	}
	if len(rows) != 3 {
		t.Fatalf("len(rows) = %d, want 3", len(rows))
	}

	server := httptest.NewServer(NewServer(db))
	defer server.Close()

	url := server.URL + "/api/events/stream?workflow_id=wf-stream-cursor&cursor=" + strconv.FormatInt(rows[1].ID, 10)
	_, reader, closeStream := openEventStream(t, url)
	defer closeStream()

	event := readNextSSEEvent(t, reader)
	if event.ID != rows[2].ID {
		t.Fatalf("event.ID = %d, want %d", event.ID, rows[2].ID)
	}
	if event.Event != pipeline.EventVerifyResult {
		t.Fatalf("event.Event = %q, want %s", event.Event, pipeline.EventVerifyResult)
	}
}

func TestEventStreamEndpointScopesByWorkflow(t *testing.T) {
	t.Parallel()

	db := newTestDB(t)
	createEventWorkflowAndTask(t, db, "wf-stream-a", "task-a")
	createEventWorkflowAndTask(t, db, "wf-stream-b", "task-b")
	if err := db.LogEvent("wf-stream-a", "task-a", pipeline.EventPhaseStart, map[string]any{"phase": "scope", "workflow": "a"}); err != nil {
		t.Fatalf("LogEvent(wf-stream-a) error: %v", err)
	}
	if err := db.LogEvent("wf-stream-b", "task-b", pipeline.EventPhaseStart, map[string]any{"phase": "scope", "workflow": "b"}); err != nil {
		t.Fatalf("LogEvent(wf-stream-b) error: %v", err)
	}

	server := httptest.NewServer(NewServer(db))
	defer server.Close()

	_, reader, closeStream := openEventStream(t, server.URL+"/api/events/stream?workflow_id=wf-stream-a")
	defer closeStream()

	event := readNextSSEEvent(t, reader)
	if event.Payload.WorkflowID == nil || *event.Payload.WorkflowID != "wf-stream-a" {
		t.Fatalf("event.Payload.WorkflowID = %#v, want wf-stream-a", event.Payload.WorkflowID)
	}
	if event.Event != pipeline.EventPhaseStart {
		t.Fatalf("event.Event = %q, want %s", event.Event, pipeline.EventPhaseStart)
	}
	data, ok := event.Payload.EventData.(map[string]any)
	if !ok || data["workflow"] != "a" {
		t.Fatalf("event.Payload.EventData = %#v, want workflow=a", event.Payload.EventData)
	}
}

func TestEventStreamEndpointDeliversNewEvents(t *testing.T) {
	t.Parallel()

	db := newTestDB(t)
	createEventWorkflowAndTask(t, db, "wf-stream-tail", "task-1")
	server := httptest.NewServer(NewServer(db))
	defer server.Close()

	_, reader, closeStream := openEventStream(t, server.URL+"/api/events/stream?workflow_id=wf-stream-tail")
	defer closeStream()

	go func() {
		time.Sleep(150 * time.Millisecond)
		_ = db.LogEvent("wf-stream-tail", "task-1", pipeline.EventBuildStdout, map[string]any{"phase": "build", "duration": "2s"})
	}()

	event := readNextSSEEvent(t, reader)
	if event.Event != pipeline.EventBuildStdout {
		t.Fatalf("event.Event = %q, want %s", event.Event, pipeline.EventBuildStdout)
	}
	data, ok := event.Payload.EventData.(map[string]any)
	if !ok || data["duration"] != "2s" {
		t.Fatalf("event.Payload.EventData = %#v, want duration=2s", event.Payload.EventData)
	}
}

func TestEventStreamEndpointStopsOnCancel(t *testing.T) {
	t.Parallel()

	db := newTestDB(t)
	ctx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequest(http.MethodGet, "/api/events/stream?workflow_id=wf-stream-cancel", nil).WithContext(ctx)
	rec := httptest.NewRecorder()

	done := make(chan struct{})
	go func() {
		NewServer(db).ServeHTTP(rec, req)
		close(done)
	}()

	time.Sleep(150 * time.Millisecond)
	cancel()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("stream handler did not stop after request cancellation")
	}

	if contentType := rec.Header().Get("Content-Type"); contentType != "text/event-stream" {
		t.Fatalf("Content-Type = %q, want text/event-stream", contentType)
	}
	if cacheControl := rec.Header().Get("Cache-Control"); cacheControl != "no-cache" {
		t.Fatalf("Cache-Control = %q, want no-cache", cacheControl)
	}
	if connection := rec.Header().Get("Connection"); connection != "keep-alive" {
		t.Fatalf("Connection = %q, want keep-alive", connection)
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

func createEventWorkflowAndTask(t *testing.T, db *database.DB, workflowID, taskID string) {
	t.Helper()

	ctx := context.Background()
	if err := db.CreateWorkflow(ctx, database.CreateWorkflowParams{
		ID:                     workflowID,
		OriginalIntent:         "stream test",
		OriginalFile:           "/tmp/work-order.yaml",
		CurrentState:           "pending",
		TargetRepo:             "repo",
		GitBranch:              "feature/stream-test",
		ContextPackagePath:     sql.NullString{},
		VerificationReportPath: sql.NullString{},
		MaxDepth:               3,
		MaxFilesChanged:        10,
		MaxDurationMins:        30,
	}); err != nil {
		t.Fatalf("CreateWorkflow() error: %v", err)
	}

	if err := db.CreateTask(ctx, database.CreateTaskParams{
		ID:            taskID,
		WorkflowID:    workflowID,
		SequenceNum:   1,
		TaskType:      "analysis",
		AgentType:     "test-agent",
		TargetRepo:    "repo",
		Phase:         "scope",
		InputArtifact: "/tmp/input.yaml",
		State:         "pending",
		MaxAttempts:   1,
	}); err != nil {
		t.Fatalf("CreateTask() error: %v", err)
	}
}

type sseEvent struct {
	ID      int64
	Event   string
	Payload eventStreamResponse
}

func openEventStream(t *testing.T, url string) (*http.Response, *bufio.Reader, func()) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		cancel()
		t.Fatalf("NewRequestWithContext() error: %v", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		cancel()
		t.Fatalf("Do() error: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		cancel()
		_ = resp.Body.Close()
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	closeStream := func() {
		cancel()
		_ = resp.Body.Close()
	}
	return resp, bufio.NewReader(resp.Body), closeStream
}

func readNextSSEEvent(t *testing.T, reader *bufio.Reader) sseEvent {
	t.Helper()

	var (
		eventID  int64
		eventSet bool
		data     strings.Builder
	)

	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			t.Fatalf("ReadString() error: %v", err)
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			break
		}

		switch {
		case strings.HasPrefix(line, "id: "):
			value := strings.TrimSpace(strings.TrimPrefix(line, "id: "))
			parsed, err := strconv.ParseInt(value, 10, 64)
			if err != nil {
				t.Fatalf("ParseInt(%q) error: %v", value, err)
			}
			eventID = parsed
			eventSet = true
		case strings.HasPrefix(line, "data: "):
			if data.Len() > 0 {
				data.WriteByte('\n')
			}
			data.WriteString(strings.TrimSpace(strings.TrimPrefix(line, "data: ")))
		}
	}

	if !eventSet {
		t.Fatal("missing SSE id field")
	}
	if data.Len() == 0 {
		t.Fatal("missing SSE data field")
	}

	var payload eventStreamResponse
	if err := json.Unmarshal([]byte(data.String()), &payload); err != nil {
		t.Fatalf("Unmarshal(data) error: %v", err)
	}

	return sseEvent{
		ID:      eventID,
		Event:   payload.EventType,
		Payload: payload,
	}
}
