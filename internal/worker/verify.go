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

	"github.com/ponchione/agent-conductor/internal/database"
	pipelineerrors "github.com/ponchione/agent-conductor/internal/errors"
	"github.com/ponchione/agent-conductor/internal/llm"
	"github.com/ponchione/agent-conductor/internal/models"
	"github.com/ponchione/agent-conductor/internal/queue"
)

func boolToInt(b bool) int64 {
	if b {
		return 1
	}
	return 0
}

type criterionPreCheck struct {
	criterion string
	met       bool
	notes     string
}

func (w *Worker) runPreChecks(ctx context.Context, criteria []string) []criterionPreCheck {
	var results []criterionPreCheck
	for _, c := range criteria {
		lower := strings.ToLower(c)
		var args []string
		if strings.Contains(lower, "go test") {
			args = []string{"test", "./..."}
		} else if strings.Contains(lower, "go build") {
			args = []string{"build", "./..."}
		} else if strings.Contains(lower, "go vet") {
			args = []string{"vet", "./..."}
		} else {
			continue
		}
		cmd := exec.CommandContext(ctx, "go", args...)
		cmd.Dir = w.cfg.Project.Path
		cmd.Env = append(os.Environ(),
			"CGO_CFLAGS=-I"+filepath.Join(w.cfg.Project.Path, "include"),
			"CGO_LDFLAGS=-L"+filepath.Join(w.cfg.Project.Path, "lib/linux_amd64")+" -llancedb_go -lm -ldl -lpthread",
		)
		out, err := cmd.CombinedOutput()
		met := err == nil
		var notes string
		if met {
			notes = "verified: exit code 0"
		} else {
			outStr := strings.TrimSpace(string(out))
			if len(outStr) > 200 {
				outStr = outStr[:200] + "..."
			}
			notes = fmt.Sprintf("verified: exit code non-zero, error: %s", outStr)
		}
		results = append(results, criterionPreCheck{criterion: c, met: met, notes: notes})
	}
	return results
}

func (w *Worker) runVerify(ctx context.Context, task *database.Task) error {
	slog.Info("Starting Verify Phase", "task", task.ID)
	w.db.LogEvent(task.WorkflowID, task.ID, "verify_started", nil)

	verifyStartedAt := time.Now()
	var lastVerifyUsage llm.Usage

	wf, err := w.db.GetWorkflow(ctx, task.WorkflowID)
	if err != nil {
		return pipelineerrors.Fatalf("verify", task.WorkflowID, task.ID, "workflow missing: %w", err)
	}

	diff, err := w.git.GetDiff(w.cfg.Project.Path, "main", wf.GitBranch)
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

	cpPath := wf.ContextPackagePath.String
	if !wf.ContextPackagePath.Valid {
		return pipelineerrors.Fatalf("verify", task.WorkflowID, task.ID, "missing context package path")
	}
	cpContent, err := os.ReadFile(cpPath)
	if err != nil {
		return pipelineerrors.Fatalf("verify", task.WorkflowID, task.ID, "failed to read context package: %w", err)
	}

	var wo models.WorkOrder
	if err := yaml.Unmarshal(woContent, &wo); err != nil {
		slog.Warn("Could not parse work order YAML for pre-checks", "error", err)
	}

	preChecks := w.runPreChecks(ctx, wo.AcceptanceCriteria)

	preCheckedSet := make(map[string]bool, len(preChecks))
	for _, r := range preChecks {
		preCheckedSet[r.criterion] = true
	}
	var remainingCriteria []string
	for _, c := range wo.AcceptanceCriteria {
		if !preCheckedSet[c] {
			remainingCriteria = append(remainingCriteria, c)
		}
	}

	allPreFailed := len(preChecks) > 0
	for _, r := range preChecks {
		if r.met {
			allPreFailed = false
			break
		}
	}

	var report models.VerificationReport

	if allPreFailed {
		slog.Warn("All pre-checked criteria failed; skipping LLM evaluation", "task", task.ID)
		criteriaResults := make([]models.CriterionResult, len(preChecks))
		for i, r := range preChecks {
			criteriaResults[i] = models.CriterionResult{
				Criterion: r.criterion,
				Met:       r.met,
				Notes:     r.notes,
			}
		}
		report = models.VerificationReport{
			Status:  "FAIL",
			Summary: "All pre-checked criteria failed (go build/test/vet). Skipping LLM evaluation.",
			Completeness: models.Completeness{
				AllCriteriaMet:  false,
				CriteriaResults: criteriaResults,
			},
		}
	} else {
		promptContext := buildVerifyPrompt(woContent, cpContent, diff, preChecks, remainingCriteria)

		maxAttempts := max(int(task.MaxAttempts), 1)

		var lastErr error
		for attempt := 1; attempt <= maxAttempts; attempt++ {
			if attempt > 1 {
				slog.Warn("Retrying verify LLM call", "task", task.ID, "attempt", attempt)
				w.db.LogEvent(task.WorkflowID, task.ID, "verify_retry", map[string]any{
					"attempt": attempt,
					"error":   lastErr.Error(),
				})
			}

			jsonStr, usage, err := w.llm.Complete(ctx, w.prompts.Verify, promptContext)
			if err != nil {
				lastErr = fmt.Errorf("llm verification failed (attempt %d): %w", attempt, err)
				continue
			}

			slog.Debug("Raw LLM response", "attempt", attempt, "response", jsonStr)

			cleanedJSON := llm.CleanLLMResponse(jsonStr)

			slog.Debug("Cleaned JSON for parsing", "json", cleanedJSON)

			if err := json.Unmarshal([]byte(cleanedJSON), &report); err != nil {
				slog.Error("JSON parse failed", "task", task.ID, "cleaned_body", cleanedJSON)
				lastErr = fmt.Errorf("invalid json from llm (attempt %d): %w", attempt, err)
				continue
			}

			lastVerifyUsage = usage
			lastErr = nil
			break
		}

		if lastErr != nil {
			return pipelineerrors.Retryablef("verify", task.WorkflowID, task.ID,
				"llm failed after %d attempts: %w", maxAttempts, lastErr)
		}

		mergePreChecks(&report, preChecks, preCheckedSet)
	}

	if err := w.db.UpdatePipelineRunVerify(ctx, database.UpdatePipelineRunVerifyParams{
		WorkflowID:        task.WorkflowID,
		VerifyStartedAt:   sql.NullString{String: verifyStartedAt.UTC().Format(time.RFC3339), Valid: true},
		VerifyCompletedAt: sql.NullString{String: time.Now().UTC().Format(time.RFC3339), Valid: true},
		VerifyTokensIn:    sql.NullInt64{Int64: int64(lastVerifyUsage.PromptTokens), Valid: true},
		VerifyTokensOut:   sql.NullInt64{Int64: int64(lastVerifyUsage.CompletionTokens), Valid: true},
		VerifyModel:       sql.NullString{String: w.cfg.LocalModel.ModelName, Valid: true},
		VerifyResult:      sql.NullString{String: report.Status, Valid: true},
		BuildScopeDrift:   boolToInt(report.ScopeDrift.Detected),
	}); err != nil {
		slog.Warn("Failed to update pipeline run verify metrics", "error", err)
	}

	dataDir := w.cfg.Project.DataDir
	reportPath, err := writeVerifyReport(&report, dataDir, task.WorkflowID)
	if err != nil {
		return pipelineerrors.Fatalf("verify", task.WorkflowID, task.ID, "%s", err)
	}

	if err := w.db.UpdateWorkflowVerification(ctx, database.UpdateWorkflowVerificationParams{
		ID:                     task.WorkflowID,
		VerificationReportPath: sql.NullString{String: reportPath, Valid: true},
		CurrentState:           "human_review",
	}); err != nil {
		return pipelineerrors.Fatalf("verify", task.WorkflowID, task.ID, "failed to update workflow verification: %w", err)
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

// buildVerifyPrompt assembles the full prompt context for the verification LLM call.
func buildVerifyPrompt(woContent, cpContent []byte, diff string, preChecks []criterionPreCheck, remainingCriteria []string) string {
	var preCheckedSection strings.Builder
	if len(preChecks) > 0 {
		preCheckedSection.WriteString("=== PRE-CHECKED CRITERIA (DO NOT RE-EVALUATE) ===\n")
		for _, r := range preChecks {
			tag := "[PASS]"
			if !r.met {
				tag = "[FAIL]"
			}
			fmt.Fprintf(&preCheckedSection, "%s %s (%s)\n", tag, r.criterion, r.notes)
		}
		preCheckedSection.WriteString("\n")
	}

	var remainingSection strings.Builder
	if len(remainingCriteria) > 0 {
		remainingSection.WriteString("=== REMAINING CRITERIA FOR YOUR EVALUATION ===\n")
		for _, c := range remainingCriteria {
			fmt.Fprintf(&remainingSection, "- %s\n", c)
		}
		remainingSection.WriteString("\n")
	}

	return fmt.Sprintf(`=== WORK ORDER ===
%s

=== CONTEXT PACKAGE ===
%s

=== IMPLEMENTATION DIFF ===
%s

%s%s`, string(woContent), string(cpContent), diff,
		preCheckedSection.String(), remainingSection.String())
}

// mergePreChecks prepends pre-checked results into the report, deduplicates
// against LLM entries, and recomputes the overall status.
func mergePreChecks(report *models.VerificationReport, preChecks []criterionPreCheck, preCheckedSet map[string]bool) {
	if len(preChecks) == 0 {
		return
	}

	preResults := make([]models.CriterionResult, len(preChecks))
	for i, r := range preChecks {
		preResults[i] = models.CriterionResult{
			Criterion: r.criterion,
			Met:       r.met,
			Notes:     r.notes,
		}
	}

	var filtered []models.CriterionResult
	for _, cr := range report.Completeness.CriteriaResults {
		if !preCheckedSet[cr.Criterion] {
			filtered = append(filtered, cr)
		}
	}
	report.Completeness.CriteriaResults = append(preResults, filtered...)

	allMet := true
	for _, r := range report.Completeness.CriteriaResults {
		if !r.Met {
			allMet = false
			break
		}
	}
	report.Completeness.AllCriteriaMet = allMet

	for _, r := range preChecks {
		if !r.met {
			report.Status = "FAIL"
			break
		}
	}
}

// writeVerifyReport serializes and writes the verification report to disk.
func writeVerifyReport(report *models.VerificationReport, dataDir, workflowID string) (string, error) {
	reportDir := filepath.Join(dataDir, "artifacts", "verify-reports")
	if err := os.MkdirAll(reportDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create verify-reports dir: %w", err)
	}
	reportPath := filepath.Join(reportDir, workflowID+"-verify-report.json")
	reportData, _ := json.MarshalIndent(report, "", "  ")
	if err := os.WriteFile(reportPath, reportData, 0644); err != nil {
		return "", fmt.Errorf("failed to write verification report: %w", err)
	}
	return reportPath, nil
}
