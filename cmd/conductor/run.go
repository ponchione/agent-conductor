package main

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/google/uuid"
	"github.com/ponchione/agent-conductor/internal/config"
	condctx "github.com/ponchione/agent-conductor/internal/context"
	"github.com/ponchione/agent-conductor/internal/database"
	"github.com/ponchione/agent-conductor/internal/executor"
	"github.com/ponchione/agent-conductor/internal/git"
	"github.com/ponchione/agent-conductor/internal/graph"
	"github.com/ponchione/agent-conductor/internal/lock"
	"github.com/ponchione/agent-conductor/internal/models"
	"github.com/ponchione/agent-conductor/internal/planner"
	"github.com/ponchione/agent-conductor/internal/queue"
	"github.com/ponchione/agent-conductor/internal/rag"
	"github.com/ponchione/agent-conductor/internal/templates"
	"github.com/ponchione/agent-conductor/internal/worker"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

var runCmd = &cobra.Command{
	Use:   "run <work-order-or-plan-path>",
	Short: "Execute a standalone work order or one canonical task from a plan manifest",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		absPath, err := filepath.Abs(args[0])
		if err != nil {
			return fmt.Errorf("invalid path: %w", err)
		}
		data, err := os.ReadFile(absPath)
		if err != nil {
			return fmt.Errorf("input file not found: %w", err)
		}
		input, err := loadRunInput(absPath, data, runTaskID)
		if err != nil {
			return err
		}

		if err := config.Validate(cfg); err != nil {
			return fmt.Errorf("invalid config: %w", err)
		}

		if err := lock.CheckServerLock(cfg.Project.DataDir); err != nil {
			return err
		}

		var prompts *templates.LoadedPrompts
		if input.WorkOrder.Type == "bootstrap" {
			prompts, err = templates.LoadPromptsForBootstrap(cfg)
		} else {
			prompts, err = templates.LoadPrompts(cfg)
		}
		if err != nil {
			slog.Error("Failed to load prompts", "error", err)
			return err
		}

		if err := config.EnsureDataDirs(cfg); err != nil {
			slog.Error("Failed to create data directories", "error", err)
			return err
		}

		dbPath := filepath.Join(cfg.Project.DataDir, "db", "conductor.db")
		if dbPath == "" {
			slog.Info("Initializing database", "path", dbPath)
		}
		slog.Info("Using database", "path", dbPath)

		db, err := database.NewDB(dbPath)
		if err != nil {
			return err
		}
		defer db.Close()

		if err := warnOnUnmetPlanDependencies(context.Background(), db, input.PlanOrigin); err != nil {
			return err
		}

		gitMgr := git.New(cfg)

		clean, err := gitMgr.WorktreeClean(cfg.Project.Path)
		if err != nil {
			return fmt.Errorf("failed to check worktree status: %w", err)
		}
		if !clean {
			return fmt.Errorf("worktree has uncommitted changes in %s — commit or stash before running conductor", cfg.Project.Path)
		}

		q := queue.New(cfg, db)
		runner := executor.NewExecutor(cfg)

		resolver, err := buildRoleResolver(cfg, pipelineModelRoles)
		if err != nil {
			return fmt.Errorf("invalid model config: %w\n  Hint: run \"conductor init-global\" or define models.providers and models.roles explicitly", err)
		}

		var ragSearcher condctx.RAGSearcher
		if cfg.EmbedModel.Endpoint != "" {
			lanceDir := filepath.Join(cfg.Project.DataDir, "rag")
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
		// Open graph store if available
		var graphQuerier condctx.GraphQuerier
		if cfg.Graph.Enabled {
			dbPath := cfg.Graph.DBPath
			if dbPath == "" {
				dbPath = filepath.Join(cfg.Project.DataDir, "graph.db")
			}
			if _, statErr := os.Stat(dbPath); statErr == nil {
				graphStore, graphErr := graph.NewGraphStore(dbPath)
				if graphErr != nil {
					slog.Warn("graph store unavailable, continuing without structural context", "error", graphErr)
				} else {
					defer graphStore.Close()
					graphQuerier = newGraphQuerierAdapter(graphStore, &cfg.Graph)
				}
			}
		}

		assembler := condctx.NewAssembler(cfg, ragSearcher, graphQuerier)

		w := worker.New("worker-1", q, db, cfg, assembler, resolver, &cfg.Guardrails, runner, gitMgr, prompts)

		return runSync(absPath, input.SourceContent, input.WorkOrder, input.PlanOrigin, w, db, cfg)
	},
}

var runTaskID string

type planRunOrigin struct {
	PlanFile          string
	PlanTaskID        string
	PlanEpicID        string
	DependencyTaskIDs []string
}

type runInput struct {
	SourceContent []byte
	WorkOrder     models.WorkOrder
	PlanOrigin    *planRunOrigin
}

func init() {
	runCmd.Flags().StringVar(&runTaskID, "task", "", "Canonical task ID from a hierarchical plan manifest")
}

func loadRunInput(absPath string, data []byte, taskID string) (runInput, error) {
	if taskID == "" {
		var wo models.WorkOrder
		if err := yaml.Unmarshal(data, &wo); err != nil {
			return runInput{}, fmt.Errorf("invalid work order YAML: %w", err)
		}
		if err := wo.Validate(); err != nil {
			return runInput{}, fmt.Errorf("work order validation failed: %w", err)
		}
		return runInput{
			SourceContent: data,
			WorkOrder:     wo,
		}, nil
	}

	planDoc, err := planner.ParsePlanManifestYAML(data)
	if err != nil {
		return runInput{}, fmt.Errorf("invalid plan manifest YAML: %w", err)
	}

	selected, err := planner.SelectPlanTask(planDoc, taskID)
	if err != nil {
		return runInput{}, err
	}

	wo, err := selected.Task.ToWorkOrder()
	if err != nil {
		return runInput{}, fmt.Errorf("task %q: %w", selected.Task.ID, err)
	}
	workOrderYAML, err := yaml.Marshal(&wo)
	if err != nil {
		return runInput{}, fmt.Errorf("marshal selected work order: %w", err)
	}

	return runInput{
		SourceContent: workOrderYAML,
		WorkOrder:     wo,
		PlanOrigin: &planRunOrigin{
			PlanFile:          absPath,
			PlanTaskID:        selected.Task.ID,
			PlanEpicID:        selected.Epic.ID,
			DependencyTaskIDs: append([]string(nil), selected.Task.DependsOn...),
		},
	}, nil
}

func warnOnUnmetPlanDependencies(ctx context.Context, db *database.DB, origin *planRunOrigin) error {
	if origin == nil {
		return nil
	}

	for _, dependencyTaskID := range origin.DependencyTaskIDs {
		successCount, err := db.CountSuccessfulApprovedRunsByPlanTaskID(ctx, database.CountSuccessfulApprovedRunsByPlanTaskIDParams{
			PlanFile:   database.String(origin.PlanFile),
			PlanTaskID: database.String(dependencyTaskID),
		})
		if err != nil {
			return fmt.Errorf("check prior pipeline runs for dependency %q in plan %q: %w", dependencyTaskID, origin.PlanFile, err)
		}
		if successCount > 0 {
			continue
		}

		message := fmt.Sprintf(
			"Warning: dependency %s for task %s has no prior PASS + approved pipeline run.\n",
			dependencyTaskID,
			origin.PlanTaskID,
		)
		fmt.Print(message)
		slog.Warn(
			"plan task dependency has no prior successful approved run",
			"plan_task_id", origin.PlanTaskID,
			"dependency_task_id", dependencyTaskID,
		)
	}

	return nil
}

// runSync executes a work order synchronously.
func runSync(absPath string, sourceContent []byte, wo models.WorkOrder, origin *planRunOrigin, w *worker.Worker, db *database.DB, cfg *config.ProjectConfig) (err error) {
	fmt.Printf("Starting synchronous execution for: %s\n\n", absPath)

	ctx := context.Background()

	sessionID, wfID, err := initializeRunSession(ctx, db, cfg, absPath, sourceContent, wo, origin)
	defer func() {
		if err == nil || sessionID == "" {
			return
		}
		if transitionErr := db.TransitionSessionState(ctx, sessionID, database.SessionStateFailed, err.Error()); transitionErr != nil {
			slog.Warn("failed to update run session state", "session_id", sessionID, "state", database.SessionStateFailed, "error", transitionErr)
		}
	}()
	if err != nil {
		return err
	}

	fmt.Printf("Workflow %s created in session %s. Processing phases...\n", wfID, sessionID)

	for {
		w.ProcessNextTask(ctx)

		wf, err := db.GetWorkflow(ctx, wfID)
		if err != nil {
			return fmt.Errorf("failed to query workflow: %w", err)
		}

		if wf.CurrentState == "human_review" || wf.CurrentState == "review_needed" ||
			wf.CurrentState == "completed" || wf.CurrentState == "failed" {
			fmt.Printf("\nExecution finished with state: %s\n", wf.CurrentState)
			break
		}

		time.Sleep(200 * time.Millisecond)
	}

	return nil
}

func initializeRunSession(ctx context.Context, db *database.DB, cfg *config.ProjectConfig, absPath string, sourceContent []byte, wo models.WorkOrder, origin *planRunOrigin) (string, string, error) {
	sessionID, err := db.StartSession(ctx, database.SessionKindRunOnly, cfg.Project.Name, absPath)
	if err != nil {
		return "", "", fmt.Errorf("failed to create session: %w", err)
	}

	wfID := uuid.New().String()
	branchName := fmt.Sprintf("%s-%s", cfg.Git.BranchPrefix, wfID[:8])
	managedWorkOrderPath := runInputWorkOrderPath(cfg.Project.DataDir, wfID)

	if err := db.CreateWorkflow(ctx, database.CreateWorkflowParams{
		ID:                     wfID,
		OriginalIntent:         "Work Order: " + filepath.Base(absPath),
		OriginalFile:           managedWorkOrderPath,
		CurrentState:           "pending",
		TargetRepo:             cfg.Project.Path,
		GitBranch:              branchName,
		ContextPackagePath:     sql.NullString{},
		VerificationReportPath: sql.NullString{},
		MaxDepth:               5,
		MaxFilesChanged:        int64(cfg.Safety.MaxFilesChanged),
		MaxDurationMins:        int64(cfg.Safety.MaxDurationMins),
	}); err != nil {
		return sessionID, "", fmt.Errorf("failed to create workflow: %w", err)
	}

	pipelineRunID := uuid.New().String()
	var planFile sql.NullString
	var planTaskID sql.NullString
	var planEpicID sql.NullString
	if origin != nil {
		planFile = database.String(origin.PlanFile)
		planTaskID = database.String(origin.PlanTaskID)
		planEpicID = database.String(origin.PlanEpicID)
	}
	if err := db.CreatePipelineRun(ctx, database.CreatePipelineRunParams{
		ID:            pipelineRunID,
		WorkflowID:    wfID,
		Project:       cfg.Project.Name,
		WorkOrderType: sql.NullString{String: wo.Type, Valid: wo.Type != ""},
		PlanFile:      planFile,
		PlanTaskID:    planTaskID,
		PlanEpicID:    planEpicID,
	}); err != nil {
		return sessionID, "", fmt.Errorf("failed to create pipeline_run: %w", err)
	}
	if err := db.LinkPipelineRunToSession(ctx, pipelineRunID, sessionID); err != nil {
		return sessionID, "", fmt.Errorf("failed to link pipeline_run to session: %w", err)
	}
	if err := persistRunInputWorkOrder(ctx, db, sessionID, wfID, managedWorkOrderPath, absPath, sourceContent, wo); err != nil {
		return sessionID, "", fmt.Errorf("failed to persist input work order: %w", err)
	}

	taskID := uuid.New().String()
	if err := db.CreateTask(ctx, database.CreateTaskParams{
		ID:            taskID,
		WorkflowID:    wfID,
		SequenceNum:   1,
		TaskType:      "execution",
		AgentType:     "claude-code",
		TargetRepo:    cfg.Project.Path,
		Phase:         "scope",
		InputArtifact: managedWorkOrderPath,
		State:         "pending",
		MaxAttempts:   2,
	}); err != nil {
		return sessionID, "", fmt.Errorf("failed to create task: %w", err)
	}

	return sessionID, wfID, nil
}
