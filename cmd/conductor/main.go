package main

import (
	stdctx "context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/ponchione/agent-conductor/internal/config"
	"github.com/ponchione/agent-conductor/internal/context"
	"github.com/ponchione/agent-conductor/internal/database"
	"github.com/ponchione/agent-conductor/internal/executor"
	"github.com/ponchione/agent-conductor/internal/gate"
	"github.com/ponchione/agent-conductor/internal/git"
	"github.com/ponchione/agent-conductor/internal/llm"
	"github.com/ponchione/agent-conductor/internal/queue"
	"github.com/ponchione/agent-conductor/internal/worker"
)

func main() {
	opts := &slog.HandlerOptions{Level: slog.LevelDebug}
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stdout, opts)))

	if len(os.Args) < 2 {
		fmt.Println("Usage: conductor <command> [args]")
		os.Exit(1)
	}

	cmd := os.Args[1]
	args := os.Args[2:]

	// Subcommand dispatch
	switch cmd {
	case "approve", "reject":
		runGateCommand(cmd, args)
		return
	case "status":
		runStatus(args)
		return
	case "run":
		projectPath := "project.yaml" // fallback default
		workOrderPath := ""

		for i := 0; i < len(args); i++ {
			if args[i] == "--project" && i+1 < len(args) {
				projectPath = args[i+1]
				i++ // skip the value
			} else if workOrderPath == "" && len(args[i]) > 0 && args[i][0] != '-' {
				workOrderPath = args[i]
			}
		}

		if workOrderPath == "" {
			fmt.Println("Usage: conductor run <work-order-path> --project <project.yaml-path>")
			os.Exit(1)
		}

		cfg, err := config.Load(projectPath)
		if err != nil {
			slog.Error("Failed to load config", "path", projectPath, "error", err)
			os.Exit(1)
		}

		dataDir := cfg.Project.DataDir
		if err := os.MkdirAll(dataDir, 0755); err != nil {
			slog.Error("Failed to create data directory", "path", dataDir, "error", err)
			os.Exit(1)
		}

		dbPath := filepath.Join(dataDir, "db", "conductor.db")
		slog.Info("Initializing database", "path", dbPath)

		db, err := database.NewDB(dbPath)
		if err != nil {
			panic(err)
		}
		defer db.Close()

		gitMgr := git.New(cfg)
		q := queue.New(cfg, db)
		runner := executor.New(cfg)
		llmClient := llm.New(cfg.LocalModel)
		assembler := context.NewAssembler(cfg, gitMgr)

		w := worker.New("worker-1", q, db, cfg, assembler, llmClient, runner, gitMgr)

		runSync(args, w, db, cfg, gitMgr)
		return

	default:
		fmt.Printf("Unknown command: %s\n", cmd)
		os.Exit(1)
	}
}

// runGateCommand handles the approve/reject subcommands.
// Usage: conductor approve <workflow-id>
//
//	conductor reject <workflow-id> [reason]
func runGateCommand(cmd string, args []string) {
	if len(args) == 0 {
		fmt.Fprintf(os.Stderr, "Usage: conductor %s <workflow-id> [reason]\n", cmd)
		os.Exit(1)
	}
	workflowID := args[0]

	projectPath := "project.yaml" // default fallback
	for i := 1; i < len(args); i++ {
		if args[i] == "--project" && i+1 < len(args) {
			projectPath = args[i+1]
			break
		}
	}

	cfg, err := config.Load(projectPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to load config: %v\n", err)
		os.Exit(1)
	}

	dbPath := filepath.Join(cfg.Project.DataDir, "db", "conductor.db")
	//fmt.Printf("\n--- DEBUG COMMAND ---\n%s\n---------------------\n\n", dbPath)
	db, err := database.NewDB(dbPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to open database: %v\n", err)
		os.Exit(1)
	}
	defer db.Close()

	ctx := stdctx.Background()
	switch cmd {
	case "approve":
		if err := gate.Approve(ctx, db, workflowID); err != nil {
			fmt.Fprintf(os.Stderr, "approve failed: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Workflow %s approved. Merging branch...\n", workflowID)
	case "reject":
		reason := ""
		if len(args) > 1 && args[1] != "--project" {
			reason = args[1]
		}

		if err := gate.Reject(ctx, db, workflowID, reason); err != nil {
			fmt.Fprintf(os.Stderr, "reject failed: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Workflow %s rejected: %s\n", workflowID, reason)
	}
}
