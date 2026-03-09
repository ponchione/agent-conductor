package executor

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
)

// ConsoleCallback returns an event callback that writes human-readable
// output to w. Tool results are suppressed (too noisy for console).
func ConsoleCallback(w io.Writer) func(StreamEvent) {
	return func(ev StreamEvent) {
		switch ev.Type {
		case "assistant":
			if ev.Content != "" {
				fmt.Fprint(w, ev.Content)
			}
		case "tool_use":
			fmt.Fprintf(w, "\nTool: %s(%s)\n", ev.ToolName, ev.ToolInput)
		case "tool_result":
			// Suppressed — too noisy for console output.
		case "result":
			if ev.Usage != nil {
				fmt.Fprintf(w, "\n--- Done: %d tokens in, %d tokens out, $%.4f ---\n",
					ev.Usage.InputTokens, ev.Usage.OutputTokens, ev.Usage.CostUSD)
			}
		}
	}
}

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

// StreamParser reads NDJSON from a claude stream, emits typed StreamEvent
// values via a callback, and accumulates a streamResult.
type StreamParser struct {
	callback func(StreamEvent)
	result   streamResult
}

// NewStreamParser creates a StreamParser that calls callback for each parsed event.
func NewStreamParser(callback func(StreamEvent)) *StreamParser {
	return &StreamParser{
		callback: callback,
		result:   streamResult{ToolCalls: make(map[string]int)},
	}
}

// Parse reads NDJSON lines from r, writes each raw line to logWriter,
// parses events, and invokes the callback for each one.
func (p *StreamParser) Parse(r io.Reader, logWriter io.Writer) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)

	for scanner.Scan() {
		line := scanner.Bytes()

		// Write raw line to log.
		_, _ = logWriter.Write(line)
		_, _ = logWriter.Write([]byte("\n"))

		// Copy bytes since scanner reuses its buffer.
		raw := make([]byte, len(line))
		copy(raw, line)

		var env streamEvent
		if err := json.Unmarshal(raw, &env); err != nil {
			slog.Warn("StreamParser: malformed line", "error", err)
			continue
		}

		ev := StreamEvent{
			Type: env.Type,
			Raw:  json.RawMessage(raw),
		}

		switch env.Type {
		case "assistant":
			p.parseAssistant(raw, &ev)
		case "tool_use":
			p.parseToolUse(raw, &ev)
		case "tool_result":
			p.parseToolResult(raw, &ev)
		case "result":
			p.parseResult(raw, &ev)
		}

		p.callback(ev)
	}

	if err := scanner.Err(); err != nil {
		slog.Warn("StreamParser: scanner error", "error", err)
	}
}

// Result returns the accumulated stream metadata.
func (p *StreamParser) Result() streamResult {
	return p.result
}

// parseAssistant extracts text content from an assistant event.
func (p *StreamParser) parseAssistant(raw []byte, ev *StreamEvent) {
	// assistantEvent doesn't include a Text field, so parse with a fuller struct.
	var full struct {
		Message struct {
			Content []struct {
				Type string         `json:"type"`
				Text string         `json:"text"`
				Name string         `json:"name"`
				Input map[string]any `json:"input"`
			} `json:"content"`
		} `json:"message"`
	}
	if err := json.Unmarshal(raw, &full); err != nil {
		return
	}
	for _, block := range full.Message.Content {
		if block.Type == "text" {
			ev.Content = block.Text
			break
		}
	}
}

// toolUseEvent is used to parse tool_use NDJSON events.
type toolUseEvent struct {
	Tool struct {
		Name  string         `json:"name"`
		Input map[string]any `json:"input"`
	} `json:"tool"`
}

// toolResultNDJSON is used to parse tool_result NDJSON events.
type toolResultNDJSON struct {
	Tool struct {
		Name   string `json:"name"`
		Output string `json:"output"`
	} `json:"tool"`
}

// parseToolUse extracts tool name and summarized input.
func (p *StreamParser) parseToolUse(raw []byte, ev *StreamEvent) {
	var te toolUseEvent
	if err := json.Unmarshal(raw, &te); err != nil {
		slog.Warn("StreamParser: failed to parse tool_use event", "error", err)
		return
	}
	ev.ToolName = te.Tool.Name
	ev.ToolInput = toolCallSummary(te.Tool.Name, te.Tool.Input)
	p.result.ToolCalls[te.Tool.Name]++
}

// parseToolResult extracts tool name and output.
func (p *StreamParser) parseToolResult(raw []byte, ev *StreamEvent) {
	var tr toolResultNDJSON
	if err := json.Unmarshal(raw, &tr); err != nil {
		slog.Warn("StreamParser: failed to parse tool_result event", "error", err)
		return
	}
	ev.ToolName = tr.Tool.Name
	ev.ToolOutput = tr.Tool.Output
}

// parseResult extracts token usage, cost, model, and session from result events.
func (p *StreamParser) parseResult(raw []byte, ev *StreamEvent) {
	var re resultEvent
	if err := json.Unmarshal(raw, &re); err != nil {
		slog.Warn("StreamParser: failed to parse result event", "error", err)
		return
	}

	totalIn := re.Usage.InputTokens + re.Usage.CacheReadInputTokens + re.Usage.CacheCreationInputTokens

	ev.Usage = &TokenUsage{
		InputTokens:  totalIn,
		OutputTokens: re.Usage.OutputTokens,
		CostUSD:      re.CostUSD,
	}

	p.result.TokensIn = totalIn
	p.result.TokensOut = re.Usage.OutputTokens
	p.result.Model = re.Model
	p.result.CostUSD = re.CostUSD
	p.result.SessionID = re.SessionID

	slog.Info("Stream result",
		"model", re.Model,
		"cost_usd", fmt.Sprintf("%.4f", re.CostUSD),
		"tokens_in", totalIn,
		"tokens_out", re.Usage.OutputTokens,
	)
}
