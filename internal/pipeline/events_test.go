package pipeline

import (
	"testing"
	"time"
)

func TestRunEventFields(t *testing.T) {
	now := time.Now()
	ev := RunEvent{
		RunID:     "run-123",
		Type:      EventPhaseStart,
		Payload:   map[string]string{"phase": "scope"},
		Timestamp: now,
	}

	if ev.RunID != "run-123" {
		t.Errorf("RunID = %q, want %q", ev.RunID, "run-123")
	}
	if ev.Type != EventPhaseStart {
		t.Errorf("Type = %q, want %q", ev.Type, EventPhaseStart)
	}
	if ev.Timestamp != now {
		t.Errorf("Timestamp mismatch")
	}
}

func TestEventTypeConstants(t *testing.T) {
	expected := map[string]string{
		"phase_start":         EventPhaseStart,
		"phase_complete":      EventPhaseComplete,
		"phase_error":         EventPhaseError,
		"build_stdout":        EventBuildStdout,
		"scope_step":          EventScopeStep,
		"verify_precheck":     EventVerifyPrecheck,
		"verify_result":       EventVerifyResult,
		"run_complete":        EventRunComplete,
		"run_awaiting_review": EventRunAwaitingReview,
	}

	for wantVal, gotVal := range expected {
		if gotVal != wantVal {
			t.Errorf("constant for %q = %q, want %q", wantVal, gotVal, wantVal)
		}
	}
}

func TestNoOpSink_Emit(t *testing.T) {
	var sink EventSink = &NoOpSink{}
	// Must not panic.
	sink.Emit(RunEvent{
		RunID:     "run-1",
		Type:      EventPhaseStart,
		Payload:   nil,
		Timestamp: time.Now(),
	})
}

func TestChannelSink_Emit(t *testing.T) {
	ch := make(chan RunEvent, 1)
	sink := NewChannelSink(ch)

	now := time.Now()
	ev := RunEvent{
		RunID:     "run-456",
		Type:      EventRunComplete,
		Payload:   "done",
		Timestamp: now,
	}

	sink.Emit(ev)

	select {
	case got := <-ch:
		if got.RunID != "run-456" {
			t.Errorf("RunID = %q, want %q", got.RunID, "run-456")
		}
		if got.Type != EventRunComplete {
			t.Errorf("Type = %q, want %q", got.Type, EventRunComplete)
		}
		if got.Payload != "done" {
			t.Errorf("Payload = %v, want %q", got.Payload, "done")
		}
		if got.Timestamp != now {
			t.Errorf("Timestamp mismatch")
		}
	default:
		t.Fatal("expected event on channel, got nothing")
	}
}

func TestChannelSink_ImplementsEventSink(t *testing.T) {
	ch := make(chan RunEvent, 1)
	var _ EventSink = NewChannelSink(ch)
}
