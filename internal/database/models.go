package database

import "time"

// Workflow represents a complete chain of work
type Workflow struct {
	ID              string     `json:"id"`
	OriginalIntent  string     `json:"original_intent"`
	OriginalFile    string     `json:"original_file"`
	CurrentState    string     `json:"current_state"`
	TargetRepo      string     `json:"target_repo"`
	GitBranch       string     `json:"git_branch"`
	MaxDepth        int        `json:"max_depth"`
	MaxFilesChanged int        `json:"max_files_changed"`
	MaxDurationMins int        `json:"max_duration_mins"`
	CurrentDepth    int        `json:"current_depth"`
	FilesChanged    int        `json:"files_changed"`
	StartedAt       *time.Time `json:"started_at"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
	CompletedAt     *time.Time `json:"completed_at"`
	ErrorMessage    string     `json:"error_message"`
}

// Task represents a single unit of work
type Task struct {
	ID             string     `json:"id"`
	WorkflowID     string     `json:"workflow_id"`
	SequenceNum    int        `json:"sequence_num"`
	TaskType       string     `json:"task_type"`
	AgentType      string     `json:"agent_type"`
	TargetRepo     string     `json:"target_repo"`
	InputArtifact  string     `json:"input_artifact"`
	OutputArtifact string     `json:"output_artifact"`
	State          string     `json:"state"`
	ClaimedBy      string     `json:"claimed_by"`
	ClaimedAt      *time.Time `json:"claimed_at"`
	Attempts       int        `json:"attempts"`
	MaxAttempts    int        `json:"max_attempts"`
	ExitCode       *int       `json:"exit_code"`
	StdoutLog      string     `json:"stdout_log"`
	StderrLog      string     `json:"stderr_log"`
	FilesChanged   []string   `json:"files_changed"` // Stored as JSON
	CreatedAt      time.Time  `json:"created_at"`
	StartedAt      *time.Time `json:"started_at"`
	CompletedAt    *time.Time `json:"completed_at"`
	ErrorMessage   string     `json:"error_message"`
}

// Event represents an audit log entry
type Event struct {
	ID         int64          `json:"id"`
	WorkflowID string         `json:"workflow_id"`
	TaskID     string         `json:"task_id"`
	EventType  string         `json:"event_type"`
	EventData  map[string]any `json:"event_data"` // Stored as JSON
	CreatedAt  time.Time      `json:"created_at"`
}
