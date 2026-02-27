package pipelineerrors

import "fmt"

// Class categorises a pipeline error so the dispatch loop can branch on it.
type Class int

const (
	Retryable  Class = iota // transient; retry if attempts remain
	Fatal                   // permanent; never retry
	NeedsHuman              // needs human review before continuing
)

func (c Class) String() string {
	switch c {
	case Retryable:
		return "retryable"
	case Fatal:
		return "fatal"
	case NeedsHuman:
		return "needs_human"
	default:
		return "unknown"
	}
}

// PipelineError is a structured error produced by a workflow phase handler.
type PipelineError struct {
	Phase      string
	WorkflowID string
	TaskID     string
	Class      Class
	Err        error
}

func (e *PipelineError) Error() string {
	scope := fmt.Sprintf("%s wf=%s task=%s", e.Phase, e.WorkflowID, e.TaskID)
	return fmt.Sprintf("%s [%s]: %v", scope, e.Class, e.Err)
}

func (e *PipelineError) Unwrap() error {
	return e.Err
}

// Retryablef creates a Retryable PipelineError.
func Retryablef(phase, wfID, taskID, format string, args ...any) *PipelineError {
	return &PipelineError{
		Phase:      phase,
		WorkflowID: wfID,
		TaskID:     taskID,
		Class:      Retryable,
		Err:        fmt.Errorf(format, args...),
	}
}

// Fatalf creates a Fatal PipelineError.
func Fatalf(phase, wfID, taskID, format string, args ...any) *PipelineError {
	return &PipelineError{
		Phase:      phase,
		WorkflowID: wfID,
		TaskID:     taskID,
		Class:      Fatal,
		Err:        fmt.Errorf(format, args...),
	}
}

// NeedsHumanf creates a NeedsHuman PipelineError.
func NeedsHumanf(phase, wfID, taskID, format string, args ...any) *PipelineError {
	return &PipelineError{
		Phase:      phase,
		WorkflowID: wfID,
		TaskID:     taskID,
		Class:      NeedsHuman,
		Err:        fmt.Errorf(format, args...),
	}
}
