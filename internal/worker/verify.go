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

	"gopkg.in/yaml.v3"

	"github.com/ponchione/agent-conductor/internal/database"
	"github.com/ponchione/agent-conductor/internal/models"
	"github.com/ponchione/agent-conductor/internal/queue"
	"github.com/ponchione/agent-conductor/internal/templates"
)

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

	wf, err := w.db.GetWorkflow(ctx, task.WorkflowID)
	if err != nil {
		return fmt.Errorf("workflow missing: %w", err)
	}

	// 1. Commit any uncommitted changes left by the build agent
	hasChanges, err := w.git.HasUncommittedChanges(w.cfg.Project.Path)
	if err != nil {
		return fmt.Errorf("failed to check for uncommitted changes: %w", err)
	}

	if hasChanges {
		slog.Info("Committing agent changes to feature branch before verification")
		if err := w.git.Commit(w.cfg.Project.Path, "Agent build phase implementation"); err != nil {
			return fmt.Errorf("failed to commit agent changes: %w", err)
		}
	}

	// 2. Get Git Diff
	diff, err := w.git.GetDiff(w.cfg.Project.Path, "main", wf.GitBranch)
	if err != nil {
		return fmt.Errorf("git diff failed: %w", err)
	}

	if diff == "" {
		slog.Warn("No changes detected in verify phase")
		diff = "(No changes)"
	}

	// 3. Read work order and context package
	woContent, err := os.ReadFile(wf.OriginalFile)
	if err != nil {
		return fmt.Errorf("failed to read work order: %w", err)
	}

	cpPath := wf.ContextPackagePath.String
	if !wf.ContextPackagePath.Valid {
		return fmt.Errorf("missing context package path")
	}
	cpContent, err := os.ReadFile(cpPath)
	if err != nil {
		return fmt.Errorf("failed to read context package: %w", err)
	}

	// 4. Parse work order for acceptance criteria and run deterministic pre-checks
	var wo models.WorkOrder
	if err := yaml.Unmarshal(woContent, &wo); err != nil {
		slog.Warn("Could not parse work order YAML for pre-checks", "error", err)
	}

	preChecks := w.runPreChecks(ctx, wo.AcceptanceCriteria)

	// Separate remaining (LLM-only) criteria
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

	// Build prompt sections for pre-checked and remaining criteria
	var preCheckedSection strings.Builder
	if len(preChecks) > 0 {
		preCheckedSection.WriteString("=== PRE-CHECKED CRITERIA (DO NOT RE-EVALUATE) ===\n")
		for _, r := range preChecks {
			tag := "[PASS]"
			if !r.met {
				tag = "[FAIL]"
			}
			preCheckedSection.WriteString(fmt.Sprintf("%s %s (%s)\n", tag, r.criterion, r.notes))
		}
		preCheckedSection.WriteString("\n")
	}

	var remainingSection strings.Builder
	if len(remainingCriteria) > 0 {
		remainingSection.WriteString("=== REMAINING CRITERIA FOR YOUR EVALUATION ===\n")
		for _, c := range remainingCriteria {
			remainingSection.WriteString(fmt.Sprintf("- %s\n", c))
		}
		remainingSection.WriteString("\n")
	}

	// 5. Check if all pre-checked criteria failed — force FAIL without LLM
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
		// 6. Build LLM prompt context
		promptContext := fmt.Sprintf(`=== WORK ORDER ===
%s

=== CONTEXT PACKAGE ===
%s

=== IMPLEMENTATION DIFF ===
%s

%s%s`, string(woContent), string(cpContent), diff,
			preCheckedSection.String(), remainingSection.String())

		// 7. Call LLM with retry
		maxAttempts := int(task.MaxAttempts)
		if maxAttempts < 1 {
			maxAttempts = 1
		}

		var lastErr error
		for attempt := 1; attempt <= maxAttempts; attempt++ {
			if attempt > 1 {
				slog.Warn("Retrying verify LLM call", "task", task.ID, "attempt", attempt)
				w.db.LogEvent(task.WorkflowID, task.ID, "verify_retry", map[string]any{
					"attempt": attempt,
					"error":   lastErr.Error(),
				})
			}

			jsonStr, err := w.llm.Complete(ctx, templates.VerifyPrompt, promptContext)
			if err != nil {
				lastErr = fmt.Errorf("llm verification failed (attempt %d): %w", attempt, err)
				continue
			}

			if err := json.Unmarshal([]byte(jsonStr), &report); err != nil {
				lastErr = fmt.Errorf("invalid json from llm (attempt %d): %w", attempt, err)
				continue
			}

			lastErr = nil
			break
		}

		if lastErr != nil {
			return lastErr
		}

		// 8. Merge pre-checked results into the report (prepend so they appear first)
		if len(preChecks) > 0 {
			preResults := make([]models.CriterionResult, len(preChecks))
			for i, r := range preChecks {
				preResults[i] = models.CriterionResult{
					Criterion: r.criterion,
					Met:       r.met,
					Notes:     r.notes,
				}
			}
			// Remove LLM entries for any criterion already pre-checked so there is
			// exactly one entry per criterion and pre-checked results always win.
			var filtered []models.CriterionResult
			for _, cr := range report.Completeness.CriteriaResults {
				if !preCheckedSet[cr.Criterion] {
					filtered = append(filtered, cr)
				}
			}
			report.Completeness.CriteriaResults = append(preResults, filtered...)

			// Recompute all_criteria_met and force FAIL if any pre-check failed
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
	}

	// 9. Write Report
	reportDir := filepath.Join(w.cfg.Project.DataDir, "artifacts", "verify-reports")
	if err := os.MkdirAll(reportDir, 0755); err != nil {
		return fmt.Errorf("failed to create verify-reports dir: %w", err)
	}
	reportPath := filepath.Join(reportDir, task.WorkflowID+"-verify-report.json")
	reportData, _ := json.MarshalIndent(report, "", "  ")
	if err := os.WriteFile(reportPath, reportData, 0644); err != nil {
		return fmt.Errorf("failed to write verification report: %w", err)
	}

	// 10. Update Workflow State → HUMAN_REVIEW
	if err := w.db.UpdateWorkflowVerification(ctx, database.UpdateWorkflowVerificationParams{
		ID:                     task.WorkflowID,
		VerificationReportPath: sql.NullString{String: reportPath, Valid: true},
		CurrentState:           "human_review",
	}); err != nil {
		return fmt.Errorf("failed to update workflow verification: %w", err)
	}

	w.db.LogEvent(task.WorkflowID, task.ID, "verify_completed", map[string]any{
		"status":           report.Status,
		"scope_drift":      report.ScopeDrift.Detected,
		"all_criteria_met": report.Completeness.AllCriteriaMet,
	})

	// Add Observability Printf
	fmt.Printf("\n--- VERIFY PHASE COMPLETE ---\n")
	fmt.Printf("Status: %s\n", report.Status)
	fmt.Printf("Summary: %s\n", report.Summary)
	fmt.Printf("-----------------------------\n")
	fmt.Printf("\n>>> WORKFLOW PAUSED FOR HUMAN REVIEW <<<\n")
	fmt.Printf("Run 'conductor status %s' to view details.\n", task.WorkflowID)
	fmt.Printf("Run 'conductor approve %s' or 'conductor reject %s' to proceed.\n\n", task.WorkflowID, task.WorkflowID)

	// 11. Complete Task
	return w.q.CompleteTask(task.ID, &queue.TaskResult{
		ExitCode:  0,
		StdoutLog: fmt.Sprintf("Verification Status: %s", report.Status),
	})
}
