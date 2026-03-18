package templates

import (
	_ "embed"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/ponchione/agent-conductor/internal/config"
)

const DefaultBuildPrompt = `
You are a Build Agent implementing a feature.
The Context Package is a JSON document with three sections: work_order, scope, and directives.
You are already on the correct git branch. Do NOT create or switch branches.

IMPLEMENTATION:
1. Implement the changes described in work_order.acceptance_criteria.
2. Modify the files listed in scope.files_to_modify.
3. Reference scope.files_to_reference for existing patterns and conventions.
4. Use scope.relevant_code for additional context on related functions.

COMMIT:
5. Commit your changes with clear, descriptive commit messages.
6. Do NOT push the branch.

CONSTRAINTS:
- Do not modify files outside the scope unless strictly necessary, and document the reason in the commit message.
- If directives.reference_module_note is present, follow its guidance.
`

// DefaultInitPrompt is the system prompt for the init command.
// It instructs Claude to derive a project config and bootstrap work order from a spec.
const DefaultInitPrompt = `
You are a senior software architect analyzing a feature specification to
produce a project configuration and a bootstrap work order for an AI coding
agent pipeline.

Return a single JSON object (no markdown, no extra text) matching this schema:

{
  "project_config": {
    "project_name": "my-project",
    "project_language": "python",
    "project_framework": "fastapi",
    "index_include": ["**/*.py", "**/*.yaml", "**/*.toml"],
    "index_exclude": ["**/__pycache__/**", "**/*.pyc", "**/.venv/**", "**/dist/**"],
    "module_path": "src/myproject",
    "module_structure": ["src/myproject/api", "src/myproject/models", "tests"],
    "shared_path": "src/myproject/common",
    "sql_path": "sql/"
  },
  "bootstrap": {
    "title": "Bootstrap project skeleton",
    "type": "bootstrap",
    "target_module": ".",
    "reference_module": "",
    "known_files": ["files expected to be created during bootstrap"],
    "requirements": [
      {
        "id": "REQ-1",
        "text": "Concrete bootstrap requirement"
      }
    ],
    "acceptance_criteria": [
      {
        "id": "AC-1",
        "description": "Concrete verifiable bootstrap outcome",
        "requirement_ids": ["REQ-1"],
        "required": true,
        "verification": {
          "kind": "diff_review",
          "focus": ["path/or/module"]
        }
      }
    ],
    "constraints": ["list of things to avoid"]
  }
}

PROJECT_CONFIG RULES:
- project_name: lowercase, hyphenated name derived from the spec.
- project_language: the primary language (python, go, typescript, rust, etc.).
- project_framework: the primary framework if any (fastapi, gin, next, etc.), or empty string.
- index_include: glob patterns for files the pipeline should index. Use **/ prefix.
  Include source files, config files (yaml, toml, json), and Dockerfile if present.
- index_exclude: glob patterns to skip. Always use **/ prefix.
  Include language-specific build artifacts, virtual environments, caches, and data dirs.
- module_path: the root source directory (e.g. "src/myproject", "internal", "cmd").
- module_structure: list of directories that form the project's package structure.
- shared_path: shared/common utilities directory if applicable, empty string if none.
- sql_path: SQL files directory if applicable, empty string if none.

The project_config will be used to produce this YAML structure:

  project:
    name: "{project_name}"
    path: "/path/to/your/project"
    language: "{project_language}"
    framework: "{project_framework}"
  index:
    include: {index_include}
    exclude: {index_exclude}
    max_rag_results: 30
    max_tree_lines: 200
    auto_reindex: true
  conventions:
    module_path: "{module_path}"
    module_structure: {module_structure}
    shared_path: "{shared_path}"
    sql_path: "{sql_path}"
  verify:
    commands:
      build: ...
      test: ...
  safety:
    max_files_changed: 50
    max_duration_mins: 60
  guardrails:
    max_investigation_targets: 6
    max_sub_calls_total: 12
    phase_timeout_seconds: 300
    max_cost_per_phase_usd: 0.50
    warn_cost_per_phase_usd: 0.10

Important:
- The generated project.yaml is project-local only.
- Do NOT include machine-level model providers, embedding endpoints, git defaults,
  executor defaults, or prompt defaults in your reasoning.
- Conductor init will supply standard verify.commands entries for common languages
  such as Go, Python, TypeScript/JavaScript, and Rust.

BOOTSTRAP WORK ORDER RULES:
- type MUST be "bootstrap".
- Use schema-version-2 semantics:
  - include explicit "requirements"
  - emit typed "acceptance_criteria" objects, not legacy string arrays
  - every acceptance criterion must include "id", "description",
    "requirement_ids", "required", and "verification"
- known_files SHOULD list the concrete files bootstrap is expected to create or
  touch first (for example dependency manifest, entry point, config, .gitignore,
  Dockerfile, or key source files).
- acceptance_criteria should cover project scaffolding such as dependency
  manifest, entry point, config, Dockerfile if applicable, and .gitignore.
- Prefer "diff_review" for bootstrap structure and file-creation assertions.
- You MAY use "precheck" only with "check: build" or "check: test" when
  the language naturally supports those validations.
- Each criterion must be objectively verifiable; avoid vague claims.
- constraints should list concrete things to avoid or preserve.

Respond ONLY with the JSON object. No markdown fences, no commentary.
`

//go:embed defaults/build.md
var defaultBuild string

//go:embed defaults/describe.md
var defaultDescribe string

//go:embed defaults/scope_decompose.md
var defaultScopeDecompose string

//go:embed defaults/scope_analyze.md
var defaultScopeAnalyze string

//go:embed defaults/scope_crosscut.md
var defaultScopeCrosscut string

//go:embed defaults/scope_synthesize.md
var defaultScopeSynthesize string

//go:embed defaults/verify_analyze.md
var defaultVerifyAnalyze string

//go:embed defaults/verify_synthesize.md
var defaultVerifySynthesize string

//go:embed defaults/bootstrap.md
var defaultBootstrap string

//go:embed defaults/plan.md
var DefaultPlanPrompt string

//go:embed defaults/plan_audit.md
var DefaultPlanAuditPrompt string

// LoadedPrompts holds the resolved prompt strings for all pipeline phases.
type LoadedPrompts struct {
	Build     string
	Plan      string
	PlanAudit string

	// Per-step fields for recursive pipeline
	ScopeDecompose   string
	ScopeAnalyze     string
	ScopeCrosscut    string
	ScopeSynthesize  string
	VerifyAnalyze    string
	VerifySynthesize string
	Describe         string
	Bootstrap        string
}

// LoadPrompts resolves each prompt from disk (if configured) or falls back to the compiled defaults.
func LoadPrompts(cfg *config.ProjectConfig) (*LoadedPrompts, error) {
	build, err := loadPrompt(cfg.Project.Path, "build", cfg.Prompts.Build, defaultBuild)
	if err != nil {
		return nil, fmt.Errorf("build prompt: %w", err)
	}
	plan, err := loadPrompt(cfg.Project.Path, "plan", cfg.Prompts.Plan, DefaultPlanPrompt)
	if err != nil {
		return nil, fmt.Errorf("plan prompt: %w", err)
	}
	planAudit, err := loadPrompt(cfg.Project.Path, "plan_audit", cfg.Prompts.PlanAudit, DefaultPlanAuditPrompt)
	if err != nil {
		return nil, fmt.Errorf("plan_audit prompt: %w", err)
	}

	scopeDecompose, err := loadPrompt(cfg.Project.Path, "scope_decompose", cfg.Prompts.ScopeDecompose, defaultScopeDecompose)
	if err != nil {
		return nil, fmt.Errorf("scope_decompose prompt: %w", err)
	}
	scopeAnalyze, err := loadPrompt(cfg.Project.Path, "scope_analyze", cfg.Prompts.ScopeAnalyze, defaultScopeAnalyze)
	if err != nil {
		return nil, fmt.Errorf("scope_analyze prompt: %w", err)
	}
	scopeCrosscut, err := loadPrompt(cfg.Project.Path, "scope_crosscut", cfg.Prompts.ScopeCrosscut, defaultScopeCrosscut)
	if err != nil {
		return nil, fmt.Errorf("scope_crosscut prompt: %w", err)
	}
	scopeSynthesize, err := loadPrompt(cfg.Project.Path, "scope_synthesize", cfg.Prompts.ScopeSynthesize, defaultScopeSynthesize)
	if err != nil {
		return nil, fmt.Errorf("scope_synthesize prompt: %w", err)
	}
	verifyAnalyze, err := loadPrompt(cfg.Project.Path, "verify_analyze", cfg.Prompts.VerifyAnalyze, defaultVerifyAnalyze)
	if err != nil {
		return nil, fmt.Errorf("verify_analyze prompt: %w", err)
	}
	verifySynthesize, err := loadPrompt(cfg.Project.Path, "verify_synthesize", cfg.Prompts.VerifySynthesize, defaultVerifySynthesize)
	if err != nil {
		return nil, fmt.Errorf("verify_synthesize prompt: %w", err)
	}
	describe, err := loadPrompt(cfg.Project.Path, "describe", cfg.Prompts.Describe, defaultDescribe)
	if err != nil {
		return nil, fmt.Errorf("describe prompt: %w", err)
	}

	return &LoadedPrompts{
		Build:            build,
		Plan:             plan,
		PlanAudit:        planAudit,
		ScopeDecompose:   scopeDecompose,
		ScopeAnalyze:     scopeAnalyze,
		ScopeCrosscut:    scopeCrosscut,
		ScopeSynthesize:  scopeSynthesize,
		VerifyAnalyze:    verifyAnalyze,
		VerifySynthesize: verifySynthesize,
		Describe:         describe,
	}, nil
}

// LoadPromptsForPlan loads only the prompts needed by conductor plan.
func LoadPromptsForPlan(cfg *config.ProjectConfig) (*LoadedPrompts, error) {
	plan, err := loadPrompt(cfg.Project.Path, "plan", cfg.Prompts.Plan, DefaultPlanPrompt)
	if err != nil {
		return nil, fmt.Errorf("plan prompt: %w", err)
	}
	planAudit, err := loadPrompt(cfg.Project.Path, "plan_audit", cfg.Prompts.PlanAudit, DefaultPlanAuditPrompt)
	if err != nil {
		return nil, fmt.Errorf("plan_audit prompt: %w", err)
	}

	return &LoadedPrompts{
		Plan:      plan,
		PlanAudit: planAudit,
	}, nil
}

// LoadPromptsForBootstrap loads only the prompts needed for bootstrap work orders.
// Scope prompts are left empty (bootstrap skips the scope LLM). Build is resolved
// through the normal 3-tier mechanism. Verify and describe use compiled defaults.
func LoadPromptsForBootstrap(cfg *config.ProjectConfig) (*LoadedPrompts, error) {
	bootstrap, err := loadPrompt(cfg.Project.Path, "bootstrap", cfg.Prompts.Bootstrap, defaultBootstrap)
	if err != nil {
		return nil, fmt.Errorf("bootstrap prompt: %w", err)
	}

	return &LoadedPrompts{
		Build:            bootstrap,
		Bootstrap:        bootstrap,
		VerifyAnalyze:    defaultVerifyAnalyze,
		VerifySynthesize: defaultVerifySynthesize,
		Describe:         defaultDescribe,
	}, nil
}

func loadPrompt(projectPath, phase, filePath, fallback string) (string, error) {
	// Tier 1: repo-local .prompts/<phase>-prompt.md override
	overridePath := filepath.Join(projectPath, ".prompts", phase+"-prompt.md")
	if data, err := os.ReadFile(overridePath); err == nil {
		slog.Debug("Using .prompts override", "phase", phase, "path", overridePath)
		return string(data), nil
	}

	// Tier 2: explicit path from project.yaml
	if filePath != "" {
		absPath := filepath.Join(projectPath, filePath)
		data, err := os.ReadFile(absPath)
		if err != nil {
			if os.IsNotExist(err) {
				return "", fmt.Errorf("prompt file not found: %s", absPath)
			}
			return "", fmt.Errorf("failed to read prompt file %s: %w", absPath, err)
		}
		return string(data), nil
	}

	// Tier 3: compiled default
	return fallback, nil
}
