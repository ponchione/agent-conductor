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
