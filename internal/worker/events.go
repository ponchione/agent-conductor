package worker

import (
	"context"
	"io"
	"log/slog"
	"os"
	"strings"
	"unicode/utf8"

	"github.com/ponchione/agent-conductor/internal/database"
	pipelineerrors "github.com/ponchione/agent-conductor/internal/errors"
	"github.com/ponchione/agent-conductor/internal/executor"
	"github.com/ponchione/agent-conductor/internal/models"
	"github.com/ponchione/agent-conductor/internal/pipeline"
	"github.com/ponchione/agent-conductor/internal/verify"
)

const maxBuildStdoutPreviewBytes = 4096

func (w *Worker) emitEvent(ctx context.Context, workflowID, taskID, eventType string, payload map[string]any) {
	if w == nil || w.db == nil {
		return
	}
	if err := w.db.LogWorkflowEvent(ctx, workflowID, taskID, eventType, payload); err != nil {
		slog.Warn("failed to log workflow event",
			"workflow_id", workflowID,
			"task_id", taskID,
			"event_type", eventType,
			"error", err,
		)
	}
}

func (w *Worker) emitPhaseStart(ctx context.Context, task *database.Task, payload map[string]any) {
	fields := cloneEventPayload(payload)
	fields["phase"] = task.Phase
	w.emitEvent(ctx, task.WorkflowID, task.ID, pipeline.EventPhaseStart, fields)
}

func (w *Worker) emitPhaseComplete(ctx context.Context, task *database.Task, payload map[string]any) {
	fields := cloneEventPayload(payload)
	fields["phase"] = task.Phase
	w.emitEvent(ctx, task.WorkflowID, task.ID, pipeline.EventPhaseComplete, fields)
}

func (w *Worker) emitPhaseError(ctx context.Context, task *database.Task, pe *pipelineerrors.PipelineError, willRetry bool) {
	fields := map[string]any{
		"phase":       task.Phase,
		"error":       pe.Error(),
		"error_class": pe.Class.String(),
		"will_retry":  willRetry,
	}
	w.emitEvent(ctx, task.WorkflowID, task.ID, pipeline.EventPhaseError, fields)
}

func (w *Worker) emitScopeStep(ctx context.Context, task *database.Task, step string, payload map[string]any) {
	fields := cloneEventPayload(payload)
	fields["phase"] = task.Phase
	fields["step"] = step
	w.emitEvent(ctx, task.WorkflowID, task.ID, pipeline.EventScopeStep, fields)
}

func (w *Worker) emitVerifyPrecheck(ctx context.Context, task *database.Task, result verify.PreCheckResult) {
	required := false
	if result.Required != nil {
		required = *result.Required
	}
	fields := map[string]any{
		"phase":             task.Phase,
		"criterion_id":      result.CriterionID,
		"criterion":         result.Criterion,
		"verification_kind": result.VerificationKind,
		"result":            result.NormalizedResult(),
		"required":          required,
		"notes":             result.Notes,
	}
	w.emitEvent(ctx, task.WorkflowID, task.ID, pipeline.EventVerifyPrecheck, fields)
}

func (w *Worker) emitVerifyResult(ctx context.Context, task *database.Task, report *models.VerificationReport) {
	if report == nil {
		return
	}
	fields := map[string]any{
		"phase":            task.Phase,
		"status":           report.Status,
		"summary":          report.Summary,
		"scope_drift":      report.ScopeDrift.Detected,
		"all_criteria_met": report.Completeness.AllCriteriaMet,
	}
	w.emitEvent(ctx, task.WorkflowID, task.ID, pipeline.EventVerifyResult, fields)
}

func (w *Worker) emitRunAwaitingReview(ctx context.Context, workflowID, taskID, phase string, payload map[string]any) {
	fields := cloneEventPayload(payload)
	fields["current_state"] = "human_review"
	if phase != "" {
		fields["phase"] = phase
	}
	w.emitEvent(ctx, workflowID, taskID, pipeline.EventRunAwaitingReview, fields)
}

func (w *Worker) emitRunComplete(ctx context.Context, workflowID, taskID, finalState, humanResult string, payload map[string]any) {
	fields := cloneEventPayload(payload)
	fields["final_state"] = finalState
	if humanResult != "" {
		fields["human_result"] = humanResult
	}
	w.emitEvent(ctx, workflowID, taskID, pipeline.EventRunComplete, fields)
}

func (w *Worker) emitBuildStdout(ctx context.Context, task *database.Task, result *executor.RunResult) {
	if result == nil {
		return
	}

	fields := map[string]any{
		"phase":       task.Phase,
		"stdout_path": result.StdoutPath,
		"exit_code":   result.ExitCode,
		"duration":    result.Duration.String(),
		"success":     result.Success,
	}
	if result.SessionID != "" {
		fields["build_session_id"] = result.SessionID
	}
	if len(result.ToolCalls) > 0 {
		fields["tool_calls"] = result.ToolCalls
	}

	preview, sizeBytes, truncated, err := readBuildStdoutPreview(result.StdoutPath, maxBuildStdoutPreviewBytes)
	if err != nil {
		slog.Warn("failed to read build stdout preview", "path", result.StdoutPath, "error", err)
	} else {
		fields["preview"] = preview
		fields["size_bytes"] = sizeBytes
		fields["truncated"] = truncated
	}

	w.emitEvent(ctx, task.WorkflowID, task.ID, pipeline.EventBuildStdout, fields)
}

func readBuildStdoutPreview(path string, maxBytes int64) (string, int64, bool, error) {
	if path == "" {
		return "", 0, false, nil
	}

	file, err := os.Open(path)
	if err != nil {
		return "", 0, false, err
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		return "", 0, false, err
	}

	sizeBytes := info.Size()
	if sizeBytes == 0 {
		return "", 0, false, nil
	}

	offset := int64(0)
	truncated := false
	if sizeBytes > maxBytes {
		offset = sizeBytes - maxBytes
		truncated = true
	}

	if _, err := file.Seek(offset, io.SeekStart); err != nil {
		return "", 0, false, err
	}
	data, err := io.ReadAll(file)
	if err != nil {
		return "", 0, false, err
	}

	result := string(data)
	if truncated && !utf8.ValidString(result) {
		result = strings.ToValidUTF8(result, "")
	}
	return result, sizeBytes, truncated, nil
}

func cloneEventPayload(payload map[string]any) map[string]any {
	if len(payload) == 0 {
		return map[string]any{}
	}

	cloned := make(map[string]any, len(payload))
	for key, value := range payload {
		cloned[key] = value
	}
	return cloned
}
