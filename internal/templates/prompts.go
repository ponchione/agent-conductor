package templates

import (
	"fmt"
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

// LoadedPrompts holds the resolved prompt strings for all three pipeline phases.
type LoadedPrompts struct {
	Scope  string
	Verify string
	Build  string
}

// LoadPrompts resolves each prompt from disk (if configured) or falls back to the compiled defaults.
func LoadPrompts(cfg *config.ProjectConfig) (*LoadedPrompts, error) {
	scope, err := loadPrompt(cfg.Project.Path, cfg.Prompts.Scope, DefaultScopePrompt)
	if err != nil {
		return nil, fmt.Errorf("scope prompt: %w", err)
	}
	verify, err := loadPrompt(cfg.Project.Path, cfg.Prompts.Verify, DefaultVerifyPrompt)
	if err != nil {
		return nil, fmt.Errorf("verify prompt: %w", err)
	}
	build, err := loadPrompt(cfg.Project.Path, cfg.Prompts.Build, DefaultBuildPrompt)
	if err != nil {
		return nil, fmt.Errorf("build prompt: %w", err)
	}
	return &LoadedPrompts{Scope: scope, Verify: verify, Build: build}, nil
}

func loadPrompt(projectPath, filePath, fallback string) (string, error) {
	if filePath == "" {
		return fallback, nil
	}
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
