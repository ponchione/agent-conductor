package llm

import (
	"context"
	"testing"

	"github.com/ponchione/agent-conductor/internal/streaming"
)

type mockBufferedClient struct {
	result        *CompletionResult
	completeCalls int
}

func (m *mockBufferedClient) Complete(_ context.Context, _, _ string) (*CompletionResult, error) {
	m.completeCalls++
	return m.result, nil
}

type mockEventStreamingClient struct {
	result         *CompletionResult
	completeCalls  int
	streamCalls    int
	callbackEvents []streaming.StreamEvent
}

func (m *mockEventStreamingClient) Complete(_ context.Context, _, _ string) (*CompletionResult, error) {
	m.completeCalls++
	return m.result, nil
}

func (m *mockEventStreamingClient) CompleteStream(_ context.Context, _, _ string, callback func(streaming.StreamEvent)) (*CompletionResult, error) {
	m.streamCalls++
	events := []streaming.StreamEvent{
		{Type: "assistant", Content: "streamed"},
		{Type: "result", Usage: &streaming.TokenUsage{InputTokens: 12, OutputTokens: 4, CostUSD: 0.01}},
	}
	for _, ev := range events {
		m.callbackEvents = append(m.callbackEvents, ev)
		if callback != nil {
			callback(ev)
		}
	}
	return m.result, nil
}

func TestCompleteWithOptionalStreaming_UsesEventStreamingClient(t *testing.T) {
	client := &mockEventStreamingClient{
		result: &CompletionResult{Content: "final", Provider: "claude-cli", Model: "sonnet"},
	}

	var seen []streaming.StreamEvent
	result, err := CompleteWithOptionalStreaming(context.Background(), client, "sys", "user", func(ev streaming.StreamEvent) {
		seen = append(seen, ev)
	})
	if err != nil {
		t.Fatalf("CompleteWithOptionalStreaming returned error: %v", err)
	}

	if client.streamCalls != 1 {
		t.Fatalf("streamCalls = %d, want 1", client.streamCalls)
	}
	if client.completeCalls != 0 {
		t.Fatalf("completeCalls = %d, want 0", client.completeCalls)
	}
	if result.Content != "final" {
		t.Fatalf("Content = %q, want %q", result.Content, "final")
	}
	if len(seen) != 2 {
		t.Fatalf("callback saw %d events, want 2", len(seen))
	}
	if seen[0].Type != "assistant" {
		t.Fatalf("first event type = %q, want assistant", seen[0].Type)
	}
}

func TestCompleteWithOptionalStreaming_FallsBackToBufferedClient(t *testing.T) {
	client := &mockBufferedClient{
		result: &CompletionResult{Content: "buffered", Provider: "openai", Model: "test"},
	}

	callbackCalled := false
	result, err := CompleteWithOptionalStreaming(context.Background(), client, "sys", "user", func(streaming.StreamEvent) {
		callbackCalled = true
	})
	if err != nil {
		t.Fatalf("CompleteWithOptionalStreaming returned error: %v", err)
	}

	if client.completeCalls != 1 {
		t.Fatalf("completeCalls = %d, want 1", client.completeCalls)
	}
	if result.Content != "buffered" {
		t.Fatalf("Content = %q, want %q", result.Content, "buffered")
	}
	if callbackCalled {
		t.Fatal("callback should not be called for buffered clients")
	}
}
