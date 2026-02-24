package config

import (
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

// ProjectConfig replaces the old global Config.
// It maps to the new project.yaml schema for a single-project orchestrator.
type ProjectConfig struct {
	Project     Project     `yaml:"project"`
	Index       Index       `yaml:"index"`
	Conventions Conventions `yaml:"conventions"`
	Prompts     Prompts     `yaml:"prompts"`
	LocalModel  LocalModel  `yaml:"local_model"`
	Safety      Safety      `yaml:"safety"`
	Git         Git         `yaml:"git"`
}

type Project struct {
	Name      string `yaml:"name"`
	Path      string `yaml:"path"`
	Language  string `yaml:"language"`
	Framework string `yaml:"framework"`
	DataDir   string `yaml:"data_dir"`
}

type Index struct {
	Include []string `yaml:"include"`
	Exclude []string `yaml:"exclude"`
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

type LocalModel struct {
	Endpoint       string  `yaml:"endpoint"`
	ModelName      string  `yaml:"model_name"`
	Temperature    float64 `yaml:"temperature"`
	TimeoutSeconds int     `yaml:"timeout_seconds"`
}

type Safety struct {
	ForbiddenPaths []string `yaml:"forbidden_paths"`
}

type Git struct {
	BranchPrefix      string `yaml:"branch_prefix"`
	CommitAuthorName  string `yaml:"commit_author_name"`
	CommitAuthorEmail string `yaml:"commit_author_email"`
}

// Load reads and parses the project.yaml file.
func Load(path string) (*ProjectConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	// Set defaults
	cfg := &ProjectConfig{
		LocalModel: LocalModel{
			Endpoint:       "http://localhost:8080/v1",
			Temperature:    0.0,
			TimeoutSeconds: 60,
		},
		Git: Git{
			BranchPrefix:      "feature/conducted",
			CommitAuthorName:  "Agent Conductor",
			CommitAuthorEmail: "conductor@local",
		},
	}

	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config: %w", err)
	}

	return cfg, nil
}

// GetTimeout returns the configured timeout as a duration.
func (lm LocalModel) GetTimeout() time.Duration {
	return time.Duration(lm.TimeoutSeconds) * time.Second
}
