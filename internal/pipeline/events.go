package pipeline

import "time"

// RunEvent represents a single event emitted during a pipeline run.
type RunEvent struct {
	RunID     string
	Type      string
	Payload   any
	Timestamp time.Time
}

// Event type constants.
const (
	EventPhaseStart        = "phase_start"
	EventPhaseComplete     = "phase_complete"
	EventPhaseError        = "phase_error"
	EventBuildStdout       = "build_stdout"
	EventScopeStep         = "scope_step"
	EventVerifyPrecheck    = "verify_precheck"
	EventVerifyResult      = "verify_result"
	EventRunComplete       = "run_complete"
	EventRunAwaitingReview = "run_awaiting_review"

	// Queue events.
	EventQueueStateChanged  = "queue_state_changed"
	EventQueueItemStarted   = "queue_item_started"
	EventQueueItemCompleted = "queue_item_completed"
)

// EventSink receives events emitted during a pipeline run.
type EventSink interface {
	Emit(event RunEvent)
}

// NoOpSink discards all events.
type NoOpSink struct{}

// Emit implements EventSink by doing nothing.
func (n *NoOpSink) Emit(event RunEvent) {}

// ChannelSink writes events to a channel.
type ChannelSink struct {
	ch chan RunEvent
}

// NewChannelSink creates a ChannelSink that writes to the provided channel.
func NewChannelSink(ch chan RunEvent) *ChannelSink {
	return &ChannelSink{ch: ch}
}

// Emit implements EventSink by sending the event to the channel.
func (s *ChannelSink) Emit(event RunEvent) {
	s.ch <- event
}
