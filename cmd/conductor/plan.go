package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/google/uuid"
	"github.com/ponchione/agent-conductor/internal/config"
	"github.com/ponchione/agent-conductor/internal/database"
	"github.com/ponchione/agent-conductor/internal/lock"
	"github.com/ponchione/agent-conductor/internal/logging"
	"github.com/ponchione/agent-conductor/internal/planner"
	"github.com/ponchione/agent-conductor/internal/templates"
	"github.com/spf13/cobra"
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
	if err := lock.CheckServerLock(cfg.Project.DataDir); err != nil {
		return err
	}

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

	result, err := planner.Generate(ctx, planner.GenerateInput{
		SpecContent:            string(specData),
		SpecFile:               specPath,
		SessionID:              sessionID,
		Config:                 cfg,
		DataDir:                cfg.Project.DataDir,
		OutputDir:              planOutputDir,
		SkipAudit:              planSkipAudit,
		AllowUnauditedFallback: planAllowUnauditedFallback,
		Timeout:                timeout,
		ClaudePath:             claudePath,
		Prompts:                prompts,
		DB:                     db,
		OnProgress: func(state, phase string) {
			slog.Info("plan progress", "state", state, "phase", phase)
		},
	})
	if err != nil {
		return err
	}

	// Persist metrics (best-effort).
	if recErr := recordPlanRun(db, sessionID, cfg.Project.Name, specPath, specData, result); recErr != nil {
		slog.Warn("failed to record plan run metrics", "error", recErr)
	}

	fmt.Printf("\nWrote hierarchical plan manifest to %s\n", result.ManifestPath)
	fmt.Printf("Epics: %d  Tasks: %d\n\n", result.Metrics.EpicCount, result.Metrics.TaskCount)

	if result.AuditSummary != nil {
		fmt.Printf("Audit: %d added, %d modified, %d unchanged\n", result.AuditSummary.Added, result.AuditSummary.Modified, result.AuditSummary.Unchanged)
		for _, change := range result.AuditSummary.Changes {
			fmt.Printf("  - %s\n", change)
		}
		fmt.Println()
	}

	return nil
}

// recordPlanRun persists plan metrics to the plan_runs table (best-effort).
// It maps planner.GenerateResult fields to database.InsertPlanRunParams.
func recordPlanRun(db *database.DB, sessionID, project, specFile string, specData []byte, result *planner.GenerateResult) error {
	id := uuid.New().String()
	specFingerprint := sha256.Sum256(specData)

	params := database.InsertPlanRunParams{
		ID:                      id,
		SpecFile:                specFile,
		Project:                 database.String(project),
		SpecFingerprint:         database.String(hex.EncodeToString(specFingerprint[:])),
		WorkOrdersGenerated:     database.Int64(result.Metrics.WorkOrdersGenerated),
		EpicCount:               database.Int64(result.Metrics.EpicCount),
		TaskCount:               database.Int64(result.Metrics.TaskCount),
		PreAuditWorkOrderCount:  database.Int64(result.Metrics.PreAuditWorkOrderCount),
		PostAuditWorkOrderCount: database.Int64(result.Metrics.PostAuditWorkOrderCount),
		GenerationRetryCount:    int64(result.Metrics.GenerationRetryCount),
	}

	if result.GenerationTrace != nil {
		if genResult := result.GenerationTrace.AggregateResult; genResult != nil {
			params.GenerationModel = database.String(genResult.Model)
			params.GenerationTokensIn = database.Int64(genResult.TokensIn)
			params.GenerationTokensOut = database.Int64(genResult.TokensOut)
			params.GenerationSessionID = database.String(genResult.SessionID)
			params.GenerationCostUsd = database.Float64(genResult.CostUSD)
			params.GenerationDurationMs = database.Int64Value(genResult.Duration.Milliseconds())
		}
		if epicResult := result.GenerationTrace.EpicResult; epicResult != nil {
			params.EpicGenerationModel = database.String(epicResult.Model)
			params.EpicGenerationTokensIn = database.Int64(epicResult.TokensIn)
			params.EpicGenerationTokensOut = database.Int64(epicResult.TokensOut)
			params.EpicGenerationCostUsd = database.Float64(epicResult.CostUSD)
			params.EpicGenerationDurationMs = database.Int64Value(epicResult.Duration.Milliseconds())
		}
		taskMetrics := planner.TaskGenerationMetrics(result.GenerationTrace)
		if taskMetrics.CallCount > 0 {
			params.TaskGenerationModel = database.String(taskMetrics.Model)
			params.TaskGenerationCallCount = database.Int64(taskMetrics.CallCount)
			params.TaskGenerationTokensIn = database.Int64(taskMetrics.TokensIn)
			params.TaskGenerationTokensOut = database.Int64(taskMetrics.TokensOut)
			params.TaskGenerationCostUsd = database.Float64(taskMetrics.CostUSD)
			params.TaskGenerationDurationMs = database.Int64Value(taskMetrics.Duration.Milliseconds())
		}
	}
	if result.AuditResult != nil {
		params.AuditModel = database.String(result.AuditResult.Model)
		params.AuditTokensIn = database.Int64(result.AuditResult.TokensIn)
		params.AuditTokensOut = database.Int64(result.AuditResult.TokensOut)
		params.AuditSessionID = database.String(result.AuditResult.SessionID)
		params.AuditCostUsd = database.Float64(result.AuditResult.CostUSD)
		params.AuditDurationMs = database.Int64Value(result.AuditResult.Duration.Milliseconds())
	}
	if result.AuditSummary != nil {
		params.AuditWorkOrdersAdded = database.Int64(result.AuditSummary.Added)
		params.AuditWorkOrdersModified = database.Int64(result.AuditSummary.Modified)
		params.AuditWorkOrdersUnchanged = database.Int64(result.AuditSummary.Unchanged)
		if len(result.AuditSummary.Changes) > 0 {
			payload, err := json.Marshal(result.AuditSummary.Changes)
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
