package main

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/google/uuid"
	"github.com/ponchione/agent-conductor/internal/config"
	condctx "github.com/ponchione/agent-conductor/internal/context"
	"github.com/ponchione/agent-conductor/internal/database"
	"github.com/ponchione/agent-conductor/internal/executor"
	"github.com/ponchione/agent-conductor/internal/git"
	"github.com/ponchione/agent-conductor/internal/llm"
	"github.com/ponchione/agent-conductor/internal/models"
	"github.com/ponchione/agent-conductor/internal/queue"
	"github.com/ponchione/agent-conductor/internal/rag"
	"github.com/ponchione/agent-conductor/internal/templates"
	"github.com/ponchione/agent-conductor/internal/worker"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

var runCmd = &cobra.Command{
	Use:   "run <work-order-path>",
	Short: "Execute a work order synchronously",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		// 1. Parse and validate work order FIRST — before any side effects
		absPath, err := filepath.Abs(args[0])
		if err != nil {
			return fmt.Errorf("invalid path: %w", err)
		}
		data, err := os.ReadFile(absPath)
		if err != nil {
			return fmt.Errorf("work order not found: %w", err)
		}
		var wo models.WorkOrder
		if err := yaml.Unmarshal(data, &wo); err != nil {
			return fmt.Errorf("invalid work order YAML: %w", err)
		}
		if err := wo.Validate(); err != nil {
			return fmt.Errorf("work order validation failed: %w", err)
		}

		// 2. Validate config
		if err := config.Validate(cfg); err != nil {
			return fmt.Errorf("invalid config: %w", err)
		}

		// 3. Load prompts and set up infrastructure
		prompts, err := templates.LoadPrompts(cfg)
		if err != nil {
			slog.Error("Failed to load prompts", "error", err)
			return err
		}

		dataDir := cfg.Project.DataDir
		if err := os.MkdirAll(dataDir, 0755); err != nil {
			slog.Error("Failed to create data directory", "path", dataDir, "error", err)
			return err
		}

		dbPath := filepath.Join(dataDir, "db", "conductor.db")
		slog.Info("Initializing database", "path", dbPath)

		db, err := database.NewDB(dbPath)
		if err != nil {
			return err
		}
		defer db.Close()

		gitMgr := git.New(cfg)
		q := queue.New(cfg, db)
		runner := executor.NewExecutor(cfg)
		llmClient := llm.New(cfg.LocalModel)

		var ragSearcher condctx.RAGSearcher
		if cfg.EmbedModel.Endpoint != "" {
			lanceDir := filepath.Join(cfg.Project.DataDir, "lancedb")
			store, err := rag.NewStore(context.Background(), lanceDir)
			if err != nil {
				slog.Warn("RAG store unavailable, proceeding without RAG", "error", err)
			} else {
				embedder := rag.NewEmbedder(rag.EmbedderConfig{
					Endpoint:       cfg.EmbedModel.Endpoint,
					TimeoutSeconds: cfg.EmbedModel.TimeoutSeconds,
				})
				ragSearcher = rag.NewSearcher(store, embedder)
			}
		}
		assembler := condctx.NewAssembler(cfg, gitMgr, ragSearcher)

		w := worker.New("worker-1", q, db, cfg, assembler, llmClient, runner, gitMgr, prompts)

		return runSync(absPath, wo, w, db, cfg, gitMgr)
	},
}

// runSync executes a work order synchronously.
func runSync(absPath string, wo models.WorkOrder, w *worker.Worker, db *database.DB, cfg *config.ProjectConfig, gitMgr *git.GitManager) error {
	fmt.Printf("Starting synchronous execution for: %s\n\n", absPath)

	ctx := context.Background()

	// 1. Create Workflow and Task manually
	wfID := uuid.New().String()
	taskID := uuid.New().String()
	branchName := fmt.Sprintf("%s-%s", cfg.Git.BranchPrefix, wfID[:8])

	fmt.Printf("Creating git branch: %s\n", branchName)
	if err := gitMgr.CreateBranch(cfg.Project.Path, branchName, "main"); err != nil {
		return fmt.Errorf("git create branch failed: %w", err)
	}

	err := db.CreateWorkflow(ctx, database.CreateWorkflowParams{
		ID:                     wfID,
		OriginalIntent:         "Work Order: " + filepath.Base(absPath),
		OriginalFile:           absPath,
		CurrentState:           "pending",
		TargetRepo:             cfg.Project.Name,
		GitBranch:              branchName,
		ContextPackagePath:     sql.NullString{},
		VerificationReportPath: sql.NullString{},
		MaxDepth:               5,
		MaxFilesChanged:        int64(cfg.Safety.MaxFilesChanged),
		MaxDurationMins:        int64(cfg.Safety.MaxDurationMins),
	})
	if err != nil {
		return fmt.Errorf("failed to create workflow: %w", err)
	}

	pipelineRunID := uuid.New().String()
	if err := db.CreatePipelineRun(ctx, database.CreatePipelineRunParams{
		ID:            pipelineRunID,
		WorkflowID:    wfID,
		Project:       cfg.Project.Name,
		WorkOrderType: sql.NullString{String: wo.Type, Valid: wo.Type != ""},
	}); err != nil {
		slog.Warn("Failed to create pipeline_run", "workflow", wfID, "error", err)
	}

	err = db.CreateTask(ctx, database.CreateTaskParams{
		ID:            taskID,
		WorkflowID:    wfID,
		SequenceNum:   1,
		TaskType:      "execution",
		AgentType:     "opencode",
		TargetRepo:    cfg.Project.Name,
		Phase:         "scope",
		InputArtifact: absPath,
		State:         "pending",
		MaxAttempts:   2,
	})
	if err != nil {
		return fmt.Errorf("failed to create task: %w", err)
	}

	// 2. Poll until workflow is complete
	fmt.Printf("Workflow %s created. Processing phases...\n", wfID)

	for {
		w.ProcessNextTask(ctx)

		wf, err := db.GetWorkflow(ctx, wfID)
		if err != nil {
			return fmt.Errorf("failed to query workflow: %w", err)
		}

		if wf.CurrentState == "human_review" || wf.CurrentState == "completed" || wf.CurrentState == "failed" {
			fmt.Printf("\nExecution finished with state: %s\n", wf.CurrentState)
			break
		}
	}

	return nil
}
