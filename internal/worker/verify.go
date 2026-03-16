package worker

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/ponchione/agent-conductor/internal/config"
	"github.com/ponchione/agent-conductor/internal/database"
	pipelineerrors "github.com/ponchione/agent-conductor/internal/errors"
	"github.com/ponchione/agent-conductor/internal/models"
	"github.com/ponchione/agent-conductor/internal/queue"
	"github.com/ponchione/agent-conductor/internal/scope"
	"github.com/ponchione/agent-conductor/internal/verify"
)

const defaultPrecheckTimeoutSeconds = 300

var runVerifyCommand = func(ctx context.Context, dir string, env []string, argv []string) ([]byte, error) {
	if len(argv) == 0 {
		return nil, fmt.Errorf("command argv must not be empty")
	}
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Dir = dir
	cmd.Env = env
	return cmd.CombinedOutput()
}

func boolToInt(b bool) int64 {
	if b {
		return 1
	}
	return 0
}

func (w *Worker) runPreChecks(ctx context.Context, wo *models.WorkOrder) []verify.PreCheckResult {
	if wo != nil && wo.EffectiveSchemaVersion() >= 2 && len(wo.TypedAcceptanceCriteria) > 0 {
		return w.runTypedPreChecks(ctx, wo.TypedAcceptanceCriteria)
	}
	if wo == nil {
		return nil
	}
	return w.runLegacyPreChecks(ctx, wo.AcceptanceCriteria)
}

func (w *Worker) runTypedPreChecks(ctx context.Context, criteria []models.TypedAcceptanceCriterion) []verify.PreCheckResult {
	var results []verify.PreCheckResult
	for _, criterion := range criteria {
		switch strings.TrimSpace(criterion.Verification.Kind) {
		case "precheck":
			results = append(results, w.runConfiguredPreCheck(ctx, criterion))
		case "http_smoke":
			results = append(results, w.runConfiguredSmokeCheck(ctx, criterion))
		}
	}
	return results
}

func (w *Worker) runLegacyPreChecks(ctx context.Context, criteria []string) []verify.PreCheckResult {
	var results []verify.PreCheckResult
	for _, criterion := range criteria {
		argv, ok := legacyPrecheckArgv(criterion)
		if !ok {
			continue
		}
		required := true
		results = append(results, w.executePreCheckCommand(
			ctx,
			verify.PreCheckResult{
				Criterion:        criterion,
				Required:         &required,
				VerificationKind: "legacy",
			},
			"",
			config.VerifyCommand{Argv: argv},
			false,
		))
	}
	return results
}

func legacyPrecheckArgv(criterion string) ([]string, bool) {
	lower := strings.ToLower(criterion)
	switch {
	case strings.Contains(lower, "go test"):
		return []string{"go", "test", "./..."}, true
	case strings.Contains(lower, "go build"):
		return []string{"go", "build", "./..."}, true
	case strings.Contains(lower, "go vet"):
		return []string{"go", "vet", "./..."}, true
	default:
		return nil, false
	}
}

func (w *Worker) runConfiguredPreCheck(ctx context.Context, criterion models.TypedAcceptanceCriterion) verify.PreCheckResult {
	checkName := strings.TrimSpace(criterion.Verification.Check)
	if w.cfg == nil {
		return verify.PreCheckResult{
			CriterionID:      criterion.ID,
			Criterion:        criterion.Description,
			Required:         criterion.Required,
			Result:           models.CriterionResultUnassessable,
			VerificationKind: criterion.Verification.Kind,
			Notes:            fmt.Sprintf("precheck %q cannot run because worker config is unavailable", checkName),
		}
	}
	command, ok := w.cfg.Verify.Commands[checkName]
	if !ok {
		return verify.PreCheckResult{
			CriterionID:      criterion.ID,
			Criterion:        criterion.Description,
			Required:         criterion.Required,
			Result:           models.CriterionResultUnassessable,
			VerificationKind: criterion.Verification.Kind,
			Notes:            fmt.Sprintf("precheck %q is not configured under verify.commands", checkName),
		}
	}
	if len(command.Argv) == 0 {
		return verify.PreCheckResult{
			CriterionID:      criterion.ID,
			Criterion:        criterion.Description,
			Required:         criterion.Required,
			Result:           models.CriterionResultUnassessable,
			VerificationKind: criterion.Verification.Kind,
			Notes:            fmt.Sprintf("precheck %q has empty argv in verify.commands", checkName),
		}
	}
	return w.executePreCheckCommand(
		ctx,
		verify.PreCheckResult{
			CriterionID:      criterion.ID,
			Criterion:        criterion.Description,
			Required:         criterion.Required,
			VerificationKind: criterion.Verification.Kind,
		},
		checkName,
		command,
		true,
	)
}

func (w *Worker) runConfiguredSmokeCheck(ctx context.Context, criterion models.TypedAcceptanceCriterion) verify.PreCheckResult {
	routeName := strings.TrimSpace(criterion.Verification.Route)
	baseResult := verify.PreCheckResult{
		CriterionID:      criterion.ID,
		Criterion:        criterion.Description,
		Required:         criterion.Required,
		VerificationKind: criterion.Verification.Kind,
	}
	if w.cfg == nil {
		baseResult.Result = models.CriterionResultUnassessable
		baseResult.Notes = fmt.Sprintf("http_smoke %q cannot run because worker config is unavailable", routeName)
		return baseResult
	}
	smoke, ok := w.cfg.Verify.Smoke[routeName]
	if !ok {
		baseResult.Result = models.CriterionResultUnassessable
		baseResult.Notes = fmt.Sprintf("http_smoke %q is not configured under verify.smoke", routeName)
		return baseResult
	}
	if len(smoke.Command.Argv) == 0 {
		baseResult.Result = models.CriterionResultUnassessable
		baseResult.Notes = fmt.Sprintf("http_smoke %q has empty argv in verify.smoke", routeName)
		return baseResult
	}
	return w.executeSmokeCommand(ctx, baseResult, routeName, smoke.Command)
}

func (w *Worker) executePreCheckCommand(
	ctx context.Context,
	result verify.PreCheckResult,
	checkName string,
	command config.VerifyCommand,
	configured bool,
) verify.PreCheckResult {
	commandCtx, cancel := context.WithTimeout(ctx, w.precheckTimeout(command.TimeoutSeconds))
	defer cancel()

	out, err := runVerifyCommand(commandCtx, w.precheckWorkdir(command.Workdir), os.Environ(), command.Argv)
	if err == nil {
		result.Result = models.CriterionResultMet
		if configured {
			result.Notes = fmt.Sprintf("precheck %q passed: %s", checkName, strings.Join(command.Argv, " "))
		} else {
			result.Notes = fmt.Sprintf("legacy precheck passed: %s", strings.Join(command.Argv, " "))
		}
		return result
	}

	outStr := strings.TrimSpace(string(out))
	if len(outStr) > 200 {
		outStr = outStr[:200] + "..."
	}
	if outStr == "" {
		outStr = err.Error()
	}

	result.Result = models.CriterionResultUnmet
	if configured {
		result.Notes = fmt.Sprintf("precheck %q failed: %s (%s)", checkName, strings.Join(command.Argv, " "), outStr)
		return result
	}
	result.Notes = fmt.Sprintf("legacy precheck failed: %s (%s)", strings.Join(command.Argv, " "), outStr)
	return result
}

func (w *Worker) executeSmokeCommand(
	ctx context.Context,
	result verify.PreCheckResult,
	routeName string,
	command config.VerifyCommand,
) verify.PreCheckResult {
	commandCtx, cancel := context.WithTimeout(ctx, w.precheckTimeout(command.TimeoutSeconds))
	defer cancel()

	out, err := runVerifyCommand(commandCtx, w.precheckWorkdir(command.Workdir), os.Environ(), command.Argv)
	if err == nil {
		result.Result = models.CriterionResultMet
		result.Notes = fmt.Sprintf("http_smoke %q passed: %s", routeName, strings.Join(command.Argv, " "))
		return result
	}

	outStr := strings.TrimSpace(string(out))
	if len(outStr) > 200 {
		outStr = outStr[:200] + "..."
	}
	if outStr == "" {
		outStr = err.Error()
	}
	result.Result = models.CriterionResultUnmet
	result.Notes = fmt.Sprintf("http_smoke %q failed: %s (%s)", routeName, strings.Join(command.Argv, " "), outStr)
	return result
}

func (w *Worker) precheckWorkdir(workdir string) string {
	projectPath := "."
	if w.cfg != nil && strings.TrimSpace(w.cfg.Project.Path) != "" {
		projectPath = w.cfg.Project.Path
	}
	if strings.TrimSpace(workdir) == "" || workdir == "." {
		return projectPath
	}
	if filepath.IsAbs(workdir) {
		return filepath.Clean(workdir)
	}
	return filepath.Join(projectPath, workdir)
}

func (w *Worker) precheckTimeout(timeoutSeconds int) time.Duration {
	if timeoutSeconds > 0 {
		return time.Duration(timeoutSeconds) * time.Second
	}
	if w.cfg != nil && w.cfg.Guardrails.PhaseTimeoutSeconds > 0 {
		return time.Duration(w.cfg.Guardrails.PhaseTimeoutSeconds) * time.Second
	}
	return defaultPrecheckTimeoutSeconds * time.Second
}

func (w *Worker) runVerify(ctx context.Context, task *database.Task) error {
	slog.Info("Starting Verify Phase", "task", task.ID)
	w.db.LogEvent(task.WorkflowID, task.ID, "verify_started", nil)

	verifyStartedAt := time.Now()

	wf, err := w.db.GetWorkflow(ctx, task.WorkflowID)
	if err != nil {
		return pipelineerrors.Fatalf("verify", task.WorkflowID, task.ID, "workflow missing: %w", err)
	}

	if err := w.git.CheckoutBranch(w.cfg.Project.Path, wf.GitBranch); err != nil {
		return pipelineerrors.Fatalf("verify", task.WorkflowID, task.ID,
			"failed to checkout branch %s for verify: %w", wf.GitBranch, err)
	}

	baseBranch := w.cfg.Git.BaseBranch
	diff, err := w.git.GetWorktreeDiffAgainstBase(w.cfg.Project.Path, baseBranch)
	if err != nil {
		return pipelineerrors.Fatalf("verify", task.WorkflowID, task.ID, "git diff failed: %w", err)
	}

	if diff == "" {
		slog.Warn("No changes detected in verify phase")
		diff = "(No changes)"
	}

	woContent, err := os.ReadFile(wf.OriginalFile)
	if err != nil {
		return pipelineerrors.Fatalf("verify", task.WorkflowID, task.ID, "failed to read work order: %w", err)
	}

	var wo models.WorkOrder
	if err := yaml.Unmarshal(woContent, &wo); err != nil {
		slog.Warn("Could not parse work order YAML for pre-checks", "error", err)
	}

	preChecks := w.runPreChecks(ctx, &wo)

	allPreFailed := len(preChecks) > 0
	for _, r := range preChecks {
		if r.NormalizedResult() != models.CriterionResultUnmet {
			allPreFailed = false
			break
		}
	}

	var report *models.VerificationReport
	var records []scope.SubCallRecord
	buildEvidence := w.loadBuildValidationEvidence(ctx, task.WorkflowID)

	if allPreFailed {
		slog.Warn("All pre-checked criteria failed; skipping LLM evaluation", "task", task.ID)
		report = verify.BuildPrecheckOnlyReport(
			&wo,
			preChecks,
			"All pre-checked criteria failed. Skipping LLM evaluation.",
		)
	} else {
		orch := verify.NewVerifyOrchestrator(
			w.models, w.cfg, w.guardrails,
			verify.VerifyPrompts{
				Analyze:    w.prompts.VerifyAnalyze,
				Synthesize: w.prompts.VerifySynthesize,
			},
		)

		var orchErr error
		report, records, orchErr = orch.Execute(ctx, &wo, diff, preChecks, buildEvidence)
		if orchErr != nil {
			return pipelineerrors.Retryablef("verify", task.WorkflowID, task.ID,
				"verify orchestrator failed: %w", orchErr)
		}
	}

	agg := aggregateTokens(records, "synthesize")

	if err := w.db.UpdatePipelineRunVerify(ctx, database.UpdatePipelineRunVerifyParams{
		WorkflowID:        task.WorkflowID,
		VerifyStartedAt:   sql.NullString{String: verifyStartedAt.UTC().Format(time.RFC3339), Valid: true},
		VerifyCompletedAt: sql.NullString{String: time.Now().UTC().Format(time.RFC3339), Valid: true},
		VerifyTokensIn:    sql.NullInt64{Int64: int64(agg.tokensIn), Valid: true},
		VerifyTokensOut:   sql.NullInt64{Int64: int64(agg.tokensOut), Valid: true},
		VerifyModel:       sql.NullString{String: agg.model, Valid: agg.model != ""},
		VerifyResult:      sql.NullString{String: report.Status, Valid: true},
		BuildScopeDrift:   boolToInt(report.ScopeDrift.Detected),
	}); err != nil {
		slog.Warn("Failed to update pipeline run verify metrics", "error", err)
	}

	w.persistSubCalls(ctx, task.WorkflowID, records)

	dataDir := w.cfg.Project.DataDir
	reportPath, err := writeVerifyReport(report, dataDir, task.WorkflowID)
	if err != nil {
		return pipelineerrors.Fatalf("verify", task.WorkflowID, task.ID, "%s", err)
	}
	w.registerWorkflowArtifact(ctx, task.WorkflowID, task.ID, database.ArtifactTypeVerifyReport, reportPath, map[string]any{
		"phase":  "verify",
		"status": report.Status,
	})

	if err := w.db.SetWorkflowVerificationAndTransition(ctx, task.WorkflowID, sql.NullString{String: reportPath, Valid: true}); err != nil {
		return pipelineerrors.Fatalf("verify", task.WorkflowID, task.ID, "failed to update workflow verification: %w", err)
	}

	if err := w.db.UpdateWorkflowBudget(ctx, database.UpdateWorkflowBudgetParams{
		CurrentDepth: wf.CurrentDepth + 1,
		FilesChanged: wf.FilesChanged,
		ID:           task.WorkflowID,
	}); err != nil {
		return pipelineerrors.Fatalf("verify", task.WorkflowID, task.ID,
			"failed to update workflow budget: %w", err)
	}

	if allPreFailed {
		return pipelineerrors.NeedsHumanf("verify", task.WorkflowID, task.ID,
			"all pre-checks failed; skipping LLM evaluation")
	}

	w.db.LogEvent(task.WorkflowID, task.ID, "verify_completed", map[string]any{
		"status":           report.Status,
		"scope_drift":      report.ScopeDrift.Detected,
		"all_criteria_met": report.Completeness.AllCriteriaMet,
	})

	slog.Info("Verify phase complete",
		"status", report.Status,
		"summary", report.Summary,
	)
	slog.Info("Workflow paused for human review",
		"workflow", task.WorkflowID,
		"hint", "run 'conductor approve/reject "+task.WorkflowID+"'",
	)

	return w.q.CompleteTask(task.ID, &queue.TaskResult{
		ExitCode:  0,
		StdoutLog: fmt.Sprintf("Verification Status: %s", report.Status),
	})
}

func (w *Worker) loadBuildValidationEvidence(ctx context.Context, workflowID string) *verify.ValidationEvidence {
	artifact, err := w.db.GetLatestArtifactByType(ctx, database.ArtifactTypeBuildValidationEvidence, "", workflowID)
	if err != nil {
		return nil
	}
	evidence, err := verify.LoadValidationEvidence(artifact.Path)
	if err != nil {
		slog.Warn("Failed to load build validation evidence", "path", artifact.Path, "error", err)
		return nil
	}
	return evidence
}

// writeVerifyReport serializes and writes the verification report to disk.
func writeVerifyReport(report *models.VerificationReport, dataDir, workflowID string) (string, error) {
	reportDir := filepath.Join(dataDir, "artifacts", "verify-reports")
	if err := os.MkdirAll(reportDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create verify-reports dir: %w", err)
	}
	reportPath := filepath.Join(reportDir, workflowID+"-verify-report.json")
	reportData, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return "", fmt.Errorf("failed to marshal verification report: %w", err)
	}
	if err := os.WriteFile(reportPath, reportData, 0644); err != nil {
		return "", fmt.Errorf("failed to write verification report: %w", err)
	}
	return reportPath, nil
}
