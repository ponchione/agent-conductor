package planner

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode"

	"github.com/ponchione/agent-conductor/internal/config"
	"github.com/ponchione/agent-conductor/internal/database"
	"github.com/ponchione/agent-conductor/internal/llm"
	"github.com/ponchione/agent-conductor/internal/streaming"
	"github.com/ponchione/agent-conductor/internal/templates"
	"gopkg.in/yaml.v3"
)

// ProgressCallback is called by the planner at phase transitions.
// state is one of: generating_epics, generating_tasks, auditing, persisting.
// phase is a human-readable description (e.g. "Generating tasks for epic 2 of 5").
type ProgressCallback func(state string, phase string)

// GenerateInput contains all configuration needed by Generate.
type GenerateInput struct {
	SpecContent            string
	SpecFile               string
	SessionID              string
	Config                 *config.ProjectConfig
	DataDir                string
	OutputDir              string
	SkipAudit              bool
	AllowUnauditedFallback bool
	Timeout                time.Duration
	ClaudePath             string
	Prompts                *templates.LoadedPrompts
	DB                     *database.DB
	OnProgress             ProgressCallback
}

// GenerateResult contains the output from a successful Generate call.
type GenerateResult struct {
	PlanDoc          *PlanDocument
	ManifestPath     string
	Metrics          PlanRunMetrics
	AuditSummary     *AuditSummary
	GenerationTrace  *PlanGenerationTrace
	AuditResult      *InvokeClaudeResult
	PreAuditTaskCount int
}

// PlanRunMetrics contains all timing, token, cost data for plan_runs.
// The caller (CLI or HTTP handler) maps these fields to database.InsertPlanRunParams.
type PlanRunMetrics struct {
	EpicCount                int
	TaskCount                int
	WorkOrdersGenerated      int
	PreAuditWorkOrderCount   int
	PostAuditWorkOrderCount  int
	GenerationRetryCount     int
}

// PlanningPhaseInvoker is the function signature for invoking a Claude planning phase.
type PlanningPhaseInvoker func(phase, systemPrompt, userMsg string) (*InvokeClaudeResult, error)

// PlanTaskGenerationTrace captures the raw output and parsed response for a single epic's task generation.
type PlanTaskGenerationTrace struct {
	EpicID   string
	EpicRef  string
	Raw      string
	Response *RawTaskPlanResponse
	Result   *InvokeClaudeResult
}

// PlanGenerationTrace captures the full trace of a plan generation run.
type PlanGenerationTrace struct {
	EpicRaw         string
	EpicResponse    *RawEpicPlanResponse
	EpicResult      *InvokeClaudeResult
	TaskTraces      []PlanTaskGenerationTrace
	AggregateRaw    string
	AggregateResult *InvokeClaudeResult
}

// PlanGenerationPhaseError wraps a phase-specific error with the raw output for debugging.
type PlanGenerationPhaseError struct {
	Phase string
	Raw   string
	Err   error
}

func (e *PlanGenerationPhaseError) Error() string {
	if e == nil || e.Err == nil {
		return ""
	}
	return e.Err.Error()
}

func (e *PlanGenerationPhaseError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// AggregatedPlanningMetrics aggregates metrics across multiple Claude invocations.
type AggregatedPlanningMetrics struct {
	Model     string
	TokensIn  int
	TokensOut int
	CostUSD   float64
	Duration  time.Duration
	CallCount int
}

// Generate runs the full plan generation pipeline.
// It is safe to call from a goroutine.
// It does NOT call recordPlanRun; the caller writes metrics to the database.
// It does NOT manage sessions; the caller handles session lifecycle.
func Generate(ctx context.Context, input GenerateInput) (*GenerateResult, error) {
	onProgress := input.OnProgress
	if onProgress == nil {
		onProgress = func(state, phase string) {}
	}

	onProgress("generating_epics", "Starting epic decomposition")

	invoker := func(phase, systemPrompt, userMsg string) (*InvokeClaudeResult, error) {
		slog.Info("invoking Claude", "phase", phase, "timeout", input.Timeout)
		result, invokeErr := InvokeClaudeWithStats(input.ClaudePath, systemPrompt, userMsg, input.Timeout, input.Config.Project.Path, newPlanStreamObserver(phase))
		if invokeErr != nil {
			return nil, invokeErr
		}
		logPlanInvocationComplete(phase, result)
		return result, nil
	}

	planDoc, generationTrace, genRetryCount, err := generatePlanDocument(input.SpecFile, []byte(input.SpecContent), input.SessionID, input.Config, input.Prompts, invoker, onProgress)
	if err != nil {
		var phaseErr *PlanGenerationPhaseError
		if isPhaseErr(err, &phaseErr) && phaseErr.Raw != "" {
			if mkErr := os.MkdirAll(input.OutputDir, 0755); mkErr != nil {
				return nil, fmt.Errorf("parse failed and could not create output dir: parse=%w, mkdir=%v", err, mkErr)
			}
			filename := "raw-plan-output.txt"
			if phaseErr.Phase != "" {
				filename = fmt.Sprintf("raw-plan-output-%s.txt", slugify(phaseErr.Phase))
			}
			rawPath := filepath.Join(input.OutputDir, filename)
			if wErr := os.WriteFile(rawPath, []byte(phaseErr.Raw), 0644); wErr != nil {
				return nil, fmt.Errorf("parse failed and could not write raw output: parse=%w, write=%v", err, wErr)
			}
			registerPlanArtifact(ctx, input.DB, input.SessionID, database.ArtifactTypePlanParseFailure, rawPath, map[string]any{
				"phase": phaseErr.Phase,
			})
			return nil, fmt.Errorf("%w\nRaw output saved to: %s", err, rawPath)
		}
		return nil, err
	}

	preAuditWorkOrderCount := planDoc.TaskCount()

	var summary *AuditSummary
	var auditResult *InvokeClaudeResult
	if input.SkipAudit {
		slog.Info("skipping audit pass (--skip-audit)")
	} else {
		onProgress("auditing", "Running audit pass")
		slog.Info("invoking Claude", "phase", "audit", "count", planDoc.TaskCount(), "timeout", input.Timeout)
		auditedPlan, auditSummary, auditRes, auditErr := auditWorkOrders(input.ClaudePath, input.Prompts.PlanAudit, input.SpecContent, planDoc, input.Config, input.Timeout)
		resolvedPlan, resolvedSummary, resolvedAuditResult, resolveErr := resolveAuditOutcome(planDoc, auditedPlan, auditSummary, auditRes, auditErr, input.AllowUnauditedFallback)
		if resolveErr != nil {
			return nil, resolveErr
		}
		planDoc = resolvedPlan
		summary = resolvedSummary
		auditResult = resolvedAuditResult
		if auditResult != nil {
			logPlanInvocationComplete("audit", auditResult)
		}
	}

	onProgress("persisting", "Writing plan manifest and artifacts")

	taskCount := planDoc.TaskCount()
	planManifestPath, err := WritePlanManifest(planDoc, input.OutputDir)
	if err != nil {
		return nil, fmt.Errorf("failed to write plan manifest: %w", err)
	}

	if persistErr := persistPlanningArtifacts(ctx, input.DB, input.SessionID, input.DataDir, generationTrace, auditResult, summary, planDoc, planManifestPath); persistErr != nil {
		slog.Warn("failed to persist planning artifacts", "session_id", input.SessionID, "error", persistErr)
	}

	return &GenerateResult{
		PlanDoc:           planDoc,
		ManifestPath:      planManifestPath,
		AuditSummary:      summary,
		GenerationTrace:   generationTrace,
		AuditResult:       auditResult,
		PreAuditTaskCount: preAuditWorkOrderCount,
		Metrics: PlanRunMetrics{
			EpicCount:               len(planDoc.Epics),
			TaskCount:               taskCount,
			WorkOrdersGenerated:     taskCount,
			PreAuditWorkOrderCount:  preAuditWorkOrderCount,
			PostAuditWorkOrderCount: taskCount,
			GenerationRetryCount:    genRetryCount,
		},
	}, nil
}

func isPhaseErr(err error, target **PlanGenerationPhaseError) bool {
	for err != nil {
		if phaseErr, ok := err.(*PlanGenerationPhaseError); ok {
			*target = phaseErr
			return true
		}
		unwrapper, ok := err.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		err = unwrapper.Unwrap()
	}
	return false
}

func generatePlanDocument(specPath string, specData []byte, sessionID string, cfg *config.ProjectConfig, prompts *templates.LoadedPrompts, invoker PlanningPhaseInvoker, onProgress ProgressCallback) (*PlanDocument, *PlanGenerationTrace, int, error) {
	builder := NewPlanContextBuilder(cfg)

	epicUserMsg := builder.BuildEpicDecomposition(string(specData))
	epicResp, epicResult, epicRaw, retryCount, err := invokeEpicPlanningPhase(invoker, prompts.PlanEpic, epicUserMsg)
	if err != nil {
		return nil, &PlanGenerationTrace{EpicRaw: epicRaw}, retryCount, err
	}

	aggregatedResult := epicResult
	var rawGeneration strings.Builder
	appendGenerationOutput(&rawGeneration, "EPIC GENERATION", epicRaw)
	trace := &PlanGenerationTrace{
		EpicRaw:      epicRaw,
		EpicResponse: epicResp,
		EpicResult:   epicResult,
	}

	priorEpics := make([]PlanEpic, 0, len(epicResp.Epics))
	taskResponses := make([]*RawTaskPlanResponse, 0, len(epicResp.Epics))
	nextTaskID := 1
	for epicIndex, rawEpic := range epicResp.Epics {
		targetEpic := PlanEpic{
			ID:             CanonicalEpicID(epicIndex + 1),
			EpicRef:        rawEpic.EpicRef,
			Title:          rawEpic.Title,
			Description:    rawEpic.Description,
			Covers:         append([]string(nil), rawEpic.Covers...),
			DependsOnEpics: append([]string(nil), rawEpic.DependsOnEpics...),
		}

		onProgress("generating_tasks", fmt.Sprintf("Generating tasks for epic %d of %d (%s)", epicIndex+1, len(epicResp.Epics), targetEpic.Title))

		taskUserMsg := builder.BuildTaskDecomposition(string(specData), targetEpic, priorEpics)
		taskResp, taskResult, taskRaw, taskRetries, taskErr := invokeTaskPlanningPhase(invoker, targetEpic.ID, prompts.PlanTask, taskUserMsg)
		retryCount += taskRetries
		if taskErr != nil {
			trace.AggregateRaw = rawGeneration.String()
			trace.AggregateResult = aggregatedResult
			return nil, trace, retryCount, taskErr
		}
		appendGenerationOutput(&rawGeneration, fmt.Sprintf("TASK GENERATION %s", targetEpic.ID), taskRaw)
		MergeInvokeClaudeResults(aggregatedResult, taskResult)
		taskResponses = append(taskResponses, taskResp)
		trace.TaskTraces = append(trace.TaskTraces, PlanTaskGenerationTrace{
			EpicID:   targetEpic.ID,
			EpicRef:  targetEpic.EpicRef,
			Raw:      taskRaw,
			Response: taskResp,
			Result:   taskResult,
		})

		priorTasks := make([]PlanTask, len(taskResp.Tasks))
		for i, rawTask := range taskResp.Tasks {
			taskID := CanonicalTaskID(nextTaskID)
			nextTaskID++

			task, convErr := rawTask.ToPlanTask(taskID, targetEpic.ID)
			if convErr != nil {
				trace.AggregateRaw = rawGeneration.String()
				trace.AggregateResult = aggregatedResult
				return nil, trace, retryCount, fmt.Errorf("epic %q task %q: %w", targetEpic.EpicRef, rawTask.TaskRef, convErr)
			}
			priorTasks[i] = task
		}
		targetEpic.Tasks = priorTasks
		priorEpics = append(priorEpics, targetEpic)
	}

	specFingerprint := sha256.Sum256(specData)
	planDoc, err := AssemblePlanDocument(PlanManifestMetadata{
		SpecFile:        specPath,
		SpecFingerprint: hex.EncodeToString(specFingerprint[:]),
		CreatedAt:       time.Now().UTC().Format(time.RFC3339),
		SessionID:       sessionID,
		GenerationModel: aggregatedResult.Model,
	}, epicResp, taskResponses)
	if err != nil {
		trace.AggregateRaw = rawGeneration.String()
		trace.AggregateResult = aggregatedResult
		return nil, trace, retryCount, fmt.Errorf("assemble plan manifest: %w", err)
	}
	if err := ValidatePlanDocument(planDoc, cfg); err != nil {
		trace.AggregateRaw = rawGeneration.String()
		trace.AggregateResult = aggregatedResult
		return nil, trace, retryCount, fmt.Errorf("planner output validation failed: %w", err)
	}

	trace.AggregateRaw = rawGeneration.String()
	trace.AggregateResult = aggregatedResult
	return planDoc, trace, retryCount, nil
}

func invokeEpicPlanningPhase(invoker PlanningPhaseInvoker, prompt, userMsg string) (*RawEpicPlanResponse, *InvokeClaudeResult, string, int, error) {
	return invokePlanningPhaseWithRetry("planning_epic", "planning_epic_retry", prompt, userMsg, invoker, ParseEpicPlanResponse)
}

func invokeTaskPlanningPhase(invoker PlanningPhaseInvoker, epicID, prompt, userMsg string) (*RawTaskPlanResponse, *InvokeClaudeResult, string, int, error) {
	phase := "planning_task_" + epicID
	retryPhase := phase + "_retry"
	return invokePlanningPhaseWithRetry(phase, retryPhase, prompt, userMsg, invoker, ParseTaskPlanResponse)
}

func invokePlanningPhaseWithRetry[T any](phase, retryPhase, prompt, userMsg string, invoker PlanningPhaseInvoker, parse func(string) (*T, error)) (*T, *InvokeClaudeResult, string, int, error) {
	result, err := invoker(phase, prompt, userMsg)
	if err != nil {
		return nil, nil, "", 0, fmt.Errorf("claude invocation failed: %w", err)
	}

	raw := result.Content
	parsed, parseErr := parse(raw)
	if parseErr == nil {
		return parsed, result, raw, 0, nil
	}

	slog.Warn("first parse attempt failed, retrying with correction", "phase", phase, "error", parseErr)
	correctionMsg := fmt.Sprintf(
		"%s\n\n=== CORRECTION ===\nYour previous response could not be parsed: %s\n\nPrevious response:\n%s\n\nPlease respond with ONLY a valid JSON object matching the schema. No markdown, no commentary.",
		userMsg, parseErr.Error(), raw,
	)

	retryResult, retryErr := invoker(retryPhase, prompt, correctionMsg)
	if retryErr != nil {
		return nil, nil, raw, 1, fmt.Errorf("claude retry invocation failed: %w", retryErr)
	}
	MergeInvokeClaudeResults(result, retryResult)
	raw = retryResult.Content

	parsed, parseErr = parse(raw)
	if parseErr != nil {
		return nil, nil, raw, 1, &PlanGenerationPhaseError{
			Phase: retryPhase,
			Raw:   raw,
			Err:   fmt.Errorf("could not parse plan response after retry: %w", parseErr),
		}
	}
	return parsed, result, raw, 1, nil
}

func appendGenerationOutput(sb *strings.Builder, title, content string) {
	if sb.Len() > 0 {
		sb.WriteString("\n")
	}
	fmt.Fprintf(sb, "=== %s ===\n", title)
	sb.WriteString(content)
	if !strings.HasSuffix(content, "\n") {
		sb.WriteString("\n")
	}
}

func aggregatePlanningMetrics(results []*InvokeClaudeResult) AggregatedPlanningMetrics {
	var metrics AggregatedPlanningMetrics
	for _, result := range results {
		if result == nil {
			continue
		}
		metrics.CallCount++
		metrics.TokensIn += result.TokensIn
		metrics.TokensOut += result.TokensOut
		metrics.CostUSD += result.CostUSD
		metrics.Duration += result.Duration

		model := strings.TrimSpace(result.Model)
		switch {
		case model == "":
		case metrics.Model == "":
			metrics.Model = model
		case metrics.Model != model:
			metrics.Model = "multiple"
		}
	}
	return metrics
}

// TaskGenerationMetrics computes aggregated metrics for the task generation phase.
func TaskGenerationMetrics(trace *PlanGenerationTrace) AggregatedPlanningMetrics {
	if trace == nil || len(trace.TaskTraces) == 0 {
		return AggregatedPlanningMetrics{}
	}

	results := make([]*InvokeClaudeResult, 0, len(trace.TaskTraces))
	for _, taskTrace := range trace.TaskTraces {
		results = append(results, taskTrace.Result)
	}
	return aggregatePlanningMetrics(results)
}

// ResolveAuditOutcome determines the final plan and summary given audit results.
func ResolveAuditOutcome(basePlan, auditedPlan *PlanDocument, summary *AuditSummary, auditResult *InvokeClaudeResult, auditErr error, allowFallback bool) (*PlanDocument, *AuditSummary, *InvokeClaudeResult, error) {
	return resolveAuditOutcome(basePlan, auditedPlan, summary, auditResult, auditErr, allowFallback)
}

func resolveAuditOutcome(basePlan, auditedPlan *PlanDocument, summary *AuditSummary, auditResult *InvokeClaudeResult, auditErr error, allowFallback bool) (*PlanDocument, *AuditSummary, *InvokeClaudeResult, error) {
	if auditErr == nil {
		return auditedPlan, summary, auditResult, nil
	}
	if !allowFallback {
		return nil, nil, nil, fmt.Errorf("audit pass failed and unaudited fallback is disabled: %w", auditErr)
	}
	slog.Warn("audit pass failed, using unaudited plan manifest due to override", "error", auditErr)
	return basePlan, nil, auditResult, nil
}

// MergeInvokeClaudeResults merges src into dst, accumulating tokens/cost/duration.
func MergeInvokeClaudeResults(dst, src *InvokeClaudeResult) {
	if dst == nil || src == nil {
		return
	}

	dst.Content = src.Content
	dst.TokensIn += src.TokensIn
	dst.TokensOut += src.TokensOut
	dst.CostUSD += src.CostUSD
	dst.Duration += src.Duration
	if src.Model != "" {
		dst.Model = src.Model
	}
	if src.SessionID != "" {
		dst.SessionID = src.SessionID
	}
	if dst.ToolCalls == nil {
		dst.ToolCalls = make(map[string]int)
	}
	for tool, count := range src.ToolCalls {
		dst.ToolCalls[tool] += count
	}
}

func auditWorkOrders(claudePath string, auditPrompt string, spec string, planDoc *PlanDocument, cfg *config.ProjectConfig, timeout time.Duration) (*PlanDocument, *AuditSummary, *InvokeClaudeResult, error) {
	userMsg, err := buildAuditUserMessage(spec, planDoc, cfg)
	if err != nil {
		return planDoc, nil, nil, err
	}

	result, err := InvokeClaudeWithStats(claudePath, auditPrompt, userMsg, timeout, cfg.Project.Path, newPlanStreamObserver("audit"))
	if err != nil {
		return planDoc, nil, nil, fmt.Errorf("audit invocation failed: %w", err)
	}

	cleaned := llm.CleanLLMResponse(result.Content)
	var resp AuditResponse
	if err := json.Unmarshal([]byte(cleaned), &resp); err != nil {
		return planDoc, nil, result, fmt.Errorf("audit response parse failed: %w", err)
	}

	if len(resp.Epics) == 0 {
		return planDoc, nil, result, fmt.Errorf("audit response contained no epics")
	}

	auditedPlan := resp.ToPlanDocument()
	if err := ValidatePlanDocument(auditedPlan, cfg); err != nil {
		return planDoc, nil, result, fmt.Errorf("audited plan failed validation: %w", err)
	}

	return auditedPlan, &resp.AuditSummary, result, nil
}

func buildAuditUserMessage(spec string, planDoc *PlanDocument, cfg *config.ProjectConfig) (string, error) {
	planJSON, err := planDoc.MarshalIndented()
	if err != nil {
		return "", fmt.Errorf("failed to marshal plan manifest: %w", err)
	}
	builder := NewPlanContextBuilder(cfg)
	return builder.BuildAuditContext(spec, planJSON), nil
}

// WritePlanManifest writes the final plan document as plan.yaml.
func WritePlanManifest(doc *PlanDocument, outputDir string) (string, error) {
	if doc == nil {
		return "", fmt.Errorf("plan document is required")
	}
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create output directory %q: %w", outputDir, err)
	}

	data, err := yaml.Marshal(doc)
	if err != nil {
		return "", fmt.Errorf("marshal plan manifest: %w", err)
	}

	path := filepath.Join(outputDir, "plan.yaml")
	if err := os.WriteFile(path, data, 0644); err != nil {
		return "", fmt.Errorf("failed to write %s: %w", path, err)
	}
	slog.Debug("wrote plan manifest", "path", path)
	return path, nil
}

// WritePlanSessionArtifact writes an artifact file to the plan session directory.
func WritePlanSessionArtifact(dataDir, sessionID, filename string, content []byte) (string, error) {
	artifactDir := filepath.Join(dataDir, "artifacts", "plans", sessionID)
	if err := os.MkdirAll(artifactDir, 0755); err != nil {
		return "", fmt.Errorf("create plan artifact dir: %w", err)
	}

	path := filepath.Join(artifactDir, filename)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return "", fmt.Errorf("create plan artifact parent dir: %w", err)
	}
	if err := os.WriteFile(path, content, 0644); err != nil {
		return "", fmt.Errorf("write plan artifact: %w", err)
	}

	return path, nil
}

// WritePlanDocumentArtifact writes a plan document as a JSON artifact.
func WritePlanDocumentArtifact(dataDir, sessionID, filename string, doc *PlanDocument) (string, error) {
	if doc == nil {
		return "", fmt.Errorf("plan document is required")
	}
	payload, err := doc.MarshalIndented()
	if err != nil {
		return "", fmt.Errorf("marshal plan document: %w", err)
	}
	return WritePlanSessionArtifact(dataDir, sessionID, filename, payload)
}

func writePlanJSONArtifact(dataDir, sessionID, filename string, payload any) (string, error) {
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshal plan artifact json: %w", err)
	}
	return WritePlanSessionArtifact(dataDir, sessionID, filename, data)
}

func persistPlanningArtifacts(ctx context.Context, db *database.DB, sessionID, dataDir string, generationTrace *PlanGenerationTrace, auditResult *InvokeClaudeResult, summary *AuditSummary, finalPlan *PlanDocument, planManifestPath string) error {
	if sessionID == "" || generationTrace == nil {
		return nil
	}

	if generationTrace.EpicRaw != "" {
		rawPath, err := WritePlanSessionArtifact(dataDir, sessionID, "epic-generation/raw.txt", []byte(generationTrace.EpicRaw))
		if err != nil {
			return fmt.Errorf("write epic generation raw artifact: %w", err)
		}
		registerPlanArtifact(ctx, db, sessionID, database.ArtifactTypePlanRawEpicGeneration, rawPath, map[string]any{"phase": "epic_generation"})
	}
	if generationTrace.EpicResponse != nil {
		structuredPath, err := writePlanJSONArtifact(dataDir, sessionID, "epic-generation/structured.json", generationTrace.EpicResponse)
		if err != nil {
			return fmt.Errorf("write epic generation structured artifact: %w", err)
		}
		registerPlanArtifact(ctx, db, sessionID, database.ArtifactTypePlanStructuredEpicGeneration, structuredPath, map[string]any{
			"phase":             "epic_generation",
			"epic_count":        len(generationTrace.EpicResponse.Epics),
			"requirement_count": len(generationTrace.EpicResponse.Requirements),
		})
	}

	for _, taskTrace := range generationTrace.TaskTraces {
		slug := taskTrace.EpicID
		if taskTrace.Raw != "" {
			rawPath, err := WritePlanSessionArtifact(dataDir, sessionID, filepath.Join("task-generation", slug, "raw.txt"), []byte(taskTrace.Raw))
			if err != nil {
				return fmt.Errorf("write task generation raw artifact for %s: %w", taskTrace.EpicID, err)
			}
			registerPlanArtifact(ctx, db, sessionID, database.ArtifactTypePlanRawTaskGeneration, rawPath, map[string]any{
				"phase":    "task_generation",
				"epic_id":  taskTrace.EpicID,
				"epic_ref": taskTrace.EpicRef,
			})
		}
		if taskTrace.Response != nil {
			structuredPath, err := writePlanJSONArtifact(dataDir, sessionID, filepath.Join("task-generation", slug, "structured.json"), taskTrace.Response)
			if err != nil {
				return fmt.Errorf("write task generation structured artifact for %s: %w", taskTrace.EpicID, err)
			}
			registerPlanArtifact(ctx, db, sessionID, database.ArtifactTypePlanStructuredTaskGeneration, structuredPath, map[string]any{
				"phase":      "task_generation",
				"epic_id":    taskTrace.EpicID,
				"epic_ref":   taskTrace.EpicRef,
				"task_count": len(taskTrace.Response.Tasks),
			})
		}
	}

	if auditResult != nil && auditResult.Content != "" {
		rawPath, err := WritePlanSessionArtifact(dataDir, sessionID, "audit/raw.txt", []byte(auditResult.Content))
		if err != nil {
			return fmt.Errorf("write audit raw artifact: %w", err)
		}
		registerPlanArtifact(ctx, db, sessionID, database.ArtifactTypePlanRawAudit, rawPath, map[string]any{"phase": "audit"})
	}
	if auditResult != nil && summary != nil && finalPlan != nil {
		structuredPath, err := WritePlanDocumentArtifact(dataDir, sessionID, "audit/structured.json", finalPlan)
		if err != nil {
			return fmt.Errorf("write audit structured artifact: %w", err)
		}
		registerPlanArtifact(ctx, db, sessionID, database.ArtifactTypePlanStructuredAudit, structuredPath, planArtifactMetadata(finalPlan, "audit"))
	}

	if finalPlan != nil && planManifestPath != "" {
		registerPlanArtifact(ctx, db, sessionID, database.ArtifactTypePlanManifest, planManifestPath, planArtifactMetadata(finalPlan, "manifest"))
	}

	return nil
}

func planArtifactMetadata(doc *PlanDocument, phase string) map[string]any {
	if doc == nil {
		return map[string]any{"phase": phase}
	}
	return map[string]any{
		"phase":             phase,
		"requirement_count": len(doc.Requirements),
		"non_goal_count":    len(doc.NonGoals),
		"warning_count":     len(doc.PlanningWarnings),
		"epic_count":        len(doc.Epics),
		"task_count":        doc.TaskCount(),
	}
}

func registerPlanArtifact(ctx context.Context, db *database.DB, sessionID, artifactType, path string, metadata map[string]any) {
	if sessionID == "" || path == "" {
		return
	}
	if _, err := db.RegisterArtifact(ctx, database.RegisterArtifactParams{
		SessionID:    sessionID,
		ArtifactType: artifactType,
		Path:         path,
		Metadata:     metadata,
	}); err != nil {
		slog.Warn("failed to register plan artifact", "session_id", sessionID, "artifact_type", artifactType, "path", path, "error", err)
	}
}

var nonAlphanumRe = regexp.MustCompile(`[^a-z0-9]+`)

// slugify converts a title into a filename-safe slug.
func slugify(title string) string {
	s := strings.ToLower(title)
	s = nonAlphanumRe.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")

	if len(s) <= 50 {
		return s
	}

	truncated := s[:50]
	if lastHyphen := strings.LastIndex(truncated, "-"); lastHyphen > 0 {
		truncated = truncated[:lastHyphen]
	}
	return strings.TrimRightFunc(truncated, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
}

func newPlanStreamObserver(phase string) func(streaming.StreamEvent) {
	var sawAssistant bool

	return func(ev streaming.StreamEvent) {
		switch ev.Type {
		case "assistant":
			if !sawAssistant && ev.Content != "" {
				sawAssistant = true
				slog.Info("Claude began emitting response", "phase", phase)
			}
		case "tool_use":
			attrs := []any{"phase", phase, "tool", ev.ToolName}
			if ev.ToolInput != "" {
				attrs = append(attrs, "input", ev.ToolInput)
			}
			slog.Info("Claude tool use", attrs...)
		}
	}
}

func logPlanInvocationComplete(phase string, result *InvokeClaudeResult) {
	if result == nil {
		return
	}

	attrs := []any{
		"phase", phase,
		"duration", result.Duration,
		"tokens_in", result.TokensIn,
		"tokens_out", result.TokensOut,
		"cost_usd", result.CostUSD,
	}
	if result.Model != "" {
		attrs = append(attrs, "model", result.Model)
	}
	if result.SessionID != "" {
		attrs = append(attrs, "session_id", result.SessionID)
	}
	if len(result.ToolCalls) > 0 {
		attrs = append(attrs, "tool_calls", result.ToolCalls)
	}

	slog.Info("Claude invocation complete", attrs...)
}
