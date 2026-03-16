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
	"github.com/ponchione/agent-conductor/internal/models"
	"github.com/ponchione/agent-conductor/internal/queue"
	"github.com/ponchione/agent-conductor/internal/scope"
	"github.com/ponchione/agent-conductor/internal/verify"
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

// TODO: refactor to use structured pre-check config instead of string matching
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
		//cmd.Env = append(os.Environ(),
		//	"CGO_CFLAGS=-I"+filepath.Join(w.cfg.Project.Path, "include"),
		//	"CGO_LDFLAGS=-L"+filepath.Join(w.cfg.Project.Path, "lib/linux_amd64")+" -llancedb_go -lm -ldl -lpthread",
		//)
		cmd.Env = os.Environ()
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

	preChecks := w.runPreChecks(ctx, wo.AcceptanceCriteria)

	preCheckedSet := make(map[string]bool, len(preChecks))
	for _, r := range preChecks {
		preCheckedSet[r.criterion] = true
	}

	allPreFailed := len(preChecks) > 0
	for _, r := range preChecks {
		if r.met {
			allPreFailed = false
			break
		}
	}

	var report *models.VerificationReport
	var records []scope.SubCallRecord

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
		report = &models.VerificationReport{
			Status:  "FAIL",
			Summary: "All pre-checked criteria failed (go build/test/vet). Skipping LLM evaluation.",
			Completeness: models.Completeness{
				AllCriteriaMet:  false,
				CriteriaResults: criteriaResults,
			},
		}
	} else {
		// Convert pre-checks to orchestrator type.
		verifyPreChecks := make([]verify.PreCheckResult, len(preChecks))
		for i, r := range preChecks {
			verifyPreChecks[i] = verify.PreCheckResult{
				Criterion: r.criterion,
				Met:       r.met,
				Notes:     r.notes,
			}
		}

		orch := verify.NewVerifyOrchestrator(
			w.models, w.cfg, w.guardrails,
			verify.VerifyPrompts{
				Analyze:    w.prompts.VerifyAnalyze,
				Synthesize: w.prompts.VerifySynthesize,
			},
		)

		var orchErr error
		report, records, orchErr = orch.Execute(ctx, &wo, diff, verifyPreChecks)
		if orchErr != nil {
			return pipelineerrors.Retryablef("verify", task.WorkflowID, task.ID,
				"verify orchestrator failed: %w", orchErr)
		}

		mergePreChecks(report, preChecks, preCheckedSet)
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
	reportData, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return "", fmt.Errorf("failed to marshal verification report: %w", err)
	}
	if err := os.WriteFile(reportPath, reportData, 0644); err != nil {
		return "", fmt.Errorf("failed to write verification report: %w", err)
	}
	return reportPath, nil
}
