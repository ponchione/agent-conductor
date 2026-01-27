package config

import (
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	LogLevel string                `yaml:"log_level"`
	LogFile  string                `yaml:"log_file"`
	Database string                `yaml:"database"`
	Scanner  ScannerConfig         `yaml:"scanner"`
	Workers  WorkerConfig          `yaml:"workers"`
	Repos    map[string]RepoConfig `yaml:"repositories"`
	Safety   SafetyConfig          `yaml:"safety"`
	Git      GitConfig             `yaml:"git"`
	GitHub   GitHubConfig          `yaml:"github"`
	Gates    GatesConfig           `yaml:"gates"`
}

type ScannerConfig struct {
	IntervalSeconds int    `yaml:"interval_seconds"`
	InboxPath       string `yaml:"inbox_path"`
}

type WorkerConfig struct {
	Count int `yaml:"count"`
}

type RepoConfig struct {
	Path                   string   `yaml:"path"`
	OpenCodeAgentExecutor  string   `yaml:"opencode_agent_executor"`
	OpenCodeAgentWorkOrder string   `yaml:"opencode_agent_workorder"`
	ForbiddenPaths         []string `yaml:"forbidden_paths"`
	AllowedPaths           []string `yaml:"allowed_paths"`
}

type SafetyConfig struct {
	MaxDepth                   int `yaml:"max_depth"`
	MaxFilesChanged            int `yaml:"max_files_changed"`
	MaxTaskDurationMinutes     int `yaml:"max_task_duration_minutes"`
	MaxWorkflowDurationMinutes int `yaml:"max_workflow_duration_minutes"`
	MaxTaskRetries             int `yaml:"max_task_retries"`
}

type GitConfig struct {
	BranchPrefix      string `yaml:"branch_prefix"`
	CommitAuthorName  string `yaml:"commit_author_name"`
	CommitAuthorEmail string `yaml:"commit_author_email"`
}

type GitHubConfig struct {
	CreatePRs bool     `yaml:"create_prs"`
	PRLabels  []string `yaml:"pr_labels"`
}

type GatesConfig struct {
	PlanningComplete    bool `yaml:"planning_complete"`
	CrossRepoTicket     bool `yaml:"cross_repo_ticket"`
	DepthThreshold      int  `yaml:"depth_threshold"`
	FilesWarningPercent int  `yaml:"files_warning_percent"`
}

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	// Set defaults
	cfg := &Config{
		LogLevel: "info",
		LogFile:  "conductor.log",
		Database: "conductor.db",
		Scanner: ScannerConfig{
			IntervalSeconds: 5,
			InboxPath:       "./inbox",
		},
		Workers: WorkerConfig{
			Count: 1,
		},
		Safety: SafetyConfig{
			MaxDepth:                   5,
			MaxFilesChanged:            50,
			MaxTaskDurationMinutes:     30,
			MaxWorkflowDurationMinutes: 60,
			MaxTaskRetries:             2,
		},
		Git: GitConfig{
			BranchPrefix:      "feature/conducted",
			CommitAuthorName:  "Agent Conductor",
			CommitAuthorEmail: "conductor@local",
		},
		Gates: GatesConfig{
			PlanningComplete:    true,
			CrossRepoTicket:     true,
			DepthThreshold:      5,
			FilesWarningPercent: 80,
		},
	}

	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config: %w", err)
	}

	return cfg, nil
}

// TaskTimeout Helper to get duration
func (s SafetyConfig) TaskTimeout() time.Duration {
	return time.Duration(s.MaxTaskDurationMinutes) * time.Minute
}
