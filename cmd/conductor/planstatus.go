package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/ponchione/agent-conductor/internal/database"
	"github.com/ponchione/agent-conductor/internal/planner"
	"github.com/spf13/cobra"
)

var planStatusCmd = &cobra.Command{
	Use:   "plan-status <plan.yaml>",
	Short: "Show hierarchical task status for a plan manifest",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		absPath, err := filepath.Abs(args[0])
		if err != nil {
			return fmt.Errorf("invalid path: %w", err)
		}
		data, err := os.ReadFile(absPath)
		if err != nil {
			return fmt.Errorf("plan manifest not found: %w", err)
		}

		planDoc, err := planner.ParsePlanManifestYAML(data)
		if err != nil {
			return fmt.Errorf("invalid plan manifest YAML: %w", err)
		}

		dbPath := filepath.Join(cfg.Project.DataDir, "db", "conductor.db")
		db, err := database.NewDB(dbPath)
		if err != nil {
			return fmt.Errorf("failed to open database: %w", err)
		}
		defer db.Close()

		report, err := buildPlanStatusReport(context.Background(), db, absPath, planDoc)
		if err != nil {
			return err
		}
		return renderPlanStatusReport(os.Stdout, report)
	},
}

type planTaskExecutionStatus string

const (
	planTaskStatusComplete       planTaskExecutionStatus = "complete"
	planTaskStatusFailed         planTaskExecutionStatus = "failed"
	planTaskStatusInProgress     planTaskExecutionStatus = "in-progress"
	planTaskStatusAwaitingReview planTaskExecutionStatus = "awaiting-review"
	planTaskStatusBlocked        planTaskExecutionStatus = "blocked"
	planTaskStatusNotStarted     planTaskExecutionStatus = "not-started"
)

type planStatusTaskView struct {
	Task       planner.PlanTask
	Status     planTaskExecutionStatus
	WorkflowID string
}

type planStatusEpicView struct {
	Epic  planner.PlanEpic
	Tasks []planStatusTaskView
}

type planStatusSummary struct {
	Complete       int
	Failed         int
	InProgress     int
	AwaitingReview int
	Blocked        int
	NotStarted     int
}

type planStatusReport struct {
	PlanPath string
	Summary  planStatusSummary
	Epics    []planStatusEpicView
}

func buildPlanStatusReport(ctx context.Context, db *database.DB, planPath string, doc *planner.PlanDocument) (*planStatusReport, error) {
	if doc == nil {
		return nil, fmt.Errorf("plan document is required")
	}

	rows, err := db.ListLatestPipelineRunsByPlanFile(ctx, database.String(planPath))
	if err != nil {
		return nil, fmt.Errorf("list latest pipeline runs for plan file: %w", err)
	}

	latestByTaskID := make(map[string]database.ListLatestPipelineRunsByPlanFileRow, len(rows))
	for _, row := range rows {
		if !row.PlanTaskID.Valid {
			continue
		}
		latestByTaskID[row.PlanTaskID.String] = row
	}

	report := &planStatusReport{
		PlanPath: planPath,
		Epics:    make([]planStatusEpicView, 0, len(doc.Epics)),
	}

	statusByTaskID := make(map[string]planTaskExecutionStatus, doc.TaskCount())
	for _, epic := range doc.Epics {
		epicView := planStatusEpicView{
			Epic:  epic,
			Tasks: make([]planStatusTaskView, 0, len(epic.Tasks)),
		}

		for _, task := range epic.Tasks {
			status, workflowID := resolvePlanTaskStatus(task, latestByTaskID[task.ID], statusByTaskID)
			statusByTaskID[task.ID] = status
			incrementPlanStatusSummary(&report.Summary, status)
			epicView.Tasks = append(epicView.Tasks, planStatusTaskView{
				Task:       task,
				Status:     status,
				WorkflowID: workflowID,
			})
		}

		report.Epics = append(report.Epics, epicView)
	}

	return report, nil
}

func resolvePlanTaskStatus(task planner.PlanTask, row database.ListLatestPipelineRunsByPlanFileRow, statusByTaskID map[string]planTaskExecutionStatus) (planTaskExecutionStatus, string) {
	if row.PlanTaskID.Valid {
		switch {
		case row.VerifyResult.Valid && row.VerifyResult.String == "PASS" && row.HumanResult.Valid && row.HumanResult.String == "approved":
			return planTaskStatusComplete, row.WorkflowID
		case row.CurrentState == "failed":
			return planTaskStatusFailed, row.WorkflowID
		case row.HumanResult.Valid && row.HumanResult.String == "rejected":
			return planTaskStatusFailed, row.WorkflowID
		case row.VerifyResult.Valid && row.VerifyResult.String == "FAIL":
			return planTaskStatusFailed, row.WorkflowID
		case row.CurrentState == "human_review" || row.CurrentState == "review_needed":
			return planTaskStatusAwaitingReview, row.WorkflowID
		case row.CurrentState == "completed":
			return planTaskStatusFailed, row.WorkflowID
		default:
			return planTaskStatusInProgress, row.WorkflowID
		}
	}

	for _, dependencyTaskID := range task.DependsOn {
		if statusByTaskID[dependencyTaskID] != planTaskStatusComplete {
			return planTaskStatusBlocked, ""
		}
	}

	return planTaskStatusNotStarted, ""
}

func incrementPlanStatusSummary(summary *planStatusSummary, status planTaskExecutionStatus) {
	switch status {
	case planTaskStatusComplete:
		summary.Complete++
	case planTaskStatusFailed:
		summary.Failed++
	case planTaskStatusInProgress:
		summary.InProgress++
	case planTaskStatusAwaitingReview:
		summary.AwaitingReview++
	case planTaskStatusBlocked:
		summary.Blocked++
	case planTaskStatusNotStarted:
		summary.NotStarted++
	}
}

func renderPlanStatusReport(w io.Writer, report *planStatusReport) error {
	if report == nil {
		return fmt.Errorf("plan status report is required")
	}

	if _, err := fmt.Fprintf(
		w,
		"Plan: %s\nSummary: complete %d  failed %d  in-progress %d  awaiting-review %d  blocked %d  not-started %d\n\n",
		report.PlanPath,
		report.Summary.Complete,
		report.Summary.Failed,
		report.Summary.InProgress,
		report.Summary.AwaitingReview,
		report.Summary.Blocked,
		report.Summary.NotStarted,
	); err != nil {
		return fmt.Errorf("write plan status summary: %w", err)
	}

	for _, epic := range report.Epics {
		if _, err := fmt.Fprintf(w, "%s %s\n", epic.Epic.ID, epic.Epic.Title); err != nil {
			return fmt.Errorf("write epic header: %w", err)
		}
		for _, task := range epic.Tasks {
			if _, err := fmt.Fprintf(w, "  [%s] %s %s\n", task.Status, task.Task.ID, task.Task.Title); err != nil {
				return fmt.Errorf("write task status: %w", err)
			}
		}
		if _, err := fmt.Fprintln(w); err != nil {
			return fmt.Errorf("write epic separator: %w", err)
		}
	}

	return nil
}
