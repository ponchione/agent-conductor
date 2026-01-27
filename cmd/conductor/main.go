package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/ponchione/agent-conductor/internal/config"
	//"github.com/ponchione/agent-conductor/internal/database"
	"github.com/ponchione/agent-conductor/internal/executor"
	//"github.com/ponchione/agent-conductor/internal/git"
	//"github.com/ponchione/agent-conductor/internal/scanner"
)

func main() {
	opts := &slog.HandlerOptions{Level: slog.LevelDebug}
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stdout, opts)))

	repoPath := "/tmp/conductor-test-repo"
	os.MkdirAll(repoPath, 0755)

	mockBinPath := createMockOpenCode(repoPath)
	oldPath := os.Getenv("PATH")
	os.Setenv("PATH", filepath.Dir(mockBinPath)+string(os.PathListSeparator)+oldPath)
	defer os.Setenv("PATH", oldPath)

	cfg := &config.Config{}

	runner := executor.New(cfg)

	verifyPhase4(runner, repoPath)
}

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
	if out, err := cmd.CombinedOutput(); err != nil {
		panic(fmt.Sprintf("cmd failed: %s %v\n%s", name, args, out))
	}
}

func createConfig(path, repoPath string) {
	content := fmt.Sprintf(`
log_level: info
database: conducter.db
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
git:
  branch_prefix: feature/test
  commit_author_name: Bot
  commit_author_email: bot@test.com
`, repoPath)
	os.WriteFile(path, []byte(content), 0644)
}

func createMockOpenCode(dir string) string {
	binPath := filepath.Join(dir, "bin", "opencode")
	os.MkdirAll(filepath.Dir(binPath), 0755)

	// Create a script that acts like opencode
	// It prints args to stdout and simulated log to stderr
	content := `#!/bin/sh
echo "Mock OpenCode Starting..."
echo "Args: $@"
echo "Doing work..."
sleep 1
echo "Work complete." >&2
exit 0
`
	if err := os.WriteFile(binPath, []byte(content), 0755); err != nil {
		panic(err)
	}
	return binPath
}

func verifyPhase4(runner *executor.OpenCodeRunner, repoPath string) {
	slog.Info("--- Starting Phase 4 Verification ---")

	ctx := context.Background()
	logDir := filepath.Join(repoPath, "logs", "task-1")

	result, err := runner.Run(ctx, executor.RunConfig{
		RepoPath:  repoPath,
		Agent:     "executor",
		InputFile: "test-order.md",
		Title:     "Test Run",
		Timeout:   5 * time.Second,
		LogDir:    logDir,
	})

	if err != nil {
		slog.Error("Runner failed unexpectedly", "error", err)
		return
	}

	slog.Info("Run finished",
		"exit_code", result.ExitCode,
		"duration", result.Duration,
		"success", result.Success)

	// Verify Logs
	stdout, _ := os.ReadFile(result.StdoutPath)
	stderr, _ := os.ReadFile(result.StderrPath)

	slog.Info("STDOUT Content", "content", string(stdout))
	slog.Info("STDERR Content", "content", string(stderr))

	if !strings.Contains(string(stdout), "Mock OpenCode Starting") {
		slog.Error("FAILED: Stdout does not contain expected output")
		return
	}

	if !result.Success {
		slog.Error("FAILED: Result marked as unsuccessful")
		return
	}

	slog.Info("SUCCESS: Mock execution captured correctly")
	slog.Info("--- Phase 4 Verification Complete ---")
}

func getBranches(dir string) []string {
	cmd := exec.Command("git", "branch", "--format=%(refname:short)")
	cmd.Dir = dir
	out, _ := cmd.Output()
	return strings.Split(strings.TrimSpace(string(out)), "\n")
}

func getGitLog(dir string) string {
	cmd := exec.Command("git", "log", "-n", "1", "--oneline")
	cmd.Dir = dir
	out, _ := cmd.Output()
	return string(out)
}

func contains(s, substr string) bool {
	return strings.Contains(s, substr)
}
