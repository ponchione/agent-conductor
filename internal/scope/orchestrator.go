package scope

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	appcontext "github.com/ponchione/agent-conductor/internal/context"

	"github.com/ponchione/agent-conductor/internal/config"
	"github.com/ponchione/agent-conductor/internal/llm"
	"github.com/ponchione/agent-conductor/internal/models"
)

// ContextGatherer abstracts context.Assembler for testability.
type ContextGatherer interface {
	GatherPreScope(ctx context.Context, wo *models.WorkOrder) (*appcontext.PreScopeBundle, error)
	GatherForTarget(ctx context.Context, wo *models.WorkOrder, target appcontext.Target) (*appcontext.TargetBundle, error)
}

// ScopePrompts holds the system prompts for each pipeline step.
type ScopePrompts struct {
	Decompose  string
	Analyze    string
	Crosscut   string
	Synthesize string
}

// ScopeOrchestrator runs the recursive scope pipeline:
// PreScope → Decompose → Gather → Analyze → CrossCut → Synthesize.
type ScopeOrchestrator struct {
	models     *llm.RoleResolver
	assembler  ContextGatherer
	cfg        *config.ProjectConfig
	guardrails *config.Guardrails
	prompts    ScopePrompts
}

// NewScopeOrchestrator creates a new orchestrator for the recursive scope pipeline.
func NewScopeOrchestrator(
	models *llm.RoleResolver,
	assembler ContextGatherer,
	cfg *config.ProjectConfig,
	guardrails *config.Guardrails,
	prompts ScopePrompts,
) *ScopeOrchestrator {
	return &ScopeOrchestrator{
		models:     models,
		assembler:  assembler,
		cfg:        cfg,
		guardrails: guardrails,
		prompts:    prompts,
	}
}

// Execute runs the full recursive scope pipeline and returns a ContextPackage.
func (o *ScopeOrchestrator) Execute(ctx context.Context, wo *models.WorkOrder) (*models.ContextPackage, []SubCallRecord, error) {
	timeout := time.Duration(o.guardrails.PhaseTimeoutSeconds) * time.Second
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	var records []SubCallRecord

	// Step 0 — PreScope (Go, no LLM)
	preScope, err := o.assembler.GatherPreScope(ctx, wo)
	if err != nil {
		return nil, records, fmt.Errorf("prescope: %w", err)
	}

	// Step 1 — Decompose (1 LLM call)
	targets, err := o.stepDecompose(ctx, wo, preScope, &records)
	if err != nil {
		return nil, records, fmt.Errorf("decompose: %w", err)
	}

	// Step 2 — Gather (Go, no LLM)
	bundles := o.stepGather(ctx, wo, targets)

	// Step 3 — Analyze (N LLM calls)
	analyses, rawAnalyses, err := o.stepAnalyze(ctx, wo, targets, bundles, &records)
	if err != nil {
		return nil, records, fmt.Errorf("analyze: %w", err)
	}
	_ = analyses // used for control flow checks only; raw JSON goes downstream

	// Step 4 — CrossCut (1 LLM call, non-fatal)
	rawCrosscut := o.stepCrosscut(ctx, wo, rawAnalyses, &records)

	// Step 5 — Synthesize (1 LLM call, 1 retry)
	pkg, err := o.stepSynthesize(ctx, wo, preScope, rawAnalyses, rawCrosscut, &records)
	if err != nil {
		return nil, records, fmt.Errorf("synthesize: %w", err)
	}

	return pkg, records, nil
}

// stepDecompose calls the LLM to break the work order into investigation targets.
func (o *ScopeOrchestrator) stepDecompose(
	ctx context.Context,
	wo *models.WorkOrder,
	preScope *appcontext.PreScopeBundle,
	records *[]SubCallRecord,
) ([]InvestigationTarget, error) {
	userMsg := buildDecomposeUserMessage(wo, preScope)

	raw, err := o.callLLM(ctx, "decompose", o.prompts.Decompose, userMsg, "scope", "decompose", "", records)
	if err != nil {
		return nil, err
	}

	cleaned := cleanJSONArray(raw)
	var targets []InvestigationTarget
	if err := json.Unmarshal([]byte(cleaned), &targets); err != nil {
		return nil, fmt.Errorf("parse decompose response: %w", err)
	}

	if len(targets) == 0 {
		return nil, fmt.Errorf("decompose returned zero investigation targets")
	}

	max := o.guardrails.MaxInvestigationTargets
	if max > 0 && len(targets) > max {
		slog.Warn("truncating investigation targets", "from", len(targets), "to", max)
		targets = targets[:max]
	}

	return targets, nil
}

// stepGather collects context bundles for each target. Errors are logged and skipped.
func (o *ScopeOrchestrator) stepGather(
	ctx context.Context,
	wo *models.WorkOrder,
	targets []InvestigationTarget,
) []*appcontext.TargetBundle {
	bundles := make([]*appcontext.TargetBundle, len(targets))
	for i, t := range targets {
		bundle, err := o.assembler.GatherForTarget(ctx, wo, appcontext.Target{
			Path:      t.Path,
			Rationale: t.Rationale,
		})
		if err != nil {
			slog.Warn("gather for target failed, using empty bundle", "target", t.Path, "error", err)
			bundle = &appcontext.TargetBundle{}
		}
		bundles[i] = bundle
	}
	return bundles
}

// stepAnalyze calls the LLM once per target and returns parsed analyses plus raw JSON strings.
func (o *ScopeOrchestrator) stepAnalyze(
	ctx context.Context,
	wo *models.WorkOrder,
	targets []InvestigationTarget,
	bundles []*appcontext.TargetBundle,
	records *[]SubCallRecord,
) ([]AreaAnalysis, []string, error) {
	analyses := make([]AreaAnalysis, len(targets))
	rawAnalyses := make([]string, len(targets))
	allFailed := true

	for i, t := range targets {
		userMsg := buildAnalyzeUserMessage(wo, t, bundles[i])
		raw, err := o.callLLM(ctx, "analyze", o.prompts.Analyze, userMsg, "scope", "analyze", t.Path, records)
		if err != nil {
			slog.Warn("analyze call failed", "target", t.Path, "error", err)
			analyses[i] = AreaAnalysis{
				Target:         t.Path,
				AnalysisFailed: true,
				FailureReason:  err.Error(),
			}
			rawAnalyses[i] = ""
			continue
		}

		rawAnalyses[i] = raw
		cleaned := llm.CleanLLMResponse(raw)
		if err := json.Unmarshal([]byte(cleaned), &analyses[i]); err != nil {
			slog.Warn("analyze parse failed", "target", t.Path, "error", err)
			analyses[i] = AreaAnalysis{
				Target:         t.Path,
				AnalysisFailed: true,
				FailureReason:  fmt.Sprintf("parse: %v", err),
			}
			continue
		}

		allFailed = false
	}

	if allFailed {
		return nil, nil, fmt.Errorf("all %d analyze calls failed", len(targets))
	}

	return analyses, rawAnalyses, nil
}

// stepCrosscut runs the crosscut analysis. Non-fatal: returns empty string on error.
func (o *ScopeOrchestrator) stepCrosscut(
	ctx context.Context,
	wo *models.WorkOrder,
	rawAnalyses []string,
	records *[]SubCallRecord,
) string {
	userMsg := buildCrosscutUserMessage(wo, rawAnalyses)
	raw, err := o.callLLM(ctx, "crosscut", o.prompts.Crosscut, userMsg, "scope", "crosscut", "", records)
	if err != nil {
		slog.Warn("crosscut call failed, proceeding without", "error", err)
		return ""
	}
	return raw
}

// stepSynthesize merges all analyses into a final ContextPackage. Retries once on failure.
func (o *ScopeOrchestrator) stepSynthesize(
	ctx context.Context,
	wo *models.WorkOrder,
	preScope *appcontext.PreScopeBundle,
	rawAnalyses []string,
	rawCrosscut string,
	records *[]SubCallRecord,
) (*models.ContextPackage, error) {
	userMsg := buildSynthesizeUserMessage(wo, preScope, rawAnalyses, rawCrosscut)

	for attempt := range 2 {
		raw, err := o.callLLM(ctx, "synthesize", o.prompts.Synthesize, userMsg, "scope", "synthesize", "", records)
		if err != nil {
			slog.Warn("synthesize call failed", "attempt", attempt+1, "error", err)
			continue
		}

		cleaned := llm.CleanLLMResponse(raw)
		var pkg models.ContextPackage
		if err := json.Unmarshal([]byte(cleaned), &pkg); err != nil {
			slog.Warn("synthesize parse failed", "attempt", attempt+1, "error", err)
			continue
		}

		return &pkg, nil
	}

	return nil, fmt.Errorf("synthesize failed after 2 attempts")
}

// callLLM resolves a client by role, makes the completion call, and records metrics.
func (o *ScopeOrchestrator) callLLM(
	ctx context.Context,
	role string,
	systemPrompt string,
	userMessage string,
	phase string,
	step string,
	targetPath string,
	records *[]SubCallRecord,
) (string, error) {
	client, err := o.models.ForRole(role)
	if err != nil {
		rec := SubCallRecord{
			Phase:        phase,
			Step:         step,
			TargetPath:   targetPath,
			Success:      false,
			ErrorMessage: err.Error(),
		}
		*records = append(*records, rec)
		return "", err
	}

	result, err := client.Complete(ctx, systemPrompt, userMessage)
	if err != nil {
		rec := SubCallRecord{
			Phase:        phase,
			Step:         step,
			TargetPath:   targetPath,
			Success:      false,
			ErrorMessage: err.Error(),
		}
		*records = append(*records, rec)
		return "", err
	}

	rec := SubCallRecord{
		Phase:            phase,
		Step:             step,
		TargetPath:       targetPath,
		Provider:         result.Provider,
		Model:            result.Model,
		TokensIn:         result.TokensIn,
		TokensOut:        result.TokensOut,
		LatencyMs:        result.LatencyMs,
		EstimatedCostUSD: o.estimateCost(result.Provider, result.TokensIn, result.TokensOut),
		Success:          true,
	}
	*records = append(*records, rec)

	return result.Content, nil
}

// estimateCost calculates estimated USD cost based on provider pricing config.
func (o *ScopeOrchestrator) estimateCost(providerEndpoint string, tokensIn, tokensOut int) float64 {
	for _, p := range o.cfg.Models.Providers {
		if strings.TrimRight(p.Endpoint, "/") == strings.TrimRight(providerEndpoint, "/") {
			if p.Pricing != nil {
				return (float64(tokensIn)*p.Pricing.InputPerMillion +
					float64(tokensOut)*p.Pricing.OutputPerMillion) / 1_000_000
			}
			return 0
		}
	}
	return 0
}

// cleanJSONArray extracts the outermost JSON array from a raw LLM response.
func cleanJSONArray(raw string) string {
	start := strings.Index(raw, "[")
	end := strings.LastIndex(raw, "]")
	if start == -1 || end == -1 || end < start {
		return strings.TrimSpace(raw)
	}
	return raw[start : end+1]
}

// --- User message builders ---

// buildWorkOrderSection formats the work order fields shared across prompts.
func buildWorkOrderSection(wo *models.WorkOrder) string {
	var sb strings.Builder
	sb.WriteString("=== WORK ORDER ===\n")
	fmt.Fprintf(&sb, "Title: %s\n", wo.Title)
	fmt.Fprintf(&sb, "Target module: %s\n", wo.TargetModule)
	if wo.ReferenceModule != "" {
		fmt.Fprintf(&sb, "Reference module: %s\n", wo.ReferenceModule)
	}
	fmt.Fprintf(&sb, "Type: %s\n", wo.Type)
	if len(wo.AcceptanceCriteria) > 0 {
		sb.WriteString("\nAcceptance criteria:\n")
		for _, ac := range wo.AcceptanceCriteria {
			fmt.Fprintf(&sb, "  - %s\n", ac)
		}
	}
	if len(wo.Constraints) > 0 {
		sb.WriteString("\nConstraints:\n")
		for _, c := range wo.Constraints {
			fmt.Fprintf(&sb, "  - %s\n", c)
		}
	}
	if len(wo.KnownFiles) > 0 {
		sb.WriteString("\nKnown files:\n")
		for _, kf := range wo.KnownFiles {
			fmt.Fprintf(&sb, "  - %s\n", kf)
		}
	}
	sb.WriteString("\n")
	return sb.String()
}

// buildDecomposeUserMessage creates the user message for the decompose step.
func buildDecomposeUserMessage(wo *models.WorkOrder, preScope *appcontext.PreScopeBundle) string {
	var sb strings.Builder

	sb.WriteString(buildWorkOrderSection(wo))

	if preScope.Conventions != "" {
		sb.WriteString("=== PROJECT CONVENTIONS ===\n")
		sb.WriteString(preScope.Conventions)
		sb.WriteString("\n")
	}

	sb.WriteString("=== PROJECT FILE TREE ===\n")
	if preScope.FileTree != "" {
		sb.WriteString(preScope.FileTree)
	} else {
		sb.WriteString("(no matching files)\n")
	}
	sb.WriteString("\n")

	return sb.String()
}

// buildAnalyzeUserMessage creates the user message for a single analyze call.
func buildAnalyzeUserMessage(wo *models.WorkOrder, target InvestigationTarget, bundle *appcontext.TargetBundle) string {
	var sb strings.Builder

	sb.WriteString(buildWorkOrderSection(wo))

	sb.WriteString("=== INVESTIGATION TARGET ===\n")
	fmt.Fprintf(&sb, "Path: %s\n", target.Path)
	fmt.Fprintf(&sb, "Rationale: %s\n", target.Rationale)
	fmt.Fprintf(&sb, "Investigation type: %s\n\n", target.InvestigationType)

	if len(bundle.Files) > 0 {
		sb.WriteString("=== TARGET FILES ===\n")
		for _, f := range bundle.Files {
			fmt.Fprintf(&sb, "--- %s ---\n", f.Path)
			sb.WriteString(f.Content)
			sb.WriteString("\n")
		}
		sb.WriteString("\n")
	}

	if len(bundle.Signatures) > 0 {
		sb.WriteString("=== FUNCTION SIGNATURES ===\n")
		for _, sig := range bundle.Signatures {
			fmt.Fprintf(&sb, "  %s\n", sig)
		}
		sb.WriteString("\n")
	}

	if len(bundle.RAGChunks) > 0 {
		sb.WriteString("=== RELEVANT CODE (RAG) ===\n")
		for _, chunk := range bundle.RAGChunks {
			fmt.Fprintf(&sb, "  %s — %s\n    %s\n", chunk.File, chunk.Function, chunk.Description)
		}
		sb.WriteString("\n")
	}

	return sb.String()
}

// buildCrosscutUserMessage creates the user message for the crosscut step.
func buildCrosscutUserMessage(wo *models.WorkOrder, rawAnalyses []string) string {
	var sb strings.Builder

	sb.WriteString(buildWorkOrderSection(wo))

	sb.WriteString("=== AREA ANALYSES ===\n")
	for i, raw := range rawAnalyses {
		if raw == "" {
			continue
		}
		fmt.Fprintf(&sb, "--- Analysis %d ---\n", i+1)
		sb.WriteString(raw)
		sb.WriteString("\n\n")
	}

	return sb.String()
}

// buildSynthesizeUserMessage creates the user message for the synthesize step.
func buildSynthesizeUserMessage(
	wo *models.WorkOrder,
	preScope *appcontext.PreScopeBundle,
	rawAnalyses []string,
	rawCrosscut string,
) string {
	var sb strings.Builder

	sb.WriteString(buildWorkOrderSection(wo))

	if preScope.Conventions != "" {
		sb.WriteString("=== PROJECT CONVENTIONS ===\n")
		sb.WriteString(preScope.Conventions)
		sb.WriteString("\n")
	}

	sb.WriteString("=== AREA ANALYSES ===\n")
	for i, raw := range rawAnalyses {
		if raw == "" {
			continue
		}
		fmt.Fprintf(&sb, "--- Analysis %d ---\n", i+1)
		sb.WriteString(raw)
		sb.WriteString("\n\n")
	}

	if rawCrosscut != "" {
		sb.WriteString("=== CROSS-CUT ANALYSIS ===\n")
		sb.WriteString(rawCrosscut)
		sb.WriteString("\n\n")
	}

	return sb.String()
}
