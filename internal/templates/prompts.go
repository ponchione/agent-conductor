package templates

import (
	_ "embed"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/ponchione/agent-conductor/internal/config"
)

const DefaultScopePrompt = `
You are a strict, deterministic codebase analyzer.
Your ONLY job is to map the provided Work Order to the existing Repository Context.

DO NOT act as a software architect. DO NOT design APIs, middleware, or databases unless they are explicitly requested in the Work Order. DO NOT invent file paths, patterns, or dependencies.

Respond ONLY in valid JSON matching this schema:

FIELD DEFINITIONS:
- summary: Precise, objective summary of the required changes based strictly on the Work Order.
- estimated_complexity: must be exactly one of "low", "medium", or "high".
- files_to_modify: array of objects. EACH object must have "path" (string) and "reason" (string).
- files_to_reference: array of objects. EACH object must have "path" (string) and "reason" (string).
- sql_files: array of objects. EACH object must have "path" (string) and "reason" (string).
- new_files: array of objects. EACH object must have "path" (string) and "purpose" (string).
  NEVER return bare strings for file arrays.
  WRONG: ["internal/scoring/hitfactor.go"]
  RIGHT: [{"path": "internal/scoring/hitfactor.go", "purpose": "Implement Hit Factor scoring logic"}]
- dependencies: array of strings. MUST BE EMPTY unless explicitly adding a package.

RESPONSE TEMPLATE (use empty arrays as your default baseline):
{
  "summary": "",
  "estimated_complexity": "low",
  "files_to_modify": [],
  "files_to_reference": [],
  "sql_files": [],
  "new_files": [],
  "dependencies": [],
  "build_instructions": "1. Specific step-by-step instructions derived only from the Work Order constraints."
}

CRITICAL RULES:
1. "files_to_modify": MUST BE EMPTY unless the Work Order explicitly requires changing an existing file.
2. "files_to_reference": You MUST extract exact, existing file paths from the Repository Context.
3. "new_files": Only list exact files explicitly requested in the Work Order.

Analyze the Work Order and Repository Context carefully. Base your output STRICTLY on the provided text.
You are to only provide the json output. Nothing else. Strictly no markdown.
`

const DefaultVerifyPrompt = `
You are a strict QA integration analyzer.

You are provided with:
1. The original Work Order
2. The Context Package (the approved implementation plan)
3. The Git Diff (the actual implementation)

Verify whether the implementation correctly fulfills the Work Order.

You must output a single valid JSON object with NO additional text, markdown, or explanation.

=== FIELD DEFINITIONS ===
Arrays that are NOT empty MUST contain objects with these exact keys:

unscoped_files — WRONG: ["internal/foo.go"]
unscoped_files — RIGHT: [{"path": "internal/foo.go", "reason_concerning": "not in scope"}]

criteria_results: [{"criterion": "string", "met": true, "notes": "string"}]
issues: ["string"]
concerns: ["string"]

=== RESPONSE TEMPLATE ===
{
  "status": "PASS",
  "summary": "Brief, objective description of the verification outcome",
  "scope_drift": {
    "detected": false,
    "unscoped_files": []
  },
  "completeness": {
    "all_criteria_met": true,
    "criteria_results": []
  },
  "pattern_consistency": {
    "follows_conventions": true,
    "issues": []
  },
  "concerns": []
}

Status definitions:
- "PASS": all acceptance criteria met, no scope drift, follows conventions
- "WARN": minor issues (e.g., small unscoped change with clear justification, one criterion partially met) but core feature works
- "FAIL": one or more acceptance criteria unmet, broken code, or significant unscoped changes

Set "status" to "FAIL" if critical requirements are missing or the code appears broken.
Set "status" to "WARN" if there are minor issues but the core feature is functional.

You must respond with json only. Absolutely no markdown is allowed
`

const DefaultBuildPrompt = `
You are a Build Agent implementing a feature.
The Context Package is a JSON document with three sections: work_order, scope, and directives.

SETUP:
1. Create and checkout the branch specified in directives.branch_name.

IMPLEMENTATION:
2. Implement the changes described in work_order.acceptance_criteria.
3. Modify the files listed in scope.files_to_modify.
4. Reference scope.files_to_reference for existing patterns and conventions.
5. Use scope.relevant_code for additional context on related functions.

COMMIT:
6. Commit your changes with clear, descriptive commit messages.
7. Do NOT push the branch.

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
    "known_files": [],
    "acceptance_criteria": ["list of verifiable assertions"],
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
  prompts:
    scope: templates/scope-prompt.md
    verify: templates/verify-prompt.md
    build: templates/build-prompt.md
  executor:
    tool: claude-code
    timeout_minutes: 30
  safety:
    max_files_changed: 50
    max_duration_mins: 60
  guardrails:
    max_investigation_targets: 6
    max_sub_calls_total: 12
    phase_timeout_seconds: 300
    max_cost_per_phase_usd: 0.50
    warn_cost_per_phase_usd: 0.10

BOOTSTRAP WORK ORDER RULES:
- type MUST be "bootstrap".
- known_files MUST be empty. Acceptance criteria define what to create, not known_files.
- acceptance_criteria MUST include creation of:
  templates/scope-prompt.md, templates/verify-prompt.md, templates/build-prompt.md
  These are conductor pipeline prompt templates that future work orders depend on.
- acceptance_criteria should also include project scaffolding: dependency manifest,
  entry point, config, Dockerfile if applicable, and .gitignore.
- Derive language-appropriate build/test commands for acceptance criteria:
  Python: "poetry install succeeds", "poetry run pytest passes"
  Go: "go build ./... passes", "go vet ./... passes"
  TypeScript: "npm install succeeds", "npm run build passes"
  Rust: "cargo build passes", "cargo test passes"
- Each criterion must be objectively verifiable (not subjective like "clean code").
- constraints should list things to avoid (e.g. "No new external dependencies beyond X").

Respond ONLY with the JSON object. No markdown fences, no commentary.
`

// DefaultPlanPrompt is the system prompt for the plan command.
// It instructs Claude to decompose a specification into work orders.
// Not wired into LoadPrompts — used as a standalone constant by the plan command.
const DefaultPlanPrompt = `
You are a senior software architect decomposing a feature specification into
discrete, ordered work orders for an AI coding agent pipeline.

You will be given the specification along with PROJECT CONTEXT that describes the
existing project (language, conventions, file tree). Use this context to produce
accurate, grounded work orders.

Return a single JSON object (no markdown, no extra text) matching this schema:

{
  "work_orders": [
    {
      "title": "Short imperative title (e.g. Add user auth middleware)",
      "type": "new_feature | bug_fix | refactor | schema_change | docs",
      "target_module": "primary directory/package this WO changes",
      "reference_module": "existing module to use as a pattern (optional, empty string if none)",
      "known_files": ["files the agent should definitely read or modify"],
      "acceptance_criteria": ["verifiable assertions that prove the WO is done"],
      "constraints": ["things the agent must NOT do or must avoid"]
    }
  ]
}

SIZING RULES:
- Each work order addresses ONE focused concern.
- Prefer 1-3 files changed per work order. If you need more, split the work order.
- Schema changes (migrations, new tables) go in a separate work order from the code that consumes them.
- Large features should be split into 3-7 work orders; trivial features may be 1-2.

DEPENDENCY ORDERING:
- Order work orders so each can be built and verified independently in sequence.
- Shared utilities and types before callers.
- Config and schema before consumers.
- Lower layers before upper layers.

ACCEPTANCE CRITERIA STANDARDS:
- Each criterion must be objectively verifiable (not subjective like "clean code").
- Derive language-appropriate build/test commands from the project context:
  Python: "poetry install succeeds", "poetry run pytest passes" (or pip equivalent)
  Go: "go build ./... passes", "go vet ./... passes", "go test ./... passes"
  TypeScript: "npm install succeeds", "npm run build passes", "npm test passes"
  Rust: "cargo build passes", "cargo test passes"
- Describe observable behavior, not implementation details.
- Include negative criteria where relevant ("X does NOT happen when Y").

CONSTRAINT STANDARDS:
- Name specific files that must NOT be modified (e.g. "Do NOT modify cmd/root.go").
- Name specific packages that must NOT be imported.
- Include "No new external dependencies" when applicable.
- Include build/test commands that must keep passing.

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

// LoadedPrompts holds the resolved prompt strings for all pipeline phases.
type LoadedPrompts struct {
	// Existing (backward-compatible)
	Scope  string
	Verify string
	Build  string

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
	scope, err := loadPrompt(cfg.Project.Path, "scope", cfg.Prompts.Scope, DefaultScopePrompt)
	if err != nil {
		return nil, fmt.Errorf("scope prompt: %w", err)
	}
	verify, err := loadPrompt(cfg.Project.Path, "verify", cfg.Prompts.Verify, DefaultVerifyPrompt)
	if err != nil {
		return nil, fmt.Errorf("verify prompt: %w", err)
	}
	build, err := loadPrompt(cfg.Project.Path, "build", cfg.Prompts.Build, defaultBuild)
	if err != nil {
		return nil, fmt.Errorf("build prompt: %w", err)
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
		Scope:            scope,
		Verify:           verify,
		Build:            build,
		ScopeDecompose:   scopeDecompose,
		ScopeAnalyze:     scopeAnalyze,
		ScopeCrosscut:    scopeCrosscut,
		ScopeSynthesize:  scopeSynthesize,
		VerifyAnalyze:    verifyAnalyze,
		VerifySynthesize: verifySynthesize,
		Describe:         describe,
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
