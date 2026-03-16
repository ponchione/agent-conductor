package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/google/uuid"
	"github.com/ponchione/agent-conductor/internal/config"
	"github.com/ponchione/agent-conductor/internal/database"
	"github.com/ponchione/agent-conductor/internal/llm"
	"github.com/ponchione/agent-conductor/internal/logging"
	"github.com/ponchione/agent-conductor/internal/models"
	"github.com/ponchione/agent-conductor/internal/streaming"
	"github.com/ponchione/agent-conductor/internal/templates"
	"github.com/ponchione/agent-conductor/internal/util"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

var planCmd = &cobra.Command{
	Use:   "plan <spec-file>",
	Short: "Generate work orders from a spec file via Claude Code",
	Long: `plan reads a freeform specification file, sends it to Claude Code with a
planning system prompt, and generates individual work order YAML files in
the output directory. This enables rapid decomposition of feature specs into
the work order format the conductor pipeline consumes.`,
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
	planOutputDir string
	planTimeout   int
	planSkipAudit bool
)

func init() {
	planCmd.Flags().StringVar(&planOutputDir, "output", "./work-orders/", "Output directory for generated work order files")
	planCmd.Flags().IntVar(&planTimeout, "timeout", 300, "Timeout in seconds for the Claude invocation")
	planCmd.Flags().BoolVar(&planSkipAudit, "skip-audit", false, "Skip the audit pass that reviews generated work orders for completeness")
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

	userMsg := buildPlanUserMessage(string(specData), cfg)
	timeout := time.Duration(planTimeout) * time.Second

	slog.Info("invoking Claude", "phase", "planning", "timeout", timeout)
	genResult, err := invokeClaudeWithStats(claudePath, templates.DefaultPlanPrompt, userMsg, timeout, newPlanStreamObserver("planning"))
	if err != nil {
		return fmt.Errorf("claude invocation failed: %w", err)
	}
	logPlanInvocationComplete("planning", genResult)
	raw := genResult.Content
	genRetryCount := 0

	workOrders, parseErr := parsePlanResponse(raw)
	if parseErr != nil {
		slog.Warn("first parse attempt failed, retrying with correction", "error", parseErr)

		correctionMsg := fmt.Sprintf(
			"%s\n\n=== CORRECTION ===\nYour previous response could not be parsed: %s\n\nPrevious response:\n%s\n\nPlease respond with ONLY a valid JSON object matching the schema. No markdown, no commentary.",
			userMsg, parseErr.Error(), raw,
		)

		slog.Info("invoking Claude", "phase", "planning_retry", "timeout", timeout)
		retryResult, retryErr := invokeClaudeWithStats(claudePath, templates.DefaultPlanPrompt, correctionMsg, timeout, newPlanStreamObserver("planning_retry"))
		if retryErr != nil {
			return fmt.Errorf("claude retry invocation failed: %w", retryErr)
		}
		logPlanInvocationComplete("planning_retry", retryResult)
		mergeInvokeClaudeResults(genResult, retryResult)
		genRetryCount++
		raw = retryResult.Content

		workOrders, parseErr = parsePlanResponse(raw)
		if parseErr != nil {
			if mkErr := os.MkdirAll(planOutputDir, 0755); mkErr != nil {
				return fmt.Errorf("parse failed and could not create output dir: parse=%w, mkdir=%v", parseErr, mkErr)
			}
			rawPath := filepath.Join(planOutputDir, "raw-plan-output.txt")
			if wErr := os.WriteFile(rawPath, []byte(raw), 0644); wErr != nil {
				return fmt.Errorf("parse failed and could not write raw output: parse=%w, write=%v", parseErr, wErr)
			}
			registerPlanArtifact(ctx, db, sessionID, database.ArtifactTypePlanParseFailure, rawPath, map[string]any{
				"phase": "planning_retry",
			})
			return fmt.Errorf("could not parse plan response after retry: %w\nRaw output saved to: %s", parseErr, rawPath)
		}
	}

	preAuditWorkOrderCount := len(workOrders)

	var summary *auditSummary
	var auditResult *invokeClaudeResult
	if planSkipAudit {
		slog.Info("skipping audit pass (--skip-audit)")
	} else {
		slog.Info("invoking Claude", "phase", "audit", "count", len(workOrders), "timeout", timeout)
		auditedOrders, auditSummary, auditRes, auditErr := auditWorkOrders(claudePath, string(specData), workOrders, cfg, timeout)
		if auditErr != nil {
			slog.Warn("audit pass failed, using unaudited work orders", "error", auditErr)
		} else {
			workOrders = auditedOrders
			summary = auditSummary
			auditResult = auditRes
			logPlanInvocationComplete("audit", auditResult)
		}
	}

	// Persist metrics (best-effort).
	if recErr := recordPlanRun(db, sessionID, cfg.Project.Name, specPath, specData, genResult, auditResult, preAuditWorkOrderCount, len(workOrders), summary, genRetryCount); recErr != nil {
		slog.Warn("failed to record plan run metrics", "error", recErr)
	}

	generationRawPath, err := writePlanSessionArtifact(cfg.Project.DataDir, sessionID, "generation-raw.txt", []byte(raw))
	if err != nil {
		slog.Warn("failed to persist raw generation output", "session_id", sessionID, "error", err)
	} else {
		registerPlanArtifact(ctx, db, sessionID, database.ArtifactTypePlanRawGeneration, generationRawPath, map[string]any{
			"phase": "planning",
		})
	}
	if auditResult != nil && auditResult.Content != "" {
		auditRawPath, writeErr := writePlanSessionArtifact(cfg.Project.DataDir, sessionID, "audit-raw.txt", []byte(auditResult.Content))
		if writeErr != nil {
			slog.Warn("failed to persist raw audit output", "session_id", sessionID, "error", writeErr)
		} else {
			registerPlanArtifact(ctx, db, sessionID, database.ArtifactTypePlanRawAudit, auditRawPath, map[string]any{
				"phase": "audit",
			})
		}
	}

	workOrderPaths, err := writeWorkOrderFiles(workOrders, planOutputDir)
	if err != nil {
		return fmt.Errorf("failed to write work order files: %w", err)
	}
	for i, path := range workOrderPaths {
		metadata := map[string]any{
			"index": i + 1,
			"title": workOrders[i].Title,
			"type":  workOrders[i].Type,
		}
		if workOrders[i].AuditSource != "" {
			metadata["audit_source"] = workOrders[i].AuditSource
		}
		registerPlanArtifact(ctx, db, sessionID, database.ArtifactTypeGeneratedWorkOrder, path, metadata)
	}

	fmt.Printf("\nGenerated %d work order(s) in %s:\n\n", len(workOrders), planOutputDir)
	for i, wo := range workOrders {
		fmt.Printf("  %03d  %-50s  [%s]\n", i+1, wo.Title, wo.Type)
	}
	fmt.Println()

	if summary != nil {
		fmt.Printf("Audit: %d added, %d modified, %d unchanged\n", summary.Added, summary.Modified, summary.Unchanged)
		for _, change := range summary.Changes {
			fmt.Printf("  - %s\n", change)
		}
		fmt.Println()
	}

	return nil
}

// recordPlanRun persists plan metrics to the plan_runs table (best-effort).
func recordPlanRun(db *database.DB, sessionID, project, specFile string, specData []byte, genResult *invokeClaudeResult, auditResult *invokeClaudeResult, preAuditWorkOrderCount, workOrdersGenerated int, summary *auditSummary, generationRetryCount int) error {
	id := uuid.New().String()
	specFingerprint := sha256.Sum256(specData)

	params := database.InsertPlanRunParams{
		ID:                      id,
		SpecFile:                specFile,
		Project:                 database.String(project),
		SpecFingerprint:         database.String(hex.EncodeToString(specFingerprint[:])),
		WorkOrdersGenerated:     database.Int64(workOrdersGenerated),
		PreAuditWorkOrderCount:  database.Int64(preAuditWorkOrderCount),
		PostAuditWorkOrderCount: database.Int64(workOrdersGenerated),
		GenerationRetryCount:    int64(generationRetryCount),
	}
	if genResult != nil {
		params.GenerationModel = database.String(genResult.Model)
		params.GenerationTokensIn = database.Int64(genResult.TokensIn)
		params.GenerationTokensOut = database.Int64(genResult.TokensOut)
		params.GenerationSessionID = database.String(genResult.SessionID)
		params.GenerationCostUsd = database.Float64(genResult.CostUSD)
		params.GenerationDurationMs = database.Int64Value(genResult.Duration.Milliseconds())
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

// buildPlanUserMessage assembles the user message sent to Claude for planning.
func buildPlanUserMessage(spec string, cfg *config.ProjectConfig) string {
	var sb strings.Builder

	sb.WriteString("=== SPECIFICATION ===\n")
	sb.WriteString(spec)
	sb.WriteString("\n")

	sb.WriteString("\n=== PROJECT CONTEXT ===\n")

	if convSection := planBuildConventions(cfg.Conventions); convSection != "" {
		sb.WriteString("\n=== PROJECT CONVENTIONS ===\n")
		sb.WriteString(convSection)
	}

	treeStr, totalFiles := planBuildFileTree(cfg)
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

	return sb.String()
}

// planBuildConventions formats project conventions for the plan prompt.
// Reimplements the unexported buildConventions from internal/context/assembly.go.
func planBuildConventions(conv config.Conventions) string {
	if conv.ModulePath == "" && len(conv.ModuleStructure) == 0 &&
		conv.SharedPath == "" && conv.SQLPath == "" && conv.DocsPath == "" {
		return ""
	}

	var sb strings.Builder
	if conv.ModulePath != "" {
		fmt.Fprintf(&sb, "Module path: %s\n", conv.ModulePath)
	}
	if len(conv.ModuleStructure) > 0 {
		fmt.Fprintf(&sb, "Module structure: %s\n", strings.Join(conv.ModuleStructure, ", "))
	}
	if conv.SharedPath != "" {
		fmt.Fprintf(&sb, "Shared path: %s\n", conv.SharedPath)
	}
	if conv.SQLPath != "" {
		fmt.Fprintf(&sb, "SQL path: %s\n", conv.SQLPath)
	}
	if conv.DocsPath != "" {
		fmt.Fprintf(&sb, "Docs path: %s\n", conv.DocsPath)
	}
	return sb.String()
}

// planBuildFileTree walks the project directory and returns an indented file tree.
// Reimplements the unexported buildFileTree from internal/context/assembly.go.
func planBuildFileTree(cfg *config.ProjectConfig) (string, int) {
	root := cfg.Project.Path
	if _, err := os.Stat(root); err != nil {
		slog.Warn("project path not accessible for file tree", "path", root, "error", err)
		return "", 0
	}

	var matched []string
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			return nil
		}
		relPath, err := filepath.Rel(root, path)
		if err != nil {
			return nil
		}
		relPath = filepath.ToSlash(relPath)

		if !util.MatchesAnyGlob(cfg.Index.Include, relPath) {
			return nil
		}
		if util.MatchesAnyGlob(cfg.Index.Exclude, relPath) {
			return nil
		}
		matched = append(matched, relPath)
		return nil
	})

	sort.Strings(matched)
	totalFiles := len(matched)
	if totalFiles == 0 {
		return "", 0
	}

	cap := cfg.Index.MaxTreeLines
	if cap <= 0 {
		cap = 200
	}

	var sb strings.Builder
	emittedDirs := make(map[string]bool)
	fileLines := 0

	for _, rel := range matched {
		if fileLines >= cap {
			break
		}

		parts := strings.Split(rel, "/")
		for depth := 0; depth < len(parts)-1; depth++ {
			dirKey := strings.Join(parts[:depth+1], "/")
			if !emittedDirs[dirKey] {
				emittedDirs[dirKey] = true
				indent := strings.Repeat("  ", depth)
				fmt.Fprintf(&sb, "%s%s/\n", indent, parts[depth])
			}
		}

		indent := strings.Repeat("  ", len(parts)-1)
		fmt.Fprintf(&sb, "%s%s\n", indent, parts[len(parts)-1])
		fileLines++
	}

	return sb.String(), totalFiles
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
func invokeClaude(claudePath, systemPrompt, userMsg string, timeout time.Duration) (string, error) {
	result, err := invokeClaudeWithStats(claudePath, systemPrompt, userMsg, timeout, nil)
	if err != nil {
		return "", err
	}
	return result.Content, nil
}

// invokeClaudeWithStats runs the claude binary with stream-json output and returns token usage.
func invokeClaudeWithStats(claudePath, systemPrompt, userMsg string, timeout time.Duration, callback func(streaming.StreamEvent)) (*invokeClaudeResult, error) {
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

// planWorkOrder is the JSON deserialization type for a single work order from Claude's response.
// Separate from models.WorkOrder which uses yaml tags.
type planWorkOrder struct {
	Title              string   `json:"title"`
	Type               string   `json:"type"`
	TargetModule       string   `json:"target_module"`
	ReferenceModule    string   `json:"reference_module"`
	KnownFiles         []string `json:"known_files"`
	AcceptanceCriteria []string `json:"acceptance_criteria"`
	Constraints        []string `json:"constraints"`
}

// planResponse is the top-level JSON envelope returned by Claude.
type planResponse struct {
	WorkOrders []planWorkOrder `json:"work_orders"`
}

// auditPlanWorkOrder extends planWorkOrder with the audit_action field.
type auditPlanWorkOrder struct {
	planWorkOrder
	AuditAction string `json:"audit_action"`
}

type auditSummary struct {
	Added     int      `json:"added"`
	Modified  int      `json:"modified"`
	Unchanged int      `json:"unchanged"`
	Changes   []string `json:"changes"`
}

type auditResponse struct {
	WorkOrders   []auditPlanWorkOrder `json:"work_orders"`
	AuditSummary auditSummary         `json:"audit_summary"`
}

// parsePlanResponse extracts and validates work orders from Claude's raw response.
func parsePlanResponse(raw string) ([]models.WorkOrder, error) {
	cleaned := llm.CleanLLMResponse(raw)

	var resp planResponse
	if err := json.Unmarshal([]byte(cleaned), &resp); err != nil {
		return nil, fmt.Errorf("JSON unmarshal failed: %w", err)
	}

	if len(resp.WorkOrders) == 0 {
		return nil, fmt.Errorf("response contained no work orders")
	}

	workOrders := make([]models.WorkOrder, len(resp.WorkOrders))
	for i, pw := range resp.WorkOrders {
		wo := models.WorkOrder{
			Title:              pw.Title,
			Type:               pw.Type,
			TargetModule:       pw.TargetModule,
			ReferenceModule:    pw.ReferenceModule,
			KnownFiles:         pw.KnownFiles,
			AcceptanceCriteria: pw.AcceptanceCriteria,
			Constraints:        pw.Constraints,
		}
		if err := wo.Validate(); err != nil {
			return nil, fmt.Errorf("work order %d (%q): %w", i+1, pw.Title, err)
		}
		workOrders[i] = wo
	}

	return workOrders, nil
}

// auditWorkOrders runs a second LLM pass to audit generated work orders against the spec.
func auditWorkOrders(claudePath string, spec string, workOrders []models.WorkOrder, cfg *config.ProjectConfig, timeout time.Duration) ([]models.WorkOrder, *auditSummary, *invokeClaudeResult, error) {
	woJSON, err := json.MarshalIndent(workOrders, "", "  ")
	if err != nil {
		return workOrders, nil, nil, fmt.Errorf("failed to marshal work orders: %w", err)
	}

	var sb strings.Builder
	sb.WriteString("=== SPECIFICATION ===\n")
	sb.WriteString(spec)
	sb.WriteString("\n")

	sb.WriteString("\n=== PROJECT CONTEXT ===\n")
	if convSection := planBuildConventions(cfg.Conventions); convSection != "" {
		sb.WriteString("\n=== PROJECT CONVENTIONS ===\n")
		sb.WriteString(convSection)
	}
	treeStr, totalFiles := planBuildFileTree(cfg)
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

	sb.WriteString("\n=== GENERATED WORK ORDERS ===\n")
	sb.Write(woJSON)
	sb.WriteString("\n")

	userMsg := sb.String()

	result, err := invokeClaudeWithStats(claudePath, templates.DefaultPlanAuditPrompt, userMsg, timeout, newPlanStreamObserver("audit"))
	if err != nil {
		return workOrders, nil, nil, fmt.Errorf("audit invocation failed: %w", err)
	}

	cleaned := llm.CleanLLMResponse(result.Content)
	var resp auditResponse
	if err := json.Unmarshal([]byte(cleaned), &resp); err != nil {
		slog.Warn("audit response parse failed, using unaudited work orders", "error", err)
		return workOrders, nil, result, nil
	}

	if len(resp.WorkOrders) == 0 {
		slog.Warn("audit response contained no work orders, using unaudited work orders")
		return workOrders, nil, result, nil
	}

	audited := make([]models.WorkOrder, len(resp.WorkOrders))
	for i, aw := range resp.WorkOrders {
		var auditSource string
		if aw.AuditAction == "added" || aw.AuditAction == "modified" {
			auditSource = aw.AuditAction
		}

		wo := models.WorkOrder{
			Title:              aw.Title,
			Type:               aw.Type,
			TargetModule:       aw.TargetModule,
			ReferenceModule:    aw.ReferenceModule,
			KnownFiles:         aw.KnownFiles,
			AcceptanceCriteria: aw.AcceptanceCriteria,
			Constraints:        aw.Constraints,
			AuditSource:        auditSource,
		}
		if err := wo.Validate(); err != nil {
			slog.Warn("audited work order failed validation, using unaudited work orders",
				"index", i+1, "title", aw.Title, "error", err)
			return workOrders, nil, result, nil
		}
		audited[i] = wo
	}

	return audited, &resp.AuditSummary, result, nil
}

// orderedWorkOrder controls YAML field order for output files.
// The field order matches the canonical work order format:
// title, type, target_module, reference_module, known_files, acceptance_criteria, constraints.
type orderedWorkOrder struct {
	Title              string   `yaml:"title"`
	Type               string   `yaml:"type"`
	TargetModule       string   `yaml:"target_module"`
	ReferenceModule    string   `yaml:"reference_module,omitempty"`
	KnownFiles         []string `yaml:"known_files,omitempty"`
	AcceptanceCriteria []string `yaml:"acceptance_criteria,omitempty"`
	Constraints        []string `yaml:"constraints,omitempty"`
	AuditSource        string   `yaml:"audit_source,omitempty"`
}

// writeWorkOrderFiles writes each work order to a numbered YAML file.
func writeWorkOrderFiles(workOrders []models.WorkOrder, outputDir string) ([]string, error) {
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create output directory %q: %w", outputDir, err)
	}

	paths := make([]string, 0, len(workOrders))
	for i, wo := range workOrders {
		ordered := orderedWorkOrder{
			Title:              wo.Title,
			Type:               wo.Type,
			TargetModule:       wo.TargetModule,
			ReferenceModule:    wo.ReferenceModule,
			KnownFiles:         wo.KnownFiles,
			AcceptanceCriteria: wo.AcceptanceCriteria,
			Constraints:        wo.Constraints,
			AuditSource:        wo.AuditSource,
		}

		data, err := yaml.Marshal(ordered)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal work order %d: %w", i+1, err)
		}

		slug := slugify(wo.Title)
		filename := fmt.Sprintf("%03d-%s.yaml", i+1, slug)
		path := filepath.Join(outputDir, filename)

		if err := os.WriteFile(path, data, 0644); err != nil {
			return nil, fmt.Errorf("failed to write %s: %w", path, err)
		}
		slog.Debug("wrote work order file", "path", path)
		paths = append(paths, path)
	}

	return paths, nil
}

func writePlanSessionArtifact(dataDir, sessionID, filename string, content []byte) (string, error) {
	artifactDir := filepath.Join(dataDir, "artifacts", "plans", sessionID)
	if err := os.MkdirAll(artifactDir, 0755); err != nil {
		return "", fmt.Errorf("create plan artifact dir: %w", err)
	}

	path := filepath.Join(artifactDir, filename)
	if err := os.WriteFile(path, content, 0644); err != nil {
		return "", fmt.Errorf("write plan artifact: %w", err)
	}

	return path, nil
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
