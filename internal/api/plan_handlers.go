package api

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/ponchione/agent-conductor/internal/database"
	"github.com/ponchione/agent-conductor/internal/pipeline"
	"github.com/ponchione/agent-conductor/internal/planner"
	"github.com/ponchione/agent-conductor/internal/templates"
)

var markdownHeadingRe = regexp.MustCompile(`(?m)^#{1,3}\s+(.+)$`)

func (s *Server) handleSubmitPlan(w http.ResponseWriter, r *http.Request) {
	var req planSubmitRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid JSON body: %v", err))
		return
	}

	if req.SpecContent == "" && req.SpecFilePath == "" {
		writeError(w, http.StatusBadRequest, "spec_content or spec_file_path is required")
		return
	}

	var specContent string
	var specPath string
	var specDisplayName string

	if req.SpecFilePath != "" {
		if _, err := os.Stat(req.SpecFilePath); err != nil {
			if os.IsNotExist(err) {
				writeError(w, http.StatusBadRequest, fmt.Sprintf("spec file %q does not exist", req.SpecFilePath))
				return
			}
			writeError(w, http.StatusInternalServerError, fmt.Sprintf("stat spec file: %v", err))
			return
		}
		data, err := os.ReadFile(req.SpecFilePath)
		if err != nil {
			writeError(w, http.StatusInternalServerError, fmt.Sprintf("read spec file: %v", err))
			return
		}
		specContent = string(data)
		specPath = req.SpecFilePath
		specDisplayName = filepath.Base(req.SpecFilePath)
	} else {
		specContent = req.SpecContent

		tmpFile, err := os.CreateTemp("", "plan-spec-*.md")
		if err != nil {
			writeError(w, http.StatusInternalServerError, fmt.Sprintf("create temp file: %v", err))
			return
		}
		if _, err := tmpFile.WriteString(req.SpecContent); err != nil {
			_ = tmpFile.Close()
			writeError(w, http.StatusInternalServerError, fmt.Sprintf("write temp file: %v", err))
			return
		}
		if err := tmpFile.Close(); err != nil {
			writeError(w, http.StatusInternalServerError, fmt.Sprintf("close temp file: %v", err))
			return
		}
		specPath = tmpFile.Name()

		// Extract a display name from the first markdown heading, or fall back to a generic name.
		if m := markdownHeadingRe.FindStringSubmatch(req.SpecContent); len(m) > 1 {
			specDisplayName = strings.TrimSpace(m[1])
		} else {
			// Use first non-empty line, truncated
			for _, line := range strings.Split(req.SpecContent, "\n") {
				trimmed := strings.TrimSpace(line)
				if trimmed != "" {
					if len(trimmed) > 80 {
						trimmed = trimmed[:80] + "..."
					}
					specDisplayName = trimmed
					break
				}
			}
			if specDisplayName == "" {
				specDisplayName = "Pasted spec"
			}
		}
	}

	// Resolve claude binary before launching goroutine; fail fast if missing.
	claudePath, err := exec.LookPath("claude")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "claude binary not found in PATH")
		return
	}

	// Load prompts before launching goroutine; fail fast if misconfigured.
	if s.cfg == nil {
		writeError(w, http.StatusInternalServerError, "server configuration is not loaded")
		return
	}
	prompts, err := templates.LoadPromptsForPlan(s.cfg)
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("load prompts: %v", err))
		return
	}

	ctx := r.Context()
	project := "default"
	if s.cfg.Project.Name != "" {
		project = s.cfg.Project.Name
	}

	sessionID, err := s.db.StartSession(ctx, database.SessionKindPlanOnly, project, specPath)
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("start session: %v", err))
		return
	}

	planRunID, err := s.db.CreateStubPlanRun(ctx, sessionID, project, specDisplayName)
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("create plan run: %v", err))
		return
	}

	if err := s.db.LinkPlanRunToSession(ctx, planRunID, sessionID); err != nil {
		slog.Warn("failed to link plan run to session", "plan_run_id", planRunID, "session_id", sessionID, "error", err)
	}

	if err := s.db.LogWorkflowEvent(ctx, "", "", pipeline.EventPlanStarted, map[string]any{
		"session_id":  sessionID,
		"plan_run_id": planRunID,
		"spec_path":   specPath,
	}); err != nil {
		slog.Warn("failed to log plan_started event", "session_id", sessionID, "error", err)
	}

	// Derive timeout from config. Default to 60 minutes if unset.
	timeoutMins := s.cfg.Safety.MaxDurationMins
	if timeoutMins <= 0 {
		timeoutMins = 60
	}

	// Determine output directory.
	outputDir := s.workOrderDir
	if outputDir == "" {
		outputDir = "./work-orders/"
	}

	// Build GenerateInput from request + config. This struct contains no
	// references to the HTTP request or response writer.
	input := planner.GenerateInput{
		SpecContent:            specContent,
		SpecFile:               specPath,
		SessionID:              sessionID,
		Config:                 s.cfg,
		DataDir:                s.cfg.Project.DataDir,
		OutputDir:              outputDir,
		SkipAudit:              false,
		AllowUnauditedFallback: false,
		Timeout:                time.Duration(timeoutMins) * time.Minute,
		ClaudePath:             claudePath,
		Prompts:                prompts,
		DB:                     s.db,
	}

	// Launch background goroutine. It captures only s (for s.db), planRunID
	// (immutable string), and input (value struct with no request-scoped data).
	// specPath is passed so temp files can be cleaned up after generation.
	go s.runPlanGeneration(planRunID, sessionID, specPath, []byte(specContent), input)

	writeJSON(w, http.StatusAccepted, planSubmitResponse{
		PlanRunID: planRunID,
		State:     database.PlanRunStatePending,
		SessionID: sessionID,
	})
}

// runPlanGeneration runs plan generation in a background goroutine.
// It must NOT reference http.Request or http.ResponseWriter.
func (s *Server) runPlanGeneration(planRunID, sessionID, specPath string, specData []byte, input planner.GenerateInput) {
	// Clean up temp spec file if one was created for pasted content.
	if strings.HasPrefix(filepath.Base(specPath), "plan-spec-") {
		defer os.Remove(specPath)
	}

	// Panic recovery: log and mark plan run as failed. Never crash the server.
	defer func() {
		if r := recover(); r != nil {
			slog.Error("plan generation panicked",
				"plan_run_id", planRunID,
				"panic", fmt.Sprintf("%v", r),
			)
			ctx := context.Background()
			if err := s.db.UpdatePlanRunFailed(ctx, planRunID, fmt.Sprintf("internal error: %v", r)); err != nil {
				slog.Error("failed to mark panicked plan run as failed",
					"plan_run_id", planRunID, "error", err)
			}
		}
	}()

	// Use context.Background() — NOT the request context — so client
	// disconnect does not cancel generation.
	ctx, cancel := context.WithTimeout(context.Background(), input.Timeout)
	defer cancel()

	// Wire progress callback: closure captures only s.db and planRunID.
	input.OnProgress = func(state, phase string) {
		if err := s.db.UpdatePlanRunState(ctx, planRunID, state, phase); err != nil {
			slog.Warn("failed to update plan run state",
				"plan_run_id", planRunID, "state", state, "phase", phase, "error", err)
		}
	}

	slog.Info("starting plan generation", "plan_run_id", planRunID, "session_id", sessionID)

	result, err := s.generateFunc(ctx, input)

	// Use a fresh context for DB updates after generation completes,
	// because the generation context may have been canceled or timed out.
	dbCtx := context.Background()

	if err != nil {
		// Distinguish timeout from other errors for a user-friendly message.
		errMsg := err.Error()
		if errors.Is(err, context.DeadlineExceeded) {
			timeoutMins := int(input.Timeout.Minutes())
			errMsg = fmt.Sprintf("plan generation timed out after %d minutes", timeoutMins)
		} else if errors.Is(err, context.Canceled) {
			errMsg = "plan generation was canceled"
		}

		slog.Error("plan generation failed",
			"plan_run_id", planRunID, "error", err)

		if dbErr := s.db.UpdatePlanRunFailed(dbCtx, planRunID, errMsg); dbErr != nil {
			slog.Error("failed to mark plan run as failed",
				"plan_run_id", planRunID, "error", dbErr)
		}
		if dbErr := s.db.TransitionSessionState(dbCtx, sessionID, database.SessionStateFailed, errMsg); dbErr != nil {
			slog.Warn("failed to transition session state",
				"session_id", sessionID, "error", dbErr)
		}
		return
	}

	// Success: record metrics, then mark complete.
	if recErr := s.recordPlanRunMetrics(dbCtx, planRunID, specData, result); recErr != nil {
		slog.Warn("failed to record plan run metrics",
			"plan_run_id", planRunID, "error", recErr)
	}

	if err := s.db.UpdatePlanRunState(dbCtx, planRunID, database.PlanRunStateComplete, ""); err != nil {
		slog.Error("failed to mark plan run as complete",
			"plan_run_id", planRunID, "error", err)
	}
	if err := s.db.TransitionSessionState(dbCtx, sessionID, database.SessionStateCompleted, ""); err != nil {
		slog.Warn("failed to transition session state",
			"session_id", sessionID, "error", err)
	}

	slog.Info("plan generation complete",
		"plan_run_id", planRunID,
		"epic_count", result.Metrics.EpicCount,
		"task_count", result.Metrics.TaskCount,
		"manifest_path", result.ManifestPath,
	)
}

// recordPlanRunMetrics updates the existing plan_run row with metrics from
// a successful generation. This mirrors the field mapping in cmd/conductor/plan.go's
// recordPlanRun, but uses UPDATE instead of INSERT since the stub row already exists.
func (s *Server) recordPlanRunMetrics(ctx context.Context, planRunID string, specData []byte, result *planner.GenerateResult) error {
	specFingerprint := sha256.Sum256(specData)

	m := database.PlanRunMetricsUpdate{
		SpecFingerprint:         database.String(hex.EncodeToString(specFingerprint[:])),
		WorkOrdersGenerated:     database.Int64(result.Metrics.WorkOrdersGenerated),
		EpicCount:               database.Int64(result.Metrics.EpicCount),
		TaskCount:               database.Int64(result.Metrics.TaskCount),
		PreAuditWorkOrderCount:  database.Int64(result.Metrics.PreAuditWorkOrderCount),
		PostAuditWorkOrderCount: database.Int64(result.Metrics.PostAuditWorkOrderCount),
		GenerationRetryCount:    int64(result.Metrics.GenerationRetryCount),
	}

	if result.GenerationTrace != nil {
		if genResult := result.GenerationTrace.AggregateResult; genResult != nil {
			m.GenerationModel = database.String(genResult.Model)
			m.GenerationTokensIn = database.Int64(genResult.TokensIn)
			m.GenerationTokensOut = database.Int64(genResult.TokensOut)
			m.GenerationSessionID = database.String(genResult.SessionID)
			m.GenerationCostUsd = database.Float64(genResult.CostUSD)
			m.GenerationDurationMs = database.Int64Value(genResult.Duration.Milliseconds())
		}
		if epicResult := result.GenerationTrace.EpicResult; epicResult != nil {
			m.EpicGenerationModel = database.String(epicResult.Model)
			m.EpicGenerationTokensIn = database.Int64(epicResult.TokensIn)
			m.EpicGenerationTokensOut = database.Int64(epicResult.TokensOut)
			m.EpicGenerationCostUsd = database.Float64(epicResult.CostUSD)
			m.EpicGenerationDurationMs = database.Int64Value(epicResult.Duration.Milliseconds())
		}
		taskMetrics := planner.TaskGenerationMetrics(result.GenerationTrace)
		if taskMetrics.CallCount > 0 {
			m.TaskGenerationModel = database.String(taskMetrics.Model)
			m.TaskGenerationCallCount = database.Int64(taskMetrics.CallCount)
			m.TaskGenerationTokensIn = database.Int64(taskMetrics.TokensIn)
			m.TaskGenerationTokensOut = database.Int64(taskMetrics.TokensOut)
			m.TaskGenerationCostUsd = database.Float64(taskMetrics.CostUSD)
			m.TaskGenerationDurationMs = database.Int64Value(taskMetrics.Duration.Milliseconds())
		}
	}

	if result.AuditResult != nil {
		m.AuditModel = database.String(result.AuditResult.Model)
		m.AuditTokensIn = database.Int64(result.AuditResult.TokensIn)
		m.AuditTokensOut = database.Int64(result.AuditResult.TokensOut)
		m.AuditSessionID = database.String(result.AuditResult.SessionID)
		m.AuditCostUsd = database.Float64(result.AuditResult.CostUSD)
		m.AuditDurationMs = database.Int64Value(result.AuditResult.Duration.Milliseconds())
	}

	if result.AuditSummary != nil {
		m.AuditWorkOrdersAdded = database.Int64(result.AuditSummary.Added)
		m.AuditWorkOrdersModified = database.Int64(result.AuditSummary.Modified)
		m.AuditWorkOrdersUnchanged = database.Int64(result.AuditSummary.Unchanged)
		if len(result.AuditSummary.Changes) > 0 {
			payload, err := json.Marshal(result.AuditSummary.Changes)
			if err != nil {
				return fmt.Errorf("marshal audit changes: %w", err)
			}
			m.AuditChangeText = database.String(string(payload))
		}
	}

	return s.db.UpdatePlanRunMetrics(ctx, planRunID, m)
}

func (s *Server) handleGetPlanRun(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	run, err := s.db.GetPlanRun(r.Context(), id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "plan run not found")
			return
		}
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("get plan run: %v", err))
		return
	}

	writeJSON(w, http.StatusOK, mapPlanRunDetail(run))
}

// planRunDetailResponse is the JSON shape returned by GET /api/plan-runs/{id}.
type planRunDetailResponse struct {
	ID                       string   `json:"id"`
	SessionID                *string  `json:"session_id"`
	SpecFile                 string   `json:"spec_file"`
	Project                  *string  `json:"project"`
	SpecFingerprint          *string  `json:"spec_fingerprint"`
	State                    string   `json:"state"`
	ErrorMessage             *string  `json:"error_message"`
	CurrentPhase             *string  `json:"current_phase"`
	GenerationModel          *string  `json:"generation_model"`
	EpicGenerationModel      *string  `json:"epic_generation_model"`
	TaskGenerationModel      *string  `json:"task_generation_model"`
	AuditModel               *string  `json:"audit_model"`
	GenerationSessionID      *string  `json:"generation_session_id"`
	AuditSessionID           *string  `json:"audit_session_id"`
	WorkOrdersGenerated      *int64   `json:"work_orders_generated"`
	EpicCount                *int64   `json:"epic_count"`
	TaskCount                *int64   `json:"task_count"`
	PreAuditWorkOrderCount   *int64   `json:"pre_audit_work_order_count"`
	PostAuditWorkOrderCount  *int64   `json:"post_audit_work_order_count"`
	AuditChangeText          *string  `json:"audit_change_text"`
	AuditWorkOrdersAdded     *int64   `json:"audit_work_orders_added"`
	AuditWorkOrdersModified  *int64   `json:"audit_work_orders_modified"`
	AuditWorkOrdersUnchanged *int64   `json:"audit_work_orders_unchanged"`
	GenerationCostUSD        *float64 `json:"generation_cost_usd"`
	EpicGenerationCostUSD    *float64 `json:"epic_generation_cost_usd"`
	TaskGenerationCostUSD    *float64 `json:"task_generation_cost_usd"`
	AuditCostUSD             *float64 `json:"audit_cost_usd"`
	GenerationDurationMs     *int64   `json:"generation_duration_ms"`
	EpicGenerationDurationMs *int64   `json:"epic_generation_duration_ms"`
	TaskGenerationDurationMs *int64   `json:"task_generation_duration_ms"`
	AuditDurationMs          *int64   `json:"audit_duration_ms"`
	GenerationRetryCount     int64    `json:"generation_retry_count"`
	GenerationTokensIn       *int64   `json:"generation_tokens_in"`
	GenerationTokensOut      *int64   `json:"generation_tokens_out"`
	EpicGenerationTokensIn   *int64   `json:"epic_generation_tokens_in"`
	EpicGenerationTokensOut  *int64   `json:"epic_generation_tokens_out"`
	TaskGenerationCallCount  *int64   `json:"task_generation_call_count"`
	TaskGenerationTokensIn   *int64   `json:"task_generation_tokens_in"`
	TaskGenerationTokensOut  *int64   `json:"task_generation_tokens_out"`
	AuditTokensIn            *int64   `json:"audit_tokens_in"`
	AuditTokensOut           *int64   `json:"audit_tokens_out"`
	CreatedAt                string   `json:"created_at"`
}

func mapPlanRunDetail(row database.GetPlanRunByIDRow) planRunDetailResponse {
	return planRunDetailResponse{
		ID:                       row.ID,
		SessionID:                stringPtr(row.SessionID),
		SpecFile:                 row.SpecFile,
		Project:                  stringPtr(row.Project),
		SpecFingerprint:          stringPtr(row.SpecFingerprint),
		State:                    row.State,
		ErrorMessage:             stringPtr(row.ErrorMessage),
		CurrentPhase:             stringPtr(row.CurrentPhase),
		GenerationModel:          stringPtr(row.GenerationModel),
		EpicGenerationModel:      stringPtr(row.EpicGenerationModel),
		TaskGenerationModel:      stringPtr(row.TaskGenerationModel),
		AuditModel:               stringPtr(row.AuditModel),
		GenerationSessionID:      stringPtr(row.GenerationSessionID),
		AuditSessionID:           stringPtr(row.AuditSessionID),
		WorkOrdersGenerated:      int64Ptr(row.WorkOrdersGenerated),
		EpicCount:                int64Ptr(row.EpicCount),
		TaskCount:                int64Ptr(row.TaskCount),
		PreAuditWorkOrderCount:   int64Ptr(row.PreAuditWorkOrderCount),
		PostAuditWorkOrderCount:  int64Ptr(row.PostAuditWorkOrderCount),
		AuditChangeText:          stringPtr(row.AuditChangeText),
		AuditWorkOrdersAdded:     int64Ptr(row.AuditWorkOrdersAdded),
		AuditWorkOrdersModified:  int64Ptr(row.AuditWorkOrdersModified),
		AuditWorkOrdersUnchanged: int64Ptr(row.AuditWorkOrdersUnchanged),
		GenerationCostUSD:        float64Ptr(row.GenerationCostUsd),
		EpicGenerationCostUSD:    float64Ptr(row.EpicGenerationCostUsd),
		TaskGenerationCostUSD:    float64Ptr(row.TaskGenerationCostUsd),
		AuditCostUSD:             float64Ptr(row.AuditCostUsd),
		GenerationDurationMs:     int64Ptr(row.GenerationDurationMs),
		EpicGenerationDurationMs: int64Ptr(row.EpicGenerationDurationMs),
		TaskGenerationDurationMs: int64Ptr(row.TaskGenerationDurationMs),
		AuditDurationMs:          int64Ptr(row.AuditDurationMs),
		GenerationRetryCount:     row.GenerationRetryCount,
		GenerationTokensIn:       int64Ptr(row.GenerationTokensIn),
		GenerationTokensOut:      int64Ptr(row.GenerationTokensOut),
		EpicGenerationTokensIn:   int64Ptr(row.EpicGenerationTokensIn),
		EpicGenerationTokensOut:  int64Ptr(row.EpicGenerationTokensOut),
		TaskGenerationCallCount:  int64Ptr(row.TaskGenerationCallCount),
		TaskGenerationTokensIn:   int64Ptr(row.TaskGenerationTokensIn),
		TaskGenerationTokensOut:  int64Ptr(row.TaskGenerationTokensOut),
		AuditTokensIn:            int64Ptr(row.AuditTokensIn),
		AuditTokensOut:           int64Ptr(row.AuditTokensOut),
		CreatedAt:                row.CreatedAt,
	}
}
