package verify

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ponchione/agent-conductor/internal/config"
	"github.com/ponchione/agent-conductor/internal/llm"
	"github.com/ponchione/agent-conductor/internal/models"
	"github.com/ponchione/agent-conductor/internal/scope"
	"github.com/ponchione/agent-conductor/internal/streaming"
)

// VerifyPrompts holds the system prompts for each verify pipeline step.
type VerifyPrompts struct {
	Analyze    string
	Synthesize string
}

// VerifyOrchestrator runs the recursive verify pipeline:
// Segment → Analyze → Synthesize.
type VerifyOrchestrator struct {
	models     *llm.RoleResolver
	cfg        *config.ProjectConfig
	guardrails *config.Guardrails
	prompts    VerifyPrompts
}

// NewVerifyOrchestrator creates a new orchestrator for the recursive verify pipeline.
func NewVerifyOrchestrator(
	models *llm.RoleResolver,
	cfg *config.ProjectConfig,
	guardrails *config.Guardrails,
	prompts VerifyPrompts,
) *VerifyOrchestrator {
	return &VerifyOrchestrator{
		models:     models,
		cfg:        cfg,
		guardrails: guardrails,
		prompts:    prompts,
	}
}

// Execute runs the full recursive verify pipeline and returns a VerificationReport.
func (o *VerifyOrchestrator) Execute(
	ctx context.Context,
	wo *models.WorkOrder,
	diff string,
	preChecks []PreCheckResult,
	buildEvidence *ValidationEvidence,
) (*models.VerificationReport, []scope.SubCallRecord, error) {
	timeout := time.Duration(o.guardrails.PhaseTimeoutSeconds) * time.Second
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	var records []scope.SubCallRecord

	// Step 1 — Segment (Go, no LLM)
	segments, referenceResults, err := o.prepareSegments(wo, diff)
	if len(segments) == 0 {
		return nil, records, fmt.Errorf("diff produced 0 segments")
	}

	// Step 2 — Analyze (N LLM calls, role verify_analyze)
	rawVerdicts := o.stepAnalyze(ctx, wo, segments, &records)

	// Step 3 — Synthesize (1 LLM call, role verify_synthesize, 1 retry)
	report, err := o.stepSynthesize(ctx, wo, preChecks, buildEvidence, rawVerdicts, &records)
	if err != nil {
		return nil, records, fmt.Errorf("synthesize: %w", err)
	}
	if len(referenceResults) > 0 {
		report.Completeness.CriteriaResults = append(report.Completeness.CriteriaResults, referenceResults...)
		report = NormalizeVerificationReport(wo, report, nil)
	}

	return report, records, nil
}

// stepAnalyze calls the LLM once per segment. Individual failures do not abort.
func (o *VerifyOrchestrator) stepAnalyze(
	ctx context.Context,
	wo *models.WorkOrder,
	segments []DiffSegment,
	records *[]scope.SubCallRecord,
) []string {
	rawVerdicts := make([]string, len(segments))

	for i, seg := range segments {
		userMsg := buildAnalyzeUserMessage(wo, seg)
		targetPath := ""
		if len(seg.Files) > 0 {
			targetPath = seg.Files[0]
		}

		raw, err := o.callLLM(ctx, "verify_analyze", o.prompts.Analyze, userMsg, "verify", "analyze", targetPath, records)
		if err != nil {
			slog.Warn("verify analyze call failed", "segment", i, "error", err)
			verdict := SegmentVerdict{
				Files:          seg.Files,
				AnalysisFailed: true,
				FailureReason:  err.Error(),
			}
			data, mErr := json.Marshal(verdict)
			if mErr != nil {
				slog.Warn("failed to marshal segment verdict", "segment", i, "error", mErr)
			}
			rawVerdicts[i] = string(data)
			continue
		}

		rawVerdicts[i] = raw
	}

	return rawVerdicts
}

// stepSynthesize merges all segment verdicts into a final VerificationReport. Retries once on failure.
func (o *VerifyOrchestrator) stepSynthesize(
	ctx context.Context,
	wo *models.WorkOrder,
	preChecks []PreCheckResult,
	buildEvidence *ValidationEvidence,
	rawVerdicts []string,
	records *[]scope.SubCallRecord,
) (*models.VerificationReport, error) {
	userMsg := buildSynthesizeUserMessage(wo, preChecks, buildEvidence, rawVerdicts)

	for attempt := range 2 {
		raw, err := o.callLLM(ctx, "verify_synthesize", o.prompts.Synthesize, userMsg, "verify", "synthesize", "", records)
		if err != nil {
			slog.Warn("verify synthesize call failed", "attempt", attempt+1, "error", err)
			continue
		}

		cleaned := llm.CleanLLMResponse(raw)
		var report models.VerificationReport
		if err := json.Unmarshal([]byte(cleaned), &report); err != nil {
			slog.Warn("verify synthesize parse failed", "attempt", attempt+1, "error", err)
			continue
		}

		return NormalizeVerificationReport(wo, &report, preChecks), nil
	}

	return nil, fmt.Errorf("synthesize failed after 2 attempts")
}

// callLLM resolves a client by role, makes the completion call, and records metrics.
func (o *VerifyOrchestrator) callLLM(
	ctx context.Context,
	role string,
	systemPrompt string,
	userMessage string,
	phase string,
	step string,
	targetPath string,
	records *[]scope.SubCallRecord,
) (string, error) {
	client, err := o.models.ForRole(role)
	if err != nil {
		rec := scope.SubCallRecord{
			Phase:        phase,
			Step:         step,
			TargetPath:   targetPath,
			Success:      false,
			ErrorMessage: err.Error(),
		}
		*records = append(*records, rec)
		return "", err
	}

	var callback func(streaming.StreamEvent)
	if _, ok := client.(llm.EventStreamingClient); ok {
		fmt.Fprintf(os.Stdout, "\n[%s/%s", phase, step)
		if targetPath != "" {
			fmt.Fprintf(os.Stdout, " %s", targetPath)
		}
		fmt.Fprintln(os.Stdout, "]")
		callback = streaming.ConsoleCallback(os.Stdout)
	}

	result, err := llm.CompleteWithOptionalStreaming(ctx, client, systemPrompt, userMessage, callback)
	if err != nil {
		rec := scope.SubCallRecord{
			Phase:        phase,
			Step:         step,
			TargetPath:   targetPath,
			Success:      false,
			ErrorMessage: err.Error(),
		}
		*records = append(*records, rec)
		return "", err
	}

	rec := scope.SubCallRecord{
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
func (o *VerifyOrchestrator) estimateCost(providerEndpoint string, tokensIn, tokensOut int) float64 {
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

// segmentDiff splits a unified diff into logical DiffSegments, grouping
// related files (e.g. foo.go and foo_test.go) into a single segment.
func segmentDiff(diff string) []DiffSegment {
	if strings.TrimSpace(diff) == "" {
		return nil
	}

	// Split on "diff --git" boundaries.
	const marker = "diff --git "
	var chunks []string

	// Handle the case where diff starts with the marker.
	if strings.HasPrefix(diff, marker) {
		diff = "\n" + diff
	}
	parts := strings.Split(diff, "\n"+marker)
	for i, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed == "" {
			continue
		}
		if i > 0 {
			// Re-add the marker we split on.
			trimmed = marker + trimmed
		}
		chunks = append(chunks, trimmed)
	}

	// Parse each chunk into a per-file segment.
	type fileChunk struct {
		path string
		diff string
	}
	var fileChunks []fileChunk
	for _, chunk := range chunks {
		path := parseFilePath(chunk)
		fileChunks = append(fileChunks, fileChunk{path: path, diff: chunk})
	}

	// Grouping pass: merge foo_test.go with foo.go.
	type group struct {
		files []string
		diffs []string
	}
	var groups []group
	used := make(map[int]bool)

	for i := 0; i < len(fileChunks); i++ {
		if used[i] {
			continue
		}
		g := group{
			files: []string{fileChunks[i].path},
			diffs: []string{fileChunks[i].diff},
		}
		used[i] = true

		base := testFileBase(fileChunks[i].path)
		if base != "" {
			// Look for the counterpart.
			for j := 0; j < len(fileChunks); j++ {
				if used[j] {
					continue
				}
				otherBase := testFileBase(fileChunks[j].path)
				if otherBase == base {
					g.files = append(g.files, fileChunks[j].path)
					g.diffs = append(g.diffs, fileChunks[j].diff)
					used[j] = true
				}
			}
		}

		groups = append(groups, g)
	}

	segments := make([]DiffSegment, len(groups))
	for i, g := range groups {
		segments[i] = DiffSegment{
			Files: g.files,
			Diff:  strings.Join(g.diffs, "\n"),
		}
	}
	return segments
}

const maxReferenceFilesPerCriterion = 4

func (o *VerifyOrchestrator) prepareSegments(
	wo *models.WorkOrder,
	diff string,
) ([]DiffSegment, []models.CriterionResult, error) {
	segments := segmentDiff(diff)
	if wo == nil || len(wo.TypedAcceptanceCriteria) == 0 {
		return segments, nil, nil
	}

	byFile := make(map[string]int, len(segments))
	for i, segment := range segments {
		for _, file := range segment.Files {
			byFile[file] = i
		}
	}

	var referenceResults []models.CriterionResult
	for _, criterion := range wo.TypedAcceptanceCriteria {
		if strings.TrimSpace(criterion.Verification.Kind) != "file_compatibility" {
			continue
		}

		referenceFiles, err := o.loadCompatibilityReferenceFiles(wo, criterion.Verification.Subject)
		if err != nil {
			referenceResults = append(referenceResults, models.CriterionResult{
				CriterionID:      criterion.ID,
				Criterion:        criterion.Description,
				Required:         criterion.Required,
				Result:           models.CriterionResultUnassessable,
				VerificationKind: criterion.Verification.Kind,
				Notes:            err.Error(),
			})
			continue
		}

		subject := filepath.Clean(strings.TrimSpace(criterion.Verification.Subject))
		if idx, ok := byFile[subject]; ok {
			segments[idx].ReferenceFiles = mergeReferenceFiles(segments[idx].ReferenceFiles, referenceFiles)
			continue
		}

		segments = append(segments, DiffSegment{
			Files:          []string{subject},
			Diff:           "(No diff for declared compatibility subject. Assess compatibility using current repository contents and the reference files below.)",
			ReferenceFiles: referenceFiles,
		})
		byFile[subject] = len(segments) - 1
	}

	return segments, referenceResults, nil
}

func (o *VerifyOrchestrator) loadCompatibilityReferenceFiles(
	wo *models.WorkOrder,
	subject string,
) ([]models.FileWithContent, error) {
	subject = strings.TrimSpace(subject)
	if subject == "" {
		return nil, fmt.Errorf("file_compatibility criterion is missing verification.subject")
	}

	projectPath := "."
	if o.cfg != nil && strings.TrimSpace(o.cfg.Project.Path) != "" {
		projectPath = o.cfg.Project.Path
	}
	maxFileBytes := 32 * 1024
	maxTotalBytes := 128 * 1024
	if o.cfg != nil && o.cfg.Index.MaxFileSizeBytes > 0 {
		maxFileBytes = o.cfg.Index.MaxFileSizeBytes
	}
	if o.cfg != nil && o.cfg.Index.MaxTotalFileSizeBytes > 0 {
		maxTotalBytes = o.cfg.Index.MaxTotalFileSizeBytes
	}

	candidates := make([]string, 0, len(wo.KnownFiles)+1)
	candidates = append(candidates, subject)
	for _, knownFile := range wo.KnownFiles {
		if knownFile == subject {
			continue
		}
		candidates = append(candidates, knownFile)
	}

	totalBytes := 0
	results := make([]models.FileWithContent, 0, maxReferenceFilesPerCriterion)
	for _, candidate := range candidates {
		if len(results) >= maxReferenceFilesPerCriterion {
			break
		}
		content, size, err := readReferenceFile(projectPath, candidate, maxFileBytes)
		if err != nil {
			if candidate == subject {
				return nil, fmt.Errorf("file_compatibility subject %q is unavailable: %w", subject, err)
			}
			continue
		}
		if totalBytes+size > maxTotalBytes {
			if candidate == subject && len(results) == 0 {
				return nil, fmt.Errorf("file_compatibility subject %q exceeds reference-file byte limits", subject)
			}
			break
		}
		results = append(results, models.FileWithContent{
			Path:   candidate,
			Source: content,
		})
		totalBytes += size
	}

	if len(results) == 0 {
		return nil, fmt.Errorf("file_compatibility subject %q could not be loaded within configured limits", subject)
	}
	return results, nil
}

func readReferenceFile(projectPath, relativePath string, maxBytes int) (string, int, error) {
	cleaned := filepath.Clean(strings.TrimSpace(relativePath))
	if cleaned == "." || cleaned == "" {
		return "", 0, fmt.Errorf("empty path")
	}
	if filepath.IsAbs(cleaned) {
		return "", 0, fmt.Errorf("path must be relative")
	}
	absPath := filepath.Join(projectPath, cleaned)
	rel, err := filepath.Rel(projectPath, absPath)
	if err != nil || strings.HasPrefix(rel, "..") {
		return "", 0, fmt.Errorf("path escapes repository root")
	}
	info, err := os.Stat(absPath)
	if err != nil {
		return "", 0, err
	}
	if info.IsDir() {
		return "", 0, fmt.Errorf("path must reference a file")
	}
	if info.Size() > int64(maxBytes) {
		return "", 0, fmt.Errorf("file exceeds max size limit")
	}
	data, err := os.ReadFile(absPath)
	if err != nil {
		return "", 0, err
	}
	return string(data), len(data), nil
}

func mergeReferenceFiles(existing, incoming []models.FileWithContent) []models.FileWithContent {
	if len(incoming) == 0 {
		return existing
	}
	seen := make(map[string]bool, len(existing))
	for _, file := range existing {
		seen[file.Path] = true
	}
	merged := append([]models.FileWithContent{}, existing...)
	for _, file := range incoming {
		if seen[file.Path] {
			continue
		}
		merged = append(merged, file)
		seen[file.Path] = true
	}
	return merged
}

// parseFilePath extracts the b/ path from a "diff --git a/... b/..." line.
func parseFilePath(chunk string) string {
	firstLine, _, _ := strings.Cut(chunk, "\n")
	// Format: diff --git a/<path> b/<path>
	if _, after, ok := strings.Cut(firstLine, " b/"); ok {
		return after
	}
	return firstLine
}

// testFileBase returns the shared base for grouping a source file with its
// test counterpart. For "foo.go" and "foo_test.go" it returns "foo.go".
// Returns empty string if the file has no Go test relationship.
func testFileBase(path string) string {
	if !strings.HasSuffix(path, ".go") {
		return ""
	}
	if base, ok := strings.CutSuffix(path, "_test.go"); ok {
		return base + ".go"
	}
	return path
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
	if len(wo.TypedAcceptanceCriteria) > 0 {
		sb.WriteString("\nTyped acceptance criteria:\n")
		for _, criterion := range wo.TypedAcceptanceCriteria {
			required := "unknown"
			if criterion.Required != nil {
				if *criterion.Required {
					required = "required"
				} else {
					required = "advisory"
				}
			}
			fmt.Fprintf(
				&sb,
				"  - %s | %s | %s | kind=%s | requirements=%s\n",
				criterion.ID,
				criterion.Description,
				required,
				criterion.Verification.Kind,
				strings.Join(criterion.RequirementIDs, ", "),
			)
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

// buildAnalyzeUserMessage creates the user message for a single verify analyze call.
func buildAnalyzeUserMessage(wo *models.WorkOrder, segment DiffSegment) string {
	var sb strings.Builder

	sb.WriteString(buildWorkOrderSection(wo))

	sb.WriteString("=== SEGMENT DIFF ===\n")
	fmt.Fprintf(&sb, "Files: %s\n\n", strings.Join(segment.Files, ", "))
	sb.WriteString(segment.Diff)
	sb.WriteString("\n")
	if len(segment.ReferenceFiles) > 0 {
		sb.WriteString("\n=== REFERENCE FILES ===\n")
		for _, file := range segment.ReferenceFiles {
			fmt.Fprintf(&sb, "--- %s ---\n", file.Path)
			sb.WriteString(file.Source)
			if !strings.HasSuffix(file.Source, "\n") {
				sb.WriteString("\n")
			}
			sb.WriteString("\n")
		}
	}

	return sb.String()
}

// buildSynthesizeUserMessage creates the user message for the verify synthesize call.
func buildSynthesizeUserMessage(wo *models.WorkOrder, preChecks []PreCheckResult, buildEvidence *ValidationEvidence, rawVerdicts []string) string {
	var sb strings.Builder

	sb.WriteString(buildWorkOrderSection(wo))

	if buildEvidence != nil && (len(buildEvidence.Commands) > 0 || len(buildEvidence.SmokeChecks) > 0) {
		sb.WriteString("=== BUILD VALIDATION EVIDENCE ===\n")
		for _, entry := range buildEvidence.Commands {
			fmt.Fprintf(&sb, "[COMMAND] %s => %s (%s)\n", entry.Name, entry.Result, entry.Notes)
		}
		for _, entry := range buildEvidence.SmokeChecks {
			fmt.Fprintf(&sb, "[SMOKE] %s => %s (%s)\n", entry.Name, entry.Result, entry.Notes)
		}
		sb.WriteString("\n")
	}

	if len(preChecks) > 0 {
		sb.WriteString("=== PRE-CHECK RESULTS ===\n")
		for _, pc := range preChecks {
			tag := "[UNASSESSABLE]"
			switch pc.NormalizedResult() {
			case models.CriterionResultMet:
				tag = "[MET]"
			case models.CriterionResultUnmet:
				tag = "[UNMET]"
			}
			fmt.Fprintf(&sb, "%s %s (%s)\n", tag, pc.Criterion, pc.Notes)
		}
		sb.WriteString("\n")
	}

	sb.WriteString("=== SEGMENT VERDICTS ===\n")
	for i, raw := range rawVerdicts {
		fmt.Fprintf(&sb, "--- Verdict %d ---\n", i+1)
		sb.WriteString(raw)
		sb.WriteString("\n\n")
	}

	return sb.String()
}
