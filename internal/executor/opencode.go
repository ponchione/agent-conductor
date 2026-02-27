package executor

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/ponchione/agent-conductor/internal/config"
)

type OpenCodeRunner struct {
	cfg *config.ProjectConfig
}

func New(cfg *config.ProjectConfig) *OpenCodeRunner {
	return &OpenCodeRunner{cfg: cfg}
}

type RunConfig struct {
	RepoPath   string
	Agent      string
	InputFiles []string
	Prompt     string // Optional trailing prompt
	Title      string
	Timeout    time.Duration
	LogDir     string
}

type RunResult struct {
	ExitCode   int
	Duration   time.Duration
	StdoutPath string
	StderrPath string
	Success    bool
}

func (r *OpenCodeRunner) Run(ctx context.Context, runCfg RunConfig) (*RunResult, error) {
	timeout := runCfg.Timeout
	if timeout == 0 {
		timeout = 30 * time.Minute
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

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
	slog.Debug("Executing command", "cmd", cmd.String())

	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = nil
	//cmd.Stdout = stdoutFile
	//cmd.Stderr = stderrFile
	//cmd.Stdin = os.Stdin

	cmd.Env = os.Environ()

	slog.Info("Executing OpenCode",
		"agent", runCfg.Agent,
		"dir", runCfg.RepoPath,
		"timeout", timeout)

	startTime := time.Now()
	err = cmd.Run()
	duration := time.Since(startTime)

	exitCode := 0
	success := true

	if err != nil {
		success = false
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else if ctx.Err() == context.DeadlineExceeded {
			exitCode = -1 // Custom code for Timeout
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
