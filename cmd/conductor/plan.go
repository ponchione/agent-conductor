package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode"

	"github.com/google/uuid"
	"github.com/ponchione/agent-conductor/internal/config"
	"github.com/ponchione/agent-conductor/internal/database"
	"github.com/ponchione/agent-conductor/internal/llm"
	"github.com/ponchione/agent-conductor/internal/logging"
	"github.com/ponchione/agent-conductor/internal/streaming"
	"github.com/ponchione/agent-conductor/internal/templates"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

var planCmd = &cobra.Command{
	Use:   "plan <spec-file>",
	Short: "Generate a hierarchical plan manifest from a spec file via Claude Code",
	Long: `plan reads a freeform specification file, sends it to Claude Code with a
planning system prompt, and generates a single hierarchical plan.yaml
manifest in the output directory. This enables rapid decomposition of
feature specs into a dependency-ordered execution plan.`,
	Args: cobra.ExactArgs(1),
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		level := slog.LevelInfo
		if verbose {
			level = slog.LevelDebug
		}
		h := logging.NewHandler(os.Stdout, &slog.HandlerOptions{Level: level})
		slog.SetDefault(slog.New(h))

		loaded, err := config.Load(projectPath)
		if err != nil {
			return fmt.Errorf("could not load project config: %w\n  For greenfield projects, use \"conductor init\" instead", err)
		}
		cfg = loaded
		return nil
	},
	RunE: runPlan,
}

var (
	planOutputDir              string
	planTimeout                int
	planSkipAudit              bool
	planAllowUnauditedFallback bool
)

func init() {
	planCmd.Flags().StringVar(&planOutputDir, "output", "./work-orders/", "Output directory for generated plan manifest")
	planCmd.Flags().IntVar(&planTimeout, "timeout", 300, "Timeout in seconds for the Claude invocation")
	planCmd.Flags().BoolVar(&planSkipAudit, "skip-audit", false, "Skip the audit pass that reviews the generated plan manifest for completeness")
	planCmd.Flags().BoolVar(&planAllowUnauditedFallback, "allow-unaudited-fallback", false, "Allow planning to continue with an unaudited plan manifest if the audit pass fails")
}

func runPlan(cmd *cobra.Command, args []string) (err error) {
	specPath := args[0]
	specData, err := os.ReadFile(specPath)
	if err != nil {
		return fmt.Errorf("failed to read spec file %q: %w", specPath, err)
	}

	if err := config.EnsureDataDirs(cfg); err != nil {
		return fmt.Errorf("failed to create data directories: %w", err)
	}

	dbPath := filepath.Join(cfg.Project.DataDir, "db", "conductor.db")
	db, err := database.NewDB(dbPath)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer db.Close()

	ctx := context.Background()
	sessionID, err := db.StartSession(ctx, database.SessionKindPlanOnly, cfg.Project.Name, specPath)
	if err != nil {
		return fmt.Errorf("create session: %w", err)
	}
	defer func() {
		if sessionID == "" {
			return
		}

		state := database.SessionStateCompleted
		errorMessage := ""
		if err != nil {
			state = database.SessionStateFailed
			errorMessage = err.Error()
		}
		if transitionErr := db.TransitionSessionState(ctx, sessionID, state, errorMessage); transitionErr != nil {
			slog.Warn("failed to update plan session state", "session_id", sessionID, "state", state, "error", transitionErr)
		}
	}()

	claudePath, err := exec.LookPath("claude")
	if err != nil {
		return fmt.Errorf("claude binary not found in PATH: %w", err)
	}
	slog.Debug("resolved claude binary", "path", claudePath)

	prompts, err := templates.LoadPromptsForPlan(cfg)
	if err != nil {
		return fmt.Errorf("load prompts: %w", err)
	}

	timeout := time.Duration(planTimeout) * time.Second
	invoker := func(phase, systemPrompt, userMsg string) (*invokeClaudeResult, error) {
		slog.Info("invoking Claude", "phase", phase, "timeout", timeout)
		result, invokeErr := invokeClaudeWithStats(claudePath, systemPrompt, userMsg, timeout, cfg.Project.Path, newPlanStreamObserver(phase))
		if invokeErr != nil {
			return nil, invokeErr
		}
		logPlanInvocationComplete(phase, result)
		return result, nil
	}

	planDoc, generationTrace, genRetryCount, err := generatePlanDocument(specPath, specData, sessionID, cfg, prompts, invoker)
	if err != nil {
		var phaseErr *planGenerationPhaseError
		if errors.As(err, &phaseErr) && phaseErr.Raw != "" {
			if mkErr := os.MkdirAll(planOutputDir, 0755); mkErr != nil {
				return fmt.Errorf("parse failed and could not create output dir: parse=%w, mkdir=%v", err, mkErr)
			}
			filename := "raw-plan-output.txt"
			if phaseErr.Phase != "" {
				filename = fmt.Sprintf("raw-plan-output-%s.txt", slugify(phaseErr.Phase))
			}
			rawPath := filepath.Join(planOutputDir, filename)
			if wErr := os.WriteFile(rawPath, []byte(phaseErr.Raw), 0644); wErr != nil {
				return fmt.Errorf("parse failed and could not write raw output: parse=%w, write=%v", err, wErr)
			}
			registerPlanArtifact(ctx, db, sessionID, database.ArtifactTypePlanParseFailure, rawPath, map[string]any{
				"phase": phaseErr.Phase,
			})
			return fmt.Errorf("%w\nRaw output saved to: %s", err, rawPath)
		}
		return err
	}

	preAuditWorkOrderCount := planDoc.TaskCount()

	var summary *auditSummary
	var auditResult *invokeClaudeResult
	if planSkipAudit {
		slog.Info("skipping audit pass (--skip-audit)")
	} else {
		slog.Info("invoking Claude", "phase", "audit", "count", planDoc.TaskCount(), "timeout", timeout)
		auditedPlan, auditSummary, auditRes, auditErr := auditWorkOrders(claudePath, prompts.PlanAudit, string(specData), planDoc, cfg, timeout)
		resolvedPlan, resolvedSummary, resolvedAuditResult, resolveErr := resolveAuditOutcome(planDoc, auditedPlan, auditSummary, auditRes, auditErr, planAllowUnauditedFallback)
		if resolveErr != nil {
			return resolveErr
		}
		planDoc = resolvedPlan
		summary = resolvedSummary
		auditResult = resolvedAuditResult
		if auditResult != nil {
			logPlanInvocationComplete("audit", auditResult)
		}
	}

	taskCount := planDoc.TaskCount()
	planManifestPath, err := writePlanManifest(planDoc, planOutputDir)
	if err != nil {
		return fmt.Errorf("failed to write plan manifest: %w", err)
	}

	// Persist metrics (best-effort).
	if recErr := recordPlanRun(db, sessionID, cfg.Project.Name, specPath, specData, generationTrace, auditResult, len(planDoc.Epics), preAuditWorkOrderCount, taskCount, summary, genRetryCount); recErr != nil {
		slog.Warn("failed to record plan run metrics", "error", recErr)
	}

	if persistErr := persistPlanningArtifacts(ctx, db, sessionID, cfg.Project.DataDir, generationTrace, auditResult, summary, planDoc, planManifestPath); persistErr != nil {
		slog.Warn("failed to persist planning artifacts", "session_id", sessionID, "error", persistErr)
	}

	fmt.Printf("\nWrote hierarchical plan manifest to %s\n", planManifestPath)
	fmt.Printf("Epics: %d  Tasks: %d\n\n", len(planDoc.Epics), taskCount)

	if summary != nil {
		fmt.Printf("Audit: %d added, %d modified, %d unchanged\n", summary.Added, summary.Modified, summary.Unchanged)
		for _, change := range summary.Changes {
			fmt.Printf("  - %s\n", change)
		}
		fmt.Println()
	}

	return nil
}

type planningPhaseInvoker func(phase, systemPrompt, userMsg string) (*invokeClaudeResult, error)

type planTaskGenerationTrace struct {
	EpicID   string
	EpicRef  string
	Raw      string
	Response *rawTaskPlanResponse
	Result   *invokeClaudeResult
}

type planGenerationTrace struct {
	EpicRaw         string
	EpicResponse    *rawEpicPlanResponse
	EpicResult      *invokeClaudeResult
	TaskTraces      []planTaskGenerationTrace
	AggregateRaw    string
	AggregateResult *invokeClaudeResult
}

type planGenerationPhaseError struct {
	Phase string
	Raw   string
	Err   error
}

func (e *planGenerationPhaseError) Error() string {
	if e == nil || e.Err == nil {
		return ""
	}
	return e.Err.Error()
}

func (e *planGenerationPhaseError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

func generatePlanDocument(specPath string, specData []byte, sessionID string, cfg *config.ProjectConfig, prompts *templates.LoadedPrompts, invoker planningPhaseInvoker) (*planDocument, *planGenerationTrace, int, error) {
	builder := newPlanContextBuilder(cfg)

	epicUserMsg := builder.BuildEpicDecomposition(string(specData))
	epicResp, epicResult, epicRaw, retryCount, err := invokeEpicPlanningPhase(invoker, prompts.PlanEpic, epicUserMsg)
	if err != nil {
		return nil, &planGenerationTrace{EpicRaw: epicRaw}, retryCount, err
	}

	aggregatedResult := epicResult
	var rawGeneration strings.Builder
	appendGenerationOutput(&rawGeneration, "EPIC GENERATION", epicRaw)
	trace := &planGenerationTrace{
		EpicRaw:      epicRaw,
		EpicResponse: epicResp,
		EpicResult:   epicResult,
	}

	priorEpics := make([]planEpic, 0, len(epicResp.Epics))
	taskResponses := make([]*rawTaskPlanResponse, 0, len(epicResp.Epics))
	nextTaskID := 1
	for epicIndex, rawEpic := range epicResp.Epics {
		targetEpic := planEpic{
			ID:             canonicalEpicID(epicIndex + 1),
			EpicRef:        rawEpic.EpicRef,
			Title:          rawEpic.Title,
			Description:    rawEpic.Description,
			Covers:         append([]string(nil), rawEpic.Covers...),
			DependsOnEpics: append([]string(nil), rawEpic.DependsOnEpics...),
		}

		taskUserMsg := builder.BuildTaskDecomposition(string(specData), targetEpic, priorEpics)
		taskResp, taskResult, taskRaw, taskRetries, taskErr := invokeTaskPlanningPhase(invoker, targetEpic.ID, prompts.PlanTask, taskUserMsg)
		retryCount += taskRetries
		if taskErr != nil {
			trace.AggregateRaw = rawGeneration.String()
			trace.AggregateResult = aggregatedResult
			return nil, trace, retryCount, taskErr
		}
		appendGenerationOutput(&rawGeneration, fmt.Sprintf("TASK GENERATION %s", targetEpic.ID), taskRaw)
		mergeInvokeClaudeResults(aggregatedResult, taskResult)
		taskResponses = append(taskResponses, taskResp)
		trace.TaskTraces = append(trace.TaskTraces, planTaskGenerationTrace{
			EpicID:   targetEpic.ID,
			EpicRef:  targetEpic.EpicRef,
			Raw:      taskRaw,
			Response: taskResp,
			Result:   taskResult,
		})

		priorTasks := make([]planTask, len(taskResp.Tasks))
		for i, rawTask := range taskResp.Tasks {
			taskID := canonicalTaskID(nextTaskID)
			nextTaskID++

			task, convErr := rawTask.toPlanTask(taskID, targetEpic.ID)
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
	planDoc, err := assemblePlanDocument(planManifestMetadata{
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
	if err := validatePlanDocument(planDoc, cfg); err != nil {
		trace.AggregateRaw = rawGeneration.String()
		trace.AggregateResult = aggregatedResult
		return nil, trace, retryCount, fmt.Errorf("planner output validation failed: %w", err)
	}

	trace.AggregateRaw = rawGeneration.String()
	trace.AggregateResult = aggregatedResult
	return planDoc, trace, retryCount, nil
}

func invokeEpicPlanningPhase(invoker planningPhaseInvoker, prompt, userMsg string) (*rawEpicPlanResponse, *invokeClaudeResult, string, int, error) {
	return invokePlanningPhaseWithRetry("planning_epic", "planning_epic_retry", prompt, userMsg, invoker, parseEpicPlanResponse)
}

func invokeTaskPlanningPhase(invoker planningPhaseInvoker, epicID, prompt, userMsg string) (*rawTaskPlanResponse, *invokeClaudeResult, string, int, error) {
	phase := "planning_task_" + epicID
	retryPhase := phase + "_retry"
	return invokePlanningPhaseWithRetry(phase, retryPhase, prompt, userMsg, invoker, parseTaskPlanResponse)
}

func invokePlanningPhaseWithRetry[T any](phase, retryPhase, prompt, userMsg string, invoker planningPhaseInvoker, parse func(string) (*T, error)) (*T, *invokeClaudeResult, string, int, error) {
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
	mergeInvokeClaudeResults(result, retryResult)
	raw = retryResult.Content

	parsed, parseErr = parse(raw)
	if parseErr != nil {
		return nil, nil, raw, 1, &planGenerationPhaseError{
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

type aggregatedPlanningMetrics struct {
	Model     string
	TokensIn  int
	TokensOut int
	CostUSD   float64
	Duration  time.Duration
	CallCount int
}

func aggregatePlanningMetrics(results []*invokeClaudeResult) aggregatedPlanningMetrics {
	var metrics aggregatedPlanningMetrics
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

func taskGenerationMetrics(trace *planGenerationTrace) aggregatedPlanningMetrics {
	if trace == nil || len(trace.TaskTraces) == 0 {
		return aggregatedPlanningMetrics{}
	}

	results := make([]*invokeClaudeResult, 0, len(trace.TaskTraces))
	for _, taskTrace := range trace.TaskTraces {
		results = append(results, taskTrace.Result)
	}
	return aggregatePlanningMetrics(results)
}

func resolveAuditOutcome(basePlan, auditedPlan *planDocument, summary *auditSummary, auditResult *invokeClaudeResult, auditErr error, allowFallback bool) (*planDocument, *auditSummary, *invokeClaudeResult, error) {
	if auditErr == nil {
		return auditedPlan, summary, auditResult, nil
	}
	if !allowFallback {
		return nil, nil, nil, fmt.Errorf("audit pass failed and unaudited fallback is disabled: %w", auditErr)
	}
	slog.Warn("audit pass failed, using unaudited plan manifest due to override", "error", auditErr)
	return basePlan, nil, auditResult, nil
}

// recordPlanRun persists plan metrics to the plan_runs table (best-effort).
func recordPlanRun(db *database.DB, sessionID, project, specFile string, specData []byte, generationTrace *planGenerationTrace, auditResult *invokeClaudeResult, epicCount, preAuditWorkOrderCount, workOrdersGenerated int, summary *auditSummary, generationRetryCount int) error {
	id := uuid.New().String()
	specFingerprint := sha256.Sum256(specData)

	params := database.InsertPlanRunParams{
		ID:                      id,
		SpecFile:                specFile,
		Project:                 database.String(project),
		SpecFingerprint:         database.String(hex.EncodeToString(specFingerprint[:])),
		WorkOrdersGenerated:     database.Int64(workOrdersGenerated),
		EpicCount:               database.Int64(epicCount),
		TaskCount:               database.Int64(workOrdersGenerated),
		PreAuditWorkOrderCount:  database.Int64(preAuditWorkOrderCount),
		PostAuditWorkOrderCount: database.Int64(workOrdersGenerated),
		GenerationRetryCount:    int64(generationRetryCount),
	}
	if generationTrace != nil {
		if genResult := generationTrace.AggregateResult; genResult != nil {
			params.GenerationModel = database.String(genResult.Model)
			params.GenerationTokensIn = database.Int64(genResult.TokensIn)
			params.GenerationTokensOut = database.Int64(genResult.TokensOut)
			params.GenerationSessionID = database.String(genResult.SessionID)
			params.GenerationCostUsd = database.Float64(genResult.CostUSD)
			params.GenerationDurationMs = database.Int64Value(genResult.Duration.Milliseconds())
		}
		if epicResult := generationTrace.EpicResult; epicResult != nil {
			params.EpicGenerationModel = database.String(epicResult.Model)
			params.EpicGenerationTokensIn = database.Int64(epicResult.TokensIn)
			params.EpicGenerationTokensOut = database.Int64(epicResult.TokensOut)
			params.EpicGenerationCostUsd = database.Float64(epicResult.CostUSD)
			params.EpicGenerationDurationMs = database.Int64Value(epicResult.Duration.Milliseconds())
		}
		taskMetrics := taskGenerationMetrics(generationTrace)
		if taskMetrics.CallCount > 0 {
			params.TaskGenerationModel = database.String(taskMetrics.Model)
			params.TaskGenerationCallCount = database.Int64(taskMetrics.CallCount)
			params.TaskGenerationTokensIn = database.Int64(taskMetrics.TokensIn)
			params.TaskGenerationTokensOut = database.Int64(taskMetrics.TokensOut)
			params.TaskGenerationCostUsd = database.Float64(taskMetrics.CostUSD)
			params.TaskGenerationDurationMs = database.Int64Value(taskMetrics.Duration.Milliseconds())
		}
	}
	if auditResult != nil {
		params.AuditModel = database.String(auditResult.Model)
		params.AuditTokensIn = database.Int64(auditResult.TokensIn)
		params.AuditTokensOut = database.Int64(auditResult.TokensOut)
		params.AuditSessionID = database.String(auditResult.SessionID)
		params.AuditCostUsd = database.Float64(auditResult.CostUSD)
		params.AuditDurationMs = database.Int64Value(auditResult.Duration.Milliseconds())
	}
	if summary != nil {
		params.AuditWorkOrdersAdded = database.Int64(summary.Added)
		params.AuditWorkOrdersModified = database.Int64(summary.Modified)
		params.AuditWorkOrdersUnchanged = database.Int64(summary.Unchanged)
		if len(summary.Changes) > 0 {
			payload, err := json.Marshal(summary.Changes)
			if err != nil {
				return fmt.Errorf("marshal audit changes: %w", err)
			}
			params.AuditChangeText = database.String(string(payload))
		}
	}

	ctx := context.Background()
	if err := db.InsertPlanRun(ctx, params); err != nil {
		return fmt.Errorf("insert plan_runs: %w", err)
	}
	if err := db.LinkPlanRunToSession(ctx, id, sessionID); err != nil {
		return fmt.Errorf("link plan_run to session: %w", err)
	}

	slog.Debug("recorded plan run", "id", id)
	return nil
}

// invokeClaudeResult holds the text output and token usage from a Claude invocation.
type invokeClaudeResult struct {
	Content   string
	TokensIn  int
	TokensOut int
	Model     string
	CostUSD   float64
	Duration  time.Duration
	SessionID string
	ToolCalls map[string]int
}

// invokeClaude runs the claude binary and returns the final text output.
func invokeClaude(claudePath, systemPrompt, userMsg string, timeout time.Duration, workDir string) (string, error) {
	result, err := invokeClaudeWithStats(claudePath, systemPrompt, userMsg, timeout, workDir, nil)
	if err != nil {
		return "", err
	}
	return result.Content, nil
}

// invokeClaudeWithStats runs the claude binary with stream-json output and returns token usage.
func invokeClaudeWithStats(claudePath, systemPrompt, userMsg string, timeout time.Duration, workDir string, callback func(streaming.StreamEvent)) (*invokeClaudeResult, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	args := []string{
		"--print",
		"--verbose",
		"--output-format", "stream-json",
		"--dangerously-skip-permissions",
		"--system-prompt", systemPrompt,
		userMsg,
	}
	cmd := exec.CommandContext(ctx, claudePath, args...)
	if workDir != "" {
		cmd.Dir = workDir
	}
	cmd.Env = append(os.Environ(), "CLAUDE_CODE_DISABLE_AUTO_MEMORY=1")

	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to create stdout pipe: %w", err)
	}

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	start := time.Now()
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("failed to start claude: %w", err)
	}

	content, sr := streaming.CollectText(stdoutPipe, nil, callback)

	if err := cmd.Wait(); err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return nil, fmt.Errorf("claude timed out after %s", timeout)
		}
		return nil, fmt.Errorf("claude exited with error: %w\nstderr: %s", err, stderr.String())
	}
	duration := time.Since(start)

	return &invokeClaudeResult{
		Content:   content,
		TokensIn:  sr.TokensIn,
		TokensOut: sr.TokensOut,
		Model:     sr.Model,
		CostUSD:   sr.CostUSD,
		Duration:  duration,
		SessionID: sr.SessionID,
		ToolCalls: sr.ToolCalls,
	}, nil
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

func logPlanInvocationComplete(phase string, result *invokeClaudeResult) {
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

func mergeInvokeClaudeResults(dst, src *invokeClaudeResult) {
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

// auditWorkOrders runs a second LLM pass to audit the assembled plan manifest against the spec.
func auditWorkOrders(claudePath string, auditPrompt string, spec string, planDoc *planDocument, cfg *config.ProjectConfig, timeout time.Duration) (*planDocument, *auditSummary, *invokeClaudeResult, error) {
	userMsg, err := buildAuditUserMessage(spec, planDoc, cfg)
	if err != nil {
		return planDoc, nil, nil, err
	}

	result, err := invokeClaudeWithStats(claudePath, auditPrompt, userMsg, timeout, cfg.Project.Path, newPlanStreamObserver("audit"))
	if err != nil {
		return planDoc, nil, nil, fmt.Errorf("audit invocation failed: %w", err)
	}

	cleaned := llm.CleanLLMResponse(result.Content)
	var resp auditResponse
	if err := json.Unmarshal([]byte(cleaned), &resp); err != nil {
		return planDoc, nil, result, fmt.Errorf("audit response parse failed: %w", err)
	}

	if len(resp.Epics) == 0 {
		return planDoc, nil, result, fmt.Errorf("audit response contained no epics")
	}

	auditedPlan := resp.toPlanDocument()
	if err := validatePlanDocument(auditedPlan, cfg); err != nil {
		return planDoc, nil, result, fmt.Errorf("audited plan failed validation: %w", err)
	}

	return auditedPlan, &resp.AuditSummary, result, nil
}

func buildAuditUserMessage(spec string, planDoc *planDocument, cfg *config.ProjectConfig) (string, error) {
	planJSON, err := planDoc.MarshalIndented()
	if err != nil {
		return "", fmt.Errorf("failed to marshal plan manifest: %w", err)
	}
	builder := newPlanContextBuilder(cfg)

	var sb strings.Builder
	sb.WriteString("=== SPECIFICATION ===\n")
	sb.WriteString(spec)
	sb.WriteString("\n")

	sb.WriteString("\n=== PROJECT CONTEXT ===\n")
	if projectFacts := builder.buildProjectFacts(); projectFacts != "" {
		sb.WriteString("\n=== PROJECT FACTS ===\n")
		sb.WriteString(projectFacts)
	}
	if convSection := builder.buildConventions(); convSection != "" {
		sb.WriteString("\n=== PROJECT CONVENTIONS ===\n")
		sb.WriteString(convSection)
	}
	treeStr, totalFiles := builder.buildFileTree()
	sb.WriteString("\n=== PROJECT FILE TREE ===\n")
	if treeStr != "" {
		sb.WriteString(treeStr)
		treeLines := strings.Count(treeStr, "\n")
		if totalFiles > treeLines {
			fmt.Fprintf(&sb, "... (%d more files not shown)\n", totalFiles-treeLines)
		}
	} else {
		sb.WriteString("(no matching files)\n")
	}

	sb.WriteString("\n=== GENERATED PLAN ===\n")
	sb.Write(planJSON)
	sb.WriteString("\n")

	return sb.String(), nil
}

// writePlanManifest writes the final plan document as plan.yaml.
func writePlanManifest(doc *planDocument, outputDir string) (string, error) {
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

func writePlanSessionArtifact(dataDir, sessionID, filename string, content []byte) (string, error) {
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

func writePlanDocumentArtifact(dataDir, sessionID, filename string, doc *planDocument) (string, error) {
	if doc == nil {
		return "", fmt.Errorf("plan document is required")
	}
	payload, err := doc.MarshalIndented()
	if err != nil {
		return "", fmt.Errorf("marshal plan document: %w", err)
	}
	return writePlanSessionArtifact(dataDir, sessionID, filename, payload)
}

func writePlanJSONArtifact(dataDir, sessionID, filename string, payload any) (string, error) {
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshal plan artifact json: %w", err)
	}
	return writePlanSessionArtifact(dataDir, sessionID, filename, data)
}

func persistPlanningArtifacts(ctx context.Context, db *database.DB, sessionID, dataDir string, generationTrace *planGenerationTrace, auditResult *invokeClaudeResult, summary *auditSummary, finalPlan *planDocument, planManifestPath string) error {
	if sessionID == "" || generationTrace == nil {
		return nil
	}

	if generationTrace.EpicRaw != "" {
		rawPath, err := writePlanSessionArtifact(dataDir, sessionID, "epic-generation/raw.txt", []byte(generationTrace.EpicRaw))
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
			rawPath, err := writePlanSessionArtifact(dataDir, sessionID, filepath.Join("task-generation", slug, "raw.txt"), []byte(taskTrace.Raw))
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
		rawPath, err := writePlanSessionArtifact(dataDir, sessionID, "audit/raw.txt", []byte(auditResult.Content))
		if err != nil {
			return fmt.Errorf("write audit raw artifact: %w", err)
		}
		registerPlanArtifact(ctx, db, sessionID, database.ArtifactTypePlanRawAudit, rawPath, map[string]any{"phase": "audit"})
	}
	if auditResult != nil && summary != nil && finalPlan != nil {
		structuredPath, err := writePlanDocumentArtifact(dataDir, sessionID, "audit/structured.json", finalPlan)
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

func planArtifactMetadata(doc *planDocument, phase string) map[string]any {
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
// Lowercase, replace non-alphanumeric runs with hyphens, trim, truncate at word boundary within 50 chars.
func slugify(title string) string {
	s := strings.ToLower(title)
	s = nonAlphanumRe.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")

	if len(s) <= 50 {
		return s
	}

	// Truncate at word boundary (hyphen) within 50 chars.
	truncated := s[:50]
	if lastHyphen := strings.LastIndex(truncated, "-"); lastHyphen > 0 {
		truncated = truncated[:lastHyphen]
	}
	return strings.TrimRightFunc(truncated, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
}
