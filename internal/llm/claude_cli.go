package llm

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"time"

	"github.com/ponchione/agent-conductor/internal/streaming"
)

// ClaudeCLIClient implements Client by shelling out to the claude CLI.
type ClaudeCLIClient struct {
	model   string
	timeout time.Duration
	workDir string
}

// Compile-time check that ClaudeCLIClient satisfies Client.
var _ Client = (*ClaudeCLIClient)(nil)

// NewClaudeCLIClient creates a new ClaudeCLIClient with the given model alias
// and command timeout.
func NewClaudeCLIClient(model string, timeout time.Duration, workDir string) *ClaudeCLIClient {
	return &ClaudeCLIClient{
		model:   model,
		timeout: timeout,
		workDir: workDir,
	}
}

// Complete runs the claude CLI with the given prompts and returns the result.
func (c *ClaudeCLIClient) Complete(ctx context.Context, systemPrompt string, userMessage string) (*CompletionResult, error) {
	return c.CompleteStream(ctx, systemPrompt, userMessage, nil)
}

// claudeCLIResponse is the top-level JSON structure returned by
// claude --print --output-format json.
//
// Example response:
//
//	{
//	  "type": "result",
//	  "result": "the LLM text output",
//	  "is_error": false,
//	  "duration_ms": 1062,
//	  "total_cost_usd": 0.01022,
//	  "usage": {
//	    "input_tokens": 1979,
//	    "output_tokens": 13,
//	    "cache_read_input_tokens": 0,
//	    "cache_creation_input_tokens": 0
//	  }
//	}
type claudeCLIResponse struct {
	Type     string      `json:"type"`
	Result   string      `json:"result"`
	IsError  bool        `json:"is_error"`
	Usage    claudeUsage `json:"usage"`
	CostUSD  float64     `json:"total_cost_usd"`
	Duration int64       `json:"duration_ms"`
}

type claudeUsage struct {
	InputTokens              int `json:"input_tokens"`
	OutputTokens             int `json:"output_tokens"`
	CacheReadInputTokens     int `json:"cache_read_input_tokens"`
	CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
}

// parseStreamOutput runs a StreamParser over NDJSON from r, assembles
// assistant text content, calls callback for each event (if non-nil),
// and returns the assembled text and stream result.
func parseStreamOutput(r io.Reader, callback func(streaming.StreamEvent)) (string, streaming.Result) {
	return streaming.CollectText(r, io.Discard, callback)
}

// CompleteStream runs the claude CLI with streaming output and calls callback
// for each StreamEvent. Returns the assembled text and a CompletionResult.
// If callback is nil, events are still parsed but not reported.
func (c *ClaudeCLIClient) CompleteStream(ctx context.Context, systemPrompt string, userMessage string, callback func(streaming.StreamEvent)) (*CompletionResult, error) {
	claudeBin, err := exec.LookPath("claude")
	if err != nil {
		return nil, fmt.Errorf("claude binary not found: %w", err)
	}

	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	args := []string{
		"--print", "--verbose",
		"--model", c.model,
		"--output-format", "stream-json",
		"--system-prompt", systemPrompt,
		"--dangerously-skip-permissions",
		"--max-turns", "1",
		userMessage,
	}

	cmd := exec.CommandContext(ctx, claudeBin, args...)
	if c.workDir != "" {
		cmd.Dir = c.workDir
	}
	// Explicitly set env to preserve parent environment while disabling auto-memory.
	// Claude Code would otherwise load CLAUDE.md and memory from the cwd hierarchy,
	// which is undesirable for non-build invocations (scope, verify, describe).
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

	text, sr := parseStreamOutput(stdoutPipe, callback)

	if err := cmd.Wait(); err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return nil, fmt.Errorf("claude CLI timed out after %s", c.timeout)
		}
		return nil, fmt.Errorf("claude CLI failed: %w; stderr: %s", err, stderr.String())
	}
	latency := time.Since(start)

	return &CompletionResult{
		Content:   text,
		Provider:  "claude-cli",
		Model:     c.model,
		LatencyMs: latency.Milliseconds(),
		TokensIn:  sr.TokensIn,
		TokensOut: sr.TokensOut,
	}, nil
}
