package planner

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"time"

	"github.com/ponchione/agent-conductor/internal/streaming"
)

// InvokeClaudeResult holds the text output and token usage from a Claude invocation.
type InvokeClaudeResult struct {
	Content   string
	TokensIn  int
	TokensOut int
	Model     string
	CostUSD   float64
	Duration  time.Duration
	SessionID string
	ToolCalls map[string]int
}

// InvokeClaude runs the claude binary and returns the final text output.
func InvokeClaude(claudePath, systemPrompt, userMsg string, timeout time.Duration, workDir string) (string, error) {
	result, err := InvokeClaudeWithStats(claudePath, systemPrompt, userMsg, timeout, workDir, nil)
	if err != nil {
		return "", err
	}
	return result.Content, nil
}

// InvokeClaudeWithStats runs the claude binary with stream-json output and returns token usage.
func InvokeClaudeWithStats(claudePath, systemPrompt, userMsg string, timeout time.Duration, workDir string, callback func(streaming.StreamEvent)) (*InvokeClaudeResult, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	args := []string{
		"--print",
		"--verbose",
		"--output-format", "stream-json",
		"--dangerously-skip-permissions",
		"--system-prompt", systemPrompt,
		userMsg,
	}
	cmd := exec.CommandContext(ctx, claudePath, args...)
	if workDir != "" {
		cmd.Dir = workDir
	}
	cmd.Env = append(os.Environ(), "CLAUDE_CODE_DISABLE_AUTO_MEMORY=1")

	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to create stdout pipe: %w", err)
	}

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	start := time.Now()
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("failed to start claude: %w", err)
	}

	content, sr := streaming.CollectText(stdoutPipe, nil, callback)

	if err := cmd.Wait(); err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return nil, fmt.Errorf("claude timed out after %s", timeout)
		}
		return nil, fmt.Errorf("claude exited with error: %w\nstderr: %s", err, stderr.String())
	}
	duration := time.Since(start)

	return &InvokeClaudeResult{
		Content:   content,
		TokensIn:  sr.TokensIn,
		TokensOut: sr.TokensOut,
		Model:     sr.Model,
		CostUSD:   sr.CostUSD,
		Duration:  duration,
		SessionID: sr.SessionID,
		ToolCalls: sr.ToolCalls,
	}, nil
}
