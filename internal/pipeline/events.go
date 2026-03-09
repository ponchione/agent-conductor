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
)
