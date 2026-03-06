package executor

import (
	"bufio"
	"context"
	"encoding/json"
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
	RepoPath   string        // working directory
	InputFiles []string      // files whose contents are passed to the agent
	Prompt     string        // system prompt / build instructions
	Timeout    time.Duration // wall-clock timeout
	LogDir     string        // directory for stdout.log / stderr.log
}

// RunResult is returned by every executor backend.
type RunResult struct {
	ExitCode   int
	Duration   time.Duration
	StdoutPath string
	StderrPath string
	Success    bool
	TokensIn   int
	TokensOut  int
	Model      string
	CostUSD    float64
	SessionID  string
	ToolCalls  map[string]int
}

// NewExecutor returns the executor backend selected by cfg.Executor.Tool.
func NewExecutor(cfg *config.ProjectConfig) BuildExecutor {
	return &ClaudeCodeExecutor{cfg: cfg}
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

// NDJSON stream types for claude --output-format stream-json

type streamEvent struct {
	Type string `json:"type"`
}

type assistantEvent struct {
	Message struct {
		Content []struct {
			Type  string                 `json:"type"`
			Name  string                 `json:"name"`
			Input map[string]any `json:"input"`
		} `json:"content"`
	} `json:"message"`
}

type resultEvent struct {
	CostUSD           float64 `json:"cost_usd"`
	SessionID         string  `json:"session_id"`
	Model             string  `json:"model"`
	TotalInputTokens  int     `json:"total_input_tokens"`
	TotalOutputTokens int     `json:"total_output_tokens"`
}

type streamResult struct {
	TokensIn  int
	TokensOut int
	Model     string
	CostUSD   float64
	SessionID string
	ToolCalls map[string]int
}

// parseNDJSON reads NDJSON lines from r, writes raw lines to logWriter, and
// returns accumulated metadata. It logs tool-call one-liners in real time.
func parseNDJSON(r io.Reader, logWriter io.Writer) streamResult {
	sr := streamResult{ToolCalls: make(map[string]int)}
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024) // 1MB buffer

	for scanner.Scan() {
		line := scanner.Bytes()
		// Write raw line to log file for post-mortem
		_, _ = logWriter.Write(line)
		_, _ = logWriter.Write([]byte("\n"))

		var env streamEvent
		if err := json.Unmarshal(line, &env); err != nil {
			slog.Warn("NDJSON: malformed line", "error", err)
			continue
		}

		switch env.Type {
		case "assistant":
			var ae assistantEvent
			if err := json.Unmarshal(line, &ae); err != nil {
				slog.Warn("NDJSON: failed to parse assistant event", "error", err)
				continue
			}
			for _, block := range ae.Message.Content {
				if block.Type == "tool_use" {
					sr.ToolCalls[block.Name]++
					summary := toolCallSummary(block.Name, block.Input)
					slog.Info("Tool call", "tool", block.Name, "detail", summary)
				}
			}
		case "result":
			var re resultEvent
			if err := json.Unmarshal(line, &re); err != nil {
				slog.Warn("NDJSON: failed to parse result event", "error", err)
				continue
			}
			sr.TokensIn = re.TotalInputTokens
			sr.TokensOut = re.TotalOutputTokens
			sr.Model = re.Model
			sr.CostUSD = re.CostUSD
			sr.SessionID = re.SessionID
		}
	}
	if err := scanner.Err(); err != nil {
		slog.Warn("NDJSON: scanner error", "error", err)
	}
	return sr
}

// toolCallSummary extracts a short description from tool input for logging.
func toolCallSummary(name string, input map[string]any) string {
	for _, key := range []string{"file_path", "command", "pattern", "query", "url"} {
		if v, ok := input[key]; ok {
			s := fmt.Sprintf("%v", v)
			if len(s) > 80 {
				s = s[:80] + "…"
			}
			return s
		}
	}
	return ""
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

	promptFlag := "--system-prompt"
	if e.cfg.Executor.ClaudeCode.AppendSystemPrompt {
		promptFlag = "--append-system-prompt"
	}

	args := []string{
		"--output-format", "stream-json",
		"--dangerously-skip-permissions",
		promptFlag, runCfg.Prompt,
		userMsg,
	}
	if model := e.cfg.Executor.ClaudeCode.Model; model != "" {
		args = append([]string{"--model", model}, args...)
	}

	claudeBin, err := exec.LookPath("claude")
	if err != nil {
		return nil, fmt.Errorf("claude binary not found: %w", err)
	}
	cmd := exec.CommandContext(ctx, claudeBin, args...)
	cmd.Dir = runCfg.RepoPath
	cmd.Stderr = io.MultiWriter(os.Stderr, stderrFile)
	cmd.Env = os.Environ()

	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to create stdout pipe: %w", err)
	}

	slog.Info("Executing ClaudeCode",
		"dir", runCfg.RepoPath,
		"timeout", timeout)
	slog.Debug("Executing command", "cmd", cmd.String())

	start := time.Now()
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("failed to start claude: %w", err)
	}

	sr := parseNDJSON(stdoutPipe, stdoutFile)

	waitErr := cmd.Wait()
	duration := time.Since(start)

	result, err := runResult(ctx, waitErr, duration, stderrFile, stdoutPath, stderrPath)
	if err != nil {
		return nil, err
	}

	result.TokensIn = sr.TokensIn
	result.TokensOut = sr.TokensOut
	result.Model = sr.Model
	result.CostUSD = sr.CostUSD
	result.SessionID = sr.SessionID
	result.ToolCalls = sr.ToolCalls

	slog.Info("ClaudeCode run complete",
		"exit_code", result.ExitCode,
		"duration", duration,
		"tokens_in", sr.TokensIn,
		"tokens_out", sr.TokensOut,
		"cost_usd", sr.CostUSD,
		"model", sr.Model,
		"tool_calls", sr.ToolCalls,
	)

	return result, nil
}
