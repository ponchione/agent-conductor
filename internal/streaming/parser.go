package streaming

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"strings"
)

// StreamParser reads NDJSON from a claude stream, emits typed StreamEvent
// values via a callback, and accumulates a Result.
type StreamParser struct {
	callback func(StreamEvent)
	result   Result
}

// NewStreamParser creates a StreamParser that calls callback for each parsed event.
func NewStreamParser(callback func(StreamEvent)) *StreamParser {
	return &StreamParser{
		callback: callback,
		result:   Result{ToolCalls: make(map[string]int)},
	}
}

// Parse reads NDJSON lines from r, writes each raw line to logWriter,
// parses events, and invokes the callback for each one.
func (p *StreamParser) Parse(r io.Reader, logWriter io.Writer) {
	if logWriter == nil {
		logWriter = io.Discard
	}

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

		var env streamEventType
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

// CollectText parses a Claude stream, returns the assembled assistant text,
// and accumulates result metadata.
func CollectText(r io.Reader, logWriter io.Writer, callback func(StreamEvent)) (string, Result) {
	var text strings.Builder

	wrappedCallback := func(ev StreamEvent) {
		if ev.Type == "assistant" && ev.Content != "" {
			text.WriteString(ev.Content)
		}
		if callback != nil {
			callback(ev)
		}
	}

	parser := NewStreamParser(wrappedCallback)
	parser.Parse(r, logWriter)

	return text.String(), parser.GetResult()
}

// GetResult returns the accumulated stream metadata.
func (p *StreamParser) GetResult() Result {
	return p.result
}

// parseAssistant extracts text content from an assistant event.
func (p *StreamParser) parseAssistant(raw []byte, ev *StreamEvent) {
	var full struct {
		Message struct {
			Content []struct {
				Type  string         `json:"type"`
				Text  string         `json:"text"`
				Name  string         `json:"name"`
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

// parseToolUse extracts tool name and summarized input.
func (p *StreamParser) parseToolUse(raw []byte, ev *StreamEvent) {
	var te toolUseEventJSON
	if err := json.Unmarshal(raw, &te); err != nil {
		slog.Warn("StreamParser: failed to parse tool_use event", "error", err)
		return
	}
	ev.ToolName = te.Tool.Name
	ev.ToolInput = ToolCallSummary(te.Tool.Name, te.Tool.Input)
	p.result.ToolCalls[te.Tool.Name]++
}

// parseToolResult extracts tool name and output.
func (p *StreamParser) parseToolResult(raw []byte, ev *StreamEvent) {
	var tr toolResultEventJSON
	if err := json.Unmarshal(raw, &tr); err != nil {
		slog.Warn("StreamParser: failed to parse tool_result event", "error", err)
		return
	}
	ev.ToolName = tr.Tool.Name
	ev.ToolOutput = tr.Tool.Output
}

// parseResult extracts token usage, cost, model, and session from result events.
func (p *StreamParser) parseResult(raw []byte, ev *StreamEvent) {
	var re resultEventJSON
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

// ToolCallSummary extracts a short description from tool input for logging.
func ToolCallSummary(name string, input map[string]any) string {
	for _, key := range []string{"file_path", "command", "pattern", "query", "url"} {
		if v, ok := input[key]; ok {
			s := fmt.Sprintf("%v", v)
			if len(s) > 80 {
				s = s[:80] + "..."
			}
			return s
		}
	}
	return ""
}
