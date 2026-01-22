package executor

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/KickBrass/kickbrass-conductor/internal/config"
)

type OpenCodeRunner struct {
	cfg *config.Config
}

func New(cfg *config.Config) *OpenCodeRunner {
	return &OpenCodeRunner{cfg: cfg}
}

type RunConfig struct {
	RepoPath  string
	Agent     string
	InputFile string
	Title     string
	Timeout   time.Duration
	LogDir    string
}

type RunResult struct {
	ExitCode   int
	Duration   time.Duration
	StdoutPath string
	StderrPath string
	Success    bool
}

func (r *OpenCodeRunner) Run(ctx context.Context, runCfg RunConfig) (*RunResult, error) {
	// 1. Setup Context with Timeout
	// Use the shorter of the provided context or the config timeout
	timeout := runCfg.Timeout
	if timeout == 0 {
		timeout = 30 * time.Minute // Default safe fallback
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// 2. Prepare Log Files
	if err := os.MkdirAll(runCfg.LogDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create log dir: %w", err)
	}

	stdoutPath := filepath.Join(runCfg.LogDir, "stdout.log")
	stderrPath := filepath.Join(runCfg.LogDir, "stderr.log")

	stdoutFile, err := os.Create(stdoutPath)
	if err != nil {
		return nil, fmt.Errorf("failed to create stdout log: %w", err)
	}
	defer stdoutFile.Close()

	stderrFile, err := os.Create(stderrPath)
	if err != nil {
		return nil, fmt.Errorf("failed to create stderr log: %w", err)
	}
	defer stderrFile.Close()

	// 3. Construct Command
	// opencode run --agent <agent> --file <file> --title <title>
	args := []string{
		"run",
		"--agent", runCfg.Agent,
		"--file", runCfg.InputFile,
		"--title", runCfg.Title,
	}

	cmd := exec.CommandContext(ctx, "opencode", args...)
	cmd.Dir = runCfg.RepoPath
	cmd.Stdout = stdoutFile
	cmd.Stderr = stderrFile

	// Inject Environment if needed (e.g. inherit current env)
	cmd.Env = os.Environ()

	slog.Info("Executing OpenCode",
		"agent", runCfg.Agent,
		"dir", runCfg.RepoPath,
		"timeout", timeout)

	// 4. Execute
	startTime := time.Now()
	err = cmd.Run()
	duration := time.Since(startTime)

	// 5. Analyze Result
	exitCode := 0
	success := true

	if err != nil {
		success = false
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else if ctx.Err() == context.DeadlineExceeded {
			exitCode = -1 // Custom code for Timeout
			slog.Error("Task timed out", "duration", duration)
			// Append timeout note to stderr log
			_, _ = stderrFile.WriteString("\n\n[ORCHESTRATOR] Task Timed Out\n")
		} else {
			// Other errors (e.g. binary not found)
			exitCode = 127
			slog.Error("Execution failed", "error", err)
		}
	}

	return &RunResult{
		ExitCode:   exitCode,
		Duration:   duration,
		StdoutPath: stdoutPath,
		StderrPath: stderrPath,
		Success:    success,
	}, nil
}
