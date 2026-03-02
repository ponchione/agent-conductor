package executor

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/ponchione/agent-conductor/internal/config"
)

// BuildExecutor is the interface implemented by all executor backends.
type BuildExecutor interface {
	Run(ctx context.Context, cfg RunConfig) (*RunResult, error)
}

// RunConfig holds the parameters passed to any executor backend.
type RunConfig struct {
	RepoPath   string        // both executors: working directory
	Agent      string        // OpenCode only: agent name
	InputFiles []string      // both: files whose contents are passed to the agent
	Prompt     string        // both: system prompt / build instructions
	Title      string        // OpenCode only: session title
	Timeout    time.Duration // both: wall-clock timeout
	LogDir     string        // both: directory for stdout.log / stderr.log
}

// RunResult is returned by every executor backend.
type RunResult struct {
	ExitCode   int
	Duration   time.Duration
	StdoutPath string
	StderrPath string
	Success    bool
}

// NewExecutor returns the executor backend selected by cfg.Executor.Tool.
func NewExecutor(cfg *config.ProjectConfig) BuildExecutor {
	switch cfg.Executor.Tool {
	case "opencode":
		return &OpenCodeExecutor{cfg: cfg}
	default: // "claude-code" and anything else
		return &ClaudeCodeExecutor{cfg: cfg}
	}
}

// openLogs creates the log directory and opens stdout/stderr log files.
func openLogs(logDir string) (stdoutPath, stderrPath string, stdoutFile, stderrFile *os.File, err error) {
	if err = os.MkdirAll(logDir, 0755); err != nil {
		return "", "", nil, nil, fmt.Errorf("failed to create log dir: %w", err)
	}

	stdoutPath = filepath.Join(logDir, "stdout.log")
	stderrPath = filepath.Join(logDir, "stderr.log")

	stdoutFile, err = os.Create(stdoutPath)
	if err != nil {
		return "", "", nil, nil, fmt.Errorf("failed to create stdout log: %w", err)
	}

	stderrFile, err = os.Create(stderrPath)
	if err != nil {
		stdoutFile.Close()
		return "", "", nil, nil, fmt.Errorf("failed to create stderr log: %w", err)
	}

	return stdoutPath, stderrPath, stdoutFile, stderrFile, nil
}

// runResult converts cmd.Run() output into a RunResult.
func runResult(ctx context.Context, err error, duration time.Duration, stderrFile *os.File, stdoutPath, stderrPath string) (*RunResult, error) {
	exitCode := 0
	success := true

	if err != nil {
		success = false
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else if ctx.Err() == context.DeadlineExceeded {
			exitCode = -1
			slog.Error("Task timed out", "duration", duration)
			_, _ = stderrFile.WriteString("\n\n[CONDUCTOR] Task Timed Out\n")
		} else {
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

// OpenCodeExecutor runs work orders via the opencode CLI.
type OpenCodeExecutor struct {
	cfg *config.ProjectConfig
}

func (e *OpenCodeExecutor) Run(ctx context.Context, runCfg RunConfig) (*RunResult, error) {
	timeout := runCfg.Timeout
	if timeout == 0 {
		timeout = 30 * time.Minute
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	stdoutPath, stderrPath, stdoutFile, stderrFile, err := openLogs(runCfg.LogDir)
	if err != nil {
		return nil, err
	}
	defer stdoutFile.Close()
	defer stderrFile.Close()

	args := []string{
		"run",
		"--agent", runCfg.Agent,
	}
	for _, f := range runCfg.InputFiles {
		args = append(args, "--file", f)
	}
	args = append(args, "--title", runCfg.Title)
	args = append(args, "Execute this work order using the provided context package.")
	if runCfg.Prompt != "" {
		args = append(args, runCfg.Prompt)
	}

	cmd := exec.CommandContext(ctx, "opencode", args...)
	cmd.Dir = runCfg.RepoPath
	cmd.Stdout = io.MultiWriter(os.Stdout, stdoutFile)
	cmd.Stderr = io.MultiWriter(os.Stderr, stderrFile)
	cmd.Env = os.Environ()

	slog.Info("Executing OpenCode",
		"agent", runCfg.Agent,
		"dir", runCfg.RepoPath,
		"timeout", timeout)
	slog.Debug("Executing command", "cmd", cmd.String())

	start := time.Now()
	err = cmd.Run()
	duration := time.Since(start)

	return runResult(ctx, err, duration, stderrFile, stdoutPath, stderrPath)
}

// ClaudeCodeExecutor runs work orders via the claude CLI in non-interactive mode.
type ClaudeCodeExecutor struct {
	cfg *config.ProjectConfig
}

func (e *ClaudeCodeExecutor) Run(ctx context.Context, runCfg RunConfig) (*RunResult, error) {
	timeout := runCfg.Timeout
	if timeout == 0 {
		timeout = 30 * time.Minute
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	stdoutPath, stderrPath, stdoutFile, stderrFile, err := openLogs(runCfg.LogDir)
	if err != nil {
		return nil, err
	}
	defer stdoutFile.Close()
	defer stderrFile.Close()

	var sb strings.Builder
	for _, f := range runCfg.InputFiles {
		contents, readErr := os.ReadFile(f)
		if readErr != nil {
			return nil, fmt.Errorf("failed to read input file %s: %w", f, readErr)
		}
		fmt.Fprintf(&sb, "=== %s ===\n%s\n\n", filepath.Base(f), contents)
	}
	sb.WriteString("Execute this work order using the provided context package.")
	userMsg := sb.String()

	args := []string{
		"--print",
		"--dangerously-skip-permissions",
		"--system-prompt", runCfg.Prompt,
		userMsg,
	}

	cmd := exec.CommandContext(ctx, "/home/gernsback/.local/bin/claude", args...)
	cmd.Dir = runCfg.RepoPath
	cmd.Stdout = io.MultiWriter(os.Stdout, stdoutFile)
	cmd.Stderr = io.MultiWriter(os.Stderr, stderrFile)
	cmd.Env = os.Environ()

	slog.Info("Executing ClaudeCode",
		"dir", runCfg.RepoPath,
		"timeout", timeout)
	slog.Debug("Executing command", "cmd", cmd.String())

	start := time.Now()
	err = cmd.Run()
	duration := time.Since(start)

	return runResult(ctx, err, duration, stderrFile, stdoutPath, stderrPath)
}
