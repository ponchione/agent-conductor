package llm

import (
	"strings"
	"testing"

	"github.com/ponchione/agent-conductor/internal/streaming"
)

func TestParseStreamOutput_AssemblesText(t *testing.T) {
	lines := []string{
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"Hello "}]}}`,
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"world"}]}}`,
		`{"type":"result","total_cost_usd":0.05,"model":"opus","usage":{"input_tokens":100,"output_tokens":20}}`,
	}
	input := strings.Join(lines, "\n") + "\n"

	var events []streaming.StreamEvent
	text, result := parseStreamOutput(strings.NewReader(input), func(ev streaming.StreamEvent) {
		events = append(events, ev)
	})

	if text != "Hello world" {
		t.Errorf("text = %q, want %q", text, "Hello world")
	}
	if result.TokensIn != 100 {
		t.Errorf("TokensIn = %d, want 100", result.TokensIn)
	}
	if result.TokensOut != 20 {
		t.Errorf("TokensOut = %d, want 20", result.TokensOut)
	}
	if result.CostUSD != 0.05 {
		t.Errorf("CostUSD = %f, want 0.05", result.CostUSD)
	}
	if len(events) != 3 {
		t.Errorf("expected 3 callback events, got %d", len(events))
	}
}

func TestParseStreamOutput_NilCallback(t *testing.T) {
	lines := []string{
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"Hi"}]}}`,
		`{"type":"result","total_cost_usd":0.01,"model":"sonnet","usage":{"input_tokens":50,"output_tokens":10}}`,
	}
	input := strings.Join(lines, "\n") + "\n"

	text, result := parseStreamOutput(strings.NewReader(input), nil)

	if text != "Hi" {
		t.Errorf("text = %q, want %q", text, "Hi")
	}
	if result.TokensIn != 50 {
		t.Errorf("TokensIn = %d, want 50", result.TokensIn)
	}
}

func TestParseStreamOutput_NoResultEvent(t *testing.T) {
	input := `{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"partial"}]}}` + "\n"

	text, result := parseStreamOutput(strings.NewReader(input), nil)

	if text != "partial" {
		t.Errorf("text = %q, want %q", text, "partial")
	}
	if result.TokensIn != 0 {
		t.Errorf("TokensIn = %d, want 0", result.TokensIn)
	}
}
