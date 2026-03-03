package config

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"gopkg.in/yaml.v3"
)

// ProjectConfig maps to the project.yaml schema for a single-project orchestrator.
type ProjectConfig struct {
	Project     Project     `yaml:"project"`
	Index       Index       `yaml:"index"`
	Conventions Conventions `yaml:"conventions"`
	Prompts     Prompts     `yaml:"prompts"`
	LocalModel  LocalModel  `yaml:"local_model"`
	EmbedModel  EmbedModel  `yaml:"embed_model"`
	Safety      Safety      `yaml:"safety"`
	Git         Git         `yaml:"git"`
	Executor    Executor    `yaml:"executor"`
}

type Project struct {
	Name      string `yaml:"name"`
	Path      string `yaml:"path"`
	Language  string `yaml:"language"`
	Framework string `yaml:"framework"`
	DataDir   string // computed — not configurable via YAML
}

type Index struct {
	Include       []string `yaml:"include"`
	Exclude       []string `yaml:"exclude"`
	MaxRAGResults int      `yaml:"max_rag_results"`
	MaxTreeLines  int      `yaml:"max_tree_lines"`
	AutoReindex   bool     `yaml:"auto_reindex"`
}

type Conventions struct {
	ModulePath      string   `yaml:"module_path"`
	ModuleStructure []string `yaml:"module_structure"`
	SharedPath      string   `yaml:"shared_path"`
	SQLPath         string   `yaml:"sql_path"`
	DocsPath        string   `yaml:"docs_path"`
}

type Prompts struct {
	Scope  string `yaml:"scope"`
	Verify string `yaml:"verify"`
	Build  string `yaml:"build"`
}

type EmbedModel struct {
	Endpoint       string `yaml:"endpoint"`
	ModelName      string `yaml:"model_name"`
	TimeoutSeconds int    `yaml:"timeout_seconds"`
}

type LocalModel struct {
	Endpoint       string  `yaml:"endpoint"`
	ModelName      string  `yaml:"model_name"`
	Temperature    float64 `yaml:"temperature"`
	TimeoutSeconds int     `yaml:"timeout_seconds"`
}

type Safety struct {
	ForbiddenPaths  []string `yaml:"forbidden_paths"`
	MaxFilesChanged int      `yaml:"max_files_changed"`
	MaxDurationMins int      `yaml:"max_duration_mins"`
}

type Executor struct {
	Tool           string           `yaml:"tool"`
	TimeoutMinutes int              `yaml:"timeout_minutes"`
	ClaudeCode     ClaudeCodeConfig `yaml:"claude_code"`
	OpenCode       OpenCodeConfig   `yaml:"opencode"`
}

// ClaudeCodeConfig is reserved for future tool-specific config.
type ClaudeCodeConfig struct{}

type OpenCodeConfig struct {
	Agent string `yaml:"agent"`
}

type Git struct {
	BranchPrefix      string `yaml:"branch_prefix"`
	CommitAuthorName  string `yaml:"commit_author_name"`
	CommitAuthorEmail string `yaml:"commit_author_email"`
}

// Load reads and merges the global and project config files.
// Layer order: hardcoded defaults → global (~/.conductor/config.yaml) → project config.
// DataDir is always computed, never read from YAML.
func Load(projectPath string) (*ProjectConfig, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("could not determine home directory: %w", err)
	}

	cfg := &ProjectConfig{
		LocalModel: LocalModel{
			Endpoint:       "http://localhost:8080/v1",
			Temperature:    0.0,
			TimeoutSeconds: 60,
		},
		EmbedModel: EmbedModel{
			Endpoint:       "http://localhost:8081/v1",
			ModelName:      "nomic-embed-code",
			TimeoutSeconds: 30,
		},
		Git: Git{
			BranchPrefix:      "feature/conducted",
			CommitAuthorName:  "Agent Conductor",
			CommitAuthorEmail: "conductor@local",
		},
		Safety: Safety{
			MaxFilesChanged: 50,
			MaxDurationMins: 60,
		},
		Executor: Executor{
			Tool: "claude-code",
		},
	}

	globalPath := filepath.Join(home, "source", ".conductor", "config.yaml")
	if globalData, err := os.ReadFile(globalPath); err == nil {
		if err := yaml.Unmarshal(globalData, cfg); err != nil {
			return nil, fmt.Errorf("failed to parse global config: %w", err)
		}
	}

	projectData, err := os.ReadFile(projectPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}
	if err := yaml.Unmarshal(projectData, cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config: %w", err)
	}

	if cfg.Index.MaxRAGResults == 0 {
		cfg.Index.MaxRAGResults = 30
	}
	if cfg.Index.MaxTreeLines == 0 {
		cfg.Index.MaxTreeLines = 200
	}

	cfg.Project.DataDir = filepath.Join(home, "source", ".conductor", "projects", cfg.Project.Name)

	return cfg, nil
}

// GetTimeout returns the configured timeout as a duration.
func (lm LocalModel) GetTimeout() time.Duration {
	return time.Duration(lm.TimeoutSeconds) * time.Second
}

// Validate checks that required fields are present.
func Validate(cfg *ProjectConfig) error {
	if cfg.Project.Name == "" {
		return fmt.Errorf("project.name is required")
	}
	if cfg.Project.Path == "" {
		return fmt.Errorf("project.path is required")
	}
	return nil
}

// EnsureDataDirs creates the standard subdirectory layout under DataDir.
func EnsureDataDirs(cfg *ProjectConfig) error {
	dirs := []string{
		filepath.Join(cfg.Project.DataDir, "db"),
		filepath.Join(cfg.Project.DataDir, "rag"),
		filepath.Join(cfg.Project.DataDir, "artifacts", "context-packages"),
		filepath.Join(cfg.Project.DataDir, "artifacts", "verify-reports"),
		filepath.Join(cfg.Project.DataDir, "logs"),
	}
	for _, d := range dirs {
		if err := os.MkdirAll(d, 0755); err != nil {
			return fmt.Errorf("failed to create directory %s: %w", d, err)
		}
	}
	return nil
}
