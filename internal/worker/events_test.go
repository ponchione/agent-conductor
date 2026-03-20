package worker

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/ponchione/agent-conductor/internal/config"
	"github.com/ponchione/agent-conductor/internal/database"
	pipelineerrors "github.com/ponchione/agent-conductor/internal/errors"
	"github.com/ponchione/agent-conductor/internal/executor"
	"github.com/ponchione/agent-conductor/internal/models"
	"github.com/ponchione/agent-conductor/internal/pipeline"
	"github.com/ponchione/agent-conductor/internal/queue"
	"github.com/ponchione/agent-conductor/internal/verify"
)

func TestEmitSuccessfulVerifyLifecycleEvents(t *testing.T) {
	db, task, _ := newWorkerEventFixture(t, "wf-worker-success", "task-verify", "verify")
	w := &Worker{db: db}

	required := true
	precheck := verify.PreCheckResult{
		CriterionID:      "AC-1",
		Criterion:        "Configured smoke check passes",
		Required:         &required,
		Result:           models.CriterionResultMet,
		VerificationKind: "http_smoke",
		Notes:            "ok",
	}
	report := &models.VerificationReport{
		Status:  "PASS",
		Summary: "Verification succeeded.",
		ScopeDrift: models.ScopeDrift{
			Detected: false,
		},
		Completeness: models.Completeness{
			AllCriteriaMet: true,
		},
	}

	w.emitPhaseStart(context.Background(), task, nil)
	w.emitVerifyPrecheck(context.Background(), task, precheck)
	w.emitVerifyResult(context.Background(), task, report)
	w.emitPhaseComplete(context.Background(), task, map[string]any{"status": report.Status})
	w.emitRunAwaitingReview(context.Background(), task.WorkflowID, task.ID, task.Phase, map[string]any{"status": report.Status})

	events := listWorkflowEvents(t, db, task.WorkflowID)
	if len(events) != 5 {
		t.Fatalf("len(events) = %d, want 5", len(events))
	}
	assertEventType(t, events[0], pipeline.EventPhaseStart)
	assertEventType(t, events[1], pipeline.EventVerifyPrecheck)
	assertEventType(t, events[2], pipeline.EventVerifyResult)
	assertEventType(t, events[3], pipeline.EventPhaseComplete)
	assertEventType(t, events[4], pipeline.EventRunAwaitingReview)

	verifyPayload := decodeEventPayload(t, events[2])
	if verifyPayload["status"] != "PASS" {
		t.Fatalf("verifyPayload[status] = %#v, want PASS", verifyPayload["status"])
	}
	if verifyPayload["session_id"] == "" {
		t.Fatalf("verifyPayload[session_id] = %#v, want linked session ID", verifyPayload["session_id"])
	}
}

func TestHandleTaskFailureNeedsHumanEmitsAwaitingReview(t *testing.T) {
	db, task, sessionID := newWorkerEventFixture(t, "wf-worker-review", "task-build", "build")
	w := &Worker{
		db: db,
		q:  queue.New(&config.ProjectConfig{}, db),
	}

	err := pipelineerrors.NeedsHumanf(task.Phase, task.WorkflowID, task.ID, "build needs review")
	w.handleTaskFailure(context.Background(), task, err, err)

	workflow, getWorkflowErr := db.GetWorkflow(context.Background(), task.WorkflowID)
	if getWorkflowErr != nil {
		t.Fatalf("GetWorkflow() error: %v", getWorkflowErr)
	}
	if workflow.CurrentState != "human_review" {
		t.Fatalf("workflow.CurrentState = %q, want human_review", workflow.CurrentState)
	}

	session, getSessionErr := db.GetSession(context.Background(), sessionID)
	if getSessionErr != nil {
		t.Fatalf("GetSession() error: %v", getSessionErr)
	}
	if session.State != database.SessionStateAwaitingReview {
		t.Fatalf("session.State = %q, want awaiting_review", session.State)
	}

	events := listWorkflowEvents(t, db, task.WorkflowID)
	if len(events) != 2 {
		t.Fatalf("len(events) = %d, want 2", len(events))
	}
	assertEventType(t, events[0], pipeline.EventPhaseError)
	assertEventType(t, events[1], pipeline.EventRunAwaitingReview)

	payload := decodeEventPayload(t, events[1])
	if payload["reason"] != "needs_human" {
		t.Fatalf("payload[reason] = %#v, want needs_human", payload["reason"])
	}
}

func TestHandleTaskFailureFatalEmitsRunComplete(t *testing.T) {
	db, task, sessionID := newWorkerEventFixture(t, "wf-worker-failed", "task-scope", "scope")
	w := &Worker{
		db: db,
		q:  queue.New(&config.ProjectConfig{}, db),
	}

	err := pipelineerrors.Fatalf(task.Phase, task.WorkflowID, task.ID, "scope failed permanently")
	w.handleTaskFailure(context.Background(), task, err, err)

	workflow, getWorkflowErr := db.GetWorkflow(context.Background(), task.WorkflowID)
	if getWorkflowErr != nil {
		t.Fatalf("GetWorkflow() error: %v", getWorkflowErr)
	}
	if workflow.CurrentState != "failed" {
		t.Fatalf("workflow.CurrentState = %q, want failed", workflow.CurrentState)
	}

	session, getSessionErr := db.GetSession(context.Background(), sessionID)
	if getSessionErr != nil {
		t.Fatalf("GetSession() error: %v", getSessionErr)
	}
	if session.State != database.SessionStateFailed {
		t.Fatalf("session.State = %q, want failed", session.State)
	}

	events := listWorkflowEvents(t, db, task.WorkflowID)
	if len(events) != 2 {
		t.Fatalf("len(events) = %d, want 2", len(events))
	}
	assertEventType(t, events[0], pipeline.EventPhaseError)
	assertEventType(t, events[1], pipeline.EventRunComplete)

	payload := decodeEventPayload(t, events[1])
	if payload["final_state"] != "failed" {
		t.Fatalf("payload[final_state] = %#v, want failed", payload["final_state"])
	}
}

func TestEmitBuildStdoutEventBoundsPreview(t *testing.T) {
	db, task, _ := newWorkerEventFixture(t, "wf-worker-build-stdout", "task-build-stdout", "build")
	w := &Worker{db: db}

	stdoutPath := filepath.Join(t.TempDir(), "stdout.log")
	stdoutBody := strings.Repeat("0123456789", int(maxBuildStdoutPreviewBytes/10)+32)
	if err := os.WriteFile(stdoutPath, []byte(stdoutBody), 0644); err != nil {
		t.Fatalf("WriteFile() error: %v", err)
	}

	w.emitBuildStdout(context.Background(), task, &executor.RunResult{
		ExitCode:   0,
		Duration:   0,
		StdoutPath: stdoutPath,
		Success:    true,
		SessionID:  "build-session-123",
		ToolCalls:  map[string]int{"Read": 3},
	})

	events := listWorkflowEvents(t, db, task.WorkflowID)
	if len(events) != 1 {
		t.Fatalf("len(events) = %d, want 1", len(events))
	}
	assertEventType(t, events[0], pipeline.EventBuildStdout)

	payload := decodeEventPayload(t, events[0])
	preview, ok := payload["preview"].(string)
	if !ok {
		t.Fatalf("payload[preview] = %#v, want string", payload["preview"])
	}
	if len(preview) > int(maxBuildStdoutPreviewBytes) {
		t.Fatalf("len(preview) = %d, want <= %d", len(preview), maxBuildStdoutPreviewBytes)
	}
	if payload["truncated"] != true {
		t.Fatalf("payload[truncated] = %#v, want true", payload["truncated"])
	}
}

func newWorkerEventFixture(t *testing.T, workflowID, taskID, phase string) (*database.DB, *database.Task, string) {
	t.Helper()

	dbPath := filepath.Join(t.TempDir(), "conductor.db")
	db, err := database.NewDB(dbPath)
	if err != nil {
		t.Fatalf("NewDB() error: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	ctx := context.Background()
	sessionID, err := db.StartSession(ctx, database.SessionKindRunOnly, "repo", "/tmp/spec.md")
	if err != nil {
		t.Fatalf("StartSession() error: %v", err)
	}

	if err := db.CreateWorkflow(ctx, database.CreateWorkflowParams{
		ID:                     workflowID,
		OriginalIntent:         "test",
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

	if err := db.CreateTask(ctx, database.CreateTaskParams{
		ID:            taskID,
		WorkflowID:    workflowID,
		SequenceNum:   1,
		TaskType:      "execution",
		AgentType:     "claude-code",
		TargetRepo:    "repo",
		Phase:         phase,
		InputArtifact: "/tmp/input.json",
		State:         "pending",
		MaxAttempts:   2,
	}); err != nil {
		t.Fatalf("CreateTask() error: %v", err)
	}

	if err := db.CreatePipelineRun(ctx, database.CreatePipelineRunParams{
		ID:            "pr-" + workflowID,
		WorkflowID:    workflowID,
		Project:       "repo",
		WorkOrderType: sql.NullString{String: "new_feature", Valid: true},
	}); err != nil {
		t.Fatalf("CreatePipelineRun() error: %v", err)
	}
	if err := db.LinkPipelineRunToSession(ctx, "pr-"+workflowID, sessionID); err != nil {
		t.Fatalf("LinkPipelineRunToSession() error: %v", err)
	}

	task, err := db.GetTask(ctx, taskID)
	if err != nil {
		t.Fatalf("GetTask() error: %v", err)
	}
	return db, &task, sessionID
}

func listWorkflowEvents(t *testing.T, db *database.DB, workflowID string) []database.Event {
	t.Helper()

	events, err := db.ListEvents(context.Background(), sql.NullString{String: workflowID, Valid: true})
	if err != nil {
		t.Fatalf("ListEvents() error: %v", err)
	}
	return events
}

func assertEventType(t *testing.T, event database.Event, want string) {
	t.Helper()
	if event.EventType != want {
		t.Fatalf("event.EventType = %q, want %q", event.EventType, want)
	}
}

func decodeEventPayload(t *testing.T, event database.Event) map[string]any {
	t.Helper()

	var payload map[string]any
	if err := json.Unmarshal([]byte(event.EventData.String), &payload); err != nil {
		t.Fatalf("Unmarshal(event payload) error: %v", err)
	}
	return payload
}

func TestReadBuildStdoutPreviewSanitizesUTF8(t *testing.T) {
	// Place a 4-byte emoji (U+1F600, bytes f0 9f 98 80) so that the
	// tail-read boundary lands 2 bytes into it, producing 2 orphan
	// continuation bytes at the start of the preview window.
	//
	// Layout: 10 bytes prefix + 4 bytes emoji + 4094 bytes suffix = 4108
	// offset = 4108 - 4096 = 12, which is byte index 12 — 2 bytes into the
	// emoji that starts at index 10.
	dir := t.TempDir()
	path := filepath.Join(dir, "stdout.log")

	prefix := strings.Repeat("a", 10)
	suffix := strings.Repeat("b", int(maxBuildStdoutPreviewBytes)-2) // 4094
	body := prefix + "😀" + suffix
	if err := os.WriteFile(path, []byte(body), 0644); err != nil {
		t.Fatalf("WriteFile() error: %v", err)
	}

	preview, _, truncated, err := readBuildStdoutPreview(path, maxBuildStdoutPreviewBytes)
	if err != nil {
		t.Fatalf("readBuildStdoutPreview() error: %v", err)
	}
	if !truncated {
		t.Fatal("expected truncated=true")
	}

	// The preview must be valid UTF-8.
	if !utf8.ValidString(preview) {
		t.Fatalf("preview contains invalid UTF-8: %q", preview[:20])
	}
}
