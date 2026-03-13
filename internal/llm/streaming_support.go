package llm

import (
	"context"

	"github.com/ponchione/agent-conductor/internal/streaming"
)

// EventStreamingClient is implemented by clients that can emit Claude-style
// NDJSON events during generation.
type EventStreamingClient interface {
	CompleteStream(ctx context.Context, systemPrompt string, userMessage string, callback func(streaming.StreamEvent)) (*CompletionResult, error)
}

// CompleteWithOptionalStreaming uses callback-based streaming when the client
// supports it and otherwise falls back to the standard buffered completion path.
func CompleteWithOptionalStreaming(
	ctx context.Context,
	client Client,
	systemPrompt string,
	userMessage string,
	callback func(streaming.StreamEvent),
) (*CompletionResult, error) {
	if streamClient, ok := client.(EventStreamingClient); ok {
		return streamClient.CompleteStream(ctx, systemPrompt, userMessage, callback)
	}
	return client.Complete(ctx, systemPrompt, userMessage)
}
