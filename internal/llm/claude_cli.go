package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
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

	args := []string{
		"--print",
		"--model", c.model,
		"--output-format", "json",
		"--system-prompt", systemPrompt,
		"--dangerously-skip-permissions",
		"--max-turns", "1",
		"--tools", "",
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

	var parts []string
	for _, block := range resp.Result {
		if block.Type == "text" {
			parts = append(parts, block.Text)
		}
	}

	return &CompletionResult{
		Content:   strings.Join(parts, ""),
		Provider:  "claude-cli",
		Model:     c.model,
		LatencyMs: latency.Milliseconds(),
		// TokensIn/TokensOut are zero because the claude CLI does not expose
		// token usage in its JSON output.
		TokensIn:  0,
		TokensOut: 0,
	}, nil
}

// claudeCLIResponse is the top-level JSON structure returned by
// claude --output-format json.
type claudeCLIResponse struct {
	Result []claudeCLIContentBlock `json:"result"`
}

// claudeCLIContentBlock represents a single content block in the CLI response.
type claudeCLIContentBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}
