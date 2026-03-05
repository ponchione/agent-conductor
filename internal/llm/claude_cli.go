package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"time"
)

// ClaudeCLIClient implements Client by shelling out to the claude CLI.
type ClaudeCLIClient struct {
	model   string
	timeout time.Duration
}

// Compile-time check that ClaudeCLIClient satisfies Client.
var _ Client = (*ClaudeCLIClient)(nil)

// NewClaudeCLIClient creates a new ClaudeCLIClient with the given model alias
// and command timeout.
func NewClaudeCLIClient(model string, timeout time.Duration) *ClaudeCLIClient {
	return &ClaudeCLIClient{
		model:   model,
		timeout: timeout,
	}
}

// Complete runs the claude CLI with the given prompts and returns the result.
func (c *ClaudeCLIClient) Complete(ctx context.Context, systemPrompt string, userMessage string) (*CompletionResult, error) {
	claudeBin, err := exec.LookPath("claude")
	if err != nil {
		return nil, fmt.Errorf("claude binary not found: %w", err)
	}

	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	// Note: --tools "" is intentionally omitted. Passing an empty string causes
	// the CLI arg parser to swallow the next positional argument (the user message).
	// --max-turns 1 is sufficient to prevent agentic tool-use loops.
	args := []string{
		"--print",
		"--model", c.model,
		"--output-format", "json",
		"--system-prompt", systemPrompt,
		"--dangerously-skip-permissions",
		"--max-turns", "1",
		userMessage,
	}

	cmd := exec.CommandContext(ctx, claudeBin, args...)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	start := time.Now()
	if err := cmd.Run(); err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return nil, fmt.Errorf("claude CLI timed out after %s", c.timeout)
		}
		return nil, fmt.Errorf("claude CLI failed: %w; stderr: %s", err, stderr.String())
	}
	latency := time.Since(start)

	var resp claudeCLIResponse
	if err := json.Unmarshal(stdout.Bytes(), &resp); err != nil {
		return nil, fmt.Errorf("failed to parse claude CLI output: %w", err)
	}

	if resp.IsError {
		return nil, fmt.Errorf("claude CLI returned error: %s", resp.Result)
	}

	return &CompletionResult{
		Content:   resp.Result,
		Provider:  "claude-cli",
		Model:     c.model,
		LatencyMs: latency.Milliseconds(),
		TokensIn:  resp.Usage.InputTokens + resp.Usage.CacheReadInputTokens + resp.Usage.CacheCreationInputTokens,
		TokensOut: resp.Usage.OutputTokens,
	}, nil
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
