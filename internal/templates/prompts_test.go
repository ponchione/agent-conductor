package templates

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ponchione/agent-conductor/internal/config"
)

func TestLoadPromptsFallsBackToEmbeddedPlannerPrompts(t *testing.T) {
	cfg := &config.ProjectConfig{}
	cfg.Project.Path = t.TempDir()

	prompts, err := LoadPrompts(cfg)
	if err != nil {
		t.Fatalf("LoadPrompts() error = %v", err)
	}

	if prompts.Plan != DefaultPlanPrompt {
		t.Fatalf("Plan prompt did not use embedded default fallback")
	}
	if prompts.PlanAudit != DefaultPlanAuditPrompt {
		t.Fatalf("PlanAudit prompt did not use embedded default fallback")
	}
}

func TestLoadPromptsLoadsConfiguredPlannerPromptPaths(t *testing.T) {
	projectDir := t.TempDir()
	planPath := filepath.Join(projectDir, "templates", "plan-prompt.md")
	planAuditPath := filepath.Join(projectDir, "templates", "plan-audit.md")
	if err := os.MkdirAll(filepath.Dir(planPath), 0755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}

	wantPlan := "custom plan prompt"
	wantPlanAudit := "custom plan audit prompt"
	if err := os.WriteFile(planPath, []byte(wantPlan), 0644); err != nil {
		t.Fatalf("WriteFile(plan) error = %v", err)
	}
	if err := os.WriteFile(planAuditPath, []byte(wantPlanAudit), 0644); err != nil {
		t.Fatalf("WriteFile(plan_audit) error = %v", err)
	}

	cfg := &config.ProjectConfig{
		Project: config.Project{Path: projectDir},
		Prompts: config.Prompts{
			Plan:      "templates/plan-prompt.md",
			PlanAudit: "templates/plan-audit.md",
		},
	}

	prompts, err := LoadPrompts(cfg)
	if err != nil {
		t.Fatalf("LoadPrompts() error = %v", err)
	}

	if prompts.Plan != wantPlan {
		t.Fatalf("Plan prompt = %q, want %q", prompts.Plan, wantPlan)
	}
	if prompts.PlanAudit != wantPlanAudit {
		t.Fatalf("PlanAudit prompt = %q, want %q", prompts.PlanAudit, wantPlanAudit)
	}
}

func TestLoadPromptsPrefersDotPromptsPlannerOverrides(t *testing.T) {
	projectDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(projectDir, ".prompts"), 0755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}

	wantPlan := "override plan prompt"
	wantPlanAudit := "override plan audit prompt"
	if err := os.WriteFile(filepath.Join(projectDir, ".prompts", "plan-prompt.md"), []byte(wantPlan), 0644); err != nil {
		t.Fatalf("WriteFile(plan override) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(projectDir, ".prompts", "plan_audit-prompt.md"), []byte(wantPlanAudit), 0644); err != nil {
		t.Fatalf("WriteFile(plan_audit override) error = %v", err)
	}

	cfg := &config.ProjectConfig{
		Project: config.Project{Path: projectDir},
		Prompts: config.Prompts{
			Plan:      "templates/ignored-plan.md",
			PlanAudit: "templates/ignored-plan-audit.md",
		},
	}

	prompts, err := LoadPrompts(cfg)
	if err != nil {
		t.Fatalf("LoadPrompts() error = %v", err)
	}

	if prompts.Plan != wantPlan {
		t.Fatalf("Plan prompt = %q, want %q", prompts.Plan, wantPlan)
	}
	if prompts.PlanAudit != wantPlanAudit {
		t.Fatalf("PlanAudit prompt = %q, want %q", prompts.PlanAudit, wantPlanAudit)
	}
}

func TestLoadPromptsReturnsHelpfulErrorForMissingConfiguredPlanPrompt(t *testing.T) {
	cfg := &config.ProjectConfig{
		Project: config.Project{Path: t.TempDir()},
		Prompts: config.Prompts{
			Plan: "templates/missing-plan.md",
		},
	}

	_, err := LoadPrompts(cfg)
	if err == nil {
		t.Fatal("LoadPrompts() error = nil, want missing prompt error")
	}
	if !strings.Contains(err.Error(), "plan prompt: prompt file not found") {
		t.Fatalf("LoadPrompts() error = %q, want plan prompt file not found", err)
	}
}

func TestLoadPromptsForPlanIgnoresUnrelatedPromptConfig(t *testing.T) {
	projectDir := t.TempDir()
	cfg := &config.ProjectConfig{
		Project: config.Project{Path: projectDir},
		Prompts: config.Prompts{
			Scope: "templates/missing-scope.md",
		},
	}

	prompts, err := LoadPromptsForPlan(cfg)
	if err != nil {
		t.Fatalf("LoadPromptsForPlan() error = %v", err)
	}
	if prompts.Plan != DefaultPlanPrompt {
		t.Fatalf("Plan prompt did not use embedded default fallback")
	}
	if prompts.PlanAudit != DefaultPlanAuditPrompt {
		t.Fatalf("PlanAudit prompt did not use embedded default fallback")
	}
}

func TestDefaultPlanPromptMatchesPhaseOnePlanContract(t *testing.T) {
	requiredSnippets := []string{
		"\"requirements\": [",
		"\"non_goals\": [",
		"\"existing_system\": [",
		"\"planning_warnings\": [",
		"\"covers\": [",
		"\"depends_on\": [",
		"`acceptance_criteria` MUST be an array of strings in Phase 1",
		"object-form or typed criteria yet.",
		"Do not invent speculative work streams",
		"Do not rewrite or soften settled specification decisions.",
	}

	for _, snippet := range requiredSnippets {
		if !strings.Contains(DefaultPlanPrompt, snippet) {
			t.Fatalf("DefaultPlanPrompt missing required snippet %q", snippet)
		}
	}
}

func TestDefaultPlanAuditPromptMatchesPhaseOneAuditContract(t *testing.T) {
	requiredSnippets := []string{
		"Requirement IDs must stay stable.",
		"`acceptance_criteria` MUST remain an array of strings in Phase 1.",
		"You may split, merge, replace, reorder, add, or delete work orders",
		"\"audit_action\": \"added | modified | unchanged\"",
		"Do not invent speculative work streams",
		"Do not rewrite or soften settled specification decisions.",
	}

	for _, snippet := range requiredSnippets {
		if !strings.Contains(DefaultPlanAuditPrompt, snippet) {
			t.Fatalf("DefaultPlanAuditPrompt missing required snippet %q", snippet)
		}
	}
}
