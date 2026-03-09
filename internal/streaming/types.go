package streaming

import "encoding/json"

// StreamEvent is a parsed event emitted by the stream parser callback.
type StreamEvent struct {
	Type       string          // event type: assistant, tool_use, tool_result, result, etc.
	Content    string          // text content for assistant events
	ToolName   string          // tool name for tool_use and tool_result events
	ToolInput  string          // summarized tool input for tool_use events
	ToolOutput string          // tool output for tool_result events
	Usage      *TokenUsage     // token usage for result events
	Raw        json.RawMessage // raw JSON of the original line
}

// TokenUsage holds token counts and cost from a result event.
type TokenUsage struct {
	InputTokens  int
	OutputTokens int
	CostUSD      float64
}

// Result holds accumulated metadata from a parsed stream.
type Result struct {
	TokensIn  int
	TokensOut int
	Model     string
	CostUSD   float64
	SessionID string
	ToolCalls map[string]int
}

// Internal JSON structs for NDJSON parsing.

type streamEventType struct {
	Type string `json:"type"`
}

type resultEventJSON struct {
	CostUSD   float64 `json:"total_cost_usd"`
	SessionID string  `json:"session_id"`
	Model     string  `json:"model"`
	Duration  int64   `json:"duration_ms"`
	IsError   bool    `json:"is_error"`
	Usage     struct {
		InputTokens              int `json:"input_tokens"`
		OutputTokens             int `json:"output_tokens"`
		CacheReadInputTokens     int `json:"cache_read_input_tokens"`
		CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
	} `json:"usage"`
}

type toolUseEventJSON struct {
	Tool struct {
		Name  string         `json:"name"`
		Input map[string]any `json:"input"`
	} `json:"tool"`
}

type toolResultEventJSON struct {
	Tool struct {
		Name   string `json:"name"`
		Output string `json:"output"`
	} `json:"tool"`
}
