package executor

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestStreamParser_AssistantTextEvent(t *testing.T) {
	input := `{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"I'll start by reading the file."}]}}` + "\n"

	var events []StreamEvent
	p := NewStreamParser(func(ev StreamEvent) {
		events = append(events, ev)
	})

	var logBuf bytes.Buffer
	p.Parse(strings.NewReader(input), &logBuf)

	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Type != "assistant" {
		t.Errorf("Type = %q, want %q", events[0].Type, "assistant")
	}
	if events[0].Content != "I'll start by reading the file." {
		t.Errorf("Content = %q, want %q", events[0].Content, "I'll start by reading the file.")
	}
	if !strings.Contains(logBuf.String(), `"type":"assistant"`) {
		t.Error("expected raw JSON in log output")
	}
}

func TestStreamParser_ToolUseEvent(t *testing.T) {
	input := `{"type":"tool_use","tool":{"name":"Read","input":{"file_path":"internal/worker/scope.go"}}}` + "\n"

	var events []StreamEvent
	p := NewStreamParser(func(ev StreamEvent) {
		events = append(events, ev)
	})

	var logBuf bytes.Buffer
	p.Parse(strings.NewReader(input), &logBuf)

	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Type != "tool_use" {
		t.Errorf("Type = %q, want %q", events[0].Type, "tool_use")
	}
	if events[0].ToolName != "Read" {
		t.Errorf("ToolName = %q, want %q", events[0].ToolName, "Read")
	}
	if events[0].ToolInput != "internal/worker/scope.go" {
		t.Errorf("ToolInput = %q, want %q", events[0].ToolInput, "internal/worker/scope.go")
	}
}

func TestStreamParser_ToolResultEvent(t *testing.T) {
	input := `{"type":"tool_result","tool":{"name":"Read","output":"package worker\n"}}` + "\n"

	var events []StreamEvent
	p := NewStreamParser(func(ev StreamEvent) {
		events = append(events, ev)
	})

	var logBuf bytes.Buffer
	p.Parse(strings.NewReader(input), &logBuf)

	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Type != "tool_result" {
		t.Errorf("Type = %q, want %q", events[0].Type, "tool_result")
	}
	if events[0].ToolName != "Read" {
		t.Errorf("ToolName = %q, want %q", events[0].ToolName, "Read")
	}
	if events[0].ToolOutput != "package worker\n" {
		t.Errorf("ToolOutput = %q, want %q", events[0].ToolOutput, "package worker\\n")
	}
}

func TestStreamParser_ResultEvent(t *testing.T) {
	input := `{"type":"result","total_cost_usd":0.12,"session_id":"sess-1","model":"claude-opus-4","usage":{"input_tokens":15000,"output_tokens":3000,"cache_read_input_tokens":500,"cache_creation_input_tokens":100}}` + "\n"

	var events []StreamEvent
	p := NewStreamParser(func(ev StreamEvent) {
		events = append(events, ev)
	})

	var logBuf bytes.Buffer
	p.Parse(strings.NewReader(input), &logBuf)

	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	ev := events[0]
	if ev.Type != "result" {
		t.Errorf("Type = %q, want %q", ev.Type, "result")
	}
	if ev.Usage == nil {
		t.Fatal("expected Usage to be set")
	}
	if ev.Usage.InputTokens != 15600 {
		t.Errorf("InputTokens = %d, want 15600 (15000+500+100)", ev.Usage.InputTokens)
	}
	if ev.Usage.OutputTokens != 3000 {
		t.Errorf("OutputTokens = %d, want 3000", ev.Usage.OutputTokens)
	}
	if ev.Usage.CostUSD != 0.12 {
		t.Errorf("CostUSD = %f, want 0.12", ev.Usage.CostUSD)
	}
}

func TestStreamParser_MultipleEvents(t *testing.T) {
	lines := []string{
		`{"type":"system","subtype":"init","session_id":"s1"}`,
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"Hello"}]}}`,
		`{"type":"tool_use","tool":{"name":"Bash","input":{"command":"ls -la"}}}`,
		`{"type":"tool_result","tool":{"name":"Bash","output":"file1.go\nfile2.go"}}`,
		`{"type":"result","total_cost_usd":0.05,"session_id":"s1","model":"claude-opus-4","usage":{"input_tokens":100,"output_tokens":50}}`,
	}
	input := strings.Join(lines, "\n") + "\n"

	var events []StreamEvent
	p := NewStreamParser(func(ev StreamEvent) {
		events = append(events, ev)
	})

	var logBuf bytes.Buffer
	p.Parse(strings.NewReader(input), &logBuf)

	if len(events) != 5 {
		t.Fatalf("expected 5 events, got %d", len(events))
	}

	logLines := strings.Split(strings.TrimSpace(logBuf.String()), "\n")
	if len(logLines) != 5 {
		t.Errorf("expected 5 log lines, got %d", len(logLines))
	}
}

func TestStreamParser_MalformedLine(t *testing.T) {
	input := "not valid json\n" + `{"type":"result","total_cost_usd":0.01,"usage":{"input_tokens":10,"output_tokens":5}}` + "\n"

	var events []StreamEvent
	p := NewStreamParser(func(ev StreamEvent) {
		events = append(events, ev)
	})

	var logBuf bytes.Buffer
	p.Parse(strings.NewReader(input), &logBuf)

	if len(events) != 1 {
		t.Fatalf("expected 1 event (result only), got %d", len(events))
	}
	if events[0].Type != "result" {
		t.Errorf("Type = %q, want %q", events[0].Type, "result")
	}
	if strings.Count(logBuf.String(), "\n") != 2 {
		t.Errorf("expected 2 log lines, got %d", strings.Count(logBuf.String(), "\n"))
	}
}

func TestStreamParser_RawFieldPopulated(t *testing.T) {
	input := `{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"hi"}]}}` + "\n"

	var events []StreamEvent
	p := NewStreamParser(func(ev StreamEvent) {
		events = append(events, ev)
	})

	var logBuf bytes.Buffer
	p.Parse(strings.NewReader(input), &logBuf)

	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Raw == nil {
		t.Fatal("expected Raw to be populated")
	}
	if !json.Valid(events[0].Raw) {
		t.Error("Raw is not valid JSON")
	}
}

func TestStreamParser_Result(t *testing.T) {
	lines := []string{
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"hi"},{"type":"tool_use","name":"Read","input":{"file_path":"f.go"}}]}}`,
		`{"type":"tool_use","tool":{"name":"Read","input":{"file_path":"f.go"}}}`,
		`{"type":"tool_use","tool":{"name":"Bash","input":{"command":"go test"}}}`,
		`{"type":"result","total_cost_usd":0.10,"session_id":"s1","model":"opus","usage":{"input_tokens":100,"output_tokens":50}}`,
	}
	input := strings.Join(lines, "\n") + "\n"

	var logBuf bytes.Buffer
	p := NewStreamParser(func(ev StreamEvent) {})
	p.Parse(strings.NewReader(input), &logBuf)

	sr := p.Result()
	if sr.TokensIn != 100 {
		t.Errorf("TokensIn = %d, want 100", sr.TokensIn)
	}
	if sr.TokensOut != 50 {
		t.Errorf("TokensOut = %d, want 50", sr.TokensOut)
	}
	if sr.CostUSD != 0.10 {
		t.Errorf("CostUSD = %f, want 0.10", sr.CostUSD)
	}
	if sr.SessionID != "s1" {
		t.Errorf("SessionID = %q, want %q", sr.SessionID, "s1")
	}
	if sr.Model != "opus" {
		t.Errorf("Model = %q, want %q", sr.Model, "opus")
	}
	if sr.ToolCalls["Read"] != 1 {
		t.Errorf("ToolCalls[Read] = %d, want 1", sr.ToolCalls["Read"])
	}
	if sr.ToolCalls["Bash"] != 1 {
		t.Errorf("ToolCalls[Bash] = %d, want 1", sr.ToolCalls["Bash"])
	}
}
