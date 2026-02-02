package main

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/ponchione/agent-conductor/internal/config"
	"github.com/ponchione/agent-conductor/internal/database"
	"github.com/ponchione/agent-conductor/internal/git"
	"github.com/ponchione/agent-conductor/internal/queue"
	"github.com/ponchione/agent-conductor/internal/scanner"
	//"github.com/ponchione/agent-conductor/internal/database"
	//"github.com/ponchione/agent-conductor/internal/git"
	//"github.com/ponchione/agent-conductor/internal/scanner"
)

func main() {
	opts := &slog.HandlerOptions{Level: slog.LevelDebug}
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stdout, opts)))

	// 1. Setup Environment
	repoPath := "/tmp/orchestrator-refactor-test"
	setupTestRepo(repoPath)

	configFile := "config.yaml"
	createConfig(configFile, repoPath)
	cfg, _ := config.Load(configFile)

	// 2. Initialize DB (New sqlc version)
	os.Remove("orchestrator.db")
	db, err := database.NewDB(cfg.Database) // Renamed constructor
	if err != nil {
		panic(err)
	}
	defer db.Close()

	// 3. Initialize Components
	gitMgr := git.New(cfg)
	scan, _ := scanner.New(cfg, db, gitMgr)
	q := queue.New(cfg, db)

	// 4. Run Verification
	verifyRefactor(scan, q, db, cfg, repoPath)
}

func verifyRefactor(s *scanner.Scanner, q *queue.Queue, db *database.DB, cfg *config.Config, repoPath string) {
	slog.Info("--- Starting SQLC Refactor Verification ---")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 1. Start Scanner
	if err := s.Start(ctx); err != nil {
		panic(err)
	}

	// 2. Create Work Order
	woPath := filepath.Join(cfg.Scanner.InboxPath, "backend", "orders", "wo-refactor.md")
	os.MkdirAll(filepath.Dir(woPath), 0755)
	os.WriteFile(woPath, []byte("# Refactor Test"), 0644)
	slog.Info("Created work order", "path", woPath)

	// 3. Wait for processing
	time.Sleep(2 * time.Second)

	// 4. Check DB for Workflow (using sqlc generated methods)
	// We don't have a "ListWorkflows" query yet, so we query the queue for the task
	// and trace it back.

	slog.Info("Attempting to claim task...")
	task, err := q.ClaimNextTask("worker-test")
	if err != nil {
		slog.Error("Claim failed", "error", err)
		return
	}

	slog.Info("Task Claimed via SQLC",
		"task_id", task.ID,
		"workflow_id", task.WorkflowID,
		"state", task.State,
	)

	// 5. Verify Workflow Details
	wf, err := db.GetWorkflow(ctx, task.WorkflowID)
	if err != nil {
		slog.Error("Failed to fetch workflow", "error", err)
		return
	}

	slog.Info("Workflow Verified",
		"id", wf.ID,
		"branch", wf.GitBranch,
		"original_file", wf.OriginalFile,
	)

	// 6. Verify Events
	// ListEvents returns []Event, error
	events, err := db.ListEvents(ctx, sqlNullString(wf.ID))
	if err != nil {
		slog.Error("Failed to list events", "error", err)
	} else {
		slog.Info("Events Found", "count", len(events))
		for _, e := range events {
			slog.Info("Event", "type", e.EventType)
		}
	}

	slog.Info("--- Verification Complete ---")
}

// Helper to handle sql.NullString creation for queries
func sqlNullString(s string) sql.NullString {
	// sqlc generated struct might be using sql.NullString or database.NullString depending on config
	// Usually it uses sql.NullString.
	// If you see type errors here, we might need to adjust.
	// For now assuming standard sql.NullString, but aliases might exist.
	// Actually, let's just use the helper we added earlier or raw struct.
	return sql.NullString{String: s, Valid: s != ""}
}

// --- Test Helpers ---

func setupTestRepo(path string) {
	os.RemoveAll(path)
	os.MkdirAll(path, 0755)
	execCmd(path, "git", "init")
	execCmd(path, "git", "config", "user.email", "test@test.com")
	execCmd(path, "git", "config", "user.name", "Test User")
	execCmd(path, "git", "commit", "--allow-empty", "-m", "Initial commit")
	execCmd(path, "git", "branch", "-m", "main")
}

func execCmd(dir, name string, args ...string) {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Run()
}

func createConfig(path, repoPath string) {
	content := fmt.Sprintf(`
log_level: info
database: orchestrator.db
scanner:
  interval_seconds: 5
  inbox_path: ./inbox
workers:
  count: 1
repositories:
  backend:
    path: %s
    opencode_agent_executor: executor
    opencode_agent_workorder: work-order
safety:
  max_depth: 5
  max_files_changed: 50
  max_workflow_duration_minutes: 60
  max_task_retries: 2
git:
  branch_prefix: feature/refactor
`, repoPath)
	os.WriteFile(path, []byte(content), 0644)
}
