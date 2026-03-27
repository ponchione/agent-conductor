package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/ponchione/agent-conductor/internal/api"
	"github.com/ponchione/agent-conductor/internal/config"
	condctx "github.com/ponchione/agent-conductor/internal/context"
	"github.com/ponchione/agent-conductor/internal/database"
	"github.com/ponchione/agent-conductor/internal/executor"
	"github.com/ponchione/agent-conductor/internal/git"
	"github.com/ponchione/agent-conductor/internal/graph"
	"github.com/ponchione/agent-conductor/internal/queue"
	"github.com/ponchione/agent-conductor/internal/rag"
	"github.com/ponchione/agent-conductor/internal/templates"
	"github.com/ponchione/agent-conductor/internal/worker"
)

func buildServeQueueCallbacks(db *database.DB, cfg *config.ProjectConfig, gitMgr *git.GitManager, workOrderDir string) (api.ExecuteFn, api.MonitorFn) {
	if db == nil || cfg == nil || gitMgr == nil {
		return nil, nil
	}

	executeFn := func(item api.QueueItem) (string, error) {
		return executeServeQueueItem(db, cfg, gitMgr, workOrderDir, item)
	}
	monitorFn := func(workflowID string) (string, error) {
		return monitorServeWorkflow(db, workflowID)
	}
	return executeFn, monitorFn
}

func executeServeQueueItem(db *database.DB, cfg *config.ProjectConfig, gitMgr *git.GitManager, workOrderDir string, item api.QueueItem) (string, error) {
	ctx := context.Background()

	if err := config.Validate(cfg); err != nil {
		return "", fmt.Errorf("invalid config: %w", err)
	}
	if err := config.EnsureDataDirs(cfg); err != nil {
		return "", fmt.Errorf("ensure data directories: %w", err)
	}

	workOrderPath, err := resolveServeQueueWorkOrderPath(item.WorkOrderFile, workOrderDir)
	if err != nil {
		return "", err
	}
	workOrderData, err := os.ReadFile(workOrderPath)
	if err != nil {
		return "", fmt.Errorf("read work order %q: %w", item.WorkOrderFile, err)
	}

	input, err := loadRunInput(workOrderPath, workOrderData, "")
	if err != nil {
		return "", err
	}

	prompts, err := loadServePrompts(cfg, input.WorkOrder.Type)
	if err != nil {
		return "", err
	}

	clean, err := gitMgr.WorktreeClean(cfg.Project.Path)
	if err != nil {
		return "", fmt.Errorf("failed to check worktree status: %w", err)
	}
	if !clean {
		return "", fmt.Errorf("worktree has uncommitted changes in %s — commit or stash before running conductor", cfg.Project.Path)
	}

	resolver, err := buildRoleResolver(cfg, pipelineModelRoles)
	if err != nil {
		return "", fmt.Errorf("invalid model config: %w\n  Hint: run \"conductor init-global\" or define models.providers and models.roles explicitly", err)
	}

	assembler, cleanup := buildServeAssembler(ctx, cfg)
	defer cleanup()

	q := queue.New(cfg, db)
	runner := executor.NewExecutor(cfg)
	w := worker.New("serve-queue-worker", q, db, cfg, assembler, resolver, &cfg.Guardrails, runner, gitMgr, prompts)

	_, workflowID, err := initializeRunSession(ctx, db, cfg, workOrderPath, input.SourceContent, input.WorkOrder, input.PlanOrigin)
	if err != nil {
		return "", err
	}
	if err := runWorkflowToTerminal(ctx, w, db, workflowID); err != nil {
		return "", err
	}
	return workflowID, nil
}

func loadServePrompts(cfg *config.ProjectConfig, workOrderType string) (*templates.LoadedPrompts, error) {
	if workOrderType == "bootstrap" {
		prompts, err := templates.LoadPromptsForBootstrap(cfg)
		if err != nil {
			return nil, fmt.Errorf("load bootstrap prompts: %w", err)
		}
		return prompts, nil
	}
	prompts, err := templates.LoadPrompts(cfg)
	if err != nil {
		return nil, fmt.Errorf("load prompts: %w", err)
	}
	return prompts, nil
}

func buildServeAssembler(ctx context.Context, cfg *config.ProjectConfig) (*condctx.Assembler, func()) {
	var (
		ragSearcher  condctx.RAGSearcher
		graphQuerier condctx.GraphQuerier
		cleanupFns   []func()
	)

	if cfg.EmbedModel.Endpoint != "" {
		lanceDir := filepath.Join(cfg.Project.DataDir, "rag")
		store, err := rag.NewStore(ctx, lanceDir)
		if err != nil {
			slog.Warn("RAG store unavailable, proceeding without RAG", "error", err)
		} else {
			cleanupFns = append(cleanupFns, func() {
				if err := store.Close(); err != nil {
					slog.Warn("failed to close RAG store", "error", err)
				}
			})
			embedder := rag.NewEmbedder(rag.EmbedderConfig{
				Endpoint:       cfg.EmbedModel.Endpoint,
				TimeoutSeconds: cfg.EmbedModel.TimeoutSeconds,
			})
			ragSearcher = rag.NewSearcher(store, embedder)
		}
	}

	if cfg.Graph.Enabled {
		graphDBPath := cfg.Graph.DBPath
		if graphDBPath == "" {
			graphDBPath = filepath.Join(cfg.Project.DataDir, "graph.db")
		}
		if _, err := os.Stat(graphDBPath); err == nil {
			graphStore, graphErr := graph.NewGraphStore(graphDBPath)
			if graphErr != nil {
				slog.Warn("graph store unavailable, continuing without structural context", "error", graphErr)
			} else {
				cleanupFns = append(cleanupFns, func() {
					if err := graphStore.Close(); err != nil {
						slog.Warn("failed to close graph store", "error", err)
					}
				})
				graphQuerier = newGraphQuerierAdapter(graphStore, &cfg.Graph)
			}
		}
	}

	cleanup := func() {
		for i := len(cleanupFns) - 1; i >= 0; i-- {
			cleanupFns[i]()
		}
	}

	return condctx.NewAssembler(cfg, ragSearcher, graphQuerier), cleanup
}

func resolveServeQueueWorkOrderPath(path, workOrderDir string) (string, error) {
	if path == "" {
		return "", fmt.Errorf("work order file is required")
	}
	if filepath.IsAbs(path) {
		return filepath.Abs(path)
	}
	if workOrderDir != "" {
		return filepath.Abs(filepath.Join(workOrderDir, path))
	}
	return filepath.Abs(path)
}

func runWorkflowToTerminal(ctx context.Context, w *worker.Worker, db *database.DB, workflowID string) error {
	for {
		w.ProcessNextTask(ctx)

		wf, err := db.GetWorkflow(ctx, workflowID)
		if err != nil {
			return fmt.Errorf("failed to query workflow: %w", err)
		}
		if isTerminalWorkflowState(wf.CurrentState) {
			return nil
		}

		time.Sleep(200 * time.Millisecond)
	}
}

func monitorServeWorkflow(db *database.DB, workflowID string) (string, error) {
	wf, err := db.GetWorkflow(context.Background(), workflowID)
	if err != nil {
		return "", fmt.Errorf("get workflow %s: %w", workflowID, err)
	}
	return wf.CurrentState, nil
}

func isTerminalWorkflowState(state string) bool {
	switch state {
	case "human_review", "review_needed", "completed", "failed":
		return true
	default:
		return false
	}
}
